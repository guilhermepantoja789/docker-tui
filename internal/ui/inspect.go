package ui

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/docker/docker/api/types/container"

	"github.com/guilhermepantoja789/docker-tui/internal/ui/styles"
)

type inspectState struct {
	active   bool
	id       string
	name     string
	curated  string
	rawJSON  string
	showJSON bool
	vp       viewport.Model
	loading  bool
	err      error
}

type inspectMsg struct {
	id      string
	curated string
	rawJSON string
	err     error
}

func (m Model) openInspect() (tea.Model, tea.Cmd) {
	if m.tab != tabContainers {
		return m, nil
	}
	id, name, ok := m.selectedID()
	if !ok {
		return m, nil
	}
	cl := m.handle.Client()
	if cl == nil {
		m.statusErr = fmt.Errorf("no docker client")
		return m, nil
	}

	w, h := m.inspectViewportSize()
	vp := viewport.New(w, h)
	m.inspect = inspectState{
		active:  true,
		id:      id,
		name:    name,
		vp:      vp,
		loading: true,
	}
	m.statusMsg = fmt.Sprintf("inspect %s…", name)
	m.statusErr = nil

	return m, fetchInspectCmd(cl.InspectContainer, id)
}

type inspectFn func(ctx context.Context, id string) (container.InspectResponse, error)

func fetchInspectCmd(fn inspectFn, id string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		resp, err := fn(ctx, id)
		if err != nil {
			return inspectMsg{id: id, err: err}
		}
		raw, _ := json.MarshalIndent(resp, "", "  ")
		return inspectMsg{
			id:      id,
			curated: formatInspectCurated(resp),
			rawJSON: string(raw),
		}
	}
}

func formatInspectCurated(insp container.InspectResponse) string {
	var b strings.Builder
	state := ""
	status := ""
	if insp.State != nil {
		state = insp.State.Status
		status = insp.State.Error
		if insp.State.ExitCode != 0 && state != "running" {
			status = fmt.Sprintf("exit=%d %s", insp.State.ExitCode, status)
		}
	}
	image := ""
	cmd := ""
	env := []string(nil)
	workdir := ""
	user := ""
	if insp.Config != nil {
		image = insp.Config.Image
		cmd = strings.Join(insp.Config.Cmd, " ")
		env = insp.Config.Env
		workdir = insp.Config.WorkingDir
		user = insp.Config.User
	}

	fmt.Fprintf(&b, "Name:       %s\n", strings.TrimPrefix(insp.Name, "/"))
	fmt.Fprintf(&b, "ID:         %s\n", insp.ID)
	fmt.Fprintf(&b, "Image:      %s\n", image)
	fmt.Fprintf(&b, "State:      %s\n", state)
	if status != "" {
		fmt.Fprintf(&b, "Status:     %s\n", status)
	}
	fmt.Fprintf(&b, "Created:    %s\n", insp.Created)
	fmt.Fprintf(&b, "Path:       %s\n", insp.Path)
	fmt.Fprintf(&b, "Args:       %s\n", strings.Join(insp.Args, " "))
	fmt.Fprintf(&b, "Cmd:        %s\n", cmd)
	fmt.Fprintf(&b, "User:       %s\n", user)
	fmt.Fprintf(&b, "WorkingDir: %s\n", workdir)
	if insp.HostConfig != nil {
		fmt.Fprintf(&b, "Restart:    %s\n", insp.HostConfig.RestartPolicy.Name)
	}

	b.WriteString("\n--- Ports ---\n")
	if insp.NetworkSettings != nil && len(insp.NetworkSettings.Ports) > 0 {
		for port, bindings := range insp.NetworkSettings.Ports {
			if len(bindings) == 0 {
				fmt.Fprintf(&b, "  %s\n", port)
				continue
			}
			for _, bind := range bindings {
				fmt.Fprintf(&b, "  %s -> %s:%s\n", port, bind.HostIP, bind.HostPort)
			}
		}
	} else {
		b.WriteString("  (none)\n")
	}

	b.WriteString("\n--- Networks ---\n")
	if insp.NetworkSettings != nil && len(insp.NetworkSettings.Networks) > 0 {
		for name, ep := range insp.NetworkSettings.Networks {
			ip := ""
			if ep != nil {
				ip = ep.IPAddress
			}
			fmt.Fprintf(&b, "  %s  ip=%s\n", name, ip)
		}
	} else {
		b.WriteString("  (none)\n")
	}

	b.WriteString("\n--- Mounts ---\n")
	if len(insp.Mounts) == 0 {
		b.WriteString("  (none)\n")
	} else {
		for _, mnt := range insp.Mounts {
			fmt.Fprintf(&b, "  %s %s -> %s (%s)\n", mnt.Type, mnt.Source, mnt.Destination, mnt.Mode)
		}
	}

	b.WriteString("\n--- Env ---\n")
	if len(env) == 0 {
		b.WriteString("  (none)\n")
	} else {
		for _, e := range env {
			fmt.Fprintf(&b, "  %s\n", e)
		}
	}

	b.WriteString("\n[J] toggle raw JSON   [o] open logs   [esc] close\n")
	return b.String()
}

func (m Model) handleInspectKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "q":
		m.inspect = inspectState{}
		return m, nil
	case "J", "j":
		// j scrolls; only J toggles JSON
		if msg.String() == "j" {
			break
		}
		m.inspect.showJSON = !m.inspect.showJSON
		m.syncInspectViewport()
		return m, nil
	case "o":
		id, name := m.inspect.id, m.inspect.name
		m.inspect = inspectState{}
		return m.openLogsFor(id, name)
	case "ctrl+c":
		m.quitting = true
		return m, tea.Quit
	}

	var cmd tea.Cmd
	m.inspect.vp, cmd = m.inspect.vp.Update(msg)
	return m, cmd
}

func (m *Model) applyInspectMsg(msg inspectMsg) {
	if !m.inspect.active || m.inspect.id != msg.id {
		return
	}
	m.inspect.loading = false
	if msg.err != nil {
		m.inspect.err = msg.err
		m.statusErr = msg.err
		m.inspect.vp.SetContent("error: " + msg.err.Error())
		return
	}
	m.inspect.curated = msg.curated
	m.inspect.rawJSON = msg.rawJSON
	m.inspect.err = nil
	m.statusMsg = fmt.Sprintf("inspect %s", m.inspect.name)
	m.syncInspectViewport()
}

func (m *Model) syncInspectViewport() {
	content := m.inspect.curated
	if m.inspect.showJSON {
		content = m.inspect.rawJSON + "\n\n[J] curated view   [esc] close\n"
	}
	if m.inspect.loading {
		content = "loading…"
	}
	m.inspect.vp.SetContent(content)
}

func (m Model) inspectViewportSize() (w, h int) {
	w = max(20, m.width-4)
	h = max(5, m.height-8)
	return w, h
}

func (m Model) renderInspect() string {
	title := styles.Header.Render(fmt.Sprintf("Inspect  %s  (%s)", m.inspect.name, truncate(m.inspect.id, 12)))
	mode := "curated"
	if m.inspect.showJSON {
		mode = "json"
	}
	meta := styles.Muted.Render("  mode=" + mode)
	body := m.inspect.vp.View()
	return title + meta + "\n" + styles.Modal.Width(max(20, m.width-2)).Render(body)
}
