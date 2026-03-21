---
name: "Cleanup"
description: "Review codebase state and prompt to update CLAUDE.md and README with new learnings"
category: Maintenance
tags: [docs, cleanup, maintenance]
---

Review the current state of the codebase and update documentation with any new learnings.

**Steps:**

1. **Gather context**
   - Read `CLAUDE.md` and `README.md` (if it exists)
   - Run `git log --oneline -20` to see recent changes
   - Run `git diff HEAD~10 --stat` (or fewer commits if needed) to understand what areas changed recently
   - Scan key directories for new files or patterns not yet documented

2. **Identify gaps**

   Compare what's documented against current reality. Look for:
   - New packages, files, or modules not mentioned in CLAUDE.md
   - Changed interfaces, signatures, or patterns that are documented with stale info
   - New keybindings, commands, config fields, or CLI subcommands
   - Architecture changes (new layers, renamed concepts, removed features)
   - New testing patterns or development workflows
   - Anything a future contributor (or future Claude session) would need to know

3. **Propose updates**

   For each gap found, show the user:
   - **What changed**: brief description
   - **Where it should go**: which file and section
   - **Proposed text**: the actual addition or edit

   Present all proposals together so the user can review before any changes are made.

4. **Apply approved changes**

   After user approval, update:
   - `CLAUDE.md` — project internals, architecture, conventions, development notes
   - `README.md` — user-facing documentation (if it exists and changes are relevant)

   Do NOT create a README.md if one doesn't exist. Only update what's already there.

5. **Summary**

   Show what was updated and any items you noticed but chose not to document (e.g., things that are obvious from the code, ephemeral state, or already covered).

**Guidelines:**
- CLAUDE.md is for Claude/developer context — architecture, conventions, patterns, gotchas
- README.md is for users — setup, usage, features
- Don't duplicate between the two
- Don't remove existing documentation unless it's clearly wrong or stale
- Keep CLAUDE.md entries concise — one-liner per concept, same style as existing entries
- When in doubt, ask the user whether something belongs in docs
