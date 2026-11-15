# Legato palettes

`palettes.json` is the authoritative design source for Dracula, Ocular Light, Ocular Dark and their two Vivid variants. Web, desktop and Android mappings derive from this tracked file; no installed app reads the research folder.

Visual guidelines are in [`DESIGN.md`](../DESIGN.md). Its semantic role bindings and Impeccable preview sidecar reference this system rather than maintaining another palette. Refresh `DESIGN.md` and `.impeccable/design.json` together when the documented system changes.

## Provenance

Ocular Light and Ocular Dark are the **unchanged** Work Light and Work Dark candidates from `eye-strain/prototypes/palettes.json`, prepared 15 November 2026 (research task `ay42i4o5`). Only their IDs and display names changed. The original source SHA-256, semantic/syntax role definitions, provenance and research limitations are recorded in the JSON. The research report, comparison and validation were inspected before integration. Its six tests and 23 Chromium checks concern the research prototype, **not this Legato port**.

These exact colours have not demonstrated fatigue, comfort, sleep or clinical superiority. They are engineering proposals with checked contrast pairs, not medical interventions. No human fatigue study has been performed. Display, room light, vision, typography and personal preference still matter.

Dracula uses the official OSS palette, created by Zeno Rocha and maintained by Dracula Theme and contributors: <https://github.com/dracula/dracula-theme>. The representative semantic mapping was supplied with the research source; Legato's application/ANSI mapping is not an official Dracula port. Copyright and MIT permission notice are in `LICENSE-Dracula`, verified against <https://github.com/dracula/dracula-theme/blob/main/LICENSE>. The same permission notice is included in the Dracula entry of `palettes.json`, so it travels with bundled client data; the compiled web asset was checked for the notice. No PRO/Alucard assets are used.

## Vivid variants

**Ocular Light Vivid** (`ocular-light-vivid`) and **Ocular Dark Vivid** (`ocular-dark-vivid`) are additive Legato designs approved by Cameron, not replacements or unchanged research candidates. They retain each original's canvas, surface, selection, foreground, secondary ink, border, number and type tokens and reuse the shared material recipes. Focus/link/info/function, success/string, warning, error and keyword accents are stronger: cleaner/brighter in dark mode, deeper/more saturated in light mode. State washes follow those accents through the existing recipes; UI inks still pass through the material contrast contract. There are no screen-specific colours.

The research source hash above describes the original candidates only. Vivid provenance is recorded in each new entry. Neither variant has evidence of fatigue superiority. Existing themes, defaults and saved terminal overrides remain unchanged; Follow app uses Vivid ANSI when a Vivid app theme is selected, and both variants are also explicit terminal choices.

## Ocular family character

All four Ocular choices share purple action/focus and identity roles, green activity/success, and blue links. Each theme's `ui` record references its existing palette roles rather than duplicating colours: `accent`, `identity`, `voice_start` and `surfaces`. The renderer merges those surface recipes with the polarity material recipes **before** checking readable UI inks. Peer-agent/identity washes are purple; teal notice washes distinguish Legato-carried messages. These are shared application materials, not page-local colours or edits to research/terminal tokens.

The speaking voice dial interpolates from the shared voice-start ink to success; the signing dial already uses accent-to-success. Labels remain explicit in every state and reduced-motion behaviour is retained. Dracula explicitly keeps its previous focus accent, link identity, selection-backed identity wash, info-backed notice wash and flat-success speaking ticks. Legato uses its unchanged CSS fallbacks. The rounded inset composer focus geometry is unchanged; Ocular now uses its purple UI focus ink.

UI identity surfaces are intentionally separate from text selection. Neither these UI mappings nor Vivid's accents claim fatigue superiority. The original research-token palettes, neutral reading/material planes, terminal ANSI and saved preferences remain unchanged.

## Rendering contract

- Source values are opaque sRGB hex. Platforms map roles, not independent hand-picked palettes.
- `render.ts` derives a terminal-safe view of the source: secondary/comment ink below 4.5:1 on canvas falls back to foreground; a boundary below 3:1 on selection uses focus. Canonical Ocular tokens pass unchanged. UI materials/inks use the separate contract below; Android applies the same derivations from the packaged JSON.
- Browser UI variables and ANSI are derived in `web/src/lib/appTheme.ts` and `terminalTheme.ts`. Ocular selection preserves text/ANSI ink on its audited selection background. Dracula selection uses its own foreground where preserving every ANSI hue would fail contrast; terminal cursor uses text with canvas inside the block. ANSI uses semantic families, with readable black/bright-black mapped to secondary ink rather than an invisible canvas colour. Bright ANSI retains the same hues rather than lowering contrast or inventing a second palette.
- The existing Legato UI and all four original terminal presets retain their palettes. An explicit terminal override is independent; absent/unknown/`app` means Follow app. Switching app themes never overwrites a saved terminal override.
- `scripts/theme-utilities.mjs` maps the existing Tailwind chromatic utility vocabulary to semantic ink, washes and boundaries. Run it after changing colour utility use; `--check` detects stale output. It contains no palette values. Media, externally authored pages and server identity tints are not recoloured as text surfaces.

## Material depth

Cameron requested refinement in the **shared theme system**, not one-off page colours. `materials` in the same JSON owns polarity-specific finishes: recessed rail, unchanged reading canvas, panel, raised control, decorative divider and restrained status washes. Each finish blends encoded sRGB channels from canonical roles or neutral endpoints, rounded per channel with `Math.round`; these are application finishes, not edits to the research palette. Web and desktop use `themeMaterial()`, Android's asset adapter follows the same recipe; sandboxed/downloaded HTML and Mermaid map to these same materials and UI inks. Named themes gain contact/ambient shadows with a vertical offset and a fine top edge; Android retains platform-native elevation. Legato's existing default is untouched.

`material_ink_contract` keeps UI text at least 4.5:1 on canvas, selection and every material except decorative divider. Starting from the **original** canonical ink, choose the first 5% blend toward foreground that passes on every surface. This retains readable secondary hierarchy and colour instead of making Dracula metadata pure white or allowing its red/pink to fail on raised/selected surfaces. UI ink is separate from canonical/terminal tokens. Required control outlines retain the strong boundary; decorative separators use the quieter divider. Persistence, IDs, geometry and terminal ANSI are unchanged; UI focus follows the application accent role.

Stable app IDs: `legato`, `dracula`, `ocular-light`, `ocular-dark`, `ocular-light-vivid`, `ocular-dark-vivid`. Names are presentation, not identifiers. Browser persistence is `legato:app-theme`; the public in-page change signal is `legato:theme-changed`. Native apps persist their own application selection and deliver it to retained/new top-frame server pages. A capable native shell exposes `legatoShell.theme = 1`; a web picker asks the key-bound bridge to save and waits for `legato:shell-theme {id, error?}`. Storage and render changes do not grant command authority.

See `docs/ai/themes.md` for automation scope, verification and platform boundaries.
