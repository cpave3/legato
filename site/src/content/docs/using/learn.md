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

You can record, search and decide learnings without any setup, and `legato learn curate` works at any time (see below).

Automatic Learn is a workflow, `learn`, which Legato ships as a template. When a card enters **Review**, it asks the card's running agent what it learned, and a curator and a critic turn the answer into candidates. To use it:

1. Add the workflow: `legato workflow add learn`, or **New workflow** › **Learn from a task** on the **Workflows** tab.
2. Its trigger arrives paused. Switch it on on the Workflows tab, and off again when you want a rest. The switch is kept for this Legato only.

The card's agent must be running when the card enters the column; otherwise the run fails at its first step. The curator and the critic run on your default agent tool. To change that, the column, or the packet's size, edit the workflow file: add `agent:` and `tier:` to the `with:` of its `curator` and `critic` steps, change `column:` under `on:`, or change `limits:` on its `packet` step. A workflow has one trigger, so for a second column add a copy under another name.

A run's own task never starts another run: the trigger skips tasks a Learn run made.

To give enabled learnings to new agents, set `learning.prompt_enabled: true`. It never changes an agent that already runs.

### Build your own

The steps of `learn` are ordinary step types that any workflow can use (see [Learn steps](/legato/using/workflows/#learn-steps)). For example, a workflow with only an `agent.interview` step keeps a card agent's reflection as evidence and curates nothing. You can then curate it later with `legato learn curate`.

### From `learning.enabled`

Before the template, `learning.enabled` and `learning.transition_columns` started Learn on their own. They are deprecated and now do nothing. At its first start with the template, a Legato with `learning.enabled: true` installed a `learn` workflow for each of those columns that its board has (`learn`, then `learn-<column>`) in its workflows folder, with your curator settings in it, and switched it on. It does this once; while the old settings are still set, its log says that they do nothing. A Legato with Learn off installed nothing.

`learning.curator_adapter`, `learning.curator_tier`, `learning.curator_working_dir` and the packet limits still apply to `legato learn curate` and `legato learn consolidate`. See the [configuration reference](/legato/reference/configuration/) for them and the limits on prompts.

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
