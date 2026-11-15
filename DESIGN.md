---
name: Legato
description: A clear, operational, state-led control room with tactile, confident tools.
colors:
  primary: "var(--color-lw-accent)"
  on-primary: "var(--theme-canvas, white)"
  link: "var(--color-lw-link)"
  rail: "var(--color-lw-rail)"
  canvas: "var(--color-lw-ground)"
  panel: "var(--color-lw-surface)"
  raised: "var(--color-lw-raised)"
  selected: "var(--color-lw-selected)"
  ink: "var(--color-lw-text)"
  secondary-ink: "var(--color-lw-text-2)"
  divider: "var(--color-lw-line)"
  boundary: "var(--color-lw-line-strong)"
  focus: "var(--theme-focus, var(--color-lw-accent))"
  running: "var(--color-lw-live)"
  running-wash: "var(--color-lw-live-soft)"
  needs-attention: "var(--color-lw-needs)"
  complete: "var(--color-lw-done)"
  error: "var(--color-lw-error)"
typography:
  page-title:
    fontFamily: "var(--font-lw, system-ui, sans-serif)"
    fontSize: "1.25rem"
    fontWeight: 600
    lineHeight: "1.75rem"
  section-title:
    fontFamily: "var(--font-lw, system-ui, sans-serif)"
    fontSize: "1rem"
    fontWeight: 600
    lineHeight: "1.5rem"
  body:
    fontFamily: "var(--font-lw, system-ui, sans-serif)"
    fontSize: "0.875rem"
    fontWeight: 400
    lineHeight: "1.25rem"
  label:
    fontFamily: "var(--font-lw, system-ui, sans-serif)"
    fontSize: "0.75rem"
    fontWeight: 500
    lineHeight: "1rem"
  code:
    fontFamily: "var(--font-lw-mono, ui-monospace, monospace)"
    fontSize: "0.75rem"
    fontWeight: 400
    lineHeight: "1rem"
rounded:
  md: "0.375rem"
  lg: "0.5rem"
  xl: "0.75rem"
  full: "9999px"
spacing:
  "2": "0.5rem"
  "3": "0.75rem"
  "4": "1rem"
  "5": "1.25rem"
  "6": "1.5rem"
components:
  button-primary:
    backgroundColor: "{colors.primary}"
    textColor: "{colors.on-primary}"
    typography: "{typography.body}"
    rounded: "{rounded.md}"
    padding: "0 1rem"
  button-secondary:
    backgroundColor: "{colors.raised}"
    textColor: "{colors.ink}"
    typography: "{typography.body}"
    rounded: "{rounded.md}"
    padding: "0 1rem"
  button-outline:
    textColor: "{colors.secondary-ink}"
    typography: "{typography.body}"
    rounded: "{rounded.md}"
    padding: "0 1rem"
  input-compact:
    backgroundColor: "{colors.raised}"
    textColor: "{colors.ink}"
    typography: "{typography.label}"
    rounded: "{rounded.md}"
    height: "2rem"
    padding: "0 0.5rem"
  navigation-item:
    backgroundColor: "{colors.rail}"
    textColor: "{colors.secondary-ink}"
    rounded: "{rounded.lg}"
  navigation-item-active:
    backgroundColor: "{colors.selected}"
    textColor: "{colors.ink}"
    rounded: "{rounded.lg}"
  status-running:
    backgroundColor: "{colors.running-wash}"
    textColor: "{colors.running}"
    typography: "{typography.label}"
    rounded: "{rounded.md}"
    padding: "0.125rem 0.5rem"
  group-panel:
    backgroundColor: "{colors.panel}"
    textColor: "{colors.ink}"
    rounded: "{rounded.xl}"
    padding: "1rem"
  task-card:
    backgroundColor: "{colors.panel}"
    textColor: "{colors.ink}"
    rounded: "{rounded.lg}"
    padding: "0.625rem 0.75rem"
---

# Design System: Legato

## Overview

**Creative North Star: "The Control Room"**

Legato is clear, operational and state-led. Its interface helps a person scan work, distinguish agent activity from requests that need attention, and act without losing their place. Hierarchy comes from meaningful state, typography and surface roles—not from decoration competing with the work.

The approved component feel is **tactile and confident**: layered surfaces and subtly raised controls make actions feel tangible, without decorative effects. This is a record of the accepted application system, not a redesign brief for the documentation site, terminal TUI or unrelated packages.

**Key Characteristics:**
- Functional colour roles rather than atmospheric palette names.
- Dense, legible operational content with a clear active location and current state.
- Distinct structural, reading, panel and raised-control planes.
- Shared recipes across web, desktop, Android and theme-aware artifacts, with native conventions retained.

## Colors

Colour carries function: where to read, what is interactive, what is selected and what needs attention.

