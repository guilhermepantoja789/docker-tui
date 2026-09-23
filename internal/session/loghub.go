package session

import (
	"bufio"
	"context"
	"io"
	"sync"

	"github.com/docker/docker/pkg/stdcopy"

	"github.com/guilhermepantoja789/docker-tui/internal/dockerx"
)

const MaxPanes = 2

// LogStreamer opens container log streams. *dockerx.Client implements this.
type LogStreamer interface {
	ContainerLogs(ctx context.Context, id string, opts dockerx.LogOptions) (io.ReadCloser, error)
}

// PaneSnapshot is an immutable view of one log pane for the UI.
type PaneSnapshot struct {
	ContainerID   string
	ContainerName string
	Lines         []string
	Err           error
	Follow        bool
	Active        bool
}

// HubSnapshot is an immutable view of both panes plus focus.
type HubSnapshot struct {
	Panes [MaxPanes]PaneSnapshot
	Focus int
	Count int
}

// LogHub manages up to MaxPanes concurrent follow streams.
type LogHub struct {
	streamer   LogStreamer
	tail       string
	bufferSize int

	mu     sync.Mutex
	panes  [MaxPanes]*logSession
	focus  int
	seq    uint64 // bumps on any change; UI can detect updates
	notify chan struct{}
}

type logSession struct {
	id     string
	name   string
	cancel context.CancelFunc
	follow bool

	mu    sync.Mutex
	lines []string
	err   error
}

// NewLogHub creates a hub. notify is buffered (1); waiters use WaitNotify.
func NewLogHub(streamer LogStreamer, tail string, bufferSize int) *LogHub {
	if bufferSize < 100 {
		bufferSize = 100
	}
	if tail == "" {
		tail = "200"
	}
	return &LogHub{
		streamer:   streamer,
		tail:       tail,
		bufferSize: bufferSize,
		notify:     make(chan struct{}, 1),
	}
}

// SetStreamer replaces the log backend (e.g. after host switch). Closes existing sessions.
func (h *LogHub) SetStreamer(s LogStreamer) {
	h.CloseAll()
	h.mu.Lock()
	h.streamer = s
	h.mu.Unlock()
}

// WaitNotify returns a channel that receives when pane contents change.
func (h *LogHub) WaitNotify() <-chan struct{} {
	return h.notify
}

// Snapshot returns a copy of current pane state.
func (h *LogHub) Snapshot() HubSnapshot {
	h.mu.Lock()
	defer h.mu.Unlock()
	var out HubSnapshot
	out.Focus = h.focus
	for i, s := range h.panes {
		if s == nil {
			continue
		}
		out.Count++
		s.mu.Lock()
		lines := append([]string(nil), s.lines...)
		err := s.err
		follow := s.follow
		id, name := s.id, s.name
		s.mu.Unlock()
		out.Panes[i] = PaneSnapshot{
			ContainerID:   id,
			ContainerName: name,
			Lines:         lines,
			Err:           err,
			Follow:        follow,
			Active:        true,
		}
	}
	return out
}

// Focus returns the focused pane index (0 or 1).
func (h *LogHub) Focus() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.focus
}

// SetFocus sets the focused pane if it is active.
func (h *LogHub) SetFocus(i int) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if i < 0 || i >= MaxPanes || h.panes[i] == nil {
		return
	}
	h.focus = i
}

// CycleFocus moves focus to the other active pane when both are open.
func (h *LogHub) CycleFocus() {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.panes[0] != nil && h.panes[1] != nil {
		h.focus = 1 - h.focus
	}
}

