# Server theme / Follow server verification

Verified in an isolated Legato clone on 2026-11-15. No live credentials, production broker/HA configuration, installed app, primary checkout or live Legato process was changed.

## Actual entrypoints

Two scratch `legato serve` instances, each with separate config/database/data/runtime, and a scratch Mochi MQTT broker drove Chromium. `verification.json` records 14 passing checks: MQTT and manual Settings share one persisted preference; only opted-in clients follow it; switching, reload, offline cache and inactive-server events preserve active-server ownership; retained commands cannot mutate state; local fallback and explicit terminal selection survive. Settings fit 320, 390 and 1440 px; screenshots are alongside this report.

The broker integration tests exercise discovery, strict payload validation, revision conflicts, nonce rotation/reconnect, retained replay and live retained rejection, bounded queues/rate, disabled control, unchanged aggregate-only ACLs, and actual CLI entrypoints. They use synthetic scratch credentials, not the operator's broker.

## Checks

- `CGO_ENABLED=0 go test ./...`: passed (isolated writable Go cache).
- Relevant event/service/command race tests: passed.
- Go vet and targeted lint: passed.
- Web: 228 files / 1,698 tests passed; TypeScript, ESLint and production/PWA build passed.
- Desktop: 205 Rust tests passed; TypeScript, production build and canonical theme catalog checks passed. Generated trusted bridge scripts execute in Node, including origin/frame/request guards and all canonical IDs.
- Android: 25 JVM tests passed against actual theme owner, preference serializer, metadata parser and generated bridge scripts. Uses cached Kotlin/JUnit dependencies and a compile-only certificate-adapter stub; this does not validate pinned HTTPS transport.
- Documentation site: built successfully with all internal links valid.

## Test audit

Workers audited their layer's added tests. Android merged duplicate reopen coverage and fixed its offline test to call the production error-completion path. Final parent audit strengthened unsupported/older-server fallback checks to transition from an applied server theme, and made native-owned web coverage wait for metadata processing rather than merely a fetch call. Desktop command coverage derives IDs from the canonical palette registry. The service persistence test name now describes the observable notification/reopen behavior rather than claiming it observes commit timing. Added partial-migration replay coverage caught and fixed a non-idempotent migration. No test-only production seams were added. An additional independent final auditor timed out without a usable report; it is not counted as verification.

Mutation checks caught forced-local Android resolution and removal of the MQTT rate limiter. Targeted regressions were observed red before their fixes, including inactive native delivery and removed-server cache handling.

## Limits

No native GUI, Android device/emulator or installed Home Assistant was tested. Android Gradle tests/APK build are blocked by the missing Android SDK. HA discovery templates were inspected and broker-tested, but not executed inside Home Assistant. Native tests and browser tests are not claims of device verification. Existing Vite chunk-size/Browserslist-age warnings remain.
