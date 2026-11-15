package service

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/engine/events"
)

func mqttMessages(c *mqttTestTransport) []mqttTestMessage {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]mqttTestMessage(nil), c.history...)
}

func TestMQTTPublisherHABirthRepublishesClearedRetainedData(t *testing.T) {
	src := &mqttTestSources{agents: []AgentSession{{Status: "running", Activity: "working"}}}
	transport := mqttNewTestTransport()
	p := mqttStartSources(t, mqttTestOptions(), src, events.New(), transport)
	transport.mu.Lock()
	expected := make(map[string]string, len(transport.messages))
	for topic, payload := range transport.messages {
		expected[topic] = payload
	}
	transport.messages = map[string]string{}
	transport.history = nil
	transport.mu.Unlock()
	p.HomeAssistantOnline()
	mqttAwait(t, func() bool { return transport.get("legato/test/availability") == "online" })
	for topic, want := range expected {
		if got := transport.get(topic); got != want {
			t.Errorf("birth %s=%q want %q", topic, got, want)
		}
	}
	messages := mqttMessages(transport)
	if len(messages) != 22 || messages[len(messages)-1] != (mqttTestMessage{"legato/test/availability", "online"}) {
		t.Fatalf("expected full 10 discovery + 10 states + attributes then online; got %v", messages)
	}
}

func TestMQTTPublisherSnapshotFailureIsOfflineWithoutFalseZeroAndRecovers(t *testing.T) {
	for _, source := range []string{"agents", "attention", "approvals"} {
		t.Run(source, func(t *testing.T) {
			src := &mqttTestSources{agents: []AgentSession{{Status: "running", Activity: "working"}}, items: []AttentionItem{{SignalKind: AttentionSignalApproval}}, approvals: []Approval{{}}}
			transport := mqttNewTestTransport()
			bus := events.New()
			mqttStartSources(t, mqttTestOptions(), src, bus, transport)
			src.mu.Lock()
			switch source {
			case "agents":
				src.err = errors.New("source unavailable")
			case "attention":
				src.attentionErr = errors.New("source unavailable")
			case "approvals":
				src.approvalsErr = errors.New("source unavailable")
			}
			src.mu.Unlock()
			bus.Publish(events.Event{Type: events.EventAgentActivityChanged})
			mqttAwait(t, func() bool { return transport.get("legato/test/availability") == "offline" })
			for _, key := range []string{"agents_running", "attention_items", "pending_approvals"} {
				if got := transport.get("legato/test/" + key + "/state"); got != "1" {
					t.Errorf("failed snapshot replaced %s with %q", key, got)
				}
			}
			src.mu.Lock()
			src.err = nil
			src.attentionErr = nil
			src.approvalsErr = nil
			src.agents = append(src.agents, AgentSession{Status: "running", Activity: "waiting"})
			src.mu.Unlock()
			// Recovery retries without requiring another event from a source.
			mqttAwait(t, func() bool {
				return transport.get("legato/test/availability") == "online" && transport.get("legato/test/agents_running/state") == "2"
			})
			messages := mqttMessages(transport)
			offline := -1
			for i, msg := range messages {
				if msg == (mqttTestMessage{"legato/test/availability", "offline"}) {
					offline = i
				}
			}
			for _, msg := range messages[offline+1:] {
				if msg.topic == "legato/test/agents_running/state" && msg.payload == "0" {
					t.Fatal("published false all-clear on snapshot failure")
				}
			}
			if messages[len(messages)-1] != (mqttTestMessage{"legato/test/availability", "online"}) {
				t.Fatal("availability became online before recovered snapshot")
			}
		})
	}
}

func TestMQTTPublisherReconnectUsesNewestAuthoritativeSnapshot(t *testing.T) {
	src := &mqttTestSources{agents: []AgentSession{{Status: "running", Activity: "working"}}}
	bus := events.New()
	first, second := mqttNewTestTransport(), mqttNewTestTransport()
	retry := make(chan struct{})
	release := make(chan struct{})
	var attempts int
	p := NewMQTTPublisher(mqttTestOptions(), src, src, mqttTestApprovals{src}, bus, func(ctx context.Context) (MQTTConnection, error) {
		attempts++
		if attempts == 1 {
			return first, nil
		}
		if attempts == 2 {
			close(retry)
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-release:
			return second, nil
		}
	})
	mqttRunTestPublisher(t, p)
	mqttAwait(t, func() bool { return first.get("legato/test/availability") == "online" })
	close(first.done)
	select {
	case <-retry:
	case <-time.After(4 * time.Second):
		t.Fatal("no reconnect attempt")
	}
	src.mu.Lock()
	src.agents = nil
	src.mu.Unlock()
	bus.Publish(events.Event{Type: events.EventAgentDied})
	src.mu.Lock()
	src.agents = []AgentSession{{Status: "running", Activity: "waiting"}, {Status: "running", Activity: "background"}}
	src.mu.Unlock()
	bus.Publish(events.Event{Type: events.EventAgentStarted})
	close(release)
	mqttAwait(t, func() bool { return second.get("legato/test/availability") == "online" })
	if second.get("legato/test/agents_running/state") != "2" || second.get("legato/test/agents_waiting/state") != "1" {
		t.Fatal("reconnect did not rebuild current snapshot")
	}
	for _, msg := range mqttMessages(second) {
		if msg.topic == "legato/test/agents_running/state" && msg.payload != "2" {
			t.Fatalf("replayed stale aggregate: %v", msg)
		}
	}
}

