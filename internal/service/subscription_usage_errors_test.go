package service

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/engine/claudeusage"
	"github.com/cpave3/legato/internal/engine/opencodeusage"
)

// Each provider's errors map to the reasons the web UI and CLI explain,
// wrapped or not; cancellation passes through and anything else is a
// plain error that carries none of the engine's words.
func TestProviderUsageErrorsGiveReasons(t *testing.T) {
	type mapping struct {
		name string
		err  error
		want string
	}
	cases := map[string]struct {
		mapper func(error) error
		limit  error
		rows   []mapping
	}{
		"claude": {claudeUsageError, &claudeusage.RateLimitedError{RetryAfter: 90 * time.Second}, []mapping{
			{"not signed in", claudeusage.ErrNotSignedIn, UsageReasonNotSignedIn},
			{"named keychain", claudeusage.ErrNamedKeychainUnavailable, UsageReasonNamedKeychainUnavailable},
			{"login expired", claudeusage.ErrLoginExpired, UsageReasonLoginExpired},
			{"no subscription", claudeusage.ErrNoSubscription, UsageReasonNoSubscription},
			{"endpoint changed", claudeusage.ErrEndpointChanged, UsageReasonEndpointChanged},
			{"anything else", errors.New("claude: secret body"), UsageReasonError},
		}},
		"opencode": {opencodeUsageError, &opencodeusage.RateLimitedError{RetryAfter: 90 * time.Second}, []mapping{
			{"no key", opencodeusage.ErrNoKey, UsageReasonMissingKey},
			{"invalid key", opencodeusage.ErrInvalidKey, UsageReasonInvalidKey},
			{"no subscription", opencodeusage.ErrNoSubscription, UsageReasonNoSubscription},
			{"endpoint changed", opencodeusage.ErrEndpointChanged, UsageReasonEndpointChanged},
			{"unreachable", opencodeusage.ErrUnreachable, UsageReasonUnreachable},
			{"anything else", errors.New("opencode: secret body"), UsageReasonError},
		}},
	}
	for provider, tc := range cases {
		t.Run(provider, func(t *testing.T) {
			if err := tc.mapper(context.Canceled); err != context.Canceled {
				t.Fatalf("canceled = %v, want context.Canceled itself", err)
			}
			if err := tc.mapper(fmt.Errorf("read: %w", context.Canceled)); !errors.Is(err, context.Canceled) {
				t.Fatalf("wrapped canceled = %v", err)
			}
			var unavailable *UsageUnavailableError
			if err := tc.mapper(fmt.Errorf("read: %w", tc.limit)); !errors.As(err, &unavailable) ||
				unavailable.Reason != UsageReasonRateLimited || unavailable.RetryAfter != 90*time.Second {
				t.Fatalf("rate limited = %#v", err)
			}
			for _, row := range tc.rows {
				for _, err := range []error{row.err, fmt.Errorf("read: %w", row.err)} {
					unavailable = nil
					got := tc.mapper(err)
					if !errors.As(got, &unavailable) || unavailable.Reason != row.want || unavailable.RetryAfter != 0 {
						t.Fatalf("%s: %v = %#v, want reason %s", row.name, err, got, row.want)
					}
					if got.Error() == err.Error() {
						t.Fatalf("%s: the engine's words leaked: %q", row.name, got.Error())
					}
				}
			}
		})
	}
}
