package main

import (
	"context"
	"github.com/cpave3/legato/config"
	"github.com/cpave3/legato/internal/engine/events"
	"github.com/cpave3/legato/internal/engine/ipc"
	"strings"
	"testing"
)

func TestMQTTOwnershipExcludesSecondPublisher(t *testing.T) {
	dir := t.TempDir()
	release, err := claimMQTTPublisher(dir)
	if err != nil {
		t.Fatal(err)
	}
	if release2, err := claimMQTTPublisher(dir); err == nil {
		release2()
		t.Fatal("second publisher acquired ownership")
	}
	release()
	release3, err := claimMQTTPublisher(dir)
	if err != nil {
		t.Fatal(err)
	}
	release3()
}

func TestMQTTIPCInvalidatesBothRuntimes(t *testing.T) {
	for _, serve := range []bool{false, true} {
		bus := events.New()
		changes, cancel := bus.SubscribeInvalidations(events.EventMQTTInvalidated)
		if serve {
			handleServeIPCMessage(ipc.Message{Type: "mqtt_changed"}, bus, &recordingServeNotifier{})
		} else {
			handleIPCMessage(ipc.Message{Type: "mqtt_changed"}, bus, nil)
		}
		select {
		case <-changes:
		default:
			t.Errorf("serve=%v: MQTT invalidation lost", serve)
		}
		cancel()
	}
}

func TestMQTTLoginRequiresHostOnlyProfile(t *testing.T) {
	t.Setenv("MQTT_PROFILE_TEST_LOGIN", `{"username":"mqtt-user","password":"private-marker"}`)
	db := channelsTestDB(t)
	broker := channelsTestBroker(t, db, config.CredentialConfig{Name: "not-host-only", Profile: "secret", Type: "userpass", Secret: "env://MQTT_PROFILE_TEST_LOGIN"})
	_, _, err := mqttLogin(context.Background(), broker, "not-host-only", "mqtt://127.0.0.1:1883")
	if err == nil {
		t.Fatal("MQTT accepted a login available for agent grants")
	}
	if strings.Contains(err.Error(), "private-marker") {
		t.Fatal("login leaked")
	}
}
