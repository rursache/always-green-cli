package slackx

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

// Slack's web client mints a fresh xoxc from the d cookie every time it boots;
// the d cookie is the durable session, valid for months, and the xoxc is a
// short-lived derivative that rotates in days; loading the client's boot page
// with the cookie yields a new token, so an expired xoxc alone never needs the
// user to paste anything again
//
// The bare workspace URL is not usable here: it answers 200 without the boot
// data unless the caller replays the browser's redirect chain, so the redirect
// endpoint is requested directly
var (
	apiTokenRe   = regexp.MustCompile(`"api_token":"(xoxc-[^"]+)"`)
	refreshLimit = int64(8 << 20)

	// bootURL is a var so tests can point it at a local server
	bootURL = func(domain string) string {
		return "https://" + domain + ".slack.com/ssb/redirect"
	}
)

const bootDomain = "slack.com"

// ErrCookieDead means the d cookie itself is no longer a valid session, so
// there is nothing left to refresh from and the user has to sign in again
var ErrCookieDead = fmt.Errorf("the Slack d cookie is no longer valid")

// WorkspaceDomain pulls the subdomain out of an auth.test url field, e.g.
// "https://acme.slack.com/" -> "acme"
func WorkspaceDomain(rawURL string) string {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return ""
	}
	if !strings.Contains(rawURL, "//") {
		rawURL = "https://" + rawURL
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	host := u.Hostname()
	if !strings.HasSuffix(host, "."+bootDomain) {
		return ""
	}
	sub := strings.TrimSuffix(host, "."+bootDomain)
	if sub == "" || strings.Contains(sub, ".") {
		return ""
	}
	return sub
}

// RefreshToken mints a new xoxc for domain using only the d cookie; it returns
// ErrCookieDead when Slack serves a page with no token, which is what a signed
// out session looks like
func RefreshToken(domain, xoxd string) (string, error) {
	if domain == "" {
		return "", fmt.Errorf("no workspace domain recorded for this workspace")
	}
	req, err := http.NewRequest(http.MethodGet, bootURL(domain), nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Cookie", cookieHeader(xoxd))
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusTooManyRequests {
		return "", RateLimited{RetryAfter: retryAfter(resp.Header.Get("Retry-After"))}
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, refreshLimit))
	if err != nil {
		return "", err
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return "", HTTPError{Status: resp.StatusCode, Body: snippet(body)}
	}
	m := apiTokenRe.FindSubmatch(body)
	if m == nil {
		// a dead session is served the sign-in page, which carries no token
		return "", ErrCookieDead
	}
	return string(m[1]), nil
}

// cookieHeader pairs d with the d-s companion Slack's client expects, d-s is
// just a timestamp the browser sets locally, so it can be synthesised
func cookieHeader(xoxd string) string {
	return fmt.Sprintf("d=%s; d-s=%d", xoxd, time.Now().Unix()-10)
}
