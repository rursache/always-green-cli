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

// AppleScript string literals only understand backslash escapes for \ and ",
// and tolerate a raw newline inside the literal - which would let a workspace
// name inject extra visual lines into the notification
func escape(s string) string {
	s = strings.Map(func(r rune) rune {
		if r == '\n' || r == '\r' || r == '\t' {
			return ' '
		}
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, s)
	s = strings.ReplaceAll(s, `\`, `\\`)
	return strings.ReplaceAll(s, `"`, `\"`)
}
