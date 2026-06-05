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
	for gid, gcfg := range conf.Groups {
		for _, mid := range gcfg.Members {
			if existing, dup := modelToGroup[mid]; dup {
				return nil, fmt.Errorf("model %q is in multiple groups: %q and %q", mid, existing, gid)
			}
			modelToGroup[mid] = gid
		}
	}

	planner := &groupPlanner{
		config:       conf,
		modelToGroup: modelToGroup,
	}

	processes := make(map[string]process.Process, len(modelToGroup))
	base := newBaseRouter("group", conf, processes, planner, proxylog)
	planner.processes = processes

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

// groupPlanner decides evictions from static group configuration.
//
// Same-group siblings are stopped when the group has swap=true. Cross-group
// exclusivity is bidirectional: loading a model from an exclusive group stops
// any other non-persistent group, and loading a model from a non-exclusive
// group stops any running exclusive non-persistent group. persistent=true
// shields a group from eviction in both directions. See issue #215.
type groupPlanner struct {
	config       config.Config
	modelToGroup map[string]string
	processes    map[string]process.Process
}

func (p *groupPlanner) EvictionFor(target string, alsoRunning []string) []string {
	tg := p.modelToGroup[target]
	tgCfg := p.config.Groups[tg]

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
		ogCfg := p.config.Groups[og]
		switch {
		case og == tg && tgCfg.Swap:
			seen[mID] = struct{}{}
			result = append(result, mID)
		// An exclusive target stops every other non-persistent group.
		case og != tg && tgCfg.Exclusive && !ogCfg.Persistent:
			seen[mID] = struct{}{}
			result = append(result, mID)
		// Bidirectional exclusivity: a non-exclusive target still stops any
		// running exclusive non-persistent group, so the two never coexist.
		case og != tg && !tgCfg.Exclusive && ogCfg.Exclusive && !ogCfg.Persistent:
			seen[mID] = struct{}{}
			result = append(result, mID)
		}
	}

	for mID, proc := range p.processes {
		st := proc.State()
		if st == process.StateStopped || st == process.StateShutdown {
			continue
		}
		consider(mID)
	}
	for _, mID := range alsoRunning {
		consider(mID)
	}
	return result
}

func (p *groupPlanner) OnSwapStart(target string) {}
