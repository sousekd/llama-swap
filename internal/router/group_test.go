package router

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/mostlygeek/llama-swap/internal/config"
	"github.com/mostlygeek/llama-swap/internal/logmon"
	"github.com/mostlygeek/llama-swap/internal/process"
)

// newTestGroup builds a Group directly from the supplied processes and config,
// bypassing NewGroup's call to process.New.
func newTestGroup(t *testing.T, conf config.Config, processes map[string]process.Process) *Group {
	t.Helper()
	modelToGroup := make(map[string]string)
	for gid, gcfg := range conf.Routing.Router.Settings.Groups {
		for _, mid := range gcfg.Members {
			modelToGroup[mid] = gid
		}
	}
	swapper := &groupSwapper{
		config:       conf,
		modelToGroup: modelToGroup,
	}
	base, err := newBaseRouter("group", conf, processes, logmon.NewWriter(io.Discard), swapper)
	if err != nil {
		t.Fatalf("newBaseRouter: %v", err)
	}
	base.testProcessed = make(chan struct{}, 64)
	g := &Group{baseRouter: base}
	go base.run()
	t.Cleanup(func() {
		if !g.shuttingDown.Load() {
			_ = g.Shutdown(time.Second)
		}
	})
	return g
}

func TestGroup_NewGroup_DuplicateMembership(t *testing.T) {
	conf := config.Config{
		Routing: groupRouting(map[string]config.GroupConfig{
			"g1": {Swap: true, Members: []string{"a"}},
			"g2": {Swap: true, Members: []string{"a"}},
		}),
		Models: map[string]config.ModelConfig{
			"a": {},
		},
	}
	log := logmon.NewWriter(io.Discard)
	if _, err := NewGroup(conf, log, log); err == nil {
		t.Fatalf("expected error for duplicate membership")
	}
}

func TestGroup_ServeHTTP_SwapStopsPrevious(t *testing.T) {
	a := newFakeProcess("a")
	a.markReady()
	go a.Run(0) // park a Run goroutine so Stop has something to release

	b := newFakeProcess("b")
	b.autoReady = true

	conf := config.Config{
		HealthCheckTimeout: 5,
		Routing: groupRouting(map[string]config.GroupConfig{
			"g": {Swap: true, Exclusive: true, Members: []string{"a", "b"}},
		}),
	}
	g := newTestGroup(t, conf, map[string]process.Process{"a": a, "b": b})

	w := httptest.NewRecorder()
	g.ServeHTTP(w, newRequest("b"))

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%q", w.Code, w.Body.String())
	}
	if got := a.stopCalls.Load(); got != 1 {
		t.Errorf("a.stopCalls=%d want 1", got)
	}
	if got := b.runCalls.Load(); got != 1 {
		t.Errorf("b.runCalls=%d want 1", got)
	}
	if got := b.serveCalls.Load(); got != 1 {
		t.Errorf("b.serveCalls=%d want 1", got)
	}
}

func TestGroup_NonSwapGroup_NoStop(t *testing.T) {
	a := newFakeProcess("a")
	a.markReady()

	b := newFakeProcess("b")
	b.autoReady = true

	conf := config.Config{
		HealthCheckTimeout: 5,
		Routing: groupRouting(map[string]config.GroupConfig{
			"g": {Swap: false, Exclusive: false, Members: []string{"a", "b"}},
		}),
	}
	g := newTestGroup(t, conf, map[string]process.Process{"a": a, "b": b})

	w := httptest.NewRecorder()
	g.ServeHTTP(w, newRequest("b"))

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%q", w.Code, w.Body.String())
	}
	if got := a.stopCalls.Load(); got != 0 {
		t.Errorf("a.stopCalls=%d want 0 (swap=false should not stop siblings)", got)
	}
	if got := b.runCalls.Load(); got != 1 {
		t.Errorf("b.runCalls=%d want 1", got)
	}
}

func TestGroup_CrossGroupExclusive(t *testing.T) {
	a := newFakeProcess("a")
	a.markReady()
	go a.Run(0)

	b := newFakeProcess("b")
	b.autoReady = true

	conf := config.Config{
		HealthCheckTimeout: 5,
		Routing: groupRouting(map[string]config.GroupConfig{
			"g1": {Swap: true, Exclusive: true, Members: []string{"a"}},
			"g2": {Swap: true, Exclusive: true, Members: []string{"b"}},
		}),
	}
	g := newTestGroup(t, conf, map[string]process.Process{"a": a, "b": b})

	w := httptest.NewRecorder()
	g.ServeHTTP(w, newRequest("b"))

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%q", w.Code, w.Body.String())
	}
	if got := a.stopCalls.Load(); got != 1 {
		t.Errorf("a.stopCalls=%d want 1 (cross-group exclusive must stop)", got)
	}
}

