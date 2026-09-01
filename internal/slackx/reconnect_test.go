package slackx

import (
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// fakeSlack stands in for the websocket endpoint and the REST API, the REST
// side always reports the user as auto-away, which is what drives the
// presence-reset reconnect
type fakeSlack struct {
	ws      *httptest.Server
	api     *httptest.Server
	dials   atomic.Int32
	authHit atomic.Int32
	refused atomic.Int32
	reject  atomic.Bool

	mu    sync.Mutex
	conns []*websocket.Conn
}

// httptest cannot close a hijacked websocket, so track them here
func (f *fakeSlack) closeAll() {
	f.mu.Lock()
	conns := append([]*websocket.Conn(nil), f.conns...)
	f.conns = nil
	f.mu.Unlock()
	for _, c := range conns {
		_ = c.Close()
	}
}

func newFakeSlack(t *testing.T) *fakeSlack {
	t.Helper()
	f := &fakeSlack{}
	up := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	f.ws = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if f.reject.Load() {
			f.refused.Add(1)
			http.Error(w, "unavailable", http.StatusServiceUnavailable)
			return
		}
		c, err := up.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		f.dials.Add(1)
		f.mu.Lock()
		f.conns = append(f.conns, c)
		f.mu.Unlock()
		go func() {
			defer c.Close()
			for {
				if _, _, err := c.ReadMessage(); err != nil {
					return
				}
			}
		}()
	}))
	f.api = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.Contains(r.URL.Path, "auth.test"):
			f.authHit.Add(1)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"ok": true, "user_id": "U1", "team_id": "T1", "team": "test",
			})
		case strings.Contains(r.URL.Path, "users.getPresence"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"ok": true, "presence": "away", "auto_away": true,
			})
		default:
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
		}
	}))
	t.Cleanup(func() {
		f.ws.Close()
		f.api.Close()
	})
	return f
}

// point the package at the fakes and shrink the timers for the duration of a test
func useFakeSlack(t *testing.T, f *fakeSlack, presence time.Duration) {
	t.Helper()
	oldWS, oldAPI := wsURL, apiBase
	oldPing, oldTickle, oldPres, oldBackoff := pingEvery, tickleEvery, presenceEvery, baseBackoff
	wsURL = "ws" + strings.TrimPrefix(f.ws.URL, "http")
	apiBase = f.api.URL
	pingEvery, tickleEvery, presenceEvery, baseBackoff = time.Hour, time.Hour, presence, 5*time.Millisecond
	log.SetOutput(io.Discard)
	t.Cleanup(func() {
		wsURL, apiBase = oldWS, oldAPI
		pingEvery, tickleEvery, presenceEvery, baseBackoff = oldPing, oldTickle, oldPres, oldBackoff
		log.SetOutput(io.Discard)
	})
}

// A presence-triggered reconnect closes the old connection, whose reader then
// reports the close; if that report is not scoped to the connection it came
// from, the loop mistakes it for the new connection dying and reconnects
// again, forever, hammering Slack with no backoff
func TestAwayReconnectDoesNotStorm(t *testing.T) {
	f := newFakeSlack(t)
	useFakeSlack(t, f, 20*time.Millisecond)

	s := NewSession("test", "T1", "U1", "xoxc-test", "xoxd-test")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() {
		s.Run(ctx)
		close(done)
	}()

	time.Sleep(400 * time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return after cancel")
	}

	// ~20 presence ticks in 400ms, and shouldReconnect fires on the first 5
	// then every 5th, so a correct run dials well under 20 times
	if got := f.dials.Load(); got > 25 {
		t.Fatalf("reconnect storm: %d dials in 400ms", got)
	}
	if got := f.dials.Load(); got < 2 {
		t.Fatalf("expected the away handler to reconnect at least once, got %d dials", got)
	}
	// every spurious teardown also runs auth.test, so this rate-limits Slack too
	if got := f.authHit.Load(); got > 10 {
		t.Fatalf("auth.test called %d times, expected only genuine disconnects", got)
	}
}

