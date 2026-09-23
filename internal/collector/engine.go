package collector

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/docker/docker/api/types/events"
	"golang.org/x/sync/errgroup"

	"github.com/guilhermepantoja/docker-tui/internal/config"
	"github.com/guilhermepantoja/docker-tui/internal/dockerx"
	"github.com/guilhermepantoja/docker-tui/internal/metrics"
	"github.com/guilhermepantoja/docker-tui/internal/model"
)

// Engine drives inventory, viewport-priority stats, and lifecycle actions.
type Engine struct {
	cfg    config.Config
	client *dockerx.Client

	snapshot atomic.Pointer[model.Snapshot]
	viewport atomic.Pointer[model.Viewport]

	mu        sync.Mutex
	meta      map[string]*containerMeta
	prevStats map[string]metrics.Sample
	rates     map[string]model.ContainerRates
	networks  []model.Network
	volumes   []model.Volume
	images    []model.Image
	lastErr   error
	sampled   int

	actionCh chan model.ActionRequest
	resultCh chan model.ActionResult

	order []string // sorted container IDs for stable UI ordering
	coldCursor int
}

type containerMeta struct {
	ID      string
	Name    string
	Image   string
	State   string
	Status  string
	Labels  map[string]string
	Created time.Time
}

// New creates an Engine. Call Run to start background work.
func New(cfg config.Config, client *dockerx.Client) *Engine {
	e := &Engine{
		cfg:       cfg,
		client:    client,
		meta:      make(map[string]*containerMeta),
		prevStats: make(map[string]metrics.Sample),
		rates:     make(map[string]model.ContainerRates),
		actionCh:  make(chan model.ActionRequest, 1),
		resultCh:  make(chan model.ActionResult, 1),
	}
	vp := model.Viewport{Start: 0, End: 30}
	e.viewport.Store(&vp)
	e.publishLocked()
	return e
}

// Snapshot returns the latest immutable snapshot (never nil).
func (e *Engine) Snapshot() *model.Snapshot {
	s := e.snapshot.Load()
	if s == nil {
		empty := &model.Snapshot{Host: e.client.Host(), UpdatedAt: time.Now().UTC()}
		return empty
	}
	return s
}

// SetViewport updates which table rows should be sampled eagerly.
func (e *Engine) SetViewport(vp model.Viewport) {
	if vp.Start < 0 {
		vp.Start = 0
	}
	if vp.End < vp.Start {
		vp.End = vp.Start
	}
	cp := vp
	if len(vp.IDs) > 0 {
		cp.IDs = append([]string(nil), vp.IDs...)
	}
	e.viewport.Store(&cp)
}

// ActionResults returns a channel of completed lifecycle actions.
func (e *Engine) ActionResults() <-chan model.ActionResult {
	return e.resultCh
}

// EnqueueAction submits a lifecycle operation (non-blocking if possible).
func (e *Engine) EnqueueAction(req model.ActionRequest) {
	select {
	case e.actionCh <- req:
	default:
		// drop if busy; UI should serialize confirmations
		select {
		case e.resultCh <- model.ActionResult{Request: req, Err: fmt.Errorf("action queue busy")}:
		default:
		}
	}
}

// Run blocks until ctx is cancelled. All worker goroutines exit cleanly.
func (e *Engine) Run(ctx context.Context) error {
	g, ctx := errgroup.WithContext(ctx)

	g.Go(func() error { return e.inventoryLoop(ctx) })
	g.Go(func() error { return e.statsLoop(ctx) })
	g.Go(func() error { return e.resourcesLoop(ctx) })
	g.Go(func() error { return e.actionLoop(ctx) })

	return g.Wait()
}

func (e *Engine) inventoryLoop(ctx context.Context) error {
	if err := e.reconcile(ctx); err != nil {
		e.setErr(err)
	}

	var cancelEvents context.CancelFunc
	startEvents := func() (<-chan events.Message, <-chan error) {
		if cancelEvents != nil {
			cancelEvents()
		}
		var eventsCtx context.Context
		eventsCtx, cancelEvents = context.WithCancel(ctx)
		return e.client.Events(eventsCtx)
	}
	defer func() {
		if cancelEvents != nil {
			cancelEvents()
		}
	}()

	msgCh, errCh := startEvents()
	ticker := time.NewTicker(e.cfg.ReconcileInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if err := e.reconcile(ctx); err != nil {
				e.setErr(err)
			}
		case err, ok := <-errCh:
			if !ok {
				return nil
			}
			if ctx.Err() != nil {
				return nil
			}
			e.setErr(fmt.Errorf("events: %w", err))
			select {
			case <-ctx.Done():
				return nil
			case <-time.After(time.Second):
			}
			msgCh, errCh = startEvents()
		case msg, ok := <-msgCh:
			if !ok {
				return nil
			}
			e.handleEvent(msg)
		}
	}
}

