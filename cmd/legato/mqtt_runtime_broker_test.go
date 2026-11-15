package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cpave3/legato/config"
	"github.com/cpave3/legato/internal/engine/events"
	"github.com/cpave3/legato/internal/engine/ipc"
	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
	"github.com/eclipse/paho.golang/paho"
	mochi "github.com/mochi-mqtt/server/v2"
	"github.com/mochi-mqtt/server/v2/hooks/auth"
	"github.com/mochi-mqtt/server/v2/listeners"
)

func mqttRuntimeBroker(t *testing.T) string {
	t.Helper()
	b := mochi.New(&mochi.Options{Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if err := b.AddHook(new(auth.AllowHook), nil); err != nil {
		t.Fatal(err)
	}
	listener := listeners.NewTCP(listeners.Config{ID: "runtime-test", Address: "127.0.0.1:0"})
	if err := b.AddListener(listener); err != nil {
		t.Fatal(err)
	}
	if err := b.Serve(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = b.Close() })
	return "mqtt://" + listener.Address()
}

func mqttRuntimeContext(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	return ctx
}

func mqttRuntimeObserver(t *testing.T, address, topic string) (*paho.Client, <-chan *paho.Publish) {
	t.Helper()
	conn, err := net.DialTimeout("tcp", strings.TrimPrefix(address, "mqtt://"), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	messages := make(chan *paho.Publish, 128)
	client := paho.NewClient(paho.ClientConfig{Conn: conn, OnPublishReceived: []func(paho.PublishReceived) (bool, error){func(p paho.PublishReceived) (bool, error) {
		messages <- p.Packet
		return true, nil
	}}})
	if _, err := client.Connect(mqttRuntimeContext(t), &paho.Connect{ClientID: "runtime-observer-" + topic, CleanStart: true, KeepAlive: 15}); err != nil {
		_ = conn.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Disconnect(&paho.Disconnect{}); _ = conn.Close() })
	if _, err := client.Subscribe(mqttRuntimeContext(t), &paho.Subscribe{Subscriptions: []paho.SubscribeOptions{{Topic: topic, QoS: 1}}}); err != nil {
		t.Fatal(err)
	}
	return client, messages
}

func mqttRuntimeReceive(t *testing.T, messages <-chan *paho.Publish) *paho.Publish {
	t.Helper()
	select {
	case message := <-messages:
		return message
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for runtime MQTT publication")
		return nil
	}
}

func mqttRuntimeFixture(t *testing.T) (*config.Config, *store.Store, *events.Bus, *service.AttentionService, *approvalRuntime) {
	t.Helper()
	t.Setenv("XDG_RUNTIME_DIR", shortDir(t))
	db := channelsTestDB(t)
	bus := events.New()
	attention := service.NewAttentionService(db, bus)
	approvals := &approvalRuntime{approvals: service.NewApprovalService(db, service.ApprovalServiceOptions{Bus: bus})}
	cfg := &config.Config{DataDir: t.TempDir(), Integrations: config.IntegrationsConfig{MQTT: config.MQTTConfig{Enabled: true, Broker: mqttRuntimeBroker(t), DeviceName: "Runtime test"}}}
	return cfg, db, bus, attention, approvals
}

func TestMQTTRuntimeBrokerDiscoveryDurableStateAndStop(t *testing.T) {
	cfg, db, bus, attention, approvals := mqttRuntimeFixture(t)
	ctx := context.Background()
	for _, seed := range []struct{ id, status, activity string }{{"working", "running", "working"}, {"waiting", "running", "waiting"}, {"idle", "running", ""}, {"background", "running", "background"}, {"error", "running", "error"}, {"dead", "dead", ""}} {
		if err := db.CreateTask(ctx, store.Task{ID: seed.id, Title: "private title", Status: "Doing"}); err != nil {
			t.Fatal(err)
		}
		if err := db.InsertAgentSession(ctx, store.AgentSession{TaskID: seed.id, TmuxSession: "not-a-live-session-" + seed.id, Status: seed.status}); err != nil {
			t.Fatal(err)
		}
		if seed.activity != "" {
			if err := db.UpdateAgentActivity(ctx, seed.id, seed.activity); err != nil {
				t.Fatal(err)
			}
		}
	}
	reeve, err := db.CreateReeveTask(ctx, "private reeve")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.InsertAgentSession(ctx, store.AgentSession{TaskID: reeve, TmuxSession: "not-a-live-reeve", Status: "running"}); err != nil {
		t.Fatal(err)
	}
	if _, err := attention.Upsert(ctx, service.AttentionInput{Producer: "runtime-test", SourceKey: "plan", SourceRevision: "1", SignalKind: service.AttentionSignalPlanDecision, Title: "private title", Reason: "private reason", Category: "plan", Urgency: "normal", Navigation: service.AttentionNavigation{Kind: "task", Target: "working"}}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := approvals.approvals.Submit(ctx, service.ApprovalInput{Source: "runtime-test", SourceKey: "request", Title: "private approval"}); err != nil {
		t.Fatal(err)
	}
	id, err := service.InstanceID(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	base := "legato/" + id
	_, messages := mqttRuntimeObserver(t, cfg.Integrations.MQTT.Broker, "#")
	stop := startMQTT(cfg, db, bus, nil, attention, approvals)
	t.Cleanup(stop)
	want := map[string]string{"agents_running": "5", "agents_working": "1", "agents_waiting": "1", "agents_idle": "1", "agents_background": "1", "agents_error": "1", "attention_items": "1", "pending_approvals": "1", "needs_attention": "ON", "agent_waiting_on_you": "ON"}
	states := map[string]string{}
	discovery := map[string]bool{}
	for {
		p := mqttRuntimeReceive(t, messages)
		if p.QoS != 1 {
			t.Fatalf("runtime publication is not QoS1: %+v", p)
		}
		if strings.Contains(string(p.Payload), "private") {
			t.Fatalf("private source content exported on %s", p.Topic)
		}
		if p.Topic == base+"/availability" && string(p.Payload) == "online" {
			break
		}
		if strings.HasSuffix(p.Topic, "/state") {
			states[strings.TrimSuffix(strings.TrimPrefix(p.Topic, base+"/"), "/state")] = string(p.Payload)
		}
		if strings.HasSuffix(p.Topic, "/config") {
			var d struct {
				UniqueID          string `json:"unique_id"`
				StateTopic        string `json:"state_topic"`
				AvailabilityTopic string `json:"availability_topic"`
				Device            struct {
					Identifiers []string `json:"identifiers"`
					Name        string   `json:"name"`
				} `json:"device"`
			}
			if err := json.Unmarshal(p.Payload, &d); err != nil {
				t.Fatal(err)
			}
			key := strings.TrimSuffix(strings.TrimPrefix(d.StateTopic, base+"/"), "/state")
			component := "sensor"
			if key == "needs_attention" || key == "agent_waiting_on_you" {
				component = "binary_sensor"
			}
			if _, ok := want[key]; !ok || d.UniqueID != id+"_"+key || d.AvailabilityTopic != base+"/availability" || d.Device.Name != "Runtime test" || len(d.Device.Identifiers) != 1 || d.Device.Identifiers[0] != id || p.Topic != "homeassistant/"+component+"/"+id+"/"+key+"/config" {
				t.Fatalf("invalid discovery on %s: %s", p.Topic, p.Payload)
			}
			discovery[key] = true
		}
	}
	if len(discovery) != 10 {
		t.Fatalf("got %d discovery entities, want ten", len(discovery))
	}
	for key, value := range want {
		if states[key] != value {
			t.Errorf("%s = %q, want %q", key, states[key], value)
		}
	}
	// Late external subscribers must see every retained discovery and state.
	_, configs := mqttRuntimeObserver(t, cfg.Integrations.MQTT.Broker, "homeassistant/#")
	retainedDiscovery := map[string]bool{}
	for len(retainedDiscovery) < 10 {
		p := mqttRuntimeReceive(t, configs)
		if !p.Retain || p.QoS != 1 || !strings.HasSuffix(p.Topic, "/config") || !json.Valid(p.Payload) {
			t.Fatalf("discovery not retained QoS1 JSON: %+v", p)
		}
		retainedDiscovery[p.Topic] = true
	}
	_, retained := mqttRuntimeObserver(t, cfg.Integrations.MQTT.Broker, base+"/#")
	retainedStates := map[string]bool{}
	for len(retainedStates) < 11 {
		p := mqttRuntimeReceive(t, retained)
		key := strings.TrimSuffix(strings.TrimPrefix(p.Topic, base+"/"), "/state")
		expected, ok := want[key]
		if p.Topic == base+"/availability" {
			expected, ok = "online", true
		}
		if !ok || string(p.Payload) != expected || !p.Retain || p.QoS != 1 {
			t.Fatalf("unexpected retained current state: %+v", p)
		}
		retainedStates[p.Topic] = true
	}
	stop()
	stop()
	if p := mqttRuntimeReceive(t, messages); p.Topic != base+"/availability" || string(p.Payload) != "offline" {
		t.Fatalf("stop did not publish offline: %+v", p)
	}
	_, offline := mqttRuntimeObserver(t, cfg.Integrations.MQTT.Broker, base+"/availability")
	if p := mqttRuntimeReceive(t, offline); string(p.Payload) != "offline" || !p.Retain || p.QoS != 1 {
		t.Fatalf("stop did not retain offline QoS1: %+v", p)
	}
}

func TestMQTTRuntimeDisabledNeedsNoDependencies(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", shortDir(t))
	cfg := &config.Config{DataDir: t.TempDir(), Integrations: config.IntegrationsConfig{MQTT: config.MQTTConfig{Broker: mqttRuntimeBroker(t), Credential: "unavailable-secret"}}}
	_, messages := mqttRuntimeObserver(t, cfg.Integrations.MQTT.Broker, "#")
	stop := startMQTT(cfg, nil, nil, nil, nil, nil)
	stop()
	stop()
	if _, err := os.Stat(filepath.Join(cfg.DataDir, "mqtt.lock")); !os.IsNotExist(err) {
		t.Fatalf("disabled runtime touched ownership: %v", err)
	}
	select {
	case p := <-messages:
		t.Fatalf("disabled runtime published: %+v", p)
	case <-time.After(150 * time.Millisecond):
	}
}

func TestMQTTRuntimeNonOwnerStopLeavesOwnerOnline(t *testing.T) {
	cfg, db, bus, attention, approvals := mqttRuntimeFixture(t)
	cfg.Integrations.MQTT.BaseTopic = "runtime/owner"
	_, messages := mqttRuntimeObserver(t, cfg.Integrations.MQTT.Broker, "runtime/owner/#")
	stop := startMQTT(cfg, db, bus, nil, attention, approvals)
	t.Cleanup(stop)
	for {
		p := mqttRuntimeReceive(t, messages)
		if p.Topic == "runtime/owner/availability" && string(p.Payload) == "online" {
			break
		}
	}
	// No sources or credential broker: a contender must not get past the lock.
	contender := *cfg
	contender.Integrations.MQTT.Credential = "must-not-read"
	stopSecond := startMQTT(&contender, nil, events.New(), nil, nil, nil)
	stopSecond()
	stopSecond()
	select {
	case p := <-messages:
		t.Fatalf("non-owner disturbed owner: %+v", p)
	case <-time.After(150 * time.Millisecond):
	}
	// A real source mutation still reaches the original owner's connection.
	if _, _, err := approvals.approvals.Submit(context.Background(), service.ApprovalInput{Source: "runtime-test", SourceKey: "after-contender-stop", Title: "private approval"}); err != nil {
		t.Fatal(err)
	}
	for {
		p := mqttRuntimeReceive(t, messages)
		if p.Topic == "runtime/owner/availability" && string(p.Payload) == "offline" {
			t.Fatal("contender stopped owner's publisher")
		}
		if p.Topic == "runtime/owner/pending_approvals/state" && string(p.Payload) == "1" {
			break
		}
	}
	_, late := mqttRuntimeObserver(t, cfg.Integrations.MQTT.Broker, "runtime/owner/availability")
	if p := mqttRuntimeReceive(t, late); string(p.Payload) != "online" || !p.Retain {
		t.Fatalf("owner no longer online: %+v", p)
	}
}

func TestMQTTRuntimeLoginSafeErrorsAndRefresh(t *testing.T) {
	ctx := context.Background()
	var logs bytes.Buffer
	previous := log.Writer()
	log.SetOutput(&logs)
	t.Cleanup(func() { log.SetOutput(previous) })
	if user, pass, err := mqttLogin(ctx, nil, "", "mqtt://127.0.0.1:1883"); err != nil || user != "" || pass != "" {
		t.Fatal("anonymous login required credentials")
	}
	db := channelsTestDB(t)
	broker := channelsTestBroker(t, db,
		config.CredentialConfig{Name: "wrong-type", Secret: "env://MQTT_RUNTIME_WRONG", Type: "value", Profile: "secret"},
		config.CredentialConfig{Name: "login", Secret: "env://MQTT_RUNTIME_LOGIN", Type: "userpass", Profile: "mqtt"},
	)
	t.Setenv("MQTT_RUNTIME_WRONG", "private-wrong-type-value")
	for _, tc := range []struct {
		name, value, want string
		broker            *service.CredentialBroker
	}{
		{"unavailable", "", "credential broker unavailable", nil},
		{"missing-private-credential", "", "could not be read", broker},
		// The shipped mqtt profile rejects value credentials at construction;
		// a value in another profile must also be refused by mqttLogin.
		{"wrong-type", "", "host-only mqtt profile", broker},
		{"login", `{"username":"private-user","password":"private-pass"`, "nonempty username and password", broker},
		{"login", `{"username":"private-user","password":""}`, "nonempty username and password", broker},
	} {
		t.Setenv("MQTT_RUNTIME_LOGIN", tc.value)
		user, pass, err := mqttLogin(ctx, tc.broker, tc.name, "mqtt://127.0.0.1:1883")
		if err == nil || !strings.Contains(err.Error(), tc.want) || user != "" || pass != "" {
			t.Fatalf("%s: expected safe %q error and no login", tc.name, tc.want)
		}
		for _, secret := range []string{"private-user", "private-pass", "private-wrong-type-value", "missing-private-credential"} {
			if strings.Contains(err.Error(), secret) || strings.Contains(logs.String(), secret) {
				t.Fatal("credential material leaked in error or log")
			}
		}
	}
	// Use a fresh broker/store so these are exactly the two successful reads.
	db2 := channelsTestDB(t)
	broker2 := channelsTestBroker(t, db2, config.CredentialConfig{Name: "login", Secret: "env://MQTT_RUNTIME_LOGIN", Type: "userpass", Profile: "mqtt"})
	resource := "mqtt://127.0.0.1:1883"
	for _, password := range []string{"private-first-password", "private-refreshed-password"} {
		t.Setenv("MQTT_RUNTIME_LOGIN", `{"username":"private-user","password":"`+password+`"}`)
		user, pass, err := mqttLogin(ctx, broker2, "login", resource)
		if err != nil || user != "private-user" || pass != password {
			t.Fatal("mqttLogin did not read current userpass secret")
		}
	}
	entries, err := broker2.Audit(ctx, store.CredentialAuditFilter{Credential: "login"})
	if err != nil || len(entries) != 2 {
		t.Fatalf("want two audited reads, got %d: %v", len(entries), err)
	}
	for _, entry := range entries {
		if entry.Event != "use" || entry.Operation != "connect" || entry.Resource != resource || !strings.Contains(entry.Detail, "MQTT publisher") {
			t.Fatalf("wrong audit metadata: %+v", entry)
		}
	}
	audit, err := json.Marshal(entries)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"private-user", "private-first-password", "private-refreshed-password"} {
		if strings.Contains(string(audit), secret) || strings.Contains(logs.String(), secret) {
			t.Fatal("successful login leaked credential material")
		}
	}
}

func TestMQTTRuntimeNonOwnerIPCForwardsContentFreeSourceChanges(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", shortDir(t))
	messages := make(chan ipc.Message, 16)
	server, err := ipc.NewServer(filepath.Join(ipc.SocketDir(), "mqtt-test.sock"), func(message ipc.Message) { messages <- message })
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = server.Close() })
	cfg := &config.Config{DataDir: t.TempDir(), Integrations: config.IntegrationsConfig{MQTT: config.MQTTConfig{Enabled: true, Broker: "must-not-dial", Credential: "must-not-read"}}}
	release, err := claimMQTTPublisher(cfg.DataDir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(release)
	bus := events.New()
	stop := startMQTT(cfg, nil, bus, nil, nil, nil)
	t.Cleanup(stop)
	for _, kind := range []events.EventType{events.EventAgentStarted, events.EventAgentActivityChanged, events.EventAgentCompleted, events.EventAgentDied, events.EventAttentionChanged, events.EventApprovalChanged} {
		bus.Publish(events.Event{Type: kind, Payload: "private task title, credential and path"})
		select {
		case message := <-messages:
			if message != (ipc.Message{Type: "mqtt_changed"}) {
				t.Fatalf("source event exported content: %+v", message)
			}
		case <-time.After(time.Second):
			t.Fatalf("source event %v not bridged", kind)
		}
	}
	// Incoming invalidation reaches both handlers but cannot echo onto IPC.
	invalidations, cancel := bus.SubscribeInvalidations(events.EventMQTTInvalidated)
	defer cancel()
	for _, serve := range []bool{false, true} {
		if serve {
			handleServeIPCMessage(ipc.Message{Type: "mqtt_changed"}, bus, &recordingServeNotifier{})
		} else {
			handleIPCMessage(ipc.Message{Type: "mqtt_changed"}, bus, nil)
		}
		select {
		case <-invalidations:
		case <-time.After(time.Second):
			t.Fatal("incoming MQTT IPC did not invalidate publisher")
		}
	}
	select {
	case message := <-messages:
		t.Fatalf("incoming IPC echoed: %+v", message)
	case <-time.After(150 * time.Millisecond):
	}
	stop()
	bus.Publish(events.Event{Type: events.EventApprovalChanged, Payload: "private after stop"})
	select {
	case message := <-messages:
		t.Fatalf("stopped bridge still forwards: %+v", message)
	case <-time.After(150 * time.Millisecond):
	}
}
