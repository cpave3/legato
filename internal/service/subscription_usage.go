package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/cpave3/legato/internal/engine/store"
)

// Subscription usage is how much of a subscription's usage limits the
// operator has used: Claude's 5-hour and weekly windows, Codex's. It is not
// Usage (Manticore's token counts). See docs/ai/usage.md.

// The providers whose subscription usage Legato reads.
const (
	UsageProviderClaude = "claude"
	UsageProviderCodex  = "codex"
)

// The usage meters: a window of a provider's limits.
const (
	UsageMeterFiveHour     = "five_hour"
	UsageMeterWeekly       = "weekly"
	UsageMeterWeeklyOpus   = "weekly_opus"
	UsageMeterWeeklySonnet = "weekly_sonnet"
)

// UsageState is how a provider's reading stands.
type UsageState string

const (
	// UsageOK: a reading within two poll intervals.
	UsageOK UsageState = "ok"
	// UsageStale: a reading older than that, or the last read failed and an
	// older reading is shown.
	UsageStale UsageState = "stale"
	// UsageUnavailable: no reading to show; Reason says why.
	UsageUnavailable UsageState = "unavailable"
)

// Why a provider's usage could not be read.
const (
	UsageReasonNotRead         = "not_read"
	UsageReasonNotSignedIn     = "not_signed_in"
	UsageReasonLoginExpired    = "login_expired"
	UsageReasonNoSubscription  = "no_subscription"
	UsageReasonCodexMissing    = "codex_missing"
	UsageReasonNoData          = "no_data"
	UsageReasonEndpointChanged = "endpoint_changed"
	UsageReasonRateLimited     = "rate_limited"
	UsageReasonUnreachable     = "unreachable"
	UsageReasonError           = "error"
)

// Polling bounds.
const (
	DefaultUsagePollInterval = 5 * time.Minute
	MinUsagePollInterval     = 2 * time.Minute
	// usageRefreshGap is the least time between two reads of a provider.
	usageRefreshGap = time.Minute
	// usageMaxBackoff caps the wait after repeated failures.
	usageMaxBackoff = 30 * time.Minute
)

// UsageWindow is one window of a provider's limits.
type UsageWindow struct {
	Meter       string    `json:"meter"`
	UsedPercent float64   `json:"used_percent"`
	ResetsAt    time.Time `json:"resets_at,omitzero"`
	// WindowMinutes is the window's length when the provider says.
	WindowMinutes int `json:"window_minutes,omitempty"`
}

// UsageCredits is Codex's credit balance.
type UsageCredits struct {
	HasCredits bool   `json:"has_credits"`
	Unlimited  bool   `json:"unlimited"`
	Balance    string `json:"balance,omitempty"`
}

// ProviderUsage is one provider's subscription usage as Legato last read it.
type ProviderUsage struct {
	Provider string     `json:"provider"`
	State    UsageState `json:"state"`
	// Stale is State == UsageStale, for readers that want a flag.
	Stale bool `json:"stale"`
	// Reason says why the state is not ok: why the last read failed.
	Reason  string        `json:"reason,omitempty"`
	Windows []UsageWindow `json:"windows"`
	Credits *UsageCredits `json:"credits,omitempty"`
	Plan    string        `json:"plan,omitempty"`
	// AsOf is when the windows were true: the read's time, or the log
	// event's for Codex's logs. Nil without windows.
	AsOf *time.Time `json:"as_of,omitempty"`
	// CheckedAt is when Legato last tried to read, nil before it has.
	CheckedAt *time.Time `json:"checked_at,omitempty"`
	// Source is where the windows came from: endpoint, app_server, logs.
	Source string `json:"source,omitempty"`
}

// SubscriptionUsage is every provider's usage, in reader order.
type SubscriptionUsage struct {
	Providers    []ProviderUsage      `json:"providers"`
	PollInterval time.Duration        `json:"-"`
	Policy       *UsagePolicySnapshot `json:"policy,omitempty"`
}

// MarshalJSON gives the poll interval in seconds.
func (u SubscriptionUsage) MarshalJSON() ([]byte, error) {
	type plain SubscriptionUsage
	return json.Marshal(struct {
		plain
		PollSeconds int `json:"poll_interval_seconds"`
	}{plain(u), int(u.PollInterval / time.Second)})
}

// SubscriptionUsageReader reads one provider's usage. On success it gives
// the windows, and AsOf when they are older than now. A failure is a
// *UsageUnavailableError when the reason is known.
type SubscriptionUsageReader interface {
	Provider() string
	Read(ctx context.Context) (ProviderUsage, error)
}

