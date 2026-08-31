//go:build linux

package autostart

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

func supported() bool { return true }

const unitName = "always-green.service"

func unitPath() string {
	dir := os.Getenv("XDG_CONFIG_HOME")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		dir = filepath.Join(home, ".config")
	}
	return filepath.Join(dir, "systemd", "user", unitName)
}

// Type=forking because the daemon subcommand runs in the foreground and we
// want systemd to track it directly, so Type=simple is correct here.
const unitTemplate = `[Unit]
Description=always-green (keep Slack presence active)
After=network-online.target

[Service]
Type=simple
ExecStart=%s daemon
Restart=on-failure
RestartSec=10

[Install]
WantedBy=default.target
`

func enable() error {
	path := unitPath()
	if path == "" {
		return fmt.Errorf("could not locate your systemd user directory")
	}
	exe, err := currentBinary()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	body := fmt.Sprintf(unitTemplate, exe)
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		return err
	}
	_ = run("systemctl", "--user", "daemon-reload")
	if err := run("systemctl", "--user", "enable", unitName); err != nil {
		return fmt.Errorf("could not enable the user service: %w", err)
	}
	// without lingering the unit only runs while a session is open; this is
	// best effort because it may need an admin on some distributions
	if u := os.Getenv("USER"); u != "" {
		_ = run("loginctl", "enable-linger", u)
	}
	return nil
}

func disable() error {
	path := unitPath()
	if path == "" {
		return nil
	}
	_ = run("systemctl", "--user", "disable", "--now", unitName)
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	_ = run("systemctl", "--user", "daemon-reload")
	return nil
}

func enabled() bool {
	path := unitPath()
	if path == "" {
		return false
	}
	_, err := os.Stat(path)
	return err == nil
}

func run(name string, args ...string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	return exec.CommandContext(ctx, name, args...).Run()
}
