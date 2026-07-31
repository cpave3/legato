# Legato Stream Deck Plugin

A [Stream Deck](https://www.elgato.com/en/stream-deck) plugin for [OpenDeck](https://github.com/nekename/OpenDeck) (Linux) that shows live Legato agent and attention status on physical keys. Press a key to open the agent or attention item in your browser.

## What it does

- **Agent tiles** — One key per active agent. Shows the agent's task title and activity state (green=working, amber=waiting, gray=idle). Press to open that agent in the Legato web UI.
- **Attention tiles** — One key per attention item (plans awaiting approval, reviews ready, workflows needing input). Shows the producer type and title. Press to open the item in the browser, or execute safe actions directly.

## Prerequisites

- [OpenDeck](https://github.com/nekename/OpenDeck) installed and running
- A Legato server running (`legato serve` or auto-started with the TUI)
- Go 1.25+ (to build; not needed at runtime — the plugin is a static binary)

## Build

From the legato repo root:

```bash
task streamdeck:build       # build the binary
task streamdeck:package      # build + assemble the .sdPlugin folder
```

This produces `streamdeck/dist/com.cpave3.legato.sdPlugin/` with the manifest, icons, and compiled binary.

## Install

1. Copy the packaged folder into OpenDeck's plugin directory:

   ```bash
   cp -r streamdeck/dist/com.cpave3.legato.sdPlugin ~/.local/share/opendeck/plugins/
   ```

2. Restart OpenDeck (or reload plugins from Settings → Plugins).

## Configure

The plugin needs the Legato server URL and auth token. It searches for a `streamdeck.yaml` config file in three locations (first match wins):

1. **Next to the plugin binary** — inside the `.sdPlugin` folder (e.g. `~/.var/app/me.amankhanna.opendeck/config/opendeck/plugins/com.cpave3.legato.sdPlugin/streamdeck.yaml`)
2. **Current working directory** — wherever the plugin process starts
3. **`~/.config/legato/streamdeck.yaml`** — the standard Legato config location

```yaml
legato_url: "http://localhost:3080"
token: "your-auth-token"
```

Environment variables `LEGATO_URL` and `LEGATO_TOKEN` override the config file.

### Flatpak note (important)

OpenDeck is distributed as a Flatpak. Plugins run inside the sandbox, which means **the plugin cannot read `~/.local/share/legato/auth-token` from the host filesystem**. You must provide the token explicitly via a `streamdeck.yaml` file placed inside the `.sdPlugin` folder:

```bash
# Get your Legato auth token
TOKEN=$(cat ~/.local/share/legato/auth-token)

# Place it in the plugin directory
cat > ~/.var/app/me.amankhanna.opendeck/config/opendeck/plugins/com.cpave3.legato.sdPlugin/streamdeck.yaml << EOF
legato_url: "http://localhost:3080"
token: "$TOKEN"
EOF
```

The `package.sh` script automatically copies `plugin/streamdeck.yaml` into the `.sdPlugin` folder if it exists, so you can create it before packaging:

```bash
echo "legato_url: http://localhost:3080" > streamdeck/plugin/streamdeck.yaml
echo "token: $(cat ~/.local/share/legato/auth-token)" >> streamdeck/plugin/streamdeck.yaml
task streamdeck:package
```

If `legato_url` is absent, it defaults to `http://localhost:3080` (works because the Flatpak has network access).

## Set up profiles in OpenDeck

The plugin uses three profiles that you create manually in OpenDeck:

### 1. Legato (home profile)

Create a profile named **Legato** with two keys:
- Key 1: Assign the **"Legato Home → Switch Profile"** action (from the OpenDeck starter pack), set profile to "Legato Agents"
- Key 2: Same action, set profile to "Legato Attention"

### 2. Legato Agents

Create a profile named **Legato Agents**:
- Keys 1–15: Assign the **"Legato → Agent Tile"** action to each
- Key 16: Assign a "Switch Profile" action to return to "Legato"

The plugin dynamically maps your active agents to these 15 slots. Empty slots appear blank. If you have more than 15 agents, the extras are invisible (future: page 2 profile).

### 3. Legato Attention

Create a profile named **Legato Attention**:
- Keys 1–15: Assign the **"Legato → Attention Tile"** action to each
- Key 16: "Switch Profile" back to "Legato"

Same dynamic mapping as agents — attention items fill slots in order. Review tiles appear only after an agent explicitly marks the review tour ready; incomplete capturing tours are excluded by the Legato attention API.

## Recovery

The plugin is designed to survive Legato stopping and starting repeatedly:

- **Legato running** → WebSocket events drive instant tile updates
- **Legato stopped** → tiles show "OFFLINE" (gray), plugin retries with exponential backoff (1s → 30s max)
- **Legato restarts** → health check succeeds, plugin reconnects, tiles refresh within 1–2 seconds
- **Token regenerated** → plugin detects 401, re-reads the auth-token file, reconnects

The OpenDeck side is also resilient: if the OpenDeck WebSocket drops, the plugin exits and OpenDeck relaunches it automatically.

## How key presses work

### Agent tiles

Pressing an agent tile opens `<legato_url>/agents?task=<task_id>` in your default browser via `xdg-open`. The Legato web UI selects that agent automatically.

### Attention tiles

The plugin checks the attention item's actions:

- If the item has an **execute** action with `intent: "primary"` and `requires_confirm: false`, the plugin executes it directly via `POST /api/attention/{id}/actions/{action_id}`. This is for safe actions like triggering a workflow step.
- If the action requires confirmation (e.g. approving a plan, marking reviewed), or is a navigation-only action, the plugin opens the deep link in the browser instead. This prevents destructive actions from firing from a hardware button without review.

Deep links:
- Plans → `/plans/<id>`
- Reviews → `/review/<id>`
- Workflow runs → `/workflows/runs/<id>`
- Tasks → `/agents?task=<id>`

## Architecture

```
streamdeck/
├── cmd/legato-streamdeck/main.go     # Entry point: parse args, wire everything
├── internal/
│   ├── opendeck/client.go            # OpenAction WS protocol client
│   ├── legato/client.go              # Legato REST + WS client with recovery
│   └── render/render.go              # SVG tile rendering
├── plugin/
│   ├── manifest.json                 # OpenAction plugin manifest
│   └── icons/                        # Placeholder key icons
├── scripts/package.sh                # Assembles .sdPlugin distribution folder
├── go.mod                            # Standalone module (github.com/cpave3/streamdeck-plugin)
└── README.md
```

The plugin is a standalone Go binary with its own `go.mod`. It talks to Legato exclusively through the HTTP/WS API — it imports nothing from Legato's `internal/` packages. This makes it an external client, same as the web UI or mobile app.

## Development

```bash
# Run tests
cd streamdeck && go test ./... -v

# Run with race detector
cd streamdeck && go test -race ./...

# Build
cd streamdeck && go build -o plugin/bin/legato-streamdeck ./cmd/legato-streamdeck

# Package
cd streamdeck && ./scripts/package.sh
```

Manual testing requires OpenDeck and a Stream Deck (or [Tacto](https://tacto.live/) on mobile). The plugin's log output goes to stderr — run OpenDeck from a terminal to see it.
