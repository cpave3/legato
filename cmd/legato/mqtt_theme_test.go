package main

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"regexp"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cpave3/legato/config"
	"github.com/cpave3/legato/internal/engine/events"
	"github.com/cpave3/legato/internal/service"
	"github.com/cpave3/legato/themes"
	"github.com/eclipse/paho.golang/paho"
	broker "github.com/mochi-mqtt/server/v2"
	"github.com/mochi-mqtt/server/v2/hooks/auth"
	"github.com/mochi-mqtt/server/v2/listeners"
)

type themeMQTTTest struct {
	t        *testing.T
	broker   *broker.Server
	address  string
	client   *paho.Client
	messages chan *paho.Publish
}

func themeBroker(t *testing.T) *themeMQTTTest { return themeBrokerHook(t, new(auth.AllowHook)) }
func themeBrokerHook(t *testing.T, hook broker.Hook) *themeMQTTTest {
	t.Helper()
	s := broker.New(&broker.Options{Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if err := s.AddHook(hook, nil); err != nil {
		t.Fatal(err)
	}
	l := listeners.NewTCP(listeners.Config{ID: "scratch", Address: "127.0.0.1:0"})
	if err := s.AddListener(l); err != nil {
		t.Fatal(err)
	}
	if err := s.Serve(); err != nil {
		t.Fatal(err)
	}
	f := &themeMQTTTest{t: t, broker: s, address: "mqtt://" + l.Address(), messages: make(chan *paho.Publish, 256)}
	t.Cleanup(func() {
		for _, c := range s.Clients.GetAll() {
			c.Stop(net.ErrClosed)
		}
		deadline := time.Now().Add(5 * time.Second)
		for s.Clients.Len() != 0 {
			if time.Now().After(deadline) {
				t.Error("scratch broker clients stuck")
				return
			}
			time.Sleep(time.Millisecond)
		}
		_ = s.Close()
	})
	conn, err := net.Dial("tcp", l.Address())
	if err != nil {
		t.Fatal(err)
	}
	f.client = paho.NewClient(paho.ClientConfig{Conn: conn, OnPublishReceived: []func(paho.PublishReceived) (bool, error){func(p paho.PublishReceived) (bool, error) {
		select {
		case f.messages <- p.Packet:
		default:
		}
		return true, nil
	}}})
	if _, err := f.client.Connect(f.ctx(), &paho.Connect{ClientID: "test", CleanStart: true, KeepAlive: 15}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = f.client.Disconnect(&paho.Disconnect{}) })
	if _, err := f.client.Subscribe(f.ctx(), &paho.Subscribe{Subscriptions: []paho.SubscribeOptions{{Topic: "#", QoS: 1}}}); err != nil {
		t.Fatal(err)
	}
	return f
}
func (f *themeMQTTTest) ctx() context.Context {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	f.t.Cleanup(cancel)
	return ctx
}
func (f *themeMQTTTest) receive(topic string) *paho.Publish {
	f.t.Helper()
	for {
		select {
		case p := <-f.messages:
			if p.Topic == topic {
				return p
			}
		case <-time.After(5 * time.Second):
			f.t.Fatalf("no publication at %s", topic)
			return nil
		}
	}
}
func (f *themeMQTTTest) send(payload []byte, retain bool) {
	f.t.Helper()
	if _, err := f.client.Publish(f.ctx(), &paho.Publish{Topic: "test/theme/set", Payload: payload, QoS: 1, Retain: retain}); err != nil {
		f.t.Fatal(err)
	}
}
func (f *themeMQTTTest) command(session, id string, revision uint64) []byte {
	b, err := json.Marshal(map[string]any{"id": id, "session": session, "expected_revision": revision, "issued_at": time.Now().Unix()})
	if err != nil {
		f.t.Fatal(err)
	}
	return b
}
func (f *themeMQTTTest) state() service.ThemePreference {
	f.t.Helper()
	p := f.receive("test/theme/state")
	var state service.ThemePreference
	if err := json.Unmarshal(p.Payload, &state); err != nil {
		f.t.Fatal(err)
	}
	return state
}
func (f *themeMQTTTest) result() string {
	f.t.Helper()
	p := f.receive("test/theme/result")
	if p.Retain {
		f.t.Fatal("result retained")
	}
	var result struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(p.Payload, &result); err != nil {
		f.t.Fatal(err)
	}
	return result.Code
}

func TestMQTTThemeRuntimeToPreferenceAndState(t *testing.T) {
	f := themeBroker(t)
	db := channelsTestDB(t)
	bus := events.New()
	cfg := config.Defaults()
	cfg.DataDir = t.TempDir()
	cfg.Integrations.MQTT = config.MQTTConfig{ThemeControl: true, Broker: f.address, BaseTopic: "test", DiscoveryPrefix: "ha"}
	// Runtime identity selects the discovery node; use the persisted identity.
	id, err := service.InstanceID(context.Background(), db)
	if err != nil {
		t.Fatal(err)
	}
	stop := startMQTT(cfg, db, bus, nil, nil, nil)
	defer stop()
	p := f.receive("ha/select/" + id + "/theme/config")
	var d map[string]any
	if err := json.Unmarshal(p.Payload, &d); err != nil {
		t.Fatal(err)
	}
	if d["command_topic"] != "test/theme/set" || d["optimistic"] != false || d["qos"] != float64(0) {
		t.Fatalf("bad discovery: %s", p.Payload)
	}
	options := d["options"].([]any)
	if len(options) != len(themes.IDs())+1 || options[0] != "none" {
		t.Fatalf("options: %v", options)
	}
	session := themeSession(t, p.Payload)
	initial := f.state()
	if initial.ID != "none" {
		t.Fatalf("initial state: %+v", initial)
	}
	f.send(f.command(session, themes.IDs()[0], initial.Revision), false)
	if code := f.result(); code != "ok" {
		t.Fatalf("result: %s", code)
	}
	next := f.state()
	if next.ID != themes.IDs()[0] || next.Revision != initial.Revision+1 {
		t.Fatalf("state: %+v", next)
	}
	current, err := service.NewThemePreferenceService(db, bus).Current(context.Background())
	if err != nil || current.ID != next.ID || current.Revision != next.Revision {
		t.Fatalf("preference: %+v %v", current, err)
	}
	f.send(f.command(session, "none", next.Revision), false)
	if code := f.result(); code != "ok" {
		t.Fatal(code)
	}
	if state := f.state(); state.ID != "none" || state.Revision != next.Revision+1 {
		t.Fatalf("clear: %+v", state)
	}
}

func TestMQTTThemeInvalidEnvelopesNeverMutate(t *testing.T) {
	f := themeBroker(t)
	db := channelsTestDB(t)
	bus := events.New()
	cfg := config.Defaults()
	cfg.DataDir = t.TempDir()
	cfg.Integrations.MQTT = config.MQTTConfig{ThemeControl: true, Broker: f.address, BaseTopic: "test", DiscoveryPrefix: "ha"}
	id, err := service.InstanceID(context.Background(), db)
	if err != nil {
		t.Fatal(err)
	}
	stop := startMQTT(cfg, db, bus, nil, nil, nil)
	defer stop()
	discovery := f.receive("ha/select/" + id + "/theme/config")
	session := themeSession(t, discovery.Payload)
	initial := f.state()
	valid := string(f.command(session, "dracula", initial.Revision))
	cases := []struct{ name, payload, code string }{
		{"oversized", strings.Repeat("x", 2049), "invalid"},
		{"unknown", strings.Replace(valid, `"id":"dracula"`, `"id":"untrusted"`, 1), "unknown_theme"},
		{"empty", strings.Replace(valid, `"id":"dracula"`, `"id":""`, 1), "unknown_theme"},
		{"unknown field", strings.TrimSuffix(valid, "}") + `,"actor":"web"}`, "invalid"},
		{"duplicate", strings.TrimSuffix(valid, "}") + `,"id":"legato"}`, "invalid"},
		{"escaped duplicate", strings.TrimSuffix(valid, "}") + `,"\u0069d":"legato"}`, "invalid"},
		{"trailing", valid + ` {}`, "invalid"},
		{"null", strings.Replace(valid, `"expected_revision":0`, `"expected_revision":null`, 1), "invalid"},
		{"negative revision", strings.Replace(valid, `"expected_revision":0`, `"expected_revision":-1`, 1), "invalid"},
		{"missing", `{"id":"dracula"}`, "invalid"},
		{"case alias", strings.Replace(valid, `"id":`, `"ID":`, 1), "invalid"},
		{"bad UTF8", strings.Replace(valid, "dracula", string([]byte{0xff}), 1), "invalid"},
		{"fraction revision", strings.Replace(valid, `"expected_revision":0`, `"expected_revision":0.0`, 1), "invalid"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			time.Sleep(210 * time.Millisecond)
			f.send([]byte(tc.payload), false)
			if code := f.result(); code != tc.code {
				t.Fatalf("result: %s", code)
			}
		})
	}
	for _, offset := range []int64{-30, -31, 6, -1 << 62, 1 << 62} {
		time.Sleep(210 * time.Millisecond)
		var envelope map[string]any
		if err := json.Unmarshal([]byte(valid), &envelope); err != nil {
			t.Fatal(err)
		}
		envelope["issued_at"] = time.Now().Unix() + offset
		payload, _ := json.Marshal(envelope)
		f.send(payload, false)
		if code := f.result(); code != "expired" {
			t.Fatalf("time offset %d: %s", offset, code)
		}
	}
	current, err := service.NewThemePreferenceService(db, bus).Current(context.Background())
	if err != nil || current.Revision != initial.Revision || current.ID != "" {
		t.Fatalf("invalid command mutated: %+v %v", current, err)
	}
	// A valid command after refusals proves processing stayed live.
	time.Sleep(210 * time.Millisecond)
	f.send([]byte(valid), false)
	if code := f.result(); code != "ok" {
		t.Fatal(code)
	}
	if state := f.state(); state.ID != "dracula" {
		t.Fatalf("valid command: %+v", state)
	}
}
func themeSession(t *testing.T, payload []byte) string {
	t.Helper()
	var d struct {
		Template string `json:"command_template"`
	}
	if err := json.Unmarshal(payload, &d); err != nil {
		t.Fatal(err)
	}
	matches := regexp.MustCompile(`"session": "([a-f0-9]{64})"`).FindStringSubmatch(d.Template)
	if len(matches) != 2 {
		t.Fatalf("session missing: %s", d.Template)
	}
	return matches[1]
}

