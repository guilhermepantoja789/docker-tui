package ui

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/guilhermepantoja/docker-tui/internal/collector"
	"github.com/guilhermepantoja/docker-tui/internal/config"
	"github.com/guilhermepantoja/docker-tui/internal/dockerx"
	"github.com/guilhermepantoja/docker-tui/internal/model"
	"github.com/guilhermepantoja/docker-tui/internal/ui/styles"
)

type tabKind int

const (
	tabContainers tabKind = iota
	tabNetworks
	tabVolumes
	tabImages
)

type sortField int

const (
	sortByName sortField = iota
	sortByState
	sortByCPU
	sortByMem
)

type confirmState struct {
	active  bool
	request model.ActionRequest
	message string
}

type hostPickerState struct {
	active   bool
	contexts []dockerx.DockerContext
	cursor   int
}

// Model is the Bubble Tea root model.
type Model struct {
	cfg    config.Config
	handle *collector.Handle
	onHost func(opts dockerx.Options) error

	width  int
	height int

	tab         tabKind
	cursor      int
	offset      int // scroll offset for virtualized list
	filter      string
	filtering   bool
	filterInput textinput.Model
	sort        sortField
	sortDesc    bool

	statusMsg string
	statusErr error

	confirm confirmState
	hosts   hostPickerState

	snap *model.Snapshot

	quitting bool
}

type tickMsg time.Time
type actionResultMsg model.ActionResult

// New creates the UI model. handle must already be connected.
func New(cfg config.Config, handle *collector.Handle, onHost func(dockerx.Options) error) Model {
	ti := textinput.New()
	ti.Placeholder = "filter…"
	ti.CharLimit = 64
	ti.Width = 30

	return Model{
		cfg:         cfg,
		handle:      handle,
		onHost:      onHost,
		filterInput: ti,
		snap:        handle.Snapshot(),
		sort:        sortByName,
	}
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(m.scheduleTick(), m.waitAction())
}

func (m Model) scheduleTick() tea.Cmd {
	return tea.Tick(m.cfg.UIRefreshInterval, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

func (m Model) waitAction() tea.Cmd {
	return func() tea.Msg {
		res, ok := <-m.handle.ActionResults()
		if !ok {
			return nil
		}
		return actionResultMsg(res)
	}
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.updateViewport()
		return m, nil

	case tickMsg:
		m.snap = m.handle.Snapshot()
		m.updateViewport()
		if m.snap != nil && m.snap.Err != nil {
			m.statusErr = m.snap.Err
		}
		return m, m.scheduleTick()

	case actionResultMsg:
		res := model.ActionResult(msg)
		if res.Err != nil {
			m.statusErr = res.Err
			m.statusMsg = ""
		} else {
			m.statusErr = nil
			m.statusMsg = fmt.Sprintf("%s %s ok", res.Request.Kind.String(), res.Request.Name)
		}
		return m, m.waitAction()

	case tea.KeyMsg:
		return m.handleKey(msg)
	}
	return m, nil
}

func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.confirm.active {
		return m.handleConfirmKey(msg)
	}
	if m.hosts.active {
		return m.handleHostKey(msg)
	}
	if m.filtering {
		return m.handleFilterKey(msg)
	}

	switch msg.String() {
	case "ctrl+c", "q":
		m.quitting = true
		return m, tea.Quit
	case "tab", "right", "l":
		m.tab = (m.tab + 1) % 4
		m.cursor = 0
		m.offset = 0
	case "shift+tab", "left", "h":
		m.tab = (m.tab + 3) % 4
		m.cursor = 0
		m.offset = 0
	case "1":
		m.tab = tabContainers
		m.cursor, m.offset = 0, 0
	case "2":
		m.tab = tabNetworks
		m.cursor, m.offset = 0, 0
	case "3":
		m.tab = tabVolumes
		m.cursor, m.offset = 0, 0
	case "4":
		m.tab = tabImages
		m.cursor, m.offset = 0, 0
	case "j", "down":
		m.moveCursor(1)
	case "k", "up":
		m.moveCursor(-1)
	case "pgdown", "ctrl+d":
		m.moveCursor(m.visibleRows())
	case "pgup", "ctrl+u":
		m.moveCursor(-m.visibleRows())
	case "g", "home":
		m.cursor = 0
		m.offset = 0
		m.updateViewport()
	case "G", "end":
		n := m.rowCount()
		if n > 0 {
			m.cursor = n - 1
		}
		m.ensureCursorVisible()
		m.updateViewport()
	case "/":
		m.filtering = true
		m.filterInput.SetValue(m.filter)
		m.filterInput.Focus()
		return m, textinput.Blink
	case "esc":
		m.filter = ""
		m.filterInput.SetValue("")
		m.cursor, m.offset = 0, 0
	case "s":
		m.cycleSort()
	case "H":
		return m.openHostPicker()
	case "a":
		if m.tab == tabContainers {
			m.queueAction(model.ActionStart, false)
		}
	case "t":
		if m.tab == tabContainers {
			m.queueAction(model.ActionStop, false)
		}
	case "r":
		if m.tab == tabContainers {
			m.queueAction(model.ActionRestart, false)
		}
	case "d", "x":
		return m.promptDestructive()
	case "enter":
		m.showDetail()
	}
	return m, nil
}

