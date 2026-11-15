package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/cpave3/legato/config"
	"github.com/cpave3/legato/internal/engine/credential"
	"github.com/cpave3/legato/internal/engine/events"
	"github.com/cpave3/legato/internal/engine/ipc"
	"github.com/cpave3/legato/internal/engine/mqtt"
	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/engine/version"
	"github.com/cpave3/legato/internal/service"
	"golang.org/x/sys/unix"
)

// claimMQTTPublisher keeps the inode in place: removing it on unlock could
// allow two processes to lock different files with the same path.
func claimMQTTPublisher(dataDir string) (func(), error) {
	if err := os.MkdirAll(dataDir, 0700); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(filepath.Join(dataDir, "mqtt.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err := unix.Flock(int(file.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		_ = file.Close()
		return nil, err
	}
	var once sync.Once
	return func() { once.Do(func() { _ = unix.Flock(int(file.Fd()), unix.LOCK_UN); _ = file.Close() }) }, nil
}

// startMQTT is process-owned, not web-server-owned. It also runs in a TUI
// with web.enabled false; short-lived CLI commands never call it.
func startMQTT(cfg *config.Config, db *store.Store, bus *events.Bus, agents service.AgentService, attention *service.AttentionService, approvals *approvalRuntime) func() {
	m := cfg.Integrations.MQTT
	if !m.Enabled {
		return func() {}
	}
	stopBridge := startMQTTInvalidations(bus)
	release, err := claimMQTTPublisher(config.ResolveDataDir(cfg))
	if err != nil {
		log.Print("MQTT: inactive; cannot claim publisher lock (another process may own it)")
		return stopBridge
	}
	id, err := service.InstanceID(context.Background(), db)
	if err != nil {
		release()
		log.Print("MQTT: cannot read persistent instance identity")
		return stopBridge
	}
	if m.BaseTopic == "" {
		m.BaseTopic = "legato/" + id
	}
	if m.DiscoveryPrefix == "" {
		m.DiscoveryPrefix = "homeassistant"
	}
	if m.DeviceName == "" {
		hostname, _ := os.Hostname()
		m.DeviceName = "Legato " + hostname
	}
	opts := service.MQTTOptions{InstanceID: id, BaseTopic: m.BaseTopic, DeviceName: m.DeviceName, DiscoveryPrefix: m.DiscoveryPrefix, SoftwareVersion: version.String(), IncludeReeve: m.IncludeReeve, AttentionDetails: m.AttentionDetails}
	var publisher *service.MQTTPublisher
	dial := func(ctx context.Context) (service.MQTTConnection, error) {
		username, password, err := mqttLogin(ctx, approvals.credentials, m.Credential, m.Broker)
		if err != nil {
			log.Print(err)
			return nil, err
		}
		return mqtt.Dial(ctx, mqtt.Options{Broker: m.Broker, ClientID: "legato-" + id, Username: username, Password: password, AvailabilityTopic: m.BaseTopic + "/availability", BirthTopic: m.DiscoveryPrefix + "/status", OnBirth: publisher.HomeAssistantOnline})
	}
	if agents == nil {
		publisher = service.NewMQTTPublisher(opts, mqttStoredAgents{db}, attention, approvals.approvals, bus, dial)
	} else {
		publisher = service.NewMQTTPublisher(opts, agents, attention, approvals.approvals, bus, dial)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); publisher.Run(ctx) }()
	var once sync.Once
	return func() { once.Do(func() { cancel(); <-done; release(); stopBridge() }) }
}

// Non-owners still forward local source changes to the lock owner. Incoming
// invalidations use a distinct event, so they cannot echo back through IPC.
func startMQTTInvalidations(bus *events.Bus) func() {
	changes, unsubscribe := bus.SubscribeInvalidations(events.EventAgentStarted, events.EventAgentActivityChanged, events.EventAgentCompleted, events.EventAgentDied, events.EventAttentionChanged, events.EventApprovalChanged)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			select {
			case <-ctx.Done():
				return
			case <-changes:
				broadcastCtx, stop := context.WithTimeout(ctx, time.Second)
				ipc.BroadcastContext(broadcastCtx, ipc.Message{Type: "mqtt_changed"})
				stop()
			}
		}
	}()
	var once sync.Once
	return func() { once.Do(func() { cancel(); <-done; unsubscribe() }) }
}

func mqttLogin(ctx context.Context, broker *service.CredentialBroker, name, resource string) (string, string, error) {
	if name == "" {
		return "", "", nil
	}
	if broker == nil {
		return "", "", errors.New("MQTT: credential broker unavailable; fix credential configuration")
	}
	for _, info := range broker.Credentials() {
		if info.Name == name && info.Profile != "mqtt" {
			return "", "", errors.New("MQTT: broker credential must use the host-only mqtt profile")
		}
	}
	secret, err := broker.UseForLegato(ctx, service.CredentialUse{Credential: name, Operation: "connect", Resource: resource, Call: "MQTT publisher"})
	if err != nil {
		return "", "", errors.New("MQTT: configured broker credential could not be read")
	}
	if secret.Type != credential.TypeUserPass {
		return "", "", errors.New("MQTT: broker credential must have type userpass")
	}
	var login struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.Unmarshal(secret.Value, &login); err != nil || login.Username == "" || login.Password == "" {
		return "", "", errors.New("MQTT: userpass must be JSON with nonempty username and password")
	}
	return login.Username, login.Password, nil
}

// No tmux is needed to read durable agent activity in a server without it.
type mqttStoredAgents struct{ db *store.Store }

func (s mqttStoredAgents) ListAgents(ctx context.Context) ([]service.AgentSession, error) {
	sessions, err := s.db.ListAgentSessions(ctx)
	if err != nil {
		return nil, fmt.Errorf("read MQTT agents: %w", err)
	}
	out := make([]service.AgentSession, 0, len(sessions))
	for _, session := range sessions {
		out = append(out, service.AgentSession{Status: session.Status, Activity: session.Activity, Reeve: service.IsReeveTask(ctx, s.db, session.TaskID)})
	}
	return out, nil
}
