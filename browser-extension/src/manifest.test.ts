import { readFileSync } from "node:fs"
import { expect, it } from "vitest"

const manifest = JSON.parse(readFileSync(new URL("../manifest.json", import.meta.url), "utf8")) as { permissions: string[] }

// tabs.captureVisibleTab needs activeTab or <all_urls>; an origin's access
// is not enough. activeTab is given when the toolbar button or its
// shortcut is used on the page, which is how pin mode starts.
it("may take a screenshot of the tab Scope's button was used on", () => {
  expect(manifest.permissions).toContain("activeTab")
  expect(manifest.permissions).not.toContain("<all_urls>")
})
