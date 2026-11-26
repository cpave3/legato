import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react"
import { MemoryRouter } from "react-router-dom"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { WorkflowsPage } from "../WorkflowsPage"

const { apiFetch, refresh } = vi.hoisted(() => ({ apiFetch: vi.fn(), refresh: vi.fn() }))

const trigger = (kind: string, summary: string) => ({ id: `trigger:${kind}`, kind: "trigger", trigger: { kind, summary, enabled: true } })
const manual = { id: "trigger:manual", kind: "trigger", trigger: { kind: "manual", summary: "Run by hand", enabled: true } }
const graph = (...triggers: object[]) => ({ nodes: [manual, ...triggers], edges: [] })
const step = (id: string, title: string, start?: string) => ({ id, title, type: "agent.run", start, with: {} })

const definitions = [
  { schema_version: 2, name: "prod-health-check", title: "Prod health check", tags: ["autopilot", "ops"], description: "Probes the production endpoints every 15 minutes.", source: "/home/me/.config/legato/workflows/prod-health-check.workflow.yaml", editable: true,
    on: { schedule: { every: "15m" } }, graph: graph(trigger("schedule", "Every 15 minutes")), steps: [step("probe", "Probe endpoints"), step("post", "Post to #ops-alerts")] },
  { schema_version: 2, name: "ci-health", title: "CI health", tags: ["autopilot"], source: "ci-health.workflow.yaml", editable: true, graph: graph(), steps: [step("read", "Read CI runs")] },
  { schema_version: 2, name: "ci-flake-check", title: "CI flake check", tags: ["autopilot"], called_by: ["ci-health"], source: "ci-flake-check.workflow.yaml", editable: true, graph: graph(), steps: [step("retry", "Retry job")] },
  { schema_version: 2, name: "review-pr", title: "Review PR", description: "Review code", source: "/project/.legato/workflows/review.yaml", editable: true, inputs: { repo: { type: "string", required: true } }, graph: graph(),
    steps: [step("prepare", "Prepare"), step("review", "Review"), step("cleanup", "Remove worktree", "manual")] },
  { schema_version: 2, name: "triage-task", title: "Triage a task", source: "builtin:triage-task", editable: false, read_only_reason: "built in", inputs: { task: { type: "task", required: true } },
    on: { "task.entered_column": { column: "Backlog" } }, graph: graph(trigger("task.entered_column", "Card enters Backlog")), steps: [step("read", "Read the task")] },
]
// The schedule's next run, a quarter of an hour away, as the API writes it.
const nextDue = new Date(Date.now() + 15 * 60_000).toISOString().replace(/:\d\d\.\d+Z$/, ":00Z")
const nextClock = nextDue.slice(11, 16)
const triggerStates = {
  "prod-health-check": [{ trigger: "schedule", file_enabled: true, enabled: true, overridden: false, schedule: { description: "Every 15 minutes", timezone: "Australia/Brisbane", next: [nextDue] } }],
  "triage-task": [{ trigger: "task.entered_column", file_enabled: true, enabled: false, overridden: true }],
}
const failedRun = { id: "wr-1", workflow_name: "prod-health-check", workflow_title: "Prod health check", definition_source: "x", definition_digest: "d", inputs_json: "{}", status: "failed", current_step_id: "probe", created_at: new Date(Date.now() - 4 * 60_000).toISOString() }
const day = (runs: number, failed = 0) => ({ runs, failed })
const stats = { days: ["d1", "d2", "d3", "d4", "d5", "d6", "d7"], workflows: {
  "prod-health-check": { last: failedRun, days: [day(96), day(96, 1), day(96), day(96), day(96), day(96), day(96)] },
} }

vi.mock("../../lib/api", () => ({ apiFetch }))
vi.mock("../../hooks/useServer", () => ({ useServer: () => ({ baseUrl: "" }) }))
vi.mock("../../hooks/useWorkflow", () => ({
  useWorkflows: () => ({ data: { definitions, errors: [], trigger_states: triggerStates }, refresh }),
  useWorkflowRuns: () => ({ data: [], refresh: vi.fn() }),
  useWorkflowStats: () => ({ data: stats, refresh: vi.fn() }),
}))

