---
title: Skills
description: Keep your agent skills in one folder, and Legato gives the ones you turn on to every Claude Code and Chimera agent it starts, inside the sandbox or out.
sidebar:
  order: 65
---

A skill is a folder of instructions, with any scripts or references it needs, that an agent loads when the task calls for it. Agent tools each keep skills in their own folder (`~/.claude/skills`, `~/.chimera/skills`), so agents of different tools, and agents in Legato's sandbox, see different sets. Put your skills in Legato's skills folder instead, and every agent Legato starts gets the same ones.

## Add a skill

Put a folder in `~/.legato/skills` with a `SKILL.md` in it:

```text
~/.legato/skills/
  release-notes/
    SKILL.md
    scripts/collect.sh
```

`SKILL.md` starts with a `name` and a `description`:

```markdown
---
name: release-notes
description: Use when asked to write release notes. Collects the merged pull requests since the last tag.
---

Run scripts/collect.sh, then …
```

- The `name` is the folder's name: lowercase letters, digits and hyphens, at most 64 characters.
- The `description` tells the agent when to use the skill.
- A link to a skill elsewhere, such as a checkout of a skills repository, works too.

Legato reads the folder each time it lists skills and each time it starts an agent, so a skill you add, change or remove counts at once: no restart.

To keep skills somewhere else, set `skills.dir` (it takes effect at the next restart):

```yaml
skills:
  dir: ~/Projects/my-skills
```

## Turn skills on and off

**Settings › Agents and workspaces › Skills** lists every skill in the folder with its description and path. A new skill is on. Turn one off with its switch, and agents started from then on do not get it.

A folder that is not a usable skill is listed with what is wrong (no `SKILL.md`, no `description`, a `name` that is not the folder's) and is never given to an agent. Fix the file and it is listed as a skill at the next look; the list checks again every few seconds.

Agents that are already running keep the skills they started with. To give one the skills as they are now, reload it.

The Reeve can turn a skill on or off too: it proposes the change, and you approve it.

## What agents see

| Tool | Gets the skills | Calls a skill |
| --- | --- | --- |
| Claude Code | Yes, as a plugin named `legato-skills` | `legato-skills:<name>`, for example `/legato-skills:release-notes` |
| Chimera | Yes, as a mod named `legato-skills` | `<name>` |
| Codex | Not yet | – |

**When a skill of the same name is in your own folders.** Claude Code offers both: your own `release-notes` and `legato-skills:release-notes`. Chimera keeps one: a skill in the project's or your own `.chimera/skills` wins over Legato's, and Legato's wins over one in `.claude/skills` and over Chimera's built-in skills.

**Codex** reads skills only from its own home folder, and Legato does not write there, so Codex agents do not get Legato's skills yet.

## Skills and the sandbox

Each agent gets its own copy of the skills that are on when it starts. In the [sandbox](/legato/security/sandbox/) that copy is read-only, and the skills folder itself is not shown at all, so an agent cannot change the skills that later agents get. The copy holds only what is inside each skill's folder: a link in a skill to a file outside it is left out.
