package service

import (
	"context"
	"time"

	"github.com/cpave3/legato/internal/engine/manticore"
)

// usageHours is how far back the hourly usage reaches.
const usageHours = 24

// Usage is how many tokens the agents used, as a Manticore instance recorded
// them. It counts every Client of that instance, not only this Legato.
type Usage struct {
	// TokensToday and RequestsToday count from local midnight. The tokens
	// are those that went in, whether read from a cache or not, and those
	// that came out.
	TokensToday       int64
	InputTokensToday  int64
	OutputTokensToday int64
	RequestsToday     int
	// Hourly holds the tokens of each of the last 24 hours, oldest first;
	// the last is the hour now running. It is nil when it could not be read.
	Hourly []int64
}

// UsageService reports token usage. It exists only when a Manticore instance
// is configured; without one there is nowhere to read usage from.
type UsageService interface {
	Usage(ctx context.Context) (Usage, error)
}

// UsageSource is where usage is read from.
type UsageSource interface {
	Summary(ctx context.Context, since time.Time) (manticore.Summary, error)
	Hourly(ctx context.Context, since time.Time) ([]manticore.Bucket, error)
}

type usageService struct {
	source UsageSource
	now    func() time.Time
}

// NewUsageService reports the usage a source recorded.
func NewUsageService(source UsageSource) UsageService {
	return &usageService{source: source, now: time.Now}
}

func (s *usageService) Usage(ctx context.Context) (Usage, error) {
	now := s.now()
	midnight := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	summary, err := s.source.Summary(ctx, midnight)
	if err != nil {
		return Usage{}, err
	}
	usage := Usage{
		TokensToday:       summary.TotalTokens,
		InputTokensToday:  summary.PromptTokens,
		OutputTokensToday: summary.CompletionTokens,
		RequestsToday:     summary.Requests,
	}

	// The total is worth showing without the hours, so a failure here is not
	// one of the whole.
	first := now.UTC().Truncate(time.Hour).Add(-(usageHours - 1) * time.Hour)
	buckets, err := s.source.Hourly(ctx, first)
	if err != nil {
		return usage, nil
	}
	usage.Hourly = make([]int64, usageHours)
	for _, bucket := range buckets {
		if hour := int(bucket.Start.Sub(first) / time.Hour); hour >= 0 && hour < usageHours {
			usage.Hourly[hour] += bucket.Tokens()
		}
	}
	return usage, nil
}
