package tui

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/rursache/always-green-cli/internal/daemon"
	"github.com/rursache/always-green-cli/internal/desktop"
	"github.com/rursache/always-green-cli/internal/importws"
	"github.com/rursache/always-green-cli/internal/schedule"
	"github.com/rursache/always-green-cli/internal/slackx"
	"github.com/rursache/always-green-cli/internal/store"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type screen int

const (
	screenList screen = iota
	screenAdd
	screenImport
	screenSchedule
	screenKeep
	screenDelete
)

type presenceMsg struct {
	teamID string
	pres   slackx.Presence
	err    error
}

type tickMsg time.Time

// how many one-second ticks between presence polls
const presencePollTicks = 30

// importScanMsg carries the scan generation it answers, so a scan the user
// backed out of cannot be mistaken for the one they started afterwards
type importScanMsg struct {
	gen   int
	found []desktop.Found
	err   error
}

// saveDoneMsg reports a workspace save that ran off the event loop; imported
// counts the workspaces saved from the Slack app, res describes a paste
type saveDoneMsg struct {
	imported int
	res      importws.Result
	err      error
}

type model struct {
	store      *store.Store
	tz         string
	workspaces []store.Workspace
	presence   map[string]slackx.Presence
	daemon     daemon.Status
	sel        int
	screen     screen
	err        string
	info       string
	width      int
	ticks      int

	nameIn   textinput.Model
	xoxcIn   textinput.Model
	xoxdIn   textinput.Model
	addFocus int

	schedDays  map[string]bool
	startIn    textinput.Model
	endIn      textinput.Model
	schedFocus int
	schedTeam  string
	schedName  string

	keepHours int
	keepTeam  string
	keepName  string

	delTeam string
	delName string

	// saving is set while a save talks to Slack in the background, so a
	// second Enter cannot submit twice or land on the next screen
	saving    bool
	importing bool
	scanGen   int
	impFound  []desktop.Found
	impPick   map[int]bool
	impSel    int
	impErr    string
}

func Run() error {
	st, err := store.Open()
	if err != nil {
		return err
	}
	if !daemon.Running() {
		if err := daemon.Start(); err != nil {
			return fmt.Errorf("could not start daemon: %w", err)
		}
	}
	m := newModel(st)
	p := tea.NewProgram(m, tea.WithAltScreen())
	_, err = p.Run()
	return err
}

func newModel(st *store.Store) model {
	cfg, _ := st.Config()
	tz := cfg.Timezone
	if tz == "" {
		tz = schedule.DetectTimezone()
	}
	m := model{
		store:     st,
		tz:        tz,
		presence:  map[string]slackx.Presence{},
		nameIn:    newInput("My company"),
		xoxcIn:    newSecretInput("xoxc-..."),
		xoxdIn:    newSecretInput("xoxd-..."),
		startIn:   newInput("9:00 AM"),
		endIn:     newInput("5:00 PM"),
		schedDays: weekdaySet(),
		keepHours: 2,
	}
	m.reload()
	return m
}

func newInput(ph string) textinput.Model {
	ti := textinput.New()
	ti.Placeholder = ph
	ti.CharLimit = 4096
	ti.Width = 56
	return ti
}

// secrets are masked so a token is not left on screen during a screenshare
func newSecretInput(ph string) textinput.Model {
	ti := newInput(ph)
	ti.EchoMode = textinput.EchoPassword
	ti.EchoCharacter = '*'
	return ti
}

func weekdaySet() map[string]bool {
	return map[string]bool{
		"monday": true, "tuesday": true, "wednesday": true,
		"thursday": true, "friday": true,
		"saturday": false, "sunday": false,
	}
}

func (m model) Init() tea.Cmd {
	return tea.Batch(pollPresence(m.workspaces), tick())
}

func tick() tea.Cmd {
	return tea.Tick(time.Second, func(t time.Time) tea.Msg { return tickMsg(t) })
}

