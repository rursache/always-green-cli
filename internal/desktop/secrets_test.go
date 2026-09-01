package desktop

import (
	"reflect"
	"testing"
)

// Every embedder of an unbranded Chromium labels its item "Chromium Safe
// Storage", only the application attribute tells a Slack item apart
func TestSecretsForApplicationMatchesByAttribute(t *testing.T) {
	raw := `[/org/freedesktop/secrets/collection/login/12]
label = Chrome Safe Storage
secret = chrome-pw
created = 2026-01-01 10:00:00
schema = chrome_libsecret_os_crypt_password_v2
attribute.application = chrome
attribute.xdg:schema = chrome_libsecret_os_crypt_password_v2
[/org/freedesktop/secrets/collection/login/34]
label = Chromium Safe Storage
secret = slack-pw-one
attribute.application = Slack
[/org/freedesktop/secrets/collection/login/35]
label = Chromium Safe Storage
secret = slack-pw-two
attribute.application = com.slack.Slack
[/org/freedesktop/secrets/collection/login/36]
label = Chromium Safe Storage
attribute.application = slack-locked
[/org/freedesktop/secrets/collection/login/37]
label = Chromium Safe Storage
secret = other-pw
attribute.application = signal
`
	got := secretsForApplication(raw, "slack")
	want := []string{"slack-pw-one", "slack-pw-two"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %q, want %q", got, want)
	}
	if got := secretsForApplication("", "slack"); len(got) != 0 {
		t.Fatalf("empty output should yield nothing, got %q", got)
	}
}
