package service

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"time"
	"unicode/utf8"

	"github.com/cpave3/legato/internal/engine/events"
	"github.com/cpave3/legato/internal/engine/mqtt"
	"github.com/cpave3/legato/themes"
)

type MQTTThemeConnection interface {
	MQTTConnection
	Commands() <-chan mqtt.Command
	PublishTransient(context.Context, string, []byte) error
}
type mqttThemePreference interface {
	Current(context.Context) (ThemePreference, error)
	Change(context.Context, uint64, string, string) (ThemePreference, error)
}

// MQTTThemeController grants only theme changes to the broker's exact-topic ACL
// writers. It owns a separate connection so aggregate-only ACLs stay unchanged.
type MQTTThemeController struct {
	opts       MQTTOptions
	preference mqttThemePreference
	bus        *events.Bus
	dial       func(context.Context) (MQTTThemeConnection, error)
	birth      chan struct{}
}

func NewMQTTThemeController(opts MQTTOptions, preference mqttThemePreference, bus *events.Bus, dial func(context.Context) (MQTTThemeConnection, error)) *MQTTThemeController {
	return &MQTTThemeController{opts: opts, preference: preference, bus: bus, dial: dial, birth: make(chan struct{}, 1)}
}
func (p *MQTTThemeController) HomeAssistantOnline() {
	select {
	case p.birth <- struct{}{}:
	default:
	}
}
func (p *MQTTThemeController) Run(ctx context.Context) {
	changes, unsubscribe := p.bus.SubscribeInvalidations(events.EventServerThemeChanged, events.EventServerThemeInvalidated)
	defer unsubscribe()
	backoff := time.Second
	for ctx.Err() == nil {
		connectCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		conn, err := p.dial(connectCtx)
		cancel()
		if err != nil {
			log.Print("MQTT theme: connection failed; check broker and credential configuration")
		} else {
			backoff = time.Second
			if err := p.connected(ctx, conn, changes); err != nil && ctx.Err() == nil {
				log.Print("MQTT theme: connection or preference unavailable")
			}
			shutdown, stop := context.WithTimeout(context.Background(), 2*time.Second)
			_ = conn.Publish(shutdown, p.opts.BaseTopic+"/theme/availability", []byte("offline"))
			_ = conn.Close(shutdown)
			stop()
		}
		if !mqttDelay(ctx, backoff) {
			return
		}
		backoff = min(backoff*2, 30*time.Second)
	}
}
func (p *MQTTThemeController) connected(ctx context.Context, conn MQTTThemeConnection, changes <-chan struct{}) error {
	nonce := make([]byte, 32)
	if _, err := rand.Read(nonce); err != nil {
		return err
	}
	session := hex.EncodeToString(nonce)
	publishState := func() error {
		current, err := p.preference.Current(ctx)
		if err != nil {
			return err
		}
		if !themes.ValidID(current.ID) {
			current.ID = "none"
		}
		data, err := json.Marshal(current)
		if err != nil {
			return err
		}
		return conn.Publish(ctx, p.opts.BaseTopic+"/theme/state", data)
	}
	syncAll := func() error {
		data, err := json.Marshal(p.discovery(session))
		if err != nil {
			return err
		}
		if err := conn.Publish(ctx, p.opts.DiscoveryPrefix+"/select/"+p.opts.InstanceID+"/theme/config", data); err != nil {
			return err
		}
		if err := publishState(); err != nil {
			return err
		}
		return conn.Publish(ctx, p.opts.BaseTopic+"/theme/availability", []byte("online"))
	}
	if err := syncAll(); err != nil {
		return err
	}
	tokens := float64(5)
	last := time.Now()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-conn.Done():
			return errors.New("connection ended")
		case <-p.birth:
			if err := syncAll(); err != nil {
				return err
			}
		case <-changes:
			if err := publishState(); err != nil {
				return err
			}
		case command := <-conn.Commands():
			now := time.Now()
			tokens = min(float64(5), tokens+now.Sub(last).Seconds()*5)
			last = now
			// Rate-limited traffic is silently dropped, including results, to bound output.
			if tokens < 1 {
				continue
			}
			tokens--
			code := p.apply(ctx, command, session, now)
			data, _ := json.Marshal(map[string]string{"code": code})
			if err := conn.PublishTransient(ctx, p.opts.BaseTopic+"/theme/result", data); err != nil {
				return err
			}
			// Re-read authoritative state on success and conflict, never optimistic state.
			if code == "ok" || code == "conflict" {
				if err := publishState(); err != nil {
					return err
				}
			}
		}
	}
}
func (p *MQTTThemeController) discovery(session string) map[string]any {
	topic := p.opts.BaseTopic + "/theme"
	return map[string]any{
		"name": "Server theme", "unique_id": p.opts.InstanceID + "_theme", "command_topic": topic + "/set", "state_topic": topic + "/state",
		"value_template": "{{ value_json.id }}", "json_attributes_topic": topic + "/state", "availability_topic": topic + "/availability",
		"optimistic": false, "qos": 0, "retain": false, "options": append([]string{"none"}, themes.IDs()...),
		"command_template": fmt.Sprintf(`{"id": {{ value | tojson }}, "session": "%s", "expected_revision": {{ this.attributes.revision | int }}, "issued_at": {{ as_timestamp(now()) | int }}}`, session),
		"device":           map[string]any{"identifiers": []string{p.opts.InstanceID}, "name": p.opts.DeviceName, "manufacturer": "Legato", "model": "Legato", "sw_version": p.opts.SoftwareVersion},
	}
}

