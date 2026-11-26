package service

import (
	"context"
	"time"

	"github.com/cpave3/legato/internal/engine/store"
)

// WorkflowStats is what the Workflows page shows of each workflow's runs
// (LEG-657): its newest run, and its runs and failures on each of the
// last days.
type WorkflowStats struct {
	// Days are the dates counted, oldest first, the last one today.
	Days      []string                `json:"days"`
	Workflows map[string]WorkflowStat `json:"workflows"`
}

// WorkflowStat is one workflow's: Days line up with WorkflowStats.Days.
type WorkflowStat struct {
	Last *store.WorkflowRun `json:"last,omitempty"`
	Days []WorkflowDayCount `json:"days"`
}

// WorkflowDayCount is how many runs started on a day, and how many of them
// failed.
type WorkflowDayCount struct {
	Runs   int `json:"runs"`
	Failed int `json:"failed"`
}

// Stats counts the runs of the days days ending on now's date in loc, and
// gives each workflow's newest run, described as the runs list describes
// it.
func (s *WorkflowService) Stats(ctx context.Context, now time.Time, loc *time.Location, days int) (*WorkflowStats, error) {
	now = now.In(loc)
	first := time.Date(now.Year(), now.Month(), now.Day()-(days-1), 0, 0, 0, 0, loc)
	out := &WorkflowStats{Days: make([]string, days), Workflows: map[string]WorkflowStat{}}
	index := map[string]int{}
	for i := range out.Days {
		out.Days[i] = first.AddDate(0, 0, i).Format(time.DateOnly)
		index[out.Days[i]] = i
	}
	stat := func(name string) WorkflowStat {
		existing, ok := out.Workflows[name]
		if !ok {
			existing.Days = make([]WorkflowDayCount, days)
		}
		return existing
	}
	marks, err := s.store.WorkflowRunsSince(ctx, first)
	if err != nil {
		return nil, err
	}
	for _, mark := range marks {
		started, err := time.Parse(time.RFC3339, mark.CreatedAt)
		if err != nil {
			continue
		}
		i, ok := index[started.In(loc).Format(time.DateOnly)]
		if !ok {
			continue
		}
		counted := stat(mark.WorkflowName)
		counted.Days[i].Runs++
		if mark.Status == "failed" {
			counted.Days[i].Failed++
		}
		out.Workflows[mark.WorkflowName] = counted
	}
	latest, err := s.store.LatestWorkflowRuns(ctx)
	if err != nil {
		return nil, err
	}
	s.describeListedRuns(ctx, latest)
	for i := range latest {
		described := stat(latest[i].WorkflowName)
		described.Last = &latest[i]
		out.Workflows[latest[i].WorkflowName] = described
	}
	return out, nil
}
