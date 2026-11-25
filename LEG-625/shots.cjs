const { chromium } = require("/home/cameron/.npm/_npx/31e32ef8478fbf80/node_modules/playwright")
const out = process.argv[2]
const base = { definition_source: "/w/ship.yaml", definition_digest: "d", inputs_json: '{"task":"LEG-559"}', related_task_id: "LEG-559", related_task_title: "Ship v1 slices 1–2", created_at: "2026-11-24T09:00:00Z", started_at: "2026-11-24T09:00:00Z" }
const stepRun = (step_id, step_order, title, executor_type, status, extra = {}) => ({ run_id: "wr-1", path: step_id, step_id, step_order, title, executor_type, definition_json: "{}", resolved_inputs_json: "{}", outputs_json: "{}", status, latest_attempt: status === "pending" ? 0 : 1, started_at: "2026-11-24T09:00:00Z", ...extra })
const attempt = extra => ({ id: "wa-2", run_id: "wr-1", path: "build", step_id: "build", attempt_number: 1, executor_type: "agent.run", agent_session_id: 7, task_id: "LEG-559", agent_kind: "claude-code", working_dir: "/home/me/.local/share/legato/jobs/ex-79dcj6qq/legato", started_at: "2026-11-24T09:01:00Z", ...extra })
const suspended = {
  run: { ...base, id: "wr-1", workflow_name: "milestone-to-stack", workflow_title: "Milestone to stack · LEG-559", status: "suspended", current_step_id: "build", error: "Legato stopped while the agent was working",
    steps: [{ step_id: "context", title: "Read the milestone", status: "succeeded" }, { step_id: "build", title: "Build the stack", status: "suspended" }, { step_id: "review", title: "Review the stack", status: "pending" }] },
  steps: [stepRun("context", 0, "Read the milestone", "task.context", "succeeded"), stepRun("build", 1, "Build the stack", "agent.run", "suspended", { error: "Legato stopped while the agent was working" }), stepRun("review", 2, "Review the stack", "agent.run", "pending")],
  attempts: [attempt({ status: "suspended", resumable: true, error: "Legato stopped while the agent was working" })], artifacts: [],
}
const legacy = JSON.parse(JSON.stringify(suspended))
legacy.run.status = "failed"; legacy.run.error = "agent exited before step completion"; legacy.run.steps[1].status = "failed"
legacy.steps[1].status = "failed"; legacy.steps[1].error = "agent exited before step completion"
legacy.attempts[0].status = "failed"; legacy.attempts[0].error = "agent exited before step completion"
const otherRun = { ...base, id: "wr-2", workflow_name: "one-off-task", workflow_title: "LEG-600 one-off to an approved PR", related_task_id: "LEG-600", related_task_title: "Fix the sync", status: "running", current_step_id: "implement", created_at: "2026-11-24T08:00:00Z", steps: [{ step_id: "triage", title: "Triage the task", status: "succeeded" }, { step_id: "implement", title: "Implement", status: "waiting_agent" }] }
const interrupted = { agents: [{ task_id: "LEG-559", title: "Ship v1 slices 1–2", agent_kind: "claude-code", working_dir: "/home/me/.local/share/legato/jobs/ex-79dcj6qq/legato", resources: [], resumable: true, can_start_new: false, sandboxed: true, ephemeral: false, interrupted_at: "2026-11-24T23:10:00Z", workflow: { run_id: "wr-1", step_id: "build", run_title: "Milestone to stack · LEG-559" } }] }
;(async () => {
  const browser = await chromium.launch()
  for (const scene of (process.argv[3] ? [process.argv[3]] : ["suspended", "legacy", "runs", "agents"])) {
    const page = await browser.newPage({ viewport: { width: 1280, height: 820 }, colorScheme: "dark" })
    await page.route("**/api/**", route => {
      const url = new URL(route.request().url())
      const p = url.pathname
      const json = body => route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify(body) })
      if (p === "/api/workflow-runs/wr-1") return json(scene === "legacy" ? legacy : suspended)
      if (p === "/api/workflow-runs") return json([suspended.run, otherRun])
      if (p === "/api/agents/interrupted") return json(interrupted)
      if (p === "/api/agents") return json([])
      if (p === "/api/meta") return json({ version: "dev" })
      return json({})
    })
    await page.route("**/ws**", route => route.abort())
    const target = { suspended: "/workflows/runs/wr-1", legacy: "/workflows/runs/wr-1", runs: "/workflows?tab=runs", agents: "/agents" }[scene]
    await page.goto("http://localhost:5197" + target)
    await page.waitForTimeout(2500)
    await page.screenshot({ path: `${out}/${scene}.png` })
    await page.close()
  }
  await browser.close()
})().catch(e => { console.error(e); process.exit(1) })
