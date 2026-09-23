package ui

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/guilhermepantoja789/docker-tui/internal/session"
	"github.com/guilhermepantoja789/docker-tui/internal/ui/styles"
)

const (
	filterPresetError = `(error|err\b|fatal|panic)`
	filterPresetWarn  = `(warn|warning)`
)

type logUpdateMsg struct{}

type paneFilter struct {
	query    string
	re       *regexp.Regexp
	invalid  bool
	caseSens bool
	invert   bool
}

type paneGeom struct {
	w, h int
}

type logViewState struct {
	vps       [session.MaxPanes]viewport.Model
	stick     [session.MaxPanes]bool // stick to bottom while following
	filters   [session.MaxPanes]paneFilter
	inited    bool
	logFocus  bool // when true, keys go to log panes
	filtering bool // filter input mode for focused pane
	filterIn  textinput.Model
}

func (m Model) waitLogs() tea.Cmd {
	if m.logs == nil {
		return nil
	}
	return func() tea.Msg {
		_, ok := <-m.logs.WaitNotify()
		if !ok {
			return nil
		}
		return logUpdateMsg{}
	}
}

func (m Model) openLogs() (tea.Model, tea.Cmd) {
	if m.tab != tabContainers {
		return m, nil
	}
	id, name, ok := m.selectedID()
	if !ok {
		return m, nil
	}
	return m.openLogsFor(id, name)
}

func (m Model) openLogsFor(id, name string) (tea.Model, tea.Cmd) {
	if m.logs == nil {
		m.statusErr = fmt.Errorf("log hub not ready")
		return m, nil
	}
	if m.handle.Client() == nil {
		m.statusErr = fmt.Errorf("no docker client")
		return m, nil
	}

	already := false
	for _, p := range m.logs.Snapshot().Panes {
		if p.Active && p.ContainerID == id {
			already = true
			break
		}
	}

	if err := m.logs.Open(context.Background(), id, name); err != nil {
		m.statusErr = err
		return m, nil
	}
	m.logView.logFocus = true
	if !already {
		focus := m.logs.Focus()
		m.logView.filters[focus] = paneFilter{}
		m.logView.stick[focus] = true
	}
	m.ensureLogViewports()
	m.syncLogViewports()
	m.statusMsg = fmt.Sprintf("logs %s (%d panes)", name, m.logs.Count())
	m.statusErr = nil
	m.updateViewport()
	return m, m.waitLogs()
}

func (m *Model) ensureLogFilterInput() {
	if m.logView.filterIn.Width > 0 {
		return
	}
	ti := textinput.New()
	ti.Placeholder = "log filter (regex)…"
	ti.CharLimit = 128
	ti.Width = 40
	m.logView.filterIn = ti
}

func (m *Model) ensureLogViewports() {
	geoms := m.logGeoms()
	for i := 0; i < session.MaxPanes; i++ {
		w, h := geoms[i].w, geoms[i].h
		if w < 1 {
			w = 20
		}
		if h < 1 {
			h = 5
		}
		if !m.logView.inited {
			m.logView.vps[i] = viewport.New(w, h)
			m.logView.stick[i] = true
		} else {
			m.logView.vps[i].Width = w
			m.logView.vps[i].Height = h
		}
	}
	m.logView.inited = true
}

func (m Model) activePaneIndices() []int {
	if m.logs == nil {
		return nil
	}
	snap := m.logs.Snapshot()
	var out []int
	for i := 0; i < session.MaxPanes; i++ {
		if snap.Panes[i].Active {
			out = append(out, i)
		}
	}
	return out
}

func (m Model) logGridDims(n int) (rows, cols int) {
	switch {
	case n <= 1:
		return 1, 1
	case n == 2:
		return 1, 2
	default:
		return 2, 2
	}
}

func (m Model) logGeoms() [session.MaxPanes]paneGeom {
	var geoms [session.MaxPanes]paneGeom
	active := m.activePaneIndices()
	n := len(active)
	if n == 0 {
		return geoms
	}
	gap := 1
	totalW := max(20, m.width-2)
	bodyH := m.logBodyHeight()
	rows, cols := m.logGridDims(n)

	cellW := (totalW - gap*(cols-1)) / cols
	if cellW < 20 {
		cellW = 20
	}
	cellH := (bodyH - gap*(rows-1)) / rows
	if cellH < 3 {
		cellH = 3
	}

	switch n {
	case 1:
		geoms[active[0]] = paneGeom{totalW, bodyH}
	case 2:
		geoms[active[0]] = paneGeom{cellW, bodyH}
		geoms[active[1]] = paneGeom{cellW, bodyH}
	case 3:
		geoms[active[0]] = paneGeom{cellW, cellH}
		geoms[active[1]] = paneGeom{cellW, cellH}
		geoms[active[2]] = paneGeom{totalW, cellH} // bottom spans full width
	default: // 4
		for i := 0; i < 4 && i < n; i++ {
			geoms[active[i]] = paneGeom{cellW, cellH}
		}
	}
	return geoms
}

