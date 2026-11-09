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
- **Overlay.** `config.Overlay(cfg, values)` applies JSON values by key to the settings the file does not pin, then runs `validate`: the same checks and normalization `Load` runs on a file (`Load` now calls it too). Its error is `Load`'s error for the same configuration. `config.OverlayAll(cfg, values, entries)` applies the values and the list entries first and validates once, so a value may name an entry kept in the database (`default_workspace` a workspace added there); `ApplySettings`, `Effective`, `Check` and `Reset` use it.
- **Storage.** Migration 098 adds `settings` (`key` primary, `value` JSON, `updated_at`, `updated_by`) and moves the instance values `web.tint`, `updates.mode` and `burnlite.projects` into it, keeping their values. Store: `ListSettings`, `SettingValues`, `GetSetting`, `SetSetting`, `DeleteSetting`. A pinned value is never written here.
- **The effective configuration.** `service.ApplySettings(ctx, db, cfg, resolve)` overlays the database's values on a configuration loaded from the file, in place. `openStore` (`cmd/legato/database.go`) calls it, so the TUI, `legato serve` and every short-lived command that opens the store read the same effective configuration. When the overlay would be invalid (the file changed since a value was saved), it is skipped and logged: the file's configuration applies, and a database value never stops Legato from starting.

## The service

`service.SettingsService` (`internal/service/settings.go`):

- `Effective`: the file, then the database.
- `List`, `Get`: each setting with `value`, `source` (`default`, `config.yaml`, `database`), `pinned`, `updated_at`, `updated_by`.
- `Check`, `Set`: the value by its own rule (`DecodeSetting`), then refused when pinned (`ErrSettingPinned`), then the whole configuration (the file, every database value and list entry, the change) through `OverlayAll`. `Set` keeps the canonical JSON.
- `Reset`: deletes the database value (checked the same way).
- `RestoreSettings`: a `SettingsRestorer` for restart rollback.
- `StringSetting`: the `TintStore` shape (`GetInstanceValue`/`SetInstanceValue` by key) over the settings, which `TintService` and `UpdateService` now take instead of the instance values.

Burnlite's projects: `service.BurnliteProjects` and `SaveBurnliteProjects` read and write the `burnlite.projects` setting; the runtime passes the configured projects only when the file pins them (`pinnedBurnliteProjects`).

## Sources, the API and the CLI