func pollPresence(list []store.Workspace) tea.Cmd {
	var cmds []tea.Cmd
	for _, ws := range list {
		ws := ws
		cmds = append(cmds, func() tea.Msg {
			p, err := slackx.GetPresence(ws.Xoxc, ws.Xoxd, ws.UserID)
			return presenceMsg{teamID: ws.TeamID, pres: p, err: err}
		})
	}
	if len(cmds) == 0 {
		return nil
	}
	return tea.Batch(cmds...)
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
	case tickMsg:
		st, _ := daemon.ReadStatus()
		m.daemon = st
		m.ticks++
		// the daemon polls Slack itself and gets presence over the websocket,
		// so the dashboard only needs an occasional refresh of its own
		if m.ticks%presencePollTicks == 0 {
			m.reload()
			return m, tea.Batch(tick(), pollPresence(m.workspaces))
		}
		if m.ticks%2 == 0 {
			m.reload()
		}
		return m, tick()
	case presenceMsg:
		if msg.err == nil {
			m.presence[msg.teamID] = msg.pres
		} else if api, ok := msg.err.(slackx.APIError); ok && slackx.TokenDead(api.Code) {
			m.presence[msg.teamID] = slackx.Presence{Presence: "invalid"}
		}
	case importScanMsg:
		// a scan the user backed out of must not yank them off whatever
		// screen they moved to in the meantime, nor stand in for a newer one
		if !m.importing || msg.gen != m.scanGen {
			return m, nil
		}
		m.importing = false
		if msg.err != nil {
			m.impErr = msg.err.Error()
			m.screen = screenAdd
			m.addFocus = 1
			m.xoxcIn.Focus()
			m.err = "desktop import failed: " + msg.err.Error() + "  (paste tokens manually)"
			return m, nil
		}
		if len(msg.found) == 0 {
			m.screen = screenAdd
			m.addFocus = 1
			m.focusAdd()
			m.err = "no workspaces in the Slack app, paste tokens instead"
			return m, nil
		}
		m.impFound = msg.found
		m.impPick = map[int]bool{}
		for i := range msg.found {
			m.impPick[i] = true
		}
		m.impSel = 0
		m.screen = screenImport
		return m, nil
	case saveDoneMsg:
		return m.saveDone(msg)
	case tea.KeyMsg:
		return m.handleKey(msg)
	}
	cmd := m.updateInputs(msg)
	return m, cmd
}

func (m model) saveDone(msg saveDoneMsg) (tea.Model, tea.Cmd) {
	m.saving = false
	if msg.err != nil {
		m.err = msg.err.Error()
		return m, nil
	}
	_ = daemon.Reload()
	m.reload()
	m.err = ""
	if msg.imported > 0 {
		m.screen = screenList
		m.info = fmt.Sprintf("imported %d workspace(s) from Slack", msg.imported)
		return m, pollPresence(m.workspaces)
	}
	m.xoxcIn.SetValue("")
	m.xoxdIn.SetValue("")
	m.nameIn.SetValue("")
	if !msg.res.Added {
		m.screen = screenList
		m.info = "refreshed " + msg.res.Name
		return m, pollPresence(m.workspaces)
	}
	ws, _ := m.store.Workspace(msg.res.TeamID)
	m.openSchedule(ws)
	m.info = "added " + msg.res.Name
	return m, pollPresence(m.workspaces)
}

// updateInputs needs the pointer receiver: the text inputs are values, and
// feeding a keystroke to a copy of the model silently drops it
func (m *model) updateInputs(msg tea.Msg) tea.Cmd {
	var cmd tea.Cmd
	switch m.screen {
	case screenAdd:
		switch m.addFocus {
		case 0:
			m.nameIn, cmd = m.nameIn.Update(msg)
		case 1:
			m.xoxcIn, cmd = m.xoxcIn.Update(msg)
		case 2:
			m.xoxdIn, cmd = m.xoxdIn.Update(msg)
		}
	case screenSchedule:
		if m.schedFocus == 0 {
			m.startIn, cmd = m.startIn.Update(msg)
		} else {
			m.endIn, cmd = m.endIn.Update(msg)
		}
	}
	return cmd
}

