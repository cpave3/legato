## ADDED Requirements

### Requirement: Plug-in protocol
An external plug-in SHALL be a program named `legato-plugin-<name>` that speaks JSON-RPC 2.0 on its standard input and output, one JSON object per line, with requests in both directions. Legato SHALL start it with a minimal environment that holds no variable named `LEGATO_*`, in the working directory `<data_dir>/plugins/<name>/`, and SHALL keep its standard error in a log file with a size limit.

#### Scenario: Request and reply
- **WHEN** Legato sends `{"jsonrpc":"2.0","id":1,"method":"store.get","params":{"ref":"vault://kv/gh"}}`
- **THEN** it SHALL read the reply with `"id":1` from the plug-in's output

#### Scenario: Environment
- **WHEN** the plug-in lists its environment
- **THEN** it SHALL find no `LEGATO_TASK_ID`, `LEGATO_BROKER_TOKEN` or other `LEGATO_*` variable

### Requirement: describe and configure
Legato's first call to a plug-in SHALL be `describe`, which returns its name, version, protocol version, kinds, capabilities and a JSON Schema for its settings. Legato SHALL refuse a plug-in whose protocol version it does not support, whose name differs from its configuration entry, or whose settings fail the schema. It SHALL then call `configure` with the settings. A reference scheme that a built-in store or another plug-in already claims SHALL be a configuration error.

#### Scenario: Unsupported protocol
- **WHEN** `describe` returns `protocol: 9`
- **THEN** Legato SHALL not use the plug-in and SHALL report an error naming it

#### Scenario: Settings fail the schema
- **WHEN** the entry's settings lack a property the schema requires
- **THEN** Legato SHALL not use the plug-in and SHALL name the missing property

#### Scenario: Scheme already claimed
- **WHEN** a plug-in claims the scheme `op`
- **THEN** loading the configuration SHALL fail

### Requirement: Methods for each kind
A plug-in SHALL be adapted to the Go interface of each kind it lists: `store.get`, `store.put`, `store.delete` and `store.sign` for stores; `issuer.accepts`, `issuer.issue` and `issuer.revoke` for issuers; `inspector.classify` for inspectors; `channel.open` and `channel.close` for channels; `policy.evaluate` for policies. An optional interface SHALL be present only when `describe` lists its capability. Binary values SHALL be base64.

#### Scenario: Store without signing
- **WHEN** a store plug-in's capabilities have `sign: false`
- **THEN** Legato SHALL NOT treat it as a `Signer` and SHALL NOT call `store.sign`

#### Scenario: Issuer that signs
- **WHEN** an issuer plug-in needs a signature with a key kept in a signing store
- **THEN** it SHALL call `broker.sign`, and Legato SHALL NOT send it the private key

### Requirement: Callbacks are checked
Legato SHALL accept `broker.authorize` and `broker.credential` only from channel plug-ins, and only for executions that plug-in opened and grants that `broker.authorize` returned to it. It SHALL accept `broker.sign` only from issuer and channel plug-ins, for credentials whose profile uses them. Each callback SHALL be recorded in the audit log under the plug-in's name.

#### Scenario: Store calls authorize
- **WHEN** a plug-in that provides only a store calls `broker.authorize`
- **THEN** Legato SHALL return an error and authorize nothing

#### Scenario: Channel asks for another execution
- **WHEN** a channel plug-in calls `broker.credential` for an execution it did not open
- **THEN** Legato SHALL return an error

### Requirement: A policy plug-in can only be stricter
For a use under a rule whose profile names a policy plug-in, Legato SHALL call `policy.evaluate` and SHALL apply the stricter of the rule's mode and the plug-in's answer. An answer that would loosen the rule SHALL be ignored and logged.

#### Scenario: Stricter answer
- **WHEN** the rule is `session` and the plug-in answers `request`
- **THEN** the use SHALL need an approval each time

