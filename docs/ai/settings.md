# Settings in the database

Settings live in `config.yaml` and, for every setting that is not file-only, in the database too. The file pins; the database fills in. Site page: `reference/configuration` § Settings in the file and in the database.

## The registry

`config/settings.go` declares every setting that can live in the database (`config.Settings()`, `LookupSetting`). Each `config.Setting` has:

- `Key`: the dotted name of `config.yaml` (`agents.reviews`), resolved to a field of `config.Config` by its `yaml` tags (`settingField`, `SettingValue`; a `*bool` reads as its documented default when nil, `pointerDefault`).
- `Kind` (`bool`, `string`, `int`, `enum` with `Enum`, `duration`, `strings`) and an optional `Check`: `DecodeSetting` checks a JSON value by them.
- `Class`: `read`, `change` or `risky` (the Reeve's classes, see Part 4 of the spec, and `docs/ai/reeve.md` once it exists).
- `Mode`: `live` (read on each use: `web.tint`, `updates.mode`, `burnlite.projects`), `new_agents`, or `restart` (wired into services at start). A setting is `live` only where it is already read on each use; nothing was rewired to make one live.
- `Secret`: the value is the name of a secret, never the secret.

`config.FileOnlySettings()` lists the file-only settings (`db`, `data_dir`, `secrets.master_key`, `secrets.onepassword`, `web.enabled`, `web.port`, `web.tls`, `web.public_url`, `approval_devices.android_app_digests`); `IsFileOnly` says whether a key is or is under one. A test fails if one is registered.

Every single value of `config.Config` is registered (a list of strings, such as `jira.project_keys`, is one value of kind `strings`). `TestEverySettingIsRegisteredOrFileOnly` (`config/settings_coverage_test.go`) walks `config.Config` by its `yaml` tags and fails for a field that is neither registered, file-only, nor a list of named entries (`config.EntryLists`: kept in tables of their own, see § Lists). `config.Config` gained `Web.Tint` and `Updates.Mode` for the two that were instance values only. Nil sections on a key's way (`integrations.argus`) are read as zero values and made when a value is set (`section`).

Modes are honest: `live` only for `web.tint`, `updates.mode` and `burnlite.projects`, which their services read on each use; everything else is `restart`, since it is read once when services are built. Classes: settings that touch security (tokens, the sandbox, credential channels, the GitHub sign-in, network announcement, updates) are `risky`; the rest `change`.

**Secrets.** A setting marked `Secret` (`jira.api_token`, `github.token`, `notifications.ntfy.token`, `manticore.api_key`) takes only a secret's name in the database: `legato://<name>` (Legato's sealed store) or `op://<vault>/<item>/<field>` (1Password); `DecodeSetting` refuses anything else. `ApplySettings` reads the secret through a `SecretResolver` (`settingSecrets` in `cmd/legato/database.go`: `configuredSecretStore` for `legato://`, `credential.OnePasswordStore` for `op://`) and sets the value in the process's configuration; a secret that cannot be read leaves the setting empty and is logged. `List` and `Get` show the name, and never a secret given in `config.yaml` (`(set in config.yaml)`). `${ENV}` references stay valid in the file, where they are pins.

## Layering

- **Pins.** `config.Load` records every dotted path the file gives a value (`pinnedKeys`: mappings walked, lists and scalars as leaves; `null` gives none). `Config.Pinned(key)` is true for that path or for any path above it that is a leaf (a list given whole). `PinnedKeys` lists the registered ones.
- **Overlay.** `config.Overlay(cfg, values)` applies JSON values by key to the settings the file does not pin, then runs `validate`: the same checks and normalization `Load` runs on a file (`Load` now calls it too). Its error is `Load`'s error for the same configuration.
- **Storage.** Migration 098 adds `settings` (`key` primary, `value` JSON, `updated_at`, `updated_by`) and moves the instance values `web.tint`, `updates.mode` and `burnlite.projects` into it, keeping their values. Store: `ListSettings`, `SettingValues`, `GetSetting`, `SetSetting`, `DeleteSetting`. A pinned value is never written here.
- **The effective configuration.** `service.ApplySettings(ctx, db, cfg, resolve)` overlays the database's values on a configuration loaded from the file, in place. `openStore` (`cmd/legato/database.go`) calls it, so the TUI, `legato serve` and every short-lived command that opens the store read the same effective configuration. When the overlay would be invalid (the file changed since a value was saved), it is skipped and logged: the file's configuration applies, and a database value never stops Legato from starting.

## The service

`service.SettingsService` (`internal/service/settings.go`):

- `Effective`: the file, then the database.
- `List`, `Get`: each setting with `value`, `source` (`default`, `config.yaml`, `database`), `pinned`, `updated_at`, `updated_by`.
- `Check`, `Set`: the value by its own rule (`DecodeSetting`), then refused when pinned (`ErrSettingPinned`), then the whole configuration (the file, every database value, the change) through `Overlay`. `Set` keeps the canonical JSON.
- `Reset`: deletes the database value (checked the same way).
- `RestoreSettings`: a `SettingsRestorer` for restart rollback.
- `StringSetting`: the `TintStore` shape (`GetInstanceValue`/`SetInstanceValue` by key) over the settings, which `TintService` and `UpdateService` now take instead of the instance values.

Burnlite's projects: `service.BurnliteProjects` and `SaveBurnliteProjects` read and write the `burnlite.projects` setting; the runtime passes the configured projects only when the file pins them (`pinnedBurnliteProjects`).

## Sources, the API and the CLI

- **Waiting for a restart.** Each process makes one `SettingsService` per store at start (`settingsFor` in `cmd/legato/settings_runtime.go`), which calls `MarkStarted`: the values it started with. A `restart`-mode setting whose effective value differs from that is `pending_restart` ("applies at restart"); `PendingRestart` lists them. `OnChange` hooks run after each `Set` or `Reset`; `wireSettings` publishes `EventSettingsChanged` on them.
- **Attention.** The `settings` producer (`SettingsAttentionAdapter`, signal `settings_restart`) raises one item, "Restart to apply N changes", while any wait, with **Restart Legato** (an `execute` action that runs `restartAll`) and **Open settings**. Its revision is the waiting keys, so a dismissed item comes back when another change waits.
- **API** (`internal/server/settings_registry.go`): `GET /api/config/settings` (`{settings: [SettingValue], file_only: [...]}`: each setting's `key`, `title`, `description`, `kind`, `enum`, `class`, `mode`, `secret`, `value`, `source`, `pinned`, `pending_restart`, `updated_at`, `updated_by`); `PUT /api/config/settings/{key}` (`{value}`; `200` with the new `SettingValue`; `404` not a setting, `409` pinned, `400` a refused value with its reason); `DELETE /api/config/settings/{key}` (reset). A change broadcasts `settings_changed`.
- **CLI.** `legato config show [--sources] [--json]` prints `key: value` for every setting; `--sources` adds `(default)`, `(config.yaml, pinned)` or `(set here)`.