// Open starts or focuses a log stream for container id.
// A third open replaces the focused pane.
func (h *LogHub) Open(parent context.Context, id, name string) error {
	h.mu.Lock()
	streamer := h.streamer
	if streamer == nil {
		h.mu.Unlock()
		return errNoStreamer
	}

	// Already open → focus it.
	for i, s := range h.panes {
		if s != nil && s.id == id {
			h.focus = i
			h.mu.Unlock()
			h.ping()
			return nil
		}
	}

	slot := -1
	for i, s := range h.panes {
		if s == nil {
			slot = i
			break
		}
	}
	if slot < 0 {
		slot = h.focus
		h.stopLocked(slot)
	}

	ctx, cancel := context.WithCancel(parent)
	sess := &logSession{
		id:     id,
		name:   name,
		cancel: cancel,
		follow: true,
		lines:  make([]string, 0, min(256, h.bufferSize)),
	}
	h.panes[slot] = sess
	h.focus = slot
	tail := h.tail
	bufSize := h.bufferSize
	h.mu.Unlock()

	go h.readLoop(ctx, streamer, sess, tail, bufSize)
	h.ping()
	return nil
}

// CloseFocused stops and clears the focused pane.
func (h *LogHub) CloseFocused() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.stopLocked(h.focus)
	// Prefer remaining pane for focus.
	if h.panes[h.focus] == nil {
		for i, s := range h.panes {
			if s != nil {
				h.focus = i
				break
			}
		}
	}
	h.ping()
}

// CloseAll stops every pane.
func (h *LogHub) CloseAll() {
	h.mu.Lock()
	defer h.mu.Unlock()
	for i := range h.panes {
		h.stopLocked(i)
	}
	h.focus = 0
	h.ping()
}

// ToggleFollow toggles follow on the focused pane (UI may still scroll independently).
func (h *LogHub) ToggleFollow() {
	h.mu.Lock()
	s := h.panes[h.focus]
	h.mu.Unlock()
	if s == nil {
		return
	}
	s.mu.Lock()
	s.follow = !s.follow
	s.mu.Unlock()
	h.ping()
}

// Active returns true if any pane is open.
func (h *LogHub) Active() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.panes[0] != nil || h.panes[1] != nil
}

// Count returns the number of open panes.
func (h *LogHub) Count() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	n := 0
	for _, s := range h.panes {
		if s != nil {
			n++
		}
	}
	return n
}

func (h *LogHub) stopLocked(i int) {
	if i < 0 || i >= MaxPanes {
		return
	}
	s := h.panes[i]
	if s == nil {
		return
	}
	s.cancel()
	h.panes[i] = nil
}

func (h *LogHub) readLoop(ctx context.Context, streamer LogStreamer, sess *logSession, tail string, bufSize int) {
	rc, err := streamer.ContainerLogs(ctx, sess.id, dockerx.LogOptions{
		Follow: true,
		Tail:   tail,
		Stdout: true,
		Stderr: true,
	})
	if err != nil {
		sess.mu.Lock()
		sess.err = err
		sess.mu.Unlock()
		h.ping()
		return
	}
	defer rc.Close()

	pr, pw := io.Pipe()
	go func() {
		defer pw.Close()
		_, copyErr := stdcopy.StdCopy(pw, pw, rc)
		if copyErr != nil && ctx.Err() == nil {
			sess.mu.Lock()
			if sess.err == nil {
				sess.err = copyErr
			}
			sess.mu.Unlock()
			h.ping()
		}
	}()

	sc := bufio.NewScanner(pr)
	// Allow long log lines.
	buf := make([]byte, 0, 64*1024)
	sc.Buffer(buf, 1024*1024)

	for sc.Scan() {
		if ctx.Err() != nil {
			return
		}
		line := sc.Text()
		sess.mu.Lock()
		sess.lines = append(sess.lines, line)
		if len(sess.lines) > bufSize {
			sess.lines = sess.lines[len(sess.lines)-bufSize:]
		}
		sess.mu.Unlock()
		h.ping()
	}
	if scanErr := sc.Err(); scanErr != nil && ctx.Err() == nil {
		sess.mu.Lock()
		sess.err = scanErr
		sess.mu.Unlock()
		h.ping()
	}
}

func (h *LogHub) ping() {
	select {
	case h.notify <- struct{}{}:
	default:
	}
}

type noStreamerError struct{}

func (noStreamerError) Error() string { return "log streamer not configured" }

var errNoStreamer error = noStreamerError{}
