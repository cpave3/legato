---
name: improve-agent-artifacts
description: Improve what Legato-launched agents show in the web UI (artifacts and cards) when an output is poor - plain, hand-drawn, broken, or in the wrong form. Use when the person shows an artifact and is not satisfied with it, or asks to tune the guidance agents get about the Legato tools. Covers how to measure the output, name the gap, change the guidance or the frame, and verify with a real agent in a scratch Legato.
---

# Improve what agents show

Agents that Legato launches get tools to show an **artifact** (side panel) or a **card** (in the conversation). What they make depends on the guidance Legato gives them and on what the sandboxed frame can do. This skill is the loop for making those outputs better. Read `docs/ai/agent-mcp.md` first: it is the reference for everything named here.

Use the `tdd` skill for every code change, as the project requires.

## Keep the person in the loop

The person asked for a better output, not for a feature. Before each step, say in one or two sentences what you found and what you will do next. **Ask before you build anything larger than a text change** (a new frame capability, a new tool argument, a new component). In the session that produced this skill, a helper was built without asking; the result was good, and the person still had to stop the work to find out what was happening.

## The loop

1. Get the evidence: the content the agent made, measured.
2. Name the gap: one of four kinds.
3. Prove the target by hand in the real frame.
4. Change the smallest thing that closes the gap.
5. Verify with a real agent and a neutral prompt.
6. Record what was observed.

Do not skip step 1 or step 5. A guess about why an output is poor is often wrong, and guidance that reads well can still change nothing.

## 1. Get the evidence

Read the item from the person's database. `items.sh` opens it read-only.

```bash
S=.claude/skills/improve-agent-artifacts/scripts
$S/items.sh ~/.local/share/legato/legato.db                      # list: id, task, placement, format, size, title
$S/items.sh ~/.local/share/legato/legato.db ui-<id> /tmp/x.html  # write one item's content
$S/measure.py /tmp/x.html
```

`measure.py` counts what the agent used. It does not judge the result. Read the counts against what the person said:

| The person says | The counts show | Kind of gap |
| --- | --- | --- |
| "It looks constrained", "can it do HTML?" | HTML, but no `<svg>`, no `<script>`, no library; arrows typed as text | A: it did not know that it could |
| "Correct, but boring" | Markdown, or HTML with little style and no script | B: it took the easy form |
| "It is broken", "nothing happens when I click" | Scripts and diagrams are there, and the page fails in the browser | C: it could not get it right |
| "Too small", "does not fit" | A good page that ignores the panel's width | D: it did not know the surface |

Also read what the agent was told at that time (`git log -p internal/cli/mcp.go`). The cause is frequently a sentence that is already there.

## 2. Name the gap

- **A. It did not know that it could.** The guidance said what the frame forbids and not what works. Fix: say what works, in the tool description, where the agent decides how to write the content.
- **B. It took the easy form.** The guidance offered a low-effort path and the agent took it. "format markdown is the quick form" was such a sentence. Fix: remove the easy-path wording, and state the properties of a good result ("a short summary at the top, a diagram that carries the structure, the detail of a part shown on a click").
- **C. It could not get it right.** The frame or a library has a trap that an agent cannot see: it gets no feedback about what its page did. Fix: make the frame do that part, then teach the frame's way with one example. Text that warns about a trap is the weaker fix.
- **D. It did not know the surface.** Fix: state the constraint together with its consequence ("the panel is narrow: one column, short node labels").

One more case has no fix in text: a small model can ignore the guidance and answer in plain text, wherever the guidance is placed.

## 3. Prove the target by hand

Before you tell agents to do something, do it yourself in the real frame. Write the page you wish the agent had made, then wrap it exactly as Legato does:

```bash
node $S/frame.mjs panel /tmp/target.html /tmp/out/panel.html 560   # the frame at the panel's default width
node $S/frame.mjs panel /tmp/target.html /tmp/out/wide.html full
node $S/frame.mjs saved /tmp/target.html /tmp/out/saved.html "Title"   # what Download saves
(cd /tmp/out && python3 -m http.server 3988 --bind 127.0.0.1)          # browsers do not automate file: addresses
```