#### Scenario: Looser answer
- **WHEN** the rule is `request` and the plug-in answers `auto`
- **THEN** the use SHALL still need an approval

### Requirement: Enabled only by path and checksum
A plug-in SHALL run only when an entry in `credential_plugins` names it with an absolute path to a regular file whose base name is `legato-plugin-<name>`, and a SHA-256 that matches the file at each start. A new entry, or a change to an entry's path, checksum or settings, SHALL take effect only after the person approves the change; until then Legato SHALL run the last approved entries.

#### Scenario: Checksum mismatch
- **WHEN** the file's SHA-256 differs from the entry's
- **THEN** Legato SHALL not start the plug-in, SHALL raise an Attention item, and credentials that need it SHALL be unavailable

#### Scenario: Entry added by an agent
- **WHEN** a new entry appears in the configuration and nobody has approved it
- **THEN** Legato SHALL NOT start that plug-in

#### Scenario: Symbolic link
- **WHEN** `path` is a symbolic link
- **THEN** loading the configuration SHALL fail

### Requirement: Process lifecycle and failure closed
Legato SHALL start a plug-in at its first use and keep it running. Calls SHALL have a 10-second limit, except `broker.authorize`, which waits as the broker does. A plug-in that exits or exceeds a limit SHALL be restarted with backoff from 1 second, doubling to at most 1 minute. While a plug-in is not running, its stores SHALL return an error, its issuers SHALL issue nothing, its inspectors SHALL refuse, and its policies SHALL answer deny. On shutdown Legato SHALL close the plug-in's input and kill it after 5 seconds.

#### Scenario: Plug-in hangs
- **WHEN** a call gets no reply in 10 seconds
- **THEN** Legato SHALL fail the call and restart the plug-in

#### Scenario: Policy down
- **WHEN** a policy plug-in is not running
- **THEN** uses under its rules SHALL be denied

### Requirement: Docker and git credential helpers as stores
Legato SHALL provide built-in stores for `docker-credential://<helper>/<server URL>`, which runs `docker-credential-<helper>`, and `git-credential://<helper>/<host>/<path>`, which runs `git credential-<helper>`. Each SHALL read a `userpass` secret with `get` and back `put` and `delete` with `store` and `erase`. The helper name SHALL match `[a-z0-9-]+`.

#### Scenario: Docker helper
- **WHEN** a credential's secret is `docker-credential://pass/https://ghcr.io`
- **THEN** Legato SHALL run `docker-credential-pass get` with `https://ghcr.io` on input and read the username and secret

#### Scenario: Invalid helper name
- **WHEN** a reference names the helper `../evil`
- **THEN** loading the configuration SHALL fail

### Requirement: Secret Service as a store
Legato SHALL provide a built-in store for `secret-service://<collection>?<attribute>=<value>…` that looks up one item by its attributes through the freedesktop Secret Service over D-Bus, using an encrypted session where the service offers one, and that creates or replaces the item for `put`. A locked collection SHALL be reported as an error and SHALL NOT be unlocked by Legato.

#### Scenario: Item found
- **WHEN** one item matches the attributes
- **THEN** the store SHALL return its secret

#### Scenario: Several items match
- **WHEN** more than one item matches
- **THEN** the store SHALL return an error that names the attributes

### Requirement: PKCS#11 signing store as a plug-in
Legato SHALL release `legato-plugin-pkcs11` beside `legato`. It SHALL be a signing store for RFC 7512 `pkcs11:` URIs, with settings for the module path and a reference to the PIN in another store. `store.get` SHALL return the public key only, and `store.sign` SHALL sign on the token. `legato` itself SHALL NOT link cgo.

#### Scenario: Sign on a token
- **WHEN** an issuer needs a signature from `pkcs11:token=YubiKey;object=legato-ca;type=private`
- **THEN** the plug-in SHALL sign on the token and return the signature
- **AND** the private key SHALL NOT leave the token
