package slackx

import (
	"sync/atomic"
	"testing"
)

func TestFinishKeepsInvalidTokenState(t *testing.T) {
	s := NewSession("ws", "T1", "U1", "xoxc", "xoxd")
	s.markDead()
	s.finish()
	if got := s.Snapshot().Status; got != "invalid_token" {
		t.Fatalf("finish clobbered the reason the session ended: %q", got)
	}
}

func TestFinishMarksOtherStatesStopped(t *testing.T) {
	s := NewSession("ws", "T1", "U1", "xoxc", "xoxd")
	s.setState("error", false)
	s.finish()
	if got := s.Snapshot().Status; got != "stopped" {
		t.Fatalf("got %q", got)
	}
}

func TestMarkDeadNotifiesOnce(t *testing.T) {
	var calls atomic.Int32
	s := NewSession("ws", "T1", "U1", "xoxc", "xoxd")
	s.OnTokenDead = func(teamID, name string) {
		if teamID != "T1" || name != "ws" {
			t.Errorf("callback got %q %q", teamID, name)
		}
		calls.Add(1)
	}
	s.markDead()
	s.markDead()
	if got := calls.Load(); got != 1 {
		t.Fatalf("expected one notification, got %d", got)
	}
	if s.Snapshot().TokenValid {
		t.Fatal("token should be marked invalid")
	}
}
