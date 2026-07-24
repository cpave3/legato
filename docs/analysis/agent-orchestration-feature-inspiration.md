# Agent Orchestration Feature Inspiration for Legato

**Date:** 2026-07-22  
**Products reviewed:** Warp/Oz, BridgeMind, Devin, Claude Code agent teams, GitHub Copilot cloud agent, and Factory

## Executive summary

Legato already matches much of the visible multi-agent feature set offered by adjacent products: mixed-agent swarms, model tiers, worktrees, plan approval, live supervision, mobile access, PR tracking, and guided review tours.

The best inspiration is therefore **not simply spawning more agents**. Legato should make agent work:

1. easier to supervise,
2. durable and resumable,
3. measurable,
4. reusable.

The strongest pattern across the products reviewed is the treatment of agent activity as durable workflow objects rather than temporary terminal sessions. Runs retain plans, decisions, messages, outcomes, and review context so that people can inspect, continue, compare, and improve them later.

If Legato implements only three ideas from this report, they should be:

1. **A unified attention inbox** for everything requiring human action.
2. **Durable run records** as the foundation for history, analytics, and resumability.
3. **A versioned plan workspace** that makes swarms more controllable and reusable.

Together, these support a clear product identity:

> **Legato is the local-first control plane where developers supervise, understand, and review work performed by any coding agent.**

---

## Current Legato position

Legato already has more competitive parity than its top-level feature list immediately suggests. Its existing capabilities include:

- multiple coding-agent harnesses,
- per-worker model selection through tiers,
- conductor/worker orchestration,
- human approval before swarm fan-out,
- worker messaging and steering,
- worktree integration,
- live and mobile terminal control,
- PR status and CI visibility,
- plan extensions,
- role-specific prompts and adapter configuration,
- and guided review tours with semantic chapters, risk markers, anchored questions, and durable answers.

The review-tour system is particularly differentiating. Many competing products expose a raw diff or execution transcript. Legato can additionally preserve the author's intended reading order and let a reviewer ask questions anchored to specific hunks or lines.

The main gaps are not basic agent execution. They are:

- durable run history,
- unified human-attention management,
- plan persistence and versioning,
- reliable resumability,
- reusable agent/workflow definitions,
- and outcome-oriented analytics.

---

## Market observations

### Warp and Oz

Warp has expanded from a terminal into an agentic development environment and orchestration platform. The most relevant ideas for Legato are:

- harness and model neutrality,
- local and remote agent handoff,
- reusable agent profiles and skills,
- structured, editable, versioned plans,
- parent/child orchestration with independent run records,
- durable, server-backed agent mailboxes,
- centralized notifications for blocked, failed, and completed work,
- team-level run observability,
- and persistent memory shared across supported harnesses.

Warp's code editor, LSP support, semantic indexing, and full cloud runtime are less directly relevant because they move Warp toward being an IDE and hosted execution platform. Legato's stronger opportunity is to remain a focused orchestration and review layer around tools developers already use.

### BridgeMind

BridgeMind's public product family includes:

- **BridgeSpace**, positioned as an Agent Development Environment,
- **BridgeMCP**, an MCP server for agentic coding workflows,
- **BridgeVoice**, a voice-to-code interface,
- and **BridgeBench**, a vibe-coding benchmark and leaderboard.

The individual product pages were protected by Cloudflare during this research, so conclusions about BridgeMind are deliberately limited to its published LLM index and public product descriptions.

The useful strategic signals are:

- users value one environment that works across Cursor, Claude Code, Copilot, Codex, Windsurf, and similar tools;
- MCP configuration and portable tool access are becoming part of the agent platform layer;
- voice is emerging as an alternate control surface, especially for hands-free or mobile supervision;
- and users want evidence about which models and workflows work, not only marketing claims.

### Devin

Devin's most relevant differentiator is **Session Insights**. Completed sessions can be analyzed for:

