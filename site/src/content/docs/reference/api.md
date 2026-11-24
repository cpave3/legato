---
title: API
description: Every route of Legato's web API, with its method, its body and what the Reeve needs to call it. How to move a card, stop or kill an agent, or message an agent.
sidebar:
  order: 25
---

<!-- Written by go test ./internal/server -run TestTheAPIReferencePageIsCurrent -update from the route table. Do not edit. -->

The web UI, the phone and the Reeve all use this API. Each call carries the web token as `Authorization: Bearer <token>` (see [Web authentication](/legato/security/web-auth/)). Bodies are JSON.

The **class** says what a call needs when the Reeve makes it with its `legato_api` tool:

- **read** runs at once. Every `GET` is a read, except the few marked otherwise below. A route Legato does not serve answers 404.
- **change** and **risky** become a change set for you to approve. A risky one can need a signed approval. See [The Reeve](/legato/using/reeve/).
- **never** is refused, whatever you approve.

A card on the board is a task: its ID is the task's. An agent is addressed by its task's ID.

## Everyday calls

| To | Call | Body |
| --- | --- | --- |
| Read the board: its columns and cards | `GET /api/board` |  |
| Read a task in brief: its column, pull requests and latest note | `GET /api/tasks/<id>?view=summary` |  |
| Read a task in full, with its description and notes | `GET /api/tasks/<id>` |  |
| Read a task's notes, each in full, with its source and time | `GET /api/tasks/<id>/notes` |  |
| List a task's plans, newest first | `GET /api/tasks/<id>/plans` |  |
| Read a plan: its text, revision and status | `GET /api/plans/<plan_id>` |  |
| Create a task (a card) | `POST /api/tasks` | `{"title": "…", "column": "Backlog"}` |
| Move a card to another column | `PATCH /api/tasks/<id>` | `{"column": "Done"}` |
| Put a card just above another in its column (`null` for last) | `PATCH /api/tasks/<id>` | `{"before": "LEG-12"}` |
| Rename a task or change its description | `PATCH /api/tasks/<id>` | `{"title": "…", "description": "…"}` |
| Archive a done task | `POST /api/tasks/<id>/archive` |  |
| Link a pull request to a task | `POST /api/tasks/<id>/link-pr` | `{"owner": "…", "repo": "…", "pr_number": 12}` |
| List the agents | `GET /api/agents` |  |
| Check on some agents in brief | `GET /api/agents?view=compact&status=running&task=<id>,<id>&workspace=<name>` |  |
| List the agents at some stages | `GET /api/agents?stage=reviewing,finished` |  |
| Set an agent's stage | `PUT /api/agents/<id>/stage` | `{"stage": "finished", "reason": "…"}` |
| Start an agent on a task | `POST /api/tasks/<id>/agent` | `{"agent_kind": "claude-code", "prompt": "…"}` |
| Message an agent that is running | `POST /api/tasks/<id>/agent` | `{"prompt": "…"}` |
| Start a macro on a task (and one of its pull requests) | `POST /api/tasks/<id>/macro` | `{"name": "Review", "pr": {"repo": "owner/name", "pr_number": 12}}` |
| Read what an agent said and did last | `GET /api/agents/<id>/conversation` |  |
| Stop an agent | `POST /api/agents/kill` | `{"task_id": "<id>"}` |
| Restart an agent, keeping its conversation | `POST /api/agents/<id>/restart` |  |
| List what needs attention | `GET /api/attention` |  |
| List what the needs-you rail shows, by its settings | `GET /api/attention?view=rail` |  |
| Change a setting | `PUT /api/config/settings/<key>` | `{"value": …}` |
| List the workers this Legato hands tasks to | `GET /api/settings/workers` |  |
| Read what a worker's review passes can do | `GET /api/settings/workers/<instance_id>/review-passes` |  |
| Hand a task to a worker, with its review passes | `POST /api/tasks/<id>/handoff` | `{"worker": "<instance_id>", "purpose": "make-change", "kind": "new-branch", "branch": "…", "repo": "owner/name", "agent_kind": "claude-code", "review": {"enabled": true, "agent": "codex", "model": "…", "tier": "…", "skill": "…", "max_passes": 3}}` |

## Handing a task to a worker

`POST /api/tasks/<id>/handoff` sends a task to a worker, as the board's **Hand off** form does, and answers `201` with `{"away": …}`. Its body:

- `worker`: the worker's instance ID, from `GET /api/settings/workers`.
- `purpose`: `make-change` or `find-out`.
- `kind`: `new-branch`, or `pushed-branch` with the branch on GitHub in `branch`.
- `repo`: the repository, `owner/name`.
- `agent_kind`: the author's agent tool; empty for the worker's default.
- `review` (make-change only, optional): what changes of the worker's review passes for this handoff. Leave it out to keep the worker's settings. `enabled` turns them on or off; `agent` is the reviewer's tool, with `model` or one of its `tier`s; `skill` and `max_passes` (at least 1). A field left out keeps the worker's. `GET /api/settings/workers/<instance_id>/review-passes` says the worker's settings and the tools, tiers and skills it has; anything else is refused with `422` in the worker's words.

