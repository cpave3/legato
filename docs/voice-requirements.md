# Voice — Requirements (separate suite app)

> **Scope**: This document describes a planned **separate app** in the legato suite, not a feature to be built into legato itself. Voice is a system-wide input device, not a developer-workflow tool, and it does not belong inside the legato TUI/server. The integration point with legato is via the planned MCP server (see `openspec/changes/knowledge-memory/`).
>
> **Working name**: TBD (placeholder: `legato-voice`).

## Why a separate app?

Voice dictation runs everywhere: editors, terminals, browsers, chat apps, system text fields. It needs a global hotkey daemon, audio device access, and a system-wide focus-injection mechanism. Bundling that into legato would:

- Make legato a tray-resident app even when the user has no kanban board to manage.
- Force a GUI/native dependency stack onto legato (currently a clean TUI + Go HTTP server).
- Conflate two unrelated concerns: project task tracking vs. operating-system-level dictation.

A separate app shares the legato suite's identity, opt-in install, and (optionally) talks to legato's MCP server for project-aware behavior.

## Inspiration / reference

BridgeMind's BridgeVoice. Concrete points worth borrowing:

- Tauri 2.0 + Rust core; small binary, multi-platform.
- Local Whisper variants (whisper.cpp) for English, cloud Whisper Large-v3-Turbo (Groq or alternative) for 99+ languages.
- Push-to-Talk and Toggle modes with customizable global hotkeys.
- <10ms recording start, <1s end-to-end latency.
- System-wide text injection — works in any focused app.
- Custom dictionary for term replacement (e.g. "Bridge mind" → BridgeMind).
- Stats: WPM, words, speaking time. History with re-copy.

What to deliberately do *better* than BridgeVoice:

- Project-aware dictionary auto-populated from legato's MCP server (workspace names, task IDs, branch names, repo names, frequent identifiers from notes).
- Dictation can target a *specific* legato agent pane rather than the focused window — useful when the user is reading code in their editor and wants to send instructions to a tmux pane.
- Open-source.

## Functional requirements

### F1. Capture
- F1.1 Push-to-Talk mode: hold a configurable global hotkey to record; release to stop.
- F1.2 Toggle mode: press a hotkey to start, press again to stop.
- F1.3 Mic device selectable from system input devices; remembers last selection.
- F1.4 Visual indicator (tray icon, optional small overlay) when recording.
- F1.5 Cancel-recording hotkey discards the current capture without transcribing.

### F2. Transcription
- F2.1 Local backend: whisper.cpp with selectable model size (tiny → large-v3). Models downloaded on demand to a known cache dir.
- F2.2 Cloud backend: pluggable provider interface; ship with Groq (Whisper Large-v3-Turbo) and OpenAI Whisper as options.
- F2.3 Language: local mode is English-only (whisper.cpp practical limit); cloud mode supports 99+ languages with auto-detect.
- F2.4 Transcription latency: <1s end-to-end for utterances up to 10s on local mode (with the small/medium model on a modern laptop).
- F2.5 Streaming option: cloud provider may return partial transcriptions; if supported, the app streams interim text for live preview.

### F3. Output (text injection)
- F3.1 Default: inject transcribed text into the currently focused application via OS-native input simulation (macOS: `CGEvent`/`AXUIElement`; Windows: `SendInput`; Linux: `wtype`/`ydotool` for Wayland, `xdotool` for X11).
- F3.2 Clipboard mode: copy to clipboard instead of injecting (configurable per profile).
- F3.3 Custom-target mode (legato integration): route text to a named legato tmux pane via the legato CLI (`legato pane send <task-id> "<text>"`). Configurable global hotkey.
- F3.4 Dictionary-driven term replacement runs after transcription, before injection.
- F3.5 Newline handling: by default a final period or "new paragraph" voice command yields a newline; configurable.

### F4. Project awareness (optional, requires legato MCP)
- F4.1 If legato MCP server is reachable (auto-discovered via known port + token, or configured), the app pulls a project term list at startup and on workspace change.
- F4.2 Terms are added to the dictionary as transcription hints (Whisper supports `initial_prompt` for context priming).
- F4.3 Voice commands like "tell legato status" or "create task <title>" route to the MCP server's `list_tasks` / `create_task` tools (out of scope for v1; documented as future).

### F5. History and stats
- F5.1 Each transcription is logged with timestamp, target app (where injected), and word count.
- F5.2 History viewer (last 100) with one-click re-copy.
- F5.3 Stats page: words today, total speaking time, WPM rolling average.
- F5.4 History retained on disk; user can clear from Settings.

