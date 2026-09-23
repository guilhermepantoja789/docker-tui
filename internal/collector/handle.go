package collector

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/guilhermepantoja789/docker-tui/internal/config"
	"github.com/guilhermepantoja789/docker-tui/internal/dockerx"
	"github.com/guilhermepantoja789/docker-tui/internal/model"
)

// Handle allows hot-swapping the active Engine when the Docker host changes.
type Handle struct {
	cfg config.Config

	mu     sync.Mutex
	engine atomic.Pointer[Engine]
	client atomic.Pointer[dockerx.Client]
	cancel context.CancelFunc
	wg     sync.WaitGroup

	fanIn chan model.ActionResult
}

// NewHandle creates an empty handle; call Switch before Run consumers.
func NewHandle(cfg config.Config) *Handle {
	h := &Handle{
		cfg:   cfg,
		fanIn: make(chan model.ActionResult, 1),
	}
	return h
}

// Switch replaces the active Docker client and collector engine.
func (h *Handle) Switch(parent context.Context, opts dockerx.Options) error {
	cl, err := dockerx.NewClient(opts)
	if err != nil {
		return err
	}
	pingCtx, cancelPing := context.WithTimeout(parent, h.cfg.StatsTimeout)
	err = cl.Ping(pingCtx)
	cancelPing()
	if err != nil {
		_ = cl.Close()
		return fmt.Errorf("connect: %w", err)
	}

	eng := New(h.cfg, cl)

	h.mu.Lock()
	defer h.mu.Unlock()

	if h.cancel != nil {
		h.cancel()
		h.wg.Wait()
	}
	if old := h.client.Load(); old != nil {
		_ = old.Close()
	}

	runCtx, cancel := context.WithCancel(parent)
	h.cancel = cancel
	h.client.Store(cl)
	h.engine.Store(eng)

	h.wg.Go(func() {
		_ = eng.Run(runCtx)
	})

	h.wg.Go(func() {
		for {
			select {
			case <-runCtx.Done():
				return
			case res, ok := <-eng.ActionResults():
				if !ok {
					return
				}
				select {
				case h.fanIn <- res:
				case <-runCtx.Done():
					return
				}
			}
		}
	})

	return nil
}

// Close stops the active engine and closes the client.
func (h *Handle) Close() {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.cancel != nil {
		h.cancel()
		h.wg.Wait()
		h.cancel = nil
	}
	if old := h.client.Load(); old != nil {
		_ = old.Close()
		h.client.Store(nil)
	}
	h.engine.Store(nil)
}

// Snapshot returns the current engine snapshot.
func (h *Handle) Snapshot() *model.Snapshot {
	eng := h.engine.Load()
	if eng == nil {
		return &model.Snapshot{}
	}
	return eng.Snapshot()
}

// SetViewport forwards to the active engine.
func (h *Handle) SetViewport(vp model.Viewport) {
	if eng := h.engine.Load(); eng != nil {
		eng.SetViewport(vp)
	}
}

// EnqueueAction forwards to the active engine.
func (h *Handle) EnqueueAction(req model.ActionRequest) {
	if eng := h.engine.Load(); eng != nil {
		eng.EnqueueAction(req)
	}
}

// ActionResults returns a stable channel across host switches.
func (h *Handle) ActionResults() <-chan model.ActionResult {
	return h.fanIn
}

// Client returns the active Docker client, if any.
func (h *Handle) Client() *dockerx.Client {
	return h.client.Load()
}
