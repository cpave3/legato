package service

import (
	"context"
	"sort"

	"github.com/cpave3/legato/internal/engine/analytics"
	"github.com/cpave3/legato/internal/engine/store"
)

type reportQueries interface {
	QueryDurations(context.Context, store.ReportRange) (store.DurationResult, error)
	QueryDailyBreakdown(context.Context, store.ReportRange) ([]store.DayEntry, error)
	QueryTaskBreakdown(context.Context, store.ReportRange) ([]store.TaskDuration, error)
	QueryThroughput(context.Context, store.ReportRange) (store.Throughput, error)
	QueryWorkspaceBreakdown(context.Context, store.ReportRange) ([]store.WorkspaceBreakdown, error)
	QueryDirectoryBreakdown(context.Context, store.ReportRange) ([]store.DirectoryBreakdown, error)
	QuerySwarmBreakdown(context.Context, store.ReportRange) ([]store.SwarmBreakdown, error)
}

type reportService struct {
	queries reportQueries
	store   *store.Store
}

// NewReportService creates a new ReportService.
func NewReportService(s *store.Store) ReportService {
	return &reportService{queries: s, store: s}
}

func (r *reportService) GenerateReport(ctx context.Context, period analytics.TimeRange) (*Report, error) {
	queryRange := store.ReportRange{Start: period.Start, End: period.End}
	// Run all analytics queries
	durations, err := r.queries.QueryDurations(ctx, queryRange)
	if err != nil {
		return nil, err
	}

	dailyBreakdown, err := r.queries.QueryDailyBreakdown(ctx, queryRange)
	if err != nil {
		return nil, err
	}

	taskBreakdown, err := r.queries.QueryTaskBreakdown(ctx, queryRange)
	if err != nil {
		return nil, err
	}

	throughput, err := r.queries.QueryThroughput(ctx, queryRange)
	if err != nil {
		return nil, err
	}

	wsBreakdown, err := r.queries.QueryWorkspaceBreakdown(ctx, queryRange)
	if err != nil {
		return nil, err
	}

	dirBreakdown, err := r.queries.QueryDirectoryBreakdown(ctx, queryRange)
	if err != nil {
		return nil, err
	}

	swarmBreakdown, err := r.queries.QuerySwarmBreakdown(ctx, queryRange)
	if err != nil {
		return nil, err
	}

	// Enrich task breakdown with metadata
	taskIDs := make([]string, len(taskBreakdown))
	for i, td := range taskBreakdown {
		taskIDs[i] = td.TaskID
	}

	taskLookup := make(map[string]*store.Task)
	for _, id := range taskIDs {
		t, err := r.store.GetTask(ctx, id)
		if err == nil && t != nil {
			taskLookup[id] = t
		}
	}

	// Build workspace color lookup
	workspaces, _ := r.store.ListWorkspaces(ctx)
	wsColorMap := make(map[int]string)
	wsNameMap := make(map[int]string)
	for _, w := range workspaces {
		if w.Color != nil {
			wsColorMap[w.ID] = *w.Color
		}
		wsNameMap[w.ID] = w.Name
	}

	// Assemble task stats
	taskStats := make([]TaskStats, len(taskBreakdown))
	for i, td := range taskBreakdown {
		ts := TaskStats{
			TaskID:  td.TaskID,
			Working: td.Working,
			Waiting: td.Waiting,
		}
		ts.DisplayID = td.TaskID
		if t, ok := taskLookup[td.TaskID]; ok {
			ts.Title = t.Title
			if t.Provider != nil {
				ts.Provider = *t.Provider
			}
			if t.RemoteID != nil {
				ts.DisplayID = *t.RemoteID
			}
			if t.WorkspaceID != nil {
				ts.WorkspaceName = wsNameMap[*t.WorkspaceID]
			}
		}
		taskStats[i] = ts
	}

	// Sort by working duration descending
	sort.Slice(taskStats, func(i, j int) bool {
		return taskStats[i].Working > taskStats[j].Working
	})

	// Convert daily breakdown
	days := make([]DayBreakdown, len(dailyBreakdown))
	for i, d := range dailyBreakdown {
		days[i] = DayBreakdown{
			Date:    d.Date,
			Working: d.Working,
			Waiting: d.Waiting,
		}
	}

	// Convert workspace breakdown
	wsStats := make([]WorkspaceStats, len(wsBreakdown))
	for i, ws := range wsBreakdown {
		wsStats[i] = WorkspaceStats{
			WorkspaceID:   ws.WorkspaceID,
			WorkspaceName: ws.WorkspaceName,
			Working:       ws.Working,
			Waiting:       ws.Waiting,
			TaskCount:     ws.TaskCount,
		}
		if ws.WorkspaceID != nil {
			wsStats[i].WorkspaceColor = wsColorMap[*ws.WorkspaceID]
		}
	}

	// Convert directory breakdown
	dirStats := make([]DirectoryStats, len(dirBreakdown))
	for i, d := range dirBreakdown {
		dirStats[i] = DirectoryStats{
			Directory: d.Directory,
			Working:   d.Working,
			Waiting:   d.Waiting,
			TaskCount: d.TaskCount,
		}
	}

	// Convert swarm breakdown
	swarmStats := make([]SwarmStats, len(swarmBreakdown))
	for i, s := range swarmBreakdown {
		swarmStats[i] = SwarmStats{
			ParentTaskID:  s.ParentTaskID,
			Title:         s.Title,
			Working:       s.Working,
			Waiting:       s.Waiting,
			WallClock:     s.WallClock,
			ParallelRatio: s.ParallelRatio,
			WorkerCount:   s.WorkerCount,
			SubtaskCount:  s.SubtaskCount,
		}
	}

	return &Report{
		Period: period,
		Summary: ReportSummary{
			TotalWorking:   durations.Working,
			TotalWaiting:   durations.Waiting,
			TasksCompleted: throughput.TasksCompleted,
			TasksCreated:   throughput.TasksCreated,
			AgentSessions:  throughput.AgentSessions,
			PRsMerged:      throughput.PRsMerged,
		},
		ByDay:       days,
		ByTask:      taskStats,
		ByWorkspace: wsStats,
		ByDirectory: dirStats,
		BySwarm:     swarmStats,
	}, nil
}
