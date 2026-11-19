import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react"
import { MemoryRouter } from "react-router-dom"
import { afterEach, beforeEach, expect, it, vi } from "vitest"
import { WorkflowsPage } from "../WorkflowsPage"

const { apiFetch, refresh } = vi.hoisted(() => ({ apiFetch: vi.fn(), refresh: vi.fn() }))
vi.mock("../../lib/api", () => ({ apiFetch }))
vi.mock("../../hooks/useServer", () => ({ useServer: () => ({ baseUrl: "" }) }))
vi.mock("../../hooks/useWorkflow", () => ({
  useWorkflows: () => ({ data: { definitions: null, errors: null }, refresh }),
  useWorkflowRuns: () => ({ data: null, refresh: vi.fn() }),
}))

const json = (body: unknown, status = 200) => Promise.resolve(new Response(JSON.stringify(body), { status }))
beforeEach(() => {
  apiFetch.mockReset()
  refresh.mockReset()
  let adds = 0
  apiFetch.mockImplementation((_: string, path: string, init?: RequestInit) => {
    if (path === "/api/workflow-templates") return json({
      templates: [{ name: "triage", title: "Triage a task" }, { name: "review-pr", title: "Review pull request" }],
      folders: [{ id: "global", label: "Your workflows", path: "/home/me/.config/legato/workflows" }, { id: "project", label: "This project", path: "/repo/.legato/workflows" }],
    })
    if (path === "/api/workflow-templates/triage" && init?.method === "POST") {
      adds++
      return adds === 1 ? json({ error: "a workflow of that name exists; choose another name: triage" }, 409) : json({ name: "my-triage", path: "/repo/.legato/workflows/my-triage.workflow.yaml" }, 201)
    }
    return json({}, 404)
  })
})
afterEach(cleanup)

it("adds a template from the empty state, asking for another name when it is taken", async () => {
  render(<MemoryRouter><WorkflowsPage /></MemoryRouter>)
  fireEvent.click(screen.getByRole("tab", { name: "Workflows" }))
  fireEvent.click(screen.getAllByRole("button", { name: "Add from template" })[0])
  const dialog = await screen.findByRole("dialog", { name: "Add from template" })
  await waitFor(() => expect(within(dialog).getByRole("option", { name: "Triage a task" })).toBeTruthy())
  expect((within(dialog).getByRole("textbox", { name: "Name" }) as HTMLInputElement).value).toBe("triage")
  fireEvent.click(within(dialog).getByRole("button", { name: "Add" }))
  expect(await within(dialog).findByRole("alert")).toHaveProperty("textContent", expect.stringContaining("choose another name"))
  fireEvent.change(within(dialog).getByRole("textbox", { name: "Name" }), { target: { value: "my-triage" } })
  fireEvent.change(within(dialog).getByRole("combobox", { name: "Folder" }), { target: { value: "project" } })
  fireEvent.click(within(dialog).getByRole("button", { name: "Add" }))
  expect(await within(dialog).findByText("/repo/.legato/workflows/my-triage.workflow.yaml")).toBeTruthy()
  const last = apiFetch.mock.calls.filter(([, path, init]) => path === "/api/workflow-templates/triage" && init?.method === "POST").pop()!
  expect(JSON.parse(last[2].body)).toEqual({ name: "my-triage", folder: "project" })
  expect(refresh).toHaveBeenCalled()
})