// mqttBlockedAgents captures a snapshot before blocking, so the mutation in the
// test really happens during a stale read, not merely before the reader starts.
type mqttBlockedAgents struct {
	source  *mqttTestSources
	mu      sync.Mutex
	entered chan struct{}
	release chan struct{}
}

func (s *mqttBlockedAgents) ListAgents(ctx context.Context) ([]AgentSession, error) {
	agents, err := s.source.ListAgents(ctx)
	s.mu.Lock()
	entered, release := s.entered, s.release
	s.entered = nil
	s.mu.Unlock()
	if entered != nil {
		close(entered)
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-release:
		}
	}
	return agents, err
}

func TestMQTTPublisherConvergesAfterMutationDuringBlockedSnapshot(t *testing.T) {
	src := &mqttTestSources{agents: []AgentSession{{Status: "running", Activity: "working"}}}
	blocked := &mqttBlockedAgents{source: src, entered: make(chan struct{}), release: make(chan struct{})}
	entered := blocked.entered
	bus := events.New()
	transport := mqttNewTestTransport()
	p := NewMQTTPublisher(mqttTestOptions(), blocked, src, mqttTestApprovals{src}, bus, func(context.Context) (MQTTConnection, error) { return transport, nil })
	mqttRunTestPublisher(t, p)
	mqttAwait(t, func() bool {
		select {
		case <-entered:
			return true
		default:
			return false
		}
	})
	src.mu.Lock()
	src.agents = nil
	src.mu.Unlock()
	bus.Publish(events.Event{Type: events.EventAgentDied})
	close(blocked.release)
	mqttAwait(t, func() bool {
		return transport.get("legato/test/availability") == "online" && transport.get("legato/test/agents_running/state") == "0"
	})
	foundStale := false
	for _, msg := range mqttMessages(transport) {
		if msg == (mqttTestMessage{"legato/test/agents_running/state", "1"}) {
			foundStale = true
		}
	}
	if !foundStale {
		t.Fatal("fixture did not exercise a stale in-flight snapshot")
	}
}

func TestMQTTPublisherSaturatedEventsPreserveFinalInvalidation(t *testing.T) {
	src := &mqttTestSources{agents: []AgentSession{{Status: "running", Activity: "working"}}}
	bus := events.New()
	// Leave a legacy event subscription undrained to reproduce the saturated bus.
	legacy := bus.Subscribe(events.EventAgentActivityChanged)
	defer bus.Unsubscribe(legacy)
	entered, release := make(chan struct{}), make(chan struct{})
	transport := mqttNewTestTransport()
	var once sync.Once
	transport.beforePublish = func(ctx context.Context, _ string) error {
		var err error
		once.Do(func() {
			close(entered)
			select {
			case <-ctx.Done():
				err = ctx.Err()
			case <-release:
			}
		})
		return err
	}
	p := NewMQTTPublisher(mqttTestOptions(), src, src, mqttTestApprovals{src}, bus, func(context.Context) (MQTTConnection, error) { return transport, nil })
	mqttRunTestPublisher(t, p)
	mqttAwait(t, func() bool {
		select {
		case <-entered:
			return true
		default:
			return false
		}
	})
	for i := 0; i < 10000; i++ {
		src.mu.Lock()
		if i == 9999 {
			src.agents = nil
		} else {
			src.agents = []AgentSession{{Status: "running", Activity: "waiting"}}
		}
		src.mu.Unlock()
		bus.Publish(events.Event{Type: events.EventAgentActivityChanged})
	}
	close(release)
	mqttAwait(t, func() bool {
		return transport.get("legato/test/agents_running/state") == "0" && transport.get("legato/test/agent_waiting_on_you/state") == "OFF"
	})
	if len(legacy) != 64 {
		t.Fatalf("fixture did not saturate ordinary event delivery: %d", len(legacy))
	}
}
