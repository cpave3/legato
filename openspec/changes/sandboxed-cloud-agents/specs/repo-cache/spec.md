## ADDED Requirements

### Requirement: On-demand clone into the cache
`RepositoryService.Ensure(owner/name)` SHALL make a bare clone of the GitHub repository at `<data_dir>/repos/<owner>/<name>.git` when none exists, and SHALL fetch it when its last fetch is older than `repositories.cache.max_age` (default 5 minutes). Clones and fetches of one repository SHALL be serialized.

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
For a job naming a repository, Legato SHALL create a clone at `<data_dir>/jobs/<execution_id>/<name>` that borrows the cache's objects (`git clone --reference`), and check out the requested ref or the default branch. Its `origin` SHALL stay the GitHub URL. The job's environment SHALL send pushes to Legato's credential proxy (`github-credentials`), or, with no `github` credential, to a placeholder URL that cannot be pushed to. A repository preset that names `repo` but no `primary_dir` SHALL use this path.

#### Scenario: Job checkout
- **WHEN** a job for `acme/widget` at ref `feature/x` starts
- **THEN** its working directory SHALL be a clone at that ref whose objects are borrowed from the cache

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
When an execution becomes terminal, Legato SHALL remove its job clones that have no commits absent from the cache. A clone with such commits SHALL be kept for 7 days and then removed.

#### Scenario: Nothing unpushed
- **WHEN** an execution ends and its clone's branch tips are all reachable from the cache or were pushed
- **THEN** the clone SHALL be removed

#### Scenario: Unpushed work
- **WHEN** an execution ends with commits that were never pushed
- **THEN** the clone SHALL be kept, and a note on the task SHALL name its path and when it will be removed
