package tui

import (
	"context"
	"fmt"
	"image/color"
	"strings"
	"time"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/huggon1/localhost-manager/internal/app"
)

var (
	background = lipgloss.Color("#0D1117")
	surface    = lipgloss.Color("#111820")
	surfaceAlt = lipgloss.Color("#151D26")
	selectedBG = lipgloss.Color("#182632")
	panel      = lipgloss.Color("#26313D")
	text       = lipgloss.Color("#E6EDF3")
	muted      = lipgloss.Color("#7D8590")
	subtle     = lipgloss.Color("#58616D")
	accent     = lipgloss.Color("#67D4E8")
	green      = lipgloss.Color("#3DDC97")
	yellow     = lipgloss.Color("#F2C94C")
	red        = lipgloss.Color("#FF6B7A")
)

type statesMsg struct {
	states []app.ProjectState
	err    error
}

type actionMsg struct {
	label string
	err   error
}

type tickMsg time.Time

type Model struct {
	manager           *app.Manager
	states            []app.ProjectState
	cursor            int
	width             int
	height            int
	query             string
	search            bool
	reorderMode       bool
	logMode           bool
	busy              string
	message           string
	err               error
	lastLoad          time.Time
	confirmRemoveID   string
	confirmRemoveName string
}

func New(manager *app.Manager) *Model {
	return &Model{manager: manager, width: 100, height: 30}
}

func (m *Model) Init() tea.Cmd {
	return tea.Batch(m.load(), tick())
}

func tick() tea.Cmd {
	return tea.Tick(2*time.Second, func(t time.Time) tea.Msg { return tickMsg(t) })
}

func (m *Model) load() tea.Cmd {
	return func() tea.Msg {
		states, err := m.manager.Projects(context.Background())
		return statesMsg{states: states, err: err}
	}
}

func (m *Model) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := message.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
	case tickMsg:
		return m, tea.Batch(m.load(), tick())
	case statesMsg:
		if msg.err != nil {
			m.err = msg.err
			return m, nil
		}
		selectedID := m.selectedID()
		m.states = msg.states
		m.lastLoad = time.Now()
		m.err = nil
		m.restoreCursor(selectedID)
	case actionMsg:
		m.busy = ""
		m.err = msg.err
		if msg.err == nil {
			m.message = msg.label
		}
		return m, m.load()
	case tea.KeyPressMsg:
		return m.handleKey(msg)
	}
	return m, nil
}

func (m *Model) handleKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	key := msg.String()
	if m.confirmRemoveID != "" {
		switch key {
		case "y", "enter":
			id, name := m.confirmRemoveID, m.confirmRemoveName
			m.confirmRemoveID, m.confirmRemoveName = "", ""
			return m, m.action("Removing "+name, func() error {
				return m.manager.Remove(context.Background(), id)
			})
		case "n", "esc", "d", "delete":
			m.confirmRemoveID, m.confirmRemoveName = "", ""
		}
		return m, nil
	}
	if m.reorderMode {
		switch key {
		case "up", "k":
			return m.moveSelected(-1)
		case "down", "j":
			return m.moveSelected(1)
		case " ", "space", "enter", "esc":
			m.reorderMode = false
			m.message = "Project order saved"
		}
		return m, nil
	}
	if m.search {
		switch key {
		case "esc":
			m.search, m.query = false, ""
		case "enter":
			m.search = false
		case "backspace":
			if m.query != "" {
				_, size := utf8.DecodeLastRuneInString(m.query)
				m.query = m.query[:len(m.query)-size]
			}
		default:
			if entered := tea.Key(msg).Text; entered != "" && !strings.ContainsAny(entered, "\r\n") {
				m.query += entered
			}
		}
		m.cursor = 0
		return m, nil
	}

	switch key {
	case "q", "ctrl+c":
		return m, tea.Quit
	case "/":
		m.search = true
	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
		}
	case "down", "j":
		if m.cursor < len(m.filtered())-1 {
			m.cursor++
		}
	case " ", "space":
		if _, ok := m.selected(); ok {
			m.reorderMode = true
			m.message = "Reorder mode"
			m.err = nil
		}
	case "l":
		m.logMode = !m.logMode
	case "d", "delete":
		if state, ok := m.selected(); ok {
			m.confirmRemoveID = state.Project.ID
			m.confirmRemoveName = state.Project.Name
			m.message = ""
			m.err = nil
		}
	case "o":
		if state, ok := m.selected(); ok && state.URL != "" {
			return m, func() tea.Msg { return actionMsg{label: "Opened " + state.URL, err: app.OpenBrowser(state.URL)} }
		}
	case "enter":
		if state, ok := m.selected(); ok {
			if state.Status == app.StatusRunning {
				return m, func() tea.Msg { return actionMsg{label: "Opened " + state.URL, err: app.OpenBrowser(state.URL)} }
			}
			return m, m.action("Starting "+state.Project.Name, func() error {
				_, err := m.manager.Start(context.Background(), state.Project.ID)
				return err
			})
		}
	case "x":
		if state, ok := m.selected(); ok && state.Status != app.StatusStopped {
			return m, m.action("Stopping "+state.Project.Name, func() error {
				return m.manager.Stop(context.Background(), state.Project.ID)
			})
		}
	case "r":
		if state, ok := m.selected(); ok {
			return m, m.action("Restarting "+state.Project.Name, func() error {
				_, err := m.manager.Restart(context.Background(), state.Project.ID)
				return err
			})
		}
	}
	return m, nil
}