func TestMQTTThemeRetainedReconnectAndRevisionProtection(t *testing.T) {
	f := themeBroker(t)
	db := channelsTestDB(t)
	bus := events.New()
	cfg := config.Defaults()
	cfg.DataDir = t.TempDir()
	cfg.Integrations.MQTT = config.MQTTConfig{ThemeControl: true, Broker: f.address, BaseTopic: "test", DiscoveryPrefix: "ha"}
	id, err := service.InstanceID(context.Background(), db)
	if err != nil {
		t.Fatal(err)
	}
	// Replay suppression is exercised on the actual runtime's exact subscription.
	f.send(f.command("previous-session", "dracula", 0), true)
	stop := startMQTT(cfg, db, bus, nil, nil, nil)
	defer stop()
	session := themeSession(t, f.receive("ha/select/"+id+"/theme/config").Payload)
	initial := f.state()
	f.receive("test/theme/availability")
	// No result for replayed retained commands; live retained commands are rejected.
	select {
	case p := <-f.messages:
		if p.Topic == "test/theme/result" {
			t.Fatal("retained replay delivered")
		}
	case <-time.After(30 * time.Millisecond):
	}
	command := f.command(session, "dracula", initial.Revision)
	f.send(command, true)
	if code := f.result(); code != "retained" {
		t.Fatalf("live retain: %s", code)
	}
	f.send(command, false)
	if code := f.result(); code != "ok" {
		t.Fatalf("valid: %s", code)
	}
	next := f.state()
	f.send(command, false)
	if code := f.result(); code != "conflict" {
		t.Fatalf("duplicate: %s", code)
	}
	f.state()
	remote, ok := f.broker.Clients.Get("legato-" + id + "-theme")
	if !ok {
		t.Fatal("theme connection absent")
	}
	if err := remote.Net.Conn.Close(); err != nil {
		t.Fatal(err)
	}
	// This nonretained command while disconnected must not be queued offline.
	f.send(f.command(session, "legato", next.Revision), false)
	newSession := themeSession(t, f.receive("ha/select/"+id+"/theme/config").Payload)
	if newSession == session {
		t.Fatal("session survived reconnect")
	}
	if state := f.state(); state != next {
		t.Fatalf("offline or retained command mutated: %+v != %+v", state, next)
	}
	f.receive("test/theme/availability")
	f.send(f.command(session, "legato", next.Revision), false)
	if code := f.result(); code != "session" {
		t.Fatalf("old discovery: %s", code)
	}
	// Re-read and publish manual changes on the supplied cross-process invalidation.
	other := service.NewThemePreferenceService(db, nil)
	saved, err := other.Change(context.Background(), next.Revision, "ocular-dark", "web")
	if err != nil {
		t.Fatal(err)
	}
	bus.Publish(events.Event{Type: events.EventServerThemeInvalidated})
	if state := f.state(); state != saved {
		t.Fatalf("manual change state: %+v != %+v", state, saved)
	}
	f.send(f.command(newSession, "legato", next.Revision), false)
	if code := f.result(); code != "conflict" {
		t.Fatalf("stale revision: %s", code)
	}
	f.state()
	f.send(f.command(newSession, "legato", saved.Revision), false)
	if code := f.result(); code != "ok" {
		t.Fatalf("fresh: %s", code)
	}
	state := f.state()
	if state.ID != "legato" || state.Revision != saved.Revision+1 {
		t.Fatalf("fresh state: %+v", state)
	}
	// Late subscribers get authoritative retained state, not optimistic commands.
	if _, err := f.client.Subscribe(f.ctx(), &paho.Subscribe{Subscriptions: []paho.SubscribeOptions{{Topic: "test/theme/state", QoS: 1}}}); err != nil {
		t.Fatal(err)
	}
	for {
		p := f.receive("test/theme/state")
		if !p.Retain {
			continue
		}
		var retained service.ThemePreference
		if err := json.Unmarshal(p.Payload, &retained); err != nil {
			t.Fatal(err)
		}
		if retained != state {
			t.Fatalf("retained state: %+v != %+v", retained, state)
		}
		break
	}
	// Clean shutdown has its own availability, never aggregate availability.
	stop()
	if p := f.receive("test/theme/availability"); string(p.Payload) != "offline" {
		t.Fatalf("shutdown availability: %s", p.Payload)
	}
}

