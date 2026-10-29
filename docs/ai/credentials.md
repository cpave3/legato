# Credentials

Legato holds credentials for agents, so no agent needs a secret pasted into its prompt, its environment or a file. An agent asks for a scope of a credential; the person's rules decide whether that happens at once, after one approval, or never; each use is checked against what was granted; and every request, decision and use is recorded. Approvals go through the approval service (see [Approvals](approvals.md)). The design is in `openspec/changes/archive/2026-10-29-credential-broker`; later phases (channels that keep the secret from the agent, short-lived issuers, signed decisions, external plug-ins) are in `credential-channels`, `github-credentials`, `signed-approvals` and `credential-plugins`.

Setting a new host up (the `op` command, a service account, the token file, `~/.ssh/config`) is in [Setting up credentials on a host](../credentials.md).

Until the agent sandbox of `sandboxed-cloud-agents` is on, an agent runs as the person's user and can read what Legato reads, the store's master key included. The broker gives least-privilege defaults and a record, not containment.

There are three ways an agent gets a credential, set by its profile's channel:

| Channel | The agent gets | Profiles |
| --- | --- | --- |
| `helper` | The value itself, from `legato credential get` or `exec` | `secret` |
| `http-proxy` | A placeholder key and Legato's proxy address; Legato puts the real key in each request it allows | `elevenlabs`, `api-key` |
| `ssh-agent` | An SSH agent socket of its own; Legato signs each login it allows | `ssh` |
| `http-proxy` with `git` | Read tokens from git's credential helper; pushes and pull requests through the proxy | `github` |

The two last ones are described under [HTTP proxy](#http-proxy) and [SSH agent](#ssh-agent).

## Configuring

```yaml
secrets:
  master_key: auto                 # or systemd-creds, secret-service, keychain, passphrase, file
  onepassword:
    token_file: ~/.config/legato/op-token   # mode 0600; or token: ops_…

credentials:
  - name: elevenlabs
    secret: legato://elevenlabs-key   # legato secret add elevenlabs-key < key.txt
    policy:
      - { operation: read, approve: auto, limit: { requests: 200/day } }
      - { operation: read, approve: window, window: 30m }   # past the limit
  - name: openai
    secret: legato://openai-key
    profile: api-key
    settings:
      upstream: https://api.openai.com/v1
      key: { header: Authorization, format: "Bearer {secret}" }
  - name: homelab
    secret: op://Legato/homelab/private key?ssh-format=openssh
    profile: ssh

credential_channels:
  http_proxy:
    listen: 127.0.0.1:0              # the default: a free loopback port
  ssh_agent:
    known_hosts: ~/.ssh/known_hosts  # the default
```

- `name` is unique, ignoring case. `secret` is a reference whose scheme picks the store (below). `profile` defaults to `secret`. `type` names the secret's type when the profile takes several.
- `policy` replaces the profile's default rules. Each rule has `operation` (a name or a list; absent means any), `resource` (a glob, `**` across `/`; absent means any), `approve`, and optional `window`, `limit` and `signed`.
- `settings` gives what a profile needs of the credential: `upstream` (an `https` address with no query) and `key` (`{header: <name>, format: "… {secret} …"}` or `{query: <name>}`). The `api-key` profile needs both; on another proxied profile they replace the profile's own.
- Config loading checks the structure (names, schemes, modes, windows, limits, settings, a loopback `listen`). The broker checks profiles and secret types when it starts; a host Legato that finds a wrong credential logs it and refuses credential requests until it is fixed.

## Rules

For each operation and resource, the rule that applies is the matching one with the highest specificity: one point for naming operations, plus the number of literal characters in its resource pattern; the earlier rule wins a tie. A rule whose limit is spent is passed over, so the next rule decides (above: automatic up to 200 uses a day, then asked once per 30 minutes). Anything no rule matches is refused.

| `approve` | The person is asked | A grant lasts |
| --- | --- | --- |
| `auto` | never | until the agent ends |
| `session` | once | until the agent ends |
| `window` | once | the rule's `window` |
| `request` | for each use | one use |
| `deny` | never; refused | — |

The approval offers the rule's mode and every stricter one: a `session` rule offers **For this session**, **For 30 minutes** (or the rule's window) and **Once**. `limit` is a count per period of one meter: `requests`, `bytes` or `characters`, per `minute`, `hour`, `day` or `week` (`characters: 20000/day`). Every allowed use counts one request and the amounts its channel measured; the totals are kept in `credential_meter_totals` (migration 071), so a restart does not reset them. A use that would take a meter past the limit is decided by the next rule. A period is the last day (or hour, …) before the use, not a calendar day. `signed: true` is accepted and shown on the card; until signed decisions exist (phase 5), such a decision is still one tap.

