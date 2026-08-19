// Command legato-streamdeck is an OpenDeck plugin that shows Legato agent
// and attention status on Stream Deck keys.
//
// OpenDeck launches this process with -port, -pluginUUID, and -registerEvent
// arguments. The plugin connects to OpenDeck over WebSocket, receives key
// lifecycle events, and renders live Legato state as visual-only tile images
// (color and shape, no text — designed for 72px keys).
//
// Interaction: short press opens the agent/item in the browser. Long press
// (hold >800ms) on an attention tile executes the primary action directly
// and flashes a green checkmark on the key. Long press on an agent tile
// toggles notify on/off.
package main

import (
	"context"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/cpave3/streamdeck-plugin/internal/legato"
	"github.com/cpave3/streamdeck-plugin/internal/opendeck"
	"github.com/cpave3/streamdeck-plugin/internal/render"
)

// holdThreshold is how long a key must be held to trigger the action.
const holdThreshold = 800 * time.Millisecond

// pluginState holds all mutable state shared between the OpenDeck event
// handler and the Legato state callback. All access is guarded by mu.
type pluginState struct {
	mu        sync.Mutex
	snap      legato.Snapshot
	deck      *opendeck.Client
	agentCtxs []string
	attnCtxs  []string
	baseURL   string
	legato    *legato.Client
	// keyDownTime tracks when each context was pressed (for hold detection)
	keyDownTime map[string]time.Time
}

func main() {
	args, err := opendeck.ParseArgs(os.Args[1:])
	if err != nil {
		log.Fatalf("[legato-streamdeck] %v", err)
	}

	cfg := loadConfig()
	log.Printf("[legato-streamdeck] connecting to Legato at %s", cfg.BaseURL)

	st := &pluginState{
		baseURL:     cfg.BaseURL,
		keyDownTime: make(map[string]time.Time),
	}

	// Legato state callback — called whenever Legato state changes.
	onState := func(snap legato.Snapshot) {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("[legato-streamdeck] panic in onState: %v", r)
			}
		}()
		st.mu.Lock()
		st.snap = snap
		deck := st.deck
		agentCtxs := st.agentCtxs
		attnCtxs := st.attnCtxs
		st.mu.Unlock()
		if deck != nil {
			pushTiles(context.Background(), deck, snap, agentCtxs, attnCtxs)
		}
	}

	st.legato = legato.NewClient(cfg, onState)

	// OpenDeck event handler.
	handler := func(ctx context.Context, e opendeck.Event) {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("[legato-streamdeck] panic in handler (%s): %v", e.Event, r)
			}
		}()
		switch e.Event {
		case "willAppear":
			st.mu.Lock()
			switch e.Action {
			case opendeck.ActionAgentTile:
				if !contains(st.agentCtxs, e.Context) {
					st.agentCtxs = append(st.agentCtxs, e.Context)
				}
			case opendeck.ActionAttnTile:
				if !contains(st.attnCtxs, e.Context) {
					st.attnCtxs = append(st.attnCtxs, e.Context)
				}
			}
			deck := st.deck
			snap := st.snap
			agentCtxs := st.agentCtxs
			attnCtxs := st.attnCtxs
			st.mu.Unlock()
			if deck != nil {
				pushTiles(ctx, deck, snap, agentCtxs, attnCtxs)
			}

		case "willDisappear":
			st.mu.Lock()
			switch e.Action {
			case opendeck.ActionAgentTile:
				st.agentCtxs = removeContext(st.agentCtxs, e.Context)
			case opendeck.ActionAttnTile:
				st.attnCtxs = removeContext(st.attnCtxs, e.Context)
			}
			delete(st.keyDownTime, e.Context)
			st.mu.Unlock()

		case "keyDown":
			st.mu.Lock()
			st.keyDownTime[e.Context] = time.Now()
			st.mu.Unlock()

		case "keyUp":
			st.mu.Lock()
			downAt, hasDown := st.keyDownTime[e.Context]
			delete(st.keyDownTime, e.Context)
			snap := st.snap
			st.mu.Unlock()

			if !hasDown {
				return
			}
			holdDuration := time.Since(downAt)
			st.handleKeyAction(ctx, e, snap, holdDuration)
		}
	}

	st.deck = opendeck.New(args, handler)

	// Start the Legato client in the background.
	st.legato.Start(context.Background())
	defer st.legato.Stop()

	// Block on the OpenDeck connection. If it drops, exit so OpenDeck
	// relaunches the plugin.
	if err := st.deck.Run(context.Background()); err != nil {
		log.Fatalf("[legato-streamdeck] opendeck error: %v", err)
	}
}

