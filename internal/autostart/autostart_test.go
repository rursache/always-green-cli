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

// A binary started through a symlink (a Homebrew shim in bin/) must keep the
// symlink in the login item: the versioned target behind it is deleted on
// the next upgrade while the link is repointed to the new version
func TestPickBinaryKeepsInvokedSymlink(t *testing.T) {
	dir := t.TempDir()
	real := filepath.Join(dir, "cellar", "1.0", "always-green")
	if err := os.MkdirAll(filepath.Dir(real), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(real, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "bin", "always-green")
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}

	got, err := pickBinary(real, link)
	if err != nil {
		t.Fatal(err)
	}
	if got != link {
		t.Fatalf("got %q, want the symlink %q", got, link)
	}

	// a bare command name is resolved through PATH the way the shell did
	t.Setenv("PATH", filepath.Dir(link))
	got, err = pickBinary(real, "always-green")
	if err != nil {
		t.Fatal(err)
	}
	if got != link {
		t.Fatalf("got %q, want the symlink %q found on PATH", got, link)
	}
}

// argv[0] is under the caller's control and may point anywhere, so it is only
// trusted when it really is the running binary
func TestPickBinaryIgnoresUnrelatedArgv0(t *testing.T) {
	dir := t.TempDir()
	real := filepath.Join(dir, "always-green")
	other := filepath.Join(dir, "other")
	for _, p := range []string{real, other} {
		if err := os.WriteFile(p, []byte("#!/bin/sh\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, argv0 := range []string{other, filepath.Join(dir, "missing"), "", "no-such-command-on-path"} {
		got, err := pickBinary(real, argv0)
		if err != nil {
			t.Fatal(err)
		}
		if got != real {
			t.Fatalf("argv0 %q: got %q, want %q", argv0, got, real)
		}
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
