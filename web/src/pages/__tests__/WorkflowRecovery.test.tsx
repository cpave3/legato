import { QueryClientProvider } from "@tanstack/react-query"
import { act, fireEvent, render, screen, waitFor } from "@testing-library/react"
import { MemoryRouter, Route, Routes } from "react-router-dom"
import { beforeEach, expect, it, vi } from "vitest"
import { createQueryClient, queryKeys } from "../../lib/query"
import { WorkflowRunPage } from "../WorkflowRunPage"
import { WorkflowsPage } from "../WorkflowsPage"

const api = vi.hoisted(() => ({ definitions: vi.fn(), runs: vi.fn(), run: vi.fn(), server: "" }))
vi.mock("../../hooks/useServer", () => ({ useServer: () => ({ baseUrl: api.server }) }))
vi.mock("../../lib/workflow", async original => ({ ...await original<typeof import("../../lib/workflow")>(), fetchWorkflows: api.definitions, fetchWorkflowRuns: api.runs, fetchWorkflowRun: api.run }))

beforeEach(() => {
  api.server = ""
  api.definitions.mockReset().mockResolvedValue({ definitions: [], errors: [] })
  api.runs.mockReset().mockResolvedValue([])
  api.run.mockReset()
  localStorage.clear()
})

function show(path: string) {
  const client = createQueryClient()
  const tree = () => <QueryClientProvider client={client}><MemoryRouter initialEntries={[path]}><Routes>
    <Route path="/workflows" element={<WorkflowsPage />} />
    <Route path="/workflows/runs/:runId" element={<WorkflowRunPage />} />
  </Routes></MemoryRouter></QueryClientProvider>
  const rendered = render(tree())
  return { client, rerender: () => rendered.rerender(tree()) }
}

it.each(["runs", "workflows"])("distinguishes an initial %s failure from empty results and retries", async tab => {
  const fetch = tab === "runs" ? api.runs : api.definitions
  fetch.mockRejectedValueOnce(new Error("Connection refused"))
  show(`/workflows?tab=${tab}`)
  expect((await screen.findByRole("alert")).textContent).toContain("Connection refused")
  expect(screen.queryByText(/No runs yet|No workflow definitions found/)).toBeNull()
  fireEvent.click(screen.getByRole("button", { name: "Retry" }))
  expect(await screen.findByText(tab === "runs" ? /No runs yet/ : "No workflow definitions found")).toBeTruthy()
  expect(screen.queryByRole("alert")).toBeNull()
})

it("keeps a successfully loaded empty runs list distinct from an initial failure", async () => {
  show("/workflows?tab=runs")
  await screen.findByText(/No runs yet/)
  api.runs.mockRejectedValueOnce(new Error("Connection refused"))
  fireEvent.click(screen.getByRole("button", { name: "Refresh workflows" }))
  expect((await screen.findByRole("alert")).textContent).toContain("Showing saved runs")
  fireEvent.click(screen.getByRole("button", { name: "Retry" }))
  await waitFor(() => expect(screen.queryByRole("alert")).toBeNull())
  expect(screen.getByText(/No runs yet/)).toBeTruthy()
})

it("recovers an initial run failure without showing another server or run's cached data", async () => {
  api.run.mockRejectedValue(new Error("Run unavailable"))
  const { client, rerender } = show("/workflows/runs/wr-1")
  expect((await screen.findByRole("alert")).textContent).toContain("Run unavailable")
  expect(screen.queryByText(/Showing saved/)).toBeNull()
  const data = { run: { id: "wr-1", workflow_title: "Recovered run", status: "succeeded", inputs_json: "{}" }, steps: [], attempts: [], artifacts: [] }
  api.run.mockResolvedValueOnce(data)
  fireEvent.click(screen.getByRole("button", { name: "Retry" }))
  expect(await screen.findByRole("heading", { name: "Recovered run" })).toBeTruthy()
  await act(async () => { client.setQueryData(queryKeys.workflowRun("other", "wr-2"), data) })
  api.server = "other"
  rerender()
  expect(screen.queryByRole("heading", { name: "Recovered run" })).toBeNull()
  expect((await screen.findByRole("alert")).textContent).toContain("Run unavailable")
  expect(screen.queryByText(/Showing saved/)).toBeNull()
  expect(api.run).toHaveBeenLastCalledWith("other", "wr-1")
})