func (m Model) handleConfirmKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "y", "enter":
		req := m.confirm.request
		m.confirm = confirmState{}
		m.handle.EnqueueAction(req)
		m.statusMsg = fmt.Sprintf("%s %s…", req.Kind.String(), req.Name)
	case "n", "esc":
		m.confirm = confirmState{}
	}
	return m, nil
}

func (m Model) handleHostKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "q":
		m.hosts.active = false
	case "j", "down":
		if m.hosts.cursor < len(m.hosts.contexts)-1 {
			m.hosts.cursor++
		}
	case "k", "up":
		if m.hosts.cursor > 0 {
			m.hosts.cursor--
		}
	case "enter":
		if m.hosts.cursor >= 0 && m.hosts.cursor < len(m.hosts.contexts) {
			ctx := m.hosts.contexts[m.hosts.cursor]
			m.hosts.active = false
			if m.onHost != nil {
				if err := m.onHost(dockerx.Options{Context: ctx.Name}); err != nil {
					m.statusErr = err
				} else {
					m.statusMsg = "switched to context " + ctx.Name
					m.statusErr = nil
					m.cursor, m.offset = 0, 0
				}
			}
		}
	}
	return m, nil
}

func (m Model) handleFilterKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.filtering = false
		m.filterInput.Blur()
		return m, nil
	case "enter":
		m.filter = m.filterInput.Value()
		m.filtering = false
		m.filterInput.Blur()
		m.cursor, m.offset = 0, 0
		m.updateViewport()
		return m, nil
	}
	var cmd tea.Cmd
	m.filterInput, cmd = m.filterInput.Update(msg)
	return m, cmd
}

func (m Model) openHostPicker() (tea.Model, tea.Cmd) {
	ctxs, err := dockerx.ListContexts()
	if err != nil {
		m.statusErr = err
		return m, nil
	}
	m.hosts = hostPickerState{active: true, contexts: ctxs}
	current := ""
	if m.snap != nil {
		current = m.snap.Host.Context
		if current == "" {
			current = m.snap.Host.Name
		}
	}
	for i, c := range ctxs {
		if c.Name == current || c.Current {
			m.hosts.cursor = i
			break
		}
	}
	return m, nil
}

func (m *Model) queueAction(kind model.ActionKind, force bool) {
	id, name, ok := m.selectedID()
	if !ok {
		return
	}
	m.handle.EnqueueAction(model.ActionRequest{Kind: kind, ID: id, Name: name, Force: force})
	m.statusMsg = fmt.Sprintf("%s %s…", kind.String(), name)
}

