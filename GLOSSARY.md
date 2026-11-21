# Legato

A board of tasks and the AI coding agents that work on them, run as one or more Legato instances.

## Language

### People

**Operator**:
The human who runs a Legato and works with its agents: the one who starts, messages, reloads and approves. Every text Legato gives an agent (messages, prompts, tool guidance, refusals) names them so.
_Avoid_: the person, the user, the human

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

**Interrupted agent**:
An agent that was running when Legato last stopped and whose tmux session was gone when it started again, because the machine or tmux went down. It is kept until it is resumed, replaced or dismissed. An agent whose session ends while Legato runs has ended, not been interrupted.
_Avoid_: crashed agent, lost agent, dead agent

**Conversation**:
One provider conversation of one agent tool, named by its session ID: a Claude Code session, a Codex thread, a Chimera session. A conversation has one or more runs.
_Avoid_: transcript (the tool's file of it), chat

**Run**:
One execution a conversation was live in. A conversation resumed three times has three runs. A run usually has one conversation; `/clear`, `/resume` or `/background` give it several, each ending the last as replaced.

**Session history**:
The list of conversations Legato knows on this instance, each with its runs, kept after its agent and its task are gone.
_Avoid_: agent history, past agents

**Operator item**:
Something an agent posts with `for_operator` for the operator to see or act on: an action only the operator can do, a question, finished work, or anything else that needs them. It is an Attention item. Every kind counts as needing the operator except finished work (a done note), which puts the agent in Finished.
_Avoid_: ask, request, notification

**Stage**:
Where an agent's work stands: Exploring, Planning, Implementing, Reviewing or Finished. Legato infers it for each agent place (a task's slot, or a workflow agent) from what the operator sets, what the agent reports, milestones such as a plan approved or a pull request merged, and edits. It is separate from what the agent is doing now and from the card's column. The Reeve has none.
_Avoid_: progress, phase, status

**Glance**:
The group every list of agents sorts an agent into: Needs you, Your turn (idle mid-work, nothing blocking), Working, or Finished. It is worked out on the server from the agent's activity, open operator items, pending plans and stage.
_Avoid_: section, bucket

**Needs-you rail**:
The column on the right of every web page that shows Attention, filtered by the rail settings, with approvals first.
_Avoid_: sidebar, inbox, tray

### Review passes

**Review loop**:
The review passes on one set of pull requests, from the first pass until it is approved, goes stuck or is ended. A handoff or a workflow run starts it.
_Avoid_: review cycle, review session

**Author**:
Whoever fixes and answers a review loop's findings: the branch's owner, or a board task's agent when asked.
_Avoid_: developer, PR owner

**Review pass**:
One review of a review loop's pull requests by the reviewer, at heads pinned when it starts, ending in a GitHub review per pull request. The first reviews them in full; a later one reviews what changed since the pass before and rules on the earlier blocking findings.
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
A check a pull request must pass before its handoff's review loop ends: one branch protection requires on its base, else one the worker lists.
_Avoid_: required CI, gate

**Feedback item**:
One thing the author must handle after a review pass: a finding, a counted comment or a failing counted check, each with its ID and the author's answer.

**Reviewer App**:
The second GitHub App a worker posts reviews as. Only Legato holds its credential; no agent is given it.

### Running Legato

**Reeve**:
The agent of a Legato instance itself, which helps the person run and configure that instance rather than work on a task. Each instance has its own.
_Avoid_: Legato agent, admin agent, management agent, steward, operator

**Reeve prime**:
The Reeve of the instance the person is working from at the moment. It reaches the Reeves of instances paired with its own, and lets them reach it back.
_Avoid_: master Reeve, home Reeve, main Reeve

**Reeve pairing**:
A pairing by phrase between two instances that lets their Reeves message each other, for instances that are not already a main and a worker.
_Avoid_: Reeve link, peer pairing

**Change set**:
The changes the Reeve proposes together for one step, approved or denied as one and carried out by Legato in order.
_Avoid_: batch, plan, transaction

**Pinned setting**:
A setting, or one entry of a list, whose value comes from the instance's `config.yaml`. Only an edit to that file changes it; the web UI and the Reeve show it as pinned.
_Avoid_: locked setting, configured setting, read-only setting

**File-only setting**:
A setting that lives only in `config.yaml` and never in the database: what the instance needs to start, to unseal its secrets, to be reached, and to trust a signature.
_Avoid_: bootstrap setting, static setting

**Check**:
One thing `legato doctor` verifies about the machine Legato runs on, with its result (ok, missing, not signed in, failed) and the fix. A **required** check failing means part of Legato cannot work.
_Avoid_: prerequisite, health check, probe

### Engrams

**Engram**:
One named individual kept by the engram CLI, with its own memories, open threads and handoffs, that an agent session can be started as. The CLI itself is "the engram CLI".
_Avoid_: identity, persona, profile

**Place key**:
How engram names where a session runs: `repo:<host>/<path>` for a repository, `dir:<path>` for a folder. Engram bindings are kept under it.
_Avoid_: scope (engram's word for any key a record can carry)

**Engram binding**:
An engram bound to a place key, so the spawn dialog lists it under "This repo" (or "This folder") there. Made by ticking Remember; kept in the database only.

**Default engram**:
The engram bound to a place that is pre-selected there when the task has no engram of its own. At most one per place.

### Legato Scope

**Pin**:
A comment the operator places on an element of a live page, with the context captured there (the page, selectors, styles, a screenshot, the component source when known).
_Avoid_: annotation, mark. Not a pinned setting

**Batch**:
The pins sent together with one press of Send.

**Pool**:
The sent pins a Legato instance keeps, by origin, until agents handle them.

**Origin subscription**:
A task slot's claim on an origin (such as `http://localhost:5173`), so that batches sent from it are pushed to that slot's agent. One slot holds an origin at a time; the newest subscriber takes over.
_Avoid_: scope, which means swarm file-ownership globs

### Automations

**Macro**:
A named, editable piece of text the operator sends to an agent. It can be sent to a running agent as it is, or started from a card or a pull request, where it also starts the task's agent if none is running.
_Avoid_: Action, saved prompt, recipe

**Due time**:
The moment a schedule says a workflow should run. Each due time makes at most one run.

**Missed run**:
A due time that passed while no Legato was running.

**Loop stop**:
The limit that pauses a trigger which has started the same workflow for the same item too often, and asks the operator.

**Workflow graph**:
The drawing of a workflow's triggers and steps, built from its definition. It is read-only; the YAML is the source of truth.
_Avoid_: flow chart, diagram, canvas

**Gate**:
A step that does not start by itself: one that waits for the operator (`trigger: manual`) or for the agent before it to end (`trigger: agent_ended`).

### Neighbouring terms

**Delegation**:
A request from Argus that a Legato fix or find out something about a case. Not a handoff.

**Remote ticket**:
A task that comes from a ticket provider such as Jira or Burnlite. Not a task on a worker instance.
