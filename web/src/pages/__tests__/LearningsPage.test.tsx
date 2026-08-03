import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react"
import { MemoryRouter, Route, Routes } from "react-router-dom"
import { LearningsPage } from "../LearningsPage"
import { LearningDetailPage } from "../LearningDetailPage"

vi.mock("../../hooks/useServer", () => ({ useServer: () => ({ baseUrl: "" }) }))

const learning = { id: "learn-1", scope: "user", kind: "preference", claim: "Prefer table tests", rationale: "They are clear", status: "candidate", confidence: 0.8, revision: 3, created_at: "2026-01-01", updated_at: "2026-01-02" }

function response(body: unknown, status = 200) {
  return Promise.resolve(new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } }))
}

describe("learning curation pages", () => {
  beforeEach(() => vi.stubGlobal("fetch", vi.fn(() => response([learning]))))
  afterEach(() => { cleanup(); vi.unstubAllGlobals() })

  it("lists claims with status, scope, kind, and detail links", async () => {
    render(<MemoryRouter><LearningsPage /></MemoryRouter>)
    expect(await screen.findByText("Prefer table tests")).toBeTruthy()
    expect(screen.getAllByText("candidate").length).toBeGreaterThan(0)
    expect(screen.getAllByText("user").length).toBeGreaterThan(0)
    expect(screen.getByText("preference")).toBeTruthy()
    expect(screen.getByRole("link", { name: /Prefer table tests/ }).getAttribute("href")).toBe("/learning/learn-1")
  })

  it("shows provenance and submits edits with the current revision", async () => {
    const view = { learning, evidence: [{ id: "ev-1", source_kind: "explicit", source_ref: "settings", content: "User requested table tests", content_digest: "sha256:abc", author: "user", created_at: "2026-01-01" }], incorporations: [{ id: "inc-1", destination: "docs", target: "README.md", action: "update", status: "verified", revision: 2, references_json: "[]", verification_result: "Still present", created_at: "2026-01-01", updated_at: "2026-01-02" }] }
    const fetchMock = vi.fn()
      .mockImplementationOnce(() => response(view))
      .mockImplementationOnce(() => response({ ...view, learning: { ...learning, claim: "Edited claim", status: "validated", revision: 4 } }))
      .mockImplementationOnce(() => response({ ...view, learning: { ...learning, claim: "Edited claim", status: "validated", revision: 4 } }))
    vi.stubGlobal("fetch", fetchMock)
    render(<MemoryRouter initialEntries={["/learning/learn-1"]}><Routes><Route path="/learning/:learningId" element={<LearningDetailPage />} /></Routes></MemoryRouter>)
    expect(await screen.findByText("User requested table tests")).toBeTruthy()
    expect(screen.getByText("Incorporation history")).toBeTruthy()
    expect(screen.getByText("README.md", { exact: false })).toBeTruthy()
    fireEvent.change(screen.getByLabelText("Claim"), { target: { value: "Edited claim" } })
    fireEvent.click(screen.getByRole("button", { name: "Save edit" }))
    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(3))
    const [, init] = fetchMock.mock.calls[1]
    expect(fetchMock.mock.calls[1][0]).toBe("/api/learnings/learn-1/edit")
    expect(JSON.parse(init.body)).toEqual({ revision: 3, claim: "Edited claim", rationale: "They are clear", scope: "user", project_key: "", category: "", tags: [] })
  })

  it("searches by category and tags and shows hybrid mode", async () => {
    const fetchMock = vi.fn((url: string) => url.includes("/search")
      ? response({ items: [learning], mode: "hybrid", semantic_status: "available" })
      : response([learning]))
    vi.stubGlobal("fetch", fetchMock)
    render(<MemoryRouter><LearningsPage /></MemoryRouter>)
    fireEvent.change(screen.getByPlaceholderText("Search claims and rationale"), { target: { value: "callbacks" } })
    fireEvent.change(screen.getByLabelText("Category filter"), { target: { value: "workflow" } })
    fireEvent.change(screen.getByLabelText("Tag filters"), { target: { value: "agents, callbacks" } })
    fireEvent.click(screen.getByRole("button", { name: "Search" }))
    await screen.findByText("hybrid search")
    const searchURL = fetchMock.mock.calls.map(call => call[0]).find(url => url.includes("/search"))
    expect(searchURL).toContain("category=workflow")
    expect(searchURL).toContain("tag=agents")
  })

  it("toggles prompt activation for a validated learning", async () => {
    const validated = { ...learning, status: "validated", revision: 4, prompt_enabled: false, prompt_decided: false }
    const enabled = { ...validated, revision: 5, prompt_enabled: true, prompt_decided: true }
    const fetchMock = vi.fn()
      .mockImplementationOnce(() => response({ learning: validated, evidence: [], incorporations: [] }))
      .mockImplementationOnce(() => response({ learning: enabled, evidence: [], incorporations: [] }))
    vi.stubGlobal("fetch", fetchMock)
    render(<MemoryRouter initialEntries={["/learning/learn-1"]}><Routes><Route path="/learning/:learningId" element={<LearningDetailPage />} /></Routes></MemoryRouter>)
    const toggle = await screen.findByRole("switch", { name: "Disabled" })
    expect(screen.queryByRole("button", { name: "Accept" })).toBeNull()
    fireEvent.click(toggle)
    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(2))
    expect(fetchMock.mock.calls[1][0]).toBe("/api/learnings/learn-1/prompt")
    expect(JSON.parse(fetchMock.mock.calls[1][1].body)).toEqual({ revision: 4, enabled: true })
    await screen.findByRole("switch", { name: "Enabled" })
  })

  it("accepts and immediately replaces decision controls with prompt activation", async () => {
    const view = { learning, evidence: [], incorporations: [] }
    const validated = { ...view, learning: { ...learning, status: "validated", revision: 4, prompt_enabled: false, prompt_decided: false } }
    const fetchMock = vi.fn()
      .mockImplementationOnce(() => response(view))
      .mockImplementationOnce(() => response(validated))
      .mockImplementationOnce(() => response(validated))
    vi.stubGlobal("fetch", fetchMock)
    render(<MemoryRouter initialEntries={["/learning/learn-1"]}><Routes><Route path="/learning/:learningId" element={<LearningDetailPage />} /></Routes></MemoryRouter>)
    fireEvent.click(await screen.findByRole("button", { name: "Accept" }))
    await screen.findByRole("switch", { name: "Disabled" })
    expect(screen.queryByRole("button", { name: "Accept" })).toBeNull()
    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(3))
    expect(JSON.parse(fetchMock.mock.calls[1][1].body)).toEqual({ revision: 3 })
  })
})
