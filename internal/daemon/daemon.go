package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/rursache/always-green-cli/internal/importws"
	"github.com/rursache/always-green-cli/internal/ipc"
	"github.com/rursache/always-green-cli/internal/notify"
	"github.com/rursache/always-green-cli/internal/paths"
	"github.com/rursache/always-green-cli/internal/schedule"
	"github.com/rursache/always-green-cli/internal/slackx"
	"github.com/rursache/always-green-cli/internal/store"
)

type WorkspaceStatus struct {
	TeamID     string `json:"team_id"`
	Name       string `json:"name"`
	Status     string `json:"status"`
	Connected  bool   `json:"connected"`
	TokenValid bool   `json:"token_valid"`
	Uptime     int64  `json:"uptime"`
	LastActive string `json:"last_activity,omitempty"`
	Connects   int    `json:"connections"`
}

type Status struct {
	Running    bool              `json:"running"`
	Workspaces []WorkspaceStatus `json:"workspaces"`
	UpdatedAt  string            `json:"updated_at"`
}

type runtime struct {
	cancel context.CancelFunc
	sess   *slackx.Session
}

// sessionSet guards the live sessions; the reconciler mutates them on the main
// loop while the IPC handler reads them from its own goroutine, so an
// unguarded map here is a fatal "concurrent map iteration and map write"
type sessionSet struct {
	mu   sync.Mutex
	byID map[string]*runtime
}

func newSessionSet() *sessionSet {
	return &sessionSet{byID: map[string]*runtime{}}
}

func (s *sessionSet) put(id string, rt *runtime) {
	s.mu.Lock()
	s.byID[id] = rt
	s.mu.Unlock()
}

func (s *sessionSet) has(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.byID[id]
	return ok
}

func (s *sessionSet) remove(id string) {
	s.mu.Lock()
	delete(s.byID, id)
	s.mu.Unlock()
}

// list returns a copy so callers can iterate without holding the lock
func (s *sessionSet) list() map[string]*runtime {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make(map[string]*runtime, len(s.byID))
	for id, rt := range s.byID {
		out[id] = rt
	}
	return out
}

// drain empties the set and returns what was in it
func (s *sessionSet) drain() []*runtime {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]*runtime, 0, len(s.byID))
	for id, rt := range s.byID {
		out = append(out, rt)
		delete(s.byID, id)
	}
	return out
}

// desktopRetryEvery bounds how often a self-healing workspace re-reads the
// Slack app, since on macOS that can mean a Keychain round trip
const desktopRetryEvery = 30 * time.Minute

// refreshTimeout caps a refresh so a blocked keychain prompt or a stalled
// request cannot hold a session goroutine, and with it a shutdown, forever
const refreshTimeout = 60 * time.Second

// refreshWithin gives up waiting after d; the refresh itself keeps running and
// will simply land on a later attempt if it eventually succeeds
func refreshWithin(st *store.Store, ws store.Workspace, d time.Duration) error {
	res := make(chan error, 1)
	go func() { res <- healWorkspace(st, ws) }()
	select {
	case err := <-res:
		return err
	case <-time.After(d):
		return fmt.Errorf("timed out reading the Slack app after %s", d)
	}
}

// healer tracks in-flight and recent desktop refresh attempts; attempts run off
// the main loop because reading the Keychain can block for a long time
type healer struct {
	mu   sync.Mutex
	last map[string]time.Time
	busy map[string]bool
}

func newHealer() *healer {
	return &healer{last: map[string]time.Time{}, busy: map[string]bool{}}
}

func (h *healer) claim(teamID string, now time.Time) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.busy[teamID] {
		return false
	}
	if at, ok := h.last[teamID]; ok && now.Sub(at) < desktopRetryEvery {
		return false
	}
	h.last[teamID] = now
	h.busy[teamID] = true
	return true
}

func (h *healer) release(teamID string) {
	h.mu.Lock()
	delete(h.busy, teamID)
	h.mu.Unlock()
}

// ErrAlreadyRunning means another daemon holds the lock
var ErrAlreadyRunning = errors.New("another always-green daemon is already running")

// acquireLock takes an exclusive advisory lock held for the process lifetime;
// the kernel drops it however the process dies, so unlike a PID file it cannot
// go stale, and a recycled PID cannot masquerade as a live daemon
func acquireLock() (*os.File, error) {
	f, err := os.OpenFile(paths.DaemonLock(), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = f.Close()
		return nil, ErrAlreadyRunning
	}
	return f, nil
}