### F6. Settings
- F6.1 Hotkey configuration UI (capture global key combinations).
- F6.2 Backend selection (local | cloud-groq | cloud-openai).
- F6.3 Local model selection with download/manage.
- F6.4 Custom dictionary editor (term → replacement, optional regex).
- F6.5 Audio device selection.
- F6.6 Output mode default (inject | clipboard | legato-pane).
- F6.7 Privacy: toggle for "send anonymous usage stats" (off by default; we don't ship stats collection in v1).

## Non-functional requirements

### N1. Performance
- N1.1 <10ms hotkey-press → recording-active.
- N1.2 <1s end-to-end on local small model for ≤10s utterances.
- N1.3 Idle CPU <0.1%; recording CPU <5% on a modern laptop.
- N1.4 Memory footprint: <200 MB resident with small Whisper model loaded; <2 GB with large-v3.

### N2. Privacy
- N2.1 Local mode never sends audio to a network.
- N2.2 Cloud mode sends audio over TLS to the configured provider only.
- N2.3 No telemetry by default; explicit opt-in for any future stats reporting.
- N2.4 No retention of audio after transcription completes (in-memory only; not written to disk unless user enables a debug-recordings flag).

### N3. Platform support
- N3.1 macOS 12+ (Apple Silicon and Intel).
- N3.2 Windows 11 (Win10 best-effort).
- N3.3 Linux: Wayland (via `wtype`/`ydotool`) and X11 (via `xdotool`). Tested on Fedora and Ubuntu LTS.
- N3.4 Single binary distribution per platform (Tauri produces these natively).

### N4. Accessibility
- N4.1 No spoken-feedback requirement in v1, but the app must coexist with system screen readers without conflict.
- N4.2 Recording indicator must be perceivable in non-tray contexts (audio chime option).

### N5. Reliability
- N5.1 Crash in transcription backend must not block the hotkey daemon — backend runs in a worker process.
- N5.2 If cloud provider times out (>10s), fall back to local model automatically (configurable).
- N5.3 If audio device disappears mid-recording (e.g., headset disconnect), surface a clear error and discard the capture.

## Architecture sketch

```
┌──────────────────────────────────────────────────────────────────┐
│ legato-voice (Tauri app, system tray)                            │
│                                                                  │
│  ┌──────────────┐    ┌──────────────┐   ┌──────────────────┐    │
│  │ Hotkey       │───▶│ Audio        │──▶│ Transcription    │    │
│  │ daemon       │    │ capture      │   │ worker (separate │    │
│  │ (rdev/global)│    │ (cpal)       │   │ process)         │    │
│  └──────────────┘    └──────────────┘   └────────┬─────────┘    │
│                                                  │              │
│                                                  ▼              │
│                                         ┌──────────────────┐    │
│                                         │ Backend          │    │
│                                         │ - whisper.cpp    │    │
│                                         │ - groq client    │    │
│                                         │ - openai client  │    │
│                                         └────────┬─────────┘    │
│                                                  │              │
│                                                  ▼              │
│  ┌──────────────┐    ┌──────────────┐   ┌──────────────────┐    │
│  │ Settings     │    │ Dictionary / │◀──│ Post-processing  │    │
│  │ UI (Tauri    │    │ history      │   │ (dict, newlines) │    │
│  │ webview)     │    │ store        │   └────────┬─────────┘    │
│  └──────────────┘    └──────────────┘            │              │
│                                                  ▼              │
│                                         ┌──────────────────┐    │
│                                         │ Output router    │    │
│                                         │ - inject         │    │
│                                         │ - clipboard      │    │
│                                         │ - legato pane    │    │
│                                         └──────────────────┘    │
│                                                                  │
│  Optional: MCP client → legato MCP server (project terms)       │
└──────────────────────────────────────────────────────────────────┘
```

**Why split processes**: keeps the always-on hotkey/UI process small and crash-tolerant. Transcription is the part most likely to break (model load failure, GPU issues, cloud timeouts). Isolating it means a transcription crash doesn't drop the hotkey listener.

## Tech stack (recommended)

| Concern | Choice | Notes |
|---|---|---|
| Framework | Tauri 2.0 | Same as BridgeVoice; small binaries, multi-platform |
| Language | Rust | Hotkey daemon, audio capture, FFI to whisper.cpp |
| UI | Tauri webview + minimal HTML/Vanilla JS | Settings only; not perf-critical |
| Audio capture | `cpal` | Cross-platform stdlib for Rust |
| Hotkeys | `rdev` or `global-hotkey` | Both Rust crates, both viable |
| Local Whisper | `whisper-rs` (whisper.cpp bindings) | |
| Cloud Whisper | `reqwest` + provider-specific JSON | |
| Settings store | `serde_json` to `$XDG_CONFIG_HOME/legato-voice/config.json` | |
| History | SQLite via `rusqlite` | Same idiom as legato itself |
| Text injection | `enigo` (cross-platform) with platform-specific fallbacks | |

## Out of scope (v1)

- Voice-to-command / voice-to-AI ("dictate a prompt and route it to Claude"). Defer to v2 once the primitives are solid.
- Speaker diarization.
- Real-time transcription overlay always-on.
- Translation (transcribe in language X, output in language Y).
- Multi-account / team sync of dictionaries.
- Mobile clients.

## Success criteria

- A developer can install legato-voice, set a hotkey, and dictate an email in any text field within 5 minutes of first launch.
- Dictation is fast enough that experienced users prefer it over typing for messages >20 words.
- With legato MCP available, terms unique to the user's project (workspace names, task IDs) transcribe correctly without manual dictionary additions.
- The app remains responsive and tray-stable across a 12-hour workday with hundreds of dictations.

## Open questions

- Do we ship cloud providers' API keys, or require BYO? **Tentative**: BYO. Avoids us managing a billing relationship.
- How do we handle the `$XDG_CONFIG_HOME/legato-voice/` vs `$HOME/Library/Application Support/legato-voice/` split per platform without UX divergence? **Defer**: Tauri provides path helpers; document each location in Settings.
- Can the MCP project-term list be fetched without a long-lived connection? **Yes** — the MCP HTTP transport gives us request-response semantics; we poll on workspace change events.
- Is "send to legato pane" a hotkey overlay (pick pane after dictation) or per-target hotkey? **Tentative**: Per-target hotkey set in Settings (Hotkey 1 → focused app, Hotkey 2 → legato pane "primary"). Overlay picker is a v2 nicety.
