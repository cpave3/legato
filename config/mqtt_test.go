package config

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func TestMQTTLoadsOptInAndAnonymousDefaults(t *testing.T) {
	for _, body := range []string{"", "integrations:\n  mqtt:\n    enabled: false\n", "integrations:\n  mqtt:\n    enabled: true\n    broker: mqtt://broker.lan:1883\n"} {
		writeTestConfig(t, body)
		cfg, err := Load()
		if err != nil {
			t.Fatal(err)
		}
		mqtt := cfg.Integrations.MQTT
		if mqtt.DiscoveryPrefix != "homeassistant" || mqtt.BaseTopic != "" || mqtt.DeviceName != "" || mqtt.Credential != "" || mqtt.IncludeReeve || mqtt.AttentionDetails {
			t.Fatalf("defaults = %+v", mqtt)
		}
		if mqtt.Enabled != (body == "integrations:\n  mqtt:\n    enabled: true\n    broker: mqtt://broker.lan:1883\n") {
			t.Fatalf("enabled = %v", mqtt.Enabled)
		}
	}
}

func TestMQTTBrokerAcceptsOnlyTCPBrokerURLs(t *testing.T) {
	for _, broker := range []string{"mqtt://broker.lan", "mqtts://broker.lan:8883", "mqtt://127.0.0.1:1883", "mqtts://[::1]:8883"} {
		t.Run(broker, func(t *testing.T) {
			writeTestConfig(t, fmt.Sprintf("integrations:\n  mqtt:\n    enabled: true\n    broker: %q\n", broker))
			cfg, err := Load()
			if err != nil || cfg.Integrations.MQTT.Broker != broker {
				t.Fatalf("broker = %v, %v", cfg, err)
			}
		})
	}
	for _, broker := range []string{"", "broker.lan:1883", "http://broker.lan", "ws://broker.lan", "mqtt:///", "mqtt://:1883", "mqtt://broker.lan/", "mqtt://broker.lan/path", "mqtt://broker.lan?", "mqtt://broker.lan?q=x", "mqtt://broker.lan#", "mqtt://broker.lan#frag", "mqtt://user:secret-marker@broker.lan", "mqtt://broker.lan:0", "mqtt://broker.lan:65536", "mqtt://broker.lan:-1", "mqtt://broker.lan:no", "mqtt://broker.lan:", "mqtt://::1"} {
		t.Run(broker, func(t *testing.T) {
			writeTestConfig(t, fmt.Sprintf("integrations:\n  mqtt:\n    enabled: true\n    broker: %q\n", broker))
			_, err := Load()
			if err == nil || !strings.Contains(err.Error(), "integrations.mqtt.broker") {
				t.Fatalf("error = %v", err)
			}
			if strings.Contains(err.Error(), "secret-marker") {
				t.Fatal("broker credential leaked in error")
			}
		})
	}
}

func TestMQTTTopicRootsRejectAmbiguousPublishTopics(t *testing.T) {
	for _, key := range []string{"discovery_prefix", "base_topic"} {
		for _, topic := range []string{"/root", "root/", "root//child", "root/+", "root/#", "root\x00child"} {
			t.Run(key+topic, func(t *testing.T) {
				writeTestConfig(t, fmt.Sprintf("integrations:\n  mqtt:\n    %s: %q\n", key, topic))
				if _, err := Load(); err == nil || !strings.Contains(err.Error(), "integrations.mqtt."+key) {
					t.Fatalf("error = %v", err)
				}
			})
		}
	}
	writeTestConfig(t, "integrations:\n  mqtt:\n    enabled: true\n    broker: mqtts://broker:8883\n    credential: missing-at-load-time\n    discovery_prefix: ha/discovery\n    base_topic: legato/instance\n    device_name: My Legato\n    include_reeve: true\n    attention_details: true\n")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	m := cfg.Integrations.MQTT
	if m.Credential != "missing-at-load-time" || m.DiscoveryPrefix != "ha/discovery" || m.BaseTopic != "legato/instance" || m.DeviceName != "My Legato" || !m.IncludeReeve || !m.AttentionDetails {
		t.Fatalf("MQTT = %+v", m)
	}
}

func TestMQTTMissingConfigKeepsDiscoveryDefault(t *testing.T) {
	t.Setenv("LEGATO_CONFIG", t.TempDir()+"/missing.yaml")
	cfg, err := Load()
	if err != nil || cfg.Integrations.MQTT.DiscoveryPrefix != "homeassistant" {
		t.Fatalf("config = %+v, %v", cfg, err)
	}
}

func TestMQTTSettingsOverlayHonorsPinsAndRestartRisks(t *testing.T) {
	writeTestConfig(t, "integrations:\n  mqtt:\n    include_reeve: false\n")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	values := map[string]json.RawMessage{
		"enabled": json.RawMessage(`true`), "broker": json.RawMessage(`"mqtts://broker:8883"`),
		"credential": json.RawMessage(`"mqtt-login"`), "discovery_prefix": json.RawMessage(`"ha"`),
		"base_topic": json.RawMessage(`"legato/test"`), "device_name": json.RawMessage(`"Test"`),
		"include_reeve": json.RawMessage(`true`), "attention_details": json.RawMessage(`true`),
	}
	overlay := map[string]json.RawMessage{}
	for name, raw := range values {
		key := "integrations.mqtt." + name
		setting, ok := LookupSetting(key)
		if !ok {
			t.Fatalf("setting %s not registered", key)
		}
		if setting.Mode != ModeRestart {
			t.Errorf("%s mode = %s", key, setting.Mode)
		}
		wantClass := ClassRisky
		if name == "device_name" || name == "include_reeve" {
			wantClass = ClassChange
		}
		if setting.Class != wantClass {
			t.Errorf("%s class = %s", key, setting.Class)
		}
		if _, err := DecodeSetting(setting, raw); err != nil {
			t.Fatal(err)
		}
		overlay[key] = raw
	}
	if err := Overlay(cfg, overlay); err != nil {
		t.Fatal(err)
	}
	m := cfg.Integrations.MQTT
	if !m.Enabled || m.Broker != "mqtts://broker:8883" || m.Credential != "mqtt-login" || m.DiscoveryPrefix != "ha" || m.BaseTopic != "legato/test" || m.DeviceName != "Test" || m.IncludeReeve || !m.AttentionDetails {
		t.Fatalf("MQTT = %+v", m)
	}
	if !cfg.Pinned("integrations.mqtt.include_reeve") || cfg.Pinned("integrations.mqtt.enabled") {
		t.Fatal("MQTT pins lost")
	}
	if err := Overlay(cfg, map[string]json.RawMessage{"integrations.mqtt.broker": json.RawMessage(`"mqtt://user:secret@broker"`)}); err == nil || !strings.Contains(err.Error(), "integrations.mqtt.broker") {
		t.Fatalf("invalid broker overlay = %v", err)
	}
}