func (m Model) promptDestructive() (tea.Model, tea.Cmd) {
	id, name, ok := m.selectedID()
	if !ok {
		return m, nil
	}
	var kind model.ActionKind
	force := true
	switch m.tab {
	case tabContainers:
		kind = model.ActionRemove
	case tabNetworks:
		kind = model.ActionRemoveNetwork
	case tabVolumes:
		kind = model.ActionRemoveVolume
	case tabImages:
		kind = model.ActionRemoveImage
	default:
		return m, nil
	}
	m.confirm = confirmState{
		active:  true,
		request: model.ActionRequest{Kind: kind, ID: id, Name: name, Force: force},
		message: fmt.Sprintf("Remove %s %q? [y/n]", kind.String(), name),
	}
	return m, nil
}

func (m *Model) showDetail() {
	id, name, ok := m.selectedID()
	if !ok {
		return
	}
	m.statusMsg = fmt.Sprintf("%s (%s)", name, truncate(id, 12))
}

func (m Model) selectedID() (id, name string, ok bool) {
	switch m.tab {
	case tabContainers:
		rows := m.filteredContainers()
		if m.cursor < 0 || m.cursor >= len(rows) {
			return "", "", false
		}
		return rows[m.cursor].ID, rows[m.cursor].Name, true
	case tabNetworks:
		rows := m.filteredNetworks()
		if m.cursor < 0 || m.cursor >= len(rows) {
			return "", "", false
		}
		return rows[m.cursor].ID, rows[m.cursor].Name, true
	case tabVolumes:
		rows := m.filteredVolumes()
		if m.cursor < 0 || m.cursor >= len(rows) {
			return "", "", false
		}
		return rows[m.cursor].Name, rows[m.cursor].Name, true
	case tabImages:
		rows := m.filteredImages()
		if m.cursor < 0 || m.cursor >= len(rows) {
			return "", "", false
		}
		name := joinTags(rows[m.cursor].Tags)
		return rows[m.cursor].ID, name, true
	}
	return "", "", false
}

func (m *Model) moveCursor(delta int) {
	n := m.rowCount()
	if n == 0 {
		m.cursor = 0
		return
	}
	m.cursor += delta
	if m.cursor < 0 {
		m.cursor = 0
	}
	if m.cursor >= n {
		m.cursor = n - 1
	}
	m.ensureCursorVisible()
	m.updateViewport()
}

func (m *Model) ensureCursorVisible() {
	vis := m.visibleRows()
	if vis <= 0 {
		return
	}
	if m.cursor < m.offset {
		m.offset = m.cursor
	}
	if m.cursor >= m.offset+vis {
		m.offset = m.cursor - vis + 1
	}
	if m.offset < 0 {
		m.offset = 0
	}
}

func (m Model) visibleRows() int {
	// header(1)+tabs(1)+status(1)+table header(1)+footer(2)+padding
	h := m.height - 8
	if h < 3 {
		h = 3
	}
	return h
}

func (m Model) rowCount() int {
	switch m.tab {
	case tabContainers:
		return len(m.filteredContainers())
	case tabNetworks:
		return len(m.filteredNetworks())
	case tabVolumes:
		return len(m.filteredVolumes())
	case tabImages:
		return len(m.filteredImages())
	}
	return 0
}

func (m *Model) updateViewport() {
	if m.tab != tabContainers || m.handle == nil {
		return
	}
	rows := m.filteredContainers()
	vis := m.visibleRows()
	start := m.offset
	end := m.offset + vis
	if start < 0 {
		start = 0
	}
	if end > len(rows) {
		end = len(rows)
	}
	buf := m.cfg.ViewportBuffer
	idStart := start - buf
	if idStart < 0 {
		idStart = 0
	}
	idEnd := end + buf
	if idEnd > len(rows) {
		idEnd = len(rows)
	}
	ids := make([]string, 0, idEnd-idStart)
	for i := idStart; i < idEnd; i++ {
		ids = append(ids, rows[i].ID)
	}
	m.handle.SetViewport(model.Viewport{Start: start, End: end, IDs: ids})
}

func (m *Model) cycleSort() {
	m.sort = (m.sort + 1) % 4
	if m.sort == sortByName {
		m.sortDesc = !m.sortDesc
	}
	m.cursor, m.offset = 0, 0
	m.updateViewport()
}

