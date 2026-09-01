package tui

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/rursache/always-green-cli/internal/desktop"
	"github.com/rursache/always-green-cli/internal/paths"
	"github.com/rursache/always-green-cli/internal/store"

	tea "github.com/charmbracelet/bubbletea"
)

func testModel(t *testing.T, workspaces ...store.Workspace) model {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	st, err := store.Open()
	if err != nil {
		t.Fatal(err)
	}
	for _, ws := range workspaces {
		if err := st.SaveWorkspace(ws); err != nil {
			t.Fatal(err)
		}
	}
	return newModel(st)
}

func key(s string) tea.KeyMsg {
	switch s {
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEscape}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

func update(t *testing.T, m model, msg tea.Msg) model {
	t.Helper()
	next, _ := m.Update(msg)
	out, ok := next.(model)
	if !ok {
		t.Fatalf("Update returned %T", next)
	}
	return out
}

// A desktop scan can take a while (a Keychain prompt on macOS), and the user
// may back out and open another screen before it finishes; its result must
// then be dropped rather than replace whatever they are doing
func TestStaleScanResultDoesNotChangeScreen(t *testing.T) {
	m := testModel(t, store.Workspace{Name: "acme", TeamID: "T1", Xoxc: "xoxc", Xoxd: "xoxd"})
	m = update(t, m, key("a"))
	if m.screen != screenImport || !m.importing {
		t.Fatalf("expected the import screen to be waiting on a scan, got screen %d importing %v", m.screen, m.importing)
	}
	m = update(t, m, key("esc"))
	m = update(t, m, key("c"))
	if m.screen != screenSchedule {
		t.Fatalf("expected the schedule screen, got %d", m.screen)
	}

	for _, msg := range []importScanMsg{
		{found: []desktop.Found{{Name: "late", TeamID: "T9", Xoxc: "x", Xoxd: "d"}}},
		{err: errors.New("keychain denied")},
		{},
	} {
		got := update(t, m, msg)
		if got.screen != screenSchedule {
			t.Fatalf("stale scan %+v moved the user to screen %d", msg, got.screen)
		}
		if got.err != "" {
			t.Fatalf("stale scan %+v surfaced an error: %q", msg, got.err)
		}
	}
}

func TestScanResultStillLandsWhileWaiting(t *testing.T) {
	m := testModel(t)
	m = update(t, m, key("a"))
	m = update(t, m, importScanMsg{found: []desktop.Found{{Name: "acme", TeamID: "T1", Xoxc: "x", Xoxd: "d"}}})
	if m.screen != screenImport || m.importing || len(m.impFound) != 1 {
		t.Fatalf("expected the pick list, got screen %d importing %v found %d", m.screen, m.importing, len(m.impFound))
	}
}

func TestDeleteReportsFailure(t *testing.T) {
	m := testModel(t, store.Workspace{Name: "acme", TeamID: "T1", Xoxc: "xoxc", Xoxd: "xoxd"})
	m = update(t, m, key("d"))
	if m.screen != screenDelete {
		t.Fatalf("expected the delete prompt, got screen %d", m.screen)
	}
	// make the store unwritable so the removal cannot go through
	if err := os.Chmod(paths.WorkspacesFile(), 0o400); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(filepath.Dir(paths.WorkspacesFile()), 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = os.Chmod(filepath.Dir(paths.WorkspacesFile()), 0o700)
		_ = os.Chmod(paths.WorkspacesFile(), 0o600)
	})
	m = update(t, m, key("y"))
	if m.screen != screenList {
		t.Fatalf("expected to be back on the list, got screen %d", m.screen)
	}
	if m.info != "" || m.err == "" {
		t.Fatalf("a failed removal must not claim success: info %q err %q", m.info, m.err)
	}
	if len(m.workspaces) != 1 {
		t.Fatalf("workspace should still be listed, got %d", len(m.workspaces))
	}
}

func TestDeleteRemovesWorkspace(t *testing.T) {
	m := testModel(t, store.Workspace{Name: "acme", TeamID: "T1", Xoxc: "xoxc", Xoxd: "xoxd"})
	m = update(t, m, key("d"))
	m = update(t, m, key("y"))
	if m.err != "" || m.info != "removed acme" || len(m.workspaces) != 0 {
		t.Fatalf("err %q info %q workspaces %d", m.err, m.info, len(m.workspaces))
	}
}