func (m model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch m.screen {
	case screenAdd:
		return m.keyAdd(msg)
	case screenImport:
		return m.keyImport(msg)
	case screenSchedule:
		return m.keySchedule(msg)
	case screenKeep:
		return m.keyKeep(msg)
	case screenDelete:
		return m.keyDelete(msg)
	default:
		return m.keyList(msg)
	}
}

func (m model) keyList(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q", "ctrl+c":
		return m, tea.Quit
	case "j", "down":
		if len(m.workspaces) > 0 {
			m.sel = (m.sel + 1) % len(m.workspaces)
		}
	case "k", "up":
		if len(m.workspaces) > 0 {
			m.sel = (m.sel - 1 + len(m.workspaces)) % len(m.workspaces)
		}
	case "r":
		m.reload()
		m.info = "refreshed"
		return m, pollPresence(m.workspaces)
	case "a":
		m.err = ""
		m.importing = true
		m.screen = screenImport
		m.impErr = ""
		m.scanGen++
		return m, scanDesktop(m.scanGen)
	case "p":
		if ws, ok := m.selected(); ok {
			paused := !ws.Paused
			_ = m.store.UpdateWorkspace(ws.TeamID, func(w *store.Workspace) {
				w.Paused = paused
				if paused {
					w.KeepOnlineUntil = ""
				}
			})
			_ = daemon.Reload()
			m.reload()
		}
	case "d":
		if ws, ok := m.selected(); ok {
			m.screen = screenDelete
			m.delTeam = ws.TeamID
			m.delName = ws.Name
		}
	case "c":
		if ws, ok := m.selected(); ok {
			m.openSchedule(ws)
		}
	case "enter":
		if ws, ok := m.selected(); ok {
			m.screen = screenKeep
			m.keepHours = 2
			m.keepTeam = ws.TeamID
			m.keepName = ws.Name
		}
	}
	return m, nil
}

func scanDesktop(gen int) tea.Cmd {
	return func() tea.Msg {
		found, err := importws.Discover()
		return importScanMsg{gen: gen, found: found, err: err}
	}
}

func (m model) keyImport(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.saving {
		return m, nil
	}
	if m.importing {
		if msg.String() == "esc" || msg.String() == "q" {
			m.importing = false
			m.screen = screenList
		}
		return m, nil
	}
	switch msg.String() {
	case "esc":
		m.screen = screenList
	case "m":
		m.screen = screenAdd
		m.addFocus = 1
		m.xoxcIn.Focus()
		m.nameIn.Blur()
		m.xoxdIn.Blur()
		m.err = ""
	case "j", "down":
		if len(m.impFound) > 0 {
			m.impSel = (m.impSel + 1) % len(m.impFound)
		}
	case "k", "up":
		if len(m.impFound) > 0 {
			m.impSel = (m.impSel - 1 + len(m.impFound)) % len(m.impFound)
		}
	case " ":
		m.impPick[m.impSel] = !m.impPick[m.impSel]
	case "enter":
		return m.submitImport()
	}
	return m, nil
}

func (m model) submitImport() (tea.Model, tea.Cmd) {
	var picked []desktop.Found
	for i, f := range m.impFound {
		if m.impPick[i] {
			picked = append(picked, f)
		}
	}
	if len(picked) == 0 {
		m.err = "select at least one workspace"
		return m, nil
	}
	m.saving = true
	m.err = ""
	st := m.store
	return m, func() tea.Msg {
		for i, f := range picked {
			if _, err := importws.Save(st, f, store.SourceDesktop); err != nil {
				return saveDoneMsg{imported: i, err: err}
			}
		}
		return saveDoneMsg{imported: len(picked)}
	}
}

func (m model) keyAdd(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.saving {
		return m, nil
	}
	switch msg.String() {
	case "esc":
		m.screen = screenList
	case "tab", "shift+tab":
		dir := 1
		if msg.String() == "shift+tab" {
			dir = -1
		}
		m.addFocus = (m.addFocus + dir + 3) % 3
		m.focusAdd()
	case "enter":
		return m.submitAdd()
	}
	cmd := m.updateInputs(msg)
	return m, cmd
}

