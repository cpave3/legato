## Context

This is phase 6 of the agent approvals and credentials plan (`.legato/plans/agent-approvals-credentials/plan.md`). It needs `credential-broker` (phase 2), which defines the Go interfaces in `internal/engine/credential`:

- `Store` with `Get`, and optional `Putter`, `Deleter` and `Signer`;
- `Issuer` with `Accepts` and `Issue`, and optional `Revoker`;
- `Inspector` with `Classify`, which returns an operation, a resource and amounts, and optional `Holder` and `Deferrer`;
- `Channel` with `Open(ctx, Execution, Broker)` (returns the environment for the agent) and `Close`;
- the policy: rules with approval modes, limits and `signed`.

A secret reference's scheme selects its store (`legato://`, `op://`, `systemd-creds://`, `keychain://`, `env://`, `file://`). Secrets are typed (`value`, `userpass`, `keypair`, `oauth-client`, `oauth-refresh`).

Legato is built without cgo (`modernc.org/sqlite`), so a PKCS#11 module cannot be loaded into the `legato` process.

## Goals / Non-Goals

**Goals:**
- Any of the five kinds can be a separate program, in any language, without a change to Legato.
- Only the person can enable a plug-in, and a changed binary is not run.
- Reuse the credential protocols that tools already speak.
- A plug-in that fails never widens access.

**Non-Goals:**
- Loading Go plug-ins (`plugin` package) or WebAssembly modules.
- A registry or download of plug-ins. The person installs the program.
- Sandboxing plug-in processes. A plug-in runs with Legato's rights; the person trusts it as they trust Legato.

## Decisions

### D1. JSON-RPC 2.0 over standard input and output, one object per line

Legato starts the program with no arguments, a minimal environment (`PATH`, `HOME`, `LANG`, `XDG_RUNTIME_DIR`, and nothing named `LEGATO_*`), and the working directory `<data_dir>/plugins/<name>/`. Each message is one JSON-RPC 2.0 object on one line. Both sides send requests: Legato calls the plug-in's methods, and the plug-in calls `broker.*` methods. Each side numbers its own request IDs. Standard error is the plug-in's log, kept in `<data_dir>/plugins/<name>/stderr.log` with a size limit.

