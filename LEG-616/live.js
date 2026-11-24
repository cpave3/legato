// LEG-616 acceptance on a scratch Legato: open views update without a reload.
const { chromium } = require(process.env.PW)
const { execFileSync, spawn } = require("child_process")
const fs = require("fs")

const BASE = "http://127.0.0.1:3987"
const TOKEN = fs.readFileSync("/tmp/lg/data/auth-token", "utf8").trim()
const SHOTS = process.env.SHOTS
const CARD = `Live card ${Date.now() % 100000}`
const cli = (...args) => execFileSync("/tmp/lg/scratch", ["/tmp/lg/legato", ...args], { cwd: "/tmp/lg", encoding: "utf8" })
const api = async (method, path, body) => {
  const res = await fetch(BASE + path, { method, headers: { Authorization: `Bearer ${TOKEN}`, "Content-Type": "application/json" }, body: body ? JSON.stringify(body) : undefined })
  const text = await res.text()
  if (!res.ok) throw new Error(`${method} ${path}: ${res.status} ${text}`)
  return text ? JSON.parse(text) : null
}
process.env.NODE_TLS_REJECT_UNAUTHORIZED = "0"
const log = (...a) => console.log(new Date().toISOString().slice(11, 23), ...a)

async function timed(what, fn) {
  const start = Date.now()
  await fn()
  log(`PASS ${what} in ${Date.now() - start} ms`)
}

;(async () => {
  const browser = await chromium.launch()
  const context = await browser.newContext({ ignoreHTTPSErrors: true, viewport: { width: 1400, height: 900 } })
  await context.addInitScript(([token, base]) => {
    localStorage.setItem("legato:token:local", token)
    localStorage.setItem("legato:token:" + base, token)
  }, [TOKEN, BASE])
  const loads = {}
  async function open(name, path, tab) {
    const page = await context.newPage()
    if (tab) await page.addInitScript((t) => localStorage.setItem("legato:workflows-tab", t), tab)
    loads[name] = 0
    page.on("load", () => { loads[name]++ })
    await page.goto(BASE + path)
    await page.waitForLoadState("networkidle")
    return page
  }

  // The trigger as its file says (on), whatever an earlier try left.
  await api("DELETE", "/api/workflows/live/triggers/task.entered_column")
  const task = await api("POST", "/api/tasks", { title: CARD, column: "Backlog" })
  const taskId = task.id
  log("task", taskId)

  const runs = await open("runs", "/workflows", "runs")
  const workflows = await open("workflows", "/workflows", "workflows")
  const review = await open("review", "/review")
  if (await runs.getByText(CARD).count()) throw new Error("the run is listed before the card moves")
  await runs.screenshot({ path: `${SHOTS}/1-runs-before.png` })

  // 1. A card move starts a triggered run in serve: the Runs list shows it.
  await api("PATCH", `/api/tasks/${taskId}`, { column: "Doing" })
  await timed("triggered run appears on the open Runs list", () => runs.getByText(CARD).first().waitFor({ timeout: 3000 }))
  await runs.screenshot({ path: `${SHOTS}/2-runs-triggered.png` })
  const list = await api("GET", "/api/workflow-runs")
  const run = list.find((r) => r.related_task_id === taskId)
  log("run", run.id, run.status, run.current_step_id)

  // 2. The run page follows the run through its steps.
  const runPage = await open("run", `/workflows/runs/${run.id}`)
  await runPage.screenshot({ path: `${SHOTS}/3-run-at-gate.png` })
  const before = await api("GET", `/api/workflow-runs/${run.id}`)
  log("before trigger:", before.run.current_step_id, before.steps.map((s) => `${s.step_id}=${s.status}`).join(" "))
  cli("workflow", "trigger", run.id, "gate")
  await timed("run page shows the step done and the next gate waiting", async () => {
    // Before: "Approve waits for you" and "Step 2 of 3".
    await runPage.getByRole("heading", { name: "Second gate waits for you" }).waitFor({ timeout: 3000 })
    await runPage.getByText(/Step 3 of 3/).first().waitFor({ timeout: 1000 })
  })
  const after = await api("GET", `/api/workflow-runs/${run.id}`)
  log("after trigger:", after.run.current_step_id, after.steps.map((s) => `${s.step_id}=${s.status}`).join(" "))
  await runPage.screenshot({ path: `${SHOTS}/4-run-next-gate.png` })

  // 3. A trigger switched from another client: the open Workflows tab follows.
  const switchControl = workflows.getByRole("switch").first()
  const was = await switchControl.getAttribute("aria-checked")
  log("switch was", was)
  await api("PUT", "/api/workflows/live/triggers/task.entered_column", { enabled: was !== "true" })
  await timed("Workflows tab shows the switch flipped elsewhere", () =>
    workflows.waitForFunction((want) => document.querySelector('[role="switch"]')?.getAttribute("aria-checked") === want, String(was !== "true"), { timeout: 3000 }))
  await workflows.screenshot({ path: `${SHOTS}/5-workflows-switched.png` })

  // 4. Reconnect: the server goes away, the run moves on meanwhile, the
  // server comes back, and the open run page catches up.
  execFileSync("pkill", ["-f", "/tmp/lg/legato serve --port 3987"])
  await runPage.waitForTimeout(1500)
  cli("workflow", "trigger", run.id, "gate2")
  const serve = spawn("/tmp/lg/scratch", ["/tmp/lg/legato", "serve", "--port", "3987"], { cwd: "/tmp/lg", stdio: ["ignore", fs.openSync("/tmp/lg/serve2.log", "a"), fs.openSync("/tmp/lg/serve2.log", "a")], detached: true })
  serve.unref()
  await timed("run page catches up after the socket reconnects", () =>
    runPage.getByRole("heading", { name: "Second gate waits for you" }).waitFor({ state: "detached", timeout: 30000 }))
  await runPage.screenshot({ path: `${SHOTS}/6-run-after-reconnect.png` })

  log("page loads (1 = never reloaded):", JSON.stringify(loads))
  if (Object.values(loads).some((n) => n !== 1)) throw new Error("a page reloaded")
  await browser.close()
})().catch((err) => { console.error("FAIL", err); process.exit(1) })