func (m *model) focusAdd() {
	m.nameIn.Blur()
	m.xoxcIn.Blur()
	m.xoxdIn.Blur()
	switch m.addFocus {
	case 0:
		m.nameIn.Focus()
	case 1:
		m.xoxcIn.Focus()
	case 2:
		m.xoxdIn.Focus()
	}
}

func (m model) submitAdd() (tea.Model, tea.Cmd) {
	xoxc := strings.TrimSpace(m.xoxcIn.Value())
	xoxd := strings.TrimSpace(m.xoxdIn.Value())
	if c, d, err := slackx.DecodeTokenBlob(xoxc); err == nil {
		xoxc, xoxd = c, d
	}
	if xoxc == "" || xoxd == "" {
		m.err = "paste both xoxc and xoxd"
		return m, nil
	}
	// the same path the CLI uses, so a pasted workspace records its domain
	// and everything else the daemon needs to refresh it later on its own;
	// it talks to Slack, so it runs as a command rather than on the loop
	f := desktop.Found{Name: strings.TrimSpace(m.nameIn.Value()), Xoxc: xoxc, Xoxd: xoxd}
	m.saving = true
	m.err = ""
	st := m.store
	return m, func() tea.Msg {
		res, err := importws.Save(st, f, store.SourcePaste)
		return saveDoneMsg{res: res, err: err}
	}
}

func (m *model) openSchedule(ws store.Workspace) {
	m.screen = screenSchedule
	m.schedTeam = ws.TeamID
	m.schedName = ws.Name
	m.schedDays = weekdaySet()
	m.startIn.SetValue("9:00 AM")
	m.endIn.SetValue("5:00 PM")
	if ws.Schedule != nil {
		m.schedDays = map[string]bool{}
		for _, id := range schedule.DayIDs {
			m.schedDays[id] = false
		}
		for _, d := range ws.Schedule.ActiveDays {
			m.schedDays[d] = true
		}
		if h, min, ok := schedule.ParseClock(ws.Schedule.StartTime); ok {
			m.startIn.SetValue(schedule.Clock12h(h, min))
		}
		if h, min, ok := schedule.ParseClock(ws.Schedule.EndTime); ok {
			m.endIn.SetValue(schedule.Clock12h(h, min))
		}
	}
	m.schedFocus = 0
	m.startIn.Focus()
	m.endIn.Blur()
	m.err = ""
}

func (m model) keySchedule(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.screen = screenList
	case "tab":
		m.schedFocus = 1 - m.schedFocus
		if m.schedFocus == 0 {
			m.startIn.Focus()
			m.endIn.Blur()
		} else {
			m.endIn.Focus()
			m.startIn.Blur()
		}
	case "w":
		for _, id := range schedule.DayIDs[:5] {
			m.schedDays[id] = true
		}
		m.schedDays["saturday"] = false
		m.schedDays["sunday"] = false
		return m, nil
	case "e":
		for _, id := range schedule.DayIDs {
			m.schedDays[id] = true
		}
		return m, nil
	case "1", "2", "3", "4", "5", "6", "7":
		if m.startIn.Focused() || m.endIn.Focused() {
			cmd := m.updateInputs(msg)
			return m, cmd
		}
		n, _ := strconv.Atoi(msg.String())
		id := schedule.DayIDs[n-1]
		m.schedDays[id] = !m.schedDays[id]
		return m, nil
	case "left":
		if m.schedFocus == 0 {
			m.startIn.SetValue(schedule.NudgeClock(m.startIn.Value(), -30))
		} else {
			m.endIn.SetValue(schedule.NudgeClock(m.endIn.Value(), -30))
		}
		return m, nil
	case "right":
		if m.schedFocus == 0 {
			m.startIn.SetValue(schedule.NudgeClock(m.startIn.Value(), 30))
		} else {
			m.endIn.SetValue(schedule.NudgeClock(m.endIn.Value(), 30))
		}
		return m, nil
	case "ctrl+a":
		_ = m.store.UpdateWorkspace(m.schedTeam, func(w *store.Workspace) {
			w.Schedule = nil
		})
		_ = daemon.Reload()
		m.reload()
		m.screen = screenList
		m.info = "always on"
		return m, nil
	case "enter":
		sh, sm, ok1 := schedule.ParseClock(m.startIn.Value())
		eh, em, ok2 := schedule.ParseClock(m.endIn.Value())
		if !ok1 || !ok2 {
			m.err = "could not parse those times"
			return m, nil
		}
		var days []string
		for _, id := range schedule.DayIDs {
			if m.schedDays[id] {
				days = append(days, id)
			}
		}
		if len(days) == 0 {
			m.err = "pick at least one day (1-7, or w / e)"
			return m, nil
		}
		_ = m.store.UpdateWorkspace(m.schedTeam, func(w *store.Workspace) {
			w.Schedule = &schedule.Window{
				ActiveDays: days,
				StartTime:  schedule.Clock24h(sh, sm),
				EndTime:    schedule.Clock24h(eh, em),
			}
		})
		_ = daemon.Reload()
		m.reload()
		m.screen = screenList
		m.info = "schedule saved"
		return m, nil
	}
	cmd := m.updateInputs(msg)
	return m, cmd
}