// TestGroup_CrossGroupNonExclusiveParallel verifies that two requests for
// models in distinct non-exclusive groups load in parallel rather than
// serializing through the router's run loop.
func TestGroup_CrossGroupNonExclusiveParallel(t *testing.T) {
	a := newFakeProcess("a")
	pb := newFakeProcess("b")

	conf := config.Config{
		HealthCheckTimeout: 5,
		Routing: groupRouting(map[string]config.GroupConfig{
			"g1": {Swap: true, Exclusive: false, Members: []string{"a"}},
			"g2": {Swap: true, Exclusive: false, Members: []string{"b"}},
		}),
	}
	g := newTestGroup(t, conf, map[string]process.Process{"a": a, "b": pb})

	w1 := httptest.NewRecorder()
	done1 := make(chan struct{})
	go func() {
		g.ServeHTTP(w1, newRequest("a"))
		close(done1)
	}()
	waitProcessed(t, g.testProcessed, 1)

	w2 := httptest.NewRecorder()
	done2 := make(chan struct{})
	go func() {
		g.ServeHTTP(w2, newRequest("b"))
		close(done2)
	}()
	waitProcessed(t, g.testProcessed, 1)

	// Both groups load concurrently — both must reach Run() before either is
	// marked ready. If the router still serialised, only one would proceed.
	<-a.runStarted
	<-pb.runStarted

	a.markReady()
	pb.markReady()

	for i, ch := range []chan struct{}{done1, done2} {
		select {
		case <-ch:
		case <-time.After(time.Second):
			t.Fatalf("request %d did not complete", i)
		}
	}
	if got := a.stopCalls.Load(); got != 0 {
		t.Errorf("a.stopCalls=%d want 0 (parallel groups don't evict each other)", got)
	}
	if got := pb.stopCalls.Load(); got != 0 {
		t.Errorf("b.stopCalls=%d want 0 (parallel groups don't evict each other)", got)
	}
}

// TestGroup_SameGroupSwapSerialises verifies that two same-group requests
// (Swap=true) serialise even when both arrive while neither has reached
// StateStarting yet — the in-flight swap target the scheduler folds into the
// running set closes that race.
func TestGroup_SameGroupSwapSerialises(t *testing.T) {
	a := newFakeProcess("a")
	pb := newFakeProcess("b")

	conf := config.Config{
		HealthCheckTimeout: 5,
		Routing: groupRouting(map[string]config.GroupConfig{
			"g": {Swap: true, Exclusive: false, Members: []string{"a", "b"}},
		}),
	}
	g := newTestGroup(t, conf, map[string]process.Process{"a": a, "b": pb})

	w1 := httptest.NewRecorder()
	done1 := make(chan struct{})
	go func() {
		g.ServeHTTP(w1, newRequest("a"))
		close(done1)
	}()
	waitProcessed(t, g.testProcessed, 1)

	// Request B arrives before A transitions to StateStarting in the process
	// state machine. Without folding the in-flight swap target into the running
	// set, the swapper would not see A as running, and B would start in
	// parallel, violating Swap=true.
	w2 := httptest.NewRecorder()
	done2 := make(chan struct{})
	go func() {
		g.ServeHTTP(w2, newRequest("b"))
		close(done2)
	}()
	waitProcessed(t, g.testProcessed, 1)

	if got := pb.runCalls.Load(); got != 0 {
		t.Errorf("b started in parallel: runCalls=%d want 0", got)
	}

	<-a.runStarted
	a.markReady()
	waitProcessed(t, g.testProcessed, 1) // swapDone(a) → b promoted
	<-pb.runStarted
	pb.markReady()

	for i, ch := range []chan struct{}{done1, done2} {
		select {
		case <-ch:
		case <-time.After(time.Second):
			t.Fatalf("request %d did not complete", i)
		}
	}
	if got := a.stopCalls.Load(); got != 1 {
		t.Errorf("a.stopCalls=%d want 1 (b's swap must stop a)", got)
	}
}

