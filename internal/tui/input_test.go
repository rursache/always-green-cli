package tui

import (
	"testing"

	"github.com/rursache/always-green-cli/internal/store"

	tea "github.com/charmbracelet/bubbletea"
)

func typeRunes(t *testing.T, m model, text string) model {
	t.Helper()
	for _, r := range text {
		m = update(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	return m
}

// The text inputs are values inside the model, so a keystroke applied to a
// copy is lost; every field the user can type into must keep what was typed
func TestTypingReachesAddFields(t *testing.T) {
	m := testModel(t)
	m.screen = screenAdd
	m.addFocus = 0
	m.focusAdd()
	m = typeRunes(t, m, "Acme")
	m = update(t, m, tea.KeyMsg{Type: tea.KeyTab})
	m = typeRunes(t, m, "xoxc-1")
	m = update(t, m, tea.KeyMsg{Type: tea.KeyTab})
	m = typeRunes(t, m, "xoxd-2")
	if got := m.nameIn.Value(); got != "Acme" {
		t.Fatalf("name field has %q", got)
	}
	if got := m.xoxcIn.Value(); got != "xoxc-1" {
		t.Fatalf("xoxc field has %q", got)
	}
	if got := m.xoxdIn.Value(); got != "xoxd-2" {
		t.Fatalf("xoxd field has %q", got)
	}
}

func TestTypingReachesScheduleFields(t *testing.T) {
	m := testModel(t, store.Workspace{Name: "acme", TeamID: "T1", Xoxc: "xoxc", Xoxd: "xoxd"})
	m.openSchedule(m.workspaces[0])
	m.startIn.SetValue("")
	m = typeRunes(t, m, "8:30 AM")
	m = update(t, m, tea.KeyMsg{Type: tea.KeyTab})
	m.endIn.SetValue("")
	m = typeRunes(t, m, "4:15 PM")
	if got := m.startIn.Value(); got != "8:30 AM" {
		t.Fatalf("start field has %q", got)
	}
	if got := m.endIn.Value(); got != "4:15 PM" {
		t.Fatalf("end field has %q", got)
	}
}