function open() {
  render(<MemoryRouter initialEntries={["/workflows?tab=workflows"]}><WorkflowsPage /></MemoryRouter>)
}
const group = (name: string) => screen.getByRole("region", { name })
const rowsOf = (region: HTMLElement) => within(region).queryAllByRole("listitem")
const row = (region: HTMLElement, title: string) => rowsOf(region).find(item => within(item).queryByRole("link", { name: title }))!

beforeEach(() => { try { localStorage.clear() } catch { /* no storage */ } })
afterEach(() => { cleanup(); apiFetch.mockReset(); refresh.mockReset() })

describe("the workflows list", () => {
  it("groups workflows by tag, Untagged last, listing one with two tags in both", () => {
    open()
    const regions = screen.getAllByRole("region").map(region => region.getAttribute("aria-label"))
    expect(regions).toEqual(["autopilot", "ops", "Untagged"])
    expect(row(group("autopilot"), "Prod health check")).toBeTruthy()
    expect(row(group("ops"), "Prod health check")).toBeTruthy()
    // In the autopilot group its other tag is shown beside it.
    expect(within(row(group("autopilot"), "Prod health check")).getByText("ops")).toBeTruthy()
    expect(rowsOf(group("Untagged")).map(item => within(item).getAllByRole("link")[0].textContent)).toEqual(["Review PR", "Triage a task"])
  })

  it("says in one row what starts a workflow, whether its triggers are on, its last run and its week", () => {
    open()
    const health = row(group("autopilot"), "Prod health check")
    expect(health.textContent).toContain("prod-health-check")
    expect(health.textContent).toContain("Every 15 minutes")
    expect(health.textContent).toContain(`next ${nextClock}`)
    expect(within(health).getByRole("button", { name: "Triggers of Prod health check: on" })).toBeTruthy()
    expect(within(health).getByRole("link", { name: /Failed/ }).getAttribute("href")).toBe("/workflows/runs/wr-1")
    expect(within(health).getByRole("img", { name: "672 runs in the last 7 days, 1 failed" })).toBeTruthy()
    expect(within(health).getByRole("button", { name: "Run Prod health check" })).toBeTruthy()
    // The description waits behind the row's details.
    expect(health.textContent).not.toContain("Probes the production endpoints")
    expect(within(health).getByRole("link", { name: "Prod health check" }).getAttribute("title")).toBe("Probes the production endpoints every 15 minutes.")

    const triage = row(group("Untagged"), "Triage a task")
    expect(triage.textContent).toContain("built in")
    expect(triage.textContent).toContain("Card enters Backlog")
    expect(within(triage).getByRole("button", { name: "Triggers of Triage a task: paused" })).toBeTruthy()
    expect(triage.textContent).toContain("Never run")
    const review = row(group("Untagged"), "Review PR")
    expect(review.textContent).toContain("Run by hand")
    expect(within(review).queryByRole("button", { name: /Triggers of/ })).toBeNull()
  })

  it("opens a row to its description, steps, inputs and each trigger's switch", () => {
    open()
    const review = row(group("Untagged"), "Review PR")
    fireEvent.click(within(review).getByRole("button", { name: "Details of Review PR" }))
    const details = within(group("Untagged")).getByRole("group", { name: "Review PR details" })
    expect(details.textContent).toContain("Review code")
    expect(details.textContent).toMatch(/Prepare.*Review.*Remove worktree/)
    expect(within(details).getByLabelText("waits for you")).toBeTruthy()
    expect(details.textContent).toContain("repo")
    expect(details.textContent).toContain("review.yaml")

    fireEvent.click(within(row(group("autopilot"), "Prod health check")).getByRole("button", { name: "Details of Prod health check" }))
    const health = within(group("autopilot")).getByRole("group", { name: "Prod health check details" })
    expect(within(health).getByRole("switch", { name: "Every 15 minutes" }).getAttribute("aria-checked")).toBe("true")
    expect(within(health).getByLabelText("Next runs of Prod health check").textContent).toContain(`${nextDue.slice(0, 10)} ${nextClock}`)
  })

  it("folds the workflows only other workflows run into one quiet line of their group", () => {
    open()
    const autopilot = group("autopilot")
    expect(row(autopilot, "CI flake check")).toBeUndefined()
    fireEvent.click(within(autopilot).getByRole("button", { name: /Run by other workflows/ }))
    const flake = row(autopilot, "CI flake check")
    expect(flake.textContent).toContain("Run by CI health")
  })

  it("collapses a group", () => {
    open()
    fireEvent.click(screen.getByRole("button", { name: /^autopilot/ }))
    expect(rowsOf(group("autopilot"))).toHaveLength(0)
    expect(screen.getByRole("button", { name: /^autopilot/ }).getAttribute("aria-expanded")).toBe("false")
  })

  it("filters by search, by what starts a workflow and by paused triggers, and lists them flat without groups", () => {
    open()
    fireEvent.change(screen.getByRole("searchbox", { name: "Search workflows" }), { target: { value: "triage" } })
    expect(screen.getAllByRole("region").map(region => region.getAttribute("aria-label"))).toEqual(["Untagged"])
    fireEvent.change(screen.getByRole("searchbox", { name: "Search workflows" }), { target: { value: "" } })

    fireEvent.click(within(screen.getByRole("group", { name: "Starts" })).getByRole("button", { name: "Schedule" }))
    expect(screen.getAllByRole("region").map(region => region.getAttribute("aria-label"))).toEqual(["autopilot", "ops"])
    fireEvent.click(within(screen.getByRole("group", { name: "Starts" })).getByRole("button", { name: "All" }))

    fireEvent.click(within(screen.getByRole("group", { name: "Triggers" })).getByRole("button", { name: "Paused" }))
    expect(screen.getAllByRole("listitem").map(item => within(item).getAllByRole("link")[0].textContent)).toEqual(["Triage a task"])
    fireEvent.click(within(screen.getByRole("group", { name: "Triggers" })).getByRole("button", { name: "Any" }))

    fireEvent.click(within(screen.getByRole("group", { name: "Group" })).getByRole("button", { name: "None" }))
    expect(screen.queryAllByRole("region")).toHaveLength(0)
    const titles = within(screen.getByRole("list", { name: "Workflows" })).getAllByRole("listitem").map(item => within(item).getAllByRole("link")[0].textContent)
    expect(titles).toEqual(["CI flake check", "CI health", "Prod health check", "Review PR", "Triage a task"])
  })

  it("says when nothing matches, and clears the search and filters", () => {
    open()
    fireEvent.change(screen.getByRole("searchbox", { name: "Search workflows" }), { target: { value: "deploy k8s" } })
    expect(screen.getByText("No workflows match “deploy k8s”")).toBeTruthy()
    fireEvent.click(screen.getByRole("button", { name: "Clear search and filters" }))
    expect(screen.getAllByRole("region")).toHaveLength(3)
  })

  it("writes a workflow's tags into its file from the row's menu", async () => {
    apiFetch.mockResolvedValue(new Response(JSON.stringify({ name: "review-pr", tags: ["reviews"] })))
    open()
    fireEvent.click(within(row(group("Untagged"), "Review PR")).getByRole("button", { name: "More for Review PR" }))
    fireEvent.click(screen.getByRole("menuitem", { name: "Tags…" }))
    const dialog = screen.getByRole("dialog", { name: "Tags for Review PR" })
    // The tags other workflows have are offered.
    expect(within(dialog).getByRole("checkbox", { name: "autopilot" })).toBeTruthy()
    fireEvent.change(within(dialog).getByRole("textbox", { name: "Find or add a tag" }), { target: { value: "reviews" } })
    fireEvent.click(within(dialog).getByRole("button", { name: "Add tag reviews" }))
    expect((within(dialog).getByRole("checkbox", { name: "reviews" }) as HTMLInputElement).checked).toBe(true)
    fireEvent.click(within(dialog).getByRole("button", { name: "Save" }))
    await waitFor(() => expect(apiFetch).toHaveBeenCalled())
    const [, path, init] = apiFetch.mock.calls[0]
    expect(path).toBe("/api/workflows/review-pr/tags")
    expect(init.method).toBe("PUT")
    expect(JSON.parse(init.body)).toEqual({ tags: ["reviews"] })
    await waitFor(() => expect(refresh).toHaveBeenCalled())
  })

  it("says why a built-in's tags can't be changed here", () => {
    open()
    fireEvent.click(within(row(group("Untagged"), "Triage a task")).getByRole("button", { name: "More for Triage a task" }))
    fireEvent.click(screen.getByRole("menuitem", { name: "Tags…" }))
    const dialog = screen.getByRole("dialog", { name: "Tags for Triage a task" })
    expect(dialog.textContent).toContain("built in")
    expect(within(dialog).queryByRole("button", { name: "Save" })).toBeNull()
  })
})
