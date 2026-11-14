# Legato docs site

The documentation site at <https://cpave3.github.io/legato/>, built with [Starlight](https://starlight.astro.build). `.github/workflows/docs.yml` builds it on every pull request and publishes it from `main`.

```bash
task docs:dev     # serve the site with live reload at http://localhost:4321/legato/
task docs:build   # build it into site/dist; fails on a broken link
```

## Where things are

- `src/content/docs/<section>/<page>.md`: the pages. Each folder is one group of the sidebar (`astro.config.mjs`), and a page's place in its group is `sidebar.order`.
- `src/content/docs/index.mdx`: the home page.
- `src/styles/legato.css`: Legato's colours, from the web app's palette.
- Generated pages; do not edit them by hand:
  - `reference/keyboard.md` is written from the TUI's hotkey registry: `go test ./internal/tui/hotkey -update`.
  - `reference/api.md` is written from the web server's route table (`internal/server/route_class_table.go`, with the everyday calls and bodies in `api_reference.go`): `go test ./internal/server -run TestTheAPIReferencePageIsCurrent -update`.
  - `reference/glossary.md` is written from `GLOSSARY.md` at each build (`src/glossary.mjs`), and is not committed.
- `reference/configuration.md` is written by hand, but `go test ./config` fails when a setting of `config.yaml` is not named on it.

## Pages

| Section | Pages |
| --- | --- |
| `start` | `what-is-legato`, `install`, `tour` |
| `running` | `workstation`, `web-server`, `devices`, `desktop-app`, `agent-server`, `instances`, `updates` |
| `using` | `board`, `agents`, `web-ui`, `handoff`, `attention`, `plans`, `reviews`, `review-handoff`, `workflows`, `swarms`, `reports`, `learn` |
| `agent-tools` | `claude-code`, `codex`, `chimera`, `model-settings`, `mcp-tools`, `resources` |
| `integrations` | `jira`, `github`, `staccato`, `yggdrasil`, `argus`, `notifications`, `manticore`, `tmux-status`, `stream-deck`, `even-g2`, `browser-extension` |
| `security` | `credentials-setup`, `credential-broker`, `sandbox`, `web-auth` |
| `reference` | `configuration`, `cli`, `api`, `keyboard`, `glossary` |
| `contributing` | `build`, `architecture` |

## Writing a page

Every page starts with frontmatter and no `#` title of its own; Starlight shows `title`:

```markdown
---
title: Workflows
description: One sentence that says what the page is for. Search results show it.
sidebar:
  order: 40
---
```

- **Who reads it.** A developer who uses Legato, not one who works on its code. Say what they see, what they type and what happens. Leave out Go types, functions and packages; `docs/ai/` is the place for those, and a page may link to it on GitHub for a reader who wants the internals.
- **How it reads.** Plain, short sentences in the active voice, in the present tense, to "you". One topic to a paragraph. Steps as a numbered list, one action to a step. No marketing words ("powerful", "seamless", "simply").
- **Facts.** Take them from the code and from `docs/ai/`. When the two disagree, the code is right. Never guess a default, a flag or a path: look it up, or leave it out.
- **Start with the point.** The first paragraph says what the feature is and why a person would use it. Setup comes next, then use, then details and limits.
- **Names.** Use the words in `GLOSSARY.md`. Call the review packet "review handoff", never "handoff" alone: a handoff is a task sent to a worker instance.
- **Config and commands.** Show settings as YAML and commands in `bash` blocks. Name a setting by its full path in code, `web.tls.enabled`, and link to the [configuration reference](/legato/reference/configuration/) rather than repeating every setting.
- **Links.** Link to another page by its full path with the base: `[Plans](/legato/using/plans/)`. The build fails on a link to a page that does not exist. Link to a file in the repository with its GitHub address: `https://github.com/cpave3/legato/blob/main/<path>`.
- **Asides.** `:::note`, `:::tip`, `:::caution` and `:::danger`, each closed with `:::`. Use them for what a reader must not miss, not for decoration.
- **Not built yet.** Say so at the top in a `:::caution` aside.
