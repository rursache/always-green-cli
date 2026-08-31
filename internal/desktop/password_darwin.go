//go:build darwin

package desktop

import (
	"context"
	"os/exec"
	"strings"
	"time"
)

// keychainTimeout is per lookup. Several accounts may exist (a direct download
// and a Mac App Store install keep separate items), and a blocked prompt would
// otherwise stall the daemon for minutes.
const keychainTimeout = 15 * time.Second

// safeStoragePasswords returns every candidate rather than the first non-empty
// one: with two Slack installs the first hit can belong to the other profile,
// and the caller can only tell by trying to decrypt with it.
func safeStoragePasswords() ([][]byte, error) {
	attempts := [][]string{
		{"find-generic-password", "-s", "Slack Safe Storage", "-w"},
		{"find-generic-password", "-s", "Slack Safe Storage", "-a", "Slack App Store Key", "-w"},
		{"find-generic-password", "-s", "Slack Safe Storage", "-a", "Slack Key", "-w"},
		{"find-generic-password", "-s", "Slack Safe Storage", "-a", "Slack", "-w"},
	}
	var out [][]byte
	seen := map[string]struct{}{}
	var first error
	for _, args := range attempts {
		ctx, cancel := context.WithTimeout(context.Background(), keychainTimeout)
		raw, err := exec.CommandContext(ctx, "security", args...).Output()
		cancel()
		if err != nil {
			if first == nil {
				first = err
			}
			continue
		}
		pw := strings.TrimSpace(string(raw))
		if pw == "" {
			continue
		}
		if _, dup := seen[pw]; dup {
			continue
		}
		seen[pw] = struct{}{}
		out = append(out, []byte(pw))
	}
	if len(out) == 0 {
		if first != nil {
			return nil, first
		}
		return nil, errDecrypt
	}
	return out, nil
}

func cookieRounds() int { return 1003 }
