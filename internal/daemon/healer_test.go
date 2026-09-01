package daemon

import (
	"errors"
	"testing"
	"time"

	"github.com/rursache/always-green-cli/internal/slackx"
)

func TestHealerThrottlesRetries(t *testing.T) {
	h := newHealer()
	now := time.Now()
	if !h.claim("T1", now) {
		t.Fatal("first attempt should be allowed")
	}
	if h.claim("T1", now) {
		t.Fatal("must not run two refreshes at once")
	}
	h.release("T1")
	if h.claim("T1", now.Add(time.Minute)) {
		t.Fatal("a retry inside the throttle window should be skipped")
	}
	if !h.claim("T1", now.Add(desktopRetryEvery+time.Second)) {
		t.Fatal("a retry past the throttle window should be allowed")
	}
}

// A refresh that outlives the wait must still report its result, and only
// then, so the caller can keep its heal claim for as long as the refresh runs
func TestRefreshWithinReportsLateResult(t *testing.T) {
	release := make(chan struct{})
	got := make(chan error, 1)
	err := refreshWithin(10*time.Millisecond, func() error {
		<-release
		return nil
	}, func(err error) { got <- err })
	if err == nil {
		t.Fatal("expected a timeout")
	}
	select {
	case <-got:
		t.Fatal("done ran before the refresh finished")
	case <-time.After(20 * time.Millisecond):
	}
	close(release)
	select {
	case err := <-got:
		if err != nil {
			t.Fatalf("done got %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("done never ran after the late refresh finished")
	}
}

func TestRefreshWithinReportsTimelyResult(t *testing.T) {
	want := errors.New("nope")
	got := make(chan error, 1)
	err := refreshWithin(time.Second, func() error { return want }, func(err error) { got <- err })
	if !errors.Is(err, want) {
		t.Fatalf("got %v", err)
	}
	select {
	case err := <-got:
		if !errors.Is(err, want) {
			t.Fatalf("done got %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("done never ran")
	}
}

func TestHealerSettlingCoversClaimAndHandling(t *testing.T) {
	h := newHealer()
	if h.isSettling("T1") {
		t.Fatal("nothing is in flight yet")
	}
	done := h.settling("T1")
	if !h.isSettling("T1") {
		t.Fatal("handling a token death must count as settling")
	}
	h.claim("T1", time.Now())
	done()
	if !h.isSettling("T1") {
		t.Fatal("a held heal claim must keep the workspace settling")
	}
	h.release("T1")
	if h.isSettling("T1") {
		t.Fatal("released and handled, nothing should be settling")
	}
}

// A session that reported dead tokens must be restarted once its death has
// been handled and the store still wants it, since that means the refresh
// produced the same tokens and nothing else will bring the workspace back
func TestNeedsRestart(t *testing.T) {
	for _, tc := range []struct {
		name     string
		status   string
		running  bool
		settling bool
		want     bool
	}{
		{"healthy", "active", true, false, false},
		{"errored", "error", true, false, true},
		{"exited", "stopped", false, false, true},
		{"dead tokens, heal in flight", "invalid_token", false, true, false},
		{"dead tokens, healed to same values", "invalid_token", false, false, true},
	} {
		got := needsRestart(slackx.Snapshot{Status: tc.status}, tc.running, tc.settling)
		if got != tc.want {
			t.Errorf("%s: needsRestart = %v, want %v", tc.name, got, tc.want)
		}
	}
}