## Asking and using

- **`request_access`** (MCP, offered when credentials are configured; its description names them) and **`legato credential request <name> --operation … [--resource …] [--reason …] [--blocking]`** ask for a scope. A request that a live grant already covers is granted; `auto` grants at once; `deny` and no rule refuse; otherwise an approval is submitted (source `credential`, deduplicated by scope while pending) and the call waits up to 60 seconds. It returns `granted`, `denied` with the person's note, or `pending` with the approval ID and the instruction to carry on with other work, or stop and end the turn when blocked.
- **When the person decides later**, the host Legato tells the agent: `[legato approvals] ap-… approved: read with elevenlabs for this session…`, or the denial with the note and "do not look for another way". A decision made while the agent still waited is not messaged.
- **`legato credential exec <name> --env VAR -- <command>`** runs one command with the value in `VAR`, so it never passes through the agent's own output. **`legato credential get <name>`** prints it. Both check the use first (`--operation`, default `read`; `--resource`): a use no grant covers becomes a request for exactly that use and waits as above. They exit `4` when the use is not allowed, `5` outside an agent session (`LEGATO_TASK_ID` and `LEGATO_EXECUTION_ID` name the session).
- A grant covers what was asked for and approved, for one execution. It ends at its window, after its one use, when its execution ends (`EventAgentDied`), or when the person ends it (Settings › Credentials, `DELETE /api/credentials/grants/{id}`).
- A resource that is itself a pattern (`…/refs/heads/legato/attention-*`) is covered by a rule whose pattern matches it as a string, and the card marks it **Broader scope**.

## HTTP proxy

The `http-proxy` channel keeps an API key in Legato. An agent sends plain HTTP to a loopback port with a placeholder in the key's place; Legato checks the request, puts the real key in, and sends it to the credential's upstream over TLS.

- **One proxy.** One host Legato (TUI or `legato serve`) serves it on `credential_channels.http_proxy.listen` and writes its address to `<runtime dir>/legato/credential-proxy.json`. Each credential is under `/c/<credential>/`, and its upstream is fixed by its profile or settings: the agent cannot name another host, and a request line that names one (as to a forward proxy) is answered `400`.
- **Placeholders.** When an agent session starts, Legato makes a placeholder for each proxied credential: `lgk_` and 32 random bytes. The store keeps only its SHA-256 with the execution and the credential (`credential_placeholders`, migration 071). The session gets the profile's variables: for `elevenlabs`, `ELEVENLABS_API_KEY` (the placeholder) and `ELEVENLABS_BASE_URL` (`http://127.0.0.1:<port>/c/<credential>`); for `api-key`, `<NAME>_API_KEY` and `<NAME>_BASE_URL`, after the credential's name in capitals. A placeholder works only for its credential and while its execution runs; anything else is `401`.
- **Each request.** The profile's route table classifies it (the `rest` inspector, below); the broker authorizes the use with its amounts; the placeholder and the key's header are removed and the real key put in; hop-by-hop and `Proxy-*` headers are dropped; the response streams back. A redirect is passed back, never followed. A response header that holds the key is dropped.
- **Answers.** No route: `403` with `method` and `path`. Refused by a rule: `403` with `operation` and `resource`. A use that needs the person waits up to 60 seconds; then `403` with `{"status": "pending", "approval": "ap-…"}`, and the agent is messaged when the person decides. A metered body over 1 MiB: `413`. An upstream that fails TLS checks or cannot be reached: `502`.
- **Meters.** A request counts one `requests`, its body in `bytes`, and `characters` where its route names a JSON field (code points, so `héllo` is 5). Response bytes are added when the response ends, so they count toward the next request: a byte limit can be passed by one response.
- **Audit.** Each use is a `use` row with the operation, resource, amounts and, in its detail, the method and path (`auto · POST /v1/text-to-speech/abc`).

