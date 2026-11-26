package service

import (
	"context"
	"errors"

	"github.com/cpave3/legato/internal/engine/claudeusage"
	"github.com/cpave3/legato/internal/engine/codexusage"
)

// ClaudeUsageReader reads Claude's subscription usage from Claude Code's
// OAuth usage endpoint (engine/claudeusage), with Claude Code's login.
type ClaudeUsageReader struct {
	Client *claudeusage.Client
}

func (r *ClaudeUsageReader) Provider() string { return UsageProviderClaude }

var claudeMeters = map[string]string{
	claudeusage.FiveHour:       UsageMeterFiveHour,
	claudeusage.SevenDay:       UsageMeterWeekly,
	claudeusage.SevenDayOpus:   UsageMeterWeeklyOpus,
	claudeusage.SevenDaySonnet: UsageMeterWeeklySonnet,
}

func (r *ClaudeUsageReader) Read(ctx context.Context) (ProviderUsage, error) {
	reading, err := r.Client.Read(ctx)
	if err != nil {
		return ProviderUsage{}, claudeUsageError(err)
	}
	usage := ProviderUsage{Source: "endpoint"}
	for _, w := range reading.Windows {
		meter, ok := claudeMeters[w.Name]
		if !ok {
			continue
		}
		usage.Windows = append(usage.Windows, UsageWindow{Meter: meter, UsedPercent: w.UsedPercent, ResetsAt: w.ResetsAt})
	}
	return usage, nil
}

func claudeUsageError(err error) error {
	var limited *claudeusage.RateLimitedError
	switch {
	case errors.Is(err, context.Canceled):
		return err
	case errors.As(err, &limited):
		return &UsageUnavailableError{Reason: UsageReasonRateLimited, RetryAfter: limited.RetryAfter}
	case errors.Is(err, claudeusage.ErrNotSignedIn):
		return &UsageUnavailableError{Reason: UsageReasonNotSignedIn}
	case errors.Is(err, claudeusage.ErrLoginExpired):
		return &UsageUnavailableError{Reason: UsageReasonLoginExpired}
	case errors.Is(err, claudeusage.ErrNoSubscription):
		return &UsageUnavailableError{Reason: UsageReasonNoSubscription}
	case errors.Is(err, claudeusage.ErrEndpointChanged):
		return &UsageUnavailableError{Reason: UsageReasonEndpointChanged}
	default:
		// The engine's errors never hold the token or the endpoint's body.
		return &UsageUnavailableError{Reason: UsageReasonError}
	}
}

// CodexAppServerReader is what CodexUsageReader asks first.
type CodexAppServerReader interface {
	Read(ctx context.Context) (codexusage.Reading, error)
}

// CodexUsageReader reads Codex's subscription usage: from a short-lived
// `codex app-server`, else from the newest rate limits in Codex's session
// logs. It assumes Chimera's Codex models use the same ChatGPT account.
type CodexUsageReader struct {
	AppServer   CodexAppServerReader
	SessionsDir string
}

func (r *CodexUsageReader) Provider() string { return UsageProviderCodex }

func (r *CodexUsageReader) Read(ctx context.Context) (ProviderUsage, error) {
	reading, appErr := r.AppServer.Read(ctx)
	if appErr == nil {
		return codexUsage(reading), nil
	}
	if errors.Is(appErr, context.Canceled) {
		return ProviderUsage{}, appErr
	}
	if !errors.Is(appErr, codexusage.ErrNotSignedIn) {
		if logged, err := codexusage.ReadLogs(r.SessionsDir); err == nil {
			return codexUsage(logged), nil
		}
	}
	switch {
	case errors.Is(appErr, codexusage.ErrCodexMissing):
		return ProviderUsage{}, &UsageUnavailableError{Reason: UsageReasonCodexMissing}
	case errors.Is(appErr, codexusage.ErrNotSignedIn):
		return ProviderUsage{}, &UsageUnavailableError{Reason: UsageReasonNotSignedIn}
	case errors.Is(appErr, codexusage.ErrNoData):
		return ProviderUsage{}, &UsageUnavailableError{Reason: UsageReasonNoData}
	case errors.Is(appErr, codexusage.ErrUnsupported):
		return ProviderUsage{}, &UsageUnavailableError{Reason: UsageReasonEndpointChanged}
	default:
		return ProviderUsage{}, &UsageUnavailableError{Reason: UsageReasonError}
	}
}

// codexUsage names each window by its length; one of neither length is
// left out.
func codexUsage(reading codexusage.Reading) ProviderUsage {
	usage := ProviderUsage{Source: reading.Source, Plan: reading.PlanType}
	if !reading.AsOf.IsZero() {
		at := reading.AsOf
		usage.AsOf = &at
	}
	for _, w := range reading.Windows {
		var meter string
		switch codexusage.Classify(w.Minutes) {
		case codexusage.FiveHour:
			meter = UsageMeterFiveHour
		case codexusage.Weekly:
			meter = UsageMeterWeekly
		default:
			continue
		}
		usage.Windows = append(usage.Windows, UsageWindow{Meter: meter, UsedPercent: w.UsedPercent, ResetsAt: w.ResetsAt, WindowMinutes: w.Minutes})
	}
	if c := reading.Credits; c != nil {
		usage.Credits = &UsageCredits{HasCredits: c.HasCredits, Unlimited: c.Unlimited, Balance: c.Balance}
	}
	return usage
}