type mqttThemeCommand struct {
	ID       string
	Session  string
	Revision uint64
	IssuedAt int64
}

func decodeMQTTThemeCommand(data []byte) (mqttThemeCommand, error) {
	var command mqttThemeCommand
	if len(data) > 2048 || !utf8.Valid(data) {
		return command, errors.New("invalid envelope")
	}
	d := json.NewDecoder(bytes.NewReader(data))
	token, err := d.Token()
	if err != nil || token != json.Delim('{') {
		return command, errors.New("invalid object")
	}
	seen := map[string]bool{}
	for d.More() {
		token, err := d.Token()
		if err != nil {
			return command, err
		}
		key, ok := token.(string)
		if !ok || seen[key] {
			return command, errors.New("duplicate field")
		}
		seen[key] = true
		// Decode pointers to reject JSON null as well as missing fields.
		switch key {
		case "id":
			var value *string
			err = d.Decode(&value)
			if value == nil {
				return command, errors.New("missing id")
			}
			command.ID = *value
		case "session":
			var value *string
			err = d.Decode(&value)
			if value == nil {
				return command, errors.New("missing session")
			}
			command.Session = *value
		case "expected_revision":
			var value *uint64
			err = d.Decode(&value)
			if value == nil {
				return command, errors.New("missing revision")
			}
			command.Revision = *value
		case "issued_at":
			var value *int64
			err = d.Decode(&value)
			if value == nil {
				return command, errors.New("missing timestamp")
			}
			command.IssuedAt = *value
		default:
			return command, errors.New("unknown field")
		}
		if err != nil {
			return command, err
		}
	}
	if _, err = d.Token(); err != nil {
		return command, err
	}
	if _, err = d.Token(); err != io.EOF {
		return command, errors.New("trailing JSON")
	}
	if len(seen) != 4 {
		return command, errors.New("missing field")
	}
	return command, nil
}
func (p *MQTTThemeController) apply(ctx context.Context, message mqtt.Command, session string, now time.Time) string {
	if message.Retained {
		return "retained"
	}
	command, err := decodeMQTTThemeCommand(message.Payload)
	if err != nil {
		return "invalid"
	}
	if command.Session != session {
		return "session"
	}
	// Compare seconds without subtracting attacker-controlled integers (overflow).
	if command.IssuedAt < now.Unix()-30 || command.IssuedAt > now.Unix()+5 {
		return "expired"
	}
	if time.Unix(command.IssuedAt, 0).Before(now.Add(-30 * time.Second)) {
		return "expired"
	}
	id := command.ID
	if id == "none" {
		id = ""
	} else if !themes.ValidID(id) {
		return "unknown_theme"
	}
	_, err = p.preference.Change(ctx, command.Revision, id, "mqtt:broker-grant")
	switch {
	case err == nil:
		return "ok"
	case errors.Is(err, ErrThemeConflict):
		return "conflict"
	case errors.Is(err, ErrUnknownTheme):
		return "unknown_theme"
	default:
		return "unavailable"
	}
}