func (m Model) logBodyHeight() int {
	body := max(10, m.height-8)
	n := 0
	if m.logs != nil {
		n = m.logs.Count()
	}
	rows, _ := m.logGridDims(n)
	if rows >= 2 {
		// ~60% of body so 2×2 cells stay usable.
		return max(8, (body*3)/5)
	}
	return max(5, body/2)
}

func (m Model) listBodyHeight() int {
	if m.logs == nil || !m.logs.Active() {
		return max(1, m.height-8)
	}
	body := max(10, m.height-8)
	return max(3, body-m.logBodyHeight()-1)
}

func (f *paneFilter) compile() {
	q := strings.TrimSpace(f.query)
	if q == "" {
		f.re = nil
		f.invalid = false
		return
	}
	pat := q
	if !f.caseSens {
		pat = "(?i)" + q
	}
	re, err := regexp.Compile(pat)
	if err != nil {
		f.re = nil
		f.invalid = true
		return
	}
	f.re = re
	f.invalid = false
}

// filterLines applies a pane filter. On empty/invalid filter, all lines pass.
// matched is the number of lines kept; total is len(lines).
func filterLines(lines []string, f paneFilter) (out []string, matched, total int) {
	total = len(lines)
	if strings.TrimSpace(f.query) == "" || f.invalid || f.re == nil {
		return lines, total, total
	}
	out = make([]string, 0, len(lines))
	for _, line := range lines {
		ok := f.re.MatchString(line)
		if f.invert {
			ok = !ok
		}
		if ok {
			out = append(out, line)
		}
	}
	return out, len(out), total
}

func (m *Model) syncLogViewports() {
	if m.logs == nil {
		return
	}
	m.ensureLogViewports()
	snap := m.logs.Snapshot()
	for i := 0; i < session.MaxPanes; i++ {
		p := snap.Panes[i]
		if !p.Active {
			m.logView.vps[i].SetContent("")
			m.logView.filters[i] = paneFilter{}
			continue
		}
		lines, _, _ := filterLines(p.Lines, m.logView.filters[i])
		var b strings.Builder
		for _, line := range lines {
			b.WriteString(line)
			b.WriteByte('\n')
		}
		if p.Err != nil {
			b.WriteString("\nerror: ")
			b.WriteString(p.Err.Error())
			b.WriteByte('\n')
		}
		wasAtBottom := m.logView.vps[i].AtBottom()
		m.logView.vps[i].SetContent(b.String())
		if m.logView.stick[i] || (p.Follow && wasAtBottom) {
			m.logView.vps[i].GotoBottom()
		}
	}
}

func (m Model) handleLogFilterKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	focus := m.logs.Focus()
	switch msg.String() {
	case "esc":
		m.logView.filters[focus] = paneFilter{}
		m.logView.filtering = false
		m.logView.filterIn.Blur()
		m.logView.filterIn.SetValue("")
		m.syncLogViewports()
		m.statusMsg = "log filter cleared"
		return m, nil
	case "enter":
		f := m.logView.filters[focus]
		f.query = m.logView.filterIn.Value()
		f.compile()
		m.logView.filters[focus] = f
		m.logView.filtering = false
		m.logView.filterIn.Blur()
		m.syncLogViewports()
		return m, nil
	}
	var cmd tea.Cmd
	m.logView.filterIn, cmd = m.logView.filterIn.Update(msg)
	f := m.logView.filters[focus]
	f.query = m.logView.filterIn.Value()
	f.compile()
	m.logView.filters[focus] = f
	m.syncLogViewports()
	return m, cmd
}

func (m *Model) applyLogFilterPreset(preset string) {
	focus := m.logs.Focus()
	f := m.logView.filters[focus]
	f.query = preset
	f.compile()
	m.logView.filters[focus] = f
	m.logView.filterIn.SetValue(preset)
	m.syncLogViewports()
}

func (m *Model) recompileFocusedFilter() {
	focus := m.logs.Focus()
	f := m.logView.filters[focus]
	f.compile()
	m.logView.filters[focus] = f
	m.syncLogViewports()
}

