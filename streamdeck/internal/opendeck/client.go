// Package opendeck implements the client side of the OpenAction WebSocket
// protocol used by OpenDeck to communicate with Stream Deck plugins.
//
// OpenDeck launches the plugin process with three arguments:
//
//	-port <port>          — the WebSocket port to connect to
//	-pluginUUID <uuid>    — the plugin's UUID from manifest.json
//	-registerEvent <name> — the event name to send upon connection
//
// The plugin opens a WebSocket to ws://127.0.0.1:<port>, sends the register
// event, then enters a message loop receiving key lifecycle events and sending
// commands (setImage, switchProfile, etc.) back to OpenDeck.
package opendeck

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"

	"nhooyr.io/websocket"
)

// MaxTiles is the number of pre-allocated tile slots per profile.
const MaxTiles = 15

// Action UUIDs — must match plugin/manifest.json.
const (
	ActionHome      = "com.cpave3.legato.home"
	ActionAgentTile = "com.cpave3.legato.agent-tile"
	ActionAttnTile  = "com.cpave3.legato.attention-tile"
)

// Event is an inbound message from OpenDeck.
type Event struct {
	Action  string          `json:"action"`
	Event   string          `json:"event"`
	Context string          `json:"context"`
	Device  string          `json:"device,omitempty"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

// SetImagePayload is the payload for the setImage command.
type SetImagePayload struct {
	Image  string `json:"image"`
	Target int    `json:"target"`
}

// EventHandler is called for each inbound event. It receives the parsed event
// and can use the Client to send commands back.
type EventHandler func(ctx context.Context, e Event)

// Client is the OpenAction WebSocket client.
type Client struct {
	conn       *websocket.Conn
	port       string
	pluginUUID string
	registerEv string
	handler    EventHandler
}

// Args holds the parsed startup arguments.
type Args struct {
	Port       string
	PluginUUID string
	RegisterEv string
}

// ParseArgs extracts -port, -pluginUUID, and -registerEvent from the
// command-line arguments OpenDeck passes to the plugin process.
func ParseArgs(argv []string) (Args, error) {
	var a Args
	for i := 0; i < len(argv); i++ {
		switch argv[i] {
		case "-port":
			if i+1 < len(argv) {
				a.Port = argv[i+1]
				i++
			}
		case "-pluginUUID":
			if i+1 < len(argv) {
				a.PluginUUID = argv[i+1]
				i++
			}
		case "-registerEvent":
			if i+1 < len(argv) {
				a.RegisterEv = argv[i+1]
				i++
			}
		}
	}
	if a.Port == "" || a.PluginUUID == "" || a.RegisterEv == "" {
		return a, fmt.Errorf("missing OpenDeck startup arguments (need -port, -pluginUUID, -registerEvent)")
	}
	return a, nil
}

// New creates a Client that will connect to OpenDeck and dispatch events to h.
func New(a Args, h EventHandler) *Client {
	return &Client{
		port:       a.Port,
		pluginUUID: a.PluginUUID,
		registerEv: a.RegisterEv,
		handler:    h,
	}
}

// Run connects to OpenDeck, registers, and blocks on the message loop.
// If the WebSocket closes, the process exits so OpenDeck relaunches it.
func (c *Client) Run(ctx context.Context) error {
	url := fmt.Sprintf("ws://127.0.0.1:%s", c.port)
	conn, _, err := websocket.Dial(ctx, url, nil)
	if err != nil {
		return fmt.Errorf("dial opendeck: %w", err)
	}
	c.conn = conn
	defer conn.Close(websocket.StatusNormalClosure, "")

	if err := c.register(ctx); err != nil {
		return fmt.Errorf("register: %w", err)
	}

	return c.recvLoop(ctx)
}

func (c *Client) register(ctx context.Context) error {
	msg := map[string]string{
		"event": c.registerEv,
		"uuid":  c.pluginUUID,
	}
	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	return c.conn.Write(ctx, websocket.MessageText, data)
}

func (c *Client) recvLoop(ctx context.Context) error {
	for {
		_, data, err := c.conn.Read(ctx)
		if err != nil {
			log.Printf("opendeck: connection closed: %v", err)
			os.Exit(1)
		}
		var e Event
		if err := json.Unmarshal(data, &e); err != nil {
			log.Printf("opendeck: invalid message: %v", err)
			continue
		}
		if c.handler != nil {
			c.handler(ctx, e)
		}
	}
}

// Send sends a raw JSON message to OpenDeck.
func (c *Client) Send(ctx context.Context, msg map[string]any) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	return c.conn.Write(ctx, websocket.MessageText, data)
}

// SetImage sends an setImage command for the given key context.
func (c *Client) SetImage(ctx context.Context, contextID, image string) error {
	return c.Send(ctx, map[string]any{
		"event":   "setImage",
		"context": contextID,
		"payload": SetImagePayload{Image: image, Target: 0},
	})
}

// SwitchProfile sends a switchProfile command (OpenDeck-specific, non-spec).
func (c *Client) SwitchProfile(ctx context.Context, device, profile string) error {
	return c.Send(ctx, map[string]any{
		"event":   "switchProfile",
		"device":  device,
		"profile": profile,
	})
}