func (m model) keyKeep(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.screen = screenList
	case "left", "h":
		if m.keepHours > 1 {
			m.keepHours--
		}
	case "right", "l":
		if m.keepHours < 10 {
			m.keepHours++
		}
	case "enter":
		until := time.Now().UTC().Add(time.Duration(m.keepHours) * time.Hour).Format(time.RFC3339)
		_ = m.store.UpdateWorkspace(m.keepTeam, func(w *store.Workspace) {
			w.KeepOnlineUntil = until
			w.Paused = false
		})
		_ = daemon.Reload()
		m.reload()
		m.screen = screenList
		m.info = fmt.Sprintf("staying online %d hours", m.keepHours)
	}
	return m, nil
}

func (m model) keyDelete(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "n":
		m.screen = screenList
	case "y", "enter":
		m.screen = screenList
		if err := m.store.RemoveWorkspace(m.delTeam); err != nil {
			m.err = "could not remove " + m.delName + ": " + err.Error()
			m.reload()
			return m, nil
		}
		_ = daemon.Reload()
		m.reload()
		m.err = ""
		m.info = "removed " + m.delName
	}
	return m, nil
}

func (m *model) reload() {
	list, _ := m.store.Workspaces()
	m.workspaces = list
	if m.sel >= len(m.workspaces) {
		m.sel = max(len(m.workspaces)-1, 0)
	}
	st, _ := daemon.ReadStatus()
	m.daemon = st
}

func (m model) selected() (store.Workspace, bool) {
	if len(m.workspaces) == 0 || m.sel < 0 || m.sel >= len(m.workspaces) {
		return store.Workspace{}, false
	}
	return m.workspaces[m.sel], true
}

var (
	titleStyle = lipgloss.NewStyle().Bold(true)
	okStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("10"))
	warnStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("11"))
	errStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
	dimStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	selStyle   = lipgloss.NewStyle().Background(lipgloss.Color("236"))
	cyanStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("14"))
)

func (m model) View() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render("always-green") + "  " + m.statusBar() + "\n\n")
	switch m.screen {
	case screenAdd:
		b.WriteString(m.viewAdd())
	case screenImport:
		b.WriteString(m.viewImport())
	case screenSchedule:
		b.WriteString(m.viewSchedule())
	case screenKeep:
		b.WriteString(fmt.Sprintf("Stay online for %s?\n\n  %s  %d hours  %s\n\nEnter confirm   Esc cancel\n",
			m.keepName, dimStyle.Render("<"), m.keepHours, dimStyle.Render(">")))
	case screenDelete:
		b.WriteString(fmt.Sprintf("Remove workspace %q?\n\ny / Enter confirm   n / Esc cancel\n", m.delName))
	default:
		b.WriteString(m.viewList())
	}
	if m.err != "" {
		b.WriteString("\n" + errStyle.Render(m.err) + "\n")
	}
	if m.info != "" && m.screen == screenList {
		b.WriteString("\n" + dimStyle.Render(m.info) + "\n")
	}
	return b.String()
}