func (m Model) filteredContainers() []model.Container {
	if m.snap == nil {
		return nil
	}
	src := m.snap.Containers
	f := strings.ToLower(m.filter)
	out := make([]model.Container, 0, len(src))
	for _, c := range src {
		if f != "" {
			if !strings.Contains(strings.ToLower(c.Name), f) &&
				!strings.Contains(strings.ToLower(c.Image), f) &&
				!strings.Contains(strings.ToLower(c.State), f) &&
				!strings.Contains(strings.ToLower(c.ID), f) {
				continue
			}
		}
		out = append(out, c)
	}
	sortContainers(out, m.sort, m.sortDesc)
	return out
}

func (m Model) filteredNetworks() []model.Network {
	if m.snap == nil {
		return nil
	}
	f := strings.ToLower(m.filter)
	out := make([]model.Network, 0, len(m.snap.Networks))
	for _, n := range m.snap.Networks {
		if f != "" && !strings.Contains(strings.ToLower(n.Name), f) && !strings.Contains(strings.ToLower(n.Driver), f) {
			continue
		}
		out = append(out, n)
	}
	return out
}

func (m Model) filteredVolumes() []model.Volume {
	if m.snap == nil {
		return nil
	}
	f := strings.ToLower(m.filter)
	out := make([]model.Volume, 0, len(m.snap.Volumes))
	for _, v := range m.snap.Volumes {
		if f != "" && !strings.Contains(strings.ToLower(v.Name), f) {
			continue
		}
		out = append(out, v)
	}
	return out
}

func (m Model) filteredImages() []model.Image {
	if m.snap == nil {
		return nil
	}
	f := strings.ToLower(m.filter)
	out := make([]model.Image, 0, len(m.snap.Images))
	for _, img := range m.snap.Images {
		if f != "" {
			tags := strings.ToLower(joinTags(img.Tags))
			if !strings.Contains(tags, f) && !strings.Contains(strings.ToLower(img.ID), f) {
				continue
			}
		}
		out = append(out, img)
	}
	return out
}

func sortContainers(rows []model.Container, field sortField, desc bool) {
	less := func(i, j int) bool {
		a, b := rows[i], rows[j]
		var cmp int
		switch field {
		case sortByState:
			cmp = strings.Compare(a.State, b.State)
		case sortByCPU:
			cmp = cmpFloat(a.Rates.CPUPercent, b.Rates.CPUPercent)
		case sortByMem:
			cmp = cmpUint(a.Rates.MemUsage, b.Rates.MemUsage)
		default:
			cmp = strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name))
		}
		if desc {
			return cmp > 0
		}
		return cmp < 0
	}
	// simple insertion sort is fine for viewport-sized filtered lists; for large N use sort.Slice
	sort.SliceStable(rows, less)
}

func cmpFloat(a, b float64) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	default:
		return 0
	}
}

func cmpUint(a, b uint64) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	default:
		return 0
	}
}

// --- View -------------------------------------------------------------------

func (m Model) View() string {
	if m.quitting {
		return ""
	}
	if m.width == 0 {
		return "loading…"
	}

	var b strings.Builder
	b.WriteString(m.renderHeader())
	b.WriteString("\n")
	b.WriteString(m.renderTabs())
	b.WriteString("\n")

	if m.hosts.active {
		b.WriteString(m.renderHostPicker())
	} else if m.confirm.active {
		b.WriteString(styles.Modal.Render(m.confirm.message))
		b.WriteString("\n")
	} else if m.filtering {
		b.WriteString("Filter: ")
		b.WriteString(m.filterInput.View())
		b.WriteString("\n")
		b.WriteString(m.renderTable())
	} else {
		b.WriteString(m.renderTable())
	}

	b.WriteString("\n")
	b.WriteString(m.renderStatus())
	b.WriteString("\n")
	b.WriteString(m.renderFooter())
	return b.String()
}

func (m Model) renderHeader() string {
	host := "unknown"
	sampled, total := 0, 0
	if m.snap != nil {
		host = m.snap.Host.Name
		if m.snap.Host.Address != "" {
			host = fmt.Sprintf("%s (%s)", m.snap.Host.Name, truncate(m.snap.Host.Address, 40))
		}
		sampled = m.snap.Sampled
		total = m.snap.Total
	}
	title := styles.Title.Render("docker-tui")
	meta := styles.Muted.Render(fmt.Sprintf("  host=%s  containers=%d  stats=%d/%d  sort=%s",
		host, total, sampled, total, sortLabel(m.sort)))
	return title + meta
}