func (m *Model) moveSelected(direction int) (tea.Model, tea.Cmd) {
	state, ok := m.selected()
	if !ok || m.busy != "" {
		return m, nil
	}
	label := "up"
	boundary := "Already at the top"
	if direction > 0 {
		label = "down"
		boundary = "Already at the bottom"
	}
	m.busy = "Moving " + state.Project.Name + " " + label
	m.message = ""
	m.err = nil
	return m, func() tea.Msg {
		moved, err := m.manager.Move(context.Background(), state.Project.ID, direction)
		if err != nil {
			return actionMsg{err: err}
		}
		if !moved {
			return actionMsg{label: boundary}
		}
		return actionMsg{label: "Moved " + state.Project.Name + " " + label}
	}
}

func (m *Model) action(label string, run func() error) tea.Cmd {
	if m.busy != "" {
		return nil
	}
	m.busy = label
	m.message = ""
	m.err = nil
	return func() tea.Msg { return actionMsg{label: label + " complete", err: run()} }
}

func (m *Model) selectedID() string {
	if state, ok := m.selected(); ok {
		return state.Project.ID
	}
	return ""
}

func (m *Model) restoreCursor(id string) {
	states := m.filtered()
	for index := range states {
		if states[index].Project.ID == id {
			m.cursor = index
			return
		}
	}
	if len(states) == 0 {
		m.cursor = 0
	} else if m.cursor >= len(states) {
		m.cursor = len(states) - 1
	}
}

func (m *Model) filtered() []app.ProjectState {
	if m.query == "" {
		return m.states
	}
	needle := strings.ToLower(m.query)
	result := make([]app.ProjectState, 0, len(m.states))
	for _, state := range m.states {
		if strings.Contains(strings.ToLower(state.Project.Name), needle) || strings.Contains(strings.ToLower(state.Project.Path), needle) {
			result = append(result, state)
		}
	}
	return result
}

func (m *Model) selected() (app.ProjectState, bool) {
	states := m.filtered()
	if len(states) == 0 || m.cursor < 0 || m.cursor >= len(states) {
		return app.ProjectState{}, false
	}
	return states[m.cursor], true
}

func (m *Model) View() tea.View {
	content := m.render()
	view := tea.NewView(content)
	view.AltScreen = true
	view.WindowTitle = "localhost-manager"
	return view
}

func (m *Model) render() string {
	w := max(m.width, 50)
	h := max(m.height, 16)
	header := m.renderHeader(w)
	footer := m.renderFooter(w)
	bodyHeight := max(8, h-lipgloss.Height(header)-lipgloss.Height(footer))
	var body string
	if w < 88 {
		body = m.renderCompact(w, bodyHeight)
	} else {
		leftWidth := max(27, min(38, w*30/100))
		rightWidth := max(36, w-leftWidth)
		left := m.renderList(leftWidth, bodyHeight)
		right := m.renderDetail(rightWidth, bodyHeight)
		body = lipgloss.JoinHorizontal(lipgloss.Top, left, right)
	}
	return lipgloss.NewStyle().Width(w).Height(h).MaxWidth(w).MaxHeight(h).Background(background).Foreground(text).Render(header + body + footer)
}

