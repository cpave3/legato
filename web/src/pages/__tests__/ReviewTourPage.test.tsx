import { handoffPacket } from "./handoffFixture"
import { act, cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react"
import { QueryClientProvider } from "@tanstack/react-query"
import { createQueryClient } from "../../lib/query"
import { MemoryRouter, Route, Routes } from "react-router-dom"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { ReviewTourPage } from "../ReviewTourPage"

const { mockFetch, mockRefresh, renderMermaid, renderWiremark } = vi.hoisted(() => ({
  mockFetch: vi.fn(),
  mockRefresh: vi.fn(),
  renderMermaid: vi.fn(),
  renderWiremark: vi.fn(),
}))
vi.stubGlobal("fetch", mockFetch)

vi.mock("mermaid", () => ({ default: { initialize: vi.fn(), render: renderMermaid } }))
vi.mock("@wiremark/core", () => ({ render: renderWiremark }))
vi.mock("../../hooks/useServer", () => ({ useServer: () => ({ baseUrl: "" }) }))
vi.mock("../../hooks/useVoiceEnabled", () => ({ useVoiceEnabled: () => true }))
vi.mock("../../hooks/useRunningAgent", () => ({ useRunningAgent: () => ({ agent: null, loading: false }) }))
vi.mock("../../components/VoiceRecorder", () => ({
  VoiceRecorder: ({ onTranscription }: { onTranscription?: (text: string) => void }) => (
    <button type="button" aria-label="Dictate review question" onClick={() => onTranscription?.("Dictated follow-up")}>Dictate</button>
  ),
}))
vi.mock("../../hooks/useReview", () => ({
  useReviewTour: () => ({
    data: {
      tour: { id: "rt-task-1", task_id: "T-1", name: "Auth review", status: "ready", base_sha: "base", head_sha: "head", summary: "Review the authentication changes" },
      passes: [{
        pass: { id: "rp-1", number: 1, status: "ready", summary: "", guidance: "" },
        captured_plan: { plan_id: "pl-1", revision_id: "pr-1", revision: 1, title: "Auth plan", markdown: "## Token flow\n\n- Refresh safely\n\n```mermaid\ngraph TD\n  Login --> Token\n```\n\n```wireframe\nWireframe mobile\n  Button \"Sign in\" contained\n```\n\n```go\nfmt.Println(\"code\")\n```" },
        steps: [
          { id: "S-1", task_id: "T-1", kind: "chapter", commit_sha: "", title: "Refresh expired tokens", narration: "Retries once after refreshing.", risk: "medium", reviewed_at: null, orphaned_at: null },
          { id: "S-2", task_id: "T-1", kind: "commit", commit_sha: "abcdef123456", title: "Commit: auth refactor", narration: "", risk: "", reviewed_at: "2026-01-01", orphaned_at: null },
        ],
        messages: [{ id: 1, step_id: "S-1", author: "agent", kind: "answer", body: "The refresh is **single-flight**." }],
        hunk_notes: [
          { id: "N-1", task_id: "T-1", step_id: "S-1", file_path: "src/auth.ts", hunk_anchor: "missing-anchor", body: "This note lost its hunk.", created_at: "2026-01-01" },
          { id: "N-2", task_id: "T-1", step_id: "S-2", file_path: "src/other.ts", hunk_anchor: "other-anchor", body: "Other step note.", created_at: "2026-01-01" },
        ], findings: [], plan_requests: [],
      }],
      steps: [
        { id: "S-1", task_id: "T-1", kind: "chapter", commit_sha: "", title: "Refresh expired tokens", narration: "Retries once after refreshing.", risk: "medium", reviewed_at: null, orphaned_at: null },
        { id: "S-2", task_id: "T-1", kind: "commit", commit_sha: "abcdef123456", title: "Commit: auth refactor", narration: "", risk: "", reviewed_at: "2026-01-01", orphaned_at: null },
      ],
      messages: [
        { id: 1, step_id: "S-1", author: "agent", kind: "answer", body: "The refresh is **single-flight**." },
      ],
      hunk_notes: [
        { id: "N-1", task_id: "T-1", step_id: "S-1", file_path: "src/auth.ts", hunk_anchor: "missing-anchor", body: "This note lost its hunk.", created_at: "2026-01-01" },
        { id: "N-2", task_id: "T-1", step_id: "S-2", file_path: "src/other.ts", hunk_anchor: "other-anchor", body: "Other step note.", created_at: "2026-01-01" },
      ],
    },
    loading: false,
    error: null,
    refresh: mockRefresh,
  }),
}))

afterEach(cleanup)

beforeEach(() => {
  mockFetch.mockReset()
  mockRefresh.mockReset()
  renderMermaid.mockReset()
  renderWiremark.mockReset()
  renderMermaid.mockResolvedValue({ svg: "<svg><text>Captured plan diagram</text></svg>" })
  renderWiremark.mockReturnValue({ svg: "<svg><text>Captured plan mockup</text></svg>", diagnostics: [] })
  mockFetch.mockResolvedValue({ ok: true, json: async () => [] })
})

function renderPage(client = createQueryClient()) {
  return render(
    <QueryClientProvider client={client}><MemoryRouter initialEntries={["/review/rt-task-1"]}>
      <Routes>
        <Route path="/review/:tourId" element={<ReviewTourPage />} />
        <Route path="/review" element={<div>Queue destination</div>} />
      </Routes>
    </MemoryRouter></QueryClientProvider>,
  )
}

describe("ReviewTourPage", () => {
  it("renders plan visuals only in captured plan context", async () => {
    renderPage()

    fireEvent.click(screen.getByText("Plan context: Auth plan · revision 1"))
    expect(screen.getByRole("heading", { name: "Token flow" }).className).toContain("text-sm")
    expect(screen.getByRole("list")).toBeTruthy()
    expect(await screen.findByText("Captured plan diagram")).toBeTruthy()
    expect(await screen.findByText("Captured plan mockup")).toBeTruthy()
    expect(screen.getByText('fmt.Println("code")').closest("pre")?.className).toContain("overflow-x-auto")
    expect(renderMermaid.mock.calls.every(([, source]) => source === "graph TD\n  Login --> Token\n")).toBe(true)
    expect(renderWiremark).toHaveBeenCalledWith("Wireframe mobile\n  Button \"Sign in\" contained\n", { theme: "dark", interactive: true })
  })

  it("keeps captured plan visuals mounted while typing review feedback", async () => {
    renderPage()

    fireEvent.click(screen.getByText("Plan context: Auth plan · revision 1"))
    expect(await screen.findByText("Captured plan diagram")).toBeTruthy()
    expect(await screen.findByText("Captured plan mockup")).toBeTruthy()
    const diagram = screen.getByRole("img", { name: "Mermaid diagram" })
    const mockup = screen.getByRole("img", { name: "Wiremark mockup" })
    const settledMermaidRenders = renderMermaid.mock.calls.length
    const settledWiremarkRenders = renderWiremark.mock.calls.length

    fireEvent.change(screen.getByLabelText("Requested change"), { target: { value: "H" } })
    fireEvent.change(screen.getByLabelText("Requested change"), { target: { value: "Handle failure" } })
    fireEvent.change(screen.getByLabelText("Question for agent"), { target: { value: "W" } })
    fireEvent.change(screen.getByLabelText("Question for agent"), { target: { value: "Why?" } })
    await new Promise((resolve) => setTimeout(resolve, 0))

    expect(renderMermaid).toHaveBeenCalledTimes(settledMermaidRenders)
    expect(renderWiremark).toHaveBeenCalledTimes(settledWiremarkRenders)
    expect(screen.getByRole("img", { name: "Mermaid diagram" })).toBe(diagram)
    expect(screen.getByRole("img", { name: "Wiremark mockup" })).toBe(mockup)
  })

  it("synchronizes Git only through the explicit action", async () => {
    renderPage()

    fireEvent.click(screen.getByRole("button", { name: "Sync changes" }))

    await waitFor(() => expect(mockFetch).toHaveBeenCalledWith(
      "/api/review/tours/rt-task-1/sync",
      expect.objectContaining({ method: "POST" }),
    ))
    expect(mockRefresh).toHaveBeenCalledTimes(1)
  })

  it("lets a reviewer inspect, mark, question, and complete a tour", async () => {
    renderPage()

    expect(screen.getByRole("heading", { name: "Auth review" })).toBeTruthy()
    expect(screen.getByText("0/1 reviewed")).toBeTruthy()
    await waitFor(() => expect(mockFetch).toHaveBeenCalledWith("/api/review/tours/rt-task-1/steps/S-1/diff", expect.anything()))

    mockFetch.mockResolvedValueOnce({ ok: true, json: async () => ({ status: "ok" }) })
    fireEvent.click(screen.getByRole("button", { name: "Mark step reviewed" }))
    await waitFor(() => expect(mockFetch).toHaveBeenCalledWith(
      "/api/review/tours/rt-task-1/steps/S-1/reviewed",
      expect.objectContaining({ method: "POST", body: JSON.stringify({ reviewed: true }) }),
    ))

    fireEvent.change(screen.getByLabelText("Question for agent"), { target: { value: "What prevents duplicate refreshes?" } })
    fireEvent.click(screen.getByRole("button", { name: "Ask agent" }))
    await waitFor(() => expect(mockFetch).toHaveBeenCalledWith(
      "/api/review/tours/rt-task-1/steps/S-1/question",
      expect.objectContaining({ body: JSON.stringify({ text: "What prevents duplicate refreshes?" }) }),
    ))
    const success = await screen.findByRole("status")
    expect(success.textContent).toContain("Question sent")
    expect(success.className).toContain("emerald")

    fireEvent.click(screen.getByRole("button", { name: "Complete review" }))
    await waitFor(() => expect(screen.getByText("Queue destination")).toBeTruthy())
    expect(mockFetch).toHaveBeenCalledWith("/api/review/tours/rt-task-1/complete", expect.objectContaining({ method: "POST" }))
  })

  it("announces API delivery warnings distinctly from successful questions", async () => {
    renderPage()
    await waitFor(() => expect(mockFetch).toHaveBeenCalledTimes(1))
    mockFetch.mockResolvedValueOnce({ ok: true, json: async () => ({ warning: "Agent delivery is delayed" }) })

    fireEvent.change(screen.getByLabelText("Question for agent"), { target: { value: "Will this arrive?" } })
    fireEvent.click(screen.getByRole("button", { name: "Ask agent" }))

    const warning = await screen.findByRole("status")
    expect(warning.textContent).toContain("Agent delivery is delayed")
    expect(warning.className).toContain("amber")
  })

  it("sends selected diff lines with a question and clears the selection", async () => {
    mockFetch.mockResolvedValueOnce({
      ok: true,
      json: async () => [{
        old_path: "src/auth.ts", new_path: "src/auth.ts", status: "modified",
        hunks: [{ anchor: "auth-hunk", header: "@@ -7,2 +7,2 @@", lines: [
          { kind: "del", old_no: 7, new_no: 0, text: "return oldToken" },
          { kind: "add", old_no: 0, new_no: 7, text: "return refreshedToken" },
        ] }],
      }],
    })
    renderPage()
    await screen.findByRole("button", { name: "Select old line 7" })

    fireEvent.click(screen.getByRole("button", { name: "Select old line 7" }))
    fireEvent.click(screen.getByRole("button", { name: "Select new line 7" }))
    expect(screen.getByText("src/auth.ts · 2 selected lines")).toBeTruthy()

    mockFetch.mockResolvedValueOnce({ ok: true, json: async () => ({ status: "ok" }) })
    fireEvent.change(screen.getByLabelText("Question for agent"), { target: { value: "Is this refresh safe?" } })
    fireEvent.click(screen.getByRole("button", { name: "Ask agent" }))

    await waitFor(() => expect(mockFetch).toHaveBeenCalledWith(
      "/api/review/tours/rt-task-1/steps/S-1/question",
      expect.objectContaining({ body: JSON.stringify({
        text: "Is this refresh safe?",
        selection: { file_path: "src/auth.ts", hunk_anchor: "auth-hunk", start: 0, end: 1 },
      }) }),
    ))
    await waitFor(() => expect(screen.queryByText("src/auth.ts · 2 selected lines")).toBeNull())
  })

  it("requires guidance before regenerating and submits it", async () => {
    renderPage()

    fireEvent.click(screen.getByRole("button", { name: "Regenerate review" }))
    const dialog = screen.getByRole("dialog", { name: "Regenerate review" })
    expect(within(dialog).getByRole("button", { name: "Regenerate" }).hasAttribute("disabled")).toBe(true)

    fireEvent.change(within(dialog).getByLabelText("Regeneration feedback"), { target: { value: "Focus on failure paths" } })
    fireEvent.click(within(dialog).getByRole("button", { name: "Regenerate" }))

    await waitFor(() => expect(mockFetch).toHaveBeenCalledWith(
      "/api/review/tours/rt-task-1/regenerate",
      expect.objectContaining({ method: "POST", body: JSON.stringify({ feedback: "Focus on failure paths" }) }),
    ))
  })

  it("flags a requested change through the active review step", async () => {
    renderPage()
    mockFetch.mockResolvedValueOnce({ ok: true, json: async () => ({ id: "F-1", status: "open" }) })

    fireEvent.change(screen.getByLabelText("Requested change"), { target: { value: "Handle refresh failure" } })
    fireEvent.click(screen.getByRole("button", { name: "Flag change" }))

    await waitFor(() => expect(mockFetch).toHaveBeenCalledWith(
      "/api/review/tours/rt-task-1/findings",
      expect.objectContaining({ method: "POST", body: JSON.stringify({ body: "Handle refresh failure", selection: { step_id: "S-1" } }) }),
    ))
  })

  it("asks for confirmation before deleting a review", async () => {
    renderPage()
    await waitFor(() => expect(mockFetch).toHaveBeenCalledTimes(1))

    fireEvent.click(screen.getByRole("button", { name: "Delete review" }))

    expect(screen.getByRole("dialog", { name: "Delete review" })).toBeTruthy()
    expect(mockFetch.mock.calls.some(([, options]) => options?.method === "DELETE")).toBe(false)
  })

  it("cancels review deletion without sending a request", async () => {
    renderPage()
    await waitFor(() => expect(mockFetch).toHaveBeenCalledTimes(1))

    fireEvent.click(screen.getByRole("button", { name: "Delete review" }))
    fireEvent.click(screen.getByRole("button", { name: "Cancel" }))

    expect(screen.queryByRole("dialog", { name: "Delete review" })).toBeNull()
    expect(mockFetch.mock.calls.some(([, options]) => options?.method === "DELETE")).toBe(false)
  })

  it("deletes a confirmed review and returns to the queue", async () => {
    renderPage()
    await waitFor(() => expect(mockFetch).toHaveBeenCalledTimes(1))
    mockFetch.mockResolvedValueOnce({ ok: true })

    fireEvent.click(screen.getByRole("button", { name: "Delete review" }))
    fireEvent.click(within(screen.getByRole("dialog", { name: "Delete review" })).getByRole("button", { name: "Delete review" }))

    await waitFor(() => expect(screen.getByText("Queue destination")).toBeTruthy())
    expect(mockFetch).toHaveBeenCalledWith("/api/review/tours/rt-task-1", expect.objectContaining({ method: "DELETE" }))
  })

  it("shows an error and stays on the tour when deletion fails", async () => {
    renderPage()
    await waitFor(() => expect(mockFetch).toHaveBeenCalledTimes(1))
    mockFetch.mockResolvedValueOnce({ ok: false, statusText: "Conflict", text: async () => "review is still capturing" })

    fireEvent.click(screen.getByRole("button", { name: "Delete review" }))
    fireEvent.click(within(screen.getByRole("dialog", { name: "Delete review" })).getByRole("button", { name: "Delete review" }))

    expect((await screen.findByText("review is still capturing")).closest('[role="alert"]')).toBeTruthy()
    expect(screen.getByRole("heading", { name: "Auth review" })).toBeTruthy()
  })

  it("shows unmatched hunk notes for only the selected step outside Q&A", async () => {
    renderPage()

    const warning = await screen.findByRole("alert", { name: "Unmatched hunk notes" })
    expect(warning.textContent).toContain("This note lost its hunk.")
    expect(screen.queryByText("Other step note.")).toBeNull()
    const emphasizedAnswer = screen.getByText("single-flight")
    expect(emphasizedAnswer.tagName).toBe("STRONG")
    expect(emphasizedAnswer.closest("aside")?.textContent).toContain("Questions & answers")
  })

  it("uses a multiline question draft and appends configured voice transcription", async () => {
    renderPage()

    const composer = screen.getByRole("textbox", { name: "Question for agent" })
    expect(composer.tagName).toBe("TEXTAREA")
    fireEvent.change(composer, { target: { value: "First line\nSecond line" } })
    fireEvent.click(await screen.findByRole("button", { name: "Dictate review question" }))
    expect((composer as HTMLTextAreaElement).value).toBe("First line\nSecond line\nDictated follow-up")
  })

  it("lets mobile header actions wrap without forcing page overflow", () => {
    const { container } = renderPage()
    const actions = container.querySelector("[data-review-actions]")
    expect(actions?.className).toContain("flex-wrap")
    expect(actions?.className).toContain("w-full")
  })

  it("starts with a wider Q&A pane and lets the reviewer resize it", () => {
    const { container } = renderPage()

    const layout = container.querySelector("[data-review-layout]") as HTMLElement
    const resizeHandle = screen.getByRole("separator", { name: "Resize questions and answers" })
    expect(layout.style.getPropertyValue("--qa-width")).toBe("28rem")
    expect(resizeHandle.getAttribute("aria-valuenow")).toBe("448")

    fireEvent.keyDown(resizeHandle, { key: "ArrowRight" })
    expect(layout.style.getPropertyValue("--qa-width")).toBe("30rem")
    expect(resizeHandle.getAttribute("aria-valuenow")).toBe("480")
  })

  it("keeps the diff pane as the shared vertical scroll container for sticky headers", () => {
    renderPage()

    const button = screen.getByRole("button", { name: "Mark step reviewed" })
    const pane = button.closest("main")
    expect(button.closest("[data-sticky-review-action]")?.className).toContain("sticky")
    expect(pane?.classList.contains("min-w-0")).toBe(true)
    expect(pane?.classList.contains("overflow-y-auto")).toBe(true)
    expect(pane?.classList.contains("overflow-x-hidden")).toBe(true)
  })

  it("loads the selected step's diff", async () => {
    renderPage()
    await waitFor(() => expect(mockFetch).toHaveBeenCalledTimes(1))
    // Expand the collapsed commits section to access the commit step
    const commitsSummary = screen.getByText(/Commits \(1\)/)
    fireEvent.click(commitsSummary)
    fireEvent.click(screen.getByRole("button", { name: /Commit: auth refactor/ }))
    await waitFor(() => expect(mockFetch).toHaveBeenCalledWith("/api/review/tours/rt-task-1/steps/S-2/diff", expect.anything()))
  })
})

it("loads handoff only after opening the view and keeps diff review available", async () => {
  mockFetch.mockImplementation(async (url: string) => ({ ok: true, json: async () => url.endsWith("/handoff") ? { tour_id: "rt-task-1", checked_at: "", packets: [] } : [] }))
  renderPage()
  expect(mockFetch.mock.calls.some(([url]) => url.endsWith("/handoff"))).toBe(false)
  fireEvent.click(screen.getByRole("button", { name: "Handoff" }))
  expect(await screen.findByText("No handoff packet")).toBeTruthy()
  expect(screen.getByText(/legato review handoff submit/)).toBeTruthy()
  fireEvent.click(screen.getByRole("button", { name: "Diff review" }))
  expect(screen.getByRole("button", { name: "Step 1: Refresh expired tokens" })).toBeTruthy()
})

it("reads pinned concepts, routes, evidence gaps and chapter links from Handoff", async () => {
  const packet = handoffPacket()
  mockFetch.mockImplementation(async (url: string) => ({ ok: true, json: async () => url.endsWith("/handoff") ? { tour_id: "rt-task-1", checked_at: "2026-09-02", packets: [packet] } : [] }))
  renderPage()
  fireEvent.click(screen.getByRole("button", { name: "Handoff" }))
  expect(await screen.findByText("Questions persist")).toBeTruthy()
  expect(screen.getByText(/Last checked: 2026-09-02/)).toBeTruthy()
  expect(screen.getByText(/Outdated check/)).toBeTruthy()
  expect(screen.getByText("No evidence attached")).toBeTruthy()
  expect(screen.getByText(/Agent-reported check/)).toBeTruthy()
  fireEvent.change(screen.getByLabelText("Reading route"), { target: { value: "maintainer" } })
  expect(screen.getAllByRole("article")[0].textContent).toContain("Delivery operations")
  expect(screen.getByText("Duplicate delivery")).toBeTruthy()
  expect(screen.getByText("other.go")).toBeTruthy()
  fireEvent.click(screen.getByText("Pinned source: queue.go:2–3"))
  expect(screen.getByText('<script>alert("source")</script>')).toBeTruthy()
  expect(document.querySelector("script")).toBeNull()
  fireEvent.click(screen.getByRole("button", { name: "Open chapter: Refresh expired tokens" }))
  expect(screen.getByRole("button", { name: "Step 1: Refresh expired tokens" })).toBeTruthy()
})

it("challenges exact packet/concept context using existing questions and safe historical findings", async () => {
  const packet = handoffPacket()
  const old = { ...handoffPacket(), id: "hp-old", revision: 0, outdated: true, outdated_reason: "Pass replaced" }
  mockFetch.mockImplementation(async (url: string) => ({ ok: true, json: async () => url.endsWith("/handoff") ? { tour_id: "rt-task-1", checked_at: "", packets: [packet, old] } : url.endsWith("/question") ? { warning: "Agent offline; question saved" } : [] }))
  renderPage()
  fireEvent.click(screen.getByRole("button", { name: "Handoff" }))
  await screen.findByText("Questions persist")
  fireEvent.change(screen.getByLabelText("Challenge Durable questions"), { target: { value: "What about retries?" } })
  fireEvent.click(screen.getAllByRole("button", { name: "Ask question" })[0])
  expect(await screen.findByText("Agent offline; question saved")).toBeTruthy()
  const question = mockFetch.mock.calls.find(([url]) => url.endsWith("/question"))!
  expect(question[0]).toContain("/steps/S-1/question")
  expect(JSON.parse(question[1].body).text).toContain("packet hp-1 · revision 1 · pass rp-1 · base base · head head · concept durability")
  fireEvent.change(screen.getByLabelText("Revision history"), { target: { value: "hp-old" } })
  expect(screen.getAllByRole("button", { name: "Ask question" })[0]).toHaveProperty("disabled", true)
  fireEvent.change(screen.getByLabelText("Challenge Durable questions"), { target: { value: "Historical gap" } })
  fireEvent.click(screen.getAllByRole("button", { name: "Flag change" })[0])
  await screen.findByText("Finding saved on the current pass with historical packet context.")
  const finding = mockFetch.mock.calls.find(([url]) => url.endsWith("/findings"))!
  expect(JSON.parse(finding[1].body).selection.step_id).toBe("")
  expect(JSON.parse(finding[1].body).body).toContain("packet hp-old")
})

it("saves optional practice and local self-assessment with CAS, reveal and skip without completing review", async () => {
  const packet = handoffPacket()
  let conflict = false
  mockFetch.mockImplementation(async (url: string, init?: RequestInit) => {
    if (url.endsWith("/note")) return conflict ? { ok: false, status: 409, text: async () => "note changed" } : { ok: true, json: async () => ({ ...JSON.parse(init!.body as string), revision: 1 }) }
    return { ok: true, json: async () => url.endsWith("/handoff") ? { tour_id: "rt-task-1", checked_at: "", packets: [packet] } : [] }
  })
  renderPage()
  fireEvent.click(screen.getByRole("button", { name: "Handoff" }))
  await screen.findByText("Questions persist")
  expect(screen.queryByText("The question stays queued.")).toBeNull()
  fireEvent.click(screen.getByRole("button", { name: "Reveal explanation" }))
  expect(screen.getByText("The question stays queued.")).toBeTruthy()
  fireEvent.change(screen.getByLabelText("Your answer: What happens offline?"), { target: { value: "It stays queued" } })
  fireEvent.change(screen.getByLabelText("Local-instance self-assessment"), { target: { value: "need_help" } })
  fireEvent.change(screen.getByLabelText("Explain it back / human note"), { target: { value: "Need to inspect retry limits" } })
  fireEvent.click(screen.getByRole("button", { name: "Save my note" }))
  await screen.findByText("Note saved. This does not complete or approve the review.")
  const saved = JSON.parse(mockFetch.mock.calls.find(([url]) => url.endsWith("/note"))![1].body)
  expect(saved).toEqual({ revision: 0, readiness: "need_help", body: "Need to inspect retry limits", answers: { offline: "It stays queued" }, skipped: [] })
  fireEvent.click(screen.getByRole("button", { name: "Skip scenario" }))
  await waitFor(() => expect(mockFetch.mock.calls.filter(([url]) => url.endsWith("/note"))).toHaveLength(2))
  const skipped = JSON.parse(mockFetch.mock.calls.filter(([url]) => url.endsWith("/note"))[1][1].body)
  expect(skipped.revision).toBe(1)
  expect(skipped.answers).toEqual({})
  expect(skipped.skipped).toEqual(["offline"])
  await waitFor(() => expect(screen.getByRole("button", { name: "Save my note" })).toHaveProperty("disabled", false))
  conflict = true
  fireEvent.click(screen.getByRole("button", { name: "Save my note" }))
  expect(await screen.findByText(/Save conflict/)).toBeTruthy()
  expect(mockFetch.mock.calls.some(([url]) => url.endsWith("/complete") || url.endsWith("/reviewed"))).toBe(false)
})

it("previews explicit export selection before copy/download and invalidates previews on selection changes", async () => {
  const packet = handoffPacket()
  const copy = vi.fn().mockResolvedValue(undefined)
  Object.defineProperty(navigator, "clipboard", { configurable: true, value: { writeText: copy } })
  mockFetch.mockImplementation(async (url: string) => ({ ok: true, json: async () => url.endsWith("/handoff") ? { tour_id: "rt-task-1", checked_at: "", packets: [packet] } : url.endsWith("/export") ? { markdown: "# AI-authored snapshot\nhead head", filename: "brief.md", warnings: ["PR linkage unverified"] } : [] }))
  renderPage()
  fireEvent.click(screen.getByRole("button", { name: "Handoff" }))
  await screen.findByText("Questions persist")
  expect(screen.queryByRole("button", { name: "Copy Markdown" })).toBeNull()
  expect(screen.getByLabelText("Include source excerpts")).toHaveProperty("checked", false)
  expect(screen.getByLabelText("Include my local notes and answers")).toHaveProperty("checked", false)
  fireEvent.click(screen.getByRole("button", { name: "Preview Markdown" }))
  await screen.findByText("PR linkage unverified")
  const input = JSON.parse(mockFetch.mock.calls.find(([url]) => url.endsWith("/export"))![1].body)
  expect(input).toEqual({ packet_id: "hp-1", format: "github", concept_ids: [], include_sources: false, include_notes: false })
  expect(copy).not.toHaveBeenCalled()
  fireEvent.click(screen.getByRole("button", { name: "Copy Markdown" }))
  await waitFor(() => expect(copy).toHaveBeenCalledWith("# AI-authored snapshot\nhead head"))
  const createURL = vi.fn().mockReturnValue("blob:brief")
  const revokeURL = vi.fn()
  vi.stubGlobal("URL", Object.assign(URL, { createObjectURL: createURL, revokeObjectURL: revokeURL }))
  const click = vi.spyOn(HTMLAnchorElement.prototype, "click").mockImplementation(() => {})
  fireEvent.click(screen.getByRole("button", { name: "Download Markdown" }))
  expect(createURL).toHaveBeenCalledWith(expect.any(Blob))
  expect(click).toHaveBeenCalled()
  click.mockRestore()
  fireEvent.change(screen.getByLabelText("Export format"), { target: { value: "maintenance" } })
  expect(screen.queryByRole("button", { name: "Copy Markdown" })).toBeNull()
  fireEvent.click(screen.getByLabelText("Choose concepts"))
  fireEvent.click(screen.getByLabelText("Export concept: Delivery operations"))
  fireEvent.click(screen.getByLabelText("Include source excerpts"))
  fireEvent.click(screen.getByRole("button", { name: "Preview Markdown" }))
  await screen.findByRole("button", { name: "Copy Markdown" })
  const selected = JSON.parse(mockFetch.mock.calls.filter(([url]) => url.endsWith("/export"))[1][1].body)
  expect(selected).toMatchObject({ format: "maintenance", concept_ids: ["durability"], include_sources: true, include_notes: false })
})

it("shows loading and retry errors, then accepts absent optional Go slices", async () => {
  const packet = handoffPacket()
  const wire = JSON.parse(JSON.stringify(packet))
  wire.input.concepts[0].flow = null
  wire.input.concepts[0].routes = null
  wire.input.concepts[0].source_ids = null
  wire.input.concepts[0].evidence_ids = null
  wire.input.concepts[0].chapter_ids = null
  wire.input.scenarios = null
  wire.input.evidence = null
  wire.sources = null
  let settle: (value: unknown) => void = () => {}
  const pending = new Promise((resolve) => { settle = resolve })
  mockFetch.mockImplementation(async (url: string) => url.endsWith("/handoff") ? pending : { ok: true, json: async () => [] })
  renderPage()
  fireEvent.click(screen.getByRole("button", { name: "Handoff" }))
  expect(screen.getByText("Loading handoff…")).toBeTruthy()
  settle({ ok: false, status: 500, text: async () => "Handoff unavailable" })
  await screen.findByText("Handoff unavailable")
  mockFetch.mockImplementation(async (url: string) => ({ ok: true, json: async () => url.endsWith("/handoff") ? { tour_id: "rt-task-1", checked_at: "", packets: [wire] } : [] }))
  fireEvent.click(screen.getByRole("button", { name: "Retry handoff" }))
  expect(await screen.findByText("Questions persist")).toBeTruthy()
  expect(screen.getAllByText("No evidence attached")).toHaveLength(2)
})

it("retains unlinked failed checks and revision comparisons, and reports export failures", async () => {
  const packet = handoffPacket()
  packet.input.evidence.push({ id: "unlinked", description: "Integration checks", source_ids: ["missing"], command: "pnpm test integration", head_sha: "head", recorded_at: "2026-09-01", outcome: "failed", reporter: "agent", limitations: "Service unavailable" })
  packet.comparison = { previous_revision: 1, safe: false, reason: "History rewritten", changed_concepts: ["durability"], changed_files: ["queue.go"], evidence_to_recheck: ["check"] }
  mockFetch.mockImplementation(async (url: string) => url.endsWith("/export") ? { ok: false, status: 500, text: async () => "Export unavailable" } : { ok: true, json: async () => url.endsWith("/handoff") ? { tour_id: "rt-task-1", checked_at: "", packets: [packet] } : [] })
  renderPage()
  fireEvent.click(screen.getByRole("button", { name: "Handoff" }))
  await screen.findByText("Questions persist")
  expect(screen.getByText("Agent-reported check: Integration checks")).toBeTruthy()
  expect(screen.getByText("Source unavailable: missing")).toBeTruthy()
  expect(screen.getByText("Full reread required: History rewritten")).toBeTruthy()
  fireEvent.click(screen.getByRole("button", { name: "Preview Markdown" }))
  expect(await screen.findByText("Export unavailable")).toBeTruthy()
  expect(screen.queryByRole("button", { name: "Download Markdown" })).toBeNull()
})

it("does not attach a packet with a different revision to a reused current chapter ID", async () => {
  const packet = handoffPacket()
  packet.input.head_sha = "historical-head"
  mockFetch.mockImplementation(async (url: string) => ({ ok: true, json: async () => url.endsWith("/handoff") ? { tour_id: "rt-task-1", checked_at: "", packets: [packet] } : [] }))
  renderPage()
  fireEvent.click(screen.getByRole("button", { name: "Handoff" }))
  await screen.findByText("Questions persist")
  expect(screen.queryByRole("button", { name: "Open chapter: Refresh expired tokens" })).toBeNull()
  fireEvent.change(screen.getByLabelText("Challenge Durable questions"), { target: { value: "Historical question" } })
  expect(screen.getAllByRole("button", { name: "Ask question" })[0]).toHaveProperty("disabled", true)
})

it("preserves unsaved local notes when a background handoff refresh fails", async () => {
  const packet = handoffPacket()
  const client = createQueryClient()
  mockFetch.mockImplementation(async (url: string) => ({ ok: true, json: async () => url.endsWith("/handoff") ? { tour_id: "rt-task-1", checked_at: "2026-09-02", packets: [packet] } : [] }))
  renderPage(client)
  fireEvent.click(screen.getByRole("button", { name: "Handoff" }))
  await screen.findByText("Questions persist")
  fireEvent.change(screen.getByLabelText("Explain it back / human note"), { target: { value: "Unsaved explanation" } })
  mockFetch.mockResolvedValue({ ok: false, status: 503, text: async () => "Server unavailable" })
  await act(async () => { await client.invalidateQueries({ queryKey: ["", "handoff", "rt-task-1"] }) })
  expect(await screen.findByText(/Server unavailable/)).toBeTruthy()
  expect(screen.getByLabelText("Explain it back / human note")).toHaveProperty("value", "Unsaved explanation")
  expect(screen.getByText("Questions persist")).toBeTruthy()
})

it.each([
  { outdated: true, outdated_reason: "Head changed" },
  { outdated: false, outdated_reason: "Freshness changed" },
])("invalidates export after synced freshness changes: %j", async (freshness) => {
  const packet = handoffPacket()
  const client = createQueryClient()
  mockFetch.mockImplementation(async (url: string) => ({ ok: true, json: async () => url.endsWith("/handoff") ? { tour_id: "rt-task-1", checked_at: "", packets: [packet] } : url.endsWith("/export") ? { markdown: "Fresh brief", filename: "brief.md", warnings: [] } : [] }))
  renderPage(client)
  fireEvent.click(screen.getByRole("button", { name: "Handoff" }))
  fireEvent.click(await screen.findByRole("button", { name: "Preview Markdown" }))
  await screen.findByRole("button", { name: "Copy Markdown" })
  act(() => client.setQueryData(["", "handoff", "rt-task-1"], { tour_id: "rt-task-1", checked_at: "now", packets: [{ ...packet, ...freshness }] }))
  await waitFor(() => expect(screen.queryByRole("button", { name: "Copy Markdown" })).toBeNull())
  expect(screen.queryByRole("button", { name: "Download Markdown" })).toBeNull()
  expect(screen.queryByLabelText("Export Markdown")).toBeNull()
})

it.each(["Diff review", "Open chapter: Refresh expired tokens"])("preserves unsaved handoff drafts via %s without hidden fetches", async (exit) => {
  const packet = handoffPacket()
  const client = createQueryClient()
  mockFetch.mockImplementation(async (url: string) => ({ ok: true, json: async () => url.endsWith("/handoff") ? { tour_id: "rt-task-1", checked_at: "", packets: [packet] } : [] }))
  renderPage(client)
  fireEvent.click(screen.getByRole("button", { name: "Handoff" }))
  fireEvent.change(await screen.findByLabelText("Explain it back / human note"), { target: { value: "Unsaved human note" } })
  fireEvent.change(screen.getByLabelText("Your answer: What happens offline?"), { target: { value: "My draft answer" } })
  fireEvent.click(screen.getByRole("button", { name: exit }))
  expect(screen.queryByRole("main", { name: "Handoff" })).toBeNull()
  const requests = mockFetch.mock.calls.filter(([url]) => url.endsWith("/handoff")).length
  await act(async () => { await client.invalidateQueries({ queryKey: ["", "handoff", "rt-task-1"] }) })
  expect(mockFetch.mock.calls.filter(([url]) => url.endsWith("/handoff"))).toHaveLength(requests)
  fireEvent.click(screen.getByRole("button", { name: "Handoff" }))
  expect(await screen.findByLabelText("Explain it back / human note")).toHaveProperty("value", "Unsaved human note")
  expect(screen.getByLabelText("Your answer: What happens offline?")).toHaveProperty("value", "My draft answer")
})

it("renders handoff images as nonfetching alt text and omits preview HTML tags", async () => {
  const packet = handoffPacket()
  const image = (alt: string) => `![${alt}](https://tracker.invalid/pixel?secret=1)`
  packet.input.overview.before = image("overview tracker")
  packet.plan_markdown = image("plan tracker")
  packet.input.changes = image("changes tracker")
  packet.comparison.previous_revision = 1
  packet.input.concepts[0].claim = image("claim tracker")
  packet.input.scenarios[0].explanation = image("explanation tracker")
  packet.outdated = true
  packet.chapters[0].narration = image("chapter tracker")
  packet.sources[0].text = '<img src="https://tracker.invalid/source">'
  const markdown = `<details>\n<summary>Sources</summary>\n\n${image("preview tracker")}\n\nVisible source content\n\n</details>`
  mockFetch.mockImplementation(async (url: string) => ({ ok: true, json: async () => url.endsWith("/handoff") ? { tour_id: "rt-task-1", checked_at: "", packets: [packet] } : url.endsWith("/export") ? { markdown, filename: "brief.md", warnings: [] } : [] }))
  const { container } = renderPage()
  fireEvent.click(screen.getByRole("button", { name: "Handoff" }))
  fireEvent.click(await screen.findByRole("button", { name: "Reveal explanation" }))
  fireEvent.click(screen.getByRole("button", { name: "Preview Markdown" }))
  await screen.findByRole("button", { name: "Copy Markdown" })
  expect(container.querySelector("img")).toBeNull()
  for (const alt of ["overview", "plan", "changes", "claim", "explanation", "chapter", "preview"]) {
    expect(screen.getByText(`${alt} tracker`)).toBeTruthy()
  }
  expect(screen.getByText("Visible source content")).toBeTruthy()
  expect(screen.queryByText(/<details>|<summary>|<\/details>/, { ignore: "textarea" })).toBeNull()
  expect(mockFetch.mock.calls.some(([url]) => url.includes("tracker.invalid"))).toBe(false)
})

it("rejects an export response started before a freshness sync", async () => {
  const packet = handoffPacket()
  const client = createQueryClient()
  let finish!: (value: unknown) => void
  const pending = new Promise((resolve) => { finish = resolve })
  mockFetch.mockImplementation(async (url: string) => url.endsWith("/export") ? { ok: true, json: () => pending } : { ok: true, json: async () => url.endsWith("/handoff") ? { tour_id: "rt-task-1", checked_at: "", packets: [packet] } : [] })
  renderPage(client)
  fireEvent.click(screen.getByRole("button", { name: "Handoff" }))
  fireEvent.click(await screen.findByRole("button", { name: "Preview Markdown" }))
  await act(async () => {
    client.setQueryData(["", "handoff", "rt-task-1"], { tour_id: "rt-task-1", checked_at: "now", packets: [{ ...packet, outdated: true, outdated_reason: "Head changed" }] })
  })
  await act(async () => { finish({ markdown: "Old unlabelled brief", filename: "brief.md", warnings: [] }) })
  await waitFor(() => expect(screen.queryByRole("button", { name: "Copy Markdown" })).toBeNull())
  expect(screen.queryByRole("button", { name: "Download Markdown" })).toBeNull()
})

it("invalidates exports containing saved notes when the note revision changes", async () => {
  const packet = handoffPacket()
  const client = createQueryClient()
  mockFetch.mockImplementation(async (url: string) => ({ ok: true, json: async () => url.endsWith("/handoff") ? { tour_id: "rt-task-1", checked_at: "", packets: [packet] } : url.endsWith("/export") ? { markdown: "Brief with old notes", filename: "brief.md", warnings: [] } : [] }))
  renderPage(client)
  fireEvent.click(screen.getByRole("button", { name: "Handoff" }))
  fireEvent.click(await screen.findByLabelText("Include my local notes and answers"))
  fireEvent.click(screen.getByRole("button", { name: "Preview Markdown" }))
  await screen.findByRole("button", { name: "Copy Markdown" })
  await act(async () => {
    client.setQueryData(["", "handoff", "rt-task-1"], { tour_id: "rt-task-1", checked_at: "now", packets: [{ ...packet, note: { ...packet.note, revision: 1, body: "New saved note" } }] })
  })
  await waitFor(() => expect(screen.queryByRole("button", { name: "Copy Markdown" })).toBeNull())
  expect(screen.queryByRole("button", { name: "Download Markdown" })).toBeNull()
})

it("keeps the selected revision and unsaved note when a newer packet arrives", async () => {
  const packet = handoffPacket()
  const client = createQueryClient()
  mockFetch.mockImplementation(async (url: string) => ({ ok: true, json: async () => url.endsWith("/handoff") ? { tour_id: "rt-task-1", checked_at: "", packets: [packet] } : [] }))
  renderPage(client)
  fireEvent.click(screen.getByRole("button", { name: "Handoff" }))
  await screen.findByText("Questions persist")
  fireEvent.change(screen.getByLabelText("Explain it back / human note"), { target: { value: "Still investigating this revision" } })
  const newer = { ...packet, id: "hp-newer", revision: packet.revision + 1 }
  await act(async () => { client.setQueryData(["", "handoff", "rt-task-1"], { tour_id: "rt-task-1", checked_at: "", packets: [newer, packet] }) })
  await screen.findByRole("option", { name: /Revision 2/ })
  expect(screen.getByLabelText("Revision history")).toHaveProperty("value", packet.id)
  expect(screen.getByLabelText("Explain it back / human note")).toHaveProperty("value", "Still investigating this revision")
})
