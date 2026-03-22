package service

import (
	"context"
	"sort"

	"github.com/cpave3/legato/internal/engine/analytics"
	"github.com/cpave3/legato/internal/engine/store"
	"github.com/jmoiron/sqlx"
)

type reportService struct {
	db    *sqlx.DB
	store *store.Store
}

// NewReportService creates a new ReportService.
func NewReportService(s *store.Store) ReportService {
	return &reportService{db: s.DB(), store: s}
}

func (r *reportService) GenerateReport(ctx context.Context, period analytics.TimeRange) (*Report, error) {
	// Run all analytics queries
	durations, err := analytics.QueryDurations(ctx, r.db, period)
	if err != nil {
		return nil, err
	}

	dailyBreakdown, err := analytics.QueryDailyBreakdown(ctx, r.db, period)
	if err != nil {
		return nil, err
	}

	taskBreakdown, err := analytics.QueryTaskBreakdown(ctx, r.db, period)
	if err != nil {
		return nil, err
	}

	throughput, err := analytics.QueryThroughput(ctx, r.db, period)
	if err != nil {
		return nil, err
	}

	wsBreakdown, err := analytics.QueryWorkspaceBreakdown(ctx, r.db, period)
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
	}, nil
}
