package paths

import (
	"os"
	"path/filepath"
)

func Dir() string {
	return filepath.Join(home(), ".always-green")
}

func MasterKey() string     { return filepath.Join(Dir(), "master.key") }
func ConfigFile() string    { return filepath.Join(Dir(), "config.json.enc") }
func WorkspacesFile() string { return filepath.Join(Dir(), "workspaces.json.enc") }
func StatusFile() string    { return filepath.Join(Dir(), "status.json") }
func DaemonPID() string     { return filepath.Join(Dir(), "daemon.pid") }
func DaemonSock() string    { return filepath.Join(Dir(), "daemon.sock") }
func DaemonLog() string     { return filepath.Join(Dir(), "daemon.log") }

func EnsureDir() error {
	return os.MkdirAll(Dir(), 0o700)
}

func home() string {
	h, err := os.UserHomeDir()
	if err != nil {
		return "."
	}
	return h
}
