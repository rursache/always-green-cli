package tui

import (
	"errors"
	"testing"

	"github.com/rursache/always-green-cli/internal/desktop"
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
