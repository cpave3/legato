# Native model settings

The native agent panel has a Model settings button beside its model selector. Chimera supplies the available controls for the resolved model. Thinking and reasoning effort appear first. Advanced contains supported sampling, budget, summary, verbosity, tier, output, and parallel-tool controls.

Edits stay local until Apply. Cancel discards edits. Reset to defaults clears the current model's session overrides only after Apply. Default inherits the configured model value, then the endpoint default. Off, false, and zero are explicit values, not defaults.

The panel disables Apply during a run or save. Failed saves retain the draft and show the returned error. Model changes, settings events, and reconnect snapshots use authoritative Chimera state. Old or unavailable capability metadata leaves the controls unavailable with an explanation.

The Go proxy retains model-specific choices such as `max` and `ultra`. It forwards settings-only requests without resetting the model, and preserves Chimera's 400 and 409 responses.

## Enable the feature

1. Release Manticore's capability endpoint and Codex translation changes.
2. Release Chimera's settings resolver, session API, and compatible adapter.
3. Add `"capabilitySource": "manticore"` to the existing Manticore provider in Chimera configuration.
4. Release the Legato panel changes through your normal process.

Keep existing provider credentials, URLs, and headers. The source implementation does not edit global configuration, restart services, or replace running executables. Consult Chimera's `docs/model-settings.md` for provider examples and custom local-server declarations.

The capabilities are model-specific. Codex requires thinking. A reasoning summary can be disabled without disabling reasoning. Some Kimi models have fixed sampling parameters, and unknown local endpoints require an explicit profile.

## Vision metadata

The existing native model selector's vision indicator now includes verified image support discovered by Chimera, including supported Manticore Codex aliases. The Go proxy and native state retain the additive `inputModalities` field. Image routing remains in Chimera: selected vision-capable models keep image turns, while text-only or unknown models use the configured fallback. No new attachment composer or panel-side routing is added.

Release Manticore's image translation and capability metadata before the updated Chimera build. Existing global configuration with `capabilitySource: "manticore"` needs no change. No database migration is required.

## Verification

The shared fixture is `contracts/model-settings-v1.json`. Tests cover Go serialization, settings patches, frontend controls, errors, reset/cancel, stale responses, and reconnect state. Backend tests use local mocks. No browser, live-model, or end-to-end tests are part of this change.

The normal web build and extension build remain separate from deployment. Existing unrelated web lint failures in `TerminalPanel.tsx`, `useAgentSelection.tsx`, and `useToast.tsx` are not changed by this feature.
