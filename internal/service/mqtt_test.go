package service

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/engine/events"
	"github.com/cpave3/legato/internal/engine/store"
)

type mqttTestSources struct {
	mu           sync.Mutex
	agents       []AgentSession
	items        []AttentionItem
	approvals    []Approval
	err          error
	attentionErr error
	approvalsErr error
}

func (s *mqttTestSources) ListAgents(context.Context) ([]AgentSession, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]AgentSession(nil), s.agents...), s.err
}
func (s *mqttTestSources) List(context.Context, AttentionFilter) ([]AttentionItem, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]AttentionItem(nil), s.items...), s.attentionErr
}

type mqttTestApprovals struct{ source *mqttTestSources }

func (s mqttTestApprovals) List(context.Context, store.ApprovalFilter) ([]Approval, error) {
	s.source.mu.Lock()
	defer s.source.mu.Unlock()
	return append([]Approval(nil), s.source.approvals...), s.source.approvalsErr
}

type mqttTestTransport struct {
	mu            sync.Mutex
	messages      map[string]string
	done          chan struct{}
	history       []mqttTestMessage
	beforePublish func(context.Context, string) error
}

type mqttTestMessage struct{ topic, payload string }

func (c *mqttTestTransport) Publish(ctx context.Context, topic string, payload []byte) error {
	if c.beforePublish != nil {
		if err := c.beforePublish(ctx, topic); err != nil {
			return err
		}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.messages[topic] = string(payload)
	c.history = append(c.history, mqttTestMessage{topic, string(payload)})
	return nil
}
func (c *mqttTestTransport) Close(context.Context) error { return nil }
func (c *mqttTestTransport) Done() <-chan struct{}       { return c.done }
func (c *mqttTestTransport) get(topic string) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.messages[topic]
}
func mqttAwait(t *testing.T, f func() bool) {
	t.Helper()
	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		if f() {
			return
		}
		time.Sleep(time.Millisecond * 5)
	}
	t.Fatal("MQTT outcome not observed")
}

func TestMQTTPublisherPublishesTenAggregateEntities(t *testing.T) {
	src := &mqttTestSources{agents: []AgentSession{
		{Status: "running", Activity: "working"}, {Status: "running", Activity: "waiting", Slot: "reviewer"}, {Status: "running", Activity: ""}, {Status: "running", Activity: "background"}, {Status: "running", Activity: "error"}, {Status: "running", Activity: "working", Reeve: true}, {Status: "stopped", Activity: "waiting"},
	}, items: []AttentionItem{{SignalKind: AttentionSignalApproval, Title: "secret /home/password", Detail: "never export"}}, approvals: []Approval{{}}}
	transport := &mqttTestTransport{messages: map[string]string{}, done: make(chan struct{})}
	bus := events.New()
	publisher := NewMQTTPublisher(MQTTOptions{InstanceID: "instance-1", BaseTopic: "legato/test", DeviceName: "Test", DiscoveryPrefix: "homeassistant"}, src, src, mqttTestApprovals{src}, bus, func(context.Context) (MQTTConnection, error) { return transport, nil })
	ctx, cancel := context.WithCancel(context.Background())
	stopped := make(chan struct{})
	go func() { defer close(stopped); publisher.Run(ctx) }()
	defer func() { cancel(); <-stopped }()
	mqttAwait(t, func() bool { return transport.get("legato/test/availability") == "online" })
	for key, want := range map[string]string{"agents_running": "5", "agents_working": "1", "agents_waiting": "1", "agents_idle": "1", "agents_background": "1", "agents_error": "1", "needs_attention": "ON", "attention_items": "1", "agent_waiting_on_you": "ON", "pending_approvals": "1"} {
		if got := transport.get("legato/test/" + key + "/state"); got != want {
			t.Errorf("%s = %q, want %s", key, got, want)
		}
	}
	transport.mu.Lock()
	discoveries := 0
	for topic, payload := range transport.messages {
		if len(topic) > 14 && topic[:14] == "homeassistant/" {
			discoveries++
			if payload == "" {
				t.Error("empty discovery")
			}
		}
	}
	transport.mu.Unlock()
	if discoveries != 10 {
		t.Fatalf("discovery count %d", discoveries)
	}
	src.mu.Lock()
	src.agents = nil
	src.items = nil
	src.approvals = nil
	src.mu.Unlock()
	bus.Publish(events.Event{Type: events.EventAgentDied})
	mqttAwait(t, func() bool {
		return transport.get("legato/test/agents_running/state") == "0" && transport.get("legato/test/needs_attention/state") == "OFF"
	})
	cancel()
	<-stopped
	if got := transport.get("legato/test/availability"); got != "offline" {
		t.Errorf("shutdown availability %q", got)
	}
}