Look at it in a browser at 560 px (panel), full width, and 390 px (phone). Open every tab, click every control.

This is evidence, not a deliverable. It answers two questions:

- Is the target possible in the frame?
- What did you need to know that an agent does not know? Each such thing is a trap (kind C). Two examples from the first use of this loop: Mermaid draws a 16 px diagram inside a hidden tab, and it names a node `mermaid-<n>-flowchart-<name>-<i>` with no `data-id`, so a click handler written from memory finds nothing.

Show the person a screenshot of the target. It is the fastest way to agree on what "better" means.

## 4. Change the smallest thing

Where the guidance is, and who reads it:

| Text | File | Read by the model when |
| --- | --- | --- |
| `MCPGuidance`: when to use which tool | `internal/cli/mcp.go` | Every session start (server instructions for Claude Code, launch prompt for Codex and Chimera) |
| Tool descriptions (`show_artifact`, `show_card`, `show_components`) | `internal/cli/mcp.go` | It looks the tool up. Claude Code and Codex keep MCP tools out of the tool list until then |
| `htmlGuide`, `diagramGuide`: what the frame allows and does | `internal/cli/mcp.go` | Part of the two HTML tools' descriptions |
| `UIComponentGuide`: the component catalog | `internal/service/agent_ui_components.go` | Part of `show_components` |
| The frame itself: theme, `legato.*` | `web/src/lib/agentUi.ts` | Never: the agent only knows what the texts above say about it |

Rules for the text:

- Say what works, not only what is forbidden.
- Give concrete triggers ("prefer a card to a Markdown table") and concrete properties of a good result. "Make it good" changes nothing.
- Do not leave an easy path in the wording.
- Give one example, and run it in the frame exactly as written before you commit it.
- State nothing about the frame that you did not check in a browser.
- Offer only what this session can do. The HTML tools do not mention diagrams when no allowed host serves Mermaid (`drawsDiagrams`).
- Every session pays for this text. Print the sizes before and after, and say them to the person.
- The guidance must not name a tool with an agent tool's prefix: each agent tool adds its own.

Tests come first:

