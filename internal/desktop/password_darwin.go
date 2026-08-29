//go:build darwin

package desktop

import (
	"context"
	"os/exec"
	"strings"
	"time"
)

func safeStoragePassword() ([]byte, error) {
	attempts := [][]string{
		{"find-generic-password", "-s", "Slack Safe Storage", "-w"},
		{"find-generic-password", "-s", "Slack Safe Storage", "-a", "Slack App Store Key", "-w"},
		{"find-generic-password", "-s", "Slack Safe Storage", "-a", "Slack Key", "-w"},
		{"find-generic-password", "-s", "Slack Safe Storage", "-a", "Slack", "-w"},
	}
	var last error
	for _, args := range attempts {
		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		out, err := exec.CommandContext(ctx, "security", args...).Output()
		cancel()
		if err != nil {
			last = err
			continue
		}
		pw := strings.TrimSpace(string(out))
		if pw != "" {
			return []byte(pw), nil
		}
	}
	if last != nil {
		return nil, last
	}
	return nil, errDecrypt
}

func cookieRounds() int { return 1003 }
