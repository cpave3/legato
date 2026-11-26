package main

import (
	"context"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/cpave3/legato/config"
	"github.com/cpave3/legato/internal/engine/ipc"
	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
)

// runUsageCmd prints the subscriptions' usage as the process that polls
// last kept it. --refresh reads now: here when no Legato is polling, else
// by asking the one that is.
func runUsageCmd(args []string) int {
	const command = "usage"
	parsed, parseErr := parseCommandArgs(args, commandFlagSpecs("usage"))
	jsonMode := parsed.Present["json"]
	if parseErr != nil {
		return renderCommandError(command, parseErr, jsonMode)
	}
	if len(parsed.Positionals) != 0 {
		return renderCommandError(command, usageError("unexpected_argument", "usage accepts no positional arguments"), jsonMode)
	}
	cfg, err := config.Load()
	if err != nil {
		return renderCommandError(command, &commandError{Code: "config_failed", Message: err.Error(), Exit: exitEnvironment}, jsonMode)
	}
	db, err := openStore(cfg)
	if err != nil {
		return renderCommandError(command, &commandError{Code: "database_failed", Message: err.Error(), Exit: exitEnvironment}, jsonMode)
	}
	defer db.Close()

	usage := cliSubscriptionUsage(cfg, db)
	if usage == nil {
		snap := service.SubscriptionUsage{Providers: []service.ProviderUsage{}}
		return writeCommandSuccess(command, snap, formatSubscriptionUsage(snap, time.Now()), jsonMode)
	}
	if parsed.Present["refresh"] {
		refreshUsageFromCLI(cfg, usage)
	}
	snap, err := usage.Snapshot(context.Background())
	if err != nil {
		return renderCommandError(command, &commandError{Code: "usage_failed", Message: err.Error(), Exit: exitInternal}, jsonMode)
	}
	return writeCommandSuccess(command, snap, formatSubscriptionUsage(snap, time.Now()), jsonMode)
}

// cliSubscriptionUsage is the service as a short-lived command uses it: no
// bus, and a kept reading announced to the running processes.
func cliSubscriptionUsage(cfg *config.Config, db *store.Store) *service.SubscriptionUsageService {
	readers := usageReaders(cfg)
	if len(readers) == 0 {
		return nil
	}
	return service.NewSubscriptionUsageService(service.SubscriptionUsageOptions{
		Store: db, Readers: readers, PollInterval: cfg.Usage.PollIntervalDuration(),
		OnChange: func() { ipc.Broadcast(ipc.Message{Type: ipcUsageChanged}) },
	})
}

// refreshUsageFromCLI reads here while no process polls (holding its lock
// meanwhile), and otherwise asks the poller and gives it a moment.
func refreshUsageFromCLI(cfg *config.Config, usage *service.SubscriptionUsageService) {
	if release, err := claimUsagePoller(config.ResolveDataDir(cfg)); err == nil {
		defer release()
		usage.Refresh(context.Background())
		return
	}
	ipc.Broadcast(ipc.Message{Type: ipcUsageRefresh})
	time.Sleep(3 * time.Second)
}

// formatSubscriptionUsage is the usage for a person: each provider with how
// fresh its reading is, then its windows.
func formatSubscriptionUsage(snap service.SubscriptionUsage, now time.Time) string {
	if len(snap.Providers) == 0 {
		return "Subscription usage is off (usage.claude.enabled and usage.codex.enabled).\n"
	}
	var b strings.Builder
	for _, p := range snap.Providers {
		name := service.UsageProviderName(p.Provider)
		if p.State == service.UsageUnavailable {
			fmt.Fprintf(&b, "%s: unavailable (%s)\n", name, service.UsageReasonText(p.Reason))
			continue
		}
		if p.Plan != "" {
			name += " (" + p.Plan + ")"
		}
		asOf := ""
		if p.AsOf != nil {
			asOf = "as of " + agoText(now.Sub(*p.AsOf))
		}
		if p.State == service.UsageStale {
			fmt.Fprintf(&b, "%s, stale: %s", name, asOf)
			if p.Reason != "" {
				fmt.Fprintf(&b, " (%s)", service.UsageReasonText(p.Reason))
			}
			b.WriteString("\n")
		} else {
			fmt.Fprintf(&b, "%s, %s\n", name, asOf)
		}
		for _, w := range p.Windows {
			line := fmt.Sprintf("  %-13s %3d%%", service.UsageMeterLabel(w.Meter), int(math.Round(w.UsedPercent)))
			switch {
			case w.ResetsAt.IsZero():
			case w.ResetsAt.After(now):
				line += "  resets in " + spanText(w.ResetsAt.Sub(now))
			default:
				line += "  reset " + agoText(now.Sub(w.ResetsAt))
			}
			b.WriteString(line + "\n")
		}
	}
	return b.String()
}

// agoText is a past span: "just now", "2m ago", "3h ago", "2d 4h ago".
func agoText(d time.Duration) string {
	if d < time.Minute {
		return "just now"
	}
	return spanText(d) + " ago"
}

// spanText is a span to the minute, in its two largest units.
func spanText(d time.Duration) string {
	minutes := int(d.Round(time.Minute) / time.Minute)
	days, hours, mins := minutes/(24*60), minutes/60%24, minutes%60
	switch {
	case days > 0 && hours > 0:
		return fmt.Sprintf("%dd %dh", days, hours)
	case days > 0:
		return fmt.Sprintf("%dd", days)
	case hours > 0 && mins > 0:
		return fmt.Sprintf("%dh %dm", hours, mins)
	case hours > 0:
		return fmt.Sprintf("%dh", hours)
	default:
		return fmt.Sprintf("%dm", max(mins, 1))
	}
}
