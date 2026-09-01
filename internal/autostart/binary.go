package autostart

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// currentBinary picks the path the launcher should run. When the binary was
// started through a symlink, such as a Homebrew shim in bin/, the launcher
// keeps that link rather than the versioned target behind it, because the
// target disappears on the next upgrade while the link is repointed
func currentBinary() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	return pickBinary(exe, os.Args[0])
}

func pickBinary(exe, argv0 string) (string, error) {
	abs, err := filepath.Abs(exe)
	if err != nil {
		return "", err
	}
	if _, err := os.Stat(abs); err != nil {
		return "", fmt.Errorf("could not locate the always-green binary: %w", err)
	}
	// os.Executable follows symlinks on some platforms, so recover the path
	// the process was invoked by and prefer it when it leads to the same file
	if invoked, ok := invokedPath(argv0); ok && sameFile(invoked, abs) {
		return invoked, nil
	}
	return abs, nil
}

func invokedPath(argv0 string) (string, bool) {
	if argv0 == "" {
		return "", false
	}
	path := argv0
	if !strings.ContainsRune(argv0, os.PathSeparator) {
		found, err := exec.LookPath(argv0)
		if err != nil {
			return "", false
		}
		path = found
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", false
	}
	return abs, true
}

func sameFile(a, b string) bool {
	fa, err := os.Stat(a)
	if err != nil {
		return false
	}
	fb, err := os.Stat(b)
	if err != nil {
		return false
	}
	return os.SameFile(fa, fb)
}
