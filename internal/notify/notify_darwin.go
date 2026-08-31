//go:build darwin

package notify

import (
	"context"
	"os/exec"
	"strings"
	"time"
)

func send(title, body string) {
	script := `display notification "` + escape(body) + `" with title "` + escape(title) + `"`
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = exec.CommandContext(ctx, "osascript", "-e", script).Run()
}

// AppleScript string literals only understand backslash escapes for \ and "
func escape(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	return strings.ReplaceAll(s, `"`, `\"`)
}
