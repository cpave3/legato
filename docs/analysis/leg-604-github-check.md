# LEG-604: the early GitHub check

Burnlite LEG-604 (spec LEG-559, audit decision X5) asks four questions before LEG-570, LEG-588 and LEG-591 start. This file records the answers and the evidence, read on 2026-11-24.

## How it was checked

The operator decided (2026-11-24) that this check runs no scratch instances, and that no GitHub App key or token is held, fetched or asked for. So the evidence is:

- what Legato's two GitHub Apps have already done on `cpave3/legato`, read with `gh api` through Legato's credential broker (the author App's read token). Every request was a REST `GET` or a GraphQL `query`; nothing was written.
- the Apps' public permission lists (`GET /apps/<slug>`, which needs no authentication);
- open pull requests of public repositories, read with GraphQL queries (question 3 only);
- GitHub's documentation.

`gh api graphql` needs a repository to pick a token, so the GraphQL commands below ran with `GH_REPO=cpave3/legato` inside a clone. REST reads of repositories other than `cpave3/legato` are refused by the broker (no token for them), so the public repositories were read with GraphQL only.

### Which App is which

- **Author App: `legato-cpave3`** (App ID 5523309, owner `cpave3`). It opens the agents' pull requests: 278 of the 283 pull requests on `cpave3/legato` are `legato-cpave3[bot]`'s. It holds the `github` credential profile (`internal/service/credential_profiles/github.yaml`).
- **Reviewer App: `legato-reviews`** (App ID 5661935, owner `cpave3`). It posts the review passes' reviews and thread replies (`legato-reviews[bot]`), on the `github-reviewer` profile ([Credentials](../ai/credentials.md) § The reviewer App).

```
$ gh api /apps/legato-cpave3 --jq '{slug,id,owner:.owner.login,permissions}'
{"id":5523309,"owner":"cpave3","slug":"legato-cpave3",
 "permissions":{"actions":"write","contents":"write","issues":"write","metadata":"read","pull_requests":"write","workflows":"write"}}

$ gh api /apps/legato-reviews --jq '{slug,id,owner:.owner.login,permissions}'
{"id":5661935,"owner":"cpave3","slug":"legato-reviews",
 "permissions":{"actions":"read","administration":"read","checks":"read","contents":"write","metadata":"read","pull_requests":"write","statuses":"read"}}
```

`/apps/<slug>` gives the permissions the App asks for. An installation can lag behind them until the account owner accepts a change. The installation itself (`GET /repos/{owner}/{repo}/installation`) needs the App's JWT, which this check does not hold:

```
$ gh api repos/cpave3/legato/installation
{"message":"A JSON web token could not be decoded","status":"401"}
```

So each answer below that depends on the installation also shows a write that succeeded with it.

`performed_via_github_app` is `null` on these Apps' pull requests and timeline events: the App acts as its own bot user, so `user`/`actor` (`legato-cpave3[bot]`, `legato-reviews[bot]`) is what names it. GraphQL gives the same bot's login without the suffix (`legato-cpave3`, `legato-reviews`).

## 1. The association of a pull request the author App opens

**Answer: `NONE`** on `cpave3/legato`, in REST (`author_association`) and GraphQL (`authorAssociation`), on all 278 of its pull requests. The Reviewer App's reviews and comments, and the author App's own comments, are `NONE` too. A bot is never `OWNER`, `MEMBER` or `COLLABORATOR`. But it can be `CONTRIBUTOR`: other Apps' pull requests in organization repositories read `CONTRIBUTOR`, since their commits land on the base. The author App's would too if commits it authored were merged. Today the agents' commits are authored as `cpave3`, and no commit on `cpave3/legato` is the App's.

**Confidence:** high for `cpave3/legato` (all of its pull requests). Medium-high that it is never one of OWNER, MEMBER or COLLABORATOR anywhere: `CONTRIBUTOR` was seen for other Apps, never a member value.

```
$ gh api --paginate 'repos/cpave3/legato/pulls?state=all&per_page=100' \
    --jq '.[] | [.user.login, .user.type, .author_association] | @tsv' | sort | uniq -c
    278 legato-cpave3[bot]  Bot   NONE
      5 cpave3              User  OWNER

$ gh api graphql -f query='query { repository(owner:"cpave3", name:"legato") {
    pullRequests(last: 5) { nodes { number authorAssociation author { __typename login } } } } }'
{"number":321,"authorAssociation":"NONE","author":{"__typename":"Bot","login":"legato-cpave3"}}  (…the same for #322, #323, #324, #327)

$ gh api repos/cpave3/legato/pulls/327 --jq '{user:.user.login, user_type:.user.type, author_association, merged_by:.merged_by.login}'
{"user":"legato-cpave3[bot]","user_type":"Bot","author_association":"NONE","merged_by":"cpave3"}

$ gh api 'repos/cpave3/legato/pulls/comments?per_page=100' --jq '.[] | [.user.login, .author_association] | @tsv' | sort | uniq -c
     36 legato-cpave3[bot]   NONE
     64 legato-reviews[bot]  NONE
$ gh api 'repos/cpave3/legato/issues/comments?per_page=100' --jq '.[] | [.user.login, .author_association] | @tsv' | sort | uniq -c
      6 cpave3               OWNER
     29 legato-cpave3[bot]   NONE

$ gh api 'repos/cpave3/legato/commits?author=legato-cpave3%5Bbot%5D' --jq length
0
$ gh api repos/cpave3/legato --jq '{owner_type:.owner.type, visibility}'
{"owner_type":"User","visibility":"public"}
```

Other Apps' pull requests in organization repositories, whose commits are merged:

```
$ gh api graphql -F q='repo:cli/cli is:pr author:app/dependabot' -f query='query($q:String!){ search(query:$q, type:ISSUE, first:5){ nodes{ ... on PullRequest { number authorAssociation author{ login __typename } } } } }'
15145  dependabot  Bot  CONTRIBUTOR   (…#15141–#15144 the same)
$ (the same for 'repo:astral-sh/uv is:pr author:app/renovate')
23360  renovate    Bot  CONTRIBUTOR   (…#23356–#23359 the same)
```

GitHub's definitions ([GraphQL enums, `CommentAuthorAssociation`](https://docs.github.com/en/enterprise-server@3.15/graphql/reference/enums); the REST schema lists the same eight values, [Pulls](https://docs.github.com/en/rest/pulls/pulls)):

> NONE: "Author has no association with the repository."
> CONTRIBUTOR: "Author has previously committed to the repository."
> COLLABORATOR: "Author has been invited to collaborate on the repository."
> MEMBER: "Author is a member of the organization that owns the repository."

## 2. What marking a pull request ready needs, as the author App

**Answer:** `markPullRequestReadyForReview` works with an author App installation token of `pull_requests: write`, `contents: write`, `metadata: read`. That is Legato's `pr.ready` operation, which Legato already runs host-side. The author App has both permissions. `pull_requests: write` alone is refused. That was recorded when `pr.ready` was built (commit `d6ccfa8e`, 2026-10-30), but this check could not test it again. GitHub's docs do not list the permission.

**Confidence:** high that `pull_requests: write` + `contents: write` is enough and that the App has it (70 flips by the App). Medium that `contents: write` is required: the evidence is that earlier refusal, not one seen here.

How it runs today: the sandbox's `gh` wrapper hands `gh pr ready` (and `--undo`) to Legato. Legato applies the `pr.ready` rule, makes a token with those three permissions, runs the GraphQL mutation itself and revokes the token (`CredentialChannels` in `internal/service/credential_github.go`; [Credentials](../ai/credentials.md) § `gh pr ready`). No agent ever holds that token, and the proxy passes no GraphQL write. So every `ready_for_review` event by `legato-cpave3[bot]` since that commit came from this host-side path:

```
# internal/service/credential_profiles/github.yaml
  # gh pr ready: GitHub wants contents: write for it too, so Legato marks
  # the pull request itself and the agent never holds this token.
  pr.ready: { pull_requests: write, contents: write, metadata: read }

# internal/service/credential_github.go
mutation($id: ID!) { markPullRequestReadyForReview(input: {pullRequestId: $id}) { pullRequest { number } } }

$ git show -s d6ccfa8e
feat(credentials): gh pr ready is done by Legato after the person allows pr.ready
GitHub refuses markPullRequestReadyForReview to a token that can only
write pull requests; it wants contents: write too, …

$ gh api --paginate 'repos/cpave3/legato/issues/events?per_page=100' \
    --jq '.[] | select(.event=="ready_for_review") | .actor.login' | sort | uniq -c
    141 cpave3
     70 legato-cpave3[bot]        # PRs #49 (2026-11-11) … #301 (2026-11-23)

$ gh api 'repos/cpave3/legato/issues/301/timeline?per_page=100' \
    --jq '.[] | select(.event=="ready_for_review" or .event=="reviewed") | [.event, (.actor.login // .user.login), (.created_at // .submitted_at)] | @tsv'
reviewed          legato-reviews[bot]  2026-11-23T03:59:03Z
reviewed          legato-reviews[bot]  2026-11-23T04:17:10Z
ready_for_review  legato-cpave3[bot]   2026-11-23T04:34:47Z
```

GitHub's docs:

- [GraphQL `markPullRequestReadyForReview`](https://docs.github.com/en/graphql/reference/pulls): "Marks a pull request ready for review." No permission is given.
- [Choosing permissions for a GitHub App](https://docs.github.com/en/apps/creating-github-apps/registering-a-github-app/choosing-permissions-for-a-github-app) does not map GraphQL mutations to permissions: "you should test your app to ensure that it has the required permissions for the GraphQL queries and mutations".
- [Changing the stage of a pull request](https://docs.github.com/en/enterprise-server@3.17/pull-requests/how-tos/create-pull-requests/changing-the-stage-of-a-pull-request): "People with write permissions to a repository and pull request authors can change the stage of a pull request." That is a person's rule; for an App, write to the repository's contents fits the refusal recorded above.

## 3. When `mergeStateStatus` is `BEHIND`

**Answer:** only when the base requires branches to be up to date: strict required status checks, in classic branch protection or a ruleset (`strict_required_status_checks_policy`). A pull request that is behind a base without that rule reads `CLEAN` or `UNSTABLE` (or `BLOCKED` or `DIRTY` for other reasons), never `BEHIND`. With the rule, a pull request that is behind and blocked by nothing else reads `BEHIND`. A behind pull request that something else blocks first (a missing approval) reads `BLOCKED`, and conflicts read `DIRTY`. So a steward that trusts `BEHIND` routes it only once nothing else blocks the merge. It needs no read of the protection, and the spec's Q6 holds.

**Confidence:** high for "not without the rule": in `rails/rails` and `astral-sh/uv`, 24 open pull requests behind their base (and not in conflict) read 16 `CLEAN`, 2 `UNSTABLE` and 6 `BLOCKED`, none `BEHIND`. A second read minutes later again had none `BEHIND` (11 were `UNKNOWN` while GitHub recomputed). High that `BEHIND` appears with the rule. Medium on the order of precedence between `BLOCKED` and `BEHIND`: it is inferred from what was seen, and GitHub does not document it.

On `cpave3/legato` itself this could not be seen. `main` has no protection and no ruleset, and no pull request is open, so there is nothing behind to read. Its classic protection settings are refused to the author App's token (no `administration` permission), though the public branch summary says it has none:

```
$ gh api repos/cpave3/legato/branches/main --jq '{protected,protection}'
{"protected":false,"protection":{"enabled":false,"required_status_checks":{"checks":[],"contexts":[],"enforcement_level":"off"}}}
$ gh api repos/cpave3/legato/branches/main/protection
{"message":"Resource not accessible by integration","status":"403"}
$ gh api repos/cpave3/legato/rules/branches/main
[]
$ gh api repos/cpave3/legato/pulls?state=open --jq length
0
```

Public repositories, read with GraphQL queries: each open pull request's `mergeStateStatus`, how far its head is behind the base (`Ref.compare(headRef: <head SHA>).behindBy`), and the rules on the base (`Ref.rules`, which shows a ruleset's `strictRequiredStatusChecksPolicy` to any reader; classic protection is not visible to a reader). The script is a loop over this query, then one `compare` per pull request:

```
query($owner:String!,$name:String!){ repository(owner:$owner,name:$name){
  defaultBranchRef{ name rules(first:30){ nodes{ type parameters{ ... on RequiredStatusChecksParameters{ strictRequiredStatusChecksPolicy } } } } }
  pullRequests(states:OPEN, first:20, orderBy:{field:UPDATED_AT,direction:DESC}){ nodes{ number isDraft isCrossRepository baseRefName headRefOid mergeStateStatus mergeable } } } }
query($owner:String!,$name:String!,$base:String!,$head:String!){ repository(owner:$owner,name:$name){
  ref(qualifiedName:$base){ compare(headRef:$head){ behindBy aheadBy } } } }
```

Without the rule: `rails/rails` (ruleset: required status checks, `strictRequiredStatusChecksPolicy: false`):

```
#     base  state  mergeStateStatus mergeable  behindBy
59950 main  ready  CLEAN            MERGEABLE  2
59918 main  ready  CLEAN            MERGEABLE  24
59283 main  ready  CLEAN            MERGEABLE  2
59867 main  ready  CLEAN            MERGEABLE  2
59822 main  draft  UNSTABLE         MERGEABLE  10
59943 main  ready  BLOCKED          MERGEABLE  137
59954 main  ready  CLEAN            MERGEABLE  0
```

`astral-sh/uv` (the same rule, `false`):

```
23375 main ready CLEAN    MERGEABLE behindBy=3
23238 main ready CLEAN    MERGEABLE behindBy=46
23329 main ready CLEAN    MERGEABLE behindBy=33
23360 main ready UNSTABLE MERGEABLE behindBy=26
23348 main ready BLOCKED  MERGEABLE behindBy=26
```

With the rule: `github/docs` (ruleset: required status checks, `strictRequiredStatusChecksPolicy: true`, plus a pull request rule and a merge queue):

```
45881 main ready BEHIND   MERGEABLE behindBy=1594
47036 main ready BEHIND   MERGEABLE behindBy=19
43243 main ready BEHIND   MERGEABLE behindBy=4732
44870 main ready BEHIND   MERGEABLE behindBy=2998
43911 main ready BEHIND   MERGEABLE behindBy=3926
46954 main ready BLOCKED  MERGEABLE behindBy=132
47019 main ready BLOCKED  MERGEABLE behindBy=35
43247 main ready DIRTY    CONFLICTING behindBy=4726
```

Two more repositories had `BEHIND` pull requests but no visible strict ruleset, so their classic protection (which a reader cannot see) is presumably strict: `tailwindlabs/tailwindcss` (`BEHIND=23` of 30 open) and `dependabot/dependabot-core` (`BEHIND=9`).

GitHub's docs:

- [GraphQL `MergeStateStatus`](https://docs.github.com/en/graphql/reference/pulls): BEHIND, "The head ref is out of date."; BLOCKED, "The merge is blocked."; CLEAN, "Mergeable and passing commit status."; UNSTABLE, "Mergeable with non-passing commit status."; DIRTY, "The merge commit cannot be cleanly created."; UNKNOWN, "The state cannot currently be determined." The docs do not say when BEHIND applies or in what order these values take precedence.
- [About protected branches](https://docs.github.com/en/repositories/configuring-branches-and-merges-in-your-repository/managing-protected-branches/about-protected-branches): required status checks are "loose" or "strict"; strict: "The branch **must** be up to date with the base branch before merging."; loose: "The branch **does not** have to be up to date with the base branch before merging."
- [Pulls REST](https://docs.github.com/en/rest/pulls/pulls): `mergeable_state` is "required, string" with no values documented.

## 4. What `resolveReviewThread` needs, as the Reviewer App

**Answer:** it works with the Reviewer App's `pr.review` token: `pull_requests: write`, `contents: write`, `metadata: read`. `legato-reviews[bot]` has resolved 62 review threads on `cpave3/legato` this way (pass N's "Resolved in pass N" reply, then the resolve). The App has both permissions. Whether `pull_requests: write` alone would do was not tested, since that needs a token. GitHub's docs give no App permission, only the person's rule: the pull request's author or someone with write access.

**Confidence:** high that the `pr.review` token is enough and that the App has it. Unknown whether `contents: write` is needed for this call, but `pr.review` needs it anyway so the App's approval counts (LEG-58). Keep `pr.review` as it is.

Only one path holds a `legato-reviews` token that can write: `ReviewPublisher.ResolveThread` (`internal/service/review_publisher.go`). It runs `open(ctx, AsReviewer, repo, true)`, so it uses the `pr.review` operation (`opReviewPost`), then the GraphQL mutation. The profile has not changed since it was added (commit `5899b96a`, 2026-11-07):

```
# internal/service/credential_profiles/github-reviewer.yaml
  pr.review: { pull_requests: write, contents: write, metadata: read }
# internal/service/review_publisher.go
mutation($id: ID!) { resolveReviewThread(input: {threadId: $id}) { thread { isResolved } } }

$ gh api graphql -f query='query { repository(owner:"cpave3", name:"legato") { pullRequests(last: 100) {
    nodes { number reviewThreads(first: 50) { nodes { isResolved resolvedBy { login } comments(first:1) { nodes { author { login } } } } } } } } }' \
    --jq '... | [isResolved, resolvedBy.login, first comment author] | @tsv' | sort | uniq -c
     62 true   legato-reviews[bot]  legato-reviews      # on 37 PRs, #217 … #321
     23 false  -                    legato-reviews

$ gh api graphql -f query='query { repository(owner:"cpave3", name:"legato") { pullRequest(number: 321) {
    reviewThreads(first: 3) { nodes { isResolved resolvedBy { login } comments(last: 2) { nodes { author { login } createdAt bodyText } } } } } } }'
{"isResolved":true,"resolvedBy":"legato-reviews[bot]","comments":[
  {"author":"legato-cpave3","createdAt":"2026-11-24T06:45:01Z","body":"Fixed in ed626a4: …"},
  {"author":"legato-reviews","createdAt":"2026-11-24T06:48:43Z","body":"Resolved in pass 2: …"}]}
```

GitHub's docs:

- [GraphQL `resolveReviewThread`](https://docs.github.com/en/graphql/reference/pulls): "Marks a review thread as resolved." No permission is given.
- [Commenting on a pull request](https://docs.github.com/en/pull-requests/collaborating-with-pull-requests/reviewing-changes-in-pull-requests/commenting-on-a-pull-request): "You can resolve a conversation in a pull request if you opened the pull request or if you have write access".
- [Changelog, 2018-09-18](https://github.blog/changelog/2018-09-18-conversation-resolution-permissions): "only the pull request author or users with write access to the repository have the ability to resolve the conversation."

## What this means for the spec

- **LEG-570 (X2):** no change. The author App's pull request reads `NONE`, and it could read `CONTRIBUTOR` but never OWNER, MEMBER or COLLABORATOR, so it is always "outside". X2's carve-out (agent-authored loops approve whatever the association) is needed and correct. Two things the ticket should know: a person-authored loop (`agent_author: false`) on a pull request the author App opened always ends with the "outside the repository" COMMENT. D8, which derives `agent_author`, is what keeps that from happening by mistake. And GraphQL names the author `legato-cpave3` while REST says `legato-cpave3[bot]`, so compare logins in one form. The Reviewer App's own comments are `NONE` too; Legato skips its own logins already.
- **LEG-588:** no change. The host-side ready flip needs `pull_requests: write` + `contents: write` (+ `metadata: read`), Legato's `pr.ready` operation as it stands. The author App has both. Reuse that operation and the existing GraphQL call (`CredentialChannels` in `credential_github.go`). The step is Legato's own host-side work, not an agent's request, so it should not wait on the `pr.ready` approval rule meant for sessions; that is a design point for the ticket, not a GitHub fact.
- **LEG-591 (Q6):** no change. `BEHIND` appears only when the base requires up-to-date branches. Routing only on `mergeStateStatus: BEHIND` is right, and no protection read is needed. Note for its tests and texts: while a missing approval or another blocker holds, a behind pull request reads `BLOCKED`, so the BEHIND item comes once the approval lands. A conflict reads `DIRTY`/`CONFLICTING` first.
- **The reviewer App:** `pr.review` resolves threads; nothing to change. [Credentials](../ai/credentials.md) § The reviewer App no longer lists it as unconfirmed.

## Still unclear

- Whether `markPullRequestReadyForReview` and `resolveReviewThread` work with `pull_requests: write` alone. GitHub does not document it, and testing it needs an App token with fewer permissions, which this check does not hold. Neither answer changes a ticket: Legato's tokens already have `contents: write` for both.
- The order of `BLOCKED` and `BEHIND` (which wins when both apply) is inferred from public pull requests, not documented.
- The author App's installation permissions themselves (they need the App's JWT). The App's successful writes stand in for them.
- Not asked here, and still for the full walk-through (LEG-602): whether the Reviewer App's `APPROVE` counts toward required approvals. `cpave3/legato` requires none, so its approvals show nothing about that.
