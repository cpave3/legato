import { describe, it, expect, vi, beforeEach, afterEach } from "vitest"
import { render, screen, waitFor, fireEvent, cleanup, within } from "@testing-library/react"
import { MemoryRouter, useLocation } from "react-router-dom"
import type { Approval } from "../../lib/approvals"

const mockDecide = vi.fn()
const mockDismiss = vi.fn()
const mockRefresh = vi.fn().mockResolvedValue(undefined)
let mockData: Approval[] = []

vi.mock("../../hooks/useServer", () => ({ useServer: () => ({ baseUrl: "http://legato" }) }))
let mockPasskey: unknown = undefined
const mockSignWithPasskey = vi.fn()
vi.mock("../../hooks/usePasskeyDevice", () => ({ usePasskeyDevice: () => mockPasskey }))
vi.mock("../../lib/passkeys", () => ({ signWithPasskey: (...args: unknown[]) => mockSignWithPasskey(...args) }))
vi.mock("../../hooks/useApprovals", () => ({
  useApprovals: () => ({ data: mockData, error: null, refresh: mockRefresh }),
}))
vi.mock("../../lib/approvals", async (importOriginal) => {
  const actual = await importOriginal<typeof import("../../lib/approvals")>()
  return {
    ...actual,
    decideApproval: (...args: unknown[]) => mockDecide(...args),
    dismissApproval: (...args: unknown[]) => mockDismiss(...args),
  }
})

import { ApprovalTray } from "../ApprovalTray"
import { setFocusMode } from "../../lib/focusMode"

function permission(overrides: Partial<Approval> = {}): Approval {
  return {
    id: "ap-11111111", source: "agent_permission", source_key: "ex/s/r1", task_id: "task-1",
    title: "Run a command", summary: "go test ./...",
    details: { request_id: "r1", agent_kind: "claude-code", task_title: "Fix attention sort", tool: "Bash", command: "go test ./...", reason: "run the tests" },
    options: [
      { id: "once", label: "Allow once", kind: "allow" },
      { id: "always", label: "Allow, and don't ask again", kind: "allow" },
      { id: "deny", label: "Deny", kind: "deny" },
    ],
    blocking: true, status: "pending", revision: 1, created_at: "2026-10-29T10:00:00Z",
    ...overrides,
  }
}

function question(): Approval {
  return permission({
    id: "ap-22222222", source: "agent_question", title: "Which database?", summary: "",
    details: { request_id: "q1", agent_kind: "codex", task_title: "Add search",
      questions: [{ id: "q1", question: "Which database?", options: [{ label: "SQLite" }, { label: "Postgres" }], multiSelect: false, allowOther: false }] },
    options: [],
  })
}

// Two questions from one agent, the first with the agent's recommendation.
function questionnaire(id = "ap-44444444", overrides: Partial<Approval> = {}): Approval {
  return permission({
    id, source: "agent_question", title: "Share links", summary: "",
    details: { request_id: "q2", agent_kind: "claude-code", task_title: "Preserve the shared version",
      questions: [
        { id: "conflicts", header: "Conflicts", question: "Should Save's 409 say which conflict it was?",
          options: [{ label: "Two conflict reasons (Recommended)", description: "Say which kind." }, { label: "Single 409", description: "One notice for both." }] },
        { id: "migration", header: "Migration", question: "Fresh schema or alter in place?",
          options: [{ label: "Fresh schema" }, { label: "Alter in place" }] },
      ] },
    options: [],
    ...overrides,
  })
}

function Where() {
  const location = useLocation()
  return <div data-testid="where">{location.pathname + location.search}</div>
}

function renderTray(path = "/board") {
  return render(<MemoryRouter initialEntries={[path]}><ApprovalTray /><Where /></MemoryRouter>)
}

