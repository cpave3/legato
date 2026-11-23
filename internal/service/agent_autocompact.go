package service

import (
	"context"
	"log"
	"sync"
	"time"
)

// AutoCompactCheckEvery is how often Legato looks for agents to compact when
// agents.compact.auto_percent is set.
const AutoCompactCheckEvery = 15 * time.Second

// AutoCompactAgent is a running agent as the auto-compactor sees it.
type AutoCompactAgent struct {
	TaskID string
	// Idle is true between turns: nothing runs that a compaction would
	// interrupt or be refused for.
	Idle bool
	// Used of Window tokens fill its context; Window 0 when unknown.
	Used, Window int
}

// AutoCompactorOptions wire an AutoCompactor.
type AutoCompactorOptions struct {
	// Percent is agents.compact.auto_percent, read at each check; 0 is off.
	Percent func(ctx context.Context) int
	// Agents are the running agents Legato can compact, with how full each
	// one's context is.
	Agents func(ctx context.Context) ([]AutoCompactAgent, error)
	// Compact compacts one, as AgentCompactor.CompactAgent does.
	Compact func(ctx context.Context, taskID string) error
}

// AutoCompactor compacts idle agents whose context is at least
// agents.compact.auto_percent full. An agent it tried is not tried again
// until its context has grown, so a compaction that fails, or that leaves the
// context over the threshold, is not repeated at every check. One that
// drops under the threshold is forgotten, so the next fill is compacted too.
type AutoCompactor struct {
	opts AutoCompactorOptions

	mu sync.Mutex
	// tried is each agent's context size when it was last compacted.
	tried map[string]int
}

// NewAutoCompactor makes an auto-compactor.
func NewAutoCompactor(opts AutoCompactorOptions) *AutoCompactor {
	return &AutoCompactor{opts: opts, tried: map[string]int{}}
}

// Check compacts the agents that are due, and returns how many it asked.
func (c *AutoCompactor) Check(ctx context.Context) int {
	percent := c.opts.Percent(ctx)
	if percent <= 0 {
		return 0
	}
	agents, err := c.opts.Agents(ctx)
	if err != nil {
		log.Printf("auto-compact: listing agents: %v", err)
		return 0
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	running := map[string]bool{}
	asked := 0
	for _, agent := range agents {
		running[agent.TaskID] = true
		if agent.Window <= 0 {
			continue
		}
		full := agent.Used*100 >= percent*agent.Window
		tried, wasTried := c.tried[agent.TaskID]
		if !full {
			delete(c.tried, agent.TaskID)
			continue
		}
		if !agent.Idle || (wasTried && agent.Used <= tried) {
			continue
		}
		c.tried[agent.TaskID] = agent.Used
		asked++
		if err := c.opts.Compact(ctx, agent.TaskID); err != nil {
			log.Printf("auto-compact: %s at %d of %d tokens: %v", agent.TaskID, agent.Used, agent.Window, err)
		}
	}
	for taskID := range c.tried {
		if !running[taskID] {
			delete(c.tried, taskID)
		}
	}
	return asked
}