- compute usage,
- number of user interventions,
- session size and task category,
- issue timelines,
- recurring failures,
- improved prompts,
- machine or repository configuration recommendations,
- useful knowledge,
- and misleading or outdated knowledge.

This turns failures and expensive sessions into inputs for improving future work. Legato could provide a more local-first, harness-neutral version using structured lifecycle events and outcomes rather than requiring full proprietary transcript analysis.

### Claude Code agent teams

Claude Code's agent teams reinforce several useful orchestration patterns:

- a lead with independent teammates,
- a shared task list,
- dependencies between tasks,
- direct user interaction with individual teammates,
- optional teammate plan approval,
- self-claiming of available work,
- role definitions with tool/model restrictions,
- and quality gates triggered when tasks complete or agents become idle.

Legato's conductor model is easier to audit than unrestricted peer coordination and should remain the default. Direct worker-to-worker communication is most useful as an explicit mode for research, competing hypotheses, or cross-layer collaboration.

### GitHub Copilot cloud agent

GitHub emphasizes:

- delegation directly from issues, chat, PR comments, and integrations,
- ephemeral execution environments,
- transparent branch and commit history,
- specialized custom agents,
- hooks and skills,
- repository memory,
- scheduled and event-driven automations,
- and PR outcome metrics such as merge rate and time to merge.

The most relevant lesson is that an agent's success should ultimately be connected to delivery outcomes, not merely whether its process exited successfully.

### Factory

Factory's positioning reinforces three broad market expectations:

- model independence,
- deployment sovereignty,
- and support across the complete software-development lifecycle.

Legato already has strong foundations for model independence and local execution. It should deepen those advantages rather than immediately pursuing a hosted platform.

---

## Recommended features

## 1. Unified attention inbox

**Inspired by:** Warp's agent notification mailbox and run-status filtering.

Create one queue for everything requiring human action:

- permission or plan approval,
- worker questions,
- failed or blocked agents,
- finished work awaiting review,
- CI or reviewer feedback on an agent PR,
- review-tour questions,
- and stale work that may need intervention.

Each item should have one obvious next action:

- **Approve**,
- **Reply**,
- **Open agent**,
- **Review changes**,
- **Retry**,
- or **Dismiss**.

Legato already captures many of the underlying signals, but they are distributed across cards, agent terminals, swarm inboxes, PR indicators, and review tours. A unified inbox would make Legato feel like an actual agent command center.

The inbox should avoid flooding users with worker-level churn. Swarms should normally aggregate notifications at the parent level while still allowing users to drill into an individual blocked or failed worker.

**Priority:** P0  
**Effort:** Medium  
**Fit:** Excellent

---

## 2. Durable run records and timelines

**Inspired by:** Warp/Oz run history, Devin Session Insights, and GitHub's transparent cloud-agent logs.

Treat every agent session or swarm as a first-class **run** attached to its task:

```text
Task
 ├─ Run #1: failed during setup
 ├─ Run #2: completed, PR #142
 └─ Run #3: review fixes
```

A run should record:

- launch source and initial prompt,
- adapter, model tier, role, repo, branch, and worktree,
- plan revisions and approvals,
- lifecycle transitions,
- human interventions,
- worker messages,
- commits and changed files,
- tests and quality-gate results,
- final result,
- linked PR,
- linked review tour,
- duration,
- and terminal outcome.

The first version does not need complete terminal transcripts. A structured event timeline would already answer:

- What did this agent do?
- What decisions were made?
- Where did it get blocked?
- Why did it stop?
- What artifact should be reviewed?

This addresses a product-model weakness: an agent is currently represented primarily by its live tmux session. After that session exits, much of the useful execution context disappears.

Durable run records are foundational for the inbox, resumability, analytics, workflow recipes, and benchmarking.

**Priority:** P0  
**Effort:** Medium–Large  
**Fit:** Excellent

---

## 3. Versioned plan workspace

**Inspired by:** Warp's editable, persistent plans with history and selective execution.

