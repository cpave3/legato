# Legato

A board of tasks and the AI coding agents that work on them, run as one or more Legato instances.

## Language

### Instances and handoff

**Main instance**:
A Legato a person works from, which owns its tasks. A person may have several, such as a workstation and a laptop.
_Avoid_: home instance, local instance, source

**Worker instance**:
A Legato that works on tasks handed to it by main instances. One worker may serve several main instances.
_Avoid_: remote, VM, cloud instance, delegate

**Instance ID**:
The identity of one Legato, made once and never changed. Other instances know it by this, not by its name.
_Avoid_: instance name, server name

**Handoff**:
A main instance sending one of its tasks to a worker instance to be worked on there.
_Avoid_: delegation, push, offload

**Make-a-change handoff**:
A handoff whose agent changes the code and ends in pull requests.
_Avoid_: fix handoff

**Find-out handoff**:
A handoff whose agent answers a question in notes and pushes nothing.
_Avoid_: investigation, read-only handoff

**Away**:
The state of a task on its main instance while a worker instance holds it.
_Avoid_: delegated, remote

**Recall**:
A main instance taking an away task back before the worker has finished with it.

**Return**:
An away task coming back to its main instance with what the worker did.

**Take back**:
A main instance ending an away task by itself, without the worker's results, when the worker cannot be reached or no longer has the task.
_Avoid_: force recall, abandon

### Agents on a task

**Agent slot**:
A place on a task for one agent at a time, with its own terminal session, execution and saved conversation. A task has a main slot and may have named slots.
_Avoid_: agent seat, lane, second agent

**Main slot**:
The slot of a task's own agent: the handoff's author, and any session the person starts. Anything that addresses "the task's agent" without naming a slot means it.
_Avoid_: primary agent, default agent

**Named slot**:
A slot other than the main one, such as `reviewer`. Only Legato starts agents in one; the person can attach to or stop them.
_Avoid_: extra slot, side agent

### Review passes

**Review pass**:
One review of all of a handoff's pull requests by a fresh reviewer, at heads pinned when it starts, ending in a GitHub review per pull request.
_Avoid_: review round, review cycle, auto-review, automated review, review tour

**Reviewer**:
The agent in a task's reviewer slot that does a review pass, on a different model from the author, and only looks.
_Avoid_: review agent, critic

**Finding**:
One problem a reviewer reports on one pull request, kept with a stable ID across passes.
_Avoid_: comment, issue

**Blocking**:
A finding that must be fixed or answered before the work is done.

**Nit**:
A finding that is optional: the author may skip it.

**Ruling**:
A later pass's judgement on an earlier blocking finding: resolved, or open with why.
_Avoid_: verdict (the whole pass's outcome)

**Clean pass**:
A review pass with no blocking finding and no open ruling. It approves the pull requests.
_Avoid_: green review, LGTM

**Counted comment**:
A comment on a pull request that the author must answer: from the repository's owners, members or collaborators, or from a listed bot.
_Avoid_: feedback comment, external feedback

**Counted check**:
A check a pull request must pass before its handoff is done: one branch protection requires on its base, else one the worker lists.
_Avoid_: required CI, gate

**Feedback item**:
One thing the author must handle after a review pass: a finding, a counted comment or a failing counted check, each with its ID and the author's answer.

**Reviewer App**:
The second GitHub App a worker posts reviews as. Only Legato holds its credential; no agent is given it.

### Engrams

**Engram**:
One named individual kept by the engram CLI, with its own memories, open threads and handoffs, that an agent session can be started as. The CLI itself is "the engram CLI".
_Avoid_: identity, persona, profile

### Neighbouring terms

**Delegation**:
A request from Argus that a Legato fix or find out something about a case. Not a handoff.

**Remote ticket**:
A task that comes from a ticket provider such as Jira or Burnlite. Not a task on a worker instance.
