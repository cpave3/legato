// Puts a new build of the glasses app on the beta channel of the operator's
// Even developer account: the next version in app.json, packed by
// scripts/pack.mjs, uploaded with evenhub upload (or evenhub create for the
// first one) and promoted to beta, where the Even app's Even Hub tab installs
// it. Needs `evenhub login` once, which only the operator can run.
// Usage: see USAGE in beta-plan.mjs.
import { execFileSync, spawnSync } from "node:child_process"
import { readFileSync, writeFileSync } from "node:fs"
import { fileURLToPath } from "node:url"
import { USAGE, nextVersion, parseArgs } from "./beta-plan.mjs"

const root = fileURLToPath(new URL("..", import.meta.url))
const PACKAGE = "dist/legato.ehpk"
// The listing evenhub create gives a new app: at most 20 and 50 characters.
const NAME = "Legato"
const TAGLINE = "Your Legato agents and its Reeve, at a glance"

function fail(message, hint) {
  console.error(`beta: ${message}`)
  if (hint) console.error(hint)
  process.exit(1)
}

// Runs evenhub with --json: its one JSON object on stdout, its prompts and
// progress on the terminal.
function evenhub(...args) {
  const run = spawnSync("pnpm", ["exec", "evenhub", ...args, "--json"], { cwd: root, stdio: ["inherit", "pipe", "inherit"], encoding: "utf8" })
  let out = {}
  try {
    out = JSON.parse(run.stdout || "{}")
  } catch {
    out = { message: run.stdout.trim() }
  }
  return { exit: run.status ?? 1, out }
}

const said = (out) => [out.message, out.hint].filter(Boolean).join("\n")

let args
try {
  args = parseArgs(process.argv.slice(2))
} catch (err) {
  fail(err.message, USAGE)
}
if (args.help) {
  console.log(USAGE)
  process.exit(0)
}

const signedIn = evenhub("self-check")
if (signedIn.exit === 2) fail("not signed in to your Even developer account.", "Run: ! pnpm -C packages/even-hub exec evenhub login")
if (signedIn.exit !== 0) fail(`evenhub self-check failed (exit ${signedIn.exit})`, said(signedIn.out))

const manifestPath = `${root}app.json`
const manifestText = readFileSync(manifestPath, "utf8")
const manifest = JSON.parse(manifestText)
// A first upload goes out as app.json has it; each later one is a new version.
const version = args.version ?? (args.create ? manifest.version : nextVersion(manifest.version))
const changelog =
  args.changelog ?? execFileSync("git", ["log", "-1", "--format=%s", "--", "."], { cwd: root, encoding: "utf8" }).trim()
if (!changelog) fail("say what changed with -m <changelog>")

const putBack = () => writeFileSync(manifestPath, manifestText)
if (version !== manifest.version) {
  writeFileSync(manifestPath, manifestText.replace(`"version": "${manifest.version}"`, `"version": "${version}"`))
}

console.log(`beta: packing Legato ${version}`)
const packed = spawnSync("node", ["scripts/pack.mjs"], { cwd: root, stdio: "inherit" })
if (packed.status !== 0) {
  putBack()
  fail("packing failed; app.json is as it was")
}

console.log(args.create ? `beta: creating ${manifest.package_id} with ${version}` : `beta: uploading ${version}: ${changelog}`)
const uploaded = args.create
  ? evenhub("create", PACKAGE, "--name", NAME, "--tagline", TAGLINE)
  : evenhub("upload", PACKAGE, "-m", changelog)
if (uploaded.exit !== 0) {
  putBack()
  const hint =
    uploaded.exit === 3
      ? `${manifest.package_id} is not on your account yet: run this again with --create.`
      : uploaded.exit === 4
        ? `${args.create ? "The app" : `Version ${version}`} is there already: pass --version with a newer one.`
        : said(uploaded.out)
  fail(`the upload failed (evenhub exit ${uploaded.exit}); app.json is as it was`, hint)
}

const promoted = evenhub("promote", version, "--to", "beta")
if (promoted.exit !== 0) {
  fail(
    `${version} is uploaded, but putting it on beta failed (evenhub exit ${promoted.exit})`,
    `${said(promoted.out)}\nTry again: pnpm -C packages/even-hub exec evenhub promote ${version} --to beta`,
  )
}

console.log(`beta: Legato ${version} is on the beta channel. Update it from the Even app's Even Hub tab.`)
if (version !== manifest.version) console.log("beta: app.json now says so; commit it.")