func (e *Engine) reconcile(ctx context.Context) error {
	list, err := e.client.ListContainers(ctx)
	if err != nil {
		return err
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	next := make(map[string]*containerMeta, len(list))
	order := make([]string, 0, len(list))
	for i := range list {
		c := list[i]
		next[c.ID] = &containerMeta{
			ID:      c.ID,
			Name:    c.Name,
			Image:   c.Image,
			State:   c.State,
			Status:  c.Status,
			Labels:  c.Labels,
			Created: c.Created,
		}
		order = append(order, c.ID)
	}
	sort.Slice(order, func(i, j int) bool {
		return strings.ToLower(next[order[i]].Name) < strings.ToLower(next[order[j]].Name)
	})

	for id := range e.prevStats {
		if _, ok := next[id]; !ok {
			delete(e.prevStats, id)
			delete(e.rates, id)
		}
	}

	e.meta = next
	e.order = order
	e.lastErr = nil
	e.publishLocked()
	return nil
}

func (e *Engine) handleEvent(msg events.Message) {
	if msg.Type != events.ContainerEventType {
		return
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	id := msg.Actor.ID
	switch msg.Action {
	case events.ActionDestroy, events.ActionRemove:
		delete(e.meta, id)
		delete(e.prevStats, id)
		delete(e.rates, id)
		e.rebuildOrderLocked()
		e.publishLocked()
	case events.ActionDie, events.ActionStop, events.ActionKill, events.ActionOOM:
		if m, ok := e.meta[id]; ok {
			m.State = "exited"
			m.Status = string(msg.Action)
			e.publishLocked()
		}
	case events.ActionStart, events.ActionCreate, events.ActionRestart,
		events.ActionPause, events.ActionUnPause, events.ActionRename, events.ActionUpdate:
		m, ok := e.meta[id]
		if !ok {
			m = &containerMeta{ID: id, Labels: map[string]string{}}
			e.meta[id] = m
			e.rebuildOrderLocked()
		}
		if name, ok := msg.Actor.Attributes["name"]; ok {
			m.Name = name
		}
		if img, ok := msg.Actor.Attributes["image"]; ok {
			m.Image = img
		}
		switch msg.Action {
		case events.ActionStart, events.ActionRestart, events.ActionUnPause:
			m.State = "running"
			m.Status = "running"
		case events.ActionPause:
			m.State = "paused"
			m.Status = "paused"
		case events.ActionCreate:
			if m.State == "" {
				m.State = "created"
			}
		}
		e.publishLocked()
	}
}

func (e *Engine) rebuildOrderLocked() {
	order := make([]string, 0, len(e.meta))
	for id := range e.meta {
		order = append(order, id)
	}
	sort.Slice(order, func(i, j int) bool {
		return strings.ToLower(e.meta[order[i]].Name) < strings.ToLower(e.meta[order[j]].Name)
	})
	e.order = order
}

func (e *Engine) statsLoop(ctx context.Context) error {
	ticker := time.NewTicker(e.cfg.StatsInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			e.sampleRound(ctx)
		}
	}
}

func (e *Engine) sampleRound(ctx context.Context) {
	ids := e.priorityIDs()
	if len(ids) == 0 {
		return
	}

	type result struct {
		id    string
		sample metrics.Sample
		err   error
	}

	sem := make(chan struct{}, e.cfg.StatsConcurrency)
	results := make(chan result, len(ids))
	var wg sync.WaitGroup

	for _, id := range ids {
		id := id
		wg.Add(1)
		go func() {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				return
			}
			defer func() { <-sem }()

			reqCtx, cancel := context.WithTimeout(ctx, e.cfg.StatsTimeout)
			defer cancel()

			stats, err := e.client.StatsOneShot(reqCtx, id)
			if err != nil {
				results <- result{id: id, err: err}
				return
			}
			results <- result{id: id, sample: metrics.FromStatsResponse(stats, time.Now().UTC())}
		}()
	}

	go func() {
		wg.Wait()
		close(results)
	}()

	e.mu.Lock()
	defer e.mu.Unlock()

	sampled := 0
	for r := range results {
		if r.err != nil {
			// skip containers that disappeared mid-sample
			continue
		}
		prev := e.prevStats[r.id]
		rates := metrics.ComputeRates(prev, r.sample)
		e.prevStats[r.id] = r.sample
		e.rates[r.id] = rates
		sampled++
	}
	e.sampled = sampled
	e.publishLocked()
}

