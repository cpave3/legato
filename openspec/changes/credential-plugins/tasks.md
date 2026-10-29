Every task is test-first (red, green, refactor). This change needs `credential-broker` (phase 2). Signed approval of plug-in changes needs `signed-approvals` (phase 5) but works without it.

## 1. Protocol

- [ ] 1.1 Line-delimited JSON-RPC 2.0 codec with requests in both directions, separate ID spaces, notifications and errors; tests over `io.Pipe`
- [ ] 1.2 `describe` and `configure`: protocol version check, name check, settings checked against the JSON Schema; tests with a fake plug-in written in Go under `testdata/`
- [ ] 1.3 Adapters from plug-in kinds to the Go interfaces (`pluginStore`, `pluginIssuer`, `pluginInspector`, `pluginChannel`, `pluginPolicy`), with optional interfaces only when `describe` lists them; tests that a store without `sign` is not a `Signer`
- [ ] 1.4 Callbacks `broker.authorize`, `broker.credential` and `broker.sign`, checked against the plug-in's kinds and the executions it opened; tests that a store plug-in calling `broker.authorize` is refused
- [ ] 1.5 Policy answers applied as the stricter of rule and plug-in; table test of every pair, and that a loosening answer is logged and ignored
- [ ] 1.6 Audit log entries for callbacks under the plug-in's name; test against the hash chain

## 2. Process and enablement

- [ ] 2.1 Config `credential_plugins` with validation: absolute path, regular file, base name `legato-plugin-<name>`, 64-hex SHA-256; tests
- [ ] 2.2 Checksum check before each start; mismatch disables the plug-in and raises an Attention item; test by changing the fake plug-in's file
- [ ] 2.3 Plug-in entries are part of the approved credentials configuration: a new or changed entry runs only after approval; service test that the last approved entries keep running meanwhile
- [ ] 2.4 Process lifecycle: start at first use, minimal environment with no `LEGATO_*` variables, working directory, stderr log with a size limit, 10-second call limit, restart with backoff, stop on shutdown; tests with a fake plug-in that hangs, exits and floods stderr
- [ ] 2.5 Failure closed: while a plug-in is down its stores error, issuers issue nothing, inspectors refuse and policies deny; service tests
- [ ] 2.6 `legato credential plugin hash <path>`; CLI test

## 3. Built-in adapters

- [ ] 3.1 `docker-credential://` store with `get`, `store`, `erase`; tests with a fake `docker-credential-test` script on `PATH`
- [ ] 3.2 `git-credential://` store with `get`, `store`, `erase`; tests with a fake `git-credential-test` helper and real `git credential`
- [ ] 3.3 Helper name validation (`[a-z0-9-]+`) for both; table test
- [ ] 3.4 `secret-service://` store over D-Bus: lookup by attributes, encrypted session, `put`, a locked collection reported as an error; tests against a private D-Bus session bus with a fake service (skipped without `dbus-daemon`)

## 4. PKCS#11 plug-in

- [ ] 4.1 `cmd/legato-plugin-pkcs11`: `describe`, `configure` (module path, PIN reference), `store.get` (public key only) and `store.sign`; tests against SoftHSM2 (skipped without it)
- [ ] 4.2 `pkcs11:` URI parsing per RFC 7512; table tests
- [ ] 4.3 Build and release the binary beside `legato`; `task build` keeps `legato` free of cgo (architecture test that `cmd/legato` does not import `miekg/pkcs11`)

## 5. Docs

- [ ] 5.1 `docs/credential-plugins.md`: the protocol with a complete example session, every method, the callbacks, limits, and how to write a plug-in
- [ ] 5.2 `docs/ai/credentials.md`: enablement, the adapters and their reference schemes
