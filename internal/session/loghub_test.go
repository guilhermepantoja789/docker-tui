package session_test

import (
	"bytes"
	"context"
	"encoding/binary"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/guilhermepantoja789/docker-tui/internal/dockerx"
	"github.com/guilhermepantoja789/docker-tui/internal/session"
)

type fakeStreamer struct {
	mu    sync.Mutex
	opens []string
	// id -> body reader factory
	bodies map[string]func() io.ReadCloser
	errFor map[string]error
}

func (f *fakeStreamer) ContainerLogs(ctx context.Context, id string, opts dockerx.LogOptions) (io.ReadCloser, error) {
	f.mu.Lock()
	f.opens = append(f.opens, id)
	err := f.errFor[id]
	mk := f.bodies[id]
	f.mu.Unlock()
	if err != nil {
		return nil, err
	}
	if mk == nil {
		return io.NopCloser(strings.NewReader("")), nil
	}
	rc := mk()
	go func() {
		<-ctx.Done()
		_ = rc.Close()
	}()
	return rc, nil
}

// multiplexFrame builds a Docker stdout frame for stdcopy.
func multiplexFrame(stream byte, payload string) []byte {
	var hdr [8]byte
	hdr[0] = stream
	binary.BigEndian.PutUint32(hdr[4:], uint32(len(payload)))
	return append(hdr[:], payload...)
}

