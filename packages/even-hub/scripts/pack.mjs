// Builds the app and packs it as dist/legato.ehpk. The app reads Legato only
// through the Legato Android app's glasses link at http://127.0.0.1:8792,
// which is the one address in app.json's network whitelist; nothing about
// any Legato, and no secret, is built in. Fails if a dev-only part of the app
// reached the bundle. Usage: node scripts/pack.mjs
import { execFileSync } from "node:child_process"
import { readdirSync, readFileSync } from "node:fs"
import { fileURLToPath } from "node:url"

const root = fileURLToPath(new URL("..", import.meta.url))
const run = (cmd, args) => execFileSync(cmd, args, { cwd: root, stdio: "inherit" })

run("pnpm", ["exec", "vite", "build"])

// The dev log relay (src/dev-log.ts) is for dev builds only.
const assets = `${root}dist/assets`
for (const file of readdirSync(assets).filter((f) => f.endsWith(".js"))) {
  if (readFileSync(`${assets}/${file}`, "utf8").includes("/__log")) {
    console.error(`dist/assets/${file} has the dev log relay in it: it must be left out of a build`)
    process.exit(1)
  }
}

const sdk = JSON.parse(readFileSync(`${root}node_modules/@evenrealities/even_hub_sdk/package.json`, "utf8")).version
run("pnpm", ["exec", "evenhub", "pack", "app.json", "dist", "-o", "dist/legato.ehpk", "--sdk-ver", sdk])
console.log("Packed dist/legato.ehpk")
