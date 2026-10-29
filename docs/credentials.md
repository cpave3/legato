# Setting up credentials on a host

How to give Legato's agents an SSH key and API keys on a new machine, so that agents use them without holding them, and you approve their use from the web UI, the TUI or your phone. Do the steps in order. What each part does, and the reference for every setting, is in [Credentials](ai/credentials.md).

| What | Where it lives | Agents get |
| --- | --- | --- |
| An SSH key | 1Password (or Legato's own store) | Their own SSH agent socket; Legato signs each login you allow |
| An API key with a proxied profile (`elevenlabs`, `api-key`) | Legato's own store or 1Password | A placeholder key and Legato's proxy address |
| Any other secret (profile `secret`) | Legato's own store or 1Password | The value, through `legato credential exec`, after you allow it |

## 1. Legato

Build or install Legato as usual (`task build`), and run its TUI or `legato serve` on the host. That process serves the credential channels (the proxy and the SSH agent sockets) for every Legato on the machine.

Approvals reach you in the web UI, the TUI (`!`), Attention, and on your phone through ntfy (`notifications.ntfy` in the configuration). Set `web.public_url` so the phone's link opens this host.

## 2. 1Password (for keys kept in 1Password)

Skip this part for keys you keep in Legato's own store (step 4).

1. **Install the `op` command.** Any install works: Homebrew (`brew install --cask 1password-cli`), or the zip from `https://cache.agilebits.com/dist/1P/op2/pkg/v<version>/op_linux_amd64_v<version>.zip`. Legato uses a service account, so the 1Password app does not need to be installed, and the app's CLI integration (which needs an `op` owned by the `onepassword-cli` group with the setgid bit) is not needed.
2. **Make a vault for Legato** (here `Legato`). A service account cannot read your Personal or Private vault. Put in it only the items agents may use.
3. **Make a service account** with read access to that vault and nothing else: on 1password.com, Developer › Service Accounts › New, or `op service-account create Legato --vault Legato:read_items` (that command needs `op signin` as yourself first). Save the token (`ops_…`) in 1Password: it is shown once.
4. **Save the token in a private file.** In zsh:

   ```sh
   (umask 077; read -rs "t?token: " && printf %s "$t" > ~/.config/legato/op-token; echo)
   ```

   In bash, `read -rs -p "token: " t`. The file must not be readable by other users; Legato refuses it otherwise.
5. **Check it** (prints names and a public key, never a secret):

   ```sh
   OP_SERVICE_ACCOUNT_TOKEN="$(cat ~/.config/legato/op-token)" op item list --vault Legato
   ```

Never export `OP_SERVICE_ACCOUNT_TOKEN` in a shell profile, and do not write `token: ${OP_SERVICE_ACCOUNT_TOKEN}` in the configuration. Legato empties that variable in agent sessions, but `token_file` keeps it out of every environment in the first place.

## 3. An SSH key

1. **The key.** In 1Password: New Item › SSH Key in the `Legato` vault (generate one, or import one without a passphrase). Or keep it in Legato's own store: `legato secret add homelab-key --type keypair --from <private key file>`.
2. **Let it in.** Add its public key to `~/.ssh/authorized_keys` on each machine agents may log in to. From 1Password: `op item get <item> --vault Legato --fields "public key"`.
3. **Know the hosts.** Legato names a host by the key the server proves it holds, looked up in `~/.ssh/known_hosts` (`credential_channels.ssh_agent.known_hosts`). Connect to each host once yourself, so it is listed. A host that is not listed is named `key:SHA256:…` in approvals and rules.
4. **Make ssh use Legato's agent in agent sessions.** `IdentityAgent` in `~/.ssh/config` wins over `SSH_AUTH_SOCK`, so a line such as `Host *` / `IdentityAgent ~/.1password/agent.sock` sends every agent's login past Legato. Keep it for your own shells only:

   ```
   # 1Password's agent everywhere except in Legato's agent sessions, which
   # have LEGATO_EXECUTION_ID and use Legato's own agent ($SSH_AUTH_SOCK).
   Match exec "test -z \"$LEGATO_EXECUTION_ID\""
   	IdentityAgent ~/.1password/agent.sock
   ```

   Put it where the `Host *` block was, before other blocks. Legato's own git keeps using 1Password. When an `ssh` credential is configured, Legato logs a warning at start if `ssh` still names another agent for every host.

## 4. API keys

Store a key in Legato's own store (`legato secret add elevenlabs-key`, the value on stdin), or keep it in 1Password and name it `op://Legato/<item>/credential`.

A proxied key works only with a tool that takes a base address. The `elevenlabs` profile gives agents `ELEVENLABS_API_KEY` (a placeholder) and `ELEVENLABS_BASE_URL`. libretto reads only the first today, so keep its key on the `secret` profile until libretto reads `ELEVENLABS_BASE_URL`.

## 5. The configuration

In `~/.config/legato/config.yaml`:

```yaml
secrets:
  onepassword:
    token_file: ~/.config/legato/op-token

credentials:
  - name: legato_default
    secret: op://Legato/legato_default/private key?ssh-format=openssh
    profile: ssh
    policy:
      - { operation: ssh.login, approve: window, window: 30m }   # offers: for 30 minutes, once, deny
      - { operation: ssh.forwarded-login, approve: deny }
  - name: elevenlabs
    secret: legato://elevenlabs-key
    profile: secret
    policy:
      - { operation: read, approve: auto, limit: { requests: 200/day } }
      - { operation: read, approve: window, window: 30m }
```

`policy` replaces the profile's rules; leave it out to use them (the `ssh` profile asks once an hour for each host). Restart Legato after a change: it reads credentials at start.

## 6. Check it

```sh
legato credential list        # every credential, its profile and store
legato secret list            # Legato's own store, and where its master key lives
```

Then, in an agent's terminal (spawn a shell agent from the web UI or the TUI):

```sh
ssh-add -l                    # lists only the credentials' keys
ssh -v <host> true            # the approval appears; "Will attempt key: legato_default … agent"
```

Approve the request, and the login goes through. `legato audit` shows each request, grant and use; `legato audit verify` checks the log's chain.

Agents are told about each credential and how to use it, in their Legato guidance: run `ssh` as usual, wait for an approval, and never get in another way.

## What this does not stop

Outside the agent sandbox (not built yet), an agent runs as you. It can read `~/.config/legato/op-token`, the configuration, `~/.ssh`, and `~/.1password/agent.sock`. These steps make the approved way the one agents use and are told to use, and they record every use; they do not make the other ways impossible.
