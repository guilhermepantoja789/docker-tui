package collector

import (
	"fmt"
	"testing"
	"time"

	"github.com/guilhermepantoja789/docker-tui/internal/config"
	"github.com/guilhermepantoja789/docker-tui/internal/model"
)

func TestEnqueueAction_QueueBusy(t *testing.T) {
	cfg := config.Config{
		StatsConcurrency:  1,
		StatsInterval:     time.Second,
		ReconcileInterval: time.Minute,
		UIRefreshInterval: 200 * time.Millisecond,
		StatsTimeout:      time.Second,
		LogTail:           "10",
		LogBuffer:         100,
	}
	e := &Engine{
		cfg:      cfg,
		actionCh: make(chan model.ActionRequest, 1),
		resultCh: make(chan model.ActionResult, 1),
		meta:     map[string]*containerMeta{},
	}
	// Fill the queue so the next enqueue reports busy.
	e.actionCh <- model.ActionRequest{Kind: model.ActionStart, ID: "1", Name: "one"}
	e.EnqueueAction(model.ActionRequest{Kind: model.ActionStop, ID: "2", Name: "two"})

	select {
	case res := <-e.resultCh:
		if res.Err == nil || res.Err.Error() != "action queue busy" {
			t.Fatalf("got %+v", res)
		}
		if res.Request.Name != "two" {
			t.Fatalf("request=%+v", res.Request)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for busy result")
	}
}

func TestPriorityIDs_PrefersExplicitIDs(t *testing.T) {
	e := &Engine{
		meta: map[string]*containerMeta{
			"aaa": {ID: "aaa", Name: "a", State: "running"},
			"bbb": {ID: "bbb", Name: "b", State: "running"},
			"ccc": {ID: "ccc", Name: "c", State: "running"},
		},
		order: []string{"aaa", "bbb", "ccc"},
	}
	vp := model.Viewport{IDs: []string{"ccc", "aaa"}}
	e.viewport.Store(&vp)

	ids := e.priorityIDs()
	if len(ids) != 2 || ids[0] != "ccc" || ids[1] != "aaa" {
		t.Fatalf("priorityIDs=%v, want [ccc aaa]", ids)
	}
}

func TestPriorityIDs_FallsBackToRange(t *testing.T) {
	e := &Engine{
		meta: map[string]*containerMeta{
			"aaa": {ID: "aaa", Name: "a", State: "running"},
			"bbb": {ID: "bbb", Name: "b", State: "running"},
			"ccc": {ID: "ccc", Name: "c", State: "running"},
		},
		order: []string{"aaa", "bbb", "ccc"},
	}
	vp := model.Viewport{Start: 1, End: 3}
	e.viewport.Store(&vp)
	e.cfg.ViewportBuffer = 0

	ids := e.priorityIDs()
	if fmt.Sprint(ids) != "[bbb ccc]" {
		t.Fatalf("priorityIDs=%v, want [bbb ccc]", ids)
	}
}
