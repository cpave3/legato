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

## 3. SSH keys

1. **The keys.** In 1Password: New Item › SSH Key in the `Legato` vault (generate one, or import one without a passphrase). With the vault as a source (step 6), every SSH Key item in it is a credential, named after its title, and a key you add or delete is seen within about two minutes, with no restart. Or keep a key in Legato's own store, `legato secret add homelab-key --type keypair --from <private key file>`, and add it to `credentials:` by itself.
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

## 5. GitHub

Agents read your repositories, push to `legato/…` branches and open pull requests through a GitHub App of your own, never with your login. Legato makes a token for each use, narrowed to one repository: read tokens last an hour, and a write token is made for one push or one pull request and revoked after it. The agent never holds a write token: its pushes go through Legato's proxy, which reads which branches each push changes.

| Use | Default rule |
| --- | --- |
| Fetch, pull, `gh` reads, API reads, of a listed repository | no prompt |
| Push to `legato/**`, or a pull request from such a branch | asked once a session, per branch |
| Push to any other branch, or a pull request from it | asked each time |
| Push to the default branch, push a tag, delete a branch | refused |

1. **Create the App.** On GitHub: Settings › Developer settings › GitHub Apps › New GitHub App.
   - Name: anything (`Legato for <you>`). Homepage URL: anything. Webhook: untick **Active**.
   - Repository permissions: **Contents** read and write, **Pull requests** read and write, **Metadata** read.
   - Where can this GitHub App be installed: **Only on this account**. Create it.
2. **Note its client ID** (`Iv23…`, on the App's page).
3. **Generate a private key** (the App's page, Private keys). Store it in Legato and delete the download:

   ```sh
   legato secret add github-app --type keypair --from ~/Downloads/<app>.private-key.pem
   ```

   Or keep it in 1Password and name it as `op://Legato/<item>/<field>`. A vault source gives SSH keys only, so the App key is listed in `credentials:` by itself.
4. **Install the App** (its page, Install App): your account, **Only select repositories**, and pick the ones agents may use.
5. **The configuration** (step 6 below), with the same repositories:

   ```yaml
   credentials:
     - name: github
       secret: legato://github-app
       profile: github
       settings:
         client_id: Iv23liAbCdEf
         repositories: [cpave3/legato, cpave3/chimera]
   ```

   A repository that is not listed is refused, whatever the App is installed on.
6. **A backstop (optional).** In each repository, Settings › Rules › Rulesets › New branch ruleset: target every branch except `legato/**`, restrict creations, updates and deletions, and add your own role to the bypass list. GitHub then refuses the App outside `legato/**` even if Legato had a bug. Rulesets on private repositories need a paid plan.

Agents' `gh` reads with Legato's read tokens and never with your own `gh` login. If you want agents to use your login for queries (`gh api`, searches across repositories), turn that off:

```yaml
credential_channels:
  gh:
    isolate_login: false
```

In an agent session, `git fetch` and `git pull` get read tokens from Legato's credential helper, and `git push` (to `https://github.com/…` or `git@github.com:…`) goes to Legato's proxy. Agents are told to work on `legato/<task ID>-<a few words>`, to ask for that one branch before the first push, and to open pull requests with the API address Legato gives them (`$LEGATO_GITHUB_API_URL`), not with `gh`, which only reads.

PR tracking keeps its own read access (`github.auth`); this changes nothing there.

## 6. The configuration

In `~/.config/legato/config.yaml`:

```yaml
secrets:
  onepassword:
    token_file: ~/.config/legato/op-token

# Every SSH Key item of the vault, with these rules.
credential_sources:
  - vault: op://Legato
    policy:
      - { operation: ssh.login, approve: window, window: 30m }   # offers: for 30 minutes, once, deny
      - { operation: ssh.forwarded-login, approve: deny }

credentials:
  - name: elevenlabs
    secret: legato://elevenlabs-key
    profile: secret
    policy:
      - { operation: read, approve: auto, limit: { requests: 200/day } }
      - { operation: read, approve: window, window: 30m }
```

`policy` replaces the profile's rules; leave it out to use them (the `ssh` profile asks once an hour for each host). A key that needs rules of its own can still be listed in `credentials:`: an entry there wins over a vault item of the same name. Restart Legato after a change to the configuration: it reads it at start. Changes in the vault need no restart.

`ssh` offers an agent's keys one after another, and a server allows only a few tries (`MaxAuthTries`, 6 by default). With more than about five keys in the vault, a login can fail with "Too many authentication failures" before the right key is offered. For such a host, name its key in `~/.ssh/config` (`IdentityFile` of the public key file, and `IdentitiesOnly yes`).

## 7. Check it

```sh
legato credential list        # every credential, its profile and store
legato secret list            # Legato's own store, and where its master key lives
```

Then, in an agent's terminal (spawn a shell agent from the web UI or the TUI):

```sh
ssh-add -l                    # lists only the credentials' keys
ssh -v <host> true            # the approval appears; "Will attempt key: legato_default … agent"
```

Approve the request, and the login goes through. For GitHub, in the same terminal:

```sh
git ls-remote https://github.com/<owner>/<repo>    # a read, no prompt
git push origin HEAD:refs/heads/legato/check-1     # an approval appears; push again after it
```

`legato audit` shows each request, grant and use; `legato audit verify` checks the log's chain.

Agents are told about each credential and how to use it, in their Legato guidance: run `ssh` as usual, wait for an approval, and never get in another way.

In a sandboxed Chimera session (`--sandbox`), `ssh` fails before it connects with `Bad owner or permissions on /etc/ssh/ssh_config.d/…`: in the sandbox, root's files appear to belong to `nobody` (`ls -ln` shows `65534`), and `ssh` refuses a configuration file it cannot trust. Run `ssh` there in host mode, as with Legato's own commands.

## What this does not stop

Outside the agent sandbox (not built yet), an agent runs as you. It can read `~/.config/legato/op-token`, the configuration, `~/.ssh`, and `~/.1password/agent.sock`, and it can push with your own `gh` login or SSH key by undoing the session's git settings. These steps make the approved way the one agents use and are told to use, and they record every use; they do not make the other ways impossible.