Legato already has conductor-generated YAML plans and human approval, but they remain relatively transient:

- browser editing is missing,
- revisions are not presented as a history,
- plan files are runtime-oriented,
- and execution is mostly whole-plan or appended extensions.

Turn the swarm plan into a durable task artifact with:

- a rich structured editor in the web UI,
- diffs between revisions,
- the ability to restore an earlier version,
- reordering or disabling steps,
- selective execution,
- pause-after-phase controls,
- comments attached to individual steps,
- explicit dependencies,
- and retention of the final plan after the swarm finishes.

This is also the natural foundation for reusable orchestration templates.

An early implementation should build on the existing conductor-dashboard design in [`docs/conductor-dashboard-web.md`](../conductor-dashboard-web.md), which already identifies the need for raw YAML retrieval, editing, and resubmission.

**Priority:** P0/P1  
**Effort:** Medium  
**Fit:** Excellent

---

## 4. Agent profiles as reusable bundles

**Inspired by:** Warp Agent Profiles, GitHub custom agents, and BridgeMind's cross-tool toolkit positioning.

Legato currently exposes the ingredients of an agent configuration separately:

- adapter,
- model tier,
- launch arguments,
- role prompt,
- Chimera mode,
- concurrency,
- and scope strictness.

Package them into named profiles:

```yaml
agent_profiles:
  cautious-backend:
    adapter: chimera
    tier: large
    mode: legato-worker
    permissions: sandboxed
    prompt: backend
    quality_gate: task check

  quick-docs:
    adapter: claude-code
    tier: small
    prompt: docs
    quality_gate: task test
```

Users could select a profile when spawning a solo agent. Conductors could assign profiles in plans rather than reconstructing configuration for every subtask.

Profiles could eventually include:

- tool or command restrictions,
- applicable skills,
- environment requirements,
- maximum run duration,
- required plan approval,
- and completion gates.

This turns Legato's existing configurability into a visible product feature.

**Priority:** P1  
**Effort:** Small–Medium  
**Fit:** Excellent

---

## 5. Post-run insights and feedback loop

**Inspired by:** Devin Session Insights.

After a run, calculate a lightweight local summary:

- successful, failed, canceled, or abandoned,
- duration,
- number of human interventions,
- repeated failures or retry loops,
- time spent waiting,
- tests and quality gates reached,
- PR opened and eventually merged,
- review findings generated,
- and likely failure category such as environment, scope, agent error, or unclear task.

Then offer concrete suggestions:

- “This run repeatedly failed because `pnpm` was missing; add it to workspace setup.”
- “Three clarifications were required; add these acceptance criteria to the task template.”
- “This role consistently uses the large tier but completes simple documentation work.”
- “The same stale instruction affected two runs.”

The design should remain local-first and explainable. It should show which events led to each conclusion rather than producing opaque health scores.

Useful aggregate measures could include:

- completion rate,
- time to first intervention,
- intervention count,
- time waiting for a human,
- quality-gate pass rate,
- PR merge rate,
- median time from agent completion to review,
- and median time to merge.

**Priority:** P1  
**Effort:** Medium  
**Fit:** Strong

---

## 6. Resumable runs and handoff packets

**Inspired by:** Warp local/cloud handoff and resumable child agents.

A Legato handoff does not need cloud execution initially. It could create a portable continuation packet containing:

- the original task and prompt,
- a summary of the prior run,
- plan and completed steps,
- branch and worktree,
- dirty diff or commit range,
- unresolved questions,
- test status,
- relevant review feedback,
- and pending inbox messages.

This would allow users to:

- resume with the same adapter,
- continue using a different adapter or model,
- replace a failed worker without losing context,
- hand work from a swarm to a solo agent,
- create a follow-up run from a completed review,
- or move from implementation into a specialized reviewer profile.

This plays directly to Legato's harness-neutral architecture. The continuation packet should use explicit artifacts and summaries rather than depending on a provider-specific conversation-resume feature.

