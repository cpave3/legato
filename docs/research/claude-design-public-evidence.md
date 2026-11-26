# Claude Design — public evidence findings (isolated feasibility investigation)

**Investigation type:** Desktop research of primary, official Anthropic sources only. No authenticated data was accessed; no Claude Design account, product UI, or API endpoint was used. Every claim below is either a documented fact with a citation, or is explicitly listed as an unknown.
**Access date for all sources:** 2026-11-26 (UTC).
**Constraint honoured:** this file is the only artifact written by this investigation.

---

## 1. What Claude Design is (documented)

- Launched **2026-04-17** as an **Anthropic Labs** product: "lets you collaborate with Claude to create polished visual work like designs, prototypes, slides, one-pagers, and more." Powered by Claude Opus 4.7; research preview for Pro, Max, Team, Enterprise. ([anthropic.com — launch announcement](https://www.anthropic.com/news/claude-design-anthropic-labs), 2026-04-17, accessed 2026-11-26)
- As of **September 2026**, Claude Design works **inside any conversation with Claude, including Claude Code and the Artifacts tab**; it is "one of the templates you can start an artifact from." ([claude.com — resources update](https://claude.com/resources/articles/claude-design-stays-on-brand-for-daily-work), 2026-06-17 with Sept 2026 update, accessed 2026-11-26; [support.claude.com — Get started with Claude Design](https://support.claude.com/en/articles/14604416-get-started-with-claude-design), accessed 2026-11-26)
- Available on **Free, Pro, Max, Ultra, Team, and Enterprise** plans, on by default (Enterprise default-on 2026-10-15). The **standalone site `claude.ai/design` closes 2026-12-14**. ([Get started](https://support.claude.com/en/articles/14604416-get-started-with-claude-design); [Move from standalone Claude Design to Claude](https://support.claude.com/en/articles/17440474-move-from-standalone-claude-design-to-claude), accessed 2026-11-26)

## 2. Generation vs. canvas editing (documented)

Official framing is conversation + canvas, with three distinct edit channels:

- "Claude Design pairs a conversation with a canvas. You describe what you want, and Claude generates a working design on the canvas beside the conversation." ([Get started](https://support.claude.com/en/articles/14604416-get-started-with-claude-design), accessed 2026-11-26)
- Structure of a design, documented verbatim: "A design is a canvas of artboards. Each artboard holds layers, such as text, shapes, and images, and a layer can hold other layers inside it." (same source)
- Three refinement channels, documented with a "when to use which" rule: **chat** for structural changes / alternatives ("Show me 2–3 alternative layouts"); **inline comments** on a selected element for targeted changes; **direct canvas editing** ("drag, resize, and align elements directly") for quick visual/aesthetic shifts. Same source; also [claude.com/product/design](https://claude.com/product/design) ("Fine-grained control" section), accessed 2026-11-26.
- Claude may ask clarifying questions and can apply design-system components automatically, self-checking output "against your design system, and makes corrections before you see them." ([Get started](https://support.claude.com/en/articles/14604416-get-started-with-claude-design); [product/design FAQ](https://claude.com/product/design), accessed 2026-11-26)
- Keyboard/accessibility editing of the canvas is fully documented (F6 areas, layer tree, shortcuts table). ([Get started](https://support.claude.com/en/articles/14604416-get-started-with-claude-design), accessed 2026-11-26)
- From Claude Code, `/design` drafts "the design as artboards on one canvas and publishes the canvas as a Claude Design artifact"; "Select an element on an artboard and change it, and your edits save automatically. You can export each artboard as PNG or PDF." `/design-sync` pulls a design system into Claude Code. ([code.claude.com — Share session output as artifacts](https://code.claude.com/docs/en/artifacts); [code.claude.com — Week 34 release notes](https://code.claude.com/docs/en/whats-new/2026-w34), accessed 2026-11-26)

**Feasibility note:** generation is conversational; editing is a real structured canvas (artboards → layers), not just re-prompting. Both are first-class in the docs.

## 3. The `.dc.html` hypothesis — **NOT confirmed by any official source**

- **No page on anthropic.com, claude.com, support.claude.com, code.claude.com, or docs.claude.com returned by search mentions the `.dc.html` extension.** A targeted search restricted to those domains (`site:` queries for `"dc.html"`) returned only official pages whose indexed snippets describe formats as "HTML" / "standalone HTML" / "Project HTML" — never `.dc.html`. (Searches run 2026-11-26.)
- What official docs do document about file formats:
  - Claude Code artifact publishes: "The published file must be **.html, .htm, or .md**, and must decode as UTF-8, or as little-endian UTF-16 by its byte-order mark." ([code.claude.com/docs/en/artifacts](https://code.claude.com/docs/en/artifacts), accessed 2026-11-26)
  - Claude Design exports: "Export as standalone HTML", "Download as .zip", and in the standalone version, Share → "Project HTML" → "Project archive" yields "a .zip file with every file in the project." ([Get started](https://support.claude.com/en/articles/14604416-get-started-with-claude-design); [Move from standalone…](https://support.claude.com/en/articles/17440474-move-from-standalone-claude-design-to-claude), accessed 2026-11-26)
- The `.dc.html` extension **does circulate in third-party sources** (GitHub repos of extracted/leaked system prompts, community skill marketplaces, third-party blogs describing "Design Component" files). These are **not official Anthropic sources** and were not treated as evidence. Anything built on the assumption that `.dc.html` is a documented, stable, public format is currently **unsupported**.
- **Verdict:** `.dc.html` = **undocumented hypothesis**. Documented reality: designs are *artifacts* on claude.ai; exports are HTML bundles/zips, PDF, PPTX, Google Slides, PNG/PDF per artboard (Claude Code view). Internal file format of a design artifact is **not publicly specified**.

## 4. Exports (documented)

From the Export menu ([Get started](https://support.claude.com/en/articles/14604416-get-started-with-claude-design), accessed 2026-11-26):

- Download as **.zip**; Export as **PDF**; Export as **PPTX**; Export to **Google Slides**; Export as **standalone HTML**.
- Per-artboard **PNG or PDF** export from the Claude Code-published design artifact. ([code.claude.com/docs/en/artifacts](https://code.claude.com/docs/en/artifacts), accessed 2026-11-26)
- **Send to connectors:** Adobe Experience Manager, Adobe for Creativity, Adobe Journey Optimizer, Base44, Canva, Gamma, HubSpot, Hyperframes, Lovable, Miro, Netlify, Replit, v0, Vercel, Wix. (same source; partner quotes at [product/design](https://claude.com/product/design))
- **Handoff to Claude Code:** "hand-off bundle" (launch announcement) / "Send to local coding agent" and "Send to Claude Code Web" (Get started list). Same sources.
- Standalone-era extras: bulk download of all owned projects as .zip; "It's free and doesn't count toward your usage limits." ([Move from standalone…](https://support.claude.com/en/articles/17440474-move-from-standalone-claude-design-to-claude), accessed 2026-11-26)

## 5. Sharing & auth model (documented)

- "Designs start private to you." Share dialog offers **Can view / Commenter / Can edit**; on Team/Enterprise: "Only people invited", "Anyone at [organization]", or "Anyone with the link" (last one only if an Owner enables External sharing). Email invitations (beta) up to 50 external invitees per artifact. ([support.claude.com — Share artifacts](https://support.claude.com/en/articles/9547008-share-artifacts); [Get started](https://support.claude.com/en/articles/14604416-get-started-with-claude-design), accessed 2026-11-26)
- **Auth required for shared/org viewing:** "To open an artifact shared with them or with your organization, people sign in to Claude." Most "Anyone with the link" artifacts open **without** sign-in; a public-link viewer who is not signed in sees "the parts that use connected apps or Claude don't work." ([Share artifacts](https://support.claude.com/en/articles/9547008-share-artifacts), accessed 2026-11-26)
- **Preview sandbox + token auth (Design-specific, documented):** "Claude Design project previews run inside a sandboxed iframe on a separate content domain… The code in a preview can't reach your Claude account, your login, or the editor." "Access to a preview is controlled by **signed tokens**—short-lived passes… Claude re-checks these tokens against your sharing permissions every time someone opens the preview, so when you remove someone's access, they're locked out right away." ([support.claude.com — Claude Design admin guide](https://support.claude.com/en/articles/14604406-claude-design-admin-guide-for-team-and-enterprise-plans), accessed 2026-11-26)
- Artifact viewer loads each artifact from a sandboxed **`*.claudeusercontent.com`** origin. ([code.claude.com/docs/en/artifacts](https://code.claude.com/docs/en/artifacts), accessed 2026-11-26)
- The original launch framing "organization-scoped sharing" applied to the standalone product; the current integrated product uses the general artifact sharing model above. ([launch announcement](https://www.anthropic.com/news/claude-design-anthropic-labs), accessed 2026-11-26)

## 6. Assets & revisions (documented facts and gaps)

**Inputs/assets (documented):**
- Attach "screenshots, images, or existing assets", DOCX/PPTX/XLSX files, linked code repositories; web capture of existing site elements (launch announcement). ([Get started](https://support.claude.com/en/articles/14604416-get-started-with-claude-design); [launch announcement](https://www.anthropic.com/news/claude-design-anthropic-labs), accessed 2026-11-26)
- **Design systems** are built from codebases (React via `/design-sync`), decks/docs, brand assets (logos, palettes, typography), Figma files, or uploads; extracted into color palette, typography, components, layout patterns; published/default per organization; admins can restrict publishing/default/deleting. ([support.claude.com — Set up your design system](https://support.claude.com/en/articles/14604397-set-up-your-design-system-in-claude-design); [admin guide](https://support.claude.com/en/articles/14604406-claude-design-admin-guide-for-team-and-enterprise-plans), accessed 2026-11-26)
- A migrated design system "keeps its files as they were"; design systems/projects move to Claude **as artifacts**; chats and comments **do not move**. ([Move from standalone…](https://support.claude.com/en/articles/17440474-move-from-standalone-claude-design-to-claude), accessed 2026-11-26)

**Revisions (documented gaps):**
- Known limitation, verbatim: "**Version history: Claude Design doesn't have version history yet.**" Workaround documented: ask Claude to "save what we have" as a separate project. ([Get started](https://support.claude.com/en/articles/14604416-get-started-with-claude-design), accessed 2026-11-26)
- On the Claude Code artifact side, each publish creates a version that the **Compliance API** can retrieve: `GET /v1/compliance/apps/code/artifacts/{artifact_id}/versions/{version_id}`. ([code.claude.com/docs/en/artifacts](https://code.claude.com/docs/en/artifacts), accessed 2026-11-26)

## 7. Security & limits (documented)

- **Sandboxing:** previews in sandboxed iframes on a separate Anthropic-operated content domain; code cannot reach the Claude account/login/editor; signed-token access re-checked per open. ([admin guide](https://support.claude.com/en/articles/14604406-claude-design-admin-guide-for-team-and-enterprise-plans), accessed 2026-11-26)
- **CSP on published artifact pages:** external requests limited to Google Fonts + five CDN hosts (cdnjs, unpkg, Tailwind, jQuery, jsDelivr `/npm/`); no backend; viewer-initiated downloads only via declared capability; 16 MiB rendered-size cap; `.html/.htm/.md` sources only. ([code.claude.com/docs/en/artifacts](https://code.claude.com/docs/en/artifacts), accessed 2026-11-26)
- **Claude Code artifact publishing availability:** requires Anthropic API (not Bedrock/GCP/Foundry), a claude.ai-signed session (API-key sessions cannot publish), and organizations **without CMEK, HIPAA config, or ZDR**. These are Claude Code publishing conditions, not proof of identical eligibility for every Design surface. ([code.claude.com/docs/en/artifacts](https://code.claude.com/docs/en/artifacts), accessed 2026-11-26)
- **Usage limits:** design activity "counts toward the same usage limits as the rest of Claude" (shared pool with Claude Code); unavailable when limits reached; extra usage optional. ([Get started](https://support.claude.com/en/articles/14604416-get-started-with-claude-design), accessed 2026-11-26)
- **Admin/retention:** artifacts on/off per org; retention periods (private vs shared) with auto-deletion; Compliance API to list/read-version/delete artifacts and audit publish/share/delete activity; public sharing off by default on Team/Enterprise. ([code.claude.com/docs/en/artifacts](https://code.claude.com/docs/en/artifacts); [admin guide](https://support.claude.com/en/articles/14604406-claude-design-admin-guide-for-team-and-enterprise-plans), accessed 2026-11-26)
- **Known product limitations (documented):** comment persistence issues; very large codebases cause lag; multi-person simultaneous editing "is still basic and may not work reliably"; no version history; no keyboard way to *add* new layers/artboards. ([Get started](https://support.claude.com/en/articles/14604416-get-started-with-claude-design), accessed 2026-11-26)

## 8. Documented facts vs. unknowns — summary

**Documented (cited above):** artifact/canvas model (artboards + layers); chat vs. inline-comment vs. direct-edit channels; design-system import/self-check; export formats (zip, PDF, PPTX, Google Slides, standalone HTML, PNG/PDF per artboard, connectors, Claude Code handoff); sharing tiers with org-scoped default and owner-gated external sharing; signed-token preview auth + sandboxed content-domain iframes; shared usage pool; admin toggles, retention, Compliance API; standalone `claude.ai/design` closes 2026-12-14.

**Unknown / not publicly documented (as of 2026-11-26):**
1. **Internal design file format** — `.dc.html` is not mentioned in any official source; the on-disk/internal representation of a design artifact is unspecified. Any "Design Component" file-format hypothesis is unverified.
2. **Public API for Claude Design itself** — no official REST/SDK surface for creating or editing designs was found; only the Enterprise *Compliance API* (list/read/delete artifacts) and Claude Code commands (`/design`, `/design-sync`) are documented.
3. **Revision/version history in the product** — explicitly absent ("doesn't have version history yet"); only publish-versions retrievable via Compliance API.
4. **Exported-HTML semantics** — official docs name "standalone HTML" / "Project HTML" exports but do not document their internal structure, scriptability, or auth model of exported bundles (exports are downloads; only in-product sharing carries the signed-token model).
5. **Asset limits** — per-artifact/per-design-system size caps are documented only indirectly (migration failures "too large for an artifact"; 16 MiB artifact render cap); no published quota table for design assets.

## 9. Sources (all accessed 2026-11-26)

| # | Source | URL |
|---|--------|-----|
| 1 | Anthropic — Introducing Claude Design by Anthropic Labs (2026-04-17) | https://www.anthropic.com/news/claude-design-anthropic-labs |
| 2 | Claude — Claude Design product page & FAQ | https://claude.com/product/design |
| 3 | Claude Help Center — Get started with Claude Design | https://support.claude.com/en/articles/14604416-get-started-with-claude-design |
| 4 | Claude Help Center — Claude Design admin guide for Team and Enterprise plans | https://support.claude.com/en/articles/14604406-claude-design-admin-guide-for-team-and-enterprise-plans |
| 5 | Claude Help Center — Set up your design system in Claude Design | https://support.claude.com/en/articles/14604397-set-up-your-design-system-in-claude-design |
| 6 | Claude Help Center — Move from standalone Claude Design to Claude | https://support.claude.com/en/articles/17440474-move-from-standalone-claude-design-to-claude |
| 7 | Claude Help Center — Share artifacts | https://support.claude.com/en/articles/9547008-share-artifacts |
| 8 | Claude Code Docs — Share session output as artifacts (incl. /design, CSP, Compliance API) | https://code.claude.com/docs/en/artifacts |
| 9 | Claude Code Docs — Week 34 2026 release notes (/design research preview) | https://code.claude.com/docs/en/whats-new/2026-w34 |
| 10 | Claude Resources — Claude Design now stays on brand for daily work (2026-06-17) | https://claude.com/resources/articles/claude-design-stays-on-brand-for-daily-work |

**Method note:** capabilities were accepted only where stated on the pages above (or pages directly linked from them). Third-party pages surfaced by search (community skills, leaked-prompt repos, comparison blogs) were used solely to identify the `.dc.html` claim's provenance and were explicitly **not** treated as evidence of official capability. No authenticated pages, dashboards, or APIs were accessed.

## Parent investigation corrections and additions

- Code artifacts' **no backend** does not mean no live integrations: the same official Code page documents brokered MCP connectors (viewer-account permissions by default), side-effecting actions, permission prompts, activated comment threads, automatic comment watches and live republishing with stale-write conflict handling. Public-link-only viewers cannot comment; signed-in organization editors can activate threads. Treat this as the **adjacent artifact agent experience**, not proof that every Design surface exposes every connector/control.
- The Code artifact CSP allows fetch/XHR/WebSocket to its own origin and Google Fonts hosts; it is not equivalent to Legato's `connect-src 'none'` policy. Google Fonts and five CDN hosts are documented; scope these limits to ordinary published Code artifact pages, not an undocumented Design runtime implementation.
- Current help explicitly documents layer hierarchy, group/ungroup, text editing, copy/paste, duplicate/delete, undo/redo, reordering within rows/columns/grids, zoom/fit and layout guides. This is substantially beyond HTML viewing. The admin guide says presentations now have Claude Slides; the launch/export pages still list deck/PPTX/Slides workflows. Scope and product packaging changed over time.
- Cameron's supplied third-party purported format reference was subsequently read by the parent investigator. Its provenance, hypotheses and independent implementation implications are assessed in `design-canvas-report.md`; none of its code or prompts was copied into this spike.
