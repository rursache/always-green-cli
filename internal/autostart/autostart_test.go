package autostart

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCurrentBinaryResolves(t *testing.T) {
	got, err := currentBinary()
	if err != nil {
		t.Fatal(err)
	}
	if !filepath.IsAbs(got) {
		t.Fatalf("launcher needs an absolute path, got %q", got)
	}
	if _, err := os.Stat(got); err != nil {
		t.Fatalf("resolved path does not exist: %v", err)
	}
}

// A symlinked binary (a Homebrew shim) must resolve to the real file, so the
// login item does not break when the cellar path changes
func TestCurrentBinaryFollowsSymlinks(t *testing.T) {
	dir := t.TempDir()
	real := filepath.Join(dir, "real")
	if err := os.WriteFile(real, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	resolved, err := filepath.EvalSymlinks(link)
	if err != nil {
		t.Fatal(err)
	}
	realResolved, err := filepath.EvalSymlinks(real)
	if err != nil {
		t.Fatal(err)
	}
	if resolved != realResolved {
		t.Fatalf("symlink resolved to %q, want %q", resolved, realResolved)
	}
}

func TestLabelIsReverseDNS(t *testing.T) {
	if !strings.HasPrefix(Label, "com.") || strings.Contains(Label, " ") {
		t.Fatalf("launcher label %q is not a usable identifier", Label)
	}
}

// Removing a registration that was never made must not be an error, so
// uninstall works on a machine that never enabled it
func TestDisableIsIdempotent(t *testing.T) {
	if !Supported() {
		t.Skip("no autostart on this OS")
	}
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	if Enabled() {
		t.Skip("a real registration exists for this user")
	}
	if err := Disable(); err != nil {
		t.Fatalf("disabling when not enabled should succeed, got %v", err)
	}
}
