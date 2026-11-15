# Theme verification — 15 November 2026

Initial port: `fd1fd9b7`, followed by license metadata and documentation at `5db3018e`. The shared material-depth follow-up below supersedes the initial visual captures/results; current screenshots and JSON reflect the refinement. Canonical palette and terminal ANSI values remain unchanged. No live Legato binary, running operator session, theme preference, broker, MQTT topic, Home Assistant configuration or installed app was changed.

## Shared depth refinement — follow-up

Cameron asked for more depth **in the theme system**, not individual screens. The canonical JSON now carries polarity-specific material recipes and a UI-ink contrast contract. Web, desktop, Android, sandboxed/downloaded HTML and Mermaid consume these roles. Reading canvas/foreground and terminal ANSI stay unchanged; panels, raised controls, recessed chrome, softer decorative separators, state washes and offset shadows give the interface its hierarchy. No layout, copy, IDs, preferences or native command contract was redesigned.

- **Web:** 1,646 tests in 224 files, TypeScript, ESLint and production build passed. The final artifact/diagram adapters and their existing tests were included. The shipped Dracula notice remains in the compiled web asset.
- **Desktop:** 51 UI tests, TypeScript and production build passed. Native Rust/IPC code did not change; the earlier 189 Rust and 6 Node results below belong to the initial port.
- **Android:** 122 JVM tests passed; supplementary all-source Android/Compose compilation passed with compiler/deprecation warnings, and the Gradle-generated palette asset matched the canonical JSON. A focused AppThemeTest run passed all five tests. The missing-SDK/native-device limits below still apply.
- **Browser:** 88 checks passed on the final frozen scratch build. This includes the original 82 paths plus real rendered panel/raised shadows and distinct planes, and required input boundaries kept separate from decorative dividers, for all three themes. `verification.json` and `component-contrast.json` contain the latest evidence; screenshots are still Chromium, not native captures. Retained real tmux/xterm, reload, 320px/390px layouts, focus, actual hover, selected diff, image controls and the deliberate unreadable negative control were rechecked.
- **Site:** all 56 pages built and internal links passed again. Static Go build passed using a writable task cache. No new server code; no full Go suite rerun.

Rendered checks caught Dracula red/pink failing 4.5:1 on new washes/raised/selected backgrounds. UI inks now start from original canonical hues and move toward foreground only as far as the shared contrast contract requires; this also avoids turning all secondary Dracula text into primary white. The source/ANSI palette is not changed to hide the failure. Original research values remain in `palettes.json`.

`themes/screenshots/before-depth/` preserves the corresponding pre-refinement captures from `5db3018e`; sibling images are the refined versions. The task artifact provides a before/refined toggle. Native runtime/device validation remains outstanding: compilation, unit tests and generated-data parity do not establish native rendering or restart behaviour.

**Follow-up test audit:** eight added/changed declarations reviewed and kept (four new declarations: web layer/contrast table, desktop restore/material table, Android material parity and semantic-ink tests; four existing binding/lifecycle declarations extended for desktop events/widget, shared voice and open/saved artifacts). They protect distinct app, shell, serialization and independent native-adapter boundaries. No redundant test or test-only production seam added; an unused renderer export was removed. Red→green was observed for collapsed layers, Dracula error on raised/selected surfaces, secondary ink flattening and artifact material propagation. Focused audit runs passed: 35 web cases, 33 desktop cases, 5 Android AppTheme cases, plus 33 artifact/Mermaid cases. The full web suite passed afterwards.

## Initial port checks

- **Web:** TypeScript and ESLint passed. Full Vitest suite: **1,643 tests, 224 files passed**. Production Vite/PWA build passed; existing large-chunk/Browserslist-age warnings remain. `node scripts/theme-utilities.mjs --check` passed; the web build script also checks generated role mappings.
- **Desktop:** **48 UI tests**, **6 Node tests**, **189 Rust library tests** passed. TypeScript and production Vite build passed. Rust/Node tests execute the real page bridge scripts, check strict IDs, origin/top-frame/key guards, save-before-delivery, error replies, invalid persisted-state fallback and retained-page delivery. UI tests go through the shell picker and widget initialization/live events. These tests do **not** start native webviews.
- **Android:** **120 JVM tests** passed, including actual Node execution of bootstrap/delivery/acknowledgement script guards. All Android Kotlin sources compiled against cached Android/Compose APIs with the Compose compiler and temporary resource-ID stubs. Gradle `:app:generateThemeAssets` passed; `mobile/app/build/generated/assets/generateThemeAssets/palettes.json` matched the canonical JSON byte-for-byte.
- **Android build limit:** standard Gradle `:app:testDebugUnitTest :app:compileDebugKotlin` could not resolve tasks because no Android SDK location is configured. The cached compiler/JVM harness is supplementary evidence, **not an APK build or device test**. SharedPreferences false/throw failures are tested at the persistence boundary; a real device's disk failure was not induced.
- **Go:** static `CGO_ENABLED=0 go build` and `go test -short ./config` passed. There are no backend/config-registry changes; the full Go suite was not rerun.
- **Site:** Astro built **56 pages**, and all internal links passed validation. Telemetry was disabled; config/cache were temporary. No documentation-site theme redesign is included.

