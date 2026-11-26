package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/cpave3/legato/config"
	"github.com/cpave3/legato/internal/engine/events"
	"github.com/cpave3/legato/internal/engine/ipc"
	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
)

const ipcUsagePin = "usage_pin"
const ipcUsagePinReply = "usage_pin_reply"

type usagePolicyHost struct {
	policy *service.UsagePolicyService
	owner  atomic.Bool
}

var usagePolicyHosts sync.Map

func usageDirectory(cfg *config.Config) string {
	dir, _ := filepath.Abs(config.ResolveDataDir(cfg))
	return dir
}

func attachUsagePolicy(cfg *config.Config, db *store.Store, usage *service.SubscriptionUsageService, changed func()) {
	currentConfig := func(ctx context.Context) (*config.Config, error) { return settingsFor(db).Effective(ctx) }
	host := &usagePolicyHost{}
	host.policy = service.NewUsagePolicyService(service.UsagePolicyOptions{
		Store: db, Readings: usage.ReadingsSnapshot,
		Config: func(ctx context.Context) (config.UsageConfig, error) {
			live, err := currentConfig(ctx)
			if err != nil {
				return config.UsageConfig{}, err
			}
			return live.Usage, nil
		},
		Validate: func(profile config.UsageProfile) error {
			for _, target := range [][2]string{{profile.Agent, profile.Tier}, reviewerPair(profile)} {
				if target[1] == "" {
					continue
				}
				adapter, ok := cfg.Adapters[target[0]]
				if !ok {
					return fmt.Errorf("restart Legato to apply %s tier %s", target[0], target[1])
				}
				if _, ok := adapter.Tiers[target[1]]; !ok {
					return fmt.Errorf("restart Legato to apply %s tier %s", target[0], target[1])
				}
			}
			return nil
		},
		OnChange: changed,
		OnSwitch: func(ctx context.Context, state service.UsageProfileState, id int64) {
			live, err := currentConfig(ctx)
			if err != nil {
				log.Printf("usage notification: %v", err)
				return
			}
			service.NotifyUsageProfileSwitch(state, id, live.Notifications.UsageEnabled(), service.NewNtfyNotifier(live.Notifications.Ntfy.URL, live.Notifications.Ntfy.Topic, live.Notifications.Ntfy.Token), usageOSNotifier(live))
		},
	})
	dir := usageDirectory(cfg)
	usagePolicyHosts.Store(dir, host)
	usage.SetPolicy(host.policy, func(ctx context.Context, pin *service.UsagePinRequest) error {
		return requestUsagePin(ctx, dir, host.policy, pin)
	})
}

func reviewerPair(p config.UsageProfile) [2]string {
	if p.Reviewer == nil {
		return [2]string{}
	}
	return [2]string{p.Reviewer.Agent, p.Reviewer.Tier}
}
func usageOSNotifier(cfg *config.Config) service.Notifier {
	if cfg.Notifications.OS.Enabled {
		return service.NewOSNotifier()
	}
	return nil
}

func requestUsagePin(ctx context.Context, dir string, policy *service.UsagePolicyService, pin *service.UsagePinRequest) error {
	if host, ok := usagePolicyHosts.Load(dir); ok && host.(*usagePolicyHost).policy == policy && host.(*usagePolicyHost).owner.Load() {
		return policy.Pin(ctx, pin)
	}
	if release, err := claimUsagePoller(dir); err == nil {
		defer release()
		return policy.Pin(ctx, pin)
	}
	data, err := json.Marshal(pin)
	if err != nil {
		return err
	}
	reply, err := ipc.BroadcastRequest(ctx, ipc.Message{Type: ipcUsagePin, Notes: dir, Content: string(data)}, ipcUsagePinReply, 5*time.Second)
	if err != nil {
		return fmt.Errorf("usage owner did not acknowledge pin: %w", err)
	}
	if reply.Status != "ok" {
		return errors.New(reply.Content)
	}
	return nil
}

func handleUsagePinIPC(msg ipc.Message) {
	value, ok := usagePolicyHosts.Load(msg.Notes)
	if !ok {
		return
	}
	host := value.(*usagePolicyHost)
	if !host.owner.Load() {
		return
	}
	var pin *service.UsagePinRequest
	err := json.Unmarshal([]byte(msg.Content), &pin)
	if err == nil {
		err = host.policy.Pin(context.Background(), pin)
	}
	reply := ipc.Message{Type: ipcUsagePinReply, Status: "ok"}
	if err != nil {
		reply.Status = "error"
		reply.Content = err.Error()
	}
	ipc.Send(msg.ReplySocket, reply)
}

// registerUsageAttention is read-only; only the usage owner emits notifications.
func registerUsageAttention(attention *service.AttentionService, db *store.Store) {
	mustRegister(attention.RegisterProducerWithEvents(service.NewUsageAttentionAdapter(db), events.EventSubscriptionUsageChanged, events.EventSubscriptionUsageInvalidated))
}