### The `rest` inspector

A profile lists routes; the first whose method (`*` for any) and path match a request decides its operation:

```yaml
routes:
  - { method: POST, path: "/v1/text-to-speech/{voice}/**", operation: tts.speak, resource: "voice/{voice}", meter: { characters: "$.text" } }
  - { method: GET,  path: "/v1/voices/**", operation: voices.read }
```

A segment is literal, `{name}` for one segment, or a last `**` for the rest (none included). The resource is the upstream host and the route's template (`api.elevenlabs.io/voice/abc`), or the host and the request path. A request no route matches is refused.

### `elevenlabs` and `api-key`

| `elevenlabs` operation | Routes | Default rule |
| --- | --- | --- |
| `tts.speak` | `POST /v1/text-to-speech/{voice}/**`, metered by `$.text` | `auto` up to 20,000 characters a day, then `window` of 1 hour |
| `voices.read`, `models.read` | `GET /v1/voices/**`, `/v2/voices/**`, `/v1/models` | `auto` |
| `account.manage`, `keys.manage` | `/v1/user/**`, `/v1/workspace/**`, `/v1/service-accounts/**`, `/v1/api-keys/**` | `deny` |

Any other ElevenLabs request is refused. `api-key` classifies `GET` and `HEAD` as `http.read` (`session`) and every other method as `http.write` (`request`).

A tool uses the proxy only when it takes a base address. libretto does not yet: it calls `https://api.elevenlabs.io` directly and reads only `ELEVENLABS_API_KEY`, so a credential with the `elevenlabs` profile gives it a placeholder it cannot use until it reads `ELEVENLABS_BASE_URL`.

## SSH agent

The `ssh-agent` channel keeps an SSH key in Legato. With at least one credential of the `ssh` profile, every agent session gets `SSH_AUTH_SOCK=<runtime dir>/legato/ssh/<execution>.sock`, a socket (`0600`, in a `0700` folder) that serves the SSH agent protocol for that execution alone.

- **Keys.** It lists the public keys of the `ssh` credentials and no other. It signs only with them, and refuses every request to add, remove or lock keys. It never passes a request to another agent, so the person's own agent (1Password's included) is out of the agent's reach through it.
- **Session binding.** OpenSSH 8.9 and later send `session-bind@openssh.com` before asking for a signature: the server's host key, the session identifier and the server's signature over it. Legato checks the signature. A sign request on a connection with no binding, a binding that does not verify, or data for another session or host key, is refused; so is data that is not a `publickey` login for `ssh-connection` (a git commit to sign, for one). A second binding on a connection is taken only after a forwarding one.
- **The use.** The host is the bound host key, named through `credential_channels.ssh_agent.known_hosts`: a plain entry by its first name (`[host]:port` as `host:port`), a hashed entry by a host name a rule names. The user is the one the login names. The use is `ssh.login` on `ssh://<user>@<host>`, or on `ssh://<user>@key:SHA256:<fingerprint>` for a key `known_hosts` does not list. A connection a forwarding bind came through is `ssh.forwarded-login`.
- **Holding.** A login that needs the person waits up to 60 seconds, inside sshd's 120-second grace time. Then the agent gets a failure, the approval stays open, and the agent is messaged when the person decides; the next `ssh` is signed.
- **`ssh` profile.** A `keypair` secret: an OpenSSH or PEM private key without a passphrase (from 1Password, `op://…/private key?ssh-format=openssh`). Default rules: `ssh.login` `window` of 1 hour, for each host; `ssh.forwarded-login` `deny`.
- **Legato's own processes.** `ssh_agent_socket`, the inherited `SSH_AUTH_SOCK` and 1Password's socket still apply to Legato and the git it runs. Only agent sessions get the execution's socket, which wins over what they inherit. With no `ssh` credential they keep the inherited one.

