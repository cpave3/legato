## Native model settings: internals

The user guide is [Model settings](../../site/src/content/docs/agent-tools/model-settings.md) (on the site at `/legato/agent-tools/model-settings/`). This file keeps the implementation and rollout notes.

### Where it lives

- `web/src/components/ComposerModelMenu.tsx` — the model chip of the native composer and its menu: the models (with `default`, a vision mark and the context window), the effort levels, and **More session settings…**, which opens the form. `NativeAgentPanel` passes the form in as `settings`.
- `web/src/components/NativeModelSettings.tsx` — `NativeModelSettingsForm`, the form. The file also exports `NativeModelSettings`, a standalone button that opens the form in a dropdown; nothing renders it now that the form opens from the model menu.
- `web/src/lib/modelSettings.ts` — the types (`ModelSettings`, `ModelCapabilities`, `ModelSettingsState`) and the labels.
- The shared fixture is `contracts/model-settings-v1.json`.

The form reads `GET /api/agents/{task_id}/chimera/model` and writes `POST` to the same path with `{expectedModel, settings, resetSettings?}`; `/api/agents/{task_id}/chimera/models` lists models (`internal/server/server.go`). The Go proxy keeps model-specific choices such as `max` and `ultra`, forwards settings-only requests without resetting the model, and preserves Chimera's `400` and `409` responses. Only settings whose capability `status` is `supported` are drawn; the form is shown only for `schemaVersion: 1` capabilities whose `model` matches the session's. A response for another model is refused with "The model changed. Reopen its settings." Model changes, `model_settings_changed` events and reconnect snapshots replace the state from the agent (authoritative Chimera state), so a stale response cannot win.

The Claude Code bridge (`internal/engine/claudebridge/model.go`) and the Codex bridge (`internal/engine/codexbridge/model.go`) answer the same protocol with one setting, `reasoningEffort` (Codex: only when the model lists its effort levels); see [Claude Code native integration](claude-code-native.md) § Model and effort and [Codex native integration](codex-native.md).

Behaviour the tests cover: edits stay local until Apply; Cancel discards; Reset to defaults clears the current model's session overrides only after Apply; Default inherits the configured model value, then the endpoint default; Off, false and zero are explicit values, not defaults; Thinking set to Off clears and disables Reasoning effort and Thinking budget; Apply is disabled during a run or a save; a failed save keeps the draft and shows the returned error; old or unavailable capability metadata leaves the controls unavailable with an explanation.

### Vision metadata

The model selector's vision mark includes verified image support discovered by Chimera, including supported Manticore Codex aliases. The Go proxy and native state keep the additive `inputModalities` field. Image routing stays in Chimera: selected vision-capable models keep image turns, while text-only or unknown models use the configured fallback. No attachment composer or panel-side routing was added for it.

### Rollout

The feature spans three projects, released in this order:

1. Manticore's capability endpoint and Codex translation changes (and its image translation and capability metadata, before the Chimera build that reads them).
2. Chimera's settings resolver, session API and compatible adapter.
3. `"capabilitySource": "manticore"` added to the existing Manticore provider in Chimera's configuration. Existing provider credentials, URLs and headers stay; a configuration that already has it needs no change.
4. Legato's panel changes, through the normal release process.

The implementation does not edit global configuration, restart services or replace running executables. No database migration is required. Chimera's own `docs/model-settings.md` has provider examples and custom local-server declarations.

Capabilities are model-specific: Codex requires thinking; a reasoning summary can be disabled without disabling reasoning; some Kimi models have fixed sampling parameters; unknown local endpoints require an explicit profile.

### Verification

Tests cover Go serialization, settings patches, frontend controls, errors, reset and cancel, stale responses and reconnect state. Backend tests use local mocks. No browser, live-model or end-to-end tests were part of the change. The web build and the extension build stay separate from deployment. Web lint failures in `TerminalPanel.tsx`, `useAgentSelection.tsx` and `useToast.tsx` predated the feature and were not changed by it.
