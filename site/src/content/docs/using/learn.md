---
title: Legato Learn
description: Keep the lessons your agents learn as evidence-backed records you approve, and give the ones you choose to new agents.
sidebar:
  order: 100
---

Legato Learn keeps what you and your agents learn while you work: facts about a codebase, your preferences, better ways to work, pitfalls. Each learning is a short claim backed by evidence. Nothing becomes standing guidance until you accept it, and nothing reaches an agent's prompt until you turn it on. Use it so the next agent does not repeat the last one's mistake.

Learn is not a memory of transcripts. Legato never reads an agent's conversation or terminal for it. An agent sends a short, structured reflection when asked, and Legato keeps only that.

## How it works

1. **Evidence.** An agent reflects on a task and reports what it observed. Each observation becomes evidence, tied to the task and the agent that made it.
2. **Curation.** A curator agent reads the new evidence beside the learnings you already have and drafts changes: a new learning, more evidence for an old one, a duplicate to merge. A second agent, the critic, checks that draft and keeps only what is new, reusable and backed by the evidence. Proposing nothing is a good outcome.
3. **Your decision.** Proposed learnings wait as candidates. You accept, edit or reject each one. The agents never decide for you.
4. **Use.** You choose which accepted learnings new agents get in their launch prompt.

A learning has a scope: `user` applies to all your work, and `project` applies to one repository.

## Setting it up

You can record, search and decide learnings without any setup. The settings below turn on the automatic parts. Both are off unless you set them:

```yaml
learning:
  enabled: true              # reflect and curate when a task enters these columns
  transition_columns: [Review]
  curator_adapter: chimera   # optional; swarm.worker.agent otherwise
  prompt_enabled: true       # give enabled learnings to new agents
```

- `learning.enabled` starts a reflection and a curation each time a task moves into one of `transition_columns`. Each column must exist on the board.
- The curator and the critic run on `learning.curator_adapter`, or on `swarm.worker.agent` when that is not set. The agent tool must have an entry under `adapters`. `learning.curator_tier` picks one of its [tiers](/legato/using/swarms/#tiers).
- `learning.prompt_enabled` lets Legato add learnings to the prompts of agents it starts. It never changes an agent that already runs.

See the [configuration reference](/legato/reference/configuration/) for the limits on curation and prompts.

## Curating a task by hand

To ask a task's agent what it learned, run:

```bash
legato learn curate --task <task-id>
```

The task's agent must be running: Legato asks it for its reflection and does not start a new one. The curator and the critic then run as a [workflow](/legato/using/workflows/), and their proposals appear as candidates.

To review your existing learnings for duplicates and over-specific wording, run `legato learn consolidate --scope user`, or `--scope project` inside a repository.

## Deciding

In the web UI, open **Learning** (on a phone, under **More**). Its count is the number of learnings waiting for your decision. The page lists learnings with filters and a search. Open one to see its claim, its evidence and where that evidence came from, then:

- **Accept** it, as written or after editing it.
- **Reject** it.
- **Enable** or **disable** it for agents' prompts.

From the command line:

```bash
legato learn list --status candidate
legato learn show <learning-id>
legato learn accept <learning-id> --revision <n>
legato learn reject <learning-id> --revision <n>
legato learn enable <learning-id> --revision <n>
```

Each decision names the learning's current revision, so you never decide on a version that has since changed. After a conflict, run `legato learn show` and try again with the new revision.

Learnings do not appear in [Attention](/legato/using/attention/). They wait on the Learning page.

## What agents get

When `learning.prompt_enabled` is on, each agent Legato starts gets a `## Learned context` section in its launch prompt. It holds the learnings that:

- you accepted, and
- you enabled for prompts, and
- have `user` scope, or `project` scope for the task's repository.

Accepted learnings start disabled. Legato keeps a record of which learnings each agent got. The **Learning** page can preview the section a new agent on a given task would get.

## Recording and searching

To record a learning yourself:

```bash
legato learn record "Run the migration tests after any schema change" --kind convention --scope project --project <project-key>
```

It starts as a candidate, like any other. Agents use search to recall what is known before they start:

```bash
legato learn search "bounded retries"
```

Search matches words by default. With `learning.embedding` set to an OpenAI-compatible embedding service, it also matches by meaning.

See the [command reference](/legato/reference/cli/) for every `legato learn` command.

## Privacy

Learnings and their evidence stay in Legato's local database. Agents' reflections are kept as written, so they must not hold secrets. There is no command yet to export, bulk-delete or redact learnings.