func waitLines(t *testing.T, hub *session.LogHub, pane int, minLines int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		snap := hub.Snapshot()
		if snap.Panes[pane].Active && len(snap.Panes[pane].Lines) >= minLines {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timeout waiting for pane %d to have >= %d lines; got %+v", pane, minLines, hub.Snapshot())
}

func TestLogHub_OpenFocusesExisting(t *testing.T) {
	f := &fakeStreamer{bodies: map[string]func() io.ReadCloser{
		"a": func() io.ReadCloser {
			return io.NopCloser(bytes.NewReader(multiplexFrame(1, "line-a\n")))
		},
	}}
	hub := session.NewLogHub(f, "10", 100)
	ctx := context.Background()
	if err := hub.Open(ctx, "a", "alpha"); err != nil {
		t.Fatal(err)
	}
	waitLines(t, hub, 0, 1)
	if err := hub.Open(ctx, "a", "alpha"); err != nil {
		t.Fatal(err)
	}
	if hub.Count() != 1 {
		t.Fatalf("count=%d, want 1", hub.Count())
	}
	if hub.Focus() != 0 {
		t.Fatalf("focus=%d, want 0", hub.Focus())
	}
}

func TestLogHub_FifthOpenReplacesFocused(t *testing.T) {
	mk := func(msg string) func() io.ReadCloser {
		return func() io.ReadCloser {
			return io.NopCloser(bytes.NewReader(multiplexFrame(1, msg+"\n")))
		}
	}
	f := &fakeStreamer{bodies: map[string]func() io.ReadCloser{
		"a": mk("from-a"),
		"b": mk("from-b"),
		"c": mk("from-c"),
		"d": mk("from-d"),
		"e": mk("from-e"),
	}}
	hub := session.NewLogHub(f, "10", 100)
	ctx := context.Background()

	ids := []string{"a", "b", "c", "d"}
	names := []string{"A", "B", "C", "D"}
	for i := range ids {
		if err := hub.Open(ctx, ids[i], names[i]); err != nil {
			t.Fatal(err)
		}
		waitLines(t, hub, i, 1)
	}
	if hub.Count() != 4 {
		t.Fatalf("count=%d, want 4", hub.Count())
	}
	// Focus is on D (slot 3). Opening E should replace focused.
	hub.SetFocus(3)
	if err := hub.Open(ctx, "e", "E"); err != nil {
		t.Fatal(err)
	}
	waitLines(t, hub, 3, 1)

	snap := hub.Snapshot()
	if hub.Count() != 4 {
		t.Fatalf("count=%d, want 4 after replace", hub.Count())
	}
	want := []string{"a", "b", "c", "e"}
	for i, id := range want {
		if snap.Panes[i].ContainerID != id {
			t.Fatalf("pane%d=%s, want %s", i, snap.Panes[i].ContainerID, id)
		}
	}
}

func TestLogHub_BufferEviction(t *testing.T) {
	var body strings.Builder
	for i := 0; i < 150; i++ {
		body.Write(multiplexFrame(1, "x\n"))
	}
	payload := body.String()
	f := &fakeStreamer{bodies: map[string]func() io.ReadCloser{
		"a": func() io.ReadCloser {
			return io.NopCloser(strings.NewReader(payload))
		},
	}}
	hub := session.NewLogHub(f, "all", 100)
	if err := hub.Open(context.Background(), "a", "A"); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		n := len(hub.Snapshot().Panes[0].Lines)
		if n == 100 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	lines := hub.Snapshot().Panes[0].Lines
	if len(lines) != 100 {
		t.Fatalf("retained %d lines, want 100", len(lines))
	}
}

func TestLogHub_CloseAllStopsSessions(t *testing.T) {
	pr, pw := io.Pipe()
	f := &fakeStreamer{bodies: map[string]func() io.ReadCloser{
		"a": func() io.ReadCloser { return pr },
	}}
	hub := session.NewLogHub(f, "10", 100)
	if err := hub.Open(context.Background(), "a", "A"); err != nil {
		t.Fatal(err)
	}
	hub.CloseAll()
	if hub.Active() {
		t.Fatal("expected inactive after CloseAll")
	}
	// Writer should eventually get ErrClosedPipe when reader side cancels/closes.
	_, _ = pw.Write(multiplexFrame(1, "bye\n"))
	_ = pw.Close()
}

func TestLogHub_SetStreamerClosesExisting(t *testing.T) {
	f1 := &fakeStreamer{bodies: map[string]func() io.ReadCloser{
		"a": func() io.ReadCloser {
			return io.NopCloser(bytes.NewReader(multiplexFrame(1, "one\n")))
		},
	}}
	hub := session.NewLogHub(f1, "10", 100)
	if err := hub.Open(context.Background(), "a", "A"); err != nil {
		t.Fatal(err)
	}
	waitLines(t, hub, 0, 1)
	hub.SetStreamer(&fakeStreamer{})
	if hub.Active() {
		t.Fatal("SetStreamer should close existing panes")
	}
}

func TestLogHub_CycleFocus(t *testing.T) {
	mk := func(msg string) func() io.ReadCloser {
		return func() io.ReadCloser {
			return io.NopCloser(bytes.NewReader(multiplexFrame(1, msg+"\n")))
		}
	}
	f := &fakeStreamer{bodies: map[string]func() io.ReadCloser{
		"a": mk("a"), "b": mk("b"), "c": mk("c"), "d": mk("d"),
	}}
	hub := session.NewLogHub(f, "10", 100)
	ctx := context.Background()
	for i, id := range []string{"a", "b", "c", "d"} {
		_ = hub.Open(ctx, id, strings.ToUpper(id))
		waitLines(t, hub, i, 1)
	}
	hub.SetFocus(0)
	hub.CycleFocus()
	if hub.Focus() != 1 {
		t.Fatalf("focus=%d, want 1", hub.Focus())
	}
	hub.CycleFocus()
	if hub.Focus() != 2 {
		t.Fatalf("focus=%d, want 2", hub.Focus())
	}
	hub.CycleFocus()
	if hub.Focus() != 3 {
		t.Fatalf("focus=%d, want 3", hub.Focus())
	}
	hub.CycleFocus()
	if hub.Focus() != 0 {
		t.Fatalf("focus=%d, want 0 (wrap)", hub.Focus())
	}
}

func TestLogHub_CycleFocusSparse(t *testing.T) {
	mk := func(msg string) func() io.ReadCloser {
		return func() io.ReadCloser {
			return io.NopCloser(bytes.NewReader(multiplexFrame(1, msg+"\n")))
		}
	}
	f := &fakeStreamer{bodies: map[string]func() io.ReadCloser{
		"a": mk("a"), "b": mk("b"), "c": mk("c"),
	}}
	hub := session.NewLogHub(f, "10", 100)
	ctx := context.Background()
	_ = hub.Open(ctx, "a", "A")
	waitLines(t, hub, 0, 1)
	_ = hub.Open(ctx, "b", "B")
	waitLines(t, hub, 1, 1)
	_ = hub.Open(ctx, "c", "C")
	waitLines(t, hub, 2, 1)

	hub.SetFocus(1)
	hub.CloseFocused() // close middle slot
	if hub.Count() != 2 {
		t.Fatalf("count=%d, want 2", hub.Count())
	}
	// Remaining active: 0 and 2. Focus should land on an active pane.
	hub.SetFocus(0)
	hub.CycleFocus()
	if hub.Focus() != 2 {
		t.Fatalf("focus=%d, want 2", hub.Focus())
	}
	hub.CycleFocus()
	if hub.Focus() != 0 {
		t.Fatalf("focus=%d, want 0", hub.Focus())
	}
}
