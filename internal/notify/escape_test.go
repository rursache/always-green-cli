//go:build darwin

package notify

import (
	"strings"
	"testing"
)

// A workspace name comes from Slack, so it is untrusted input pasted into an
// AppleScript string literal
func TestEscapeKeepsPayloadInsideTheStringLiteral(t *testing.T) {
	for _, name := range []string{
		`"; do shell script "touch /tmp/pwned"; "`,
		`ends with a backslash \`,
		`\" already escaped`,
		`\\"`,
		strings.Repeat(`\`, 7) + `"`,
	} {
		got := escape(name)
		if unbalanced(got) {
			t.Errorf("escape(%q) = %q leaves the literal open", name, got)
		}
	}
}

// every quote must be preceded by an odd number of backslashes, i.e. escaped
func unbalanced(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] != '"' {
			continue
		}
		n := 0
		for j := i - 1; j >= 0 && s[j] == '\\'; j-- {
			n++
		}
		if n%2 == 0 {
			return true
		}
	}
	return false
}

// AppleScript tolerates a raw newline inside a literal, so control characters
// let a name inject extra visual lines into the notification
func TestEscapeStripsControlCharacters(t *testing.T) {
	got := escape("acme\nSecurity Alert\r\x00\x07 corp")
	if strings.ContainsAny(got, "\n\r\x00\x07") {
		t.Fatalf("control characters survived: %q", got)
	}
	if !strings.Contains(got, "acme") || !strings.Contains(got, "corp") {
		t.Fatalf("visible text was lost: %q", got)
	}
}

func TestEscapeLeavesOrdinaryNamesAlone(t *testing.T) {
	if got := escape("Acme Corp"); got != "Acme Corp" {
		t.Fatalf("got %q", got)
	}
}