*Alternatives:* LSP-style `Content-Length` framing; gRPC (as HashiCorp's go-plugin). Rejected: newline-delimited JSON is what Legato's IPC and MCP stdio already use, needs no library in the plug-in, and is easy to test by hand.

### D2. `describe` and `configure`

The first call is `describe`:

```json
{"name":"vault","version":"1.2.0","protocol":1,
 "kinds":["store","issuer"],
 "capabilities":{"store":{"schemes":["vault"],"put":true,"delete":false,"sign":false},"issuer":{"revoke":true}},
 "settings_schema":{"type":"object","properties":{"address":{"type":"string"}},"required":["address"]}}
```

Legato refuses a plug-in whose `protocol` it does not support, whose `name` differs from its entry, or whose settings fail the schema. It then calls `configure {settings}`. A store plug-in claims reference schemes; a scheme that a built-in store or another plug-in already claims is a configuration error.

### D3. Methods mirror the Go interfaces

| Kind | Methods (Legato → plug-in) |
| --- | --- |
| store | `store.get {ref}` → typed secret; `store.put {ref, secret}`; `store.delete {ref}`; `store.sign {ref, algorithm, data}` → signature |
| issuer | `issuer.accepts {profile, secret_type}` → bool; `issuer.issue {grant, secret?}` → `{credential, expires_at}`; `issuer.revoke {credential}` |
| inspector | `inspector.classify {request}` → `{operation, resource, amounts}` |
| channel | `channel.open {execution}` → `{env}`; `channel.close {execution}` |
| policy | `policy.evaluate {use, rule}` → `{decision, reason}` |

Binary values are base64. Each plug-in kind is adapted to the matching Go interface inside Legato (`pluginStore`, `pluginIssuer` and so on), so the broker does not know whether a kind is built in or external. Optional interfaces (`Signer`, `Revoker`) are present only when `describe` lists the capability.

An issuer plug-in receives the secret only when it does not ask for signing; one that signs (a JWT, a certificate) calls `broker.sign`, so a key in a signing store never reaches the plug-in.

### D4. Callbacks from the plug-in

| Method (plug-in → Legato) | Who may call it |
| --- | --- |
| `broker.authorize {execution, operation, resource, amounts}` → `{granted, approval?}` | channel plug-ins, for an execution they opened |
| `broker.credential {execution, grant}` → credential | channel plug-ins, for a grant from `authorize` |
| `broker.sign {credential, data}` → signature | issuer and channel plug-ins, for credentials whose profile uses them |

Legato checks each callback against the plug-in's kinds and the executions it opened. `broker.authorize` waits as `CredentialBroker.Authorize` does (up to 60 seconds); the plug-in must not time out first. Callbacks are recorded in the audit log under the plug-in's name.

### D5. A policy plug-in can only make a decision stricter

Legato evaluates the person's rules first. For a use under a rule whose profile names a policy plug-in, it calls `policy.evaluate`. The answer is `keep`, `deny`, `request` or `signed`. Legato applies the stricter of the rule's mode and the answer (`deny` > `request` with `signed` > `request` > `window` > `session` > `auto`). An answer that would loosen the rule is treated as `keep` and logged.

The plan states that the person can never approve more than a rule allows. A plug-in is no exception.

### D6. Enablement by path and checksum

```yaml
credential_plugins:
  - name: vault
    path: /usr/local/bin/legato-plugin-vault
    sha256: 9f2c…e01a
    settings: { address: https://vault.lan:8200 }
```

- `path` is absolute and names a regular file, not a symbolic link, whose base name is `legato-plugin-<name>`.
- Before each start Legato hashes the file. A mismatch disables the plug-in, raises an Attention item, and leaves every credential that needs it unavailable.
- `credential_plugins` is part of the credentials configuration. A new entry, or a changed `path`, `sha256` or `settings`, takes effect only after the person approves the change; once a device is enrolled the approval needs a signature (`signed-approvals`). Until then Legato runs the last approved entries.
- `legato credential plugin hash <path>` prints the checksum for the person to paste.

*Alternative:* a directory of plug-ins found by name. Rejected: an agent that can write to that directory could add one.

### D7. A long-running process with bounded calls

A plug-in starts at its first use and keeps running. Calls have a 10-second limit, except `broker.authorize`, which waits as the broker does. A plug-in that exits or exceeds a limit is restarted with backoff (1 s, doubling, at most 1 minute). While it is down, its stores return an error, its issuers issue nothing, its inspectors refuse, and its policies answer `deny`. Legato closes the plug-in's input on shutdown and kills it after 5 seconds.

### D8. Built-in adapters for existing helpers

- **`docker-credential://<helper>/<server URL>`**: runs `docker-credential-<helper> get` with the server URL on input and reads `{Username, Secret}` as a `userpass` secret; `store` and `erase` back `put` and `delete`.
- **`git-credential://<helper>/<host>/<path>`**: runs `git credential-<helper> get` with `protocol`, `host` and `path` on input and reads `username` and `password`; `store` and `erase` back `put` and `delete`.
- **`secret-service://<collection>?<attribute>=<value>…`**: looks up an item by attributes through D-Bus (`org.freedesktop.secrets`), opening a session with DH encryption where the service offers it; `put` creates or replaces the item.

The helper's name is checked against `[a-z0-9-]+`, and the helper is found on the configured `PATH` of Legato's process. These are built in because they need no cgo and no long-running process.

### D9. PKCS#11 as an external plug-in

`legato-plugin-pkcs11` is a separate binary built with cgo and released beside `legato`. It is a signing store for `pkcs11:` URIs (RFC 7512, for example `pkcs11:token=YubiKey;object=legato-ca;type=private`). Its settings name the module (`/usr/lib/libykcs11.so`) and the PIN reference (a reference into another store, never the PIN itself). `store.get` returns the public key only; `store.sign` signs on the token. This keeps cgo out of `legato` and uses the same protocol as any other plug-in, which tests the protocol.

## Risks / Trade-offs

- [A plug-in is malicious or buggy] → The person enabled it by checksum; policies can only tighten; failures deny. Docs say a plug-in runs with Legato's rights.
- [An agent replaces the plug-in binary] → The checksum stops it at the next start. A running process keeps running until restart; the hash is also checked at each restart.
- [An agent edits `credential_plugins`] → Changes need the person's approval and, with a device, a signature.
- [A slow plug-in stalls credential use] → Call limits, restart with backoff, and failure closed.
- [Secret Service locked collections prompt the desktop] → The adapter reports a locked collection as an error and does not unlock it.
- [Protocol drift] → `protocol` is versioned; Legato supports the versions it lists and refuses others.

## Migration Plan

- Additive. No `credential_plugins` entry and no new reference scheme means no process and no behaviour change.
- Migration: none for the protocol; the approved credentials configuration from `credential-broker` gains the plug-in entries.
- Rollback: remove the entry; credentials that need it become unavailable and say so.

## Open Questions

- Should a plug-in be able to serve several executions' channels in one process, or should Legato start one process for each execution for channels?
- Should Legato pin a plug-in's `describe` output (kinds and capabilities) with its checksum, so an update that adds a kind needs approval?
- Should the Secret Service adapter also serve as the store of the Legato store's master key, which `credential-broker` lists as a Linux option?
- How should `legato-plugin-pkcs11` be packaged for macOS, where the PKCS#11 module paths differ?