## Tasks and cards

| Method | Route | Class | What it does | Body |
| --- | --- | --- | --- | --- |
| `GET` | `/api/tasks` | read | List the tasks |  |
| `POST` | `/api/tasks` | change | Create a task | `{"title": "…", "description": "…", "column": "Backlog", "priority": "", "workspace_id": 1}` |
| `GET` | `/api/tasks/search` | read | Search the tasks |  |
| `POST` | `/api/tasks/{id}/archive` | change | Archive a task |  |
| `POST` | `/api/tasks/{id}/restore` | change | Restore an archived task |  |
| `GET` | `/api/tasks/archived` | read | List the archived tasks |  |
| `GET` | `/api/tasks/{id}/activity` | read | Read a task's activity |  |
| `GET` | `/api/tasks/{id}/notes` | read | Read a task's notes in full |  |
| `GET` | `/api/tasks/{id}/plans` | read | List a task's plans |  |
| `GET` | `/api/tasks/{id}/stacks` | read | List a task's branch stacks |  |
| `GET` | `/api/tasks/{id}/stacks/{clone}/document` | read | Read a branch stack's import file |  |
| `POST` | `/api/tasks/{id}/agent` | change | Start, resume or message a task's agent | `{"agent_kind": "claude-code", "tier": "…", "prompt": "…", "working_dir": "…", "repo": "owner/name", "resources": ["…"]}` |
| `POST` | `/api/tasks/{id}/macro` | change | Start a macro on a task: its agent starts or is messaged | `{"name": "Review", "pr": {"repo": "owner/name", "pr_number": 12, "url": "…"}}` |
| `POST` | `/api/tasks/{id}/worktree` | change | Create a worktree for a task |  |
| `GET` | `/api/tasks/{id}/artifacts` | read | List a task's artifacts |  |
| `POST` | `/api/tasks/{id}/link-pr` | change | Link a pull request to a task | `{"owner": "…", "repo": "…", "pr_number": 12}` |
| `GET` | `/api/tasks/{id}/pr-preview` | read | Preview a pull request |  |
| `POST` | `/api/tasks/{id}/unlink-pr` | change | Unlink a task's pull request |  |
| `PUT` | `/api/tasks/{id}/code-context` | change | Set a task's repository and branch |  |
| `POST` | `/api/tasks/{id}/refresh-pr` | change | Refresh a task's pull request |  |
| `POST` | `/api/tasks/{id}/rediscover-pr` | change | Find a task's pull request again |  |
| `GET` | `/api/tasks/{id}` | read | Read a task |  |
| `PATCH` | `/api/tasks/{id}` | change | Edit or move a task | `{"title": "…", "description": "…", "column": "Done", "before": "LEG-12", "workspace_id": 1}` |
| `DELETE` | `/api/tasks/{id}` | risky | Delete a task |  |
| `POST` | `/api/tasks/{id}/handoff` | change | Hand a task to a worker | `{"worker": "<instance_id>", "purpose": "make-change", "kind": "new-branch", "branch": "…", "repo": "owner/name", "agent_kind": "claude-code", "review": {"enabled": true, "agent": "codex", "model": "…", "tier": "…", "skill": "…", "max_passes": 3}}` |
| `POST` | `/api/tasks/{id}/return` | change | Return a finished task from its worker |  |
| `POST` | `/api/tasks/{id}/recall` | risky | Recall a task from its worker, stopping its agents |  |
| `POST` | `/api/tasks/{id}/take-back` | risky | Take a task back without its results |  |
| `POST` | `/api/tasks/{id}/away/review/{action}` | change | Act on an away task's review passes |  |

## Agents