// handleKeyAction routes a key press based on hold duration.
// Short press (<800ms): open in browser.
// Long press (>=800ms): execute primary action if safe, else open browser.
func (st *pluginState) handleKeyAction(ctx context.Context, e opendeck.Event, snap legato.Snapshot, holdDuration time.Duration) {
	isHold := holdDuration >= holdThreshold

	switch e.Action {
	case opendeck.ActionAgentTile:
		idx := st.contextIndex(st.agentCtxs, e.Context)
		if idx < 0 || idx >= len(snap.Agents) {
			return
		}
		agent := snap.Agents[idx]
		if isHold {
			// Long press: toggle notify for this agent
			newNotify := !agent.NotifyEnabled
			err := st.legato.ToggleNotify(ctx, agent.TaskID, newNotify)
			if err != nil {
				log.Printf("[legato-streamdeck] toggle notify for %s: %v", agent.TaskID, err)
				_ = st.deck.ShowAlert(ctx, e.Context)
			} else {
				log.Printf("[legato-streamdeck] notify %s → %v", agent.TaskID, newNotify)
				_ = st.deck.ShowOK(ctx, e.Context)
				// Update local state so the dot appears immediately
				st.mu.Lock()
				if idx < len(st.snap.Agents) {
					st.snap.Agents[idx].NotifyEnabled = newNotify
				}
				st.mu.Unlock()
				// Re-render tiles
				st.mu.Lock()
				snap := st.snap
				deck := st.deck
				agentCtxs := st.agentCtxs
				attnCtxs := st.attnCtxs
				st.mu.Unlock()
				if deck != nil {
					pushTiles(ctx, deck, snap, agentCtxs, attnCtxs)
				}
			}
		} else {
			// Short press: open in browser
			_ = openStreamDeckBrowser(st.baseURL + "/agents?task=" + agent.TaskID)
		}

	case opendeck.ActionAttnTile:
		idx := st.contextIndex(st.attnCtxs, e.Context)
		if idx < 0 || idx >= len(snap.Attention) {
			return
		}
		item := snap.Attention[idx]

		if isHold {
			// Long press: try to execute the primary action
			var safeAction *legato.AttentionAction
			for i := range item.Actions {
				a := &item.Actions[i]
				if a.Kind == "execute" && !a.RequiresConfirm {
					safeAction = a
					break
				}
			}

			if safeAction != nil {
				err := st.legato.ExecuteAction(ctx, item.ID, safeAction.ID, item.SourceRevision)
				if err != nil {
					log.Printf("[legato-streamdeck] execute action %s: %v", safeAction.ID, err)
					_ = st.deck.ShowAlert(ctx, e.Context)
					// Fall back to browser
					_ = openStreamDeckBrowser(legato.AttentionDeepLink(st.baseURL, item.Navigation))
				} else {
					log.Printf("[legato-streamdeck] executed action %s on %s", safeAction.ID, item.ID)
					_ = st.deck.ShowOK(ctx, e.Context)
				}
			} else {
				// No safe action — open browser even on hold
				_ = openStreamDeckBrowser(legato.AttentionDeepLink(st.baseURL, item.Navigation))
			}
		} else {
			// Short press: open browser
			_ = openStreamDeckBrowser(legato.AttentionDeepLink(st.baseURL, item.Navigation))
		}
	}
}

func (st *pluginState) contextIndex(list []string, ctx string) int {
	st.mu.Lock()
	defer st.mu.Unlock()
	for i, c := range list {
		if c == ctx {
			return i
		}
	}
	return -1
}

