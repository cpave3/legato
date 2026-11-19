import { readFile } from "node:fs/promises"
import { createContext, runInContext } from "node:vm"

const manifest = JSON.parse(await readFile(new URL("../dist/manifest.json", import.meta.url), "utf8"))
if (!manifest.permissions?.includes("sidePanel") || manifest.side_panel?.default_path !== "sidepanel.html") {
  throw new Error("manifest must register the Side Panel API and sidepanel.html")
}
await readFile(new URL("../dist/sidepanel.html", import.meta.url), "utf8")

// tickets.js and scope.js are not in the manifest: the service worker
// registers them at run time for each ticket provider's site
// (src/ticket-sites.ts) and each site Scope is allowed on (src/scope-origins.ts).
// scope-source.js is run in a page's MAIN world (src/scope-runtime.ts).
const contentScripts = new Set(["tickets.js", "scope.js", "scope-source.js"])
for (const declaration of manifest.content_scripts ?? []) {
  for (const script of declaration.js ?? []) contentScripts.add(script)
}
for (const script of contentScripts) {
  const source = await readFile(new URL(`../dist/${script}`, import.meta.url), "utf8")
  if (/^\s*(?:import|export)\s/m.test(source)) {
    throw new Error(`${script} contains ES module syntax, but Manifest V3 content scripts execute as classic scripts`)
  }
}

// executeScript answers scope-source.js's last value, so the script must be
// one expression whose value is the reading, and must leave the page no
// global and no React DevTools hook. Run in a page with no marked element,
// its value is null.
{
  const source = await readFile(new URL("../dist/scope-source.js", import.meta.url), "utf8")
  const page = { document: { querySelector: () => null, defaultView: null } }
  const context = createContext(page)
  const before = new Set(Object.keys(page))
  const value = runInContext(source, context)
  if (value !== null) throw new Error(`scope-source.js must evaluate to its reading (null on an unmarked page), not ${String(value)}`)
  const added = Object.keys(page).filter((key) => !before.has(key))
  if (added.length) throw new Error(`scope-source.js left globals in the page: ${added.join(", ")}`)
}
