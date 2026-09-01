package daemon

import (
	"errors"
	"testing"
	"time"
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
