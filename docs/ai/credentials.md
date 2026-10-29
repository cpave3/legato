# Credentials

Legato holds credentials for agents, so no agent needs a secret pasted into its prompt, its environment or a file. An agent asks for a scope of a credential; the person's rules decide whether that happens at once, after one approval, or never; each use is checked against what was granted; and every request, decision and use is recorded. Approvals go through the approval service (see [Approvals](approvals.md)). The design is in `openspec/changes/credential-broker`; later phases (channels that keep the secret from the agent, short-lived issuers, signed decisions, external plug-ins) are in `credential-channels`, `github-credentials`, `signed-approvals` and `credential-plugins`.

Until the agent sandbox of `sandboxed-cloud-agents` is on, an agent runs as the person's user and can read what Legato reads, the store's master key included. The broker gives least-privilege defaults and a record, not containment.

## Configuring

```yaml
secrets:
  master_key: auto                 # or systemd-creds, secret-service, keychain, passphrase, file
  onepassword:
    token: ${OP_SERVICE_ACCOUNT_TOKEN}

credentials:
  - name: elevenlabs
    secret: legato://elevenlabs-key   # legato secret add elevenlabs-key < key.txt
    policy:
      - { operation: read, approve: auto, limit: { requests: 200/day } }
      - { operation: read, approve: window, window: 30m }   # past the limit
  - name: homelab
    secret: op://Legato/homelab/private key
    profile: secret
```

- `name` is unique, ignoring case. `secret` is a reference whose scheme picks the store (below). `profile` defaults to `secret`. `type` names the secret's type when the profile takes several.
- `policy` replaces the profile's default rules. Each rule has `operation` (a name or a list; absent means any), `resource` (a glob, `**` across `/`; absent means any), `approve`, and optional `window`, `limit` and `signed`.
- Config loading checks the structure (names, schemes, modes, windows, limits). The broker checks profiles and secret types when it starts; a host Legato that finds a wrong credential logs it and refuses credential requests until it is fixed.

## Rules

For each operation and resource, the rule that applies is the matching one with the highest specificity: one point for naming operations, plus the number of literal characters in its resource pattern; the earlier rule wins a tie. A rule whose limit is spent is passed over, so the next rule decides (above: automatic up to 200 uses a day, then asked once per 30 minutes). Anything no rule matches is refused.

| `approve` | The person is asked | A grant lasts |
| --- | --- | --- |
| `auto` | never | until the agent ends |
| `session` | once | until the agent ends |
| `window` | once | the rule's `window` |
| `request` | for each use | one use |
| `deny` | never; refused | — |

The approval offers the rule's mode and every stricter one: a `session` rule offers **For this session**, **For 30 minutes** (or the rule's window) and **Once**. `limit` counts `use` rows of the audit log per credential: `requests: N/minute|hour|day|week`; other meters come with the channels that measure them. `signed: true` is accepted and shown on the card; until signed decisions exist (phase 5), such a decision is still one tap.

## Asking and using

- **`request_access`** (MCP, offered when credentials are configured; its description names them) and **`legato credential request <name> --operation … [--resource …] [--reason …] [--blocking]`** ask for a scope. A request that a live grant already covers is granted; `auto` grants at once; `deny` and no rule refuse; otherwise an approval is submitted (source `credential`, deduplicated by scope while pending) and the call waits up to 60 seconds. It returns `granted`, `denied` with the person's note, or `pending` with the approval ID and the instruction to carry on with other work, or stop and end the turn when blocked.
- **When the person decides later**, the host Legato tells the agent: `[legato approvals] ap-… approved: read with elevenlabs for this session…`, or the denial with the note and "do not look for another way". A decision made while the agent still waited is not messaged.
- **`legato credential exec <name> --env VAR -- <command>`** runs one command with the value in `VAR`, so it never passes through the agent's own output. **`legato credential get <name>`** prints it. Both check the use first (`--operation`, default `read`; `--resource`): a use no grant covers becomes a request for exactly that use and waits as above. They exit `4` when the use is not allowed, `5` outside an agent session (`LEGATO_TASK_ID` and `LEGATO_EXECUTION_ID` name the session).
- A grant covers what was asked for and approved, for one execution. It ends at its window, after its one use, when its execution ends (`EventAgentDied`), or when the person ends it (Settings › Credentials, `DELETE /api/credentials/grants/{id}`).
- A resource that is itself a pattern (`…/refs/heads/legato/attention-*`) is covered by a rule whose pattern matches it as a string, and the card marks it **Broader scope**.

