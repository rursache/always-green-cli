package slackx

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestWorkspaceDomain(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want string
	}{
		{"https://acme.slack.com/", "acme"},
		{"https://acme.slack.com", "acme"},
		{"acme.slack.com", "acme"},
		{"https://my-team-1.slack.com/", "my-team-1"},
		{"", ""},
		{"https://slack.com/", ""},
		{"https://evil.com/", ""},
		{"https://acme.slack.com.evil.com/", ""},
		{"https://a.b.slack.com/", ""},
	} {
		if got := WorkspaceDomain(tc.in); got != tc.want {
			t.Errorf("WorkspaceDomain(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// point the boot endpoint at a local server, keeping the real path
func withBootServer(t *testing.T, h http.HandlerFunc) {
	t.Helper()
	srv := httptest.NewServer(h)
	old := bootURL
	bootURL = func(string) string { return srv.URL + "/ssb/redirect" }
	t.Cleanup(func() {
		bootURL = old
		srv.Close()
	})
}

func TestRefreshTokenExtractsApiToken(t *testing.T) {
	want := "xoxc-1111111111111-2222222222222-3333333333333-" + strings.Repeat("a", 64)
	withBootServer(t, func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.Header.Get("Cookie"), "d=xoxd-mycookie") {
			t.Errorf("d cookie not sent, got %q", r.Header.Get("Cookie"))
		}
		if !strings.Contains(r.Header.Get("Cookie"), "d-s=") {
			t.Errorf("d-s companion cookie missing, got %q", r.Header.Get("Cookie"))
		}
		if r.URL.Path != "/ssb/redirect" {
			t.Errorf("path = %q", r.URL.Path)
		}
		fmt.Fprintf(w, `<html><script>var boot_data = {"team_id":"T1","api_token":"%s","user_id":"U1"};</script></html>`, want)
	})
	got, err := RefreshToken("acme", "xoxd-mycookie")
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("got %q", got)
	}
}

// A signed-out session is served the login page, which carries no token: that
// is the only case where the user has to do something
func TestRefreshTokenReportsDeadCookie(t *testing.T) {
	withBootServer(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<html><body>Sign in to Slack</body></html>`)
	})
	_, err := RefreshToken("acme", "xoxd-stale")
	if !errors.Is(err, ErrCookieDead) {
		t.Fatalf("expected ErrCookieDead, got %v", err)
	}
}

func TestRefreshTokenSurfacesRateLimit(t *testing.T) {
	withBootServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "17")
		w.WriteHeader(http.StatusTooManyRequests)
	})
	_, err := RefreshToken("acme", "xoxd-x")
	var rl RateLimited
	if !errors.As(err, &rl) {
		t.Fatalf("expected RateLimited, got %T %v", err, err)
	}
	// a rate limit must not be mistaken for a dead cookie
	if errors.Is(err, ErrCookieDead) {
		t.Fatal("a rate limit must not read as a dead cookie")
	}
}

func TestRefreshTokenSurfacesServerError(t *testing.T) {
	withBootServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		fmt.Fprint(w, "<html>502</html>")
	})
	_, err := RefreshToken("acme", "xoxd-x")
	var he HTTPError
	if !errors.As(err, &he) || he.Status != http.StatusBadGateway {
		t.Fatalf("expected a 502 HTTPError, got %T %v", err, err)
	}
	if errors.Is(err, ErrCookieDead) {
		t.Fatal("a gateway error must not read as a dead cookie")
	}
}

func TestRefreshTokenNeedsDomain(t *testing.T) {
	if _, err := RefreshToken("", "xoxd-x"); err == nil {
		t.Fatal("a missing domain must be an error, not a request to slack.com")
	}
}
