import { defineConfig } from "vite"
import { resolve } from "node:path"

export default defineConfig({
  publicDir: "public",
  build: {
    outDir: "dist",
    emptyOutDir: true,
    rollupOptions: {
      input: {
        background: resolve(import.meta.dirname, "src/background.ts"),
        github: resolve(import.meta.dirname, "src/content/github.ts"),
        jira: resolve(import.meta.dirname, "src/content/jira.ts"),
        options: resolve(import.meta.dirname, "options.html"),
      },
      output: {
        entryFileNames: "[name].js",
        chunkFileNames: "chunks/[name]-[hash].js",
        assetFileNames: "assets/[name]-[hash][extname]",
      },
    },
  },
})
