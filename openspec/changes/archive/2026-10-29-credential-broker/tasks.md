Every task is test-first (red, green, refactor).

## 1. Engine: types, matching, stores

- [x] 1.1 `internal/engine/credential` types and interfaces; resource glob matching and specificity; table tests for covering and specificity
- [x] 1.2 Secret references: parse each scheme; tests for valid and invalid references
- [x] 1.3 Stores `env`, `file` (mode check), `systemd-creds` (credentials directory), `op` and `keychain` (fake binaries on `PATH`); tests per store, including that values never appear in errors
- [x] 1.4 Sealing: AES-256-GCM with HKDF per name and type; tests for round trip and a moved row failing
- [x] 1.5 Master key sources (systemd-creds, secret-tool, security, passphrase, key file) behind command runners; tests with fake binaries for each, `auto` order and the recorded source

## 2. Store

- [x] 2.1 Migration 070 `secrets`; store CRUD tests
- [x] 2.2 `credential_grants` in migration 070; tests for live grants of an execution, ending by expiry, by use and by execution end
- [x] 2.3 `credential_audit` in migration 070 with the hash chain appended in an immediate transaction; tests for the chain, verify, an edited row, and concurrent appends

## 3. Config and profiles

- [x] 3.1 `credentials` and `secrets` config blocks with validation; tests for unknown profile, scheme, duplicate name and rule fields
- [x] 3.2 Profiles: embedded `secret`, the user profile folder, replace by name; tests

## 4. Broker service

- [x] 4.1 Rule selection and modes; tests for each mode, ties, no match
- [x] 4.2 `Request`: auto grant, deny, approval with a 60-second wait (injected clock), pending; tests
- [x] 4.3 `credential` approval resolver: grants by chosen option, `[legato approvals]` messages for approve and deny; tests with a recording messenger
- [x] 4.4 `Authorize`: use inside a grant, outside it, single-use grants, limits from the audit log; tests
- [x] 4.5 End grants on `EventAgentDied`; test
- [x] 4.6 Audit rows for every event; tests that no secret value is recorded

## 5. Agent tools and CLI

- [x] 5.1 MCP `request_access` and guidance; tests for the three results and tool listing
- [x] 5.2 `legato credential request|get|exec|list`; contract tests for JSON, exit 4 on pending or denied, and that `exec` passes the value only to the command
- [x] 5.3 `legato secret add|list|remove`; contract tests that values are never printed
- [x] 5.4 `legato audit` and `legato audit verify`; contract tests
- [x] 5.5 `GET /api/credentials` and `GET /api/credentials/audit`; handler tests

## 6. Wiring and docs

- [x] 6.1 Build the broker in the TUI, `legato serve`, `legato mcp serve` and the CLI; register the resolver in host processes
- [x] 6.2 Web: credential approvals show the scope, the reason as the agent's words, and a broader-scope mark; component test
- [x] 6.3 Docs: `docs/ai/credentials.md`, `config.md`, `cli.md`, `agent-mcp.md`, `packages.md`, `database.md`; CLAUDE.md doc list
- [x] 6.4 Check in a scratch Legato: an agent asks for a credential, the person approves on the web, the agent gets the value with `legato credential exec`, and `legato audit verify` passes
