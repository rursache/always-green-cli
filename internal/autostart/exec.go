package autostart

import (
	"context"
	"os/exec"
	"time"
)

// runTimeout bounds how long an external launcher command (launchctl,
// systemctl, loginctl) is allowed to run before we give up on it
const runTimeout = 15 * time.Second

func run(name string, args ...string) error {
	ctx, cancel := context.WithTimeout(context.Background(), runTimeout)
	defer cancel()
	return exec.CommandContext(ctx, name, args...).Run()
}