| Method | Route | Class | What it does | Body |
| --- | --- | --- | --- | --- |
| `GET` | `/api/agents` | read | List the agents |  |
| `POST` | `/api/agents/spawn` | change | Start an agent | `{"title": "…", "agent_kind": "claude-code", "tier": "…", "working_dir": "…", "prompt": "…"}` |
| `POST` | `/api/agents/kill` | risky | Stop an agent | `{"task_id": "<id>"}` |
| `PATCH` | `/api/agents/{task_id}/notify` | change | Turn an agent's notifications on or off | `{"enabled": true}` |
| `PUT` | `/api/agents/{task_id}/group` | change | Put an agent's task in a group, or take it out | `{"group": "…"}` |
| `PUT` | `/api/agents/{task_id}/stage` | change | Set an agent's stage | `{"stage": "finished", "reason": "…"}` |
| `GET` | `/api/agents/{task_id}/stage-history` | read | Read an agent's stage history |  |
| `GET` | `/api/agents/{task_id}/terminal` | never | Type into an agent's terminal |  |
| `GET` | `/api/preparing-agents` | read | List the agents being prepared |  |
| `DELETE` | `/api/preparing-agents/{task_id}` | change | Dismiss an agent that failed to start |  |
| `GET` | `/api/agents/{task_id}/resources` | read | List an agent's resources |  |
| `POST` | `/api/agents/{task_id}/resources` | risky | Give an agent resources, restarting it if needed |  |
| `POST` | `/api/agents/{task_id}/restart` | risky | Restart an agent | `{"switch_sandbox": false}` |
| `POST` | `/api/agents/{task_id}/compact` | change | Compact an agent's context with its tool's own compaction, between turns; Claude Code also takes focus instructions | `{"instructions": "…"}` |
| `GET` | `/api/agents/interrupted` | read | List the agents a crash interrupted |  |
| `POST` | `/api/agents/interrupted/resume-all` | change | Resume every interrupted agent |  |
| `POST` | `/api/agents/{task_id}/resume` | change | Resume an interrupted agent, or start a new one in its place |  |
| `POST` | `/api/agents/{task_id}/dismiss` | change | Dismiss an interrupted agent |  |
| `GET` | `/api/agents/{task_id}/preview` | read | Preview an agent's terminal |  |
| `GET` | `/api/agents/{task_id}/conversation` | read | Read an agent's latest conversation |  |
| `GET` | `/api/agents/{task_id}/related` | read | List the agents on the other side of an agent's review loops |  |
| `GET` | `/api/agents/graph` | read | Read the agents' graph: each running agent's repository, worktree, badges and place in its active workflow runs, those runs, and the links between agents (recent peer messages, review runs, the Reeve's watches) |  |
| `GET` | `/api/agents/{task_id}/image` | read | Read an image an agent read |  |
| `POST` | `/api/agents/{task_id}/image` | change | Store a pasted image for an agent |  |
| `GET` | `/api/agents/{task_id}/ui-items` | read | List what an agent showed |  |
| `GET` | `/api/ui-items/{id}` | read | Read something an agent showed |  |
| `DELETE` | `/api/ui-items/{id}` | risky | Delete something an agent showed |  |
| `GET` | `/api/ui-items/{id}/studio/{rest...}` | read | Read from a studio |  |
| `POST` | `/api/ui-items/{id}/studio/{rest...}` | change | Change a studio's storyboard |  |
| `GET` | `/api/agents/{task_id}/review-briefing` | read | List an agent's review briefings |  |
| `GET` | `/api/agents/{task_id}/review-briefing/page` | read | Read a review briefing page |  |
| `GET` | `/agent-ui-images/` | read | Read an image an agent showed |  |
| `GET` | `/agent-ui-videos/` | read | Read a video an agent showed |  |
| `POST` | `/api/agents/{task_id}/chimera/discover` | change | Connect to an agent's native session |  |
| `POST` | `/api/agents/{task_id}/chimera/downgrade` | change | Switch an agent to its terminal |  |
| `GET` | `/api/agents/{task_id}/chimera/snapshot` | read | Read an agent's conversation |  |
| `POST` | `/api/agents/{task_id}/chimera/send` | change | Send a message to an agent | `{"content": "…"}` |
| `POST` | `/api/agents/{task_id}/chimera/interrupt` | risky | Interrupt an agent |  |
| `POST` | `/api/agents/{task_id}/chimera/permission` | never | Answer an agent's permission request |  |
| `GET` | `/api/agents/{task_id}/chimera/modes` | read | List an agent's modes |  |
| `POST` | `/api/agents/{task_id}/chimera/modes` | risky | Change an agent's mode |  |
| `GET` | `/api/agents/{task_id}/chimera/commands` | read | List an agent's commands |  |
| `GET` | `/api/agents/{task_id}/chimera/skills` | read | List an agent's skills |  |
| `POST` | `/api/agents/{task_id}/chimera/bang` | never | Run a shell command in an agent's session |  |
| `GET` | `/api/agents/{task_id}/chimera/model` | read | Read an agent's model |  |
| `POST` | `/api/agents/{task_id}/chimera/model` | change | Change an agent's model |  |
| `GET` | `/api/agents/{task_id}/chimera/models` | read | List the models an agent can use |  |

## Board

| Method | Route | Class | What it does | Body |
| --- | --- | --- | --- | --- |
| `GET` | `/api/board` | read | Read the board |  |
| `POST` | `/api/board/sync` | change | Sync the board with its ticket providers |  |
| `POST` | `/api/board/archive-done` | risky | Archive every done task |  |
| `GET` | `/api/workspaces` | read | List the workspaces |  |
| `GET` | `/api/remote/search` | read | Search remote tickets |  |
| `POST` | `/api/remote/import` | change | Import a remote ticket |  |
| `GET` | `/api/repo/detect` | read | Detect the current repository |  |
| `GET` | `/api/ticket-providers` | read | List the ticket providers |  |

## Attention and reports

| Method | Route | Class | What it does | Body |
| --- | --- | --- | --- | --- |
| `GET` | `/api/navigation-summary` | read | Read the navigation counts |  |
| `GET` | `/api/attention` | read | List what needs attention |  |
| `GET` | `/api/report` | read | Read the time report |  |
| `POST` | `/api/attention/{id}/actions/{action_id}` | change | Take an attention item's action |  |
| `POST` | `/api/attention/{id}/dismiss` | change | Dismiss an attention item |  |

## The Reeve

| Method | Route | Class | What it does | Body |
| --- | --- | --- | --- | --- |
| `GET` | `/api/reeve` | read | List the Reeve's sessions |  |
| `POST` | `/api/reeve/sessions` | change | Start a Reeve session |  |
| `POST` | `/api/reeve/sessions/{task_id}/end` | change | End a Reeve session |  |
| `POST` | `/api/reeve/sessions/{task_id}/resume` | change | Resume a Reeve session |  |
| `DELETE` | `/api/reeve/sessions/{task_id}` | risky | Delete a Reeve session |  |
| `POST` | `/api/reeve/message` | change | Send the Reeve a message | `{"text": "…"}` |
| `GET` | `/api/reeve/sessions/{task_id}/change-sets` | read | List a Reeve session's change sets |  |
| `GET` | `/api/reeve/auto` | read | Show what the Reeve runs without asking, and the sets that ran so |  |
| `POST` | `/api/reeve/auto/off` | change | Turn off everything the Reeve runs without asking (only makes it ask more) |  |
| `POST` | `/api/reeve/secrets/{approval_id}` | never | Store a secret the operator pasted |  |
| `GET` | `/api/reeve/voice` | read | Check whether a spoken conversation with the Reeve will work here |  |
| `GET` | `/api/reeve/voice/lines/{id}` | read | Fetch the audio of a line the Reeve said, for the device listening |  |
| `GET` | `/api/reeve/voice/session` | never | Talk to the Reeve: stream the microphone, hear its answers |  |
| `GET` | `/api/reeve/engram` | read | Show the Reeve's engram: its seed, memories and open threads |  |
| `PUT` | `/api/reeve/engram/seed` | risky | Change the Reeve's seed (one of its own rules: needs a signature) |  |
| `DELETE` | `/api/reeve/engram/memories/{id}` | risky | Delete one of the Reeve's memories for good |  |
| `DELETE` | `/api/reeve/engram/threads/{id}` | risky | Close one of the Reeve's open threads |  |
| `GET` | `/api/reeve/watches` | read | List the agents the Reeve watches |  |
| `POST` | `/api/reeve/watches` | read | Watch an agent: the Reeve is told when it waits, goes idle, adds a note, opens a pull request or exits |  |
| `DELETE` | `/api/reeve/watches/{task_id}` | read | Stop watching an agent |  |
| `POST` | `/api/integrations/reeve/messages` | change | Message this instance's Reeve, as another instance's |  |
| `GET` | `/api/integrations/reeve/approvals` | read | List this instance's Reeve change sets waiting for its person, for a paired instance |  |
| `GET` | `/api/reeve/peers/approvals` | read | List paired instances' Reeve change sets waiting for their person |  |
| `GET` | `/api/reeve/peers` | read | List the instances whose Reeves this one can message |  |
| `POST` | `/api/reeve/peers` | risky | Pair with another instance's Reeve |  |
| `DELETE` | `/api/reeve/peers/{instance_id}` | risky | Remove a Reeve pairing |  |

## Approvals and devices

| Method | Route | Class | What it does | Body |
| --- | --- | --- | --- | --- |
| `GET` | `/api/approvals` | read | List the approval requests |  |
| `GET` | `/api/approvals/{id}` | read | Read an approval request |  |
| `POST` | `/api/approvals/{id}/decision` | never | Decide an approval request |  |
| `POST` | `/api/approvals/{id}/dismiss` | never | Dismiss an approval request |  |
| `POST` | `/api/approvals/{id}/challenge` | never | Get a challenge to sign an approval |  |
| `GET` | `/api/devices` | read | List the approval devices |  |
| `POST` | `/api/devices` | never | Enroll an approval device |  |
| `POST` | `/api/devices/challenge` | never | Get a challenge to enroll a device |  |
| `POST` | `/api/devices/passkey/options` | never | Get the options to enroll a passkey |  |
| `DELETE` | `/api/devices/{id}` | never | Remove an approval device |  |

## Credentials

| Method | Route | Class | What it does | Body |
| --- | --- | --- | --- | --- |
| `GET` | `/api/credentials` | read | List the credentials |  |
| `GET` | `/api/credentials/grants` | read | List the credential grants |  |
| `DELETE` | `/api/credentials/grants/{id}` | risky | Revoke a credential grant |  |
| `GET` | `/api/credentials/audit` | read | Read the credential audit log |  |
| `GET` | `/api/credentials/audit/verify` | read | Verify the credential audit log |  |

## Plans, reviews and learnings

| Method | Route | Class | What it does | Body |
| --- | --- | --- | --- | --- |
| `GET` | `/api/review/queue` | read | List the reviews |  |
| `POST` | `/api/review/tours/bulk-delete` | risky | Delete several reviews |  |
| `GET` | `/api/review/tours/{tour_id}` | read | Read a review |  |
| `DELETE` | `/api/review/tours/{tour_id}` | risky | Delete a review |  |
| `POST` | `/api/review/tours/{tour_id}/sync` | change | Sync a review with its branch |  |
| `GET` | `/api/review/tours/{tour_id}/handoff` | read | Read a review's handoff packet |  |
| `POST` | `/api/review/tours/{tour_id}/handoff/export` | read | Export a review's handoff packet |  |
| `POST` | `/api/review/tours/{tour_id}/handoff/{packet_id}/note` | change | Save a note on a handoff packet |  |
| `GET` | `/api/review/tours/{tour_id}/steps/{step_id}/diff` | read | Read a review step's diff |  |
| `POST` | `/api/review/tours/{tour_id}/steps/{step_id}/reviewed` | change | Mark a review step reviewed |  |
| `PATCH` | `/api/review/tours/{tour_id}/steps/{step_id}/reviewed` | change | Mark a review step reviewed |  |
| `POST` | `/api/review/tours/{tour_id}/steps/{step_id}/question` | change | Ask a question on a review step |  |
| `POST` | `/api/review/tours/{tour_id}/findings` | change | Add a review finding |  |
| `POST` | `/api/review/tours/{tour_id}/request-plan` | change | Ask for a plan from a review |  |
| `POST` | `/api/review/tours/{tour_id}/regenerate` | change | Regenerate a review |  |
| `POST` | `/api/review/tours/{tour_id}/complete` | change | Complete a review |  |
| `GET` | `/api/plans/queue` | read | List the plans |  |
| `GET` | `/api/plans/{plan_id}` | read | Read a plan |  |
| `GET` | `/api/plans/{plan_id}/revisions` | read | List a plan's revisions |  |
| `POST` | `/api/plans/{plan_id}/responses/{question_key}` | change | Answer a plan's question |  |
| `PUT` | `/api/plans/{plan_id}/responses/{question_key}` | change | Answer a plan's question |  |
| `POST` | `/api/plans/{plan_id}/comments` | change | Comment on a plan |  |
| `PATCH` | `/api/plans/{plan_id}/comments/{comment_id}` | change | Edit a plan comment |  |
| `POST` | `/api/plans/{plan_id}/questions` | change | Ask a question on a plan |  |
| `POST` | `/api/plans/{plan_id}/request-changes` | change | Ask for changes to a plan |  |
| `POST` | `/api/plans/{plan_id}/approve` | change | Approve a plan |  |
| `POST` | `/api/plans/{plan_id}/reject` | change | Reject a plan |  |
| `POST` | `/api/plans/{plan_id}/reopen` | change | Reopen a plan |  |
| `GET` | `/api/learnings` | read | List the learnings |  |
| `POST` | `/api/learnings` | change | Propose a learning |  |
| `GET` | `/api/learnings/search` | read | Search the learnings |  |
| `GET` | `/api/learnings/context` | read | Read the learnings for a context |  |
| `GET` | `/api/learnings/prompt-preview` | read | Preview the learnings prompt |  |
| `GET` | `/api/learnings/{id}` | read | Read a learning |  |
| `POST` | `/api/learnings/{id}/accept` | change | Accept a learning |  |
| `POST` | `/api/learnings/{id}/edit` | change | Edit a learning |  |
| `PATCH` | `/api/learnings/{id}/edit` | change | Edit a learning |  |
| `POST` | `/api/learnings/{id}/reject` | change | Reject a learning |  |
| `POST` | `/api/learnings/{id}/prompt` | change | Turn a learning on or off in prompts |  |
| `POST` | `/api/learnings/{id}/incorporations` | change | Propose incorporating a learning |  |
| `POST` | `/api/learning-incorporations/{id}/submit` | change | Submit a learning incorporation |  |
| `POST` | `/api/learning-incorporations/{id}/verify` | change | Verify a learning incorporation |  |
| `POST` | `/api/learning-incorporations/{id}/verify-target` | change | Verify a learning incorporation's target |  |
| `POST` | `/api/learning-incorporations/drift` | change | Check learning incorporations for drift |  |
| `POST` | `/api/learning-reflections/{id}/submit` | change | Submit a learning reflection |  |
| `GET` | `/api/learning-prompt-manifests/{id}` | read | Read a learnings prompt manifest |  |
| `GET` | `/plan-images/` | read | Read a plan's image |  |
| `GET` | `/review-images/` | read | Read a review's image |  |

## Legato Scope

| Method | Route | Class | What it does | Body |
| --- | --- | --- | --- | --- |
| `POST` | `/api/scope/batches` | change | Send a batch of pins from a page | `{"origin": "http://localhost:5173", "pins": [{"comment": "…", "page": {"url": "http://localhost:5173/…"}, "target": {"selectors": ["…"]}, "screenshot": "data:image/jpeg;base64,…"}]}` |
| `GET` | `/api/scope/pins` | read | List an origin's pins |  |
| `POST` | `/api/scope/pins/{id}/reopen` | change | Reopen a resolved pin with a new comment | `{"comment": "…"}` |
| `POST` | `/api/scope/pins/{id}/withdraw` | change | Withdraw a pin no agent has been told of |  |
| `GET` | `/api/scope/pins/{id}/screenshot` | read | Read a pin's screenshot |  |

## Workflows and swarms

| Method | Route | Class | What it does | Body |
| --- | --- | --- | --- | --- |
| `GET` | `/api/workflows` | read | List the workflows, each with its workflow graph |  |
| `GET` | `/api/workflow-schema` | read | Read the JSON Schema of a workflow file |  |
| `GET` | `/api/workflow-templates` | read | List the bundled workflow templates and where they can go |  |
| `POST` | `/api/workflow-templates/{template}` | change | Add a bundled workflow template to a workflows folder | `{"name": "…", "folder": "global"}` |
| `POST` | `/api/workflow-drafts` | change | Save a drafted workflow, its triggers paused, to the workflows folder | `{"yaml": "schema_version: 1\nname: …"}` |
| `POST` | `/api/workflows/{name}/runs` | change | Start a workflow |  |
| `GET` | `/api/workflows/{name}/file` | read | Read a workflow's file, as its bytes, with its digest and whether it can be edited |  |
| `PUT` | `/api/workflows/{name}/file` | change | Save a workflow's file over the version it was read at | `{"yaml": "schema_version: 1\nname: …", "digest": "sha256:…", "confirm_triggers": false}` |
| `DELETE` | `/api/workflows/{name}/file` | change | Delete a workflow's file | `{"digest": "sha256:…"}` |
| `POST` | `/api/workflows/{name}/copy` | change | Copy a built-in workflow to a workflows folder | `{"folder": "global"}` |
| `POST` | `/api/workflow-files` | change | Create a workflow file in a workflows folder | `{"yaml": "schema_version: 1\nname: …", "folder": "global", "confirm_triggers": false}` |
| `POST` | `/api/workflow-files/check` | read | Check a workflow file's text, writing nothing | `{"yaml": "schema_version: 1\nname: …"}` |
| `PUT` | `/api/workflows/{name}/triggers/{trigger}` | change | Switch a workflow's trigger on or off here | `{"enabled": false}` |
| `DELETE` | `/api/workflows/{name}/triggers/{trigger}` | change | Leave a workflow's trigger to its file |  |
| `GET` | `/api/workflow-triggers/{request_id}` | read | Read a workflow trigger request |  |
| `POST` | `/api/workflow-triggers/{request_id}` | change | Answer a workflow trigger request |  |
| `GET` | `/api/workflow-runs` | read | List the workflow runs |  |
| `GET` | `/api/workflow-runs/{run_id}` | read | Read a workflow run, with the workflow graph of its own copy |  |
| `POST` | `/api/workflow-runs/{run_id}/cancel` | risky | Cancel a workflow run |  |
| `POST` | `/api/workflow-runs/{run_id}/resume-agent` | change | Start the run's card agent with a macro, or send the macro to the one running |  |
| `POST` | `/api/workflow-runs/{run_id}/steps/{step_id}/retry` | change | Retry a workflow step |  |
| `POST` | `/api/workflow-runs/{run_id}/steps/{step_id}/trigger` | change | Trigger a manual workflow step |  |
| `POST` | `/api/workflow-runs/{run_id}/steps/{step_id}/prompt` | change | Remind a workflow step's agent |  |
| `POST` | `/api/workflow-runs/{run_id}/steps/{step_id}/another-pass` | change | Run another pass of a run's review loop |  |
| `GET` | `/api/workflow-artifacts/{artifact_id}` | read | Read a workflow artifact |  |
| `POST` | `/api/swarm/start` | change | Start a swarm |  |
| `POST` | `/api/swarm/cancel` | risky | Cancel a swarm |  |
| `POST` | `/api/swarm/dispatch` | change | Dispatch a swarm worker |  |
| `POST` | `/api/swarm/message` | change | Message a swarm agent |  |
| `POST` | `/api/swarm/broadcast` | change | Message every swarm worker |  |
| `POST` | `/api/swarm/close` | change | Close a swarm worker |  |
| `POST` | `/api/swarm/finish` | change | Finish a swarm |  |
| `POST` | `/api/swarm/next-step` | change | Start a swarm's next step |  |
| `POST` | `/api/swarm/extend-plan` | change | Extend a swarm's plan |  |
| `GET` | `/api/swarm/status/` | read | Read a swarm's status |  |
| `GET` | `/api/swarm/inbox/` | read | Read a swarm's inbox |  |
| `GET` | `/api/swarm/pending-plan/` | read | Read a swarm's pending plan |  |
| `GET` | `/api/swarm/pending-plans` | read | List the pending swarm plans |  |

## Settings, updates and the machine

| Method | Route | Class | What it does | Body |
| --- | --- | --- | --- | --- |
| `GET` | `/api/settings` | read | Read the settings |  |
| `PUT` | `/api/settings/tint` | change | Set this Legato's tint |  |
| `GET` | `/api/settings/theme` | read | Read this server's preferred theme |  |
| `PUT` | `/api/settings/theme` | change | Set this server's preferred theme | `{"id": "ocular-dark-vivid", "expected_revision": 0}` |
| `GET` | `/api/settings/mcp/connections` | read | List the MCP connections |  |
| `GET` | `/api/settings/mcp/connections/{name}` | read | Read an MCP connection |  |
| `POST` | `/api/settings/mcp/connections` | risky | Add an MCP connection |  |
| `PATCH` | `/api/settings/mcp/connections/{name}` | risky | Change, turn on or turn off an MCP connection |  |
| `DELETE` | `/api/settings/mcp/connections/{name}` | risky | Remove an MCP connection |  |
| `POST` | `/api/settings/mcp/connections/{name}/test` | risky | Test an MCP connection: start a local server or reach a remote one, and list its tools |  |
| `GET` | `/api/engrams` | read | List the engrams agents can be started as |  |
| `GET` | `/api/engram-bindings` | read | List the engrams bound to repositories and folders |  |
| `DELETE` | `/api/engram-bindings` | change | Unbind an engram from a repository or folder |  |
| `PUT` | `/api/engram-bindings/default` | change | Set or clear a repository's default engram |  |
| `GET` | `/api/settings/repository-guidance` | read | Read the repository guidance for agents |  |
| `PUT` | `/api/settings/repository-guidance` | change | Save repository guidance for agents |  |
| `DELETE` | `/api/settings/repository-guidance` | risky | Delete saved repository guidance |  |
| `GET` | `/api/settings/skills` | read | List the skills Legato gives agents |  |
| `PUT` | `/api/settings/skills/{name}` | change | Turn a skill on or off for agents started from now on | `{"enabled": false}` |
| `PUT` | `/api/settings/updates` | risky | Set how Legato updates itself |  |
| `GET` | `/api/doctor` | read | Read the checks of what Legato needs from this machine |  |
| `POST` | `/api/doctor/check` | read | Run the checks of what Legato needs from this machine again |  |
| `GET` | `/api/setup` | read | Read the setup checklist |  |
| `PUT` | `/api/setup/dismissed` | change | Hide the setup checklist, or bring it back |  |
| `GET` | `/api/updates` | read | Read the update status |  |
| `POST` | `/api/updates/check` | read | Check for a new release |  |
| `POST` | `/api/updates/apply` | risky | Install an update and restart Legato |  |
| `POST` | `/api/restart` | risky | Restart Legato |  |
| `GET` | `/api/config/settings` | read | List the settings |  |
| `PUT` | `/api/config/settings/{key}` | change | Change a setting | `{"value": …}` |
| `DELETE` | `/api/config/settings/{key}` | change | Reset a setting |  |
| `GET` | `/api/config/lists` | read | List the settings lists |  |
| `GET` | `/api/config/move` | read | List the settings that can be moved |  |
| `POST` | `/api/config/move` | risky | Move settings out of config.yaml |  |
| `GET` | `/api/config/export` | read | Export the settings |  |
| `PUT` | `/api/config/lists/{list}/{name}` | change | Save an entry of a settings list | `{"value": {…}}` |
| `DELETE` | `/api/config/lists/{list}/{name}` | change | Delete an entry of a settings list |  |
| `POST` | `/api/updates/tools/{name}/apply` | risky | Update a tool Legato installs |  |
| `POST` | `/api/hot-reload/build` | risky | Rebuild and reload Legato |  |
| `GET` | `/api/adapters` | read | List the agent tools |  |
| `GET` | `/api/directories` | read | Suggest directories |  |
| `GET` | `/api/directories/presets` | read | List the directory presets |  |
| `POST` | `/api/directories/presets` | change | Save a directory preset |  |
| `GET` | `/api/repositories` | read | List the repositories |  |
| `POST` | `/api/repositories` | change | Add a repository |  |
| `DELETE` | `/api/repositories/{id}` | risky | Remove a repository |  |
| `GET` | `/api/job-clones` | read | List the job clones |  |
| `DELETE` | `/api/job-clones/{id}` | risky | Delete a job clone |  |
| `GET` | `/api/resources` | read | List the resources |  |
| `POST` | `/api/resources` | change | Save a resource |  |
| `PUT` | `/api/resources/{id}` | change | Edit a resource |  |
| `DELETE` | `/api/resources/{id}` | risky | Delete a resource |  |
| `GET` | `/api/macros` | read | List the macros |  |
| `PUT` | `/api/macros/{name}` | change | Save a macro: a new one, an edit, or an edit of a built-in; {"favorite": true} alone stars it |  |
| `DELETE` | `/api/macros/{name}` | change | Delete a macro added in Legato |  |
| `POST` | `/api/macros/{name}/reset` | change | Put a built-in macro back as shipped |  |
| `POST` | `/api/macros/{name}/rename` | change | Rename a macro added in Legato |  |
| `POST` | `/api/voice/transcribe` | read | Transcribe a voice recording |  |
| `GET` | `/api/settings/integrations` | read | Read the integrations |  |
| `GET` | `/api/settings/mains` | read | List the main instances |  |
| `DELETE` | `/api/settings/mains/{instance_id}` | risky | Unpair a main instance |  |
| `GET` | `/api/settings/workers` | read | List the workers |  |
| `POST` | `/api/settings/workers` | risky | Pair a worker |  |
| `GET` | `/api/settings/workers/{instance_id}/review-passes` | read | Read a worker's review passes |  |
| `DELETE` | `/api/settings/workers/{instance_id}` | risky | Remove a worker |  |
| `POST` | `/api/settings/integrations/burnlite/connect` | risky | Connect Burnlite |  |
| `PUT` | `/api/settings/integrations/burnlite` | change | Choose the Burnlite projects |  |
| `DELETE` | `/api/settings/integrations/burnlite` | risky | Disconnect Burnlite |  |
| `POST` | `/api/config/import` | risky | Import settings from a file, as a change set |  |
| `PUT` | `/api/settings/mains/{instance_id}/reeve` | risky | Let a paired main's Reeve message this one's, or stop it |  |

## Instances and integrations

| Method | Route | Class | What it does | Body |
| --- | --- | --- | --- | --- |
| `GET` | `/api/pairing` | never | Show the open pairing request, with its phrase |  |
| `POST` | `/api/pairing` | never | Open a pairing request |  |
| `DELETE` | `/api/pairing` | risky | Cancel the pairing request |  |
| `POST` | `/api/pair/exchange` | never | Exchange keys to pair a device |  |
| `POST` | `/api/pair/confirm` | never | Confirm the pairing of a device |  |
| `GET` | `/api/integrations/owner` | read | Find which task owns a pull request |  |
| `POST` | `/api/integrations/delegations` | change | Delegate work to an agent |  |
| `GET` | `/api/integrations/delegations/{id}` | read | Read a delegation |  |
| `POST` | `/api/integrations/delegations/{id}/messages` | change | Message a delegation's agent |  |
| `PUT` | `/api/integrations/attention/{key}` | change | Record a case as an attention item |  |
| `POST` | `/api/integrations/attention/{key}/resolve` | change | Resolve a case |  |
| `POST` | `/api/integrations/attention/sync` | change | Sync the open cases |  |
| `GET` | `/api/integrations/handoff/pairing` | read | Read this worker's pairing |  |
| `GET` | `/api/integrations/handoff/capabilities` | read | Read what this worker can do for a handoff |  |
| `POST` | `/api/integrations/handoff/tasks` | change | Accept a handed-off task |  |
| `GET` | `/api/integrations/handoff/tasks/{handoff_id}` | read | Read a handoff |  |
| `POST` | `/api/integrations/handoff/tasks/{handoff_id}/return` | change | Return a handed-off task |  |
| `POST` | `/api/integrations/handoff/tasks/{handoff_id}/archive` | change | Archive a returned task |  |
| `GET` | `/api/integrations/handoff/pull` | read | Read the state of the handoffs |  |
| `GET` | `/api/integrations/handoff/events` | read | Follow the handoffs live |  |
| `POST` | `/api/workers/{instance_id}/return-done` | change | Return a worker's done tasks |  |
| `POST` | `/api/integrations/handoff/tasks/{handoff_id}/drop` | change | Drop a handoff |  |
| `POST` | `/api/integrations/handoff/leave` | risky | Unpair this main from the worker |  |
| `POST` | `/api/integrations/handoff/tasks/{handoff_id}/review/{action}` | change | Act on a handoff's review passes |  |
| `GET` | `/oauth/callback` | risky | Finish connecting an integration |  |

## Other

| Method | Route | Class | What it does | Body |
| --- | --- | --- | --- | --- |
| `GET` | `/health` | read | Check that Legato is running |  |
| `GET` | `/frontend-meta` | read | Read the web UI's build details |  |
| `GET` | `/api/meta` | read | Read this Legato's version and abilities |  |
| `GET` | `/api/agent-groups` | read | List the agent groups |  |
| `PUT` | `/api/agent-groups/{name}` | change | Rename an agent group | `{"name": "…"}` |
| `DELETE` | `/api/agent-groups/{name}` | change | Delete an agent group, leaving its agents ungrouped |  |
| `POST` | `/api/mcp/connections/{name}/oauth/begin` | never | Start authorizing an MCP connection |  |
| `POST` | `/api/mcp/connections/{name}/oauth/disconnect` | never | Disconnect an MCP connection's authorization |  |
| `GET` | `/api/mcp/oauth/attempts/{id}` | never | Read an MCP authorization attempt |  |
| `POST` | `/api/mcp/oauth/attempts/{id}/deliver` | never | Deliver an MCP authorization response from the Legato app |  |
| `POST` | `/api/mcp/oauth/attempts/{id}/confirm` | never | Confirm an MCP authorization |  |
| `POST` | `/api/mcp/oauth/attempts/{id}/cancel` | never | Cancel an MCP authorization attempt |  |
| `GET` | `/api/ca-cert` | read | Download the CA certificate |  |
| `GET` | `/api/sessions` | read | Search the session history: the conversations agents had here, with whether each can be restored |  |
| `GET` | `/api/sessions/{id}` | read | Show a conversation of the session history with its runs |  |
| `POST` | `/api/sessions/{id}/restore` | change | Restore a conversation of the session history (start it again as it last ran), or start a new one in its place with a hand-over |  |
| `DELETE` | `/api/sessions/{id}` | change | Forget a conversation: remove it from the session history (the tool's transcript stays) |  |
| `GET` | `/ws` | never | Open the live connection that can type into agents |  |
| `GET` | `/` | read | Load the web UI |  |

The settings routes (`/api/config/settings/{key}`, `/api/config/lists/{list}/{name}`) take each setting's class from the [configuration reference](/legato/reference/configuration/): the class above is the least a change needs.
