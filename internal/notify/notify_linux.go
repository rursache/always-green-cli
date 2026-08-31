//go:build linux

package notify

import (
	"context"
	"os/exec"
	"time"
)

func send(title, body string) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = exec.CommandContext(ctx, "notify-send", title, body).Run()
}