The Android supplementary checks ran with `/tmp/legato-android-theme/all-tests.sh` and `compile-android.sh`, using cached Kotlin 2.1.20, JUnit 4.13.2, Android APIs and real project sources. Use the installed Java 21 for Gradle; the host's default Java 26 is incompatible with this Gradle version. With a configured SDK, use the repository's normal `task mobile:test` and APK build; no SDK, emulator or app was installed here.

## Actual rendered web paths

`node scripts/verify-themes.mjs` drives cached **Chromium headless shell** with a disposable profile against a scratch Legato on its own port. The scratch instance has its own home, config, SQLite, data/runtime folders and tmux server; the CLI wrapper removes broker/session variables as `docs/agents/scratch-instance.md` requires. A scratch shell session prints ANSI text; no AI provider or real task session is used.

The checker exercises:

- Settings › Appearance › App theme → rendered body/UI roles → reload persistence for all three new themes.
- Original terminal choices, Follow app and preserved explicit Night override.
- A retained **real tmux/xterm** view in another tab: theme changes propagate without remounting its screen; terminal header matches the app surface. Desktop and 390px terminal captures show real ANSI output and cursor.
- Actual board and Settings views at desktop, **390px and 320px**, with no page horizontal overflow.
- Attention, Plans, Review, Reports, Workflows, Learning, Remote and Audit routes, checking that old fixed dark page canvases do not remain under light-mode ink.
- A separate browser fixture importing **production** buttons, state pills, Markdown, diff selection, component cards and image lightbox. It uses the real picker, checks desktop/390px/320px layouts, independently computes rendered text/background contrast (including background alpha compositing), tests selected diff lines and actual desktop hover, and verifies keyboard focus. It opens no review tour and invokes no recording or approval action.
- A deliberate 1:1 text/background mutation verifies that the contrast checker really detects unreadable components; it is removed before subsequent checks/captures.
- No browser JavaScript exceptions.

The final **82-check** machine-readable list is in `verification.json`; rendered component contrast results are in `component-contrast.json`. This is a targeted regression check, **not full WCAG conformance or evidence of clinical readability/fatigue superiority**. Disabled controls, arbitrary author CSS, every dialog, every possible user tint, assistive technologies, zoom/OS contrast modes and all browsers/devices were not exhaustively audited.

### Reproduce with your own scratch instance

Never point this command at the operator's instance. The checker requires an explicit loopback port other than 3080 and an explicit scratch token file. Start the component fixture with Vite on another isolated port; the fixture itself makes no server changes.

```sh
BROWSER=/path/to/existing/chrome-headless-shell \
LEGATO_THEME_TEST_URL=http://localhost:3987 \
LEGATO_THEME_TEST_TOKEN_FILE=/tmp/your-scratch/data/auth-token \
LEGATO_THEME_TEST_AGENT=your-scratch-shell-task-id \
LEGATO_THEME_PREVIEW_URL=http://127.0.0.1:3988/src/test/theme-preview.html \
node scripts/verify-themes.mjs
```

The terminal and component fixture parameters are optional; without them the checker explicitly omits those paths. Native wrappers are **not** covered by this browser command. Desktop hover is emulated and checked as active, not inferred from merely moving a pointer. Screenshots are Chromium captures, not native app screenshots.

## Findings caught while integrating

1. Mounted terminals originally read their palette only once. The regression now proves both terminal components update without creating another xterm/connection.
2. The live-terminal header retained a near-black background in Ocular Light. Its label now sits on the canonical surface.
3. Old Plans/Review and other page canvases were still hardcoded dark. The route-level smoke checks catch this even when component-only tests pass.
4. Image-lightbox captions were **1.24:1** in Ocular Light because neutral text changed but the backdrop stayed black. Only the lightbox's control/header surface is themed; images and the media backdrop remain untouched. The actual lightbox path is contrast-checked now.
5. Native page-only saves could diverge from the persisted shell. Both wrappers now save first through guarded requests and reply with success/error; unavailable bridges and timeouts reach the web picker instead of pretending to save.

## Native/platform limits

- No native Tauri GUI, GTK/WebKit, macOS WKWebView, Android Compose/WebView, emulator or physical-device screenshot was captured for this change. Native event/load timing, device system bars, real restart/reload and actual OS rendering still need on-device validation. Tested abstractions/compilation must not be described as native E2E coverage.
- Native system menus, file/camera/fingerprint/certificate dialogs, keyboard and Android launch splash remain platform-owned or retain their documented original appearance. The desktop floating widget's own content is themed, but its OS context menu is not.
- The standalone terminal TUI, external editors/agents and Even G2 display were not changed. Explicit truecolour/OSC output from applications can override xterm palette choices; no terminal bytes or external tool preferences are rewritten.
- MQTT remains LEG-157's read-only profile. The proposed instance-default/select contract and remaining authorization work are documented in `docs/ai/themes.md`, not implemented.

## Test audit

A read-only Chimera audit reviewed the new theme regressions. Two exact fallback-literal tests were tightened to the runtime role contract; selected-diff colour proof was merged into the existing public selection test. Separate composer, native conversation and image-read bindings were retained because they are independent production callers, not the shared Button path. Native serialization/security/storage tests remain. Missing-bridge and timeout behaviours were added at the picker boundary. No test-only production seam was introduced. The merged/changed tests and their siblings passed; initial picker, mounted-terminal, native and actual-lightbox regressions were observed failing before their fixes.