- **`~/.ssh/config`.** `IdentityAgent` there wins over `SSH_AUTH_SOCK`: a `Host *` block naming 1Password's agent sends every agent's login past Legato. Set it only outside agent sessions, with `Match exec "test -z \"$LEGATO_EXECUTION_ID\""`. A host Legato with an `ssh` credential runs `ssh -G` at start, as an agent session would, and logs a warning when it names another agent (`sshAgentOverride`). `ssh -G` does not run `Match exec`, so such a block is not seen either way.

An agent can still use `~/.1password/agent.sock` or `~/.ssh` directly: outside the sandbox nothing stops it. 1Password then asks the person as before.

## What agents are told

`cli.CredentialGuidance` adds a paragraph to Legato's guidance for each configured credential, by its channel: for an SSH key, run `ssh` as usual, let a login wait up to a minute for the person, and after a refusal tell them and wait for the `[legato approvals]` message, never using another key, `IdentityFile`, `IdentityAgent` or `ssh -A`; for a proxied key, send requests to the base URL variable with the key variable, and treat a `403` with `"status": "pending"` as waiting for the person; for a secret, `request_access` and `legato credential exec`. Claude Code reads it in the MCP server's instructions (`legato mcp serve`); Codex and Chimera get it in their launch prompt, from the configuration (`describeCredentials`). `CredentialSummary` carries each credential's `channels` and `env` (the variables a session gets) for it.

## Vaults as sources

`credential_sources` makes every SSH Key item of a 1Password vault a credential of the `ssh` profile, with the source's `policy` (else the profile's):

```yaml
credential_sources:
  - vault: op://Legato
    policy:
      - { operation: ssh.login, approve: window, window: 30m }
```

