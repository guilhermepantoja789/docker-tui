package ui

import (
	"context"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/guilhermepantoja789/docker-tui/internal/session"
	"github.com/guilhermepantoja789/docker-tui/internal/ui/styles"
)

type logUpdateMsg struct{}

type logViewState struct {
	vps      [session.MaxPanes]viewport.Model
	stick    [session.MaxPanes]bool // stick to bottom while following
	inited   bool
	logFocus bool // when true, keys go to log panes
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
	cl := m.handle.Client()
	if cl == nil {
		m.statusErr = fmt.Errorf("no docker client")
		return m, nil
	}
	m.logs.SetStreamer(cl)

	if err := m.logs.Open(context.Background(), id, name); err != nil {
		m.statusErr = err
		return m, nil
	}
	m.logView.logFocus = true
	m.ensureLogViewports()
	m.syncLogViewports()
	m.statusMsg = fmt.Sprintf("logs %s (%d panes)", name, m.logs.Count())
	m.statusErr = nil
	m.updateViewport()
	return m, m.waitLogs()
}

func (m *Model) ensureLogViewports() {
	w, h := m.logPaneSize()
	for i := 0; i < session.MaxPanes; i++ {
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

func (m Model) logPaneSize() (w, h int) {
	n := 1
	if m.logs != nil {
		if c := m.logs.Count(); c > 0 {
			n = c
		}
	}
	gap := 1
	totalW := max(20, m.width-2)
	w = (totalW - gap*(n-1)) / max(n, 1)
	if w < 20 {
		w = 20
	}
	// Logs share vertical space with the container list.
	h = max(5, m.logBodyHeight())
	return w, h
}

func (m Model) logBodyHeight() int {
	// Half of body rows (body ≈ height - 8 chrome), at least 5.
	body := max(10, m.height-8)
	return max(5, body/2)
}

func (m Model) listBodyHeight() int {
	if m.logs == nil || !m.logs.Active() {
		return max(1, m.height-8)
	}
	body := max(10, m.height-8)
	return max(3, body-m.logBodyHeight()-1)
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
			continue
		}
		var b strings.Builder
		for _, line := range p.Lines {
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

func (m Model) handleLogsKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
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
		// Toggle focus back to the container list so a second pane can be opened.
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

func (m Model) renderLogs() string {
	if m.logs == nil {
		return styles.Muted.Render("no log hub")
	}
	snap := m.logs.Snapshot()
	m.ensureLogViewports()

	var panes []string
	for i := 0; i < session.MaxPanes; i++ {
		p := snap.Panes[i]
		if !p.Active {
			continue
		}
		follow := "follow"
		if !m.logView.stick[i] {
			follow = "scroll"
		}
		title := fmt.Sprintf("%s (%s) [%s]", p.ContainerName, truncate(p.ContainerID, 12), follow)
		if i == snap.Focus && m.logView.logFocus {
			title = styles.TabActive.Render(title)
		} else {
			title = styles.TabInactive.Render(title)
		}
		body := m.logView.vps[i].View()
		box := lipgloss.JoinVertical(lipgloss.Left, title, styles.Modal.Width(m.logView.vps[i].Width).Render(body))
		panes = append(panes, box)
	}
	if len(panes) == 0 {
		return styles.Muted.Render("no log panes")
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, panes...)
}