// TestGroup_PersistentNotEvicted verifies that a group with persistent=true
// is never evicted when another exclusive group starts loading. The running
// model in the persistent group stays alive alongside the new one.
func TestGroup_PersistentNotEvicted(t *testing.T) {
	a := newFakeProcess("a")
	a.markReady()
	go a.Run(0)

	b := newFakeProcess("b")
	b.autoReady = true

	conf := config.Config{
		HealthCheckTimeout: 5,
		Routing: groupRouting(map[string]config.GroupConfig{
			"persist": {Swap: true, Exclusive: false, Persistent: true, Members: []string{"a"}},
			"other":   {Swap: true, Exclusive: true, Members: []string{"b"}},
		}),
	}
	g := newTestGroup(t, conf, map[string]process.Process{"a": a, "b": b})

	w := httptest.NewRecorder()
	g.ServeHTTP(w, newRequest("b"))

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%q", w.Code, w.Body.String())
	}
	if got := a.stopCalls.Load(); got != 0 {
		t.Errorf("a.stopCalls=%d want 0 (persistent group must not be evicted)", got)
	}
	if a.State() != process.StateStarting && a.State() != process.StateReady {
		t.Errorf("a state=%s want still running", a.State())
	}
	if got := b.runCalls.Load(); got != 1 {
		t.Errorf("b.runCalls=%d want 1", got)
	}
}

// TestGroup_NonExclusiveUnloadsExclusive verifies bidirectional exclusivity:
// loading a model in a non-exclusive group unloads any running exclusive
// non-persistent group, so the two never coexist. See issue #215.
func TestGroup_NonExclusiveUnloadsExclusive(t *testing.T) {
	a := newFakeProcess("a")
	a.markReady()
	go a.Run(0)

	b := newFakeProcess("b")
	b.autoReady = true

	conf := config.Config{
		HealthCheckTimeout: 5,
		Routing: groupRouting(map[string]config.GroupConfig{
			"g1": {Swap: true, Exclusive: true, Members: []string{"a"}},
			"g2": {Swap: true, Exclusive: false, Members: []string{"b"}},
		}),
	}
	g := newTestGroup(t, conf, map[string]process.Process{"a": a, "b": b})

	w := httptest.NewRecorder()
	g.ServeHTTP(w, newRequest("b"))

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%q", w.Code, w.Body.String())
	}
	if got := a.stopCalls.Load(); got != 1 {
		t.Errorf("a.stopCalls=%d want 1 (non-exclusive target must unload exclusive group)", got)
	}
	if got := b.runCalls.Load(); got != 1 {
		t.Errorf("b.runCalls=%d want 1", got)
	}
}

// TestGroup_NonExclusiveDoesNotUnloadPersistentExclusive verifies that
// bidirectional exclusivity still respects persistent=true: a non-exclusive
// target does not evict a running exclusive group that is persistent.
func TestGroup_NonExclusiveDoesNotUnloadPersistentExclusive(t *testing.T) {
	a := newFakeProcess("a")
	a.markReady()
	go a.Run(0)

	b := newFakeProcess("b")
	b.autoReady = true

	conf := config.Config{
		HealthCheckTimeout: 5,
		Routing: groupRouting(map[string]config.GroupConfig{
			"g1": {Swap: true, Exclusive: true, Persistent: true, Members: []string{"a"}},
			"g2": {Swap: true, Exclusive: false, Members: []string{"b"}},
		}),
	}
	g := newTestGroup(t, conf, map[string]process.Process{"a": a, "b": b})

	w := httptest.NewRecorder()
	g.ServeHTTP(w, newRequest("b"))

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%q", w.Code, w.Body.String())
	}
	if got := a.stopCalls.Load(); got != 0 {
		t.Errorf("a.stopCalls=%d want 0 (persistent exclusive group must not be evicted)", got)
	}
	if a.State() != process.StateStarting && a.State() != process.StateReady {
		t.Errorf("a state=%s want still running", a.State())
	}
	if got := b.runCalls.Load(); got != 1 {
		t.Errorf("b.runCalls=%d want 1", got)
	}
}