type aggregateOnlyMQTTACL struct {
	auth.AllowHook
	unexpected atomic.Int32
}

func (h *aggregateOnlyMQTTACL) ID() string { return "aggregate-only" }
func (h *aggregateOnlyMQTTACL) OnACLCheck(c *broker.Client, topic string, write bool) bool {
	if c.ID == "test" {
		return true
	}
	if write {
		if strings.Contains(topic, "/theme/") {
			h.unexpected.Add(1)
			return false
		}
		return true
	}
	if topic == "ha/status" {
		return true
	}
	h.unexpected.Add(1)
	return false
}
func TestMQTTThemeDisabledPreservesAggregateOnlyACL(t *testing.T) {
	hook := new(aggregateOnlyMQTTACL)
	f := themeBrokerHook(t, hook)
	db := channelsTestDB(t)
	bus := events.New()
	cfg := config.Defaults()
	cfg.DataDir = t.TempDir()
	cfg.Integrations.MQTT = config.MQTTConfig{Enabled: true, Broker: f.address, BaseTopic: "test", DiscoveryPrefix: "ha"}
	stop := startMQTT(cfg, db, bus, nil, service.NewAttentionService(db, bus), &approvalRuntime{approvals: service.NewApprovalService(db, service.ApprovalServiceOptions{})})
	defer stop()
	if p := f.receive("test/availability"); string(p.Payload) != "online" {
		t.Fatalf("aggregate unavailable: %s", p.Payload)
	}
	// An exact-topic broker writer has no theme authority without the opt-in.
	f.send(f.command("anything", "dracula", 0), false)
	// A subsequent HA birth and aggregate resync form the processing barrier.
	if _, err := f.client.Publish(f.ctx(), &paho.Publish{Topic: "ha/status", Payload: []byte("online"), QoS: 1}); err != nil {
		t.Fatal(err)
	}
	f.receive("test/availability")
	if hook.unexpected.Load() != 0 {
		t.Fatal("disabled control expanded aggregate subscriptions")
	}
	if f.broker.Clients.Len() != 2 {
		t.Fatalf("disabled theme opened extra connection: %d", f.broker.Clients.Len())
	}
	current, err := service.NewThemePreferenceService(db, bus).Current(context.Background())
	if err != nil || current.ID != "" || current.Revision != 0 {
		t.Fatalf("disabled mutation: %+v %v", current, err)
	}
}

