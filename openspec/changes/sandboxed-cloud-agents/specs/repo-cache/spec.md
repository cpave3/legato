## ADDED Requirements

### Requirement: On-demand clone into the cache
`RepositoryService.Ensure(owner/name)` SHALL make a bare clone of the GitHub repository at `<data_dir>/repos/<owner>/<name>.git` when none exists, and SHALL fetch it when its last fetch is older than `repository_cache.max_age` (default 5 minutes; `repositories` is a list, so the key is top-level). Clones and fetches of one repository SHALL be serialized.

#### Scenario: First use
- **WHEN** `Ensure("acme/widget")` is called and no cache exists
- **THEN** Legato SHALL clone the repository into `<data_dir>/repos/acme/widget.git`

#### Scenario: Recently fetched
- **WHEN** the cache was fetched less than `max_age` ago
- **THEN** `Ensure` SHALL NOT contact GitHub

#### Scenario: Concurrent callers
- **WHEN** two jobs call `Ensure("acme/widget")` at the same time on a cold cache
- **THEN** exactly one clone SHALL run and both callers SHALL receive the same path

#### Scenario: Repository not found or not permitted
- **WHEN** GitHub answers that the repository does not exist or the credential cannot read it
- **THEN** `Ensure` SHALL return an error naming the repository and SHALL leave no partial directory

### Requirement: Host credentials for git
Host clones and fetches SHALL authenticate with a read token that the credential broker issues for that one repository (`git.read` under the `github` profile), supplied to the single git process for that command only. Legato SHALL NOT write the token to any file, git configuration or remote URL.

#### Scenario: Credential not on disk
- **WHEN** a clone has completed
- **THEN** no file under the data directory and no git configuration SHALL contain the credential

#### Scenario: No credential available
- **WHEN** no `github` credential is configured, or the repository is not in its `settings.repositories`
- **THEN** `Ensure` SHALL fail with an error that names the credential setting to change

### Requirement: One clone per job
For a job naming a repository, Legato SHALL create a clone at `<data_dir>/jobs/<execution_id>/<name>` that borrows the cache's objects (`git clone --reference`), and check out the requested ref or the default branch, then the job's own branch: GitHub's branch of the requested name when it exists, else a new branch made there that tracks nothing (`legato/<task id>` when no name is given). Its `origin` SHALL stay the GitHub URL. The job's environment SHALL send pushes to Legato's credential proxy (`github-credentials`), or, with no `github` credential, to a placeholder URL that cannot be pushed to. A repository preset that names `repo` but no `primary_dir` SHALL use this path.

#### Scenario: Job checkout
- **WHEN** a job for `acme/widget` at ref `feature/x` starts
- **THEN** its working directory SHALL be a clone at that ref whose objects are borrowed from the cache

#### Scenario: A branch of its own
- **WHEN** a job for `acme/widget` from `main` starts with no branch named, for task `t1`
- **THEN** its clone SHALL be on a new branch `legato/t1` at `main`, tracking nothing

#### Scenario: Push goes to Legato
- **WHEN** `git push` is run inside the job clone and a `github` credential is configured
- **THEN** git SHALL send the push to Legato's credential proxy, not to GitHub

#### Scenario: Push with no credential
- **WHEN** `git push` is run inside the job clone and no `github` credential is configured
- **THEN** it SHALL fail without contacting GitHub

#### Scenario: Preset without a directory
- **WHEN** a workflow or spawn uses a preset with `repo: acme/widget` and no `primary_dir`
- **THEN** Legato SHALL use a job clone instead of failing for a missing directory

#### Scenario: Code context
- **WHEN** a job clone is created for a task
- **THEN** the task's code context SHALL record `acme/widget` and the branch

### Requirement: Job clone cleanup
When no agent has worked in a job clone for `repository_cache.keep_idle` (default 7 days; `0` keeps clones until removed by hand), Legato SHALL remove it if it has no commits absent from the cache. A clone with such commits SHALL be kept for 7 days more and then removed. A clone SHALL be removable at once from the Remote page (`/remote`), except while an agent works in it. Settings SHALL keep an always-visible link at `#job-clones` to that page, regardless of `repository_cache.enabled`, and SHALL NOT duplicate the inventory. The desktop rail and mobile More sheet's Tools group SHALL link to Remote; More SHALL remain active on that route. This UI relocation SHALL NOT change retention.

#### Scenario: Nothing unpushed
- **WHEN** no agent has worked in a clone for `keep_idle` and its branch tips are all reachable from the cache or were pushed
- **THEN** the clone SHALL be removed

#### Scenario: Idle less than keep_idle
- **WHEN** an agent last worked in a clone less than `keep_idle` ago
- **THEN** the clone SHALL be kept

#### Scenario: Unpushed work
- **WHEN** an execution ends with commits that were never pushed
- **THEN** the clone SHALL be kept, and a note on the task SHALL name its path and when it will be removed

### Requirement: Shared job clones
A spawn SHALL be able to name a live job clone (`clone_id`) to work in instead of a new one, so that the branches of a stack share one folder. The agent SHALL work on the clone's current branch, or on a named branch made at the clone's current commit and checked out, tracking nothing. Legato SHALL do this by writing the clone's git files itself, confined to its `.git` folder, and SHALL NOT run git in the clone. A named branch SHALL be refused while another agent works in the clone, and when a branch of that name exists other than the one checked out. A task with a live clone of a repository SHALL work in it again when spawned with that repository. Every task that works in a clone SHALL be recorded with it, and the sandbox SHALL let each of them work in it. Agents working in one clone at once SHALL see each other as peers.

#### Scenario: Next branch of a stack
- **WHEN** task `t2` is spawned in the clone of task `t1`, which is on `stack/one`, with branch `stack/two`
- **THEN** the clone SHALL be on a new branch `stack/two` at `stack/one`'s commit, with uncommitted changes left as they were, and `t2`'s code context SHALL name `stack/two` on `stack/one`

#### Scenario: Another agent at work
- **WHEN** a spawn names a new branch for a clone another agent is working in
- **THEN** it SHALL be refused, and a spawn that names no branch SHALL join the clone on its branch

#### Scenario: Planted configuration
- **WHEN** a clone's `.git/config` defines a filter, hooks or an fsmonitor, or its `.git/refs` holds a link out of the clone
- **THEN** joining it SHALL run none of them and write nothing outside the clone

#### Scenario: The task's next agent
- **WHEN** a task whose agent worked in a clone of `acme/widget` is spawned again with `acme/widget`
- **THEN** it SHALL work in that clone, on its branch

### Requirement: Truthful managed-clone inventory
The Remote page SHALL describe the selected server's managed job clones, not arbitrary checkouts or cloud-provider state. The legacy `unpushed` field SHALL be presented as local tips absent from the cache, never as verified GitHub push status. The cache may be stale. Comparison failures SHALL be shown as unavailable, not as zero. The UI SHALL state that working-tree changes are not checked, and removal confirmation SHALL warn that local commits and unchecked working-tree changes may be lost. Refreshing the inventory SHALL NOT imply a GitHub fetch or a working-tree inspection.

#### Scenario: Cache comparison only
- **WHEN** a clone has tips absent from the cache
- **THEN** Remote SHALL describe them as local tips absent from cache, not as confirmed unpushed work
- **AND** SHALL state that working-tree changes are not checked

#### Scenario: Fresh clone creation disabled
- **WHEN** `repository_cache.enabled` is false
- **THEN** Remote and the Settings link at `#job-clones` SHALL remain available to manage existing clones