- **Waiting for a restart.** Each process makes one `SettingsService` per store at start (`settingsFor` in `cmd/legato/settings_runtime.go`), which calls `MarkStarted`: the values it started with. A `restart`-mode setting whose effective value differs from that is `pending_restart` ("applies at restart"); `PendingRestart` lists them. `OnChange` hooks run after each `Set` or `Reset`; `wireSettings` publishes `EventSettingsChanged` on them.
- **Attention.** The `settings` producer (`SettingsAttentionAdapter`, signal `settings_restart`) raises one item, "Restart to apply N changes", while any wait, with **Restart Legato** (an `execute` action that runs `restartAll`) and **Open settings**. Its revision is the waiting keys, so a dismissed item comes back when another change waits.
- **API** (`internal/server/settings_registry.go`): `GET /api/config/settings` (`{settings: [SettingValue], file_only: [...]}`: each setting's `key`, `title`, `description`, `kind`, `enum`, `class`, `mode`, `secret`, `value`, `source`, `pinned`, `pending_restart`, `updated_at`, `updated_by`); `PUT /api/config/settings/{key}` (`{value}`; `200` with the new `SettingValue`; `404` not a setting, `409` pinned, `400` a refused value with its reason); `DELETE /api/config/settings/{key}` (reset). A change broadcasts `settings_changed`.
- **CLI.** `legato config show [--sources] [--json]` prints `key: value` for every setting; `--sources` adds `(default)`, `(config.yaml, pinned)` or `(set here)`.

## Lists

A list of named entries is the file's entries, each pinned and matched by name ignoring case, plus the database's. Saving an entry whose name the file has is refused (`config.ErrEntryPinned`).

- **Lists kept in `setting_entries`** (migration 099: `list`, `name`, `value` JSON in `config.yaml`'s shape, `updated_at`, `updated_by`; key `(list, name)`): `config.EntryListDefs()` — `workspaces`, `macros`, `adapters` (a map: its keys name entries), `credentials`, `credential_sources` (named by `vault`), `agents.tmux_options` (map), `agents.sandbox.mounts` (named by `path`), `swarm.prompts` (map). One table holds them all, keyed by list, rather than a table each: the entries are configuration in `config.yaml`'s shape, and one table keeps the overlay generic. Classes: credentials, credential sources and sandbox mounts are `risky`; the rest `change`. Modes: `restart`, except `agents.tmux_options` (`new_agents`).
- **Lists with tables of their own** keep them and the same rule (`config.EntryLists` lists all): repository presets (`repository_presets`, refusing a name of `config.yaml`'s `repositories`), repository guidance (`repository_guidance`, refusing a configured repository), resources (`resources`, refusing a configured name), directory presets (a configured path is answered with the configured preset), and the board's columns (`column_mappings`, made from `board.columns` by `legato columns sync`).
- **Config.** `Load` records each entry list's names as the file gives them (`recordFileEntries`; `Config.PinnedEntry`, `FileEntries`). `config.OverlayEntries` adds the database's entries (decoded from JSON by their `yaml` tags; the name set from the key) and runs `validate`. `EntryValue` reads one entry back in its YAML shape.
- **Service.** `ApplySettings` and `Effective` overlay the values, then the entries. `SettingsService.Lists` (each list with its entries and their `source`; a secret written in the file is shown as `(set in config.yaml)`), `SetEntry` and `CheckEntry` (refused when pinned, or when the whole configuration would be invalid), `DeleteEntry`.
- **API.** `GET /api/config/lists`; `PUT /api/config/lists/{list}/{name}` (`{value}`; `409` for a name the file has, `400` with the reason for a refused entry); `DELETE /api/config/lists/{list}/{name}` (`404` for none). Each change broadcasts `settings_changed`.
- **Web.** Settings › Lists (`components/settings/ListSettings.tsx`): each list as a card, the file's entries with a lock, the database's with **Edit** and **Remove**, and **Add**, which takes a name and the entry's fields as JSON in `config.yaml`'s shape.

## Writing config.yaml, and the move

- **The writer** (`config/writer.go`). `EditFile(path, edit)` parses the file into `yaml.v3` nodes, applies `edit`, and writes it back atomically (a temporary file renamed over it), after copying the old file to `config.yaml.<UTC time>.bak` (`BackupPath`). Comments, order and every value not edited stay as they were; an `${ENV}` reference stays one. Helpers: `RemoveKey` (and the sections it leaves empty), `SetKey` (keeping the key's place and comments), `RemoveListEntry` (by name field, or a map key). `EditBytes` does the same on bytes, for a check or a diff before writing.
- **What can move** (`service.SettingsMover.Movable`): every registered setting the file pins, except a secret setting and one whose value in the file reads an environment variable (moving it would freeze the variable's value); and every entry the file gives of an entry list, except a list that reads a variable.
- **A move** (`SettingsMover.Move`): refused while a restart is pending (`ErrRestartPending`). It copies each value into `settings` and each entry into `setting_entries`, keeping the old database values (an entry the file shadowed included, so a rollback restores it rather than deleting it); rewrites `config.yaml` without them (with its backup); and records the restart (`RestartRollback.Begin`) with the backup and the old values (`RestartSetting.List` set for an entry). The caller restarts: a start that fails puts back both the file and the database (`SettingsService.RestoreSettings`, which the runtime's rollback now gets).
- **The offer.** The `settings-move` Attention producer (`SettingsMoveAttentionAdapter`, signal `settings_move`) raises one item while anything can move, "Move N settings from config.yaml into the database", navigating to Settings › Lists › `#move`. Its revision never changes (`offer-1`), so once dismissed it stays dismissed.
- **API.** `GET /api/config/move` (`{settings, entries: [{list, name}]}`); `POST /api/config/move` with the same shape (`202 {restart}`, then `restartAll`; `409` while a restart is pending; `400` with the reason for anything that cannot move).
- **Web.** `MoveSettings.tsx`: every movable setting and entry, ticked; untick to keep pinned; **Move N to the database and restart**. Each pinned, non-secret registry row has **Move to the database (restarts Legato)**.

## Fresh installs

- `config.WriteFirstConfig(path)` writes, when there is no file, one that sets nothing: comments pointing to Settings and `legato config show --sources`, and the file-only settings commented out. `loadConfig` (TUI and `legato serve`) calls it before loading. A file already there is never touched; one written by provisioning pins what it holds.
- The setup wizard (`internal/setup`) with `WithSettings(sink)` saves Jira's settings in the database (`jira.base_url`, `jira.email`, `jira.project_keys`) and the token in Legato's secret store as `jira-api-token`, naming it in `jira.api_token`. The columns were always the database's (`column_mappings`). The TUI re-applies the settings after the wizard, so its sync starts with them.

## Export

`SettingsService.Export(ctx, pinned)` writes the settings set in the database (single values by key, list entries under their list, through `config.SetKey` on a new document) as YAML shaped like `config.yaml`, with a header comment. With `pinned`, the settings and entries `config.yaml` pins are included. Secrets are names: a database secret setting is a name already; a pinned one is written only when the file's value is a `legato://` or `op://` reference (read from the file itself, since the listing hides a pinned secret), and a credential whose secret is not a reference is left out. The export loads as a `config.yaml`. `legato config export [--pinned]` prints it; `GET /api/config/export[?pinned=1]` serves it as `legato-settings.yaml`; Settings › Lists › **Export settings** (`ExportSettings.tsx`, with **Include settings from config.yaml**) downloads it. 

## Import

`ChangeSetService.ProposeImport(ctx, data, by)` loads a file shaped like `config.yaml` (`config.LoadBytes`; one that does not load is refused) and turns it into one change set, proposed without waiting: a `PUT /api/config/settings/{key}` for each registered setting the file gives (`Config.PinnedSettingsAndOthers`), and a `PUT /api/config/lists/{list}/{name}` for each list entry. File-only keys are left out and listed (`skipped`). The set has no Reeve session (`task_id` `""`), so its results are not messaged; the person sees it in the tray as a change set at its strictest call's level, and the broker's process runs it once approved. `legato config import <file> [--json]` proposes it (and broadcasts `approval_changed`); `POST /api/config/import` (the file as the body; `202` with `{id, approval_id, level, calls, skipped}`) serves Settings › Lists › **Import settings…**.