func (m *Model) renderHeader(width int) string {
	running := 0
	for _, state := range m.states {
		if state.Status == app.StatusRunning {
			running++
		}
	}
	brand := lipgloss.NewStyle().Bold(true).Foreground(text).Render("LOCALHOST-MANAGER")
	tagline := lipgloss.NewStyle().Foreground(muted).Render("Local projects")
	summary := lipgloss.NewStyle().Foreground(muted).Render(fmt.Sprintf("%d projects  ·  %d running", len(m.states), running))
	search := "/ Search"
	if m.search || m.query != "" {
		search = "/ " + m.query + "▌"
	}
	right := lipgloss.NewStyle().Foreground(accent).Render(search)
	left := brand + "  " + tagline
	right = summary + "    " + right
	gap := max(2, width-lipgloss.Width(left)-lipgloss.Width(right)-4)
	line := "  " + left + strings.Repeat(" ", gap) + right
	if lipgloss.Width(line) > width {
		line = "  " + brand + strings.Repeat(" ", max(2, width-lipgloss.Width(brand)-lipgloss.Width(right)-4)) + right
	}
	rule := lipgloss.NewStyle().Foreground(panel).Render(strings.Repeat("─", width))
	return "\n" + clipStyled(line, width) + "\n" + rule
}

func (m *Model) renderList(width, height int) string {
	style := lipgloss.NewStyle().Width(max(1, width-1)).Height(max(1, height)).BorderRight(true).BorderStyle(lipgloss.NormalBorder()).BorderForeground(panel).Padding(0, 1)
	states := m.filtered()
	title := sectionTitle("Projects", fmt.Sprintf("%d", len(states)))
	rows := []string{" " + title, ""}
	if len(states) == 0 {
		rows = append(rows, lipgloss.NewStyle().Foreground(muted).Render("No matching projects"))
	}
	maxRows := max(1, height-2)
	start := 0
	if m.cursor >= maxRows {
		start = m.cursor - maxRows + 1
	}
	end := min(len(states), start+maxRows)
	for i := start; i < end; i++ {
		rows = append(rows, renderProjectRow(states[i], i == m.cursor, m.reorderMode && i == m.cursor, max(8, width-4)))
	}
	return style.Render(strings.Join(rows, "\n"))
}

func (m *Model) renderDetail(width, height int) string {
	style := lipgloss.NewStyle().Width(max(1, width-3)).Height(max(1, height)).Padding(0, 2)
	state, ok := m.selected()
	if !ok {
		empty := lipgloss.NewStyle().Foreground(muted).Align(lipgloss.Center).Width(max(1, width-7)).PaddingTop(max(1, height/3)).Render("No projects yet\n\nAsk your agent to register a runnable project.")
		return style.Render(empty)
	}
	contentWidth := max(12, width-7)
	name := lipgloss.NewStyle().Bold(true).Foreground(text).Render(clip(state.Project.Name, max(8, contentWidth-24)))
	status := statusPill(state.Status)
	open := ""
	if state.URL != "" {
		open = lipgloss.NewStyle().Foreground(accent).Render("o  Open ↗")
	}
	gap := max(2, contentWidth-lipgloss.Width(name)-lipgloss.Width(status)-lipgloss.Width(open)-4)
	titleLine := name + "  " + status + strings.Repeat(" ", gap) + open
	sections := []string{clipStyled(titleLine, contentWidth)}
	displayURL := state.URL
	if displayURL == "" {
		displayURL = state.Project.URLTemplate
	}
	sections = append(sections, lipgloss.NewStyle().Foreground(accent).Render(clip(displayURL, contentWidth)))

	if m.logMode {
		sections = append(sections, "", m.renderLogPanel(state, contentWidth, max(4, height-4)))
	} else {
		metadata := field("Command", strings.Join(state.Project.Command, " "), contentWidth) + "\n\n" + field("Directory", state.Project.Path, contentWidth)
		logsHeight := max(4, height-lipgloss.Height(metadata)-6)
		sections = append(sections, "", metadata, "", m.renderLogPanel(state, contentWidth, logsHeight))
	}
	return style.Render(strings.Join(sections, "\n"))
}

