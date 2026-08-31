package autostart

import (
	"fmt"
	"os"
	"path/filepath"
)

// currentBinary resolves the path the launcher should run. Symlinks are
// followed so a Homebrew shim in bin/ does not break when the cellar moves.
func currentBinary() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	abs, err := filepath.Abs(exe)
	if err != nil {
		return "", err
	}
	if _, err := os.Stat(abs); err != nil {
		return "", fmt.Errorf("could not locate the always-green binary: %w", err)
	}
	return abs, nil
}