**Authority:** [themes/palettes.json](themes/palettes.json) owns named-theme colours, material recipes and the UI-ink contrast contract. [themes/render.ts](themes/render.ts) derives the web/desktop materials; Android consumes the same JSON. The frontmatter above binds design roles to existing CSS variables; it does **not** create a second hex palette. Typography, radius and spacing entries record reused web conventions, not new runtime declarations. [themes/README.md](themes/README.md) describes provenance and derivation; [docs/ai/themes.md](docs/ai/themes.md) describes adapters and persistence.

The role references resolve in the selected application theme; a standalone consumer must load that context rather than assume one fixed palette. No synthetic hue ramps are introduced for these multi-theme roles.

### Primary

- **Action accent:** primary actions, current selection boundaries and focus—not every inactive surface.
- **On-action ink:** paired with the action fill by the theme, never assumed to be white in every polarity.
- **Link ink:** interactive text, with an underline or another visible affordance where needed; a link is not a second brand accent.

### Neutral

- **Rail:** recessed navigation and structural chrome.
- **Canvas:** the steady reading/terminal ground. Do not change it locally to manufacture depth.
- **Panel:** grouped operational content above the canvas.
- **Raised:** controls and smaller elevated surfaces above a panel.
- **Selection:** the current choice or selected range; distinct from hover and focus.
- **Primary/secondary ink:** content and supporting metadata, both readable on their surface.
- **Divider/strong boundary:** quiet structural separation versus a required control outline. These are different roles.

### Operational states

Running, needs attention, complete, error and idle use labelled state, iconography and restrained washes; never hue alone. Preserve the meaning of each state across themes. Server identity tints remain independent of theme and must not be repurposed as status.

**The Shared Roles Rule.** New surfaces use the shared roles and recipes. Do not add a screen-specific palette or copy derived RGB values out of a screenshot.

**The Readable State Rule.** Ordinary UI text targets at least 4.5:1; required control and focus boundaries target at least 3:1. Check default, hover, selected, error and focus states. The named-theme ink contract adapts original hues across the material surfaces; it does not change canonical research or terminal ANSI values. Do not assume every legacy/default combination already meets these targets.

Choices remain Legato, Dracula, Ocular Light and Ocular Dark. Legato's existing default and saved terminal overrides are preserved. Ocular is not a proven fatigue or medical intervention. Dracula attribution and its MIT notice remain with the canonical assets.

## Typography

The themed web UI and Android use **Geist Sans** for operational text, with **Geist Mono** for code, identifiers, durations and measurements. The frontmatter records the reused web title/body/label conventions; there is no marketing display type role.

Use weight and grouping before increasing size. Settings titles use the page-title role; Board has existing larger phone and more compact wide-layout headings. Secondary text is smaller or lower in the hierarchy, not made unreadable through opacity. Keep task titles in the UI face; reserve monospace for actual data.

Platform and legacy exceptions are real: web body text still has a system-font fallback in `web/src/index.css`; desktop chrome uses its existing Inter/system stack at 14px/1.45; Android's base text is 15sp with 13sp secondary text and native 24/28sp headings. Do not silently replace these with one cross-platform CSS scale. Match the host component and use native scalable units.

## Layout

Keep related content tight and separate different groups generously. The web uses the existing quarter-rem spacing rhythm, commonly the steps in frontmatter. Settings groups contain rows separated by dividers; they do not need a competing elevated card for every label. Task cards are independent work items, not a marketing-card page scaffold.

Web navigation switches from the existing 64px left rail to a five-slot bottom bar below the medium breakpoint. The app reserves space for that bar and device safe areas. Boards change their composition on narrow screens rather than shrinking a desktop board until it fits. Use structural wrapping/collapse, not fluid display typography.

The observed web breakpoints and native/chrome metrics are recorded in the sidecar. Desktop retains its 68px server dock and 40px title bar; Android uses single-column native screens, safe-drawing insets and its existing 16dp gutters. These are platform mappings, not identical CSS copied everywhere.

Check actual 320px and 390px web views as well as wide views. Keep input, selection and action paths usable by keyboard and touch. Do not infer native-device coverage from browser screenshots; [themes/VERIFICATION.md](themes/VERIFICATION.md) states the current limits.

## Elevation & Depth

Depth is a hybrid of tonal layers and soft structural shadows. The rail recedes; the reading canvas stays steady; panels group content; raised surfaces make controls tangible. Tactility comes from their relationships, not from making every element louder.

### Shadow Vocabulary