// pushTiles sends the current Legato state to all active tile contexts.
func pushTiles(ctx context.Context, deck *opendeck.Client, snap legato.Snapshot, agentCtxs, attnCtxs []string) {
	// Disconnected — all tiles show offline.
	if !snap.Connected {
		img := render.DataURI(render.Disconnected())
		for _, c := range agentCtxs {
			_ = deck.SetImage(ctx, c, img)
		}
		for _, c := range attnCtxs {
			_ = deck.SetImage(ctx, c, img)
		}
		return
	}

	// No token — show lock icon.
	if !snap.HasToken {
		img := render.DataURI(render.NoToken())
		for _, c := range agentCtxs {
			_ = deck.SetImage(ctx, c, img)
		}
		for _, c := range attnCtxs {
			_ = deck.SetImage(ctx, c, img)
		}
		return
	}

	// Agent tiles.
	for i, c := range agentCtxs {
		if i >= opendeck.MaxTiles {
			break
		}
		if i < len(snap.Agents) {
			a := snap.Agents[i]
			var img string
			if a.Status == "dead" {
				img = render.DataURI(render.AgentDead(a.TaskID))
			} else {
				img = render.DataURI(render.Agent(a.Activity, a.TaskID, a.WorkingSeconds, a.WaitingSeconds, a.ContextPressure, a.NotifyEnabled))
			}
			_ = deck.SetImage(ctx, c, img)
		} else {
			_ = deck.SetImage(ctx, c, render.DataURI(render.EmptyAgent()))
		}
	}

	// Attention tiles.
	for i, c := range attnCtxs {
		if i >= opendeck.MaxTiles {
			break
		}
		if i < len(snap.Attention) {
			item := snap.Attention[i]
			img := render.DataURI(render.Attention(item.Producer, item.Urgency, item.Navigation.Target, i, len(snap.Attention)))
			_ = deck.SetImage(ctx, c, img)
		} else {
			_ = deck.SetImage(ctx, c, render.DataURI(render.EmptyAttention()))
		}
	}
}

func openStreamDeckBrowser(destination string) error {
	marked, err := legato.StreamDeckDeepLink(destination)
	if err != nil {
		return err
	}
	return openBrowser(marked)
}

// openBrowser opens a URL in the default browser. Inside the Flatpak sandbox,
// it uses flatpak-spawn --host to invoke xdg-open on the host.
func openBrowser(url string) error {
	if _, err := exec.LookPath("flatpak-spawn"); err == nil {
		return exec.Command("flatpak-spawn", "--host", "xdg-open", url).Start()
	}
	return exec.Command("xdg-open", url).Start()
}

func removeContext(list []string, ctx string) []string {
	for i, c := range list {
		if c == ctx {
			return append(list[:i], list[i+1:]...)
		}
	}
	return list
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// --- Config ---

// loadConfig resolves the plugin configuration. It searches for a config file
// in several locations, since OpenDeck runs plugins inside a Flatpak sandbox
// where the host home directory is not directly accessible:
//
//  1. Next to the plugin binary (the .sdPlugin directory — always readable)
//  2. The current working directory
//  3. ~/.config/legato/streamdeck.yaml (works outside Flatpak)
//
// Environment variables LEGATO_URL and LEGATO_TOKEN override the file.
func loadConfig() legato.Config {
	cfg := legato.Config{
		BaseURL: "http://localhost:3080",
	}

	// Try config files in priority order.
	var configPaths []string
	if exe, err := os.Executable(); err == nil {
		configPaths = append(configPaths, filepath.Join(filepath.Dir(exe), "streamdeck.yaml"))
	}
	if wd, err := os.Getwd(); err == nil {
		configPaths = append(configPaths, filepath.Join(wd, "streamdeck.yaml"))
	}
	if home, err := os.UserHomeDir(); err == nil {
		configPaths = append(configPaths, filepath.Join(home, ".config", "legato", "streamdeck.yaml"))
	}

	for _, p := range configPaths {
		data, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		log.Printf("[legato-streamdeck] config loaded from %s", p)
		parseConfig(data, &cfg)
		break
	}

	// Environment variable overrides.
	if envURL := os.Getenv("LEGATO_URL"); envURL != "" {
		cfg.BaseURL = envURL
	}
	if envToken := os.Getenv("LEGATO_TOKEN"); envToken != "" {
		cfg.Token = envToken
	}

	if cfg.Token != "" {
		log.Printf("[legato-streamdeck] using explicit token from config/env")
	}
	return cfg
}

func parseConfig(data []byte, cfg *legato.Config) {
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "legato_url:") {
			cfg.BaseURL = strings.Trim(strings.TrimSpace(strings.TrimPrefix(line, "legato_url:")), `"`)
		}
		if strings.HasPrefix(line, "token:") {
			cfg.Token = strings.Trim(strings.TrimSpace(strings.TrimPrefix(line, "token:")), `"`)
		}
	}
}