func (m Model) handleLogsKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.logView.filtering {
		return m.handleLogFilterKey(msg)
	}

	switch msg.String() {
	case "esc":
		m.logs.CloseFocused()
		if !m.logs.Active() {
			m.logView.logFocus = false
			m.statusMsg = "logs closed"
			m.updateViewport()
			return m, nil
		}
		m.syncLogViewports()
		m.updateViewport()
		return m, nil
	case "q", "ctrl+c":
		m.quitting = true
		m.logs.CloseAll()
		return m, tea.Quit
	case "tab":
		// Toggle focus back to the container list so another pane can be opened.
		m.logView.logFocus = false
		m.statusMsg = "list focus — select another container and press o"
		return m, nil
	case "l", "right", "h", "left":
		m.logs.CycleFocus()
		return m, nil
	case "f":
		focus := m.logs.Focus()
		m.logs.ToggleFollow()
		m.logView.stick[focus] = !m.logView.stick[focus]
		if m.logView.stick[focus] {
			m.logView.vps[focus].GotoBottom()
		}
		return m, nil
	case "o":
		m.logView.logFocus = false
		m.statusMsg = "list focus — select another container and press o"
		return m, nil
	case "/":
		m.ensureLogFilterInput()
		focus := m.logs.Focus()
		m.logView.filtering = true
		m.logView.filterIn.SetValue(m.logView.filters[focus].query)
		m.logView.filterIn.Focus()
		return m, textinput.Blink
	case "c":
		focus := m.logs.Focus()
		f := m.logView.filters[focus]
		f.caseSens = !f.caseSens
		m.logView.filters[focus] = f
		m.recompileFocusedFilter()
		return m, nil
	case "i":
		focus := m.logs.Focus()
		f := m.logView.filters[focus]
		f.invert = !f.invert
		m.logView.filters[focus] = f
		m.syncLogViewports()
		return m, nil
	case "E":
		m.applyLogFilterPreset(filterPresetError)
		m.statusMsg = "log filter: error preset"
		return m, nil
	case "W":
		m.applyLogFilterPreset(filterPresetWarn)
		m.statusMsg = "log filter: warn preset"
		return m, nil
	case "g", "home":
		focus := m.logs.Focus()
		m.logView.stick[focus] = false
		m.logView.vps[focus].GotoTop()
		return m, nil
	case "G", "end":
		focus := m.logs.Focus()
		m.logView.stick[focus] = true
		m.logView.vps[focus].GotoBottom()
		return m, nil
	}

	focus := m.logs.Focus()
	var cmd tea.Cmd
	m.logView.vps[focus], cmd = m.logView.vps[focus].Update(msg)
	if !m.logView.vps[focus].AtBottom() {
		m.logView.stick[focus] = false
	} else {
		m.logView.stick[focus] = true
	}
	return m, cmd
}

func (m Model) renderPaneBox(i int, p session.PaneSnapshot, focused bool) string {
	follow := "follow"
	if !m.logView.stick[i] {
		follow = "scroll"
	}
	f := m.logView.filters[i]
	_, matched, total := filterLines(p.Lines, f)

	title := fmt.Sprintf("%s (%s) [%s]", p.ContainerName, truncate(p.ContainerID, 12), follow)
	if strings.TrimSpace(f.query) != "" {
		flags := ""
		if f.caseSens {
			flags += "c"
		}
		if f.invert {
			flags += "!"
		}
		if flags != "" {
			flags = " " + flags
		}
		if f.invalid {
			title += fmt.Sprintf(" filter: bad regex%s", flags)
		} else {
			q := f.query
			if len(q) > 24 {
				q = q[:24] + "…"
			}
			title += fmt.Sprintf(" filter:%s%s %d/%d", q, flags, matched, total)
		}
	}
	if focused && m.logView.logFocus {
		title = styles.TabActive.Render(title)
	} else {
		title = styles.TabInactive.Render(title)
	}
	body := m.logView.vps[i].View()
	w := m.logView.vps[i].Width
	return lipgloss.JoinVertical(lipgloss.Left, title, styles.Modal.Width(w).Render(body))
}

func (m Model) renderLogs() string {
	if m.logs == nil {
		return styles.Muted.Render("no log hub")
	}
	snap := m.logs.Snapshot()
	m.ensureLogViewports()

	active := m.activePaneIndices()
	if len(active) == 0 {
		return styles.Muted.Render("no log panes")
	}

	var prefix string
	if m.logView.filtering {
		prefix = "Log filter: " + m.logView.filterIn.View() + "\n"
	}

	boxes := make([]string, len(active))
	for i, idx := range active {
		boxes[i] = m.renderPaneBox(idx, snap.Panes[idx], idx == snap.Focus)
	}

	n := len(active)
	var grid string
	switch n {
	case 1:
		grid = boxes[0]
	case 2:
		grid = lipgloss.JoinHorizontal(lipgloss.Top, boxes[0], boxes[1])
	case 3:
		top := lipgloss.JoinHorizontal(lipgloss.Top, boxes[0], boxes[1])
		grid = lipgloss.JoinVertical(lipgloss.Left, top, boxes[2])
	default:
		top := lipgloss.JoinHorizontal(lipgloss.Top, boxes[0], boxes[1])
		bot := lipgloss.JoinHorizontal(lipgloss.Top, boxes[2], boxes[3])
		grid = lipgloss.JoinVertical(lipgloss.Left, top, bot)
	}
	return prefix + grid
}
