package service

import (
	"context"
	"testing"
	"time"

	"github.com/cpave3/legato/config"
)

func charactersLimited(count int64) (config.CredentialRule, config.CredentialRule) {
	limited := rule("auto", "tts.speak")
	limited.Limits = []config.RuleLimit{{Meter: "characters", Count: count, Period: 24 * time.Hour}}
	window := rule("window", "tts.speak")
	window.WindowDuration = time.Hour
	return limited, window
}

func speak(characters int64) CredentialUse {
	u := use("tts.speak", "api.elevenlabs.io/voice/abc")
	u.Amounts = map[string]int64{"characters": characters}
	return u
}

func TestAUseThatWouldPassACharacterLimitFallsToTheNextRule(t *testing.T) {
	limited, window := charactersLimited(1000)
	f := newBrokerFixture(t, eleven(limited, window))
	ctx := context.Background()

	if d, _ := f.broker.Authorize(ctx, speak(600)); !d.Allowed {
		t.Fatalf("600 of 1000: %+v", d)
	}
	if d, _ := f.broker.Authorize(ctx, speak(400)); !d.Allowed {
		t.Fatalf("1000 of 1000: %+v", d)
	}
	d, _ := f.broker.Authorize(ctx, speak(1))
	if d.Allowed || d.ApprovalID == "" {
		t.Fatalf("1001 of 1000: %+v; want it asked", d)
	}
}

func TestMeasuredBytesCountTowardTheNextUse(t *testing.T) {
	limited := rule("auto", "tts.speak")
	limited.Limits = []config.RuleLimit{{Meter: "bytes", Count: 5000, Period: 24 * time.Hour}}
	f := newBrokerFixture(t, eleven(limited))
	ctx := context.Background()

	u := speak(10)
	u.Amounts["bytes"] = 100
	if d, _ := f.broker.Authorize(ctx, u); !d.Allowed {
		t.Fatalf("first: %+v", d)
	}
	f.broker.RecordMeasured(ctx, "eleven", map[string]int64{"bytes": 6000})
	if d, _ := f.broker.Authorize(ctx, u); d.Allowed {
		t.Fatalf("after 6100 bytes of 5000: %+v; want refused", d)
	}
}

func TestAnAutoGrantDoesNotOutliveItsRulesLimit(t *testing.T) {
	limited, window := charactersLimited(100)
	f := newBrokerFixture(t, eleven(limited, window))
	ctx := context.Background()

	// The agent asks first; the auto rule grants.
	result, err := f.broker.Request(ctx, request([]string{"tts.speak"}, "api.elevenlabs.io/voice/abc"))
	if err != nil || result.Status != AccessGranted {
		t.Fatalf("request = %+v, %v", result, err)
	}
	if d, _ := f.broker.Authorize(ctx, speak(100)); !d.Allowed {
		t.Fatalf("within the limit: %+v", d)
	}
	if d, _ := f.broker.Authorize(ctx, speak(50)); d.Allowed {
		t.Fatalf("past the limit the auto grant still allowed it: %+v", d)
	}
}

func TestADenyRuleSaysTheCredentialDoesNotAllowIt(t *testing.T) {
	f := newBrokerFixture(t, eleven(rule("deny", "keys.manage")))
	d, _ := f.broker.Authorize(context.Background(), use("keys.manage", "api.elevenlabs.io/v1/api-keys"))
	if d.Allowed || d.Reason != "eleven does not allow keys.manage on api.elevenlabs.io/v1/api-keys" {
		t.Fatalf("decision = %+v", d)
	}
	d, _ = f.broker.Authorize(context.Background(), use("other", ""))
	if d.Reason != "no rule of eleven allows other on all of it" {
		t.Fatalf("no rule: %+v", d)
	}
}