// UsageUnavailableError is a read that failed for a known reason.
type UsageUnavailableError struct {
	Reason string
	// RetryAfter is how long the provider asked Legato to wait.
	RetryAfter time.Duration
}

func (e *UsageUnavailableError) Error() string {
	return "subscription usage unavailable: " + e.Reason
}

// usageReasonKeepsReading: after a failure for this reason the last
// reading is still worth showing, as stale.
func usageReasonKeepsReading(reason string) bool {
	switch reason {
	case UsageReasonEndpointChanged, UsageReasonRateLimited, UsageReasonUnreachable, UsageReasonError:
		return true
	}
	return false
}

// usageReasonBacksOff: a failure that is the provider's or the network's,
// so asking again soon would not help.
func usageReasonBacksOff(reason string) bool { return usageReasonKeepsReading(reason) }

// SubscriptionUsageStore keeps each provider's newest reading.
type SubscriptionUsageStore interface {
	ListSubscriptionUsage(ctx context.Context) ([]store.SubscriptionUsageRow, error)
	PutSubscriptionUsage(ctx context.Context, provider, reading string) error
}

// SubscriptionUsageOptions builds a SubscriptionUsageService.
type SubscriptionUsageOptions struct {
	Store   SubscriptionUsageStore
	Readers []SubscriptionUsageReader
	// PollInterval is DefaultUsagePollInterval when zero, and never less
	// than MinUsagePollInterval.
	PollInterval time.Duration
	Now          func() time.Time
	// OnChange is called after a reading is kept.
	OnChange func()
	// Forward asks the process that polls to refresh, when this one does
	// not (RequestRefresh).
	Forward   func()
	AfterPoll func(context.Context) error
}

// SubscriptionUsageService reads each provider's usage, keeps the newest
// reading in the database and serves it. Every process can serve it; only
// the one that runs Run reads providers (cmd/legato holds usage.lock for it).
type SubscriptionUsageService struct {
	opts   SubscriptionUsageOptions
	policy *UsagePolicyService
	pin    func(context.Context, *UsagePinRequest) error

	mu       sync.Mutex
	polling  bool
	provider map[string]*usagePoll
	// seeded: the schedule was taken from the kept readings.
	seeded sync.Once
}

// usagePoll is when a provider was last read and may next be.
type usagePoll struct {
	reading  sync.Mutex
	lastRead time.Time
	nextRead time.Time
	// backoffUntil holds back a refresh too, after a failure that backs off.
	backoffUntil time.Time
	failures     int
}

