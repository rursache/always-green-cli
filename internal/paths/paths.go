package paths

import (
	"os"
	"path/filepath"
)

func Dir() string {
	return filepath.Join(home(), ".always-green")
}

func MasterKey() string      { return filepath.Join(Dir(), "master.key") }
func ConfigFile() string     { return filepath.Join(Dir(), "config.json.enc") }
func WorkspacesFile() string { return filepath.Join(Dir(), "workspaces.json.enc") }
func StatusFile() string     { return filepath.Join(Dir(), "status.json") }
func DaemonPID() string      { return filepath.Join(Dir(), "daemon.pid") }
func DaemonLock() string     { return filepath.Join(Dir(), "daemon.lock") }
func StoreLock() string      { return filepath.Join(Dir(), "store.lock") }
func DaemonSock() string     { return filepath.Join(Dir(), "daemon.sock") }
func DaemonLog() string      { return filepath.Join(Dir(), "daemon.log") }

// EnsureDir creates the config directory private, and tightens it if it
// already exists with looser bits: it holds the master key and the tokens
func EnsureDir() error {
	dir := Dir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	st, err := os.Stat(dir)
	if err != nil {
		return err
	}
	if st.Mode().Perm()&0o077 != 0 {
		return os.Chmod(dir, 0o700)
	}
	return nil
}

func home() string {
	h, err := os.UserHomeDir()
	if err != nil {
		return "."
	}
	return h
}
