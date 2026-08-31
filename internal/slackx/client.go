package slackx

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// apiBase is a var so tests can point the client at a local server
var apiBase = "https://slack.com/api"

const userAgent = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"

var tokenDead = map[string]struct{}{
	"invalid_auth": {}, "not_authed": {}, "token_expired": {},
	"token_revoked": {}, "account_inactive": {},
}

type Auth struct {
	OK     bool   `json:"ok"`
	User   string `json:"user"`
	UserID string `json:"user_id"`
	Team   string `json:"team"`
	TeamID string `json:"team_id"`
	Error  string `json:"error"`
}

type Presence struct {
	OK              bool   `json:"ok"`
	Presence        string `json:"presence"`
	ConnectionCount int    `json:"connection_count"`
	LastActivity    int64  `json:"last_activity"`
	AutoAway        bool   `json:"auto_away"`
	ManualAway      bool   `json:"manual_away"`
	Error           string `json:"error"`
}

type UserProfile struct {
	Name        string
	RealName    string
	DisplayName string
	Email       string
}

type APIError struct {
	Code string
}

func (e APIError) Error() string {
	return e.Code
}

func TokenDead(code string) bool {
	_, ok := tokenDead[code]
	return ok
}

func DecodeTokenBlob(raw string) (xoxc, xoxd string, err error) {
	raw = strings.TrimSpace(raw)
	data, err := base64.StdEncoding.DecodeString(raw)
	if err != nil {
		data, err = base64.RawStdEncoding.DecodeString(raw)
		if err != nil {
			return "", "", fmt.Errorf("not a base64 token blob")
		}
	}
	var payload struct {
		Xoxc string `json:"xoxc"`
		Xoxd string `json:"xoxd"`
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		return "", "", fmt.Errorf("blob is not JSON")
	}
	if payload.Xoxc == "" || payload.Xoxd == "" {
		return "", "", fmt.Errorf("blob is missing xoxc or xoxd")
	}
	return payload.Xoxc, payload.Xoxd, nil
}

func headers(xoxc, xoxd string) http.Header {
	h := make(http.Header)
	h.Set("Cookie", "d="+xoxd)
	h.Set("Authorization", "Bearer "+xoxc)
	h.Set("User-Agent", userAgent)
	h.Set("Accept", "application/json, text/plain, */*")
	h.Set("Origin", "https://app.slack.com")
	h.Set("Referer", "https://app.slack.com/")
	return h
}

func AuthTest(xoxc, xoxd string) (Auth, error) {
	var out Auth
	if err := do("POST", apiBase+"/auth.test", xoxc, xoxd, nil, &out); err != nil {
		return Auth{}, err
	}
	if !out.OK {
		return Auth{}, APIError{Code: orUnknown(out.Error)}
	}
	return out, nil
}

func GetPresence(xoxc, xoxd, userID string) (Presence, error) {
	q := url.Values{}
	if userID != "" {
		q.Set("user", userID)
	}
	path := apiBase + "/users.getPresence"
	if encoded := q.Encode(); encoded != "" {
		path += "?" + encoded
	}
	var out Presence
	if err := do("GET", path, xoxc, xoxd, nil, &out); err != nil {
		return Presence{}, err
	}
	if !out.OK {
		return Presence{}, APIError{Code: orUnknown(out.Error)}
	}
	return out, nil
}

func GetUser(xoxc, xoxd, userID string) (UserProfile, error) {
	if userID == "" {
		return UserProfile{}, nil
	}
	path := apiBase + "/users.info?user=" + url.QueryEscape(userID)
	var wrap struct {
		OK    bool   `json:"ok"`
		Error string `json:"error"`
		User  struct {
			Name    string `json:"name"`
			Profile struct {
				RealName    string `json:"real_name"`
				DisplayName string `json:"display_name"`
				Email       string `json:"email"`
			} `json:"profile"`
			RealName string `json:"real_name"`
		} `json:"user"`
	}
	if err := do("GET", path, xoxc, xoxd, nil, &wrap); err != nil {
		return UserProfile{}, err
	}
	if !wrap.OK {
		return UserProfile{}, nil
	}
	real := wrap.User.RealName
	if real == "" {
		real = wrap.User.Profile.RealName
	}
	return UserProfile{
		Name:        wrap.User.Name,
		RealName:    real,
		DisplayName: wrap.User.Profile.DisplayName,
		Email:       wrap.User.Profile.Email,
	}, nil
}

func do(method, rawURL, xoxc, xoxd string, body io.Reader, dest any) error {
	req, err := http.NewRequest(method, rawURL, body)
	if err != nil {
		return err
	}
	req.Header = headers(xoxc, xoxd)
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, dest)
}

func orUnknown(s string) string {
	if s == "" {
		return "unknown_error"
	}
	return s
}
