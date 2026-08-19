import { defineConfig } from "vite"
import { resolve } from "node:path"

const entry = process.env.CONTENT_SCRIPT
if (entry !== "github" && entry !== "jira") throw new Error("CONTENT_SCRIPT must be github or jira")

export default defineConfig({
  publicDir: false,
  build: {
    outDir: "dist",
    emptyOutDir: false,
    lib: {
      entry: resolve(import.meta.dirname, `src/content/${entry}.ts`),
      formats: ["iife"],
      name: `Legato${entry[0].toUpperCase()}${entry.slice(1)}`,
      fileName: () => `${entry}.js`,
    },
  },
})
