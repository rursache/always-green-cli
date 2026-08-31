package store

import (
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/rursache/always-green/internal/paths"
)

func tempStore(t *testing.T) *Store {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	st, err := Open()
	if err != nil {
		t.Fatal(err)
	}
	return st
}

func TestRoundTripWorkspace(t *testing.T) {
	st := tempStore(t)
	if err := st.SaveWorkspace(Workspace{Name: "acme", TeamID: "T1", Xoxc: "xoxc-1", Xoxd: "xoxd-1"}); err != nil {
		t.Fatal(err)
	}
	got, ok := st.Workspace("T1")
	if !ok || got.Name != "acme" || got.Xoxc != "xoxc-1" {
		t.Fatalf("got %+v ok=%v", got, ok)
	}
}

// Tokens are only as private as the key that decrypts them
func TestMasterKeyIsPrivate(t *testing.T) {
	st := tempStore(t)
	_ = st
	info, err := os.Stat(paths.MasterKey())
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm&0o077 != 0 {
		t.Fatalf("master.key is %o, must not be readable by others", perm)
	}
}

func TestLooseMasterKeyIsTightened(t *testing.T) {
	tempStore(t)
	if err := os.Chmod(paths.MasterKey(), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(paths.MasterKey())
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm&0o077 != 0 {
		t.Fatalf("a world-readable key was accepted as-is: %o", perm)
	}
}

// MkdirAll does not touch an existing directory's mode
func TestExistingConfigDirIsTightened(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := filepath.Join(home, ".always-green")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := paths.EnsureDir(); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm&0o077 != 0 {
		t.Fatalf("config dir left at %o", perm)
	}
}

func TestClearWorkspacesOnFreshInstall(t *testing.T) {
	st := tempStore(t)
	if err := st.ClearWorkspaces(); err != nil {
		t.Fatalf("clearing an empty store should succeed, got %v", err)
	}
}

// The lock must admit exactly one holder at a time, including across separate
// file descriptors in one process, which is what two Stores amount to
func TestWithFileLockAdmitsOneAtATime(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if err := paths.EnsureDir(); err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	inside, peak := 0, 0
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := withFileLock(func() error {
				mu.Lock()
				inside++
				if inside > peak {
					peak = inside
				}
				mu.Unlock()
				time.Sleep(2 * time.Millisecond)
				mu.Lock()
				inside--
				mu.Unlock()
				return nil
			}); err != nil {
				t.Errorf("withFileLock: %v", err)
			}
		}()
	}
	wg.Wait()
	if peak != 1 {
		t.Fatalf("%d writers were inside the critical section at once", peak)
	}
}

// A read-modify-write that another process can interleave with loses the
// other side's field. The sleep widens the window a real daemon/CLI pair hits
// by doing decrypt, mutate, encrypt, write between the read and the rename.
func TestConcurrentUpdatesDoNotLoseFields(t *testing.T) {
	st := tempStore(t)
	if err := st.SaveWorkspace(Workspace{Name: "acme", TeamID: "T1"}); err != nil {
		t.Fatal(err)
	}
	other, err := Open()
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 30; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = st.UpdateWorkspace("T1", func(w *Workspace) {
				time.Sleep(time.Millisecond)
				w.TokenInvalid = true
			})
		}()
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = other.UpdateWorkspace("T1", func(w *Workspace) {
				time.Sleep(time.Millisecond)
				w.Paused = true
			})
		}()
	}
	wg.Wait()
	got, ok := st.Workspace("T1")
	if !ok {
		t.Fatal("workspace vanished under concurrent updates")
	}
	if !got.TokenInvalid || !got.Paused {
		t.Fatalf("an update was lost: TokenInvalid=%v Paused=%v", got.TokenInvalid, got.Paused)
	}
}

// The lock has to hold across processes, not just goroutines
func TestStoreLockIsCrossProcess(t *testing.T) {
	if os.Getenv("AG_LOCK_CHILD") == "1" {
		st, err := Open()
		if err != nil {
			os.Exit(2)
		}
		for i := 0; i < 60; i++ {
			if err := st.UpdateWorkspace("T1", func(w *Workspace) {
				time.Sleep(time.Millisecond)
				w.Paused = true
			}); err != nil {
				os.Exit(3)
			}
		}
		os.Exit(0)
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	st, err := Open()
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SaveWorkspace(Workspace{Name: "acme", TeamID: "T1"}); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(os.Args[0], "-test.run=TestStoreLockIsCrossProcess")
	cmd.Env = append(os.Environ(), "AG_LOCK_CHILD=1", "HOME="+home)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 60; i++ {
		if err := st.UpdateWorkspace("T1", func(w *Workspace) {
			time.Sleep(time.Millisecond)
			w.TokenInvalid = true
		}); err != nil {
			t.Fatal(err)
		}
	}
	got, ok := st.Workspace("T1")
	if !ok {
		t.Fatal("workspace vanished")
	}
	if !got.TokenInvalid || !got.Paused {
		t.Fatalf("cross-process update lost: TokenInvalid=%v Paused=%v", got.TokenInvalid, got.Paused)
	}
}
