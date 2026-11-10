---
title: Keyboard shortcuts
description: Every key of the Legato TUI, page by page.
sidebar:
  order: 30
---

<!-- Written by go test ./internal/tui/hotkey -update from the hotkey registry. Do not edit. -->

In the TUI, press `?` to see these keys. The help shows only the keys that work with your configuration. This page lists all of them.

## Everywhere

| Key | Action |
| --- | --- |
| `?` | Toggle keyboard reference |
| `!` | Answer pending approvals |
| `R` | Open the Reeve |
| `q/ctrl+c` | Quit Legato |

## Board

### Navigation

| Key | Action |
| --- | --- |
| `h` | Move to previous column |
| `l` | Move to next column |
| `j` | Move down within column |
| `k` | Move up within column |
| `g` | Jump to first card |
| `G` | Jump to last card |
| `1` | Jump to column |
| `2` | Jump to column |
| `3` | Jump to column |
| `4` | Jump to column |
| `5` | Jump to column |

### Actions

| Key | Action |
| --- | --- |
| `enter` | Open task detail |
| `c` | Open task context menu |
| `m` | Move task |
| `v` | Move task to workspace |
| `n` | Create new task |
| `d` | Delete task |
| `i` | Import remote ticket (with a ticket provider such as Jira) |
| `p` | Link PR to task |
| `ctrl+r` | Refresh linked PR |
| `ctrl+p` | Rediscover task PR |
| `w` | Switch workspace |
| `/` | Search tasks |
| `r` | Force sync |
| `X` | Archive done cards |
| `B` | Browse archived tasks |
| `b` | Create task worktree (with Yggdrasil worktrees on) |
| `a` | Spawn agent for task |
| `t` | Open task agent terminal |

### Swarm

| Key | Action |
| --- | --- |
| `S` | Start a swarm |
| `C` | Cancel active swarm |

### Views

| Key | Action |
| --- | --- |
| `z` | Open or collapse columns with no cards |
| `A` | Open agents view |
| `T` | Open reporting view |
| `V` | Open review tours |
| `P` | Open implementation plans |
| `I` | Open attention inbox |

## Task detail

| Key | Action |
| --- | --- |
| `esc/q` | Return to board |
| `j` | Scroll down |
| `k` | Scroll up |
| `d` | Scroll half page down |
| `u` | Scroll half page up |
| `g` | Jump to top |
| `G` | Jump to bottom |
| `y` | Copy description |
| `Y` | Copy full task context |
| `m` | Move task |
| `v` | Move task to workspace |
| `D` | Delete task |
| `o` | Open remote task |
| `t` | Edit task title |
| `e` | Edit Legato task details |

## Agents

| Key | Action |
| --- | --- |
| `esc/q` | Return to board |
| `j` | Select next agent |
| `k` | Select previous agent |
| `w` | Select the next agent that needs you |
| `s` | Spawn agent |
| `X` | Kill agent |
| `R` | Reload agent, resuming its conversation |
| `U` | Reload agent out of the sandbox, or back into it, resuming its conversation |
| `enter/tab` | Attach to agent session |
| `m` | Send macro |
| `g` | Set task group |
| `M` | Open swarm agent actions |
| `l` | Toggle worker details |
| `n` | Toggle notifications (with ntfy notifications set up) |
| `v` | Start voice dictation (with voice input on) |
| `o` | Open the agent's cards and artifacts in the web UI |
| `a` | Ask the Reeve about the selected agent |

### In the list

| Key | Action |
| --- | --- |
| `e` | Show every agent at once |

### In the ensemble

| Key | Action |
| --- | --- |
| `h` | Select the agent to the left |
| `l` | Select the agent to the right |
| `e` | Show the agent list and one terminal |

## Reports

| Key | Action |
| --- | --- |
| `esc/q` | Return to board |
| `tab` | Change report period |
| `h/left` | Previous period |
| `l/right` | Next period |
| `j/down` | Scroll down |
| `k/up` | Scroll up |
| `C` | Copy report markdown |

## Reviews

### In the queue

| Key | Action |
| --- | --- |
| `esc/q` | Return to board |
| `j/down` | Select next review |
| `k/up` | Select previous review |
| `enter` | Open review tour |
| `r` | Refresh review queue |
| `x` | Delete review |

### In a review tour

| Key | Action |
| --- | --- |
| `j/down` | Select next step |
| `k/up` | Select previous step |
| `d/ctrl+d` | Scroll diff down |
| `u/ctrl+u` | Scroll diff up |
| `g` | Jump to top of diff |
| `G` | Jump to bottom of diff |
| `space` | Toggle step reviewed |
| `a` | Ask a question |
| `c` | Complete review |
| `x` | Delete review |
| `esc/q` | Return to review queue |

## Plans

### In the queue

| Key | Action |
| --- | --- |
| `esc/q` | Return to board |
| `j/down` | Select next plan |
| `k/up` | Select previous plan |
| `enter` | Open plan |

### Reading a plan

| Key | Action |
| --- | --- |
| `esc/q` | Return to plan queue |
| `j/down` | Scroll down |
| `k/up` | Scroll up |
| `tab` | Select next question |
| `e` | Answer selected question |
| `c` | Comment on plan |
| `?` | Ask about plan |
| `a` | Approve plan |
| `x` | Reject plan |
| `r` | Request plan changes |
| `o` | Reopen plan |

## Attention

| Key | Action |
| --- | --- |
| `esc/q` | Return to board |
| `j/down` | Select next item |
| `k/up` | Select previous item |
| `enter` | Execute primary action |
| `d` | Dismiss attention item |

## Spawn dialog

### On the engram row

| Key | Action |
| --- | --- |
| `h/left` | Previous engram |
| `l/right` | Next engram |
| `r` | Remember the engram for this repository, or clear its default |