func (m model) statusBar() string {
	var parts []string
	if daemon.Running() && m.daemon.Running {
		n := 0
		for _, ws := range m.daemon.Workspaces {
			if ws.Connected {
				n++
			}
		}
		parts = append(parts, okStyle.Render(fmt.Sprintf("daemon %d/%d", n, len(m.workspaces))))
	} else {
		parts = append(parts, errStyle.Render("daemon down"))
	}
	now := time.Now()
	if loc, err := time.LoadLocation(m.tz); err == nil {
		now = now.In(loc)
	}
	parts = append(parts, dimStyle.Render(m.tz+" "+now.Format("15:04")))
	return strings.Join(parts, "   ")
}

func (m model) viewList() string {
	if len(m.workspaces) == 0 {
		return "No workspaces yet\n\n" + cyanStyle.Render("[a]") + " import from the Slack app\n\n" +
			dimStyle.Render("q quit   a import")
	}
	var b strings.Builder
	now := time.Now()
	daemonBy := map[string]daemon.WorkspaceStatus{}
	for _, d := range m.daemon.Workspaces {
		daemonBy[d.TeamID] = d
	}
	for i, ws := range m.workspaces {
		line := m.renderWS(ws, daemonBy[ws.TeamID], now, i == m.sel)
		if i == m.sel {
			b.WriteString(selStyle.Render(line) + "\n")
		} else {
			b.WriteString(line + "\n")
		}
		b.WriteString("\n")
	}
	b.WriteString(dimStyle.Render("j/k move   a import   p pause   c schedule   Enter stay online   d delete   r refresh   q quit"))
	return b.String()
}

func (m model) renderWS(ws store.Workspace, dws daemon.WorkspaceStatus, now time.Time, selected bool) string {
	prefix := "  "
	if selected {
		prefix = "> "
	}
	badge, badgeColor := badgeOf(ws, dws, now, m.tz)
	head := prefix + titleStyle.Render(ws.Name) + "  " + badgeColor.Render("["+badge+"]")
	status, statusColor := statusOf(ws, dws, m.presence[ws.TeamID], now, m.tz)
	lines := []string{head, "  " + statusColor.Render(status)}
	if ws.UserInfo != nil && (ws.UserInfo.RealName != "" || ws.UserInfo.Email != "") {
		u := ws.UserInfo.RealName
		if ws.UserInfo.Email != "" {
			u += " (" + ws.UserInfo.Email + ")"
		}
		lines = append(lines, "  "+dimStyle.Render(u))
	}
	pres := m.presence[ws.TeamID]
	dot := dimStyle.Render("o unknown")
	if pres.Presence == "active" {
		dot = okStyle.Render("● active")
	} else if pres.Presence == "away" {
		dot = warnStyle.Render("● away")
	}
	last := ""
	if pres.LastActivity > 0 {
		last = "  last " + time.Unix(pres.LastActivity, 0).Local().Format("3:04 PM")
	}
	lines = append(lines, fmt.Sprintf("  %s  %s%d conn%s", dimStyle.Render("status"), dot, pres.ConnectionCount, dimStyle.Render(last)))
	lines = append(lines, "  "+dimStyle.Render(schedule.Format(ws.Schedule)))
	if selected {
		action := "Pause"
		if ws.Paused {
			action = "Resume"
		}
		lines = append(lines, "  "+cyanStyle.Render("[p] "+action+"   [c] Schedule   [Enter] Stay online   [d] Delete"))
	}
	return strings.Join(lines, "\n")
}

func badgeOf(ws store.Workspace, dws daemon.WorkspaceStatus, now time.Time, tz string) (string, lipgloss.Style) {
	if ws.TokenInvalid || (!dws.TokenValid && dws.TeamID != "") {
		return "Expired", errStyle
	}
	if ws.KeepOnlineActive(now) {
		return "Stay online", cyanStyle
	}
	if ws.Paused {
		return "Paused", warnStyle
	}
	if !schedule.InWindow(ws.Schedule, now, tz) {
		return "Outside hours", dimStyle
	}
	return "Active", okStyle
}