// TestGroup_SamePoolExclusiveEvicts verifies that two exclusive groups in the
// same named pool still evict each other. See issue #632.
func TestGroup_SamePoolExclusiveEvicts(t *testing.T) {
	a := newFakeProcess("a")
	a.markReady()
	go a.Run(0)

	b := newFakeProcess("b")
	b.autoReady = true

	conf := config.Config{
		HealthCheckTimeout: 5,
		Routing: groupRouting(map[string]config.GroupConfig{
			"g1": {Swap: true, Exclusive: true, Pool: "GPU-0", Members: []string{"a"}},
			"g2": {Swap: true, Exclusive: true, Pool: "GPU-0", Members: []string{"b"}},
		}),
	}
	g := newTestGroup(t, conf, map[string]process.Process{"a": a, "b": b})

	w := httptest.NewRecorder()
	g.ServeHTTP(w, newRequest("b"))

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%q", w.Code, w.Body.String())
	}
	if got := a.stopCalls.Load(); got != 1 {
		t.Errorf("a.stopCalls=%d want 1 (same-pool exclusive must evict)", got)
	}
}

// TestGroup_DifferentPoolsIsolated verifies that exclusive groups in different
// named pools do not evict each other. See issue #632.
func TestGroup_DifferentPoolsIsolated(t *testing.T) {
	a := newFakeProcess("a")
	a.markReady()
	go a.Run(0)

	b := newFakeProcess("b")
	b.autoReady = true

	conf := config.Config{
		HealthCheckTimeout: 5,
		Routing: groupRouting(map[string]config.GroupConfig{
			"g1": {Swap: true, Exclusive: true, Pool: "GPU-0", Members: []string{"a"}},
			"g2": {Swap: true, Exclusive: true, Pool: "GPU-1", Members: []string{"b"}},
		}),
	}
	g := newTestGroup(t, conf, map[string]process.Process{"a": a, "b": b})

	w := httptest.NewRecorder()
	g.ServeHTTP(w, newRequest("b"))

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%q", w.Code, w.Body.String())
	}
	if got := a.stopCalls.Load(); got != 0 {
		t.Errorf("a.stopCalls=%d want 0 (different pools must not evict)", got)
	}
	if got := b.runCalls.Load(); got != 1 {
		t.Errorf("b.runCalls=%d want 1", got)
	}
}

// TestGroup_GlobalPoolInteractsWithNamed verifies that a group with no pool is
// global and still evicts a named-pool group (and vice versa). See issue #632.
func TestGroup_GlobalPoolInteractsWithNamed(t *testing.T) {
	a := newFakeProcess("a")
	a.markReady()
	go a.Run(0)

	b := newFakeProcess("b")
	b.autoReady = true

	conf := config.Config{
		HealthCheckTimeout: 5,
		Routing: groupRouting(map[string]config.GroupConfig{
			"g1": {Swap: true, Exclusive: true, Pool: "GPU-0", Members: []string{"a"}},
			"g2": {Swap: true, Exclusive: true, Members: []string{"b"}}, // no pool = global
		}),
	}
	g := newTestGroup(t, conf, map[string]process.Process{"a": a, "b": b})

	w := httptest.NewRecorder()
	g.ServeHTTP(w, newRequest("b"))

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%q", w.Code, w.Body.String())
	}
	if got := a.stopCalls.Load(); got != 1 {
		t.Errorf("a.stopCalls=%d want 1 (global pool must interact with named pool)", got)
	}
}

// TestGroup_DifferentPoolsBidirectionalIsolated verifies that pool scoping also
// gates the bidirectional case: a non-exclusive group in one pool does not
// evict an exclusive group in a different pool. See issues #215 and #632.
func TestGroup_DifferentPoolsBidirectionalIsolated(t *testing.T) {
	a := newFakeProcess("a")
	a.markReady()
	go a.Run(0)

	b := newFakeProcess("b")
	b.autoReady = true

	conf := config.Config{
		HealthCheckTimeout: 5,
		Routing: groupRouting(map[string]config.GroupConfig{
			"g1": {Swap: true, Exclusive: true, Pool: "GPU-0", Members: []string{"a"}},
			"g2": {Swap: true, Exclusive: false, Pool: "GPU-1", Members: []string{"b"}},
		}),
	}
	g := newTestGroup(t, conf, map[string]process.Process{"a": a, "b": b})

	w := httptest.NewRecorder()
	g.ServeHTTP(w, newRequest("b"))

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%q", w.Code, w.Body.String())
	}
	if got := a.stopCalls.Load(); got != 0 {
		t.Errorf("a.stopCalls=%d want 0 (different pools must not evict, even bidirectionally)", got)
	}
}
