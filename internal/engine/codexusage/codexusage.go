// Package codexusage reads a ChatGPT account's Codex usage limits: the
// windows Codex reports with the percent used and when each resets.
//
// It reads them two ways, both with Codex's own login, so the ChatGPT token
// never passes through this package: AppServer asks a short-lived
// `codex app-server` (`account/rateLimits/read`), which is live; ReadLogs
// takes the newest `rate_limits` Codex wrote into its session logs, which is
// only as fresh as the last Codex turn. The app server's protocol is marked
// experimental, so an answer this package does not understand is an error,
// never a guess.
package codexusage

import (
	"errors"
	"time"
)

// Where a reading came from.
const (
	SourceAppServer = "app_server"
	SourceLogs      = "logs"
)

// The window kinds Classify gives.
const (
	FiveHour = "five_hour"
	Weekly   = "weekly"
)

var (
	// ErrCodexMissing: no codex command to run.
	ErrCodexMissing = errors.New("codex: command not found")
	// ErrNotSignedIn: Codex has no ChatGPT login.
	ErrNotSignedIn = errors.New("codex: not signed in")
	// ErrNoData: nothing reported any usage window.
	ErrNoData = errors.New("codex: no usage data")
	// ErrUnsupported: this codex's app server cannot read rate limits.
	ErrUnsupported = errors.New("codex: app server cannot read rate limits")
)

// Window is one usage window. Codex's primary and secondary windows are not
// always the same lengths, so a window is known by its length (Classify).
type Window struct {
	Minutes     int
	UsedPercent float64
	// ResetsAt is zero when Codex did not say.
	ResetsAt time.Time
}

// Credits is the account's credit balance, as Codex reports it.
type Credits struct {
	HasCredits bool
	Unlimited  bool
	Balance    string
}

// Reading is what one read found.
type Reading struct {
	Windows  []Window
	Credits  *Credits
	PlanType string
	// AsOf is when Codex reported it: the log event's time for SourceLogs,
	// zero for SourceAppServer (it is now).
	AsOf   time.Time
	Source string
}

// Classify says which kind of window a length is: FiveHour up to six
// hours, Weekly from a week less an hour, otherwise "".
func Classify(minutes int) string {
	switch {
	case minutes <= 0:
		return ""
	case minutes <= 360:
		return FiveHour
	case minutes >= 7*24*60-60:
		return Weekly
	default:
		return ""
	}
}

// wireWindow is a window in the app server's camelCase or the logs'
// snake_case.
type wireWindow struct {
	UsedPercent        *float64 `json:"usedPercent"`
	WindowDurationMins *int     `json:"windowDurationMins"`
	ResetsAt           *int64   `json:"resetsAt"`

	UsedPercentSnake   *float64 `json:"used_percent"`
	WindowMinutesSnake *int     `json:"window_minutes"`
	ResetsAtSnake      *int64   `json:"resets_at"`
}

func (w *wireWindow) window() (Window, bool) {
	if w == nil {
		return Window{}, false
	}
	used, minutes, resets := w.UsedPercent, w.WindowDurationMins, w.ResetsAt
	if used == nil {
		used = w.UsedPercentSnake
	}
	if minutes == nil {
		minutes = w.WindowMinutesSnake
	}
	if resets == nil {
		resets = w.ResetsAtSnake
	}
	if used == nil {
		return Window{}, false
	}
	window := Window{UsedPercent: *used}
	if minutes != nil {
		window.Minutes = *minutes
	}
	if resets != nil && *resets > 0 {
		window.ResetsAt = time.Unix(*resets, 0).UTC()
	}
	return window, true
}

type wireCredits struct {
	HasCredits      *bool  `json:"hasCredits"`
	HasCreditsSnake *bool  `json:"has_credits"`
	Unlimited       bool   `json:"unlimited"`
	Balance         string `json:"balance"`
}

type wireLimits struct {
	Primary   *wireWindow  `json:"primary"`
	Secondary *wireWindow  `json:"secondary"`
	Credits   *wireCredits `json:"credits"`
	PlanType  string       `json:"planType"`
}

func (l *wireLimits) reading(source string) (Reading, error) {
	if l == nil {
		return Reading{}, ErrNoData
	}
	reading := Reading{Source: source, PlanType: l.PlanType}
	for _, w := range []*wireWindow{l.Primary, l.Secondary} {
		if window, ok := w.window(); ok {
			reading.Windows = append(reading.Windows, window)
		}
	}
	if c := l.Credits; c != nil {
		has := c.HasCredits
		if has == nil {
			has = c.HasCreditsSnake
		}
		reading.Credits = &Credits{HasCredits: has != nil && *has, Unlimited: c.Unlimited, Balance: c.Balance}
	}
	if len(reading.Windows) == 0 {
		return Reading{}, ErrNoData
	}
	return reading, nil
}