- `internal/cli/mcp_test.go` asserts the phrases that carry an instruction (`mcpDescriptions` gives each tool's description for a set of allowed hosts). Assert what must be said and what must not be taught.
- `web/src/lib/__tests__/agentUiDiagrams.test.ts` runs the frame's own script: it writes the document Legato builds into a frame of the test page, with a stand-in for the Mermaid library. Use the same method for any new frame behaviour.
- jsdom does not lay anything out and treats a written frame as already loaded. What depends on layout or load order is checked in a real browser (step 3), not in a unit test.

When the fix is in the frame:

- Keep the interface tiny. The agent learns it from a few sentences.
- Put it in the saved file too (`buildStandaloneDocument`), or downloads lose it.
- A page that does not use the new thing must behave as before.

## 5. Verify with a real agent

`scratch.sh` runs a scratch Legato built from the working tree, with its own database, sockets, tmux server and binary.

```bash
$S/scratch.sh up                                  # builds web/ and the binary, starts the server on :3987
T=$($S/scratch.sh task "Show the architecture")
$S/scratch.sh agent $T chimera                    # or claude-code, codex; third argument is the working directory
$S/scratch.sh pane $T                             # wait until the agent tool is ready for input
$S/scratch.sh ask $T "Examine <feature> of this codebase and show me its architecture. Do not change any files."
$S/scratch.sh wait 300                            # until one more item is shown; if nothing comes, read the pane
$S/scratch.sh key $T Enter                        # answer a prompt in the pane, after you read it
$S/scratch.sh items
$S/scratch.sh dump ui-<id> /tmp/after.html && $S/measure.py /tmp/after.html
$S/scratch.sh url $T ui-<id>                      # address to open, and the token to paste
$S/scratch.sh down
```

- Use a **neutral prompt**: the words the person used, with no hint about format and no tool name. Do not end it with "just answer"; that suppresses cards.
- Use the same prompt before and after the change, and the agent kind that produced the poor output.
- Compare the two `measure.py` outputs. Then look at the result in the browser: the counts say what was used, not whether it works.
- In the browser, the web UI asks for the token (placeholder `Paste token...`, button `Connect`). Agent HTML is in the page's second frame. A click on an SVG node needs `dispatchEvent("click")`: the label on top of it intercepts a pointer click.
- Check the cases that fail silently: every hidden tab, a click on a node, the panel at its default width, the downloaded file.
- A question whose answer is short must still get a plain text answer. Check that the change did not make agents show a card for everything.
- One run with one agent is one data point. Say so when you report. Run a second agent kind when the change is in `MCPGuidance`, since the three tools receive it by different routes.

`ask` types into the agent's pane. It was tested with Chimera and Claude Code. For Codex, check the pane before and after.

**When `wait` shows nothing, read the pane** (`scratch.sh pane $T 40`). The agent can be waiting for the person:

- An agent tool in a manual permission mode stops at each shell command it wants to run. Claude Code did so three times in one run, for commands that read files and for two checks of its own page. Read the whole command. Approve it (`scratch.sh key $T Enter`) only when the person would: a command that reads, and writes nothing.
- A short answer is plain text, and nothing is shown. That is correct behaviour, not a failure.

Runs differ much in length. Chimera took about 3 minutes for an architecture page. Claude Code on its default model, at a high effort setting, took about 12 minutes: before it showed the page, it parsed its diagrams with the Mermaid in `web/node_modules` and ran its script against a stand-in for the frame's helper. An agent cannot see what it drew, and a careful one spends time on that.

## 6. Record

- `docs/ai/agent-mcp.md`: what agents did before, what changed, and what was observed after ("How an agent learns that the tools exist", "Diagrams in a frame"). Write only what you measured.
- Add a row to the history below.

## Safety

- Never rebuild `./legato` in the checkout. The person's running Legato reloads from it, and every live session reloads its mod.
- Never run a binary built from the working tree against the person's database: it migrates the schema past what their installed binary supports. Read that database only with `sqlite3 -readonly` (`items.sh` does).
- `scratch.sh up` runs `pnpm build`, which rewrites `internal/server/static/dist/`. Those files are ignored by Git and are embedded only at the next build.
- Scratch agents are real: they use the person's agent tool configuration and model credits, and they can change files in their working directory. Say "Do not change any files" in the prompt, and stop them with `scratch.sh down`.
- Do not find processes with `pgrep -f` or `pkill -f`: the pattern matches your own shell. `scratch.sh` matches the scratch binary by its executable path.
- Keep the shell's working directory at the repository root.
- Other agents may share the worktree. Do not revert or commit changes that are not yours.

## History

| The person said | Evidence | Gap | Fix | Observed after |
| --- | --- | --- | --- | --- |
| "It feels constrained by primitives. Can it do arbitrary HTML?" | 11 KB of HTML from Chimera: no SVG, no script, no library, arrows typed as text | A | The HTML tools say what works in the frame, not only what is forbidden. `MCPGuidance` says custom HTML is there to be used in full | A Markdown artifact with a Mermaid flowchart, no typed arrows |
| "Okay but very visually boring. Keep the clickables, use Mermaid for layout, add styling" | Markdown with two Mermaid fences and tables; no style, no script | B, then C and D found in step 3 | `show_artifact` asks for a page the person explores, not a document; Markdown is for plain prose; the panel's width is stated. The frame draws Mermaid itself (`legato.diagrams`, named node colors, `legato.selectNode`) | Chimera: an HTML page with a summary, four tabs, four diagrams with named colors, and details on a node click. Claude Code: five tabs, five diagrams, a walk-through, a detail sheet, and a button that proposes a message. In both, diagrams in hidden tabs drew at full size; the downloaded file of the first worked |
| "Downloads lose the styles" | The raw file had a transparent background, black text, a serif font | Not guidance: the theme came from the frame | Download saves a whole page with the theme in it (`buildStandaloneDocument`) | The file looks the same outside Legato |
