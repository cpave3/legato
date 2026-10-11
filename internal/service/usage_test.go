package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/engine/manticore"
)

type fakeUsageSource struct {
	summary      manticore.Summary
	summaryErr   error
	hourly       []manticore.Bucket
	hourlyErr    error
	summarySince time.Time
	hourlySince  time.Time
}

func (f *fakeUsageSource) Summary(_ context.Context, since time.Time) (manticore.Summary, error) {
	f.summarySince = since
	return f.summary, f.summaryErr
}

func (f *fakeUsageSource) Hourly(_ context.Context, since time.Time) ([]manticore.Bucket, error) {
	f.hourlySince = since
	return f.hourly, f.hourlyErr
}

func usageAt(source UsageSource, now time.Time) UsageService {
	svc := NewUsageService(source).(*usageService)
	svc.now = func() time.Time { return now }
	return svc
}

func TestUsageCountsTheTokensSinceLocalMidnight(t *testing.T) {
	zone := time.FixedZone("AEDT", 11*60*60)
	source := &fakeUsageSource{summary: manticore.Summary{Requests: 12, PromptTokens: 1_390_000, CompletionTokens: 10_000, TotalTokens: 1_400_000}}

	got, err := usageAt(source, time.Date(2026, 10, 11, 17, 44, 30, 0, zone)).Usage(context.Background())

	if err != nil {
		t.Fatal(err)
	}
	if got.TokensToday != 1_400_000 || got.RequestsToday != 12 {
		t.Errorf("usage = %+v", got)
	}
	if got.InputTokensToday != 1_390_000 || got.OutputTokensToday != 10_000 {
		t.Errorf("usage = %+v, want what went in and what came out apart", got)
	}
	if want := time.Date(2026, 10, 11, 0, 0, 0, 0, zone); !source.summarySince.Equal(want) {
		t.Errorf("asked for usage since %v, want %v", source.summarySince, want)
	}
}

func TestUsageLaysTheLastDayOutHourByHour(t *testing.T) {
	now := time.Date(2026, 10, 11, 6, 44, 0, 0, time.UTC)
	source := &fakeUsageSource{hourly: []manticore.Bucket{
		{Start: time.Date(2026, 10, 10, 7, 0, 0, 0, time.UTC), PromptTokens: 100, CompletionTokens: 5},
		{Start: time.Date(2026, 10, 11, 6, 0, 0, 0, time.UTC), PromptTokens: 900, CompletionTokens: 50},
	}}

	got, err := usageAt(source, now).Usage(context.Background())

	if err != nil {
		t.Fatal(err)
	}
	if len(got.Hourly) != 24 {
		t.Fatalf("got %d hours, want 24: %v", len(got.Hourly), got.Hourly)
	}
	if got.Hourly[0] != 105 || got.Hourly[23] != 950 {
		t.Errorf("first and last hour = %d and %d, want 105 and 950", got.Hourly[0], got.Hourly[23])
	}
	for hour := 1; hour < 23; hour++ {
		if got.Hourly[hour] != 0 {
			t.Errorf("hour %d = %d, want none", hour, got.Hourly[hour])
		}
	}
	if want := time.Date(2026, 10, 10, 7, 0, 0, 0, time.UTC); !source.hourlySince.Equal(want) {
		t.Errorf("asked for hours since %v, want %v", source.hourlySince, want)
	}
}

func TestUsageWithoutItsHoursIsStillUsage(t *testing.T) {
	source := &fakeUsageSource{summary: manticore.Summary{TotalTokens: 42}, hourlyErr: errors.New("timed out")}

	got, err := usageAt(source, time.Now()).Usage(context.Background())

	if err != nil || got.TokensToday != 42 || got.Hourly != nil {
		t.Errorf("usage = %+v, err = %v; want the total and no hours", got, err)
	}
}

func TestUsageThatCannotBeReadIsAnError(t *testing.T) {
	source := &fakeUsageSource{summaryErr: errors.New("connection refused")}

	if _, err := usageAt(source, time.Now()).Usage(context.Background()); err == nil {
		t.Fatal("want an error when the total cannot be read")
	}
}