**Priority:** P1  
**Effort:** Medium–Large  
**Fit:** Strong

---

## 7. Durable orchestration mailbox

**Inspired by:** Warp/Oz's server-backed, sequenced agent mailboxes and Claude's shared team coordination.

Legato currently uses `tmux send-keys` as its inter-agent message bus. This is documented as best-effort in [`docs/ai/swarm.md`](../ai/swarm.md): messages can arrive mid-turn and are processed when the receiving agent returns to a prompt.

Gradually replace that with persisted inbox messages supporting:

- monotonic sequence IDs,
- delivered, read, and acknowledged states,
- replay after restart,
- structured question, progress, decision, and result messages,
- reliable ordering between messages and lifecycle transitions,
- and workers that can be restarted and receive pending work.

Keep `send-keys` as a wake-up or compatibility signal for harnesses that do not expose a native event interface. The durable mailbox, rather than the terminal input, should become the source of truth.

This work is less visible than the attention inbox or plan editor, but it is the foundation for reliable resumability and cross-machine execution.

**Priority:** P1 infrastructure  
**Effort:** Large  
**Risk:** High

---

## 8. Workflow recipes and triggers

**Inspired by:** Warp skills-as-agents, Devin Playbooks and Automations, and GitHub agent automations.

Let users save recurring task workflows such as:

- investigate failing CI,
- dependency upgrade,
- three-lens PR review,
- reproduce and fix a bug,
- documentation drift audit,
- security review,
- issue triage,
- and release preparation.

A recipe would combine:

```text
trigger + task template + agent profile + swarm shape + quality gate
```

For example:

```yaml
recipes:
  review-pr:
    trigger: manual
    profile: review-conductor
    swarm:
      - security-reviewer
      - correctness-reviewer
      - tests-docs-reviewer
    completion_gate: all_workers_closed
    output: review_tour
```

Start with manually invoked recipes. Add schedules and webhooks only after run records, profiles, and reliable messaging exist. This avoids prematurely turning Legato into a hosted automation platform.

**Priority:** P2  
**Effort:** Medium initially  
**Fit:** Strong after run records and profiles

---

## 9. Capability and environment preflight

**Inspired by:** Warp environments and BridgeMCP.

Before launching an agent or approving a swarm, display:

- required CLIs and whether they are installed,
- repository and worktree state,
- detected project instructions and skills,
- adapter availability,
- configured tiers and modes,
- MCP configuration detected for the chosen harness,
- required environment variables by name, never value,
- expected build and test commands,
- and likely conflicts between parallel workers.

This would catch environment failures before spending agent time.

Legato should not become an MCP host initially. It can provide value simply by inspecting and explaining what each chosen harness will inherit, highlighting configuration differences, and warning about unavailable capabilities.

**Priority:** P2  
**Effort:** Medium  
**Fit:** Good

---

## 10. Benchmark agent and profile combinations

**Inspired by:** BridgeMind's BridgeBench and the model-independent positioning shared by Warp and Factory.

A local benchmark could rerun a small set of repository-specific tasks against selected profiles and compare:

- success rate,
- duration,
- interventions,
- quality-gate result,
- review findings,
- and estimated or manually entered cost.

This could answer practical questions such as:

- Does the small Chimera profile reliably handle documentation changes?
- Which profile has the best first-pass test success rate?
- Does a critic worker improve outcomes enough to justify its runtime?
- Which harness performs best on this repository's migration tasks?

The benchmark becomes useful only after Legato has durable run and outcome data. It should compare repeatable task fixtures and observable results rather than offering a generic model leaderboard.

**Priority:** P3  
**Effort:** Large  
**Fit:** Interesting long-term differentiator

---

## Competitive parity overview

