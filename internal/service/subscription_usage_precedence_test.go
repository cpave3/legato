package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/engine/claudeusage"
	"github.com/cpave3/legato/internal/engine/opencodeusage"
)

// When one error carries several causes (errors.Join, or a wrap of a wrap),
// precedence is fixed: cancellation, then the rate limit, then each
// provider's sentinels in its own order.
func TestProviderUsageErrorsPrecedence(t *testing.T) {
	claudeLimit := &claudeusage.RateLimitedError{RetryAfter: time.Minute}
	openLimit := &opencodeusage.RateLimitedError{RetryAfter: time.Minute}
	for _, tc := range []struct {
		name   string
		mapper func(error) error
		err    error
		want   string // "" means context.Canceled itself
	}{
		{"claude canceled beats rate limit", claudeUsageError, errors.Join(claudeLimit, context.Canceled), ""},
		{"claude rate limit beats sentinels", claudeUsageError, errors.Join(claudeusage.ErrNotSignedIn, claudeLimit), UsageReasonRateLimited},
		{"claude not signed in beats keychain", claudeUsageError, errors.Join(claudeusage.ErrNamedKeychainUnavailable, claudeusage.ErrNotSignedIn), UsageReasonNotSignedIn},
		{"claude keychain beats expired", claudeUsageError, errors.Join(claudeusage.ErrLoginExpired, claudeusage.ErrNamedKeychainUnavailable), UsageReasonNamedKeychainUnavailable},
		{"claude expired beats no subscription", claudeUsageError, errors.Join(claudeusage.ErrNoSubscription, claudeusage.ErrLoginExpired), UsageReasonLoginExpired},
		{"claude no subscription beats endpoint", claudeUsageError, errors.Join(claudeusage.ErrEndpointChanged, claudeusage.ErrNoSubscription), UsageReasonNoSubscription},
		{"claude ignores opencode's sentinels", claudeUsageError, opencodeusage.ErrNoSubscription, UsageReasonError},
		{"opencode canceled beats rate limit", opencodeUsageError, errors.Join(openLimit, context.Canceled), ""},
		{"opencode rate limit beats sentinels", opencodeUsageError, errors.Join(opencodeusage.ErrUnreachable, openLimit), UsageReasonRateLimited},
		{"opencode no key beats invalid key", opencodeUsageError, errors.Join(opencodeusage.ErrInvalidKey, opencodeusage.ErrNoKey), UsageReasonMissingKey},
		{"opencode invalid key beats no subscription", opencodeUsageError, errors.Join(opencodeusage.ErrNoSubscription, opencodeusage.ErrInvalidKey), UsageReasonInvalidKey},
		{"opencode endpoint beats unreachable", opencodeUsageError, errors.Join(opencodeusage.ErrUnreachable, opencodeusage.ErrEndpointChanged), UsageReasonEndpointChanged},
		{"opencode ignores claude's rate limit", opencodeUsageError, claudeLimit, UsageReasonError},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.mapper(tc.err)
			if tc.want == "" {
				if !errors.Is(got, context.Canceled) {
					t.Fatalf("got %v, want cancellation", got)
				}
				return
			}
			var unavailable *UsageUnavailableError
			if !errors.As(got, &unavailable) || unavailable.Reason != tc.want {
				t.Fatalf("got %#v, want reason %s", got, tc.want)
			}
		})
	}
}
