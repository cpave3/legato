# How Legato and engram name a repository, and the key an engram binding uses

Research for [#15](https://github.com/cpave3/legato/issues/15), on the map [#12](https://github.com/cpave3/legato/issues/12) "Start Legato agent sessions as engrams".

Sources: Legato at `81b2008`; engram (`github.com/cpave3/engram`) at `b51ee38` (2026-10-26, its `main` today); `engram help concepts`; the session metadata and memory files under `~/.local/share/engram/identities/`, read only. Where this note says "probed", the function was copied verbatim into a scratch program, or git was run in scratch repositories, and the output is quoted.

## Answer

**Use engram's own "here" scope string as the binding key**: `repo:<host>/<path>` for a repository with an origin remote, else `dir:<path>`. A spawn matches a binding when the binding's key is any of the scopes engram counts as here for that directory, as engram's `isHere` does.

Legato computes the key itself, with a small Go port of engram's rules, tested against a shared table of examples. It does not ask engram for it, because no engram command prints it today, and because a job clone's key is needed before the clone exists. Ask engram for `engram where --json` and a promise that the scope format is stable; once that exists, Legato can check its port against it.

## How engram names a place

All of it is in `internal/cli/scope.go`. `placeOf(cwd)` returns a **primary** scope, which new records get, and a list of **scopes** that all count as "here", with the primary first.

1. `git -C <cwd> remote get-url origin`. If `remoteKey` of the result is not empty, `repo:<key>` is the first scope.
2. `git -C <cwd> rev-parse --path-format=absolute --git-common-dir`. If this succeeds, the next scope is `dir:` + the **parent of the common git dir**. This is the main worktree's path, for the main checkout, a subfolder of it, and every linked worktree.
3. Outside git: `dir:` + `filepath.Clean(filepath.EvalSymlinks(cwd))`. If resolving the symlinks fails, the unresolved cwd is used.

So inside a repository with a usable origin the place is `[repo:…, dir:<main worktree>]`. A repository without one is `[dir:<main worktree>]`, and a folder outside git is `[dir:<real path>]`. The `dir:` scope is kept as a second name so that memories saved before a remote was added stay in reach. engram's test `TestMemoriesStayInReachWhenTheirDirectoryBecomesARepository` checks this. `engram help concepts` § Scope says the same in prose. The session's `meta.json` records the primary as `"scope"` (`session.go`, `Scope: a.here()`), and each memory's front matter carries a `scope:` line.

### `remoteKey`: the exact normalisation

```go
switch {
case remote == "":                       return ""
case strings.Contains(remote, "://"):    // URL form
    u, err := url.Parse(remote); if err != nil || u.Scheme == "file" { return "" }
    host, path = u.Hostname(), u.Path    // drops user:pass@ and :port
case strings.Contains(remote, ":") && !strings.HasPrefix(remote, "/"):  // scp form
    host, path, _ = strings.Cut(remote, ":")
    host = host[strings.LastIndex(host, "@")+1:]                          // drops user@
default:                                 return ""  // a local path
}
path = strings.Trim(strings.TrimSuffix(strings.Trim(path, "/"), ".git"), "/")
if host == "" || path == "" { return "" }
return strings.ToLower(host + "/" + path)
```

| Question | engram's rule | Probed |
| --- | --- | --- |
| ssh vs https | Both give `host/path` | `git@github.com:cpave3/Legato.git`, `https://github.com/cpave3/legato`, `ssh://git@github.com/cpave3/legato.git`, `git+ssh://…`, `git://…` and `http://…` all give `github.com/cpave3/legato` |
| `.git` suffix | One trailing `.git` is removed, after any trailing `/` | `…/legato.git/` gives `…/legato`. `…/legato.git.git` gives `…/legato.git`. `my.git-tools.git` gives `my.git-tools` |
| Case | Host **and path** are lowercased | `https://GitHub.com/CPave3/Legato.git` gives `github.com/cpave3/legato` |
| `user@`, credentials | Removed (`url.Hostname()`, or the text after the last `@` in scp form) | `https://x-access-token:tok@github.com/…` gives `github.com/cpave3/legato` |
| Ports | Removed from URL forms. The scp form has no port, so a number after the `:` stays part of the path | `ssh://git@github.com:22/…` gives `github.com/cpave3/legato`. `https://gitlab.example.com:8443/group/proj.git` gives `gitlab.example.com/group/proj`. `git@host.example.com:2222/proj.git` gives `host.example.com/2222/proj` |
| Hosts other than GitHub | Any host, any depth of path | `git@gitlab.com:group/sub/proj.git` gives `gitlab.com/group/sub/proj`. `https://dev.azure.com/org/proj/_git/repo` keeps the whole path |
| ssh host aliases | Not resolved: the alias is taken as the host | `github-work:cpave3/legato.git` gives `github-work/cpave3/legato` |
| Local remotes | No key, so engram falls back to `dir:` | `file:///…`, `/srv/git/x.git` and `../x.git` give `""`. So does Legato's push placeholder `/nonexistent/legato-pushes-need-a-github-credential` |
| URL escapes | Decoded (`u.Path`) | `legato%2Ex` gives `legato.x` |

`git remote get-url` applies `url.<base>.insteadOf` before engram sees the address, and it reads the **fetch** URL, not `pushurl`. Both were probed: with a pushurl set, `get-url` still prints the fetch URL. This matters inside a Legato session, because Legato puts `url.https://github.com/.insteadOf=git@github.com:` (and `ssh://git@github.com/`) in the session's `GIT_CONFIG_*` environment. So engram inside a session sees `https://github.com/…`, and engram on the host sees `git@github.com:…`. Both normalise to the same key, so the scope does not change.

### The `dir:` fallback

| Where | The common git dir | engram's `dir:` |
| --- | --- | --- |
| Main checkout, or a subfolder of it | `<main>/.git` | `<main>` |
| Linked worktree (`git worktree add`) | `<main>/.git` | `<main>`: the same place as the main checkout |
| Legato job clone (`clone --reference <cache>`) | `<clone>/.git`, since alternates do not change the common dir | `<data_dir>/jobs/<execution>/<name>`, but its origin is always set, so `repo:` is the primary |
| Bare repository `x.git` | `x.git` itself | the **parent** of `x.git` (a quirk) |
| Submodule | `<super>/.git/modules/<path>` | `<super>/.git/modules/<parent of path>` (a quirk). Submodules nearly always have a URL origin, so this is rarely used |
| Symlinked path to a checkout | git gives the real path | the real path |
| Not in git | — | `EvalSymlinks(cwd)`, cleaned |

Real data on this machine shows that symlinks matter. `/home` links to `var/home`. Sessions record `cwd: /home/cameron/Projects/engram`, but their scope is `dir:/var/home/cameron/Projects/engram`. The memories in the store carry `repo:github.com/autopilot-dev/autopilot` (58 of them), `repo:github.com/cpave3/{estimator,personal-agent,legato}`, `dir:/var/home/…` and `global`. The checkout `/home/cameron/Projects/personal-ai-agent` is `repo:github.com/cpave3/personal-agent`, so the folder's name does not matter. The worktree `/var/home/cameron/Development/autopilot.ag-10340-role-elevation` has the same scope as the main checkout. Sessions started by Legato have `key: legato:ex-…`, and their scope is computed in the same way.

## How Legato names a repository today

Legato has no single repository identity. It has GitHub-only `owner/name` strings, made in several ways, and plain paths.

| Where | Form | Normalisation | Source |
| --- | --- | --- | --- |
| Repository presets (config `repositories[].repo`, and `repository_presets` from the web UI) | `owner/name` | `normalizeRepo` trims space, then removes one of the prefixes `https://github.com/`, `http://github.com/`, `git@github.com:`, `ssh://git@github.com/`, `github.com/`, then one trailing `/` and one `.git`. `validRepo` requires `^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`. **Case is kept**. Lookups use `strings.EqualFold` (`PrimaryDir`) | `internal/service/repository_presets.go:128`, `internal/service/repository.go:106-119`, `config/config.go:118` |
| Credential `settings.repositories` | `owner/name` | `githubRepo` regex, case kept | `config/credentials.go:320` |
| Repository cache | `<data_dir>/repos/<owner>/<name>.git` | path from `owner/name` | `repository.go` `CachePath` |
| Job clone | at `<data_dir>/jobs/<execution>/<name>` with origin = the cache's `remote.origin.url` = `strings.TrimSuffix(profile.git, "/") + "/" + repo + ".git"`, which is `https://github.com/<owner>/<name>.git` with the preset's case. `pushurl` is the proxy's address or `/nonexistent/legato-pushes-need-a-github-credential`. `job_clones.repo` keeps `owner/name` | — | `credential_github.go:492-504`, `credential_profiles/github.yaml` (`git: https://github.com`), `repository.go:315-372`, `gitrepo.go:219-279` |
| `gitrepo.OriginURL` | the raw `remote.origin.url` of a **bare cache** (`git --git-dir … config --get`), run under the hardened environment (`GIT_CONFIG_GLOBAL=/dev/null`, no system config), so no `insteadOf` applies | — | `internal/engine/gitrepo/gitrepo.go:352` |
| Code context of a task (`InspectCheckout`, used to capture an agent's checkout and by workflows) and `GET /api/repo/detect` (`github.Client.DetectRepoAt`) | `owner/name` | Two regexes, `(?:ssh://)?git@github\.com[:/]([^/]+)/(.+?)(?:\.git)?$` and `https://github\.com/([^/]+)/(.+?)(?:\.git)?$`, with case kept | `internal/engine/git/git.go:51-70`, `internal/engine/github/client.go:443-471` |
| `gh` shim (`GHRepo`) | `owner/name` | `github\.com[:/]([A-Za-z0-9_.-]+)/([A-Za-z0-9_.-]+?)(\.git)?/?$`, with case kept | `internal/cli/credential_git.go:395-438` |
| Directory presets (`directory_presets.path`) | an absolute path | `~` is expanded and the path cleaned. **Symlinks are not resolved** | `internal/service/directory.go:100-113`, migration `067_directory_presets.sql` |
| Where an agent works (`executions.working_dir`, `CodeContext.WorkingDir`) | the directory as given | not resolved. `gitWorktreeRoot` (used for peers) resolves symlinks and uses `--show-toplevel`, which gives the worktree, not the main checkout | `internal/service/agent.go:555, 814, 1189, 1794` |

Probing Legato's regexes shows how far they are from a general key:

| Remote | `engine/git` and `engine/github` | `cli` `GHRepo` |
| --- | --- | --- |
| `git@github.com:cpave3/Legato.git` | `cpave3/Legato` | `cpave3/Legato` |
| `https://github.com/cpave3/legato/` | `cpave3/legato/` (keeps the slash) | `cpave3/legato` |
| `ssh://git@github.com:22/cpave3/legato.git` | `22/cpave3/legato` (wrong) | no match |
| `https://x-access-token@github.com/…` | no match | `cpave3/legato` |
| `https://GitHub.com/…`, any GitLab or ssh-alias remote | no match | no match |

## The differences that matter for a binding

1. **Hosts.** engram supports any host. Legato's parsers only accept `github.com`. A key built only from `owner/name` cannot bind a GitLab checkout or a repository without a remote.
2. **Case.** engram lowercases the host and the path. Legato keeps the case and compares with `EqualFold` in only one place. A preset `CPave3/Legato`, a job clone made from it, and a checkout whose remote says `cpave3/legato` are one repository to GitHub and to engram, but they are three strings to Legato.
3. **Folders outside git, and repositories without a remote.** engram uses the main worktree or the real path. Legato stores unresolved paths, and its only helper finds the worktree's top level, not the main worktree. On this machine `/home` is a symlink, so an unresolved path would not match engram's scope.
4. **The `insteadOf` setting and the environment.** engram runs plain `git` with the person's configuration. Legato's `gitrepo` runs a hardened git without the global configuration. The session's own `insteadOf` rules (ssh to https on github.com) do not change the key. Rules that change the *host* would.
5. **No key before a clone exists.** Spawning on a repository with no `primary_dir` makes a job clone during the spawn, after the dialog has shown the default engram. The dialog only has `owner/name` at that time.

## Recommendation

### The key

Store each binding under engram's scope string, exactly as engram writes it:

- `repo:<lowercased host>/<path>`, made as `remoteKey` makes it, when the directory's origin gives a key. Example: `repo:github.com/cpave3/legato`.
- otherwise `dir:<main worktree>`, the parent of `--git-common-dir`, for a repository without a usable origin;
- otherwise `dir:<EvalSymlinks(abs(dir))>` for a folder outside git.

**Matching.** For the spawn's directory, compute the full place, `[primary, dir:…]`. A binding matches if its key is in that list. If both a `repo:` binding and a `dir:` binding match, the `repo:` one wins. A binding made while a repository had no remote therefore keeps matching after a remote is added, as engram's memories do. **Remember for this repo** stores the primary.

**A job clone or a repository preset with no folder.** Use `repo:github.com/` + `strings.ToLower(owner/name)`. Legato already knows `owner/name` (`job_clones.repo`, the preset), so it does not run git in a clone that the agent controls (`gitrepo` avoids that on purpose). This is the same key engram computes inside the clone, because the clone's origin is `https://github.com/<owner>/<name>.git`. The push URL is not read. This assumes that the GitHub credential profile's `git` stays `https://github.com`. If a profile names another host (for example GitHub Enterprise), take the host from `profile.git`.

Why this key:

- It is the model that was settled on the map: matched by origin remote, so job clones and worktrees match; a plain folder when not in git.
- It is the same string that engram puts in session `meta.json` and in memory front matter. The Settings page can show it as it is, and later features (for example "this engram has N memories here") can join on it without a translation.
- It covers hosts other than GitHub and repositories without a remote. Legato's `owner/name` covers neither.
- Lowercasing joins presets, clones and checkouts that differ only in case.

### Legato's own port, not a call to engram

| | Ask engram each time | Port the rules into Legato (recommended) |
| --- | --- | --- |
| Drift | None: one source | Possible. Reduce it with a shared table of examples (the probe table above), and later a check against `engram where --json` |
| Exists today | **No.** No command prints a place. `boot` only says "Here is …" in prose, and it starts a session | Yes, about 40 lines |
| Job clone before it exists | Not possible: engram needs a directory | `repo:github.com/<owner/name>` |
| When engram is absent | No bindings to match, which is acceptable since the feature is off | Works. Settings can still list and remove bindings |
| Cost | A process for each dialog, each typed directory and each spawn | Two `git` calls, which Legato already makes for code context |
| Process in the directory | engram runs `git` there | Legato runs `git` there with the person's configuration, as engram does. It does not use `gitrepo`'s hardened runner, so `insteadOf` rules affect the key in the same way |

The format is effectively frozen already: engram writes it into every memory file, so changing the normalisation would leave memories out of reach. A port is therefore low-risk, provided engram says so in writing.

A natural place for the port is the engram client package that the boot and probe tickets will need anyway, for example `internal/engine/engram` with `Place(dir) (primary string, scopes []string)` and `RepoScope(host, ownerName string) string`. Engine packages may not import each other, so this package needs its own `exec` of git, as engram does.

The `engram boot` that Legato runs on the host must run with the agent's working directory as its cwd, the job clone for a clone spawn. Otherwise the session's recorded scope is that of the wrong directory. Legato can check that the "Here is …" line of the packet equals the key it matched, and log a mismatch.

### Requests to engram

1. **A machine-readable place**: `engram where [--dir <path>] --json` printing `{"primary": "...", "scopes": ["...", "..."]}`. It needs no identity and starts no session. Legato's tests, and a runtime check of the port, can use it.
2. **Document the scope format as stable**: the `repo:`/`dir:` grammar and the `remoteKey` rules. Memories and sessions already depend on it.
3. **Optional fixes to the quirks**, for engram's maintainers to decide. Legato only has to follow any change in its port:
   - an ssh host alias (`github-work:owner/repo`) is taken as a host, so a checkout cloned through an alias does not match a job clone of the same repository. Resolving it (`ssh -G <alias>`'s `hostname`) would fix this;
   - for a bare repository, `dir:` is the parent of `x.git`, and for a submodule it is `.git/modules/<parent>`. Both are shared with siblings;
   - ports are dropped from URL forms, so two servers on one host would share a key (rare).

## Not covered

- The case-sensitivity of paths on hosts other than GitHub. engram lowercases them anyway, and Legato's port must do the same to match.
- How a binding survives an engram being renamed or deleted. This is a separate part of the map ("When an engram goes away").