describe("ApprovalTray", () => {
  afterEach(() => { cleanup(); setFocusMode(false) })
  beforeEach(() => {
    vi.clearAllMocks()
    mockData = []
    mockDecide.mockResolvedValue({})
    mockDismiss.mockResolvedValue({})
  })

  it("shows a credential request's scope, the agent's reason and a broader mark", async () => {
    mockData = [permission({
      id: "ap-33333333", source: "credential", title: "Use github", summary: "push on github://acme/app/refs/heads/legato/attention-*",
      details: { credential: "github", operations: ["push"], resources: ["github://acme/app/refs/heads/legato/attention-*"], reason: "Split the refactor into three PRs", broader: true, task_title: "Refactor", agent_kind: "codex" },
      options: [{ id: "session", label: "For this session", kind: "allow" }, { id: "once", label: "Once", kind: "allow" }, { id: "deny", label: "Deny", kind: "deny" }],
    })]
    renderTray()

    const tray = screen.getByRole("region", { name: "Approvals" })
    expect(within(tray).getByText("Broader scope")).toBeTruthy()
    expect(within(tray).getByText("github://acme/app/refs/heads/legato/attention-*")).toBeTruthy()
    expect(within(tray).getByText("“Split the refactor into three PRs”")).toBeTruthy()
    expect(within(tray).getByText("The agent's reason")).toBeTruthy()
    fireEvent.click(within(tray).getByRole("button", { name: "For this session" }))
    await waitFor(() => expect(mockDecide).toHaveBeenCalledWith("http://legato", "ap-33333333", { decision: "allow", option: "session" }))
  })

  it("shows nothing while no approval is pending", () => {
    const { container } = renderTray()
    expect(container.querySelector("[aria-label='Approvals']")).toBeNull()
  })

  it("shows each pending approval with who asks, what it runs and its options", () => {
    mockData = [permission()]
    renderTray()

    const tray = screen.getByRole("region", { name: "Approvals" })
    expect(within(tray).getByText("1 needs you")).toBeTruthy()
    expect(within(tray).getByText("Run a command")).toBeTruthy()
    expect(within(tray).getByText("Fix attention sort · claude-code")).toBeTruthy()
    expect(within(tray).getByText("go test ./...")).toBeTruthy()
    expect(within(tray).getByRole("button", { name: "Allow once" })).toBeTruthy()
    expect(within(tray).getByRole("button", { name: "Allow, and don't ask again" })).toBeTruthy()
    expect(within(tray).getByRole("button", { name: "Deny…" })).toBeTruthy()
  })

  it("allows with the option the person picks", async () => {
    mockData = [permission()]
    renderTray()

    fireEvent.click(screen.getByRole("button", { name: "Allow, and don't ask again" }))

    await waitFor(() => expect(mockDecide).toHaveBeenCalledWith("http://legato", "ap-11111111", { decision: "allow", option: "always" }))
    expect(mockRefresh).toHaveBeenCalled()
  })

  it("denies with a note for the agent", async () => {
    mockData = [permission()]
    renderTray()

    fireEvent.click(screen.getByRole("button", { name: "Deny…" }))
    fireEvent.change(screen.getByLabelText("Note for the agent"), { target: { value: "use make test" } })
    fireEvent.click(screen.getByRole("button", { name: "Deny" }))

    await waitFor(() => expect(mockDecide).toHaveBeenCalledWith("http://legato", "ap-11111111", { decision: "deny", feedback: "use make test" }))
  })

  it("answers a question and can dismiss one", async () => {
    mockData = [question()]
    renderTray()

    fireEvent.click(screen.getByLabelText(/Postgres/))
    fireEvent.click(screen.getByRole("button", { name: /Send/ }))
    await waitFor(() => expect(mockDecide).toHaveBeenCalledWith("http://legato", "ap-22222222", { decision: "answer", answers: { q1: ["Postgres"] } }))

    fireEvent.click(screen.getByRole("button", { name: /Dismiss/ }))
    await waitFor(() => expect(mockDismiss).toHaveBeenCalledWith("http://legato", "ap-22222222"))
  })

  it("says why a decision failed and keeps the card", async () => {
    mockData = [permission()]
    mockDecide.mockRejectedValue(new Error("Already decided somewhere else."))
    renderTray()

    fireEvent.click(screen.getByRole("button", { name: "Allow once" }))

    expect(await screen.findByText("Already decided somewhere else.")).toBeTruthy()
    expect(screen.getByText("Run a command")).toBeTruthy()
  })

  it("can be folded to a count and opened again", () => {
    mockData = [permission(), question()]
    renderTray()

    fireEvent.click(screen.getByRole("button", { name: "Hide approvals" }))
    expect(screen.queryByText("Run a command")).toBeNull()

    fireEvent.click(screen.getByRole("button", { name: "2 need you" }))
    expect(screen.getByText("Run a command")).toBeTruthy()
  })

  it("opens on the approval an address names and drops it from the address", async () => {
    mockData = [permission(), question()]
    renderTray("/board?approval=ap-22222222&x=1")

    const card = screen.getAllByText("Which database?")[0].closest("article")
    expect(card?.getAttribute("data-focused")).toBe("true")
    await waitFor(() => expect(screen.getByTestId("where").textContent).toBe("/board?x=1"))
  })

  describe("an approval that needs a signature", () => {
    function signed(): Approval {
      return permission({ id: "ap-33333333", source: "credential", title: "Use github", signed: true,
        details: { credential: "github", operations: ["git.push"], resources: ["github:cpave3/legato@refs/heads/main"], reason: "push the fix" },
        options: [{ id: "once", label: "Once", kind: "allow" }, { id: "deny", label: "Deny", kind: "deny" }] })
    }
    afterEach(() => {
      delete (window as { legatoShell?: unknown }).legatoShell
      delete (window as { ReactNativeWebView?: unknown }).ReactNativeWebView
    })

    it("says it needs the phone in a browser, and offers no allow", () => {
      mockData = [signed()]
      renderTray()
      expect(screen.getByText(/Needs your phone/)).toBeTruthy()
      expect(screen.queryByRole("button", { name: "Once" })).toBeNull()
      expect(screen.getByRole("button", { name: "Deny…" })).toBeTruthy()
    })

    it("signs with this page's passkey in a browser that has one", async () => {
      const device = { id: "dev-p", kind: "passkey", name: "Desktop", status: "active", origin: window.location.origin, created_at: "" }
      mockPasskey = device
      mockSignWithPasskey.mockResolvedValue(undefined)
      mockData = [signed()]
      mockRefresh.mockClear()
      renderTray()
      expect(screen.queryByText(/Needs your phone/)).toBeNull()
      fireEvent.click(screen.getByRole("button", { name: /Sign with passkey: Once/ }))
      await waitFor(() => expect(mockSignWithPasskey).toHaveBeenCalledWith("http://legato", "ap-33333333", device, "once"))
      await waitFor(() => expect(mockRefresh).toHaveBeenCalled())
      mockPasskey = undefined
    })

    it("says why when the passkey does not sign", async () => {
      mockPasskey = { id: "dev-p", kind: "passkey", name: "Desktop", status: "active", origin: window.location.origin, created_at: "" }
      mockSignWithPasskey.mockRejectedValue(new Error("The operation either timed out or was not allowed."))
      mockData = [signed()]
      renderTray()
      fireEvent.click(screen.getByRole("button", { name: /Sign with passkey: Once/ }))
      expect(await screen.findByRole("alert")).toBeTruthy()
      mockPasskey = undefined
    })

    it("opens the app's sheet inside the app, and refreshes when it is decided", async () => {
      const posted: string[] = []
      ;(window as { legatoShell?: unknown }).legatoShell = { approval: 1, key: "k1" }
      ;(window as { ReactNativeWebView?: unknown }).ReactNativeWebView = { postMessage: (m: string) => posted.push(m) }
      mockData = [signed()]
      mockRefresh.mockClear()
      renderTray()
      fireEvent.click(screen.getByRole("button", { name: /Approve and sign/ }))
      expect(JSON.parse(posted[0])).toEqual({ legatoShell: "approval", key: "k1", id: "ap-33333333" })
      window.dispatchEvent(new CustomEvent("legato:shell-approval", { detail: { id: "ap-33333333", status: "decided", option: "once" } }))
      await waitFor(() => expect(mockRefresh).toHaveBeenCalled())
    })
  })

  describe("focus mode", () => {
    it("opens the waiting questions one at a time", () => {
      mockData = [questionnaire()]
      renderTray()
      fireEvent.click(screen.getByRole("button", { name: "Answer in focus mode" }))

      const focus = screen.getByRole("dialog", { name: "Focus mode" })
      expect(within(focus).getByText("Question 1 of 2")).toBeTruthy()
      expect(within(focus).getByText("Should Save's 409 say which conflict it was?")).toBeTruthy()
      expect(within(focus).queryByText("Fresh schema or alter in place?")).toBeNull()
    })

    function openFocus() {
      fireEvent.click(screen.getByRole("button", { name: "Answer in focus mode" }))
      return screen.getByRole("dialog", { name: "Focus mode" })
    }
    const press = (focus: HTMLElement, key: string, extra: Record<string, boolean> = {}) => fireEvent.keyDown(focus, { key, ...extra })

    it("answers with keys, then reviews and sends one approval's answers together", async () => {
      mockData = [questionnaire()]
      renderTray()
      const focus = openFocus()

      press(focus, "2")
      expect(within(focus).getByRole("button", { name: /Single 409/ }).getAttribute("aria-pressed")).toBe("true")
      press(focus, "ArrowUp")
      expect(within(focus).getByRole("button", { name: /Two conflict reasons/ }).getAttribute("aria-pressed")).toBe("true")
      press(focus, "Enter")
      expect(within(focus).getByText("Question 2 of 2")).toBeTruthy()
      press(focus, "ArrowLeft")
      expect(within(focus).getByText("Question 1 of 2")).toBeTruthy()
      press(focus, "Enter")
      press(focus, "2")
      press(focus, "Enter")

      const review = within(focus).getByRole("region", { name: "Review your answers" })
      expect(within(review).getByText("Two conflict reasons")).toBeTruthy()
      expect(within(review).getByText("Alter in place")).toBeTruthy()
      expect(mockDecide).not.toHaveBeenCalled()
      press(focus, "Enter", { metaKey: true })
      await waitFor(() => expect(mockDecide).toHaveBeenCalledWith("http://legato", "ap-44444444", {
        decision: "answer",
        answers: { conflicts: ["Two conflict reasons (Recommended)"], migration: ["Alter in place"] },
      }))
      expect(mockRefresh).toHaveBeenCalled()
    })

    it("marks the agent's recommendation without changing the answer it sends", () => {
      mockData = [questionnaire()]
      renderTray()
      const focus = openFocus()
      const option = within(focus).getByRole("button", { name: /Two conflict reasons/ })
      expect(within(option).getByText("Two conflict reasons")).toBeTruthy()
      expect(within(option).getByText("Recommended")).toBeTruthy()
    })

    it("takes an answer in the person's own words", async () => {
      mockData = [questionnaire()]
      renderTray()
      const focus = openFocus()
      press(focus, "/")
      const own = within(focus).getByRole("textbox", { name: "Or answer in your own words" })
      expect(document.activeElement).toBe(own)
      fireEvent.change(own, { target: { value: "Ask the team" } })
      fireEvent.keyDown(own, { key: "Enter" })
      expect(within(focus).getByText("Question 2 of 2")).toBeTruthy()
      press(focus, "1")
      press(focus, "Enter")
      press(focus, "Enter")
      await waitFor(() => expect(mockDecide).toHaveBeenCalledWith("http://legato", "ap-44444444", {
        decision: "answer", answers: { conflicts: ["Ask the team"], migration: ["Fresh schema"] },
      }))
    })

    it("sends a single question from the question itself, and not before it is answered", async () => {
      mockData = [question()]
      renderTray()
      const focus = openFocus()
      expect(within(focus).queryByText(/Question 1 of/)).toBeNull()
      press(focus, "Enter")
      expect(within(focus).getByRole("alert").textContent).toBe("Choose an answer first.")
      press(focus, "2")
      press(focus, "Enter")
      await waitFor(() => expect(mockDecide).toHaveBeenCalledWith("http://legato", "ap-22222222", { decision: "answer", answers: { q1: ["Postgres"] } }))
    })

    it("leaves on Esc, back to the tray", async () => {
      mockData = [questionnaire()]
      renderTray()
      press(openFocus(), "Escape")
      await waitFor(() => expect(screen.queryByRole("dialog", { name: "Focus mode" })).toBeNull())
      expect(screen.getByRole("region", { name: "Approvals" })).toBeTruthy()
      expect(screen.queryByText(/Focus is on/)).toBeNull()
    })

    it("fades out when nothing is left to answer, and opens again when a question comes in", async () => {
      mockData = [questionnaire()]
      const view = renderTray()
      openFocus()

      mockData = []
      view.rerender(<MemoryRouter initialEntries={["/board"]}><ApprovalTray /><Where /></MemoryRouter>)
      await waitFor(() => expect(screen.queryByRole("dialog", { name: "Focus mode" })).toBeNull())
      expect(screen.getByRole("status").textContent).toContain("Focus is on. It opens again when a question comes in.")

      mockData = [questionnaire("ap-55555555")]
      view.rerender(<MemoryRouter initialEntries={["/board"]}><ApprovalTray /><Where /></MemoryRouter>)
      expect(screen.getByRole("dialog", { name: "Focus mode" })).toBeTruthy()
      expect(screen.queryByText(/Focus is on/)).toBeNull()
    })

    // Layout drops the tray while nothing is pending; focus mode outlives it.
    it("stays on while the tray is gone, and opens when the next question comes", () => {
      mockData = [questionnaire()]
      const view = renderTray()
      openFocus()
      view.unmount()

      mockData = [questionnaire("ap-55555555")]
      renderTray()
      expect(screen.getByRole("dialog", { name: "Focus mode" })).toBeTruthy()
    })

    it("says in the tray that focus is on while other approvals wait", async () => {
      mockData = [questionnaire(), permission()]
      const view = renderTray()
      openFocus()

      mockData = [permission()]
      view.rerender(<MemoryRouter initialEntries={["/board"]}><ApprovalTray /><Where /></MemoryRouter>)
      await waitFor(() => expect(screen.queryByRole("dialog", { name: "Focus mode" })).toBeNull())
      const tray = screen.getByRole("region", { name: "Approvals" })
      expect(within(tray).getByText("Focus is on")).toBeTruthy()
      expect(screen.queryByText(/It opens again when a question comes in/)).toBeNull()
      fireEvent.click(within(tray).getByRole("button", { name: "Leave focus" }))
      expect(within(tray).queryByText("Focus is on")).toBeNull()
    })

    it("offers focus mode only when an agent asks a question", () => {
      mockData = [permission()]
      renderTray()
      expect(screen.queryByRole("button", { name: "Answer in focus mode" })).toBeNull()
    })
  })
})
