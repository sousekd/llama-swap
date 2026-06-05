package router

import (
	"fmt"

	"github.com/mostlygeek/llama-swap/internal/config"
	"github.com/mostlygeek/llama-swap/internal/logmon"
	"github.com/mostlygeek/llama-swap/internal/process"
)

type Group struct {
	*baseRouter
}

func NewGroup(conf config.Config, proxylog, upstreamlog *logmon.Monitor) (*Group, error) {
	modelToGroup := make(map[string]string)
	for gid, gcfg := range conf.Routing.Router.Settings.Groups {
		for _, mid := range gcfg.Members {
			if existing, dup := modelToGroup[mid]; dup {
				return nil, fmt.Errorf("model %q is in multiple groups: %q and %q", mid, existing, gid)
			}
			modelToGroup[mid] = gid
		}
	}

	swapper := &groupSwapper{
		config:       conf,
		modelToGroup: modelToGroup,
	}

	processes := make(map[string]process.Process, len(modelToGroup))
	base, err := newBaseRouter("group", conf, processes, proxylog, swapper)
	if err != nil {
		return nil, fmt.Errorf("creating base router: %w", err)
	}

	for mid := range modelToGroup {
		modelCfg, _, ok := conf.FindConfig(mid)
		if !ok {
			base.shutdownFn()
			base.procCancel()
			return nil, fmt.Errorf("no model config for %q", mid)
		}
		procLog := logmon.NewWriter(upstreamlog)
		p, err := process.New(base.procCtx, mid, modelCfg, procLog, proxylog)
		if err != nil {
			base.shutdownFn()
			base.procCancel()
			return nil, fmt.Errorf("creating process for %q: %w", mid, err)
		}
		processes[mid] = p
	}

	g := &Group{baseRouter: base}
	go base.run()
	return g, nil
}

// groupSwapper decides evictions from static group configuration.
//
// Same-group siblings are stopped when the group has swap=true. Cross-group
// exclusivity is bidirectional: loading a model from an exclusive group stops
// any other non-persistent group, and loading a model from a non-exclusive
// group stops any running exclusive non-persistent group. persistent=true
// shields a group from eviction in both directions. See issue #215.
//
// Cross-group eviction is further scoped by pools: two groups interact only
// when their pools interact (see poolsInteract). See issue #632.
type groupSwapper struct {
	config       config.Config
	modelToGroup map[string]string
}

// poolsInteract reports whether two pools can evict each other. An empty pool
// is global and interacts with every pool; named pools interact only with the
// same name or with the global pool. See issue #632.
func poolsInteract(a, b string) bool {
	return a == "" || b == "" || a == b
}

func (p *groupSwapper) EvictionFor(target string, running []string) []string {
	tg := p.modelToGroup[target]
	tgCfg := p.config.Routing.Router.Settings.Groups[tg]

	seen := make(map[string]struct{})
	var result []string
	consider := func(mID string) {
		if mID == target {
			return
		}
		if _, dup := seen[mID]; dup {
			return
		}
		og := p.modelToGroup[mID]
		ogCfg := p.config.Routing.Router.Settings.Groups[og]
		switch {
		case og == tg && tgCfg.Swap:
			seen[mID] = struct{}{}
			result = append(result, mID)
		// An exclusive target stops every other non-persistent group in an
		// interacting pool.
		case og != tg && tgCfg.Exclusive && !ogCfg.Persistent && poolsInteract(tgCfg.Pool, ogCfg.Pool):
			seen[mID] = struct{}{}
			result = append(result, mID)
		// Bidirectional exclusivity: a non-exclusive target still stops any
		// running exclusive non-persistent group in an interacting pool, so the
		// two never coexist.
		case og != tg && !tgCfg.Exclusive && ogCfg.Exclusive && !ogCfg.Persistent && poolsInteract(tgCfg.Pool, ogCfg.Pool):
			seen[mID] = struct{}{}
			result = append(result, mID)
		}
	}

	for _, mID := range running {
		consider(mID)
	}
	return result
}

func (p *groupSwapper) OnSwapStart(target string, running []string) {}
