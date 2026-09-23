package ui

import (
	"context"
	"io"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/network"

	"github.com/guilhermepantoja789/docker-tui/internal/collector"
	"github.com/guilhermepantoja789/docker-tui/internal/config"
	"github.com/guilhermepantoja789/docker-tui/internal/dockerx"
	"github.com/guilhermepantoja789/docker-tui/internal/model"
	"github.com/guilhermepantoja789/docker-tui/internal/session"
)

func testConfig() config.Config {
	return config.Config{
		StatsConcurrency:  1,
		StatsInterval:     time.Second,
		ReconcileInterval: time.Minute,
		UIRefreshInterval: 200 * time.Millisecond,
		StatsTimeout:      time.Second,
		LogTail:           "50",
		LogBuffer:         500,
	}
}

type emptyStreamer struct{}

func (emptyStreamer) ContainerLogs(ctx context.Context, id string, opts dockerx.LogOptions) (io.ReadCloser, error) {
	return io.NopCloser(strings.NewReader("")), nil
}

func TestFormatInspectCurated_IncludesCoreFields(t *testing.T) {
	insp := container.InspectResponse{
		ContainerJSONBase: &container.ContainerJSONBase{
			ID:      "abc123def456",
			Name:    "/api",
			Created: "2024-01-02T03:04:05Z",
			Path:    "/app",
			Args:    []string{"serve"},
			State:   &container.State{Status: "running"},
			HostConfig: &container.HostConfig{
				RestartPolicy: container.RestartPolicy{Name: "always"},
			},
		},
		Config: &container.Config{
			Image:      "ghcr.io/example/api:1",
			Cmd:        []string{"/app", "serve"},
			Env:        []string{"PORT=8080"},
			WorkingDir: "/app",
			User:       "1000",
		},
		NetworkSettings: &container.NetworkSettings{
			Networks: map[string]*network.EndpointSettings{
				"bridge": {IPAddress: "172.17.0.2"},
			},
		},
		Mounts: []container.MountPoint{
			{Type: "bind", Source: "/data", Destination: "/data", Mode: "rw"},
		},
	}
	out := formatInspectCurated(insp)
	for _, want := range []string{"api", "abc123", "running", "ghcr.io/example/api:1", "PORT=8080", "bridge", "/data"} {
		if !strings.Contains(out, want) {
			t.Fatalf("curated missing %q:\n%s", want, out)
		}
	}
}

func TestModel_OpenInspectRequiresContainersTab(t *testing.T) {
	h := collector.NewHandle(testConfig())
	m := New(testConfig(), h, nil)
	m.width, m.height = 120, 40
	m.tab = tabNetworks
	m.snap = &model.Snapshot{
		Networks: []model.Network{{ID: "n1", Name: "bridge"}},
	}
	next, _ := m.openInspect()
	nm := next.(Model)
	if nm.inspect.active {
		t.Fatal("inspect should not open on networks tab")
	}
}

func TestModel_InspectEscCloses(t *testing.T) {
	h := collector.NewHandle(testConfig())
	m := New(testConfig(), h, nil)
	m.width, m.height = 120, 40
	m.inspect = inspectState{active: true, id: "x", name: "x"}
	next, _ := m.handleInspectKey(tea.KeyMsg{Type: tea.KeyEsc})
	nm := next.(Model)
	if nm.inspect.active {
		t.Fatal("esc should close inspect")
	}
}

func TestModel_LogFocusCycleAndEsc(t *testing.T) {
	h := collector.NewHandle(testConfig())
	m := New(testConfig(), h, nil)
	m.width, m.height = 120, 40
	m.logs = session.NewLogHub(emptyStreamer{}, "10", 100)
	ctx := context.Background()
	if err := m.logs.Open(ctx, "c1", "one"); err != nil {
		t.Fatal(err)
	}
	if err := m.logs.Open(ctx, "c2", "two"); err != nil {
		t.Fatal(err)
	}
	m.logView.logFocus = true
	m.ensureLogViewports()

	if m.logs.Count() != 2 {
		t.Fatalf("count=%d", m.logs.Count())
	}
	m.logs.SetFocus(0)
	next, _ := m.handleLogsKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'l'}})
	nm := next.(Model)
	if nm.logs.Focus() != 1 {
		t.Fatalf("focus=%d after l, want 1", nm.logs.Focus())
	}

	next, _ = nm.handleLogsKey(tea.KeyMsg{Type: tea.KeyEsc})
	nm = next.(Model)
	if nm.logs.Count() != 1 {
		t.Fatalf("count=%d after esc, want 1", nm.logs.Count())
	}
	next, _ = nm.handleLogsKey(tea.KeyMsg{Type: tea.KeyEsc})
	nm = next.(Model)
	if nm.logs.Active() {
		t.Fatal("second esc should close last pane")
	}
	if nm.logView.logFocus {
		t.Fatal("logFocus should clear when no panes remain")
	}
}

func TestConfig_LogValidation(t *testing.T) {
	cfg := testConfig()
	cfg.LogBuffer = 50
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected log-buffer validation error")
	}
	cfg.LogBuffer = 100
	cfg.LogTail = ""
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected log-tail validation error")
	}
}
