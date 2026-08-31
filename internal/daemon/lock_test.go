package daemon

import (
	"os"
	"testing"

	"github.com/rursache/always-green/internal/paths"
)

func withTempHome(t *testing.T) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	if err := paths.EnsureDir(); err != nil {
		t.Fatal(err)
	}
}

func TestRunningIsFalseWithNoDaemon(t *testing.T) {
	withTempHome(t)
	if Running() {
		t.Fatal("no daemon has started, Running must be false")
	}
}

// A PID file left behind by a crash used to be trusted whenever some unrelated
// process happened to hold that PID, so Start would silently do nothing
func TestStalePidFileIsNotMistakenForALiveDaemon(t *testing.T) {
	withTempHome(t)
	// PID 1 always exists and always answers signal 0, standing in for any
	// recycled PID; under the old kill(pid,0) check this read as "running"
	if err := os.WriteFile(paths.DaemonPID(), []byte("1"), 0o600); err != nil {
		t.Fatal(err)
	}
	if Running() {
		t.Fatal("a stale PID file must not count as a running daemon")
	}
}

func TestLockIsExclusive(t *testing.T) {
	withTempHome(t)
	lock, err := acquireLock()
	if err != nil {
		t.Fatal(err)
	}
	if !Running() {
		t.Fatal("Running must see a held lock")
	}
	if _, err := acquireLock(); err != ErrAlreadyRunning {
		t.Fatalf("a second daemon must be refused, got %v", err)
	}
	if err := lock.Close(); err != nil {
		t.Fatal(err)
	}
	if Running() {
		t.Fatal("closing the lock must release it")
	}
	if _, err := acquireLock(); err != nil {
		t.Fatalf("lock should be reacquirable after release: %v", err)
	}
}