// NewSubscriptionUsageService serves the readers' usage.
func NewSubscriptionUsageService(opts SubscriptionUsageOptions) *SubscriptionUsageService {
	if opts.PollInterval == 0 {
		opts.PollInterval = DefaultUsagePollInterval
	}
	if opts.PollInterval < MinUsagePollInterval {
		opts.PollInterval = MinUsagePollInterval
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	s := &SubscriptionUsageService{opts: opts, provider: map[string]*usagePoll{}}
	for _, r := range opts.Readers {
		s.provider[r.Provider()] = &usagePoll{}
	}
	return s
}

// PollInterval is how often providers are read.
func (s *SubscriptionUsageService) PollInterval() time.Duration { return s.opts.PollInterval }

// Snapshot is every provider's newest reading from the database. It never
// reads a provider.
func (s *SubscriptionUsageService) Snapshot(ctx context.Context) (SubscriptionUsage, error) {
	out, err := s.ReadingsSnapshot(ctx)
	if err != nil || s.policy == nil {
		return out, err
	}
	policy, err := s.policy.Snapshot(ctx)
	out.Policy = &policy
	return out, err
}

// SetPolicy connects policy visibility and the owner-acknowledged pin route.
func (s *SubscriptionUsageService) SetPolicy(policy *UsagePolicyService, pin func(context.Context, *UsagePinRequest) error) {
	s.policy = policy
	s.pin = pin
	s.opts.AfterPoll = policy.Evaluate
}
func (s *SubscriptionUsageService) Pin(ctx context.Context, pin *UsagePinRequest) error {
	if s.pin == nil {
		return errors.New("usage policy is unavailable")
	}
	return s.pin(ctx, pin)
}

// ReadingsSnapshot is the cache without policy, used by the evaluator.
func (s *SubscriptionUsageService) ReadingsSnapshot(ctx context.Context) (SubscriptionUsage, error) {
	rows, err := s.opts.Store.ListSubscriptionUsage(ctx)
	if err != nil {
		return SubscriptionUsage{}, err
	}
	kept := map[string]ProviderUsage{}
	for _, row := range rows {
		var usage ProviderUsage
		if json.Unmarshal([]byte(row.Reading), &usage) == nil {
			kept[row.Provider] = usage
		}
	}
	now := s.opts.Now()
	out := SubscriptionUsage{PollInterval: s.opts.PollInterval, Providers: []ProviderUsage{}}
	for _, r := range s.opts.Readers {
		usage, ok := kept[r.Provider()]
		if !ok {
			usage = ProviderUsage{Provider: r.Provider(), State: UsageUnavailable, Reason: UsageReasonNotRead}
		}
		out.Providers = append(out.Providers, s.judge(usage, now))
	}
	return out, nil
}

// judge sets the state the reading has now: stale once older than two
// intervals.
func (s *SubscriptionUsageService) judge(usage ProviderUsage, now time.Time) ProviderUsage {
	if usage.Windows == nil {
		usage.Windows = []UsageWindow{}
	}
	switch {
	case len(usage.Windows) == 0 || usage.AsOf == nil:
		usage.State = UsageUnavailable
		if usage.Reason == "" {
			usage.Reason = UsageReasonNoData
		}
	case usage.Reason != "" || now.Sub(*usage.AsOf) > 2*s.opts.PollInterval:
		usage.State = UsageStale
	default:
		usage.State = UsageOK
	}
	usage.Stale = usage.State == UsageStale
	return usage
}

// Poll reads each provider whose next read is due. Run calls it.
func (s *SubscriptionUsageService) Poll(ctx context.Context) {
	s.read(ctx, false)
}

// Refresh reads each provider now, except one read in the last minute or
// waiting out a failure.
func (s *SubscriptionUsageService) Refresh(ctx context.Context) {
	s.read(ctx, true)
}

// RequestRefresh refreshes in the background when this process polls, and
// otherwise asks the one that does.
func (s *SubscriptionUsageService) RequestRefresh() {
	s.mu.Lock()
	polling := s.polling
	s.mu.Unlock()
	if polling {
		go s.Refresh(context.Background())
		return
	}
	if s.opts.Forward != nil {
		s.opts.Forward()
	}
}

// Run polls until ctx ends: at once, then every minute for whatever is due.
func (s *SubscriptionUsageService) Run(ctx context.Context) {
	s.mu.Lock()
	s.polling = true
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		s.polling = false
		s.mu.Unlock()
	}()
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		s.Poll(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (s *SubscriptionUsageService) read(ctx context.Context, refresh bool) {
	s.seeded.Do(func() { s.seed(ctx) })
	var wg sync.WaitGroup
	changed := false
	var changedMu sync.Mutex
	for _, r := range s.opts.Readers {
		poll := s.provider[r.Provider()]
		wg.Add(1)
		go func() {
			defer wg.Done()
			if s.readOne(ctx, r, poll, refresh) {
				changedMu.Lock()
				changed = true
				changedMu.Unlock()
			}
		}()
	}
	wg.Wait()
	if s.opts.AfterPoll != nil {
		if err := s.opts.AfterPoll(ctx); err != nil {
			log.Printf("usage policy: %v", err)
		}
	}
	if changed && s.opts.OnChange != nil {
		s.opts.OnChange()
	}
}

// readOne reads a provider if it is due, one read at a time, and keeps
// what it found. It reports whether it kept anything.
func (s *SubscriptionUsageService) readOne(ctx context.Context, r SubscriptionUsageReader, poll *usagePoll, refresh bool) bool {
	if !poll.reading.TryLock() {
		return false
	}
	defer poll.reading.Unlock()
	now := s.opts.Now()
	if now.Before(poll.backoffUntil) || (!refresh && now.Before(poll.nextRead)) {
		return false
	}
	if refresh && !poll.lastRead.IsZero() && now.Sub(poll.lastRead) < usageRefreshGap {
		return false
	}

	usage, err := r.Read(ctx)
	now = s.opts.Now()
	poll.lastRead = now
	poll.nextRead = now.Add(s.opts.PollInterval)
	poll.backoffUntil = time.Time{}
	if err != nil && errors.Is(err, context.Canceled) {
		return false
	}

	checked := now
	var kept ProviderUsage
	if err == nil {
		poll.failures = 0
		kept = usage
		kept.Reason = ""
		if kept.AsOf == nil {
			kept.AsOf = &checked
		}
	} else {
		reason, retryAfter := UsageReasonError, time.Duration(0)
		var unavailable *UsageUnavailableError
		if errors.As(err, &unavailable) {
			reason, retryAfter = unavailable.Reason, unavailable.RetryAfter
		} else {
			log.Printf("subscription usage: reading %s failed: %v", r.Provider(), err)
		}
		if usageReasonBacksOff(reason) {
			poll.failures++
			wait := s.opts.PollInterval << min(poll.failures, 4)
			if retryAfter > 0 {
				wait = retryAfter
			}
			wait = min(wait, usageMaxBackoff)
			if retryAfter > usageMaxBackoff {
				wait = retryAfter
			}
			poll.nextRead = now.Add(wait)
			poll.backoffUntil = poll.nextRead
		} else {
			poll.failures = 0
		}
		if usageReasonKeepsReading(reason) {
			kept = s.lastKept(ctx, r.Provider())
		}
		kept.Reason = reason
	}
	kept.Provider = r.Provider()
	kept.CheckedAt = &checked
	kept.State, kept.Stale = "", false
	data, err := json.Marshal(kept)
	if err != nil {
		return false
	}
	if err := s.opts.Store.PutSubscriptionUsage(ctx, r.Provider(), string(data)); err != nil {
		log.Printf("subscription usage: keeping %s's reading: %v", r.Provider(), err)
		return false
	}
	return true
}

// seed starts each provider's schedule from its kept reading, so a
// restarted Legato waits out the interval since the last read instead of
// asking every provider again at once, and the minute between refreshes
// holds across processes (each `legato usage --refresh` is a new service).
func (s *SubscriptionUsageService) seed(ctx context.Context) {
	rows, err := s.opts.Store.ListSubscriptionUsage(ctx)
	if err != nil {
		return
	}
	for _, row := range rows {
		poll, ok := s.provider[row.Provider]
		if !ok {
			continue
		}
		var usage ProviderUsage
		if json.Unmarshal([]byte(row.Reading), &usage) != nil || usage.CheckedAt == nil {
			continue
		}
		poll.reading.Lock()
		poll.lastRead = *usage.CheckedAt
		poll.nextRead = usage.CheckedAt.Add(s.opts.PollInterval)
		poll.reading.Unlock()
	}
}

// lastKept is a provider's kept reading, or none.
func (s *SubscriptionUsageService) lastKept(ctx context.Context, provider string) ProviderUsage {
	rows, err := s.opts.Store.ListSubscriptionUsage(ctx)
	if err != nil {
		return ProviderUsage{}
	}
	for _, row := range rows {
		if row.Provider != provider {
			continue
		}
		var usage ProviderUsage
		if json.Unmarshal([]byte(row.Reading), &usage) == nil {
			return usage
		}
	}
	return ProviderUsage{}
}

// String is a one-line summary for logs; it never holds a token.
func (u ProviderUsage) String() string {
	return fmt.Sprintf("%s %s %s %d windows", u.Provider, u.State, u.Reason, len(u.Windows))
}

// UsageReasonText says a reason in the operator's words, for the TUI and the
// CLI (the web UI keeps its own copy in web/src/lib/usage.ts).
func UsageReasonText(reason string) string {
	switch reason {
	case UsageReasonNotRead:
		return "not read yet"
	case UsageReasonNotSignedIn:
		return "sign in to it on this machine"
	case UsageReasonLoginExpired:
		return "its login has expired; using it signs in again"
	case UsageReasonNoSubscription:
		return "this login has no subscription limits"
	case UsageReasonCodexMissing:
		return "the codex command is not installed"
	case UsageReasonNoData:
		return "nothing reported yet"
	case UsageReasonEndpointChanged:
		return "its usage report changed; Legato cannot read it"
	case UsageReasonRateLimited:
		return "asked too often; waiting"
	case UsageReasonUnreachable:
		return "could not reach it"
	default:
		return "could not read it"
	}
}

// UsageProviderName is a provider's name as the operator knows it.
func UsageProviderName(provider string) string {
	switch provider {
	case UsageProviderClaude:
		return "Claude"
	case UsageProviderCodex:
		return "Codex"
	}
	return provider
}

// UsageMeterLabel is a meter's short label.
func UsageMeterLabel(meter string) string {
	switch meter {
	case UsageMeterFiveHour:
		return "5-hour"
	case UsageMeterWeekly:
		return "weekly"
	case UsageMeterWeeklyOpus:
		return "weekly Opus"
	case UsageMeterWeeklySonnet:
		return "weekly Sonnet"
	}
	return meter
}