## Stores

| Reference | Store |
| --- | --- |
| `legato://name` | Legato's own store: `secrets` table, sealed |
| `op://vault/item/field` | 1Password, through `op read` as the service account in `secrets.onepassword.token` |
| `systemd-creds://name` | `$CREDENTIALS_DIRECTORY/name`, a systemd unit's `LoadCredentialEncrypted=` |
| `keychain://service/account` | the macOS Keychain, through `security` |
| `env://NAME` | the Legato process's environment |
| `file:///path` | a file, refused when other users can read it |

Values are read when used, never cached, logged or put in an error. What `op` or `security` print is never repeated.

## Legato's own store

`legato secret add <name> [--type value|userpass|keypair|oauth-client|oauth-refresh] [--from <file>]` (the value from stdin otherwise), `legato secret list` (names, types, dates and the master key's source; never values), `legato secret remove <name>`.

- Each value is sealed with AES-256-GCM under a key derived with HKDF-SHA256 from the master key for its name and type, which are also its additional data: a value moved to another name, or read as another type, does not open.
- The master key is 32 random bytes made on first use. `auto` takes the first source that works: `systemd-creds` (sealed to `<data_dir>/secrets.key.cred`, Linux), the Secret Service (`secret-tool`, attributes `service legato key master`), the Keychain (macOS, service `legato`, account `master-key`), a passphrase in `LEGATO_SECRETS_PASSPHRASE` (PBKDF2-SHA256, 600,000 rounds, salt in `<data_dir>/secrets.salt`), and a key file `<data_dir>/secrets.key` (mode 0600).
- `<data_dir>/secrets.source` records the source and a value sealed under the key. Later starts use that source and no other, and refuse a key that does not open the value (a wrong passphrase).

## Profiles

A profile is YAML: `name`, `description`, `issuer` (`static` in this phase), `channels`, `accepts` (secret types) and default `policy`. Legato ships `secret` (a value handed to the agent on request; default rule `read: session`). Profiles in `<config dir>/profiles/*.yaml` add to or replace the shipped ones by name. Shipped profiles are in `internal/service/credential_profiles/`.

## Audit log

`credential_audit` (migration 070) is append-only. Events: `request`, `pending`, `grant`, `deny`, `use`, `refuse`, `revoke`. Each row records the time, execution, task, agent tool, workspace, credential, operation, resource, amount, approval and who decided; never a value. Each row holds the previous row's hash and its own, the SHA-256 of the previous hash and the row's canonical JSON, appended inside a transaction that takes SQLite's write lock first, so processes appending together keep one chain. The time is stored as text so it reads back exactly as it was hashed.

`legato audit [--credential] [--task] [--execution] [--limit]` prints rows newest first; `legato audit verify` names the first row whose hashes do not match. The web UI reads `GET /api/credentials/audit`.

## Code

- `internal/engine/credential`: secret types and references, the stores, sealing, master key sources (`LoadMasterKey`, `RecordedSource`), resource matching (`Covers`, `Specificity`, `IsPattern`), the plug-in interfaces (`Store`, `Issuer`, `Inspector`, `Channel`, with `Putter`, `Deleter`, `Signer`, `Revoker`) and `StaticIssuer`.
- `internal/engine/store/credential.go`: `secrets`, `credential_grants`, `credential_audit`.
- `internal/service/credential_broker.go`: `CredentialBroker` (`Request`, `Authorize`, `Value`, `Grants`, `EndGrant`, `Audit`, the `credential` resolver); `credential_secrets.go`: `SecretStoreService`; `credential_profiles.go`.
- `internal/cli/mcp_credentials.go`: `request_access` and `AccessText`.
- `cmd/legato/credential_runtime.go`, `credential_contract.go`: wiring and the `secret`, `credential` and `audit` commands. Host processes (TUI, `legato serve`) build the broker with the resolver and a messenger; `legato mcp serve` and the CLI build it without, so their approvals are decided by a host.
- `internal/server/credentials.go`: `GET /api/credentials`, `GET /api/credentials/grants`, `DELETE /api/credentials/grants/{id}`, `GET /api/credentials/audit`.
- `web/src/components/CredentialsSettings.tsx` (Settings › Credentials) and the credential card of `ApprovalTray`.