func (e *Engine) priorityIDs() []string {
	vp := e.viewport.Load()
	if vp == nil {
		vp = &model.Viewport{Start: 0, End: 30}
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	order := e.order
	hot := make([]string, 0, 64)
	seen := make(map[string]struct{}, 64)

	addHot := func(id string) {
		if _, ok := seen[id]; ok {
			return
		}
		m := e.meta[id]
		if m == nil || m.State != "running" {
			return
		}
		seen[id] = struct{}{}
		hot = append(hot, id)
	}

	if len(vp.IDs) > 0 {
		for _, id := range vp.IDs {
			addHot(id)
		}
	} else {
		buf := e.cfg.ViewportBuffer
		start := vp.Start - buf
		if start < 0 {
			start = 0
		}
		end := vp.End + buf
		if end > len(order) {
			end = len(order)
		}
		for i := start; i < end; i++ {
			addHot(order[i])
		}
	}

	coldBudget := e.cfg.StatsConcurrency
	cold := make([]string, 0, coldBudget)
	var coldStart int
	if len(order) > 0 {
		coldStart = e.coldCursor % len(order)
		e.coldCursor = (e.coldCursor + 1) % len(order)
	}
	for n := 0; n < len(order) && len(cold) < coldBudget; n++ {
		id := order[(coldStart+n)%len(order)]
		if _, ok := seen[id]; ok {
			continue
		}
		m := e.meta[id]
		if m == nil || m.State != "running" {
			continue
		}
		cold = append(cold, id)
	}

	out := make([]string, 0, len(hot)+len(cold))
	out = append(out, hot...)
	out = append(out, cold...)
	return out
}

func (e *Engine) resourcesLoop(ctx context.Context) error {
	if err := e.refreshResources(ctx); err != nil {
		e.setErr(err)
	}
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if err := e.refreshResources(ctx); err != nil {
				e.setErr(err)
			}
		}
	}
}

func (e *Engine) refreshResources(ctx context.Context) error {
	nets, err := e.client.ListNetworks(ctx)
	if err != nil {
		return err
	}
	vols, err := e.client.ListVolumes(ctx)
	if err != nil {
		return err
	}
	imgs, err := e.client.ListImages(ctx)
	if err != nil {
		return err
	}

	e.mu.Lock()
	defer e.mu.Unlock()
	e.networks = nets
	e.volumes = vols
	e.images = imgs
	e.publishLocked()
	return nil
}

func (e *Engine) actionLoop(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return nil
		case req := <-e.actionCh:
			err := e.execAction(ctx, req)
			res := model.ActionResult{Request: req, Err: err}
			select {
			case e.resultCh <- res:
			case <-ctx.Done():
				return nil
			}
			// Events will update inventory; also force reconcile for reliability.
			_ = e.reconcile(ctx)
			_ = e.refreshResources(ctx)
		}
	}
}

func (e *Engine) execAction(ctx context.Context, req model.ActionRequest) error {
	actx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	switch req.Kind {
	case model.ActionStart:
		return e.client.StartContainer(actx, req.ID)
	case model.ActionStop:
		return e.client.StopContainer(actx, req.ID, nil)
	case model.ActionRestart:
		return e.client.RestartContainer(actx, req.ID, nil)
	case model.ActionRemove:
		return e.client.RemoveContainer(actx, req.ID, req.Force)
	case model.ActionRemoveNetwork:
		return e.client.RemoveNetwork(actx, req.ID)
	case model.ActionRemoveVolume:
		return e.client.RemoveVolume(actx, req.ID, req.Force)
	case model.ActionRemoveImage:
		return e.client.RemoveImage(actx, req.ID, req.Force)
	default:
		return fmt.Errorf("unknown action %d", req.Kind)
	}
}

func (e *Engine) setErr(err error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.lastErr = err
	e.publishLocked()
}

func (e *Engine) publishLocked() {
	host := e.client.Host()
	containers := make([]model.Container, 0, len(e.order))
	for _, id := range e.order {
		m := e.meta[id]
		if m == nil {
			continue
		}
		c := model.Container{
			ID:      m.ID,
			Name:    m.Name,
			Image:   m.Image,
			State:   m.State,
			Status:  m.Status,
			Labels:  copyLabels(m.Labels),
			Host:    host.Name,
			Created: m.Created,
		}
		if r, ok := e.rates[id]; ok {
			c.Rates = r
			c.HasRates = true
		}
		containers = append(containers, c)
	}

	nets := append([]model.Network(nil), e.networks...)
	vols := append([]model.Volume(nil), e.volumes...)
	imgs := make([]model.Image, len(e.images))
	copy(imgs, e.images)
	for i := range imgs {
		imgs[i].Tags = append([]string(nil), e.images[i].Tags...)
	}

	snap := &model.Snapshot{
		Host:       host,
		Containers: containers,
		Networks:   nets,
		Volumes:    vols,
		Images:     imgs,
		Sampled:    e.sampled,
		Total:      len(containers),
		UpdatedAt:  time.Now().UTC(),
		Err:        e.lastErr,
	}
	e.snapshot.Store(snap)
}

func copyLabels(in map[string]string) map[string]string {
	if len(in) == 0 {
		return map[string]string{}
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}
