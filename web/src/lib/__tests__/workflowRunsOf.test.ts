import { beforeEach, describe, expect, it, vi } from "vitest"

const apiFetch = vi.hoisted(() => vi.fn())
vi.mock("../api", () => ({ apiFetch }))

import { fetchWorkflowRunsOf } from "../workflow"

describe("fetchWorkflowRunsOf", () => {
  beforeEach(() => { apiFetch.mockReset(); apiFetch.mockResolvedValue(new Response("[]")) })

  it("asks the server for one workflow's newest runs, so other workflows' runs never crowd them out", async () => {
    await fetchWorkflowRunsOf("", "triage & co", 10)
    expect(apiFetch).toHaveBeenCalledWith("", "/api/workflow-runs?workflow_name=triage+%26+co&limit=10")
  })

  it("reads an empty answer as no runs", async () => {
    apiFetch.mockResolvedValue(new Response("null"))
    expect(await fetchWorkflowRunsOf("", "triage", 10)).toEqual([])
  })
})
