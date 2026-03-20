## ADDED Requirements

### Requirement: gh CLI availability check

The GitHub PR client SHALL verify that the `gh` CLI is installed and available on PATH before attempting any operations. The check MUST use `exec.LookPath` to allow test injection.

#### Scenario: gh CLI is installed

- **WHEN** the client is initialized and `gh` is found on PATH
- **THEN** the client SHALL be ready to accept PR status queries

#### Scenario: gh CLI is not installed

- **WHEN** the client is initialized and `gh` is not found on PATH
- **THEN** the client SHALL return a descriptive error indicating that `gh` CLI is required and where to install it

### Requirement: Fetch PR status by branch name

The client SHALL query GitHub for PR status associated with a given branch using `gh pr list --head <branch> --state all --limit 1 --json number,headRefName,title,state,isDraft,reviewDecision,url,statusCheckRollup`. The client MUST parse the JSON response into a structured `PRStatus` type.

#### Scenario: Branch has an open PR

- **WHEN** a branch name is queried and an open PR exists for that branch
- **THEN** the client SHALL return a `PRStatus` with `HasPR=true`, the PR number, title, state `OPEN`, review decision, draft status, check status, and URL

#### Scenario: Branch has a merged PR

- **WHEN** a branch name is queried and the PR has been merged
- **THEN** the client SHALL return a `PRStatus` with state `MERGED`

#### Scenario: Branch has no PR

- **WHEN** a branch name is queried and no PR exists for that branch
- **THEN** the client SHALL return a `PRStatus` with `HasPR=false` and all other fields at zero values

#### Scenario: Multiple PRs for a branch

- **WHEN** a branch has multiple PRs (e.g., one closed, one open)
- **THEN** the client SHALL prefer the open PR, falling back to merged, then closed, based on state priority

### Requirement: Batch fetch PR status for multiple branches

The client SHALL support fetching PR status for multiple branches concurrently. Each branch query MUST execute in its own goroutine. Concurrency MUST be limited by a semaphore (default 5) to avoid overwhelming the GitHub API.

#### Scenario: Batch query with multiple branches

- **WHEN** PR status is requested for 8 branches
- **THEN** the client SHALL execute queries concurrently with at most 5 in flight at a time and return a map of branch name to `PRStatus`

#### Scenario: Partial failure in batch query

- **WHEN** one branch query fails but others succeed
- **THEN** the client SHALL return results for successful queries and an error aggregating the failures

#### Scenario: Empty branch list

- **WHEN** an empty list of branches is provided
- **THEN** the client SHALL return an empty map without making any `gh` CLI calls

### Requirement: Derive CI check status from status check rollup

The client SHALL derive an aggregate CI status from the `statusCheckRollup` array: `"pass"` if all checks are SUCCESS/NEUTRAL/SKIPPED, `"fail"` if any check is FAILURE/ERROR/CANCELLED/TIMED_OUT/ACTION_REQUIRED, `"pending"` if checks are still running, or `""` if no checks exist.

#### Scenario: All checks passing

- **WHEN** all status checks have conclusion SUCCESS, NEUTRAL, or SKIPPED
- **THEN** the derived check status SHALL be `"pass"`

#### Scenario: Any check failing

- **WHEN** at least one status check has conclusion FAILURE
- **THEN** the derived check status SHALL be `"fail"`

#### Scenario: Checks still running

- **WHEN** some checks have no conclusion yet (still in progress)
- **THEN** the derived check status SHALL be `"pending"`

#### Scenario: No checks configured

- **WHEN** the status check rollup is empty
- **THEN** the derived check status SHALL be `""`

### Requirement: Fetch PR comment count

The client SHALL support fetching the total comment count for a PR using `gh api repos/{owner}/{repo}/pulls/{number}` and reading the `comments` and `review_comments` fields.

#### Scenario: PR with comments

- **WHEN** a PR has 2 issue comments and 5 review comments
- **THEN** the client SHALL return a total comment count of 7

#### Scenario: PR with no comments

- **WHEN** a PR has no comments
- **THEN** the client SHALL return a comment count of 0

### Requirement: Detect repository owner and name

The client SHALL detect the GitHub repository owner and name from the current git remote using `git remote get-url origin` and parsing the SSH or HTTPS URL format.

#### Scenario: SSH remote URL

- **WHEN** the git remote is `git@github.com:owner/repo.git`
- **THEN** the client SHALL parse owner as `owner` and repo as `repo`

#### Scenario: HTTPS remote URL

- **WHEN** the git remote is `https://github.com/owner/repo.git`
- **THEN** the client SHALL parse owner as `owner` and repo as `repo`

#### Scenario: No git remote

- **WHEN** the working directory has no git remote configured
- **THEN** the client SHALL return an error indicating no GitHub remote was found
