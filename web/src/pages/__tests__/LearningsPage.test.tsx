import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react"
import { Link, MemoryRouter, Route, Routes } from "react-router-dom"
import { LearningsPage } from "../LearningsPage"
import { LearningDetailPage } from "../LearningDetailPage"

const server = vi.hoisted(() => ({ baseUrl: "" }))
vi.mock("../../hooks/useServer", () => ({ useServer: () => server }))

const learning = { id: "learn-1", scope: "user", kind: "preference", claim: "Prefer table tests", rationale: "They are clear", status: "candidate", confidence: 0.8, revision: 3, created_at: "2026-01-01", updated_at: "2026-01-02" }

function response(body: unknown, status = 200) {
  return Promise.resolve(new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } }))
}

describe("learning curation pages", () => {
  beforeEach(() => { server.baseUrl = ""; vi.stubGlobal("fetch", vi.fn(() => response([learning]))) })
  afterEach(() => { cleanup(); vi.unstubAllGlobals() })

  it("lists claims with status, scope, kind, and detail links", async () => {
    render(<MemoryRouter><LearningsPage /></MemoryRouter>)
    expect(await screen.findByText("Prefer table tests")).toBeTruthy()
    expect(screen.getAllByText("candidate").length).toBeGreaterThan(0)
    expect(screen.getAllByText("user").length).toBeGreaterThan(0)
    expect(screen.getByText("preference")).toBeTruthy()
    expect(screen.getByRole("link", { name: /Prefer table tests/ }).getAttribute("href")).toBe("/learning/learn-1")
  })

  it.each(["server", "learning"])("ignores an old load after switching %s", async identity => {
    let finish!: (value: Response) => void
    const fetchMock = vi.fn()
      .mockImplementationOnce(() => new Promise<Response>(resolve => { finish = resolve }))
      .mockImplementation(() => response({ learning: { ...learning, claim: "New server claim" }, evidence: [] }))
    vi.stubGlobal("fetch", fetchMock)
    const page = <MemoryRouter initialEntries={["/learning/learn-1"]}><Link to="/learning/learn-2">Next learning</Link><Routes><Route path="/learning/:learningId" element={<LearningDetailPage />} /></Routes></MemoryRouter>
    const { rerender } = render(page)
    if (identity === "server") {
      server.baseUrl = "https://second.example"
      rerender(<MemoryRouter initialEntries={["/learning/learn-1"]}><Link to="/learning/learn-2">Next learning</Link><Routes><Route path="/learning/:learningId" element={<LearningDetailPage />} /></Routes></MemoryRouter>)
    } else {
      fireEvent.click(screen.getByRole("link", { name: "Next learning" }))
    }
    await screen.findByDisplayValue("New server claim")
    await act(async () => { finish(await response({ learning, evidence: [] })) })
    expect((screen.getByLabelText("Claim") as HTMLTextAreaElement).value).toBe("New server claim")
  })

  it("does not follow a stale decision with another request after a server switch", async () => {
    let finish!: (value: Response) => void
    const fetchMock = vi.fn()
      .mockImplementationOnce(() => response({ learning, evidence: [] }))
      .mockImplementationOnce(() => new Promise<Response>(resolve => { finish = resolve }))
      .mockImplementation(() => response({ learning: { ...learning, claim: "New server claim" }, evidence: [] }))
    vi.stubGlobal("fetch", fetchMock)
    const { rerender } = render(<MemoryRouter initialEntries={["/learning/learn-1"]}><Routes><Route path="/learning/:learningId" element={<LearningDetailPage />} /></Routes></MemoryRouter>)
    fireEvent.click(await screen.findByRole("button", { name: "Accept" }))
    server.baseUrl = "https://second.example"
    rerender(<MemoryRouter initialEntries={["/learning/learn-1"]}><Routes><Route path="/learning/:learningId" element={<LearningDetailPage />} /></Routes></MemoryRouter>)
    await screen.findByDisplayValue("New server claim")
    await act(async () => { finish(await response({ learning: { ...learning, status: "validated" }, evidence: [] })) })
    expect(fetchMock).toHaveBeenCalledTimes(3)
    expect((screen.getByLabelText("Claim") as HTMLTextAreaElement).value).toBe("New server claim")
  })

  it.each(["decision", "follow-up", "prompt"].flatMap(stage => [200, 409].map(status => ({ stage, status }))))("ignores a late $stage response ($status) after changing learning IDs", async ({ stage, status }) => {
    let finish!: (value: Response) => void
    const view = { learning: { ...learning, status: stage === "prompt" ? "validated" : "candidate" }, evidence: [] }
    const fetchMock = vi.fn((url: string) => {
      if (url.endsWith("learn-2")) return response({ learning: { ...learning, id: "learn-2", claim: "Second learning" }, evidence: [] })
      return response(view)
    })
      .mockImplementationOnce(() => response(view))
    if (stage === "follow-up") fetchMock.mockImplementationOnce(() => response(view))
    fetchMock.mockImplementationOnce(() => new Promise<Response>(resolve => { finish = resolve }))
    vi.stubGlobal("fetch", fetchMock)
    render(<MemoryRouter initialEntries={["/learning/learn-1"]}><Link to="/learning/learn-2">Next learning</Link><Routes><Route path="/learning/:learningId" element={<LearningDetailPage />} /></Routes></MemoryRouter>)
    if (stage === "prompt") fireEvent.click(await screen.findByRole("switch"))
    else fireEvent.click(await screen.findByRole("button", { name: "Accept" }))
    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(stage === "follow-up" ? 3 : 2))
    fireEvent.click(screen.getByRole("link", { name: "Next learning" }))
    await screen.findByDisplayValue("Second learning")
    fireEvent.change(screen.getByLabelText("Claim"), { target: { value: "Second draft" } })
    await act(async () => { finish(await response(status === 200 ? view : { error: "Old request failed" }, status)) })
    expect(screen.queryByRole("alert")).toBeNull()
    expect((screen.getByLabelText("Claim") as HTMLTextAreaElement).value).toBe("Second draft")
    expect((screen.getByRole("button", { name: "Save edit" }) as HTMLButtonElement).disabled).toBe(false)
  })

  it("keeps a dirty draft on refresh and requires explicit rebase before saving", async () => {
    const latest = { ...learning, claim: "Latest server claim", revision: 4 }
    const fetchMock = vi.fn()
      .mockImplementationOnce(() => response({ learning, evidence: [] }))
      .mockImplementationOnce(() => response({ learning: latest, evidence: [] }))
      .mockImplementation(() => response({ learning: { ...latest, claim: "My draft", revision: 5 }, evidence: [] }))
    vi.stubGlobal("fetch", fetchMock)
    render(<MemoryRouter initialEntries={["/learning/learn-1"]}><Routes><Route path="/learning/:learningId" element={<LearningDetailPage />} /></Routes></MemoryRouter>)
    fireEvent.change(await screen.findByLabelText("Claim"), { target: { value: "My draft" } })
    fireEvent.click(screen.getByRole("button", { name: "Refresh" }))
    await screen.findByText("Latest server claim")
    expect((screen.getByLabelText("Claim") as HTMLTextAreaElement).value).toBe("My draft")
    expect(screen.getByRole("alert").textContent).toContain("revision 3")
    expect((screen.getByRole("button", { name: "Save edit" }) as HTMLButtonElement).disabled).toBe(true)
    fireEvent.click(screen.getByRole("button", { name: "Keep draft on revision 4" }))
    fireEvent.click(screen.getByRole("button", { name: "Save edit" }))
    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(4))
    expect(JSON.parse(fetchMock.mock.calls[2][1].body)).toMatchObject({ revision: 4, claim: "My draft" })
  })

  it("recovers from a rejected revision by refreshing latest and discarding the draft", async () => {
    const fetchMock = vi.fn()
      .mockImplementationOnce(() => response({ learning, evidence: [] }))
      .mockImplementationOnce(() => response({ error: "revision conflict" }, 409))
      .mockImplementation(() => response({ learning: { ...learning, claim: "Server edit", revision: 4 }, evidence: [] }))
    vi.stubGlobal("fetch", fetchMock)
    render(<MemoryRouter initialEntries={["/learning/learn-1"]}><Routes><Route path="/learning/:learningId" element={<LearningDetailPage />} /></Routes></MemoryRouter>)
    fireEvent.change(await screen.findByLabelText("Claim"), { target: { value: "My draft" } })
    fireEvent.click(screen.getByRole("button", { name: "Save edit" }))
    await screen.findByText(/revision conflict/)
    expect((screen.getByLabelText("Claim") as HTMLTextAreaElement).value).toBe("My draft")
    expect((screen.getByRole("button", { name: "Save edit" }) as HTMLButtonElement).disabled).toBe(true)
    fireEvent.click(screen.getByRole("button", { name: "Refresh latest" }))
    await screen.findByText("Server edit")
    fireEvent.click(screen.getByRole("button", { name: "Discard draft" }))
    expect((screen.getByLabelText("Claim") as HTMLTextAreaElement).value).toBe("Server edit")
    expect(screen.queryByRole("alert")).toBeNull()
    expect((screen.getByRole("button", { name: "Save edit" }) as HTMLButtonElement).disabled).toBe(false)
  })

  it("retries an initial load failure", async () => {
    const fetchMock = vi.fn()
      .mockImplementationOnce(() => response({ error: "Unavailable" }, 503))
      .mockImplementationOnce(() => response({ learning, evidence: [] }))
    vi.stubGlobal("fetch", fetchMock)
    render(<MemoryRouter initialEntries={["/learning/learn-1"]}><Routes><Route path="/learning/:learningId" element={<LearningDetailPage />} /></Routes></MemoryRouter>)
    await screen.findByRole("alert")
    fireEvent.click(screen.getByRole("button", { name: "Retry" }))
    await screen.findByLabelText("Claim")
    expect(screen.queryByRole("alert")).toBeNull()
  })

  it("preserves unsaved edits when accepting and locks fields while the decision is pending", async () => {
    let finish!: (value: Response) => void
    const accepted = { learning: { ...learning, status: "validated", revision: 4 }, evidence: [] }
    const fetchMock = vi.fn()
      .mockImplementationOnce(() => response({ learning, evidence: [] }))
      .mockImplementationOnce(() => new Promise<Response>(resolve => { finish = resolve }))
      .mockImplementation(() => response(accepted))
    vi.stubGlobal("fetch", fetchMock)
    render(<MemoryRouter initialEntries={["/learning/learn-1"]}><Routes><Route path="/learning/:learningId" element={<LearningDetailPage />} /></Routes></MemoryRouter>)
    fireEvent.change(await screen.findByLabelText("Claim"), { target: { value: "My draft" } })
    fireEvent.click(screen.getByRole("button", { name: "Accept" }))
    expect(screen.getByLabelText("Claim").matches(":disabled")).toBe(true)
    await act(async () => { finish(await response(accepted)) })
    await screen.findByRole("switch")
    expect((screen.getByLabelText("Claim") as HTMLTextAreaElement).value).toBe("My draft")
    expect(screen.getByRole("alert").textContent).toContain("revision 3")
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