func RunForeground() error {
	if err := paths.EnsureDir(); err != nil {
		return err
	}
	lock, err := acquireLock()
	if err != nil {
		return err
	}
	defer lock.Close()
	logf, err := os.OpenFile(paths.DaemonLog(), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer logf.Close()
	log.SetOutput(logf)
	log.SetFlags(log.LstdFlags)

	if err := os.WriteFile(paths.DaemonPID(), []byte(strconv.Itoa(os.Getpid())), 0o600); err != nil {
		return err
	}
	defer os.Remove(paths.DaemonPID())
	defer os.Remove(paths.DaemonSock())

	st, err := store.Open()
	if err != nil {
		return err
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sessions := newSessionSet()
	heal := newHealer()
	reload := make(chan struct{}, 1)
	kick := func() {
		select {
		case reload <- struct{}{}:
		default:
		}
	}

	var ln net.Listener
	ln, err = ipc.Listen(func(cmd string) string {
		switch cmd {
		case "reload":
			kick()
			return "ok"
		case "stop":
			cancel()
			return "stopping"
		case "ping":
			return "ok"
		case "status":
			return ipc.Encode(snapshot(true, sessions))
		default:
			return "unknown command"
		}
	})
	if err != nil {
		return err
	}
	defer ln.Close()

	sigc := make(chan os.Signal, 1)
	signal.Notify(sigc, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigc
		cancel()
	}()

	log.Printf("daemon starting")
	syncSessions(ctx, st, sessions, heal, kick)
	_ = writeStatus(true, sessions)

	tick := time.NewTicker(5 * time.Second)
	sched := time.NewTicker(60 * time.Second)
	defer tick.Stop()
	defer sched.Stop()

	for {
		select {
		case <-ctx.Done():
			stopAll(sessions)
			_ = writeStatus(false, sessions)
			log.Printf("daemon stopped")
			return nil
		case <-reload:
			syncSessions(ctx, st, sessions, heal, kick)
		case <-sched.C:
			syncSessions(ctx, st, sessions, heal, kick)
		case <-tick.C:
			_ = writeStatus(true, sessions)
		}
	}
}

func Start() error {
	if Running() {
		return nil
	}
	if err := paths.EnsureDir(); err != nil {
		return err
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	logf, err := os.OpenFile(paths.DaemonLog(), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer logf.Close()
	cmd := exec.Command(exe, "daemon")
	cmd.Stdout = logf
	cmd.Stderr = logf
	cmd.Stdin = nil
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return err
	}
	// wait for the socket to answer: Running() only proves the process exists,
	// and a reload sent before ipc.Listen binds would be silently lost
	for i := 0; i < 60; i++ {
		time.Sleep(100 * time.Millisecond)
		if Ready() {
			return nil
		}
	}
	return fmt.Errorf("daemon did not start, see %s", paths.DaemonLog())
}

// Ready reports whether the daemon is listening and answering
func Ready() bool {
	_, err := ipc.Send("ping")
	return err == nil
}

func Stop() error {
	if !Running() {
		return nil
	}
	// a shutdown can be waiting on a slow keychain read or websocket close,
	// so give it room before escalating
	if _, err := ipc.Send("stop"); err == nil {
		if waitGone(15 * time.Second) {
			return nil
		}
	}
	pid, ok := readPID()
	if !ok {
		return nil
	}
	_ = syscall.Kill(pid, syscall.SIGTERM)
	if waitGone(10 * time.Second) {
		return nil
	}
	_ = syscall.Kill(pid, syscall.SIGKILL)
	if !waitGone(2 * time.Second) {
		return fmt.Errorf("daemon (PID %d) did not exit", pid)
	}
	// SIGKILL cannot run the daemon's own cleanup
	_ = os.Remove(paths.DaemonPID())
	_ = os.Remove(paths.DaemonSock())
	return nil
}

func waitGone(within time.Duration) bool {
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		if !Running() {
			return true
		}
		time.Sleep(100 * time.Millisecond)
	}
	return !Running()
}

func Reload() error {
	_, err := ipc.Send("reload")
	return err
}

// Running reports whether a daemon holds the lock; probing by taking the lock
// and dropping it immediately means a crashed daemon is never mistaken for a
// live one, and neither is an unrelated process that inherited its PID
func Running() bool {
	if err := paths.EnsureDir(); err != nil {
		return false
	}
	f, err := os.OpenFile(paths.DaemonLock(), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return false
	}
	defer f.Close()
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		return true
	}
	_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
	return false
}

func PID() int {
	pid, _ := readPID()
	return pid
}

func ReadStatus() (Status, error) {
	raw, err := os.ReadFile(paths.StatusFile())
	if err != nil {
		return Status{}, err
	}
	var st Status
	if err := json.Unmarshal(raw, &st); err != nil {
		return Status{}, err
	}
	return st, nil
}

// readPID is for display and for signalling; liveness comes from the lock
func readPID() (int, bool) {
	raw, err := os.ReadFile(paths.DaemonPID())
	if err != nil {
		return 0, false
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(raw)))
	if err != nil || pid <= 0 {
		return 0, false
	}
	return pid, true
}

