package store

import (
	"context"

	"github.com/cpave3/legato/internal/engine/analytics"
)

func (s *Store) QueryDurations(ctx context.Context, period analytics.TimeRange) (analytics.DurationResult, error) {
	return analytics.QueryDurations(ctx, s.db, period)
}

func (s *Store) QueryDailyBreakdown(ctx context.Context, period analytics.TimeRange) ([]analytics.DayEntry, error) {
	return analytics.QueryDailyBreakdown(ctx, s.db, period)
}

func (s *Store) QueryTaskBreakdown(ctx context.Context, period analytics.TimeRange) ([]analytics.TaskDuration, error) {
	return analytics.QueryTaskBreakdown(ctx, s.db, period)
}

func (s *Store) QueryThroughput(ctx context.Context, period analytics.TimeRange) (analytics.Throughput, error) {
	return analytics.QueryThroughput(ctx, s.db, period)
}

func (s *Store) QueryWorkspaceBreakdown(ctx context.Context, period analytics.TimeRange) ([]analytics.WorkspaceBreakdown, error) {
	return analytics.QueryWorkspaceBreakdown(ctx, s.db, period)
}

func (s *Store) QueryDirectoryBreakdown(ctx context.Context, period analytics.TimeRange) ([]analytics.DirectoryBreakdown, error) {
	return analytics.QueryDirectoryBreakdown(ctx, s.db, period)
}

func (s *Store) QuerySwarmBreakdown(ctx context.Context, period analytics.TimeRange) ([]analytics.SwarmBreakdown, error) {
	return analytics.QuerySwarmBreakdown(ctx, s.db, period)
}