// A server-side close must be recovered from, exactly once per close
func TestServerCloseReconnectsOnce(t *testing.T) {
	f := newFakeSlack(t)
	useFakeSlack(t, f, time.Hour) // no presence-driven reconnects

	s := NewSession("test", "T1", "U1", "xoxc-test", "xoxd-test")
	runSession(t, s)

	waitFor(t, func() bool { return f.dials.Load() == 1 }, "first connection")
	f.closeAll()
	waitFor(t, func() bool { return f.dials.Load() == 2 }, "reconnect after server close")

	time.Sleep(150 * time.Millisecond)
	if got := f.dials.Load(); got != 2 {
		t.Fatalf("one close should cause one reconnect, got %d dials", got)
	}
}

// A presence-triggered reconnect tears down the live connection before it
// dials again, so a dial failure there must be retried like any other drop
// rather than leaving the session reporting connected with no socket
func TestAwayReconnectDialFailureRecovers(t *testing.T) {
	f := newFakeSlack(t)
	useFakeSlack(t, f, 20*time.Millisecond)

	s := NewSession("test", "T1", "U1", "xoxc-test", "xoxd-test")
	runSession(t, s)

	waitFor(t, func() bool { return f.dials.Load() == 1 }, "first connection")
	f.reject.Store(true)
	waitFor(t, func() bool { return f.refused.Load() >= 2 }, "retried dials while the server is down")
	f.reject.Store(false)
	waitFor(t, func() bool { return f.dials.Load() >= 2 }, "reconnect once the server is back")

	snap := s.Snapshot()
	if !snap.Connected || snap.Status == "error" || snap.Status == "stopped" {
		t.Fatalf("session should be live again, got %+v", snap)
	}
	if !s.Running() {
		t.Fatal("session should still be running")
	}
}

// When a presence-triggered reconnect exhausts its retries the session must
// end in the error state so the daemon restarts it, not linger as connected
func TestAwayReconnectExhaustedEndsInError(t *testing.T) {
	f := newFakeSlack(t)
	useFakeSlack(t, f, 20*time.Millisecond)

	s := NewSession("test", "T1", "U1", "xoxc-test", "xoxd-test")
	_, done := runSession(t, s)

	waitFor(t, func() bool { return f.dials.Load() == 1 }, "first connection")
	f.reject.Store(true)
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not give up while the server stayed down")
	}
	if got := f.refused.Load(); got < maxConnectTry {
		t.Fatalf("expected %d dial attempts before giving up, got %d", maxConnectTry, got)
	}
	if snap := s.Snapshot(); snap.Connected || snap.Status != "stopped" {
		t.Fatalf("expected a stopped, disconnected session, got %+v", snap)
	}
}

// Run must not leave a reader goroutine blocked handing over an event
func TestRunReleasesReaderOnCancel(t *testing.T) {
	f := newFakeSlack(t)
	useFakeSlack(t, f, 20*time.Millisecond)

	s := NewSession("test", "T1", "U1", "xoxc-test", "xoxd-test")
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		s.Run(ctx)
		close(done)
	}()
	waitFor(t, func() bool { return f.dials.Load() >= 1 }, "connection")
	cancel()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("Run leaked: did not return within 3s of cancel")
	}
}

// runSession starts Run in the background and makes sure it has fully
// returned before the test's cleanup restores the package-level fakes
func runSession(t *testing.T, s *Session) (cancel func(), done <-chan struct{}) {
	t.Helper()
	ctx, cancelCtx := context.WithCancel(context.Background())
	ch := make(chan struct{})
	go func() {
		s.Run(ctx)
		close(ch)
	}()
	t.Cleanup(func() {
		cancelCtx()
		select {
		case <-ch:
		case <-time.After(5 * time.Second):
			t.Error("Run did not return after cancel")
		}
	})
	return cancelCtx, ch
}

func waitFor(t *testing.T, cond func() bool, what string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}
