// The ci/stack commit status of a GitHub stack (LEG-629), adapted from
// autopilot's scripts/build/stack-status.cjs.
//
// Only the last PR of a GitHub stack runs the heavy jobs of ci.yml (see their
// `if:`). Its own checks gate it, so its ci/stack status simply passes. The PRs
// below it pass ci/stack once the last PR's build passes and it is approved
// (or no approval is required), if that build contains their head.
//
// Unlike autopilot's, drafts are treated as any other PR: agents open draft
// PRs, and their review loops count the checks.
//
// Called from ci.yml's `stack` and `stack-status` jobs and from stack.yml, with
// actions/github-script's `github` and `context`.
const stackContext = 'ci/stack';

// The workflow whose last PR's run onReview reads, and the job of that run
// that posts the build's result: cmd/legato's ci_workflow_test.go checks both
// against the workflow files.
const buildWorkflow = 'ci.yml';
const resultJob = 'stack-status';

// pr is the event payload's PR, or a fresh read when the payload may be stale.
const stackStatus = ({ github, context, pr }) => {
  const { owner, repo } = context.repo;
  const thisRun = `${context.serverUrl}/${owner}/${repo}/actions/runs/${context.runId}`;
  const postStatus = (sha, state, description, targetUrl = thisRun) =>
    github.rest.repos.createCommitStatus({
      owner,
      repo,
      sha,
      state,
      description,
      context: stackContext,
      target_url: targetUrl,
    });
  const openPrWithHead = async (ref) =>
    (await github.rest.pulls.list({ owner, repo, state: 'open', head: `${owner}:${ref}` })).data[0];
  const isLast = !pr.stack || pr.stack.position === pr.stack.size;

  // Posts on each open PR below this one whose head this PR's head contains.
  const postBelow = async (state, description, targetUrl) => {
    for (let position = pr.stack?.position, ref = pr.base.ref; position > 1; position--) {
      const below = await openPrWithHead(ref);
      if (!below) return;
      const { data: comparison } = await github.rest.repos.compareCommitsWithBasehead({
        owner,
        repo,
        basehead: `${below.head.sha}...${pr.head.sha}`,
        per_page: 1,
      });
      if (['ahead', 'identical'].includes(comparison.status))
        await postStatus(below.head.sha, state, description, targetUrl);
      ref = below.base.ref;
    }
  };

  // reviewDecision applies the base branch's approval rules; null means none are required.
  const isApproved = async () => {
    const { repository } = await github.graphql(
      'query($owner: String!, $repo: String!, $number: Int!) { repository(owner: $owner, name: $repo) { pullRequest(number: $number) { reviewDecision } } }',
      { owner, repo, number: pr.number }
    );
    return [null, 'APPROVED'].includes(repository.pullRequest.reviewDecision);
  };

  // buildUrl is the build's run, which the statuses link to.
  const postStackResult = async (buildPassed, buildUrl) => {
    if (!buildPassed) return postBelow('failure', `PR build failed on #${pr.number}`, buildUrl);
    if (await isApproved())
      return postBelow('success', `PR build passed and approved on #${pr.number}`, buildUrl);
    return postBelow('pending', `Waiting for approval on #${pr.number}`, buildUrl);
  };

  return {
    async onBuildStart() {
      if (isLast) {
        await postStatus(pr.head.sha, 'success', 'Gated by its own checks');
        return postBelow('pending', `PR build running on #${pr.number}`);
      }
      // Keep any result the last PR already posted for this commit.
      const { data } = await github.rest.repos.getCombinedStatusForRef({
        owner,
        repo,
        ref: pr.head.sha,
      });
      if (!data.statuses.some((status) => status.context === stackContext)) {
        await postStatus(pr.head.sha, 'pending', 'Waiting for the last PR in the stack');
      }
    },

    // buildPassed: every job of the last PR's run, but the stack jobs, succeeded.
    // The payload is from the build's start: a PR may have joined the stack
    // above, or the PR a newer head, since. Then this result is not the
    // stack's, and a newer run's (or the new top's) is.
    async onBuildEnd(buildPassed) {
      const { data: now } = await github.rest.pulls.get({ owner, repo, pull_number: pr.number });
      if (now.head.sha !== pr.head.sha) return;
      if (now.stack && now.stack.position !== now.stack.size) return;
      return postStackResult(buildPassed);
    },

    // An approval during the build is read when the build ends; one after it
    // is read here, from the last PR's latest run.
    async onReview() {
      if (!pr.stack || !isLast) return;
      const {
        data: { workflow_runs: runs },
      } = await github.rest.actions.listWorkflowRuns({
        owner,
        repo,
        workflow_id: buildWorkflow,
        event: 'pull_request',
        head_sha: pr.head.sha,
        per_page: 1,
      });
      const run = runs[0];
      if (run?.status !== 'completed' || run.conclusion === 'cancelled') return;
      const { data } = await github.rest.actions.listJobsForWorkflowRun({
        owner,
        repo,
        run_id: run.id,
        filter: 'latest',
      });
      const built = data.jobs.some((job) => job.name === resultJob && job.conclusion !== 'skipped');
      if (built) await postStackResult(run.conclusion === 'success', run.html_url);
    },

    // Closing the last PR leaves the PR below it the top of the open ones, but
    // GitHub keeps a closed PR in its stack, so that PR's position stays below
    // the size and its heavy jobs keep skipping until the stack is dissolved
    // (Unstack) and linked again without the closed PR. Say so. A re-run will
    // not help: it replays the old payload, with the old position.
    async onClose() {
      const below = await openPrWithHead(pr.base.ref);
      const wasLast = pr.stack && pr.stack.position === pr.stack.size;
      if (!below?.stack || (below.stack.position !== below.stack.size && !wasLast)) return;
      const description = `#${pr.number} closed; unstack and link the stack again without it, then push to #${below.number} to build it`;
      await postStatus(below.head.sha, 'pending', description);
      await stackStatus({ github, context, pr: below }).postBelow('pending', description);
    },

    postBelow,
  };
};

module.exports = stackStatus;
module.exports.stackContext = stackContext;
module.exports.buildWorkflow = buildWorkflow;
module.exports.resultJob = resultJob;
