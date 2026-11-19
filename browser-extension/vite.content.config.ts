import { defineConfig, type Plugin } from "vite"
import { resolve } from "node:path"

// The scripts put into pages: the content scripts, and scope-source, which
// the service worker runs in a page's MAIN world (src/scope-source/page.ts).
const entries: Record<string, string> = {
  github: "src/content/github.ts",
  tickets: "src/content/tickets.ts",
  scope: "src/content/scope.ts",
  "scope-source": "src/scope-source/page.ts",
}

const entry = process.env.CONTENT_SCRIPT ?? ""
if (!(entry in entries)) throw new Error(`CONTENT_SCRIPT must be one of ${Object.keys(entries).join(", ")}`)
const name = `Legato${entry.split("-").map((part) => part[0].toUpperCase() + part.slice(1)).join("")}`

// executeScript answers a script's last value. An IIFE library is
// `var Name = (function () { … return value })();`, whose value is
// nothing; without the `var Name =` it is the value, and the page gets no
// global. scripts/verify-build.mjs runs the bundle to check.
const completionValue: Plugin = {
  name: "completion-value",
  apply: "build",
  enforce: "post",
  generateBundle(_options, bundle) {
    for (const chunk of Object.values(bundle)) {
      if (chunk.type !== "chunk") continue
      const declaration = new RegExp(`^((?:"use strict";)?\\s*)var ${name}\\s*=\\s*`)
      if (!declaration.test(chunk.code)) throw new Error(`${chunk.fileName} does not start with var ${name} =`)
      chunk.code = chunk.code.replace(declaration, "$1")
    }
  },
}

export default defineConfig({
  publicDir: false,
  plugins: entry === "scope-source" ? [completionValue] : [],
  build: {
    outDir: "dist",
    emptyOutDir: false,
    lib: {
      entry: resolve(import.meta.dirname, entries[entry]),
      formats: ["iife"],
      name,
      fileName: () => `${entry}.js`,
    },
  },
})
