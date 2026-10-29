## Context

Phase 1 (`agent-approvals`) gives Legato durable approval requests, a resolver per source, the 60-second-friendly decision API, and every surface where the person answers. This phase adds the credential side.

Agent-side Legato processes (`legato mcp serve`, CLI commands in an agent's pane) open the database and read `config.yaml` directly. Until the sandbox of `sandboxed-cloud-agents` is on, an agent runs as the person's user and can read whatever Legato can. The broker therefore gives convenience, least-privilege defaults and a record in this phase, not containment.

Engine packages may not import each other, so a store that keeps data in the database cannot import `internal/engine/store`; it receives the persistence it needs as an interface.

## Goals / Non-Goals

**Goals:**
- No agent needs a pasted secret: it asks Legato, under the person's rules.
- One policy model for every credential: approval modes, scoped grants, limits.
- Any secret in any store, chosen by its reference.
- Every request, decision and use recorded, with tampering detectable.
- The same broker code runs in the host Legato and in agent-side processes, so a later broker socket can replace the direct path without changing callers.

**Non-Goals:**
- Keeping the secret from the agent. `http-proxy` and `ssh-agent` are phase 3.
- Short-lived issuers other than `static` (phase 4) and signed decisions (phase 5). `signed: true` is parsed; until phase 5 such rules are approved with a tap, and the card says the decision is unsigned.
- External plug-ins (phase 6).
- Putting secrets in an agent's environment at launch.

## Decisions

### D1. One engine package for the plug-in contracts and the built-ins

`internal/engine/credential` holds the types (`Secret`, `SecretType`, `Operation`, `Resource`, `Credential`), the interfaces (`Store`, `Issuer`, `Inspector`, `Channel`, with optional `Putter`, `Deleter`, `Signer`, `Revoker`), resource matching, the built-in stores and the `static` issuer. The Legato store's rows come through a `SecretRecords` interface that `store.Store` implements.

*Alternative:* one engine package per store. Rejected for now: each would need its own copy of the types, because engine packages cannot share one.

### D2. Secret references pick the store

| Reference | Store | How it reads |
| --- | --- | --- |
| `legato://name` | Legato's own | the `secrets` table, decrypted |
| `op://vault/item/field` | 1Password | `op read <ref>` with `OP_SERVICE_ACCOUNT_TOKEN` from `secrets.onepassword.token` |
| `systemd-creds://name` | systemd | `$CREDENTIALS_DIRECTORY/name` (a unit's `LoadCredentialEncrypted=`) |
| `keychain://service/account` | macOS Keychain | `security find-generic-password -s service -a account -w` |
| `env://NAME` | environment | the variable of the Legato process |
| `file:///abs/path` | file | the file's content, refused when other users can read it |

Values are read when used and are not cached beyond one operation.

### D3. Legato's own store

A row of `secrets` holds `name`, `type`, `nonce`, `ciphertext` and timestamps. Each value is sealed with AES-256-GCM under a key derived with HKDF-SHA256 from the master key, with the name and type as additional data, so a row moved to another name does not open.

The master key is 32 random bytes. `secrets.master_key: auto` takes the first that works:

1. `systemd-creds`: the key sealed to `<data_dir>/secrets.key.cred` with `systemd-creds encrypt --user` (TPM-bound where available).
2. Secret Service: `secret-tool` with attributes `service legato`, `key master`.
3. macOS Keychain: service `legato`, account `master-key`.
4. A passphrase in `LEGATO_SECRETS_PASSPHRASE`, stretched with PBKDF2-SHA256 (600,000 rounds) and a salt in `<data_dir>/secrets.salt`.
5. A key file `<data_dir>/secrets.key`, mode 0600, as today's auth token is kept.

The source that made the key is recorded in `<data_dir>/secrets.source`, so a later start uses the same one. `legato secret list` names the source.

### D4. Credentials and profiles

```yaml
credentials:
  - name: elevenlabs
    secret: legato://elevenlabs-key
    profile: secret            # default
    policy:
      - { operation: read, approve: session }
```

A profile sets `issuer`, `channels`, `accepts` (secret types) and default `policy`. Shipped profiles are embedded YAML; profiles in `<config dir>/legato/profiles/` and `<project>/.legato/profiles/` replace them by name. Phase 2 ships `secret`: issuer `static`, channel `helper` (`legato credential get`), default rule `read` with `session`.

### D5. Rules and approval modes

A rule has `operation` (a name or list; absent means any), `resource` (a glob; absent means any), `approve`, and optional `window`, `limit` and `signed`. The rule that applies is the matching one with the highest specificity: one point for naming operations, plus the number of literal characters in its resource pattern; the earlier rule wins a tie. Anything no rule matches is refused.

| Mode | Asks | Grant lasts |
| --- | --- | --- |
| `auto` | never | until the agent ends |
| `session` | once | until the agent ends |
| `window` | once | the rule's window |
| `request` | each time | one use |
| `deny` | never; refused | — |

A requested resource may itself be a pattern. A rule covers it when the rule's pattern matches the requested pattern as a string. A request with a pattern is marked broader.

The approval card offers the rule's mode and every stricter one (`session` → session, window of 30 minutes, once; `window` → window, once; `request` → once).

### D6. Requests, grants and uses

- `Request(ctx, AccessRequest)` checks the rules. `auto` grants at once. `deny` refuses. Otherwise it submits an approval (source `credential`) and waits up to 60 seconds for a decision by reading the approval, then returns `granted`, `denied` or `pending`.
- `Authorize(ctx, Use)` looks for a live grant of the execution that covers the use. A use with no grant is a request for exactly that use.
- A grant has an execution, a credential, operations, resources, a mode, an expiry and uses left. It ends at its expiry, at its last use, or when its execution is terminal.
- Limits count the uses recorded in the audit log for the rule's window (`requests: 100/day`). Past a limit the use is a request.

The decision is made in the host process, which runs the `credential` resolver: it writes the grant and sends `[legato approvals] <id> approved: <scope> for <grant>`, or the denial with the person's note, with `QueueMessage`. An agent-side process that waited reads the grant from the database.

### D7. What the agent sees

- MCP `request_access {credential, operations, resources, reason, blocking}` returns text: granted (and how to use it), pending ("Legato will tell you the decision; if you are not blocked, continue with other work; if you are blocked, stop and end your turn"), or denied with the note.
- `legato credential get <name> [--operation read] [--resource …]` prints the value after `Authorize`; it waits as `Request` does and exits 4 when pending or denied, with the same text on stderr.
- `legato credential exec <name> --env VAR -- <command>` runs one command with the value in one environment variable, so the value never passes through the agent's own output.
- `legato credential request` mirrors the tool; `legato credential list` shows names, profiles, stores and rules, never values.
- The guidance (`MCPGuidance`) tells agents to ask before they need a credential and to ask for the smallest scope.

### D8. The audit log

`credential_audit` rows: `seq`, `at`, `event` (`request`, `pending`, `grant`, `deny`, `use`, `refuse`, `issue`, `expire`), `execution_id`, `task_id`, `agent_kind`, `workspace_id`, `credential`, `operation`, `resource`, `amount`, `approval_id`, `decided_by`, `detail`, `prev_hash`, `hash`. `hash` is the SHA-256 of `prev_hash` and the row's canonical JSON. A row is appended in an immediate transaction that reads the last hash first, so writers from several processes keep one chain. A secret value is never recorded; an issued token would be recorded by its SHA-256 hash.

### D9. One broker for every process

`service.CredentialBroker` is built in the TUI, in `legato serve`, in `legato mcp serve` and in `legato credential`. The host additionally registers the resolver and ends grants on `EventAgentDied`. When the broker socket of `sandboxed-cloud-agents` exists, agent-side processes will call the host through it with the same request types.

## Risks / Trade-offs

- [An unsandboxed agent can read the master key source and the database] → Stated in the docs and in `legato secret list`; the sandbox is the fix.
- [An agent can ignore "stop and wait"] → The broker still refuses uses without a grant.
- [A rule typo grants too much] → Default deny, the specificity rule, and `legato credential list` showing the rule that applies to an example resource (`--explain`).
- [Several processes append to the audit chain] → Immediate transactions serialise the append.
- [`op` or `security` print secrets on stdout] → Read through a pipe, never logged, never on the command line.

## Migration Plan

Migrations 070 (`secrets`), 071 (`credential_grants`) and 072 (`credential_audit`). No data moves. Without `credentials` in config, nothing runs.

## Open Questions

- Should a credential be able to put its value in an agent's environment at launch, for tools that read only an environment variable? It skips per-use checks, so it is left out of this phase.