func sortLabel(s sortField) string {
	switch s {
	case sortByState:
		return "state"
	case sortByCPU:
		return "cpu"
	case sortByMem:
		return "mem"
	default:
		return "name"
	}
}

func (m Model) renderTabs() string {
	names := []string{"1 Containers", "2 Networks", "3 Volumes", "4 Images"}
	parts := make([]string, len(names))
	for i, n := range names {
		if tabKind(i) == m.tab {
			parts[i] = styles.TabActive.Render(n)
		} else {
			parts[i] = styles.TabInactive.Render(n)
		}
	}
	return strings.Join(parts, " ")
}

func (m Model) renderTable() string {
	switch m.tab {
	case tabContainers:
		return m.renderContainers()
	case tabNetworks:
		return m.renderNetworks()
	case tabVolumes:
		return m.renderVolumes()
	case tabImages:
		return m.renderImages()
	}
	return ""
}

func (m Model) renderContainers() string {
	rows := m.filteredContainers()
	vis := m.visibleRows()
	start := m.offset
	end := start + vis
	if end > len(rows) {
		end = len(rows)
	}
	if start > len(rows) {
		start = 0
	}

	w := m.colWidths()
	var b strings.Builder
	hdr := styles.Header.Render(fmt.Sprintf("%s %s %s %s %s %s %s %s %s",
		pad("HOST", w.host),
		pad("NAME", w.name),
		pad("ID", w.id),
		pad("STATE", w.state),
		pad("CPU", w.cpu),
		pad("MEM", w.mem),
		pad("NET I/O", w.net),
		pad("BLOCK I/O", w.blk),
		pad("IMAGE", w.image),
	))
	b.WriteString(hdr)
	b.WriteString("\n")

	for i := start; i < end; i++ {
		c := rows[i]
		cpu, mem, net, blk := "—", "—", "—", "—"
		if c.HasRates {
			cpu = formatCPU(c.Rates.CPUPercent)
			mem = formatMem(c.Rates.MemUsage, c.Rates.MemLimit)
			net = formatBps(c.Rates.NetRxBps) + "↓ " + formatBps(c.Rates.NetTxBps) + "↑"
			blk = formatBps(c.Rates.BlkReadBps) + "↓ " + formatBps(c.Rates.BlkWriteBps) + "↑"
		}
		state := styles.StateStyle(c.State).Render(pad(c.State, w.state))
		line := fmt.Sprintf("%s %s %s %s %s %s %s %s %s",
			pad(truncate(c.Host, w.host), w.host),
			pad(truncate(c.Name, w.name), w.name),
			pad(c.ShortID(), w.id),
			state,
			pad(cpu, w.cpu),
			pad(truncate(mem, w.mem), w.mem),
			pad(truncate(net, w.net), w.net),
			pad(truncate(blk, w.blk), w.blk),
			pad(truncate(c.Image, w.image), w.image),
		)
		if i == m.cursor {
			line = styles.Selected.Render(line)
		}
		b.WriteString(line)
		b.WriteString("\n")
	}
	if len(rows) == 0 {
		b.WriteString(styles.Muted.Render("no containers"))
		b.WriteString("\n")
	}
	return b.String()
}

type colWidths struct {
	host, name, id, state, cpu, mem, net, blk, image int
}

func (m Model) colWidths() colWidths {
	// fixed-ish columns; remaining width goes to name/image
	w := colWidths{host: 10, name: 20, id: 12, state: 8, cpu: 7, mem: 14, net: 18, blk: 18, image: 20}
	used := w.host + w.name + w.id + w.state + w.cpu + w.mem + w.net + w.blk + w.image + 8
	extra := m.width - used
	if extra > 0 {
		w.name += extra / 2
		w.image += extra - extra/2
	}
	return w
}

