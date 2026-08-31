package slackx

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func withAPI(t *testing.T, h http.HandlerFunc) {
	t.Helper()
	srv := httptest.NewServer(h)
	old := apiBase
	apiBase = srv.URL
	t.Cleanup(func() {
		apiBase = old
		srv.Close()
	})
}

// A 429 must never look like a dead token, or a rate limit would permanently
// flag a healthy workspace as needing re-auth
func TestRateLimitIsNotTokenDeath(t *testing.T) {
	withAPI(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "42")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"ok":false,"error":"ratelimited"}`))
	})
	_, err := AuthTest("xoxc", "xoxd")
	if err == nil {
		t.Fatal("expected an error")
	}
	var rl RateLimited
	if !errors.As(err, &rl) {
		t.Fatalf("expected RateLimited, got %T: %v", err, err)
	}
	if rl.RetryAfter != 42*time.Second {
		t.Fatalf("Retry-After not honoured: %v", rl.RetryAfter)
	}
	var api APIError
	if errors.As(err, &api) && TokenDead(api.Code) {
		t.Fatal("a rate limit must not classify as a dead token")
	}
}

func TestRateLimitDefaultsWhenRetryAfterMissing(t *testing.T) {
	withAPI(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	})
	_, err := AuthTest("xoxc", "xoxd")
	var rl RateLimited
	if !errors.As(err, &rl) || rl.RetryAfter != 30*time.Second {
		t.Fatalf("expected a default retry window, got %v (%T)", err, err)
	}
}

// A captive portal or proxy returning HTML must surface as a transport
// problem, not as an unparseable-JSON mystery
func TestNonJSONErrorPageReportsStatus(t *testing.T) {
	withAPI(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte("<html><body>502 Bad Gateway</body></html>"))
	})
	_, err := AuthTest("xoxc", "xoxd")
	var he HTTPError
	if !errors.As(err, &he) {
		t.Fatalf("expected HTTPError, got %T: %v", err, err)
	}
	if he.Status != http.StatusBadGateway {
		t.Fatalf("got status %d", he.Status)
	}
	var api APIError
	if errors.As(err, &api) && TokenDead(api.Code) {
		t.Fatal("a gateway error must not classify as a dead token")
	}
}

// Slack signals real API failures as HTTP 200 with ok:false
func TestSlackErrorBodyStillParsed(t *testing.T) {
	withAPI(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"ok":false,"error":"invalid_auth"}`))
	})
	_, err := AuthTest("xoxc", "xoxd")
	var api APIError
	if !errors.As(err, &api) {
		t.Fatalf("expected APIError, got %T: %v", err, err)
	}
	if !TokenDead(api.Code) {
		t.Fatalf("invalid_auth should be a dead token, got %q", api.Code)
	}
}

func TestRetryAfterParsing(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want time.Duration
	}{
		{"10", 10 * time.Second},
		{" 5 ", 5 * time.Second},
		{"", 30 * time.Second},
		{"garbage", 30 * time.Second},
		{"0", 30 * time.Second},
		{"-3", 30 * time.Second},
	} {
		if got := retryAfter(tc.in); got != tc.want {
			t.Errorf("retryAfter(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}