- The vault is listed with `op item list --vault <vault> --categories "SSH Key"`. Each item is a credential named after its title (characters other than letters, digits, `.`, `_` and `-` become `-`; an empty title gives the item's ID), read by ID as `op://<vault id>/<item id>/private key?ssh-format=openssh`, so a renamed item keeps working. A `credentials:` entry of the same name wins; of two items with one name, the first is used.
- **No restart.** A host Legato lists the vault at start and every two minutes. A use, and the SSH agent's list of keys, list it again when the last list is older than 30 seconds (`sourceStaleAfter`), so a new key is offered at the next `ssh`. A vault that cannot be listed keeps the keys it gave last.
- **No private key to list keys.** The socket lists a vault key from the item's `public key` field (`op item get … --fields "label=public key"`), cached by item version. The private key is read only to sign a login.
- Only `ssh` for now; API keys from a vault (by a tag naming the profile) come later.
- `CredentialSummary.source` names the vault of such a credential; `CredentialBroker.Sources` lists the vaults, and the guidance names a vault once ("every SSH key in the 1Password vault Legato") instead of each key.

## GitHub

The `github` profile reaches GitHub through a GitHub App the person creates (setup: [Setting up credentials on a host](../credentials.md) § GitHub). Its secret is the App's private key (`keypair`); its `settings` are `client_id` and `repositories` (owner/name, required). GitHub cannot narrow a token to a branch, so reads use narrow tokens the agent holds, and writes never leave the proxy.

- **Tokens: the `jwt-exchange` issuer.** A template in the profile (`jwt_exchange`): sign an RS256 JWT with the App key (`iss` the client ID, 10 minutes, back-dated a minute), look up the repository's installation (cached an hour), exchange it for a token narrowed to that one repository and the operation's permissions (the profile's `permissions` map), and revoke with `DELETE /installation/token`. Templates fill placeholders (`{settings.*}`, `{owner}`, `{repo}`, `{permissions}`, `{now±n}`, looked-up values) with plain values; every URL is `https` with a written-out host. Read tokens are cached while they have more than 5 minutes left and the scope matches (`JWTExchange.Cached`); a token that can write is made for one use and revoked after it, retried once on failure.
- **Reads.** The session's `GIT_CONFIG_*` set, for `https://github.com`, an empty `credential.helper` (dropping every helper of the person's, global ones too) then `!<legato> credential git <credential>`, and `useHttpPath`. The helper asks the host's proxy (`POST /c/<credential>/token` with the placeholder) for a read token for the repository git names, after the broker allows `git.read` on `github:<owner>/<repo>`. A `gh` wrapper first on the session's `PATH` (`<runtime dir>/legato/bin/gh`) runs the real `gh` with `GH_TOKEN` from `legato credential gh-token` (the repository from `-R`, `GH_REPO` or `origin`). With `credential_channels.gh.isolate_login` (the default) the session also gets an empty `GH_CONFIG_DIR` and `GH_TOKEN=legato-signed-out`: `gh` takes `GH_TOKEN` before its keyring, so neither the wrapper nor a `gh` called by its full path reaches the person's login, and `gh auth token` prints only the stand-in. An empty `GH_CONFIG_DIR` alone is not enough: `gh auth token` still reads the keyring. Tokens come from the host, so the App key is read in one process only.
- **Fetches in the sandbox.** A sandboxed session also gets `url.https://github.com/.insteadOf` for `git@github.com:` and `ssh://git@github.com/`, so a clone with an ssh `origin` fetches with read tokens; an unsandboxed one keeps fetching over ssh with the person's keys. See [Sandbox](sandbox.md) § SSH and git.
- **Pushes.** `url.<proxy>/c/<credential>/git/.pushInsteadOf` sends pushes to `https://github.com/`, `git@github.com:` and `ssh://git@github.com/` to the proxy, and the same helper answers the proxy with the placeholder (Basic auth). At the advertisement (`info/refs?service=git-receive-pack`) the proxy makes a `contents: write` token; it pushes with it and revokes it when the push ends, or after 2 minutes. The push body's commands are read (`ReadReceivePack`: pkt-lines, gzip, SHA-1 and SHA-256, push options; signed pushes and anything malformed refused) and each ref update classified: `git.push`, `git.push-default` (the default branch, read with a read token and cached 10 minutes), `git.push-tag`, `git.delete-ref`; other refs are refused. Resource: `github:<owner>/<repo>@<ref>`.
- **All or nothing.** `CredentialBroker.CheckUse` decides each ref without spending a grant; only when every one is allowed are they used (`Authorize`). A ref that needs the person is asked for at once (no wait: git cannot) and the push refused. A refusal is a receive-pack answer git prints: messages on side band 2 (`remote: Legato: …`, with how to ask, or `pending ap-…`), and `ng <ref> <reason>` for every ref.
- **REST.** `/c/<credential>/api/` serves `https://api.github.com`: `GET /repos/{owner}/{repo}/**` is `api.read`; `POST /repos/{owner}/{repo}/pulls` is `pr.create` on `github:<owner>/<repo>@refs/heads/<head>` (the head read from the body: a route's resource may name `{$.field}`). Every other route is refused. The agent gets `LEGATO_GITHUB_API_URL` and `LEGATO_GITHUB_TOKEN` (the placeholder).
- **Rules.** `{repositories}` in a rule's resource expands to one rule per listed repository. Defaults: `git.read`, `api.read` `auto`; `git.push`, `pr.create` on `…@refs/heads/legato/**` `session`; on `…@refs/heads/**` `request` (signed); `git.push-default`, `git.push-tag`, `git.delete-ref` `deny`. A repository not listed is refused with a reason that names `settings.repositories`.
- **Guidance.** Agents are told to work on `legato/$LEGATO_TASK_ID-…`, ask for that one branch with `request_access` before the first push, push as usual, and open pull requests with `curl` to `$LEGATO_GITHUB_API_URL` (`gh` reads only).
- Not built yet: GraphQL writes, the `oauth2` and `ssh-cert` issuers (`openspec/changes/archive/2026-10-29-github-credentials`).

## Who serves the channels

The first host Legato to take `<runtime dir>/legato/credential-channels.lock` serves the proxy and the sockets for every Legato on the machine; another takes over within 5 seconds of it ending. It opens a socket for each running execution (at once for the sessions it spawns, within 5 seconds for others) and closes it, and ends its placeholders, when the execution ends. Commands that spawn agents (`legato swarm dispatch`, `legato workflow run`) give their sessions the same variables and serve nothing.

## Stores

| Reference | Store |
| --- | --- |
| `legato://name` | Legato's own store: `secrets` table, sealed |
| `op://vault/item/field` | 1Password, through `op read` as the service account of `secrets.onepassword` (below) |
| `systemd-creds://name` | `$CREDENTIALS_DIRECTORY/name`, a systemd unit's `LoadCredentialEncrypted=` |
| `keychain://service/account` | the macOS Keychain, through `security` |
| `env://NAME` | the Legato process's environment |
| `file:///path` | a file, refused when other users can read it |

Values are read when used, never cached, logged or put in an error. What `op` or `security` print is never repeated.

### The 1Password service account

`op://` references are read with `op read` as a 1Password service account, so 1Password never asks the person: the rules are the only check. Make one with read access to a vault of its own (`op service-account create Legato --vault Legato:read_items`, or Developer › Service Accounts on 1password.com; it cannot read Personal or Private vaults). Any `op` works for this, a Homebrew one too: the setgid install that 1Password's app integration needs is not required.

Give Legato the token as `secrets.onepassword.token_file`: a file of mode `0600`, read for each `op read` (a file other users can read is refused), so a new token takes effect without a restart. `token` takes the token itself. Do not use `token: ${OP_SERVICE_ACCOUNT_TOKEN}` with the token exported in a shell profile: an agent with that variable can run `op read` on the whole vault, past the rules and the audit log.

### What agent sessions do not inherit

Every agent session gets these variables set empty, whatever Legato or the tmux server inherited: `OP_SERVICE_ACCOUNT_TOKEN`, `LEGATO_SECRETS_PASSPHRASE`, and the variable each `env://` credential of the `http-proxy` or `ssh-agent` channel is read from. A host Legato also removes the first two from its own environment once it has read them, so a tmux server it starts does not hold them. A `helper` credential's `env://` variable is left alone, because `legato credential get` reads it in the agent's own session.

Commands in an agent's session (`legato credential get`, `legato mcp serve`) read `op://` secrets only through `token_file` or `token`, and `legato://` secrets only with a master key source other than `passphrase`. Outside the sandbox an agent can still read the token file, the configuration and Legato's process environment: this keeps the token out of its environment, not out of its reach.

## Legato's own store

`legato secret add <name> [--type value|userpass|keypair|oauth-client|oauth-refresh] [--from <file>]` (the value from stdin otherwise), `legato secret list` (names, types, dates and the master key's source; never values), `legato secret remove <name>`.

- Each value is sealed with AES-256-GCM under a key derived with HKDF-SHA256 from the master key for its name and type, which are also its additional data: a value moved to another name, or read as another type, does not open.
- The master key is 32 random bytes made on first use. `auto` takes the first source that works: `systemd-creds` (sealed to `<data_dir>/secrets.key.cred`, Linux), the Secret Service (`secret-tool`, attributes `service legato key master`), the Keychain (macOS, service `legato`, account `master-key`), a passphrase in `LEGATO_SECRETS_PASSPHRASE` (PBKDF2-SHA256, 600,000 rounds, salt in `<data_dir>/secrets.salt`), and a key file `<data_dir>/secrets.key` (mode 0600).
- `<data_dir>/secrets.source` records the source and a value sealed under the key. Later starts use that source and no other, and refuse a key that does not open the value (a wrong passphrase).

## Profiles

A profile is YAML: `name`, `description`, `issuer` (`static` in this phase), `channels` (`helper`, `http-proxy`, `ssh-agent`), `accepts` (secret types) and default `policy`; a proxied profile adds `inspector: rest`, `upstream`, `key`, `routes` and `env`, and an SSH one `inspector: ssh`. Legato ships `secret` (a value handed to the agent on request; default rule `read: session`), `elevenlabs`, `api-key` and `ssh`. Profiles in `<config dir>/profiles/*.yaml` add to or replace the shipped ones by name. Shipped profiles are in `internal/service/credential_profiles/`.

## Audit log

`credential_audit` (migration 070) is append-only. Events: `request`, `pending`, `grant`, `deny`, `use`, `refuse`, `revoke`. Each row records the time, execution, task, agent tool, workspace, credential, operation, resource, amount, approval and who decided; never a value. Each row holds the previous row's hash and its own, the SHA-256 of the previous hash and the row's canonical JSON, appended inside a transaction that takes SQLite's write lock first, so processes appending together keep one chain. The time is stored as text so it reads back exactly as it was hashed.

`legato audit [--credential] [--task] [--execution] [--limit]` prints rows newest first; `legato audit verify` names the first row whose hashes do not match. The web UI reads `GET /api/credentials/audit`.

## Code

- `internal/engine/credential`: secret types and references, the stores, sealing, master key sources (`LoadMasterKey`, `RecordedSource`), resource matching (`Covers`, `Specificity`, `IsPattern`), the plug-in interfaces (`Store`, `Issuer`, `Inspector`, `Channel`, with `Putter`, `Deleter`, `Signer`, `Revoker`) and `StaticIssuer`. The channels: `HTTPProxy` (`httpproxy.go`) with `RestInspector` (`rest.go`), and `SSHAgentServer` (`sshagent.go`) with `KnownHosts` (`knownhosts.go`). They ask Legato through `ProxyBackend` and `SSHBackend`.
- `internal/engine/credential`: GitHub's parts: `JWTExchange` and `ExchangeTemplate` (`issuer_jwt.go`), `ReadReceivePack`, `ClassifyRefUpdate` and `RefusePush` (`inspect_git.go`), the proxy's `/git/` and `/token` routes and `GitBackend` (`httpproxy_git.go`).
- `internal/engine/store/credential_channels.go`: placeholders, meter totals, running executions.
- `internal/service/credential_github.go`: tokens per use, the default branch, `AuthorizePush`, the session's git settings and the `gh` wrapper. `internal/cli/credential_git.go`: the git helper and `gh-token`.
- `internal/service/credential_channels.go`: `CredentialChannels`, the session environment and the backend of both channels; `SessionEnvProvider`, which `agentService.SetSessionEnv` takes.
- `cmd/legato/credential_channels_runtime.go`: the channel host (lock, proxy listener, address file, socket reconcile) and `attachCredentialChannels` for commands that spawn agents.
- `internal/engine/store/credential.go`: `secrets`, `credential_grants`, `credential_audit`.
- `internal/service/credential_broker.go`: `CredentialBroker` (`Request`, `Authorize`, `Value`, `Grants`, `EndGrant`, `Audit`, the `credential` resolver); `credential_secrets.go`: `SecretStoreService`; `credential_profiles.go`.
- `internal/cli/mcp_credentials.go`: `request_access` and `AccessText`.
- `cmd/legato/credential_runtime.go`, `credential_contract.go`: wiring and the `secret`, `credential` and `audit` commands. Host processes (TUI, `legato serve`) build the broker with the resolver and a messenger; `legato mcp serve` and the CLI build it without, so their approvals are decided by a host.
- `internal/server/credentials.go`: `GET /api/credentials`, `GET /api/credentials/grants`, `DELETE /api/credentials/grants/{id}`, `GET /api/credentials/audit`.
- `web/src/components/CredentialsSettings.tsx` (Settings › Credentials) and the credential card of `ApprovalTray`.