func (m *Model) renderCompact(width, height int) string {
	// The detail pane needs fourteen lines for its title, metadata, and log panel.
	// Reserve those lines first so the footer remains visible in an 80×24 terminal.
	listHeight := max(5, min(9, height-14))
	return m.renderList(width, listHeight) + m.renderDetail(width, max(5, height-listHeight))
}

func (m *Model) renderLogs(state app.ProjectState, width, height int) string {
	logs, err := m.manager.Logs(context.Background(), state.Project.ID, max(3, height))
	if err != nil {
		return lipgloss.NewStyle().Foreground(red).Render(err.Error())
	}
	lines := strings.Split(logs, "\n")
	if len(lines) > height {
		lines = lines[len(lines)-height:]
	}
	for i := range lines {
		lines[i] = clip(stripControl(lines[i]), width)
	}
	return lipgloss.NewStyle().Foreground(lipgloss.Color("#A1A1AA")).Render(strings.Join(lines, "\n"))
}

func (m *Model) renderFooter(width int) string {
	if m.confirmRemoveID != "" {
		question := lipgloss.NewStyle().Bold(true).Foreground(red).Render("Remove " + clip(m.confirmRemoveName, max(8, width/3)) + "?")
		note := lipgloss.NewStyle().Foreground(muted).Render("A running project will be stopped first.")
		actions := keycap("y") + " " + lipgloss.NewStyle().Foreground(text).Render("Confirm") + "    " + keycap("Esc") + " " + lipgloss.NewStyle().Foreground(text).Render("Cancel")
		line := "  " + question + "  " + note + "    " + actions
		if lipgloss.Width(line) > width {
			line = "  " + question + "    " + actions
		}
		return footerStyle(width).Render(clipStyled(line, width-2))
	}

	status := ""
	if m.busy != "" {
		frames := []string{"◐", "◓", "◑", "◒"}
		status = lipgloss.NewStyle().Foreground(yellow).Render(frames[time.Now().UnixMilli()/250%4] + " " + m.busy)
	} else if m.err != nil {
		status = lipgloss.NewStyle().Foreground(red).Render("× " + clip(m.err.Error(), width/2))
	} else if m.message != "" {
		status = lipgloss.NewStyle().Foreground(green).Render("✓ " + m.message)
	}
	if m.reorderMode {
		help := lipgloss.NewStyle().Bold(true).Foreground(accent).Render("Reorder mode") + "    " + keycap("↑↓/jk") + " " + lipgloss.NewStyle().Foreground(text).Render("Move project") + "    " + keycap("Space/Enter") + " " + lipgloss.NewStyle().Foreground(text).Render("Done") + "    " + keycap("Esc") + " " + lipgloss.NewStyle().Foreground(text).Render("Exit")
		if status != "" && m.busy != "" {
			help = status + "\n" + help
		}
		return footerStyle(width).Render(clipStyled(help, width-2))
	}
	help := renderHelp(width)
	if status != "" {
		help = status + "\n" + help
	}
	return footerStyle(width).Render(help)
}

func footerStyle(width int) lipgloss.Style {
	return lipgloss.NewStyle().Width(max(1, width-2)).BorderTop(true).BorderStyle(lipgloss.NormalBorder()).BorderForeground(panel).Padding(0, 1)
}

func renderHelp(width int) string {
	items := []struct{ key, action string }{
		{"↑↓/jk", "Move"}, {"↵", "Open/Start"}, {"Space", "Reorder"}, {"d", "Remove"}, {"l", "Logs"}, {"/", "Search"}, {"q", "Quit"},
	}
	separator := "   "
	if width < 100 {
		items = []struct{ key, action string }{{"↑↓/jk", "Move"}, {"↵", "Open"}, {"Space", "Order"}, {"d", "Remove"}, {"q", "Quit"}}
	}
	if width < 65 {
		items = []struct{ key, action string }{{"↵", "Open"}, {"Space", "Order"}, {"d", "Remove"}, {"q", "Quit"}}
		separator = "  "
	}
	parts := make([]string, 0, len(items))
	for _, item := range items {
		parts = append(parts, keycap(item.key)+" "+lipgloss.NewStyle().Foreground(muted).Render(item.action))
	}
	return strings.Join(parts, separator)
}

