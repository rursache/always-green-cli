package daemon

import (
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
