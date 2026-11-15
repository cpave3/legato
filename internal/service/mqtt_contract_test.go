package service

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/cpave3/legato/internal/engine/events"
	"github.com/cpave3/legato/internal/engine/store"
)

func mqttRunTestPublisher(t *testing.T, p *MQTTPublisher) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	stopped := make(chan struct{})
	go func() { defer close(stopped); p.Run(ctx) }()
	t.Cleanup(func() { cancel(); <-stopped })
}

func mqttNewTestTransport() *mqttTestTransport {
	return &mqttTestTransport{messages: map[string]string{}, done: make(chan struct{})}
}

func mqttTestOptions() MQTTOptions {
	return MQTTOptions{InstanceID: "instance-1", BaseTopic: "legato/test", DeviceName: "Test", DiscoveryPrefix: "homeassistant", SoftwareVersion: "test-version"}
}

func mqttStartSources(t *testing.T, opts MQTTOptions, src *mqttTestSources, bus *events.Bus, transport *mqttTestTransport) *MQTTPublisher {
	t.Helper()
	p := NewMQTTPublisher(opts, src, src, mqttTestApprovals{src}, bus, func(context.Context) (MQTTConnection, error) { return transport, nil })
	mqttRunTestPublisher(t, p)
	mqttAwait(t, func() bool { return transport.get("legato/test/availability") == "online" })
	return p
}

func TestMQTTPublisherPrivacyAndBoundedSafeAttributes(t *testing.T) {
	for _, details := range []bool{false, true} {
		t.Run(map[bool]string{false: "counts only", true: "safe details"}[details], func(t *testing.T) {
			secret := "SECRET-token /home/operator/private https://private.example/run?password=hunter2"
			src := &mqttTestSources{agents: []AgentSession{{Status: "running", Activity: "waiting", Title: secret, TaskID: secret}}, approvals: []Approval{{Approval: store.Approval{Title: secret}}}}
			for i := 0; i < 100; i++ {
				kind := AttentionSignalApproval
				if i >= 10 {
					kind = secret
				}
				src.items = append(src.items, AttentionItem{SignalKind: kind, Title: secret, Detail: secret, Reason: secret, TaskID: secret})
			}
			opts := mqttTestOptions()
			opts.AttentionDetails = details
			transport := mqttNewTestTransport()
			if !details {
				transport.messages["legato/test/attention_items/attributes"] = `{"items":[{"title":"old retained detail"}]}`
			}
			mqttStartSources(t, opts, src, events.New(), transport)
			transport.mu.Lock()
			messages := append([]mqttTestMessage(nil), transport.history...)
			transport.mu.Unlock()
			for _, msg := range messages {
				for _, forbidden := range []string{"SECRET-token", "/home/operator", "private.example", "hunter2"} {
					if strings.Contains(msg.topic+msg.payload, forbidden) {
						t.Fatalf("private source text exported on %s", msg.topic)
					}
				}
			}
			payload := transport.get("legato/test/attention_items/attributes")
			discovery := transport.get("homeassistant/sensor/instance-1/attention_items/config")
			if !details {
				if payload != "" || strings.Contains(discovery, "json_attributes_topic") {
					t.Fatal("counts-only mode exports attributes")
				}
				return
			}
			if len(payload) > 8*1024 {
				t.Fatalf("attributes exceed 8 KiB: %d", len(payload))
			}
			var attrs struct {
				Items     []map[string]string `json:"items"`
				Total     int                 `json:"total"`
				Truncated bool                `json:"truncated"`
			}
			if err := json.Unmarshal([]byte(payload), &attrs); err != nil {
				t.Fatal(err)
			}
			if len(attrs.Items) != 20 || attrs.Total != 100 || !attrs.Truncated {
				t.Fatalf("bounds: %+v", attrs)
			}
			previous := ""
			for _, item := range attrs.Items {
				if len(item) != 2 || item["kind"] < previous {
					t.Fatalf("non-deterministic or unsafe item: %v", item)
				}
				previous = item["kind"]
				if !((item["kind"] == "approval" && item["title"] == "Approval pending") || (item["kind"] == "attention" && item["title"] == "Attention required")) {
					t.Fatalf("not a generated safe label: %v", item)
				}
			}
			if !strings.Contains(discovery, `"json_attributes_topic":"legato/test/attention_items/attributes"`) {
				t.Fatal("detail discovery missing attributes topic")
			}
		})
	}
}

func TestMQTTPublisherIncludesReeveWhenRequested(t *testing.T) {
	src := &mqttTestSources{agents: []AgentSession{{Status: "running", Activity: "waiting", Reeve: true}, {Status: "running", Activity: "working"}, {Status: "interrupted", Activity: "waiting", Reeve: true}}}
	opts := mqttTestOptions()
	opts.IncludeReeve = true
	transport := mqttNewTestTransport()
	mqttStartSources(t, opts, src, events.New(), transport)
	for key, want := range map[string]string{"agents_running": "2", "agents_working": "1", "agents_waiting": "1", "agent_waiting_on_you": "ON"} {
		if got := transport.get("legato/test/" + key + "/state"); got != want {
			t.Errorf("%s=%q want %s", key, got, want)
		}
	}
}
