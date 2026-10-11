#!/usr/bin/env node
// Wraps agent HTML exactly as Legato does, without a running Legato.
//
//   node frame.mjs panel <content.html> <out.html> [width in px | full]
//       A host page with the sandboxed frame, as the artifact panel draws it.
//   node frame.mjs saved <content.html> <out.html> [title]
//       The file the panel's Download button saves.
//
// The functions are taken from web/src/lib/agentUi.ts as it is now, so the
// output changes with the code. Serve the output over HTTP to look at it: a
// browser automation tool does not open file: addresses.
import { readFileSync, writeFileSync } from "node:fs"
import { stripTypeScriptTypes } from "node:module"
import { dirname, resolve } from "node:path"
import { fileURLToPath } from "node:url"

const sourcePath = resolve(dirname(fileURLToPath(import.meta.url)), "../../../../web/src/lib/agentUi.ts")
const source = readFileSync(sourcePath, "utf8")
const [from, to] = [source.indexOf("const THEME_VARIABLES"), source.indexOf("const MAX_PROPOSED_MESSAGE")]
if (from < 0 || to < from) {
  console.error(`frame.mjs: ${sourcePath} no longer has the part between THEME_VARIABLES and MAX_PROPOSED_MESSAGE; update this script`)
  process.exit(1)
}
const script = stripTypeScriptTypes(source.slice(from, to)).replace(/export /g, "")
const { buildSandboxDocument, buildStandaloneDocument } = new Function(`${script}\nreturn { buildSandboxDocument, buildStandaloneDocument }`)()

// config.DefaultAgentResourceDomains, unless LEGATO_RESOURCE_DOMAINS (comma separated) says otherwise.
const resourceDomains = (process.env.LEGATO_RESOURCE_DOMAINS ?? "cdnjs.cloudflare.com,cdn.jsdelivr.net,unpkg.com,fonts.googleapis.com,fonts.gstatic.com")
  .split(",").map((host) => host.trim()).filter(Boolean)

const [mode, content, out, extra] = process.argv.slice(2)
if (!["panel", "saved"].includes(mode) || !content || !out) {
  console.error("usage: frame.mjs panel <content.html> <out.html> [width|full]\n       frame.mjs saved <content.html> <out.html> [title]")
  process.exit(2)
}
const html = readFileSync(content, "utf8")
if (mode === "saved") {
  writeFileSync(out, buildStandaloneDocument({ html, title: extra ?? "Artifact", resourceDomains }))
} else {
  const frame = buildSandboxDocument({ html, resourceDomains, fill: true }).replace(/&/g, "&amp;").replace(/"/g, "&quot;")
  const width = !extra || extra === "full" ? "100%" : `${Number(extra)}px`
  writeFileSync(out, `<!doctype html><html><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1"><style>html,body{margin:0;height:100%;background:#09090b}iframe{border:0;display:block;width:${width};height:100vh;margin:0 0 0 auto;border-left:1px solid #27272a}</style></head><body><iframe id="frame" sandbox="allow-scripts" srcdoc="${frame}"></iframe></body></html>`)
}
console.log(`wrote ${out}`)