func keycap(value string) string {
	return lipgloss.NewStyle().Foreground(text).Background(surfaceAlt).Padding(0, 1).Render(value)
}

func renderProjectRow(state app.ProjectState, active, moving bool, width int) string {
	meta := stateMeta(state)
	nameWidth := max(6, width-lipgloss.Width(meta)-7)
	name := clip(state.Project.Name, nameWidth)
	line := "  " + statusDot(state.Status) + "  " + padRight(name, nameWidth) + "  " + lipgloss.NewStyle().Foreground(muted).Render(meta)
	style := lipgloss.NewStyle().Width(width).Foreground(text)
	if active {
		marker := "▎"
		if moving {
			marker = "↕"
		}
		line = lipgloss.NewStyle().Bold(moving).Foreground(accent).Render(marker) + " " + statusDot(state.Status) + "  " + lipgloss.NewStyle().Bold(true).Foreground(text).Render(padRight(name, nameWidth)) + "  " + lipgloss.NewStyle().Foreground(muted).Render(meta)
		style = style.Background(selectedBG)
	}
	return style.Render(line)
}

func (m *Model) renderLogPanel(state app.ProjectState, width, height int) string {
	title := sectionTitle("Logs", logBadge(state.Status))
	logs := m.renderLogs(state, max(1, width-4), max(1, height-2))
	return lipgloss.NewStyle().Width(max(1, width)).Height(max(3, height)).Background(surface).Padding(0, 2).Render(title + "\n\n" + logs)
}

func sectionTitle(left, right string) string {
	return lipgloss.NewStyle().Bold(true).Foreground(text).Render(left) + "  " + lipgloss.NewStyle().Foreground(muted).Render(right)
}

func field(name, value string, width int) string {
	return lipgloss.NewStyle().Foreground(muted).Render(name) + "\n" + lipgloss.NewStyle().Foreground(text).Render(clip(value, width))
}

func statusPill(status app.Status) string {
	return lipgloss.NewStyle().Foreground(statusColor(status)).Background(surfaceAlt).Padding(0, 1).Render("● " + titleStatus(status))
}

func titleStatus(status app.Status) string {
	value := string(status)
	if value == "" {
		return "Unknown"
	}
	return strings.ToUpper(value[:1]) + value[1:]
}

func logBadge(status app.Status) string {
	if status == app.StatusRunning {
		return "Live"
	}
	return titleStatus(status)
}

func statusDot(status app.Status) string {
	return lipgloss.NewStyle().Foreground(statusColor(status)).Render(map[app.Status]string{
		app.StatusRunning: "●", app.StatusStarting: "◐", app.StatusUnhealthy: "!", app.StatusCrashed: "×", app.StatusStopped: "○",
	}[status])
}

func statusColor(status app.Status) color.Color {
	switch status {
	case app.StatusRunning:
		return green
	case app.StatusStarting:
		return yellow
	case app.StatusUnhealthy:
		return yellow
	case app.StatusCrashed:
		return red
	default:
		return muted
	}
}

func stateMeta(state app.ProjectState) string {
	if state.Run != nil && state.Run.Port > 0 {
		return fmt.Sprintf(":%d", state.Run.Port)
	}
	return titleStatus(state.Status)
}

func padRight(value string, width int) string {
	return value + strings.Repeat(" ", max(0, width-utf8.RuneCountInString(value)))
}

func clipStyled(value string, width int) string {
	if lipgloss.Width(value) <= width {
		return value
	}
	return lipgloss.NewStyle().MaxWidth(max(1, width)).Render(value)
}

func clip(value string, width int) string {
	if width <= 0 {
		return ""
	}
	runes := []rune(value)
	if len(runes) <= width {
		return value
	}
	if width == 1 {
		return "…"
	}
	return string(runes[:width-1]) + "…"
}

func stripControl(value string) string {
	return strings.Map(func(r rune) rune {
		if r == '\t' || r >= 32 {
			return r
		}
		return -1
	}, value)
}

func Run(manager *app.Manager) error {
	_, err := tea.NewProgram(New(manager)).Run()
	return err
}