func (m Model) renderNetworks() string {
	rows := m.filteredNetworks()
	vis := m.visibleRows()
	start := m.offset
	end := min(start+vis, len(rows))

	var b strings.Builder
	b.WriteString(styles.Header.Render(fmt.Sprintf("%-20s %-12s %-10s %8s %-12s", "NAME", "DRIVER", "SCOPE", "CTRS", "ID")))
	b.WriteString("\n")
	for i := start; i < end; i++ {
		n := rows[i]
		line := fmt.Sprintf("%-20s %-12s %-10s %8d %-12s",
			truncate(n.Name, 20), truncate(n.Driver, 12), truncate(n.Scope, 10), n.Containers, truncate(n.ID, 12))
		if i == m.cursor {
			line = styles.Selected.Render(line)
		}
		b.WriteString(line)
		b.WriteString("\n")
	}
	return b.String()
}

func (m Model) renderVolumes() string {
	rows := m.filteredVolumes()
	vis := m.visibleRows()
	start := m.offset
	end := min(start+vis, len(rows))

	var b strings.Builder
	b.WriteString(styles.Header.Render(fmt.Sprintf("%-30s %-12s %s", "NAME", "DRIVER", "MOUNTPOINT")))
	b.WriteString("\n")
	for i := start; i < end; i++ {
		v := rows[i]
		line := fmt.Sprintf("%-30s %-12s %s",
			truncate(v.Name, 30), truncate(v.Driver, 12), truncate(v.Mountpoint, max(10, m.width-46)))
		if i == m.cursor {
			line = styles.Selected.Render(line)
		}
		b.WriteString(line)
		b.WriteString("\n")
	}
	return b.String()
}

func (m Model) renderImages() string {
	rows := m.filteredImages()
	vis := m.visibleRows()
	start := m.offset
	end := min(start+vis, len(rows))

	var b strings.Builder
	b.WriteString(styles.Header.Render(fmt.Sprintf("%-14s %-12s %-40s %s", "ID", "SIZE", "TAGS", "CREATED")))
	b.WriteString("\n")
	for i := start; i < end; i++ {
		img := rows[i]
		line := fmt.Sprintf("%-14s %-12s %-40s %s",
			img.ShortID(),
			formatSize(img.Size),
			truncate(joinTags(img.Tags), 40),
			img.Created.Format(time.RFC3339),
		)
		if i == m.cursor {
			line = styles.Selected.Render(line)
		}
		b.WriteString(line)
		b.WriteString("\n")
	}
	return b.String()
}

func (m Model) renderHostPicker() string {
	var b strings.Builder
	b.WriteString(styles.Header.Render("Docker contexts  (enter to switch, esc to cancel)"))
	b.WriteString("\n")
	for i, c := range m.hosts.contexts {
		mark := " "
		if c.Current {
			mark = "*"
		}
		ep := c.Endpoints["docker"]
		line := fmt.Sprintf("%s %-24s %s", mark, c.Name, ep)
		if i == m.hosts.cursor {
			line = styles.Selected.Render(line)
		}
		b.WriteString(line)
		b.WriteString("\n")
	}
	return b.String()
}

func (m Model) renderStatus() string {
	if m.statusErr != nil {
		return styles.StatusErr.Render("error: " + m.statusErr.Error())
	}
	if m.statusMsg != "" {
		return styles.StatusOK.Render(m.statusMsg)
	}
	if m.filter != "" {
		return styles.Muted.Render("filter: " + m.filter)
	}
	return styles.Muted.Render(" ")
}

func (m Model) renderFooter() string {
	help := "tab/1-4 views  j/k move  / filter  s sort  a start  t stop  r restart  d delete  H host  q quit"
	return styles.Help.Render(truncate(help, max(0, m.width)))
}

// Run starts the Bubble Tea program.
func Run(ctx context.Context, cfg config.Config, handle *collector.Handle, onHost func(dockerx.Options) error) error {
	m := New(cfg, handle, onHost)
	p := tea.NewProgram(m, tea.WithAltScreen(), tea.WithContext(ctx))
	_, err := p.Run()
	return err
}

var _ = lipgloss.NewStyle