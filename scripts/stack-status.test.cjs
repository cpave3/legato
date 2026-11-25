// Tests of stack-status.cjs against a fake GitHub client. Run with
// `node --test scripts/stack-status.test.cjs` (cmd/legato's
// TestStackStatusScript runs them with the Go suite).
const test = require('node:test');
const assert = require('node:assert/strict');
const stackStatus = require('./stack-status.cjs');

const context = { repo: { owner: 'o', repo: 'r' }, serverUrl: 'https://github.com', runId: 7 };

// prs are the open pull requests: { number, head: { ref, sha }, base: { ref }, stack }.
// compare maps 'below...top' to its status ('ahead' unless named).
const fakeGithub = ({ prs = [], compare = {}, statuses = {}, reviewDecision = null, runs = [], jobs = {} } = {}) => {
  const posted = [];
  const github = {
    posted,
    rest: {
      repos: {
        createCommitStatus: async (args) => posted.push(args),
        compareCommitsWithBasehead: async ({ basehead }) => ({ data: { status: compare[basehead] || 'ahead' } }),
        getCombinedStatusForRef: async ({ ref }) => ({ data: { statuses: statuses[ref] || [] } }),
      },
      pulls: {
        // The PR as GitHub has it now: fresh, from prs.
        get: async ({ pull_number }) => {
          const found = prs.find((pr) => pr.number === pull_number);
          if (!found) throw Object.assign(new Error('Not Found'), { status: 404 });
          return { data: found };
        },
        list: async ({ head, state }) => {
          assert.equal(state, 'open');
          return { data: prs.filter((pr) => `o:${pr.head.ref}` === head) };
        },
      },
      actions: {
        listWorkflowRuns: async (args) => {
          github.runsAsked = args;
          return { data: { workflow_runs: runs } };
        },
        listJobsForWorkflowRun: async ({ run_id }) => ({ data: { jobs: jobs[run_id] || [] } }),
      },
    },
    graphql: async () => ({ repository: { pullRequest: { reviewDecision } } }),
  };
  return github;
};

const pr = (number, position, size, base) => ({
  number,
  head: { ref: `b${number}`, sha: `s${number}` },
  base: { ref: base ?? (number > 1 ? `b${number - 1}` : 'main') },
  stack: position ? { position, size } : null,
});

// A stack of three: #1 on main, #2 on #1, #3 on #2.
const stackOf3 = () => [pr(1, 1, 3), pr(2, 2, 3), pr(3, 3, 3)];

const states = (github) => github.posted.map((s) => [s.sha, s.state]);

test('an unstacked PR passes ci/stack on itself only', async () => {
  const github = fakeGithub();
  await stackStatus({ github, context, pr: pr(9, null, null, 'main') }).onBuildStart();
  assert.deepEqual(states(github), [['s9', 'success']]);
  assert.equal(github.posted[0].context, 'ci/stack');
  assert.equal(github.posted[0].target_url, 'https://github.com/o/r/actions/runs/7');
});

test('the top PR passes itself and marks the PRs below pending', async () => {
  const github = fakeGithub({ prs: stackOf3() });
  await stackStatus({ github, context, pr: pr(3, 3, 3) }).onBuildStart();
  assert.deepEqual(states(github), [
    ['s3', 'success'],
    ['s2', 'pending'],
    ['s1', 'pending'],
  ]);
  assert.equal(github.posted[1].description, 'PR build running on #3');
});

test('a lower PR with no ci/stack yet waits for the top', async () => {
  const github = fakeGithub({ prs: stackOf3() });
  await stackStatus({ github, context, pr: pr(2, 2, 3) }).onBuildStart();
  assert.deepEqual(states(github), [['s2', 'pending']]);
  assert.equal(github.posted[0].description, 'Waiting for the last PR in the stack');
});

test('a lower PR keeps a ci/stack the top already posted on its head', async () => {
  const github = fakeGithub({
    prs: stackOf3(),
    statuses: { s2: [{ context: 'ci/stack', state: 'success' }] },
  });
  await stackStatus({ github, context, pr: pr(2, 2, 3) }).onBuildStart();
  assert.deepEqual(states(github), []);
});

test('the walk down skips a PR the top build does not contain, and goes on below it', async () => {
  const github = fakeGithub({ prs: stackOf3(), compare: { 's2...s3': 'diverged' } });
  await stackStatus({ github, context, pr: pr(3, 3, 3) }).onBuildStart();
  assert.deepEqual(states(github), [
    ['s3', 'success'],
    ['s1', 'pending'],
  ]);
});

test('the walk down stops at a base with no open PR', async () => {
  // The payload still counts three, but #1 is no longer open.
  const github = fakeGithub({ prs: [pr(2, 2, 3), pr(3, 3, 3)] });
  await stackStatus({ github, context, pr: pr(3, 3, 3) }).onBuildStart();
  assert.deepEqual(states(github), [
    ['s3', 'success'],
    ['s2', 'pending'],
  ]);
});

for (const reviewDecision of [null, 'APPROVED']) {
  test(`a passed build passes the PRs below when the review decision is ${reviewDecision}`, async () => {
    const github = fakeGithub({ prs: stackOf3(), reviewDecision });
    await stackStatus({ github, context, pr: pr(3, 3, 3) }).onBuildEnd(true);
    assert.deepEqual(states(github), [
      ['s2', 'success'],
      ['s1', 'success'],
    ]);
    assert.equal(github.posted[0].description, 'PR build passed and approved on #3');
  });
}

test('a passed build that still needs approval leaves the PRs below pending', async () => {
  const github = fakeGithub({ prs: stackOf3(), reviewDecision: 'REVIEW_REQUIRED' });
  await stackStatus({ github, context, pr: pr(3, 3, 3) }).onBuildEnd(true);
  assert.deepEqual(states(github), [
    ['s2', 'pending'],
    ['s1', 'pending'],
  ]);
  assert.equal(github.posted[0].description, 'Waiting for approval on #3');
});

