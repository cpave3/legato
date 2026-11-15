// Runs the app in the Even Hub simulator against the recorded fixture and
// saves what the glasses and the phone show to docs/screenshots/. Fails when
// the app does not start, logs an error, or draws nothing.
//
//   node scripts/screenshots.mjs             the simulator on your display
//   node scripts/screenshots.mjs --headless  under xvfb-run, with no window
//
// It walks the pages: the active tab, the idle tab, an agent's feed, one
// entry in full, and a message dictated to it (held to talk) (the fixtures transcribe any recording to one sentence).
import { spawn } from "node:child_process"
import { mkdirSync, writeFileSync } from "node:fs"
import { fileURLToPath } from "node:url"

const root = fileURLToPath(new URL("..", import.meta.url))
const out = `${root}docs/screenshots`
const headless = process.argv.includes("--headless")
const DEV_PORT = 5181
const SIM_PORT = 9898
const sim = `http://127.0.0.1:${SIM_PORT}`
const children = []

function start(cmd, args, env = {}) {
  const child = spawn(cmd, args, {
    cwd: root,
    env: { ...process.env, ...env },
    stdio: ["ignore", "pipe", "pipe"],
    detached: true,
  })
  let log = ""
  child.stdout.on("data", (d) => (log += d))
  child.stderr.on("data", (d) => (log += d))
  children.push(child)
  return { child, log: () => log }
}

function stopAll() {
  for (const child of children) {
    try {
      process.kill(-child.pid, "SIGTERM")
    } catch {
      // already gone
    }
  }
}

const sleep = (ms) => new Promise((r) => setTimeout(r, ms))

async function until(what, check, timeoutMs = 30_000) {
  const deadline = Date.now() + timeoutMs
  while (Date.now() < deadline) {
    try {
      const value = await check()
      if (value) return value
    } catch {
      // not up yet
    }
    await sleep(250)
  }
  throw new Error(`timed out waiting for ${what}`)
}

async function console_(since) {
  const res = await fetch(`${sim}/api/console${since === undefined ? "" : `?since_id=${since}`}`)
  return (await res.json()).entries
}

async function input(action) {
  const res = await fetch(`${sim}/api/input`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ action }),
  })
  if (!res.ok) throw new Error(`input ${action}: ${res.status}`)
}

async function save(kind, name) {
  const res = await fetch(`${sim}/api/screenshot/${kind}`)
  if (!res.ok) throw new Error(`screenshot ${kind}: ${res.status}`)
  const png = Buffer.from(await res.arrayBuffer())
  writeFileSync(`${out}/${name}`, png)
  return png
}

// Lit pixels of the glasses' RGBA framebuffer would need a PNG decoder; its
// compressed size is enough to tell an empty frame (a few hundred bytes)
// from a drawn one.
const drawn = (png) => png.length > 2000

try {
  mkdirSync(out, { recursive: true })

  // The dev server on the fixture, whatever .env.local says.
  start("pnpm", ["exec", "vite", "--host", "127.0.0.1", "--port", String(DEV_PORT), "--strictPort"], {
    LEGATO_URL: "",
    LEGATO_TOKEN: "",
  })
  await until("the dev server", async () => (await fetch(`http://127.0.0.1:${DEV_PORT}/legato/api/agents`)).ok)

  // Headless, there is no sound server: the null device records silence,
  // which the fixtures "transcribe" all the same.
  const simArgs = [...(headless ? ["--aid", "alsa:null"] : []), "--automation-port", String(SIM_PORT), `http://127.0.0.1:${DEV_PORT}/`]
  const simBin = `${root}node_modules/.bin/evenhub-simulator`
  if (headless) start("xvfb-run", ["-a", "-s", "-screen 0 1600x1000x24", simBin, ...simArgs])
  else start(simBin, simArgs)
  await until("the simulator", async () => (await fetch(`${sim}/api/ping`)).ok)

  await until("the app to start", async () => {
    const entries = await console_()
    return entries.find((e) => e.message.includes("[legato-even-hub] ready"))
  })
  const step = async (action, ms = 700) => {
    await input(action)
    await sleep(ms)
  }
  const shot = async (name) => {
    if (!drawn(await save("glasses", name))) throw new Error(`the glasses show nothing for ${name}`)
  }

  await sleep(500)
  await shot("agents.png")
  await save("webview", "phone.png")

  // The idle tab is the active tab's last item.
  for (let i = 0; i < 20; i++) await step("down", 150)
  await step("click")
  await shot("idle.png")
  await step("double_click")

  // The second agent's feed, one entry in full, then a message to it, held
  // to talk.
  await step("down")
  await step("click", 1200)
  await shot("agent.png")
  await step("click")
  await shot("entry.png")
  await step("double_click")
  await step("long_press", 2500)
  await shot("listening.png")
  await step("long_press_release", 1500)
  await shot("message.png")
  await step("double_click")
  await step("double_click")

  const errors = (await console_()).filter((e) => e.level === "error" || /^\[(uncaught|unhandledrejection|fetch)\]/.test(e.message))
  if (errors.length) throw new Error(`the app logged errors:\n${errors.map((e) => e.message).join("\n")}`)

  console.log(`Saved agents, idle, agent, entry, listening, message and phone screenshots to ${out}`)
} catch (err) {
  console.error(String(err))
  process.exitCode = 1
} finally {
  stopAll()
}
