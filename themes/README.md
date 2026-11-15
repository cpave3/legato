# Legato palettes

`palettes.json` is the authoritative design source for Dracula, Ocular Light and Ocular Dark. Web, desktop and Android mappings derive from this tracked file; no installed app reads the research folder.

## Provenance

Ocular Light and Ocular Dark are the **unchanged** Work Light and Work Dark candidates from `eye-strain/prototypes/palettes.json`, prepared 15 November 2026 (research task `ay42i4o5`). Only their IDs and display names changed. The original source SHA-256, semantic/syntax role definitions, provenance and research limitations are recorded in the JSON. The research report, comparison and validation were inspected before integration. Its six tests and 23 Chromium checks concern the research prototype, **not this Legato port**.

These exact colours have not demonstrated fatigue, comfort, sleep or clinical superiority. They are engineering proposals with checked contrast pairs, not medical interventions. No human fatigue study has been performed. Display, room light, vision, typography and personal preference still matter.

Dracula uses the official OSS palette, created by Zeno Rocha and maintained by Dracula Theme and contributors: <https://github.com/dracula/dracula-theme>. The representative semantic mapping was supplied with the research source; Legato's application/ANSI mapping is not an official Dracula port. Copyright and MIT permission notice are in `LICENSE-Dracula`, verified against <https://github.com/dracula/dracula-theme/blob/main/LICENSE>. The same permission notice is included in the Dracula entry of `palettes.json`, so it travels with bundled client data; the compiled web asset was checked for the notice. No PRO/Alucard assets are used.

## Rendering contract

- Source values are opaque sRGB hex. Platforms map roles, not independent hand-picked palettes.
- `render.ts` derives web/desktop small-text mappings. When a secondary/comment token is below 4.5:1 on canvas it uses the palette's own foreground. A boundary below 3:1 on selection uses focus. Ocular tokens pass unchanged. This makes Dracula's small secondary text readable without changing or misrepresenting its upstream comment value. Android must apply the same documented rule in its native adapter.
- Browser UI variables and ANSI are derived in `web/src/lib/appTheme.ts` and `terminalTheme.ts`. Ocular selection preserves text/ANSI ink on its audited selection background. Dracula selection uses its own foreground where preserving every ANSI hue would fail contrast; terminal cursor uses text with canvas inside the block. ANSI uses semantic families, with readable black/bright-black mapped to secondary ink rather than an invisible canvas colour. Bright ANSI retains the same hues rather than lowering contrast or inventing a second palette.
- The existing Legato UI and all four original terminal presets retain their palettes. An explicit terminal override is independent; absent/unknown/`app` means Follow app. Switching app themes never overwrites a saved terminal override.
- `scripts/theme-utilities.mjs` maps the existing Tailwind chromatic utility vocabulary to semantic ink, washes and boundaries. Run it after changing colour utility use; `--check` detects stale output. It contains no palette values. Media, externally authored pages and server identity tints are not recoloured as text surfaces.

Stable app IDs: `legato`, `dracula`, `ocular-light`, `ocular-dark`. Names are presentation, not identifiers. Browser persistence is `legato:app-theme`; the public in-page change signal is `legato:theme-changed`. Native apps persist their own application selection and deliver it to retained/new top-frame server pages. A capable native shell exposes `legatoShell.theme = 1`; a web picker asks the key-bound bridge to save and waits for `legato:shell-theme {id, error?}`. Storage and render changes do not grant command authority.

See `docs/ai/themes.md` for automation scope, verification and platform boundaries.