test('a failed build fails the PRs below, approved or not', async () => {
  const github = fakeGithub({ prs: stackOf3(), reviewDecision: 'APPROVED' });
  await stackStatus({ github, context, pr: pr(3, 3, 3) }).onBuildEnd(false);
  assert.deepEqual(states(github), [
    ['s2', 'failure'],
    ['s1', 'failure'],
  ]);
  assert.equal(github.posted[0].description, 'PR build failed on #3');
});

test('a passed build posts nothing below a head it does not contain', async () => {
  const github = fakeGithub({ prs: stackOf3(), compare: { 's1...s3': 'behind' } });
  await stackStatus({ github, context, pr: pr(3, 3, 3) }).onBuildEnd(true);
  assert.deepEqual(states(github), [['s2', 'success']]);
});

test('a review of an unstacked or a lower PR changes nothing', async () => {
  for (const reviewed of [pr(9, null, null, 'main'), pr(2, 2, 3)]) {
    const github = fakeGithub({ prs: stackOf3() });
    await stackStatus({ github, context, pr: reviewed }).onReview();
    assert.deepEqual(states(github), []);
    assert.equal(github.runsAsked, undefined);
  }
});

test("a review of the top PR posts its last build's result below", async () => {
  const github = fakeGithub({
    prs: stackOf3(),
    runs: [{ id: 40, status: 'completed', conclusion: 'success', html_url: 'https://github.com/o/r/actions/runs/40' }],
    jobs: { 40: [{ name: 'stack-status', conclusion: 'success' }] },
  });
  await stackStatus({ github, context, pr: pr(3, 3, 3) }).onReview();
  assert.deepEqual(states(github), [
    ['s2', 'success'],
    ['s1', 'success'],
  ]);
  // The link leads to the build, not to the review's run.
  assert.equal(github.posted[0].target_url, 'https://github.com/o/r/actions/runs/40');
  assert.equal(github.runsAsked.workflow_id, 'ci.yml');
  assert.equal(github.runsAsked.head_sha, 's3');
  assert.equal(github.runsAsked.event, 'pull_request');
});

test("a review of the top PR posts a failed build's failure below", async () => {
  const github = fakeGithub({
    prs: stackOf3(),
    runs: [{ id: 40, status: 'completed', conclusion: 'failure' }],
    jobs: { 40: [{ name: 'stack-status', conclusion: 'success' }] },
  });
  await stackStatus({ github, context, pr: pr(3, 3, 3) }).onReview();
  assert.deepEqual(states(github), [
    ['s2', 'failure'],
    ['s1', 'failure'],
  ]);
});

test('a review of the top PR waits for a build that has not finished or did not post', async () => {
  const cases = [
    { runs: [] },
    { runs: [{ id: 40, status: 'in_progress', conclusion: null }] },
    { runs: [{ id: 40, status: 'completed', conclusion: 'cancelled' }] },
    // A run from before the PR joined the stack: its stack-status skipped.
    {
      runs: [{ id: 40, status: 'completed', conclusion: 'success' }],
      jobs: { 40: [{ name: 'stack-status', conclusion: 'skipped' }] },
    },
  ];
  for (const c of cases) {
    const github = fakeGithub({ prs: stackOf3(), ...c });
    await stackStatus({ github, context, pr: pr(3, 3, 3) }).onReview();
    assert.deepEqual(states(github), [], JSON.stringify(c));
  }
});

test('closing the top PR says what will build the new top', async () => {
  // GitHub may not have dropped #3 from the stack yet: #2 can still say 2 of 3.
  for (const below of [pr(2, 2, 3), pr(2, 2, 2)]) {
    const github = fakeGithub({ prs: [pr(1, 1, 3), below] });
    await stackStatus({ github, context, pr: pr(3, 3, 3) }).onClose();
    assert.deepEqual(states(github), [
      ['s2', 'pending'],
      ['s1', 'pending'],
    ]);
    // GitHub keeps a closed PR in its stack, so #2 is not the top until the stack is made again.
    assert.equal(
      github.posted[0].description,
      '#3 closed; unstack and link the stack again without it, then push to #2 to build it'
    );
  }
});

test('closing a PR below the top, or one with nothing below, changes nothing', async () => {
  for (const [open, closed] of [
    [[pr(1, 1, 3), pr(3, 3, 3)], pr(2, 2, 3)],
    [[pr(2, 2, 3), pr(3, 3, 3)], pr(1, 1, 3)],
    [[], pr(9, null, null, 'main')],
  ]) {
    const github = fakeGithub({ prs: open });
    await stackStatus({ github, context, pr: closed }).onClose();
    assert.deepEqual(states(github), [], `closing #${closed.number}`);
  }
});

test("a former top's build posts nothing once the stack has grown above it", async () => {
  // #3 was the top of two when its build started; #4 has joined since.
  const started = pr(3, 2, 2);
  const github = fakeGithub({ prs: [pr(2, 1, 3), pr(3, 2, 3), pr(4, 3, 3)] });
  await stackStatus({ github, context, pr: started }).onBuildEnd(true);
  assert.deepEqual(states(github), []);
});

test("a top's build posts nothing once the top has a newer head", async () => {
  const pushed = { ...pr(3, 3, 3), head: { ref: 'b3', sha: 's3-new' } };
  const github = fakeGithub({ prs: [pr(1, 1, 3), pr(2, 2, 3), pushed] });
  await stackStatus({ github, context, pr: pr(3, 3, 3) }).onBuildEnd(false);
  assert.deepEqual(states(github), []);
});