- **Contact/control:** `var(--shadow-lw-control)` gives a fine top edge and a small vertically offset contact shadow. Use it for raised/selected controls where the material adapter already applies it.
- **Panel:** `var(--shadow-lw-panel)` adds a restrained ambient falloff to the contact shadow. Use it for grouped elevated content, not every child row.
- **Native:** Android maps these planes to its Material3 surface roles and native elevation. Desktop uses its adapter aliases. Do not imitate CSS shadows by inventing unrelated native colours.

The canonical recipe owns shadow ink, opacity, highlight and surface blends; their values must not be duplicated here. The sidecar references the live variables. Existing Legato-default and platform-owned treatments remain unchanged unless a separate change explicitly migrates them.

**The Tactile Hierarchy Rule.** A control should feel actionable and a panel should feel grouped; neither should compete with the content through ornamental glow, arbitrary gradients or a new motion effect.

## Shapes

Use the existing gently rounded vocabulary. Web controls, work cards and grouped panels use the different established radius steps in frontmatter, rather than one oversized radius everywhere. Toggles and compact state indicators may use full rounding; the whole interface is not pill-shaped.

Keep native exceptions: Android grouped surfaces/buttons use 12dp corners and fields 10dp; desktop retains its bespoke control/card/sheet corners. Borders are normally thin. Decorative dividers must not be substituted for the stronger boundary that makes a control findable.

## Components

Sidecar snippets are self-contained, role-based web previews. They are not replacements for the production React/Compose components, exact snapshots of every legacy variant, or evidence of native-device rendering. Reuse the host primitives for real screens.

### Buttons

Tactile and confident, with a clear action hierarchy. Reuse the shared Button variants or the host settings/native primitives rather than making another family. The shared web Button uses the control radius, medium weight, an action fill or restrained raised/outline/ghost treatment, and a default minimum height of 44px. Its compact variants and existing 40px settings buttons remain contextual exceptions. Android primary actions are 48dp high; round actions are 44dp.

Hover, keyboard focus, selected, busy, disabled and error states must remain distinguishable. Use the focus role (with the action accent as the existing-default fallback), not an unverified dark ring. Disabled controls are visibly inactive and cannot submit; do not hide save failures. The sidecar previews primary, secondary and outline variants.

### Chips / State indicators

Compact, labelled operational metadata. A state wash supports its ink and icon; it is not a saturated decoration. A selected choice is not the same as a running state. Checked theme options retain their visible selection treatment and accessible radio state.

### Cards / Containers

Group related content on the panel role, with quiet borders/dividers and the panel shadow where appropriate. Task cards retain a compact title, supporting identifiers/metadata and explicit activity/review strips. Selection uses the shared selected surface and accent boundary. Do not apply a second palette inside a card.

### Inputs / Fields

Use readable ink, a theme-aware surface, a strong required boundary and an explicit label. Placeholder text is supporting guidance, not a substitute for that label. Preserve the host's sizing: compact web settings fields, roomier composers, desktop fields and Android's minimum 48dp fields are not one universal-height component. The sidecar's compact field is an observed web variant, not a mobile touch-target recommendation.

### Navigation

Always make the current location and pending work legible. Reuse the wide icon-and-label rail, the phone's bottom navigation and More disclosure, or the native shell's server dock/header. Keep names, accessible labels and counts. Server tints identify servers; state badges identify work.

### Activity and feedback

Existing working-agent traces communicate actual activity; reduced-motion settings disable their movement. Loading, empty, unavailable and failed states explain what is happening and leave the next action findable. Do not add an animation to every component to make polish visible. Preserve the user's existing motion/effect preferences.

### Theme-aware artifacts

Sandboxed and downloaded HTML and Mermaid consume the same functional surfaces, readable inks and depth roles. Authored content can override them, so arbitrary custom HTML is not automatically contrast-safe. External media is not recoloured as a UI surface. The terminal palette remains independently selectable and is not rewritten by a UI-depth refinement.

## Do's and Don'ts

### Do:
- **Do** make the current state and the next action easy to scan.
- **Do** use shared material roles across web and native adapters, with platform-appropriate geometry and elevation.
- **Do** keep primary content, secondary metadata, selection and focus distinct and readable.
- **Do** use structural depth to make panels grouped and controls tactile.
- **Do** verify actual rendered states and small screens; report native and accessibility verification limits honestly.
- **Do** keep canonical palette data, derived UI ink and terminal ANSI responsibilities separate.

### Don't:
- **Don't** solve a new screen with another palette or pasted derived colour values.
- **Don't** flatten rail, canvas, panel and raised control into one paint value.
- **Don't** use quiet dividers as required control boundaries or colour alone as status.
- **Don't** manufacture hierarchy with unreadable opacity, decorative glow or motion on every surface.
- **Don't** turn legacy/platform exceptions into new global rules or silently change saved theme, terminal or motion preferences.
- **Don't** claim Ocular fatigue superiority, native-device verification or full accessibility conformance from the current evidence.
