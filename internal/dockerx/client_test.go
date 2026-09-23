package dockerx_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/docker/docker/pkg/stdcopy"

	"github.com/guilhermepantoja789/docker-tui/internal/dockerx"
	"github.com/guilhermepantoja789/docker-tui/internal/model"
)

func TestCLIArgs_PrefersContext(t *testing.T) {
	args := dockerx.CLIArgs(model.Host{Context: "prod", Address: "tcp://ignored:2375"}, "ps")
	want := []string{"--context", "prod", "ps"}
	if strings.Join(args, " ") != strings.Join(want, " ") {
		t.Fatalf("got %v, want %v", args, want)
	}
}

func TestCLIArgs_UsesHostWhenNoContext(t *testing.T) {
	args := dockerx.CLIArgs(model.Host{Address: "unix:///var/run/docker.sock"}, "attach", "abc")
	want := []string{"-H", "unix:///var/run/docker.sock", "attach", "abc"}
	if strings.Join(args, " ") != strings.Join(want, " ") {
		t.Fatalf("got %v, want %v", args, want)
	}
}

func TestCLIArgs_DefaultContextOmitsFlags(t *testing.T) {
	args := dockerx.CLIArgs(model.Host{Context: "default"}, "exec", "-it", "x", "sh")
	want := []string{"exec", "-it", "x", "sh"}
	if strings.Join(args, " ") != strings.Join(want, " ") {
		t.Fatalf("got %v, want %v", args, want)
	}
}

func TestFormatCLIError_Nil(t *testing.T) {
	if err := dockerx.FormatCLIError("exec", nil); err != nil {
		t.Fatalf("got %v", err)
	}
}

func TestInspectAndLogs_HTTPMock(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		switch {
		case path == "/_ping" || strings.HasSuffix(path, "/_ping"):
			w.Header().Set("Api-Version", "1.45")
			w.Header().Set("OSType", "linux")
			w.WriteHeader(http.StatusOK)
		case strings.HasSuffix(path, "/containers/abc123/json"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"Id":      "abc123def456",
				"Name":    "/web",
				"Created": "2024-01-01T00:00:00Z",
				"Path":    "nginx",
				"Args":    []string{},
				"State":   map[string]any{"Status": "running"},
				"Config": map[string]any{
					"Image": "nginx:latest",
					"Cmd":   []string{"nginx"},
					"Env":   []string{"FOO=1"},
				},
				"HostConfig": map[string]any{
					"RestartPolicy": map[string]any{"Name": "no"},
				},
				"NetworkSettings": map[string]any{
					"Ports":    map[string]any{},
					"Networks": map[string]any{},
				},
				"Mounts": []any{},
			})
		case strings.HasSuffix(path, "/containers/abc123/logs"):
			hdr := []byte{1, 0, 0, 0, 0, 0, 0, 6}
			_, _ = w.Write(hdr)
			_, _ = w.Write([]byte("hello\n"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	cl, err := dockerx.NewClient(dockerx.Options{Host: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	defer cl.Close()

	ctx := context.Background()
	insp, err := cl.InspectContainer(ctx, "abc123")
	if err != nil {
		t.Fatalf("inspect: %v", err)
	}
	if insp.Name != "/web" {
		t.Fatalf("name=%q", insp.Name)
	}
	if insp.Config == nil || insp.Config.Image != "nginx:latest" {
		t.Fatalf("unexpected config: %+v", insp.Config)
	}

	rc, err := cl.ContainerLogs(ctx, "abc123", dockerx.LogOptions{Stdout: true, Stderr: true, Tail: "10"})
	if err != nil {
		t.Fatalf("logs: %v", err)
	}
	defer rc.Close()

	var out strings.Builder
	if _, err := stdcopy.StdCopy(&out, &out, rc); err != nil && err != io.EOF {
		t.Fatalf("demux: %v", err)
	}
	if !strings.Contains(out.String(), "hello") {
		t.Fatalf("demuxed %q missing hello", out.String())
	}
}
