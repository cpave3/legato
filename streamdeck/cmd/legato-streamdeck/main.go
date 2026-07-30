// Command legato-streamdeck is an OpenDeck plugin that shows Legato agent
// and attention status on Stream Deck keys.
//
// OpenDeck launches this process with -port, -pluginUUID, and -registerEvent
// arguments. The plugin connects to OpenDeck over WebSocket, receives key
// lifecycle events, and renders live Legato state (agents, attention items)
// as SVG tile images on the physical keys.
package main

import (
	"context"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"github.com/cpave3/streamdeck-plugin/internal/legato"
	"github.com/cpave3/streamdeck-plugin/internal/opendeck"
	"github.com/cpave3/streamdeck-plugin/internal/render"
)

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
}

func main() {
	args, err := opendeck.ParseArgs(os.Args[1:])
	if err != nil {
		log.Fatalf("[legato-streamdeck] %v", err)
	}

	cfg := loadConfig()
	log.Printf("[legato-streamdeck] connecting to Legato at %s", cfg.BaseURL)

	st := &pluginState{baseURL: cfg.BaseURL}

	// Legato state callback — called whenever Legato state changes.
	onState := func(snap legato.Snapshot) {
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
		switch e.Event {
		case "willAppear":
			st.mu.Lock()
			switch e.Action {
			case opendeck.ActionAgentTile:
				st.agentCtxs = append(st.agentCtxs, e.Context)
			case opendeck.ActionAttnTile:
				st.attnCtxs = append(st.attnCtxs, e.Context)
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
			st.mu.Unlock()

		case "keyUp":
			st.handleKeyPress(ctx, e)
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

	// No token — show no-token state.
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
			img := render.DataURI(render.Agent(a.Title, a.Activity, a.Role))
			_ = deck.SetImage(ctx, c, img)
		} else {
			_ = deck.SetImage(ctx, c, render.DataURI(render.Empty()))
		}
	}

	// Attention tiles.
	for i, c := range attnCtxs {
		if i >= opendeck.MaxTiles {
			break
		}
		if i < len(snap.Attention) {
			item := snap.Attention[i]
			img := render.DataURI(render.Attention(item.Producer, item.Title, item.Urgency, i, len(snap.Attention)))
			_ = deck.SetImage(ctx, c, img)
		} else {
			_ = deck.SetImage(ctx, c, render.DataURI(render.Empty()))
		}
	}
}

// handleKeyPress routes a keyUp event to the appropriate action.
func (st *pluginState) handleKeyPress(ctx context.Context, e opendeck.Event) {
	st.mu.Lock()
	snap := st.snap
	baseURL := st.baseURL
	st.mu.Unlock()

	switch e.Action {
	case opendeck.ActionAgentTile:
		// Find the slot index for this context.
		idx := -1
		st.mu.Lock()
		for i, c := range st.agentCtxs {
			if c == e.Context {
				idx = i
				break
			}
		}
		st.mu.Unlock()
		if idx < 0 || idx >= len(snap.Agents) {
			return
		}
		agent := snap.Agents[idx]
		url := baseURL + "/agents?task=" + agent.TaskID
		_ = openBrowser(url)

	case opendeck.ActionAttnTile:
		idx := -1
		st.mu.Lock()
		for i, c := range st.attnCtxs {
			if c == e.Context {
				idx = i
				break
			}
		}
		st.mu.Unlock()
		if idx < 0 || idx >= len(snap.Attention) {
			return
		}
		item := snap.Attention[idx]

		// Check for a safe direct-execute action (non-confirm, execute kind).
		var safeAction *legato.AttentionAction
		for i := range item.Actions {
			a := &item.Actions[i]
			if a.Kind == "execute" && !a.RequiresConfirm && a.Intent == "primary" {
				safeAction = a
				break
			}
		}

		if safeAction != nil {
			// Execute directly via API.
			if err := st.legato.ExecuteAction(ctx, item.ID, safeAction.ID, item.SourceRevision); err != nil {
				log.Printf("[legato-streamdeck] execute action %s: %v", safeAction.ID, err)
				// Fall back to opening the browser.
				url := legato.AttentionDeepLink(baseURL, item.Navigation)
				_ = openBrowser(url)
			}
		} else {
			// Open in browser for confirmation or navigation.
			url := legato.AttentionDeepLink(baseURL, item.Navigation)
			_ = openBrowser(url)
		}
	}
}

// openBrowser opens a URL in the default browser via xdg-open.
func openBrowser(url string) error {
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
