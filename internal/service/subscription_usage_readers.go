package service

import (
	"context"
	"errors"
	"github.com/cpave3/legato/config"

	"github.com/cpave3/legato/internal/engine/claudeusage"
	"github.com/cpave3/legato/internal/engine/codexusage"
	"github.com/cpave3/legato/internal/engine/opencodeusage"
)

// ClaudeUsageReader reads Claude's subscription usage from Claude Code's
// OAuth usage endpoint (engine/claudeusage), with Claude Code's login.
type ClaudeUsageReader struct {
	Client  *claudeusage.Client
	Account string
	Label   string
}

func (r *ClaudeUsageReader) Provider() string {
	return config.UsageReaderID(UsageProviderClaude, r.Account)
}

func (r *ClaudeUsageReader) usageAccount() (string, string, string) {
	return UsageProviderClaude, r.Account, r.Label
}

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

// claudeUsageReasons are the reasons Claude's usage reader gives, in the
// order they are checked.
var claudeUsageReasons = []usageErrorReason{
	{claudeusage.ErrNotSignedIn, UsageReasonNotSignedIn},
	{claudeusage.ErrNamedKeychainUnavailable, UsageReasonNamedKeychainUnavailable},
	{claudeusage.ErrLoginExpired, UsageReasonLoginExpired},
	{claudeusage.ErrNoSubscription, UsageReasonNoSubscription},
	{claudeusage.ErrEndpointChanged, UsageReasonEndpointChanged},
}

// claudeUsageError explains a failed Claude read. The engine's errors
// never hold the token or the endpoint's body.
func claudeUsageError(err error) error {
	var limited *claudeusage.RateLimitedError
	if !errors.Is(err, context.Canceled) && errors.As(err, &limited) {
		return &UsageUnavailableError{Reason: UsageReasonRateLimited, RetryAfter: limited.RetryAfter}
	}
	return usageError(err, claudeUsageReasons)
}

// usageErrorReason is one engine error a provider's reader explains, and
// the reason it gives.
type usageErrorReason struct {
	err    error
	reason string
}

// usageError explains a failed read that is not a rate limit: cancellation
// passes through, the first matching reason is given, and anything else
// is UsageReasonError, so none of the engine's words reach the person.
func usageError(err error, reasons []usageErrorReason) error {
	if errors.Is(err, context.Canceled) {
		return err
	}
	for _, r := range reasons {
		if errors.Is(err, r.err) {
			return &UsageUnavailableError{Reason: r.reason}
		}
	}
	return &UsageUnavailableError{Reason: UsageReasonError}
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

// OpenCodeUsageReader reads an OpenCode Go subscription's usage from the
// usage endpoint OpenCode documents (engine/opencodeusage), with the API key
// the settings resolved at start. The reading is the account's, so it
// counts every client that uses the key, not only Legato.
type OpenCodeUsageReader struct {
	Client *opencodeusage.Client
}

func (r *OpenCodeUsageReader) Provider() string { return UsageProviderOpenCode }

func (r *OpenCodeUsageReader) Read(ctx context.Context) (ProviderUsage, error) {
	reading, err := r.Client.Read(ctx)
	if err != nil {
		return ProviderUsage{}, opencodeUsageError(err)
	}
	usage := ProviderUsage{Source: "endpoint"}
	for _, m := range reading.Meters {
		var meter string
		switch m.Name {
		case opencodeusage.Rolling:
			meter = UsageMeterFiveHour
		case opencodeusage.Weekly:
			meter = UsageMeterWeekly
		case opencodeusage.Monthly:
			meter = UsageMeterMonthly
		default:
			continue
		}
		usage.Windows = append(usage.Windows, UsageWindow{Meter: meter, UsedPercent: m.UsedPercent, ResetsAt: m.ResetsAt})
	}
	return usage, nil
}

// opencodeUsageReasons are the reasons OpenCode Go's usage reader gives,
// in the order they are checked.
var opencodeUsageReasons = []usageErrorReason{
	{opencodeusage.ErrNoKey, UsageReasonMissingKey},
	{opencodeusage.ErrInvalidKey, UsageReasonInvalidKey},
	{opencodeusage.ErrNoSubscription, UsageReasonNoSubscription},
	{opencodeusage.ErrEndpointChanged, UsageReasonEndpointChanged},
	{opencodeusage.ErrUnreachable, UsageReasonUnreachable},
}

// opencodeUsageError explains a failed OpenCode Go read. The engine's
// errors never hold the key or the endpoint's body.
func opencodeUsageError(err error) error {
	var limited *opencodeusage.RateLimitedError
	if !errors.Is(err, context.Canceled) && errors.As(err, &limited) {
		return &UsageUnavailableError{Reason: UsageReasonRateLimited, RetryAfter: limited.RetryAfter}
	}
	return usageError(err, opencodeUsageReasons)
}
