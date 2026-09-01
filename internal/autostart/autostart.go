// Package autostart registers the daemon to start when the user logs in,
// entirely within the user's own account: a LaunchAgent on macOS, a systemd
// user unit on Linux, nothing here needs sudo or touches a system directory
package autostart

import "errors"

// ErrUnsupported means this OS has no userland autostart mechanism we handle
var ErrUnsupported = errors.New("launch at login is not supported on this OS")

// Label identifies our entry in the platform's launcher
const Label = "com.rursache.always-green"

// Enable registers the daemon to start at login, it is idempotent
func Enable() error { return enable() }

// Disable removes the registration, disabling when not enabled is not an error
func Disable() error { return disable() }

// Enabled reports whether the registration is currently in place
func Enabled() bool { return enabled() }

// Supported reports whether this OS has an autostart path at all
func Supported() bool { return supported() }