func statusOf(ws store.Workspace, dws daemon.WorkspaceStatus, p slackx.Presence, now time.Time, tz string) (string, lipgloss.Style) {
	if ws.TokenInvalid || p.Presence == "invalid" || (!dws.TokenValid && dws.TeamID != "") {
		return "tokens expired, quit and run: always-green reauth", errStyle
	}
	if ws.KeepOnlineActive(now) {
		until, _ := time.Parse(time.RFC3339, ws.KeepOnlineUntil)
		if p.Presence == "away" && p.ManualAway {
			return "you set yourself Away in Slack", warnStyle
		}
		if p.Presence == "away" {
			return "showing away, reconnecting", warnStyle
		}
		return "staying online until " + until.Local().Format("3:04 PM"), okStyle
	}
	if ws.Paused {
		return "paused", warnStyle
	}
	if !schedule.InWindow(ws.Schedule, now, tz) {
		return "outside scheduled hours", dimStyle
	}
	if !dws.Connected {
		return "connecting to Slack", warnStyle
	}
	if p.Presence == "away" && p.ManualAway {
		return "you set yourself Away in Slack", warnStyle
	}
	if p.Presence == "away" {
		return "showing away, reconnecting", warnStyle
	}
	if p.Presence == "active" {
		return "keeping you green", okStyle
	}
	return "checking status", dimStyle
}

func (m model) viewImport() string {
	if m.saving {
		return titleStyle.Render("Saving workspaces...") + "\n\n" + dimStyle.Render("checking the tokens with Slack")
	}
	if m.importing {
		return titleStyle.Render("Reading Slack desktop app...") + "\n\n" +
			dimStyle.Render("macOS may ask for Keychain access to Slack Safe Storage") + "\n" +
			dimStyle.Render("Esc cancel")
	}
	var b strings.Builder
	b.WriteString(titleStyle.Render("Workspaces in the Slack app") + "\n")
	b.WriteString(dimStyle.Render("Space toggle   Enter import   m paste tokens   Esc cancel") + "\n\n")
	for i, f := range m.impFound {
		mark := " "
		if m.impPick[i] {
			mark = "x"
		}
		name := f.Name
		if name == "" {
			name = f.TeamID
		}
		if name == "" {
			name = "workspace"
		}
		line := fmt.Sprintf(" [%s] %s", mark, name)
		if i == m.impSel {
			b.WriteString(selStyle.Render(line) + "\n")
		} else {
			b.WriteString(line + "\n")
		}
	}
	return b.String()
}

func (m model) viewAdd() string {
	return strings.Join([]string{
		titleStyle.Render("Add a Slack workspace"),
		dimStyle.Render("Chrome: app.slack.com Console, always-green snippet, then copy cookie d"),
		"",
		"Name (optional)",
		m.nameIn.View(),
		"",
		"xoxc",
		m.xoxcIn.View(),
		"xoxd (cookie d)",
		m.xoxdIn.View(),
		"",
		m.addFooter(),
	}, "\n")
}

func (m model) addFooter() string {
	if m.saving {
		return dimStyle.Render("Checking the tokens with Slack...")
	}
	return "Tab fields   Enter save   Esc cancel"
}

func (m model) viewSchedule() string {
	var days []string
	for i, id := range schedule.DayIDs {
		mark := " "
		if m.schedDays[id] {
			mark = "x"
		}
		days = append(days, fmt.Sprintf("%d[%s]%s", i+1, mark, schedule.DayShort[id]))
	}
	return strings.Join([]string{
		titleStyle.Render("Schedule: " + m.schedName),
		dimStyle.Render("timezone " + m.tz + "   w weekdays   e every day   1-7 toggle   Ctrl+a always on"),
		"",
		strings.Join(days, "  "),
		"",
		"Start  " + m.startIn.View(),
		"End    " + m.endIn.View(),
		"",
		"Left/Right nudge 30m   Enter save   Esc skip",
	}, "\n")
}
