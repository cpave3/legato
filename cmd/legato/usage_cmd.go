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
	readers := usageReaders(cfg, db)
	usage := service.NewSubscriptionUsageService(service.SubscriptionUsageOptions{
		Store: db, Readers: readers, PollInterval: cfg.Usage.PollIntervalDuration(),
		OnChange: func() { ipc.Broadcast(ipc.Message{Type: ipcUsageChanged}) },
	})
	attachUsagePolicy(cfg, db, usage, func() { ipc.Broadcast(ipc.Message{Type: ipcUsageChanged}) })
	return usage
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
	var b strings.Builder
	if snap.Policy != nil && (snap.Policy.Active.Initialized || len(snap.Policy.Profiles) > 0) {
		active := snap.Policy.Active
		name := active.Profile.Name
		if name == "" {
			name = "configured defaults"
		}
		fmt.Fprintf(&b, "Usage profile: %s\n%s\n", name, active.Reason.Text)
		if active.Error != "" {
			fmt.Fprintf(&b, "Policy error: %s\n", active.Error)
		}
		if active.Pin != nil {
			expiry := "until unpin"
			if active.Pin.Until != nil {
				expiry = "until " + active.Pin.Until.Format(time.RFC3339)
			}
			fmt.Fprintf(&b, "Pinned: %s (%s)\n", active.Pin.Profile, expiry)
		}
		for _, scope := range snap.Policy.Scopes {
			name := scope.Active.Profile.Name
			if name == "" {
				name = "configured defaults"
			}
			fmt.Fprintf(&b, "New work in %s (default %s): %s — %s\n", strings.Join(scope.Repositories, ", "), scope.Baseline, name, scope.Active.Reason.Text)
			if scope.Active.Error != "" {
				fmt.Fprintf(&b, "  Policy error: %s\n", scope.Active.Error)
			}
		}
		for _, change := range snap.Policy.Switches {
			fmt.Fprintf(&b, "  %s: %s → %s — %s\n", change.Since, change.From, change.To, change.Reason)
		}
	}
	if len(snap.Providers) == 0 {
		b.WriteString("Subscription usage is off (usage.claude.enabled, usage.codex.enabled and usage.opencode.enabled).\n")
		return b.String()
	}
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

func runUsagePinCmd(action string, args []string) int {
	command := "usage " + action
	parsed, parseErr := parseCommandArgs(args, commandFlagSpecs("usage", action))
	jsonMode := parsed.Present["json"]
	if parseErr != nil {
		return renderCommandError(command, parseErr, jsonMode)
	}
	want := 0
	if action == "pin" {
		want = 1
	}
	if len(parsed.Positionals) != want {
		return renderCommandError(command, usageError("invalid_arguments", "usage pin needs one profile; usage unpin needs none"), jsonMode)
	}
	var pin *service.UsagePinRequest
	if action == "pin" {
		pin = &service.UsagePinRequest{Profile: parsed.Positionals[0], UntilReset: parsed.Values["until-reset"]}
		if parsed.Present["until"] {
			until, err := time.Parse(time.RFC3339, parsed.Values["until"])
			if err != nil {
				return renderCommandError(command, usageError("invalid_time", "--until needs an RFC3339 time with a time zone"), jsonMode)
			}
			pin.Until = &until
		}
		if pin.Until != nil && pin.UntilReset != "" {
			return renderCommandError(command, usageError("invalid_expiry", "choose --until or --until-reset, not both"), jsonMode)
		}
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
	if err := usage.Pin(context.Background(), pin); err != nil {
		return renderCommandError(command, &commandError{Code: "pin_failed", Message: err.Error(), Exit: exitUsage}, jsonMode)
	}
	snap, err := usage.Snapshot(context.Background())
	if err != nil {
		return renderCommandError(command, &commandError{Code: "usage_failed", Message: err.Error(), Exit: exitInternal}, jsonMode)
	}
	return writeCommandSuccess(command, snap, formatSubscriptionUsage(snap, time.Now()), jsonMode)
}
