package config

import (
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
)

// MQTTConfig controls the optional Home Assistant integrations.
type MQTTConfig struct {
	ThemeControl    bool   `yaml:"theme_control"`
	Enabled         bool   `yaml:"enabled"`
	Broker          string `yaml:"broker"`
	Credential      string `yaml:"credential"`
	DiscoveryPrefix string `yaml:"discovery_prefix"`
	// Empty BaseTopic and DeviceName are resolved using runtime identity.
	BaseTopic        string `yaml:"base_topic"`
	DeviceName       string `yaml:"device_name"`
	IncludeReeve     bool   `yaml:"include_reeve"`
	AttentionDetails bool   `yaml:"attention_details"`
}

func normalizeMQTT(m *MQTTConfig) error {
	if m.DiscoveryPrefix == "" {
		m.DiscoveryPrefix = "homeassistant"
	}
	for key, topic := range map[string]string{"discovery_prefix": m.DiscoveryPrefix, "base_topic": m.BaseTopic} {
		if topic == "" {
			continue
		}
		// MQTT topics are at most 65535 bytes, including appended suffixes.
		// Reserve /agent_waiting_on_you/state (also enough for attributes).
		limit := 65535 - len("/agent_waiting_on_you/state")
		if key == "discovery_prefix" {
			// Reserve the longest discovery suffix with a 36-byte UUID node ID
			// (the current srv- instance IDs are shorter): 79 bytes in total.
			limit = 65535 - len("/binary_sensor/") - 36 - len("/agent_waiting_on_you/config")
		}
		if len(topic) > limit {
			return fmt.Errorf("integrations.mqtt.%s: topic root must be at most %d bytes to leave room for published topic suffixes", key, limit)
		}
		if strings.ContainsAny(topic, "+#\x00") || strings.HasPrefix(topic, "/") || strings.HasSuffix(topic, "/") || strings.Contains(topic, "//") {
			return fmt.Errorf("integrations.mqtt.%s: topic roots need nonempty segments without wildcards, NUL or trailing slash", key)
		}
	}
	if m.Enabled || m.ThemeControl || m.Broker != "" {
		if err := validateMQTTBroker(m.Broker); err != nil {
			return err
		}
	}
	return nil
}

func validateMQTTBroker(broker string) error {
	bad := fmt.Errorf("integrations.mqtt.broker: want mqtt:// or mqtts:// with a host and optional port (1–65535), no credentials, path, query or fragment")
	u, err := url.Parse(broker)
	if err != nil || (u.Scheme != "mqtt" && u.Scheme != "mqtts") || u.Hostname() == "" || u.User != nil || u.Path != "" || u.Opaque != "" || strings.ContainsAny(broker, "?#") {
		return bad
	}
	if strings.Contains(u.Host, ":") {
		if strings.HasPrefix(u.Host, "[") && strings.HasSuffix(u.Host, "]") {
			if net.ParseIP(u.Hostname()) == nil {
				return bad
			}
		} else {
			if _, _, err := net.SplitHostPort(u.Host); err != nil {
				return bad
			}
			port, err := strconv.Atoi(u.Port())
			if err != nil || port < 1 || port > 65535 {
				return bad
			}
		}
	}
	return nil
}
