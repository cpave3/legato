## Why

An agent that needs an API key today gets the key itself: pasted into its prompt, its environment or a file. The key then works for anything, from anywhere, for as long as it lives, and nothing records what the agent did with it. SSH keys reach agents through the 1Password SSH agent, whose prompts a person must approve at the desk.

This is phase 2 of the agent approvals and credentials plan. It adds the core of a credential broker: where secrets live, the rules that decide how often the person is asked, the grants an agent holds, and a record of every use. Later phases add channels that keep the secret from the agent (an HTTP proxy, an SSH agent), short-lived issuers (GitHub Apps, OAuth 2, SSH certificates), signed decisions and external plug-ins.

## What Changes

- **Five plug-in kinds** in a new engine package `internal/engine/credential`: stores, issuers, channels, inspectors, and the policy that uses them. Phase 2 ships stores and the `static` issuer; the other kinds have their interfaces now and implementations later.
- **Typed secrets.** A secret is `value`, `userpass`, `keypair`, `oauth-client` or `oauth-refresh`. Each plug-in declares the types it accepts, and Legato checks a credential's wiring when it loads the configuration.
- **Stores.** Legato's own encrypted store (`legato://name`), a 1Password service account through the `op` command (`op://vault/item/field`), systemd credentials (`systemd-creds://name`), the macOS Keychain (`keychain://service/account`), an environment variable (`env://NAME`) and a file (`file:///path`).
- **Legato's own store.** `legato secret add|list|remove`. Values are encrypted with AES-256-GCM under a master key from `systemd-creds`, the Secret Service, the Keychain, a passphrase, or a key file, in that order of preference.
- **Credentials and profiles in config.** `credentials:` names each credential, its secret reference, its profile and its rules. A profile is YAML that sets the issuer, channels and default rules. Phase 2 ships one profile, `secret`, for values handed to the agent on request.
- **Rules and approval modes.** Each rule matches operations and resources and says `auto`, `session`, `window`, `request` or `deny`, with optional limits. The most specific matching rule applies; anything no rule matches is refused.
- **Scoped grants.** An agent asks for a scope with `request_access`. A grant covers that scope and nothing more, and each use is checked against it. A grant ends with its window, its last use or its agent.
- **Waiting.** `request_access` and `legato credential get` wait up to 60 seconds for the person. After that they return `pending` with the approval ID. The decision reaches the agent as a `[legato approvals]` message.
- **Audit log.** `credential_audit` records every request, decision, grant, use and refusal with full attribution, each row holding the hash of the one before. `legato audit` reads it and `legato audit verify` checks the chain.
- **Agent-facing tools.** The MCP tool `request_access`, and `legato credential request|get|list`. The agent guidance says to ask for the smallest scope, to carry on with other work while a request is pending, and to stop when blocked.

## Capabilities

### New Capabilities
- `credential-broker`: Credentials, profiles, rules and approval modes, scoped grants, `request_access`, `legato credential`, and the 60-second wait.
- `secret-stores`: Secret references, the built-in stores, typed secrets, and Legato's own encrypted store with its master key.
- `credential-audit`: The hash-chained audit log, its fields, and `legato audit`.

### Modified Capabilities
- `approvals` (from `agent-approvals`): a new source, `credential`, whose decisions create grants and message the agent.

## Impact

- **Code:** `internal/engine/credential` (new: types, interfaces, stores, `static`, crypto, matching); `internal/engine/store` (migrations for secrets, grants, audit); `internal/service` (`credential_broker.go`, `credential_approval.go`, `credential_profiles.go`); `config` (`credentials`, `secrets`); `internal/cli` (`request_access`, guidance); `cmd/legato` (`credential`, `secret`, `audit` commands; wiring).
- **APIs:** one MCP tool; three CLI command groups; `GET /api/credentials` (names, profiles, rules, no secrets) and `GET /api/credentials/audit` for the web UI.
- **Config:** `credentials` (list), `secrets.master_key` (`auto` by default), `secrets.onepassword.token`.
- **Dependencies:** none in Go. The `op` command for 1Password references; `systemd-creds`, `secret-tool` or `security` for the master key where present.
- **Unchanged:** with no `credentials` configured, nothing changes for agents.
