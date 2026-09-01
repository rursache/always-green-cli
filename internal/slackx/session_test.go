package slackx

import (
	"errors"
	"strings"
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

// The daemon reads Running to tell a death that is still being handled from
// one it may react to, so the flag must only drop once the callback is done
func TestMarkDeadKeepsRunningUntilCallbackReturns(t *testing.T) {
	s := NewSession("ws", "T1", "U1", "xoxc", "xoxd")
	s.running.Store(true)
	var duringCallback bool
	s.OnTokenDead = func(string, string) {
		duringCallback = s.Running()
		if got := s.Snapshot().Status; got != "invalid_token" {
			t.Errorf("status inside the callback is %q", got)
		}
	}
	s.markDead()
	if !duringCallback {
		t.Fatal("session must still report running while the callback handles the death")
	}
	if s.Running() {
		t.Fatal("session must stop running once the callback returns")
	}
}

func TestIsAuthFailUsesStatusNotErrorText(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		want bool
	}{
		{"401 handshake", &handshakeError{Status: 401, err: errors.New("bad handshake")}, true},
		{"403 handshake", &handshakeError{Status: 403, err: errors.New("bad handshake")}, true},
		{"429 handshake", &handshakeError{Status: 429, err: errors.New("bad handshake")}, false},
		{"400 handshake", &handshakeError{Status: 400, err: errors.New("bad handshake")}, false},
		{"transport error", errors.New("dial tcp: connection refused"), false},
		// a proxy or captive portal can put " 403" in an error that has
		// nothing to do with our tokens; that must not kill the workspace
		{"unrelated text containing 403", errors.New("proxy returned 403 for an unrelated probe"), false},
		{"unrelated text containing 401", errors.New("upstream 401 while fetching pac file"), false},
		{"nil", nil, false},
	} {
		if got := isAuthFail(tc.err); got != tc.want {
			t.Errorf("%s: isAuthFail = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestHandshakeErrorUnwraps(t *testing.T) {
	inner := errors.New("bad handshake")
	err := error(&handshakeError{Status: 401, err: inner})
	if !errors.Is(err, inner) {
		t.Fatal("handshakeError must unwrap to the dial error")
	}
	if !strings.Contains(err.Error(), "401") {
		t.Fatalf("status should appear in the message, got %q", err.Error())
	}
}
