## ADDED Requirements

### Requirement: Secret references
A secret reference's scheme SHALL select its store: `legato://`, `op://`, `systemd-creds://`, `keychain://`, `env://` or `file://`. Legato SHALL read a value when it is used and SHALL NOT write it to logs, errors, the audit log or command lines.

#### Scenario: Environment reference
- **WHEN** a credential's secret is `env://ELEVEN_KEY` and the variable is set
- **THEN** its value SHALL be the variable's value

#### Scenario: Unreadable file
- **WHEN** a `file://` secret can be read by other users
- **THEN** reading it SHALL fail with an error that names the file and not its content

#### Scenario: 1Password
- **WHEN** a credential's secret is `op://Legato/key/credential`
- **THEN** Legato SHALL read it with `op read` and the service account token from `secrets.onepassword.token`

### Requirement: Typed secrets
Each secret SHALL have a type: `value`, `userpass`, `keypair`, `oauth-client` or `oauth-refresh`. A secret from an external store SHALL take the type its profile expects.

#### Scenario: Wrong type
- **WHEN** a secret of type `value` is used by a profile that accepts only `keypair`
- **THEN** loading the configuration SHALL fail

### Requirement: Legato's own store
`legato secret add <name> --type <type>` SHALL read the value from standard input or `--from <file>` and keep it encrypted with AES-256-GCM under a key derived from the master key, with the name and type as additional data. `legato secret list` SHALL show names, types, dates and the master key's source, never values. `legato secret remove <name>` SHALL delete one.

#### Scenario: Value never shown
- **WHEN** a secret is added and the secrets are listed
- **THEN** the list SHALL NOT contain the value

#### Scenario: Moved row
- **WHEN** a stored ciphertext is copied to another name
- **THEN** decrypting it under that name SHALL fail

### Requirement: Master key
With `secrets.master_key: auto`, Legato SHALL use the first master key source that works, in this order: systemd credentials, the Secret Service, the macOS Keychain, a passphrase in `LEGATO_SECRETS_PASSPHRASE`, and a key file in the data directory with mode 0600. Once a source has made the key, Legato SHALL use that source on every later start.

#### Scenario: Key file fallback
- **WHEN** no other source is available
- **THEN** Legato SHALL create `<data_dir>/secrets.key` with mode 0600 and use it

#### Scenario: Source kept
- **WHEN** the key was made with the Secret Service and Legato starts again
- **THEN** Legato SHALL read the key from the Secret Service
