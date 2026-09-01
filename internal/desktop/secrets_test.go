package desktop

import (
	"reflect"
	"testing"
)

func TestSecretsLabelledPicksMatchingItems(t *testing.T) {
	raw := `[/org/freedesktop/secrets/collection/login/12]
label = Chrome Safe Storage
secret = chrome-pw
created = 2026-01-01 10:00:00
schema = chrome_libsecret_os_crypt_password_v2
attribute.application = chrome
attribute.xdg:schema = chrome_libsecret_os_crypt_password_v2
[/org/freedesktop/secrets/collection/login/34]
label = Slack Safe Storage
secret = slack-pw-one
attribute.application = Slack
[/org/freedesktop/secrets/collection/login/35]
label = Slack Safe Storage
secret = slack-pw-two
attribute.application = slack-flatpak
[/org/freedesktop/secrets/collection/login/36]
label = Slack Safe Storage
attribute.application = locked-item
`
	got := secretsLabelled(raw, "Slack Safe Storage")
	want := []string{"slack-pw-one", "slack-pw-two"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %q, want %q", got, want)
	}
	if got := secretsLabelled("", "Slack Safe Storage"); len(got) != 0 {
		t.Fatalf("empty output should yield nothing, got %q", got)
	}
}
