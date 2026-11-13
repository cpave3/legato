// Builds the app and packs it as dist/legato.ehpk for one Legato: the
// manifest's network whitelist gets that Legato's origin, which must be known
// when the package is made. Usage: LEGATO_URL=https://… node scripts/pack.mjs
import { execFileSync } from "node:child_process"
import { mkdirSync, readFileSync, writeFileSync } from "node:fs"
import { fileURLToPath } from "node:url"

const root = fileURLToPath(new URL("..", import.meta.url))
const raw = process.env.LEGATO_URL?.trim()
if (!raw) {
  console.error("Set LEGATO_URL to the Legato the glasses will read, e.g. https://erebus.example.ts.net")
  process.exit(1)
}
const origin = new URL(raw).origin

const manifest = JSON.parse(readFileSync(`${root}app.json`, "utf8"))
for (const permission of manifest.permissions) {
  if (permission.name === "network") permission.whitelist = [origin]
}
mkdirSync(`${root}.pack`, { recursive: true })
writeFileSync(`${root}.pack/app.json`, `${JSON.stringify(manifest, null, 2)}\n`)

const sdk = JSON.parse(readFileSync(`${root}node_modules/@evenrealities/even_hub_sdk/package.json`, "utf8")).version
const run = (cmd, args) => execFileSync(cmd, args, { cwd: root, stdio: "inherit" })
run("pnpm", ["exec", "vite", "build"])
run("pnpm", ["exec", "evenhub", "pack", ".pack/app.json", "dist", "-o", "dist/legato.ehpk", "--sdk-ver", sdk])
console.log(`Packed dist/legato.ehpk for ${origin}`)