func syncSessions(ctx context.Context, st *store.Store, sessions *sessionSet, heal *healer, kick func()) {
	cfg, _ := st.Config()
	tz := cfg.Timezone
	if tz == "" {
		tz = schedule.DetectTimezone()
	}
	list, err := st.Workspaces()
	if err != nil {
		log.Printf("load workspaces: %v", err)
		return
	}
	now := time.Now()
	want := map[string]store.Workspace{}
	for _, ws := range list {
		// a workspace imported from the Slack app can fix itself: the app keeps
		// rotating its own tokens, so re-read them instead of nagging the user
		if ws.TokenInvalid {
			tryHeal(st, heal, kick, ws, now)
		}
		if ws.Eligible(now, tz) {
			want[ws.TeamID] = ws
		}
	}

	for id, rt := range sessions.list() {
		ws, ok := want[id]
		if !ok {
			log.Printf("stopping %s", rt.sess.Name)
			rt.cancel()
			sessions.remove(id)
			continue
		}
		if rt.sess.Xoxc != ws.Xoxc || rt.sess.Xoxd != ws.Xoxd {
			log.Printf("tokens changed for %s", ws.Name)
			rt.cancel()
			sessions.remove(id)
			continue
		}
		snap := rt.sess.Snapshot()
		if snap.Status == "invalid_token" {
			continue
		}
		if !rt.sess.Running() || snap.Status == "error" {
			log.Printf("restarting %s", ws.Name)
			rt.cancel()
			sessions.remove(id)
		}
	}

	for id, ws := range want {
		if sessions.has(id) {
			continue
		}
		startSession(ctx, st, sessions, heal, kick, ws)
	}
}

func tryHeal(st *store.Store, heal *healer, kick func(), ws store.Workspace, now time.Time) {
	if !heal.claim(ws.TeamID, now) {
		return
	}
	name := ws.Name
	teamID := ws.TeamID
	go func() {
		defer heal.release(teamID)
		if err := healWorkspace(st, ws); err != nil {
			log.Printf("[%s] token refresh failed: %v", name, err)
			return
		}
		log.Printf("[%s] refreshed tokens", name)
		kick()
	}()
}

// healWorkspace re-mints a token without troubling the user; a workspace from
// the Slack app re-reads the app; one pasted from Chrome mints a new xoxc from
// its d cookie, which outlives the token by months
func healWorkspace(st *store.Store, ws store.Workspace) error {
	if ws.Source == store.SourceDesktop {
		return importws.RefreshDesktop(st, ws.TeamID)
	}
	return importws.RefreshCookie(st, ws.TeamID)
}

// onTokenDead is called from a session goroutine once Slack rejects the tokens
func onTokenDead(st *store.Store, heal *healer, kick func(), teamID, name string) {
	// share the throttle with the periodic retry so tokens that keep passing
	// auth.test but keep failing the websocket cannot spin the Slack app
	ws, ok := st.Workspace(teamID)
	fromDesktop := ok && ws.Source == store.SourceDesktop
	if ok && heal.claim(teamID, time.Now()) {
		// bounded: a keychain read can block on a prompt, and this runs on the
		// session goroutine that a shutdown may be waiting for
		err := refreshWithin(st, ws, refreshTimeout)
		heal.release(teamID)
		if err == nil {
			log.Printf("[%s] tokens expired, refreshed automatically", name)
			kick()
			return
		}
		log.Printf("[%s] automatic refresh failed: %v", name, err)
	}
	if err := st.MarkTokenInvalid(teamID); err != nil {
		log.Printf("[%s] could not flag expired tokens: %v", name, err)
	}
	log.Printf("[%s] tokens expired, run: always-green reauth", name)
	notify.TokenExpired(name, fromDesktop)
	kick()
}

func startSession(parent context.Context, st *store.Store, sessions *sessionSet, heal *healer, kick func(), ws store.Workspace) {
	log.Printf("starting %s", ws.Name)
	ctx, cancel := context.WithCancel(parent)
	sess := slackx.NewSession(ws.Name, ws.TeamID, ws.UserID, ws.Xoxc, ws.Xoxd)
	sess.OnTokenDead = func(teamID, name string) {
		onTokenDead(st, heal, kick, teamID, name)
	}
	sessions.put(ws.TeamID, &runtime{cancel: cancel, sess: sess})
	go sess.Run(ctx)
}

func stopAll(sessions *sessionSet) {
	for _, rt := range sessions.drain() {
		rt.sess.Stop()
		rt.cancel()
	}
}

func snapshot(running bool, sessions *sessionSet) Status {
	out := Status{Running: running, UpdatedAt: time.Now().UTC().Format(time.RFC3339)}
	for _, rt := range sessions.list() {
		s := rt.sess.Snapshot()
		item := WorkspaceStatus{
			TeamID:     s.TeamID,
			Name:       s.Name,
			Status:     s.Status,
			Connected:  s.Connected,
			TokenValid: s.TokenValid,
			Uptime:     int64(s.Uptime.Seconds()),
			Connects:   s.Connects,
		}
		if !s.LastActive.IsZero() {
			item.LastActive = s.LastActive.UTC().Format(time.RFC3339)
		}
		out.Workspaces = append(out.Workspaces, item)
	}
	return out
}

func writeStatus(running bool, sessions *sessionSet) error {
	data, err := json.MarshalIndent(snapshot(running, sessions), "", "  ")
	if err != nil {
		return err
	}
	tmp := paths.StatusFile() + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, paths.StatusFile())
}
