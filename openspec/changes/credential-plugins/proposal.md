## Why

The `credential-broker` change defines five kinds of plug-in (store, issuer, channel, inspector, policy) as Go interfaces, and Legato ships the built-in ones. A person whose secrets are in Vault, a hardware token, or a password manager Legato does not know cannot use them without a change to Legato. Many tools already speak small credential protocols (Docker credential helpers, git credential helpers, PKCS#11, the freedesktop Secret Service), and Legato can reuse them.

## What Changes

- **External plug-ins.** A program named `legato-plugin-<name>` speaks JSON-RPC 2.0 over its standard input and output, one JSON object per line. It can provide any of the five kinds. Legato starts it at first use and keeps it running.
- **`describe`.** The first call returns the plug-in's name, protocol version, kinds, capabilities (for example a store that can sign, or an issuer that can revoke) and a JSON Schema for its settings.
- **Methods mirror the Go interfaces.** `store.get`, `store.put`, `store.delete`, `store.sign`; `issuer.accepts`, `issuer.issue`, `issuer.revoke`; `inspector.classify`; `channel.open`, `channel.close`; `policy.evaluate`. A channel plug-in calls back into Legato with `broker.authorize` and `broker.credential`; any plug-in can call `broker.sign` to sign with a store's key.
- **Enabled only by the person.** A plug-in is enabled by an entry in `credential_plugins` with its absolute path and its SHA-256. Legato checks the checksum at each start. Adding or changing an entry is a change to the credentials configuration, which needs the person's approval (signed once a device is enrolled). An agent cannot add one.
- **A policy plug-in can only be stricter.** Legato takes the stricter of the matching rule and the plug-in's answer.
- **Adapters for existing protocols.** Built in: `docker-credential-*` helpers and git credential helpers as stores (`docker-credential://`, `git-credential://`), and the freedesktop Secret Service as a store (`secret-service://`). Shipped beside Legato as an external plug-in: `legato-plugin-pkcs11`, which uses a PKCS#11 module as a signing store (`pkcs11:` URIs).

## Capabilities

### New Capabilities
- `credential-plugins`: The plug-in protocol, `describe`, the methods of each kind and the callbacks, the process lifecycle, enablement by path and checksum, and the adapters for Docker helpers, git helpers, PKCS#11 and Secret Service.

### Modified Capabilities
- None in the main specs. `secret-stores` (from `credential-broker`) gains the new reference schemes through its existing registry.

## Impact

- **Code:** `internal/engine/credential` (new files: `plugin_rpc.go`, `plugin_process.go`, `store_docker.go`, `store_gitcred.go`, `store_secretservice.go`); `internal/service` (plug-in registry, enablement checks, approvals for changes); `cmd/legato-plugin-pkcs11/` (new binary, built with cgo, released beside `legato`); `config/` (`credential_plugins`).
- **APIs:** the plug-in protocol (versioned, documented in `docs/credential-plugins.md`); reference schemes `docker-credential://`, `git-credential://`, `secret-service://`, `pkcs11:`.
- **Config:** `credential_plugins: [{name, path, sha256, settings}]`; `pkcs11` settings name the module path.
- **Dependencies:** `github.com/godbus/dbus/v5` (pure Go) for Secret Service; `github.com/miekg/pkcs11` (cgo) in the PKCS#11 plug-in only, so `legato` itself stays free of cgo.
- **Unchanged:** built-in plug-ins keep their Go interfaces; a Legato with no `credential_plugins` starts no process.
