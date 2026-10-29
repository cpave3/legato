Every task is test-first (red, green, refactor).

## 1. Engine: types, matching, stores

- [ ] 1.1 `internal/engine/credential` types and interfaces; resource glob matching and specificity; table tests for covering and specificity
- [ ] 1.2 Secret references: parse each scheme; tests for valid and invalid references
- [ ] 1.3 Stores `env`, `file` (mode check), `systemd-creds` (credentials directory), `op` and `keychain` (fake binaries on `PATH`); tests per store, including that values never appear in errors
- [ ] 1.4 Sealing: AES-256-GCM with HKDF per name and type; tests for round trip and a moved row failing
- [ ] 1.5 Master key sources (systemd-creds, secret-tool, security, passphrase, key file) behind command runners; tests with fake binaries for each, `auto` order and the recorded source

## 2. Store

- [ ] 2.1 Migration 070 `secrets`; store CRUD tests
- [ ] 2.2 Migration 071 `credential_grants`; tests for live grants of an execution, ending by expiry, by use and by execution end
- [ ] 2.3 Migration 072 `credential_audit` with the hash chain appended in an immediate transaction; tests for the chain, verify, an edited row, and concurrent appends

## 3. Config and profiles

- [ ] 3.1 `credentials` and `secrets` config blocks with validation; tests for unknown profile, scheme, duplicate name and rule fields
- [ ] 3.2 Profiles: embedded `secret`, user and project profile folders, replace by name; tests

## 4. Broker service

- [ ] 4.1 Rule selection and modes; tests for each mode, ties, no match
- [ ] 4.2 `Request`: auto grant, deny, approval with a 60-second wait (injected clock), pending; tests
- [ ] 4.3 `credential` approval resolver: grants by chosen option, `[legato approvals]` messages for approve and deny; tests with a recording messenger
- [ ] 4.4 `Authorize`: use inside a grant, outside it, single-use grants, limits from the audit log; tests
- [ ] 4.5 End grants on `EventAgentDied`; test
- [ ] 4.6 Audit rows for every event; tests that no secret value is recorded

## 5. Agent tools and CLI

- [ ] 5.1 MCP `request_access` and guidance; tests for the three results and tool listing
- [ ] 5.2 `legato credential request|get|exec|list`; contract tests for JSON, exit 4 on pending or denied, and that `exec` passes the value only to the command
- [ ] 5.3 `legato secret add|list|remove`; contract tests that values are never printed
- [ ] 5.4 `legato audit` and `legato audit verify`; contract tests
- [ ] 5.5 `GET /api/credentials` and `GET /api/credentials/audit`; handler tests

## 6. Wiring and docs

- [ ] 6.1 Build the broker in the TUI, `legato serve`, `legato mcp serve` and the CLI; register the resolver in host processes
- [ ] 6.2 Web: credential approvals show the scope, the reason as the agent's words, and a broader-scope mark; component test
- [ ] 6.3 Docs: `docs/ai/credentials.md`, `config.md`, `cli.md`, `agent-mcp.md`, `packages.md`, `database.md`; CLAUDE.md doc list
- [ ] 6.4 Check in a scratch Legato: an agent asks for a credential, the person approves on the web, the agent gets the value with `legato credential exec`, and `legato audit verify` passes