func TestMQTTThemeRateLimitsMutationsAndRecovers(t *testing.T) {
	f := themeBroker(t)
	db := channelsTestDB(t)
	bus := events.New()
	cfg := config.Defaults()
	cfg.DataDir = t.TempDir()
	cfg.Integrations.MQTT = config.MQTTConfig{ThemeControl: true, Broker: f.address, BaseTopic: "test", DiscoveryPrefix: "ha"}
	id, err := service.InstanceID(context.Background(), db)
	if err != nil {
		t.Fatal(err)
	}
	stop := startMQTT(cfg, db, bus, nil, nil, nil)
	defer stop()
	session := themeSession(t, f.receive("ha/select/"+id+"/theme/config").Payload)
	f.state()
	f.receive("test/theme/availability")
	preference := service.NewThemePreferenceService(db, bus)
	started := time.Now()
	// Keep the queue uncongested: the worker must rate-limit, not just rely on
	// the transport dropping a single instantaneous burst.
	for i := 0; i < 30; i++ {
		current, err := preference.Current(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		f.send(f.command(session, "dracula", current.Revision), false)
		time.Sleep(10 * time.Millisecond)
	}
	time.Sleep(250 * time.Millisecond)
	current, err := preference.Current(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	budget := uint64(5 + time.Since(started).Seconds()*5)
	if current.Revision == 0 || current.Revision > budget {
		t.Fatalf("rate budget=%d, mutations=%d", budget, current.Revision)
	}
	// Drain old publications before checking that a new, fresh command succeeds.
	for len(f.messages) > 0 {
		<-f.messages
	}
	f.send(f.command(session, "ocular-light", current.Revision), false)
	if code := f.result(); code != "ok" {
		t.Fatalf("rate recovery: %s", code)
	}
	if state := f.state(); state.ID != "ocular-light" || state.Revision != current.Revision+1 {
		t.Fatalf("recovery state: %+v", state)
	}
}
