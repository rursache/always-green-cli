package tui

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/rursache/always-green-cli/internal/desktop"
	"github.com/rursache/always-green-cli/internal/importws"
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
		{gen: m.scanGen, found: []desktop.Found{{Name: "late", TeamID: "T9", Xoxc: "x", Xoxd: "d"}}},
		{gen: m.scanGen, err: errors.New("keychain denied")},
		{gen: m.scanGen},
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
	m = update(t, m, importScanMsg{gen: m.scanGen, found: []desktop.Found{{Name: "acme", TeamID: "T1", Xoxc: "x", Xoxd: "d"}}})
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

// The store can shrink underneath an open dashboard (a clear from another
// terminal), and the selection must land on a real row, or on zero when the
// list is empty so the first workspace added afterwards is selected
func TestReloadClampsSelection(t *testing.T) {
	ws := func(id string) store.Workspace {
		return store.Workspace{Name: id, TeamID: id, Xoxc: "xoxc", Xoxd: "xoxd"}
	}
	m := testModel(t, ws("T1"), ws("T2"), ws("T3"))
	m = update(t, m, key("j"))
	m = update(t, m, key("j"))
	if m.sel != 2 {
		t.Fatalf("expected the last row selected, got %d", m.sel)
	}
	if err := m.store.ClearWorkspaces(); err != nil {
		t.Fatal(err)
	}
	m.reload()
	if m.sel != 0 {
		t.Fatalf("selection on an empty list should be 0, got %d", m.sel)
	}
	if err := m.store.SaveWorkspace(ws("T4")); err != nil {
		t.Fatal(err)
	}
	m.reload()
	if got, ok := m.selected(); !ok || got.TeamID != "T4" {
		t.Fatalf("the re-added workspace should be selected, got %+v ok %v", got, ok)
	}
}

func keyMsg(t *testing.T, m model, k string) (model, tea.Cmd) {
	t.Helper()
	next, cmd := m.Update(key(k))
	return next.(model), cmd
}

func typeInto(t *testing.T, m model, text string) model {
	t.Helper()
	for _, r := range text {
		m = update(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	return m
}

// Talking to Slack from Update freezes the whole dashboard for the length
// of the round trip, so a paste is handed to a command and Enter is a no-op
// until that command reports back
func TestAddRunsSaveAsCommandAndIgnoresRepeatEnter(t *testing.T) {
	m := testModel(t)
	m = update(t, m, key("a"))
	m = update(t, m, key("esc"))
	m = update(t, m, key("a"))
	m = update(t, m, importScanMsg{gen: m.scanGen})
	if m.screen != screenAdd || !m.xoxcIn.Focused() {
		t.Fatalf("expected the paste screen with the xoxc field focused, got screen %d", m.screen)
	}
	m = typeInto(t, m, "xoxc-token")
	m = update(t, m, tea.KeyMsg{Type: tea.KeyTab})
	m = typeInto(t, m, "xoxd-cookie")

	m, cmd := keyMsg(t, m, "enter")
	if cmd == nil {
		t.Fatal("submitting must return a command, not save inline")
	}
	if !m.saving {
		t.Fatal("model should be marked saving while the command runs")
	}
	m, cmd = keyMsg(t, m, "enter")
	if cmd != nil {
		t.Fatal("a second Enter while saving must not submit again")
	}
	m, _ = keyMsg(t, m, "esc")
	if m.screen != screenAdd {
		t.Fatal("keys must be ignored while saving")
	}

	m = update(t, m, saveDoneMsg{err: errors.New("Slack rejected these tokens")})
	if m.saving || m.err == "" || m.screen != screenAdd {
		t.Fatalf("a failed save should stay on the paste screen with the error: saving %v err %q screen %d", m.saving, m.err, m.screen)
	}
	if got := m.xoxcIn.Value(); got != "xoxc-token" {
		t.Fatalf("a failed save must keep what was typed, got %q", got)
	}
}

func TestAddSuccessOpensScheduleForNewWorkspace(t *testing.T) {
	m := testModel(t, store.Workspace{Name: "acme", TeamID: "T1", Xoxc: "xoxc", Xoxd: "xoxd"})
	m.screen = screenAdd
	m.saving = true
	m.xoxcIn.SetValue("xoxc")
	m = update(t, m, saveDoneMsg{res: importws.Result{Name: "acme", TeamID: "T1", Added: true}})
	if m.saving || m.screen != screenSchedule || m.schedTeam != "T1" {
		t.Fatalf("saving %v screen %d team %q", m.saving, m.screen, m.schedTeam)
	}
	if m.xoxcIn.Value() != "" {
		t.Fatal("the token fields should be cleared after a save")
	}
	m.screen = screenAdd
	m.saving = true
	m = update(t, m, saveDoneMsg{res: importws.Result{Name: "acme", TeamID: "T1"}})
	if m.screen != screenList || m.info != "refreshed acme" {
		t.Fatalf("a re-paste should return to the list: screen %d info %q", m.screen, m.info)
	}
}

func TestImportRunsSaveAsCommand(t *testing.T) {
	m := testModel(t)
	m = update(t, m, key("a"))
	m = update(t, m, importScanMsg{gen: m.scanGen, found: []desktop.Found{{Name: "acme", TeamID: "T1", Xoxc: "x", Xoxd: "d"}}})
	m, cmd := keyMsg(t, m, "enter")
	if cmd == nil || !m.saving {
		t.Fatalf("import must run as a command: cmd %v saving %v", cmd != nil, m.saving)
	}
	m, cmd = keyMsg(t, m, "enter")
	if cmd != nil {
		t.Fatal("a second Enter while saving must not import again")
	}
	m = update(t, m, saveDoneMsg{imported: 1})
	if m.saving || m.screen != screenList || m.info == "" {
		t.Fatalf("saving %v screen %d info %q", m.saving, m.screen, m.info)
	}
}

func TestImportWithNothingPickedDoesNotSave(t *testing.T) {
	m := testModel(t)
	m = update(t, m, key("a"))
	m = update(t, m, importScanMsg{gen: m.scanGen, found: []desktop.Found{{Name: "acme", TeamID: "T1", Xoxc: "x", Xoxd: "d"}}})
	m = update(t, m, key(" "))
	m, cmd := keyMsg(t, m, "enter")
	if cmd != nil || m.saving || m.err == "" {
		t.Fatalf("cmd %v saving %v err %q", cmd != nil, m.saving, m.err)
	}
}

// Backing out of a slow scan and starting another leaves two scans in
// flight; only the newer one's result may be applied, whichever arrives first
func TestOlderScanResultIsIgnoredAfterRescan(t *testing.T) {
	m := testModel(t)
	m = update(t, m, key("a"))
	first := m.scanGen
	m = update(t, m, key("esc"))
	m = update(t, m, key("a"))
	second := m.scanGen
	if first == second {
		t.Fatal("each scan must get its own generation")
	}
	m = update(t, m, importScanMsg{gen: first, err: errors.New("stale failure")})
	if !m.importing || m.err != "" || m.screen != screenImport {
		t.Fatalf("the older scan must not be applied: importing %v err %q screen %d", m.importing, m.err, m.screen)
	}
	m = update(t, m, importScanMsg{gen: second, found: []desktop.Found{{Name: "acme", TeamID: "T1", Xoxc: "x", Xoxd: "d"}}})
	if m.importing || len(m.impFound) != 1 {
		t.Fatalf("the newer scan must land: importing %v found %d", m.importing, len(m.impFound))
	}
}