| Market feature | Legato status |
|---|---|
| Multiple coding-agent harnesses | Already present |
| Per-worker model selection | Already present through tiers |
| Parent/worker orchestration | Already present |
| Human approval before fan-out | Already present |
| Worker messaging and steering | Already present |
| Worktree integration | Already present |
| Live/mobile terminal control | Already present |
| PR status and CI visibility | Already present |
| Inline guided code review | Stronger than many competitors through review tours |
| Multi-worker dashboard | Designed but not implemented |
| Rich plan editor and history | Partial |
| Durable run history | Major gap |
| Unified action inbox | Major gap |
| Run insights and outcome analytics | Major gap |
| Reliable resumability | Major gap |
| Scheduled/event-triggered agents | Not currently central |
| Reusable agent profiles | Building blocks exist, but not packaged |
| Durable inter-agent messaging | Current implementation is best-effort |

---

## Features not to copy yet

### Built-in code editor or semantic index

Warp is becoming an IDE and terminal. Legato should stay the orchestration and review layer around whichever editor and agent the developer prefers. Building LSP, editor, and indexing features would be expensive and weaken its focus.

Legato can instead deep-link files into the user's editor and preserve precise paths, hunks, and line references in plans and reviews.

### Hosted cloud execution

Cloud execution is useful eventually, but it introduces authentication, secrets, billing, isolation, compute, network access, and reliability obligations.

First make local runs durable and resumable. Then define a remote execution-provider interface that could support self-hosted workers, SSH hosts, CI runners, or third-party clouds without requiring Legato to become a SaaS platform.

### General MCP marketplace

BridgeMind emphasizes BridgeMCP, and Warp has extensive MCP management. Legato should initially detect and expose the selected harness's capabilities rather than duplicating every harness's MCP implementation.

A later capability registry could normalize which tools are available to each adapter/profile without requiring Legato to proxy every tool call.

### Voice as a top priority

BridgeVoice validates that voice interaction has demand, especially for mobile supervision, and Legato already has voice-related requirements. However, voice should sit on top of a good attention inbox and structured action system rather than precede them.

Voice is most valuable for actions such as:

- answering a worker question,
- rejecting a plan with feedback,
- sending a correction,
- summarizing a run,
- or dictating review feedback.

### Unstructured peer-to-peer swarms

Claude teams and Warp support richer peer coordination, but Legato's conductor model is easier to audit and review. Direct worker communication is useful for research and competing-hypothesis workflows, but it should be an explicit orchestration mode rather than the default.

### Vanity analytics

Raw counts such as “agents launched” or “tokens consumed” are not sufficient measures of value. Analytics should connect execution to outcomes such as:

- quality gates passed,
- review findings resolved,
- PRs merged,
- time to merge,
- interventions required,
- and repeated environment failures avoided.

---

## Recommended roadmap

## Phase 1: Agent command center

1. Add a unified attention inbox.
2. Introduce durable run and event records.
3. Add scannable run history with status, duration, adapter, branch, and outcome.
4. Link each run to its task, PR, and review tour.
5. Aggregate swarm notifications at the parent while preserving worker drill-down.

**Outcome:** Legato becomes the place users look to understand what needs attention and what their agents have done.

## Phase 2: Reliable orchestration

1. Add a versioned web plan editor.
2. Introduce named agent profiles.
3. Replace `send-keys` as the source of truth with a durable mailbox.
4. Add resume, replacement, and handoff packets.
5. Build the multi-worker conductor dashboard.
6. Add explicit completion and quality gates to plans/profiles.

**Outcome:** Agent workflows survive restarts, handoffs, failed workers, and human revisions without losing intent or state.

## Phase 3: Learning system

1. Add post-run insights.
2. Add manually invoked workflow recipes.
3. Add capability and environment preflight.
4. Compare outcomes by agent profile and model tier.
5. Add optional schedules and external triggers.
6. Explore remote execution providers only after local resumability is proven.

**Outcome:** Legato helps users improve how they delegate work, not merely execute more of it.

---

## Suggested product model

A durable data model could evolve toward the following hierarchy:

```text
Workspace
 └─ Task
     ├─ Plans
     │   ├─ Revision 1
     │   └─ Revision 2 (approved)
     ├─ Runs
     │   ├─ Solo run
     │   └─ Swarm run
     │       ├─ Conductor execution
     │       └─ Worker executions
     ├─ Attention items
     ├─ Pull request
     └─ Review tours
```

The important distinction is between:

- a **task**, which captures the user's desired outcome,
- a **plan**, which captures an intended approach,
- a **run**, which captures one attempt to execute that approach,
- an **agent execution**, which is one participant within a run,
- an **attention item**, which captures a required human decision,
- and a **review tour**, which captures how the resulting changes should be understood and evaluated.

This model prevents tmux sessions, plans, PRs, and reviews from becoming disconnected feature silos.

---

## Evaluation principles

Future feature proposals should be evaluated against these principles:

### Local-first

Legato's local SQLite and tmux architecture is a strategic advantage. New features should continue to work without a hosted account where practical.

### Harness-neutral

Agent profiles, runs, handoffs, inbox items, and reviews should use Legato-owned concepts rather than provider-specific conversation formats.

### Human attention is scarce

The product should optimize the user's ability to identify and resolve consequential decisions. It should suppress routine worker churn and elevate blocked, failed, risky, and review-ready work.

### Durable intent over raw transcripts

Plans, decisions, summaries, artifacts, and structured events are more reusable than complete terminal logs. Full transcripts may be optional, but durable intent should be mandatory.

### Outcomes over activity

Measure whether work passed validation, survived review, and shipped—not simply how many agents ran or how long they stayed busy.

### Review is part of execution

The run should not be considered complete merely because an agent stopped. Review readiness, reviewer questions, requested fixes, and PR outcomes belong to the same lifecycle.

---

## Sources

### Warp and Oz

- [Warp](https://www.warp.dev/)
- [Warp documentation index](https://docs.warp.dev/llms.txt)
- [Warp Agent Platform](https://docs.warp.dev/agent-platform/)
- [Warp multi-agent orchestration](https://docs.warp.dev/platform/orchestration/)
- [Warp cloud-agent management](https://docs.warp.dev/platform/managing-cloud-agents/)
- [Warp planning](https://docs.warp.dev/agent-platform/capabilities/planning/)
- [Warp agent profiles and permissions](https://docs.warp.dev/agent-platform/capabilities/agent-profiles-permissions/)
- [Warp handoff](https://docs.warp.dev/platform/handoff/)
- [Warp agent memory](https://docs.warp.dev/agent-platform/agent-memory/)
- [Warp code review](https://docs.warp.dev/code/code-review/)

### BridgeMind

- [BridgeMind](https://www.bridgemind.ai/)
- [BridgeMind LLM index](https://www.bridgemind.ai/llms.txt)
- [BridgeSpace](https://www.bridgemind.ai/products/bridgespace)
- [BridgeMCP](https://www.bridgemind.ai/bridgemcp)
- [BridgeVoice](https://www.bridgemind.ai/products/bridgevoice)
- [BridgeBench](https://www.bridgemind.ai/bridgebench)

### Other adjacent products

- [Devin introduction](https://docs.devin.ai/get-started/devin-intro)
- [Devin Session Insights](https://docs.devin.ai/product-guides/session-insights)
- [Claude Code agent teams](https://code.claude.com/docs/en/agent-teams)
- [GitHub Copilot cloud agent](https://docs.github.com/en/copilot/concepts/agents/cloud-agent/about-cloud-agent)
- [Factory](https://factory.ai/)

---

## Research caveat

This report was prepared from public product and documentation pages available on 2026-07-22. Product capabilities and positioning can change rapidly.

BridgeMind's normal product pages returned Cloudflare challenges to automated requests. BridgeMind-specific observations are therefore intentionally limited to its published `llms.txt`, which identifies BridgeSpace, BridgeMCP, BridgeVoice, and BridgeBench and gives short public descriptions of each product.
