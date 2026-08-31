package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/rursache/always-green/internal/importws"
	"github.com/rursache/always-green/internal/ipc"
	"github.com/rursache/always-green/internal/notify"
	"github.com/rursache/always-green/internal/paths"
	"github.com/rursache/always-green/internal/schedule"
	"github.com/rursache/always-green/internal/slackx"
	"github.com/rursache/always-green/internal/store"
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

// desktopRetryEvery bounds how often a self-healing workspace re-reads the
// Slack app, since on macOS that can mean a Keychain round trip
const desktopRetryEvery = 30 * time.Minute

// healer tracks in-flight and recent desktop refresh attempts. Attempts run off
// the main loop because reading the Keychain can block for a long time.
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

func RunForeground() error {
	if err := paths.EnsureDir(); err != nil {
		return err
	}
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

	sessions := map[string]*runtime{}
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
	for i := 0; i < 20; i++ {
		time.Sleep(150 * time.Millisecond)
		if Running() {
			return nil
		}
	}
	return fmt.Errorf("daemon did not start, see %s", paths.DaemonLog())
}

func Stop() error {
	if !Running() {
		return nil
	}
	if _, err := ipc.Send("stop"); err == nil {
		for i := 0; i < 20; i++ {
			time.Sleep(150 * time.Millisecond)
			if !Running() {
				return nil
			}
		}
	}
	pid, ok := pidIfAlive()
	if !ok {
		return nil
	}
	_ = syscall.Kill(pid, syscall.SIGTERM)
	for i := 0; i < 10; i++ {
		time.Sleep(150 * time.Millisecond)
		if !Running() {
			return nil
		}
	}
	_ = syscall.Kill(pid, syscall.SIGKILL)
	time.Sleep(200 * time.Millisecond)
	return nil
}

func Reload() error {
	_, err := ipc.Send("reload")
	return err
}

func Running() bool {
	_, ok := pidIfAlive()
	return ok
}

func PID() int {
	pid, _ := pidIfAlive()
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

func pidIfAlive() (int, bool) {
	raw, err := os.ReadFile(paths.DaemonPID())
	if err != nil {
		return 0, false
	}
	pid, err := strconv.Atoi(string(raw))
	if err != nil || pid <= 0 {
		return 0, false
	}
	if err := syscall.Kill(pid, 0); err != nil {
		_ = os.Remove(paths.DaemonPID())
		return 0, false
	}
	return pid, true
}

func syncSessions(ctx context.Context, st *store.Store, sessions map[string]*runtime, heal *healer, kick func()) {
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
		if ws.TokenInvalid && ws.Source == store.SourceDesktop {
			tryDesktopHeal(st, heal, kick, ws, now)
		}
		if ws.Eligible(now, tz) {
			want[ws.TeamID] = ws
		}
	}

	for id, rt := range sessions {
		ws, ok := want[id]
		if !ok {
			log.Printf("stopping %s", rt.sess.Name)
			rt.cancel()
			delete(sessions, id)
			continue
		}
		if rt.sess.Xoxc != ws.Xoxc || rt.sess.Xoxd != ws.Xoxd {
			log.Printf("tokens changed for %s", ws.Name)
			rt.cancel()
			delete(sessions, id)
			continue
		}
		snap := rt.sess.Snapshot()
		if snap.Status == "invalid_token" {
			continue
		}
		if !rt.sess.Running() || snap.Status == "error" {
			log.Printf("restarting %s", ws.Name)
			rt.cancel()
			delete(sessions, id)
		}
	}

	for id, ws := range want {
		if _, ok := sessions[id]; ok {
			continue
		}
		startSession(ctx, st, sessions, heal, kick, ws)
	}
}

func tryDesktopHeal(st *store.Store, heal *healer, kick func(), ws store.Workspace, now time.Time) {
	if !heal.claim(ws.TeamID, now) {
		return
	}
	name, teamID := ws.Name, ws.TeamID
	go func() {
		defer heal.release(teamID)
		if err := importws.RefreshDesktop(st, teamID); err != nil {
			log.Printf("[%s] desktop refresh failed: %v", name, err)
			return
		}
		log.Printf("[%s] refreshed tokens from the Slack app", name)
		kick()
	}()
}

// onTokenDead is called from a session goroutine once Slack rejects the tokens
func onTokenDead(st *store.Store, heal *healer, kick func(), teamID, name string) {
	// share the throttle with the periodic retry so tokens that keep passing
	// auth.test but keep failing the websocket cannot spin the Slack app
	ws, ok := st.Workspace(teamID)
	fromDesktop := ok && ws.Source == store.SourceDesktop
	if fromDesktop && heal.claim(teamID, time.Now()) {
		err := importws.RefreshDesktop(st, teamID)
		heal.release(teamID)
		if err == nil {
			log.Printf("[%s] tokens expired, refreshed from the Slack app", name)
			kick()
			return
		}
		log.Printf("[%s] desktop refresh failed: %v", name, err)
	}
	if err := st.MarkTokenInvalid(teamID); err != nil {
		log.Printf("[%s] could not flag expired tokens: %v", name, err)
	}
	log.Printf("[%s] tokens expired, run: always-green reauth", name)
	notify.TokenExpired(name, fromDesktop)
	kick()
}

func startSession(parent context.Context, st *store.Store, sessions map[string]*runtime, heal *healer, kick func(), ws store.Workspace) {
	log.Printf("starting %s", ws.Name)
	ctx, cancel := context.WithCancel(parent)
	sess := slackx.NewSession(ws.Name, ws.TeamID, ws.UserID, ws.Xoxc, ws.Xoxd)
	sess.OnTokenDead = func(teamID, name string) {
		onTokenDead(st, heal, kick, teamID, name)
	}
	sessions[ws.TeamID] = &runtime{cancel: cancel, sess: sess}
	go sess.Run(ctx)
}

func stopAll(sessions map[string]*runtime) {
	for id, rt := range sessions {
		rt.sess.Stop()
		rt.cancel()
		delete(sessions, id)
	}
}

func snapshot(running bool, sessions map[string]*runtime) Status {
	out := Status{Running: running, UpdatedAt: time.Now().UTC().Format(time.RFC3339)}
	for _, rt := range sessions {
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

func writeStatus(running bool, sessions map[string]*runtime) error {
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
