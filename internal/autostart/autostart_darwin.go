//go:build darwin

package autostart

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func supported() bool { return true }

func plistPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, "Library", "LaunchAgents", Label+".plist")
}

// RunAtLoad without KeepAlive: launchd starts the daemon at login, but a
// deliberate "always-green stop" stays stopped instead of being resurrected.
const plistTemplate = `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key>
	<string>%s</string>
	<key>ProgramArguments</key>
	<array>
		<string>%s</string>
		<string>daemon</string>
	</array>
	<key>RunAtLoad</key>
	<true/>
	<key>KeepAlive</key>
	<false/>
	<key>ProcessType</key>
	<string>Background</string>
	<key>StandardErrorPath</key>
	<string>%s</string>
</dict>
</plist>
`

func enable() error {
	path := plistPath()
	if path == "" {
		return fmt.Errorf("could not locate your LaunchAgents directory")
	}
	exe, err := currentBinary()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	logPath := filepath.Join(filepath.Dir(path), Label+".err.log")
	body := fmt.Sprintf(plistTemplate, Label, escapeXML(exe), escapeXML(logPath))
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		return err
	}
	// replace any previous registration so a moved binary is picked up
	_ = run("launchctl", "bootout", target())
	if err := run("launchctl", "bootstrap", guiDomain(), path); err != nil {
		// older macOS only understands load
		if err2 := run("launchctl", "load", "-w", path); err2 != nil {
			return fmt.Errorf("could not register the login item: %w", err)
		}
	}
	return nil
}

func disable() error {
	path := plistPath()
	if path == "" {
		return nil
	}
	_ = run("launchctl", "bootout", target())
	_ = run("launchctl", "unload", "-w", path)
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func enabled() bool {
	path := plistPath()
	if path == "" {
		return false
	}
	if _, err := os.Stat(path); err != nil {
		return false
	}
	// the plist can exist while launchd no longer has it loaded (a manual
	// bootout, or a bootstrap that partially failed), so confirm with launchd
	return run("launchctl", "print", target()) == nil
}

func guiDomain() string { return fmt.Sprintf("gui/%d", os.Getuid()) }
func target() string    { return guiDomain() + "/" + Label }

func escapeXML(s string) string {
	r := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;")
	return r.Replace(s)
}
