package ui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/guilhermepantoja789/docker-tui/internal/dockerx"
)

type execDoneMsg struct {
	op  string
	err error
}

func (m Model) selectedRunningContainer() (id, name string, err error) {
	if m.tab != tabContainers {
		return "", "", fmt.Errorf("switch to containers view")
	}
	rows := m.filteredContainers()
	if m.cursor < 0 || m.cursor >= len(rows) {
		return "", "", fmt.Errorf("select a running container")
	}
	c := rows[m.cursor]
	if !strings.EqualFold(c.State, "running") {
		return "", "", fmt.Errorf("%s is not running (%s)", c.Name, c.State)
	}
	return c.ID, c.Name, nil
}

func (m Model) startExec() (tea.Model, tea.Cmd) {
	id, name, err := m.selectedRunningContainer()
	if err != nil {
		m.statusErr = err
		return m, nil
	}
	cl := m.handle.Client()
	if cl == nil {
		m.statusErr = fmt.Errorf("no docker client")
		return m, nil
	}
	cmd := dockerx.ExecCommand(cl.Host(), id)
	m.statusMsg = fmt.Sprintf("exec %s…", name)
	m.statusErr = nil
	return m, tea.ExecProcess(cmd, func(err error) tea.Msg {
		return execDoneMsg{op: "exec " + name, err: dockerx.FormatCLIError("exec", err)}
	})
}

func (m Model) startAttach() (tea.Model, tea.Cmd) {
	id, name, err := m.selectedRunningContainer()
	if err != nil {
		m.statusErr = err
		return m, nil
	}
	cl := m.handle.Client()
	if cl == nil {
		m.statusErr = fmt.Errorf("no docker client")
		return m, nil
	}
	cmd := dockerx.AttachCommand(cl.Host(), id)
	m.statusMsg = fmt.Sprintf("attach %s…", name)
	m.statusErr = nil
	return m, tea.ExecProcess(cmd, func(err error) tea.Msg {
		return execDoneMsg{op: "attach " + name, err: dockerx.FormatCLIError("attach", err)}
	})
}
