import { afterEach, describe, expect, it, vi } from "vitest"
import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react"
import { MemoryRouter } from "react-router-dom"
import type { Approval } from "../../../lib/approvals"
import { QuestionFocus } from "../QuestionFocus"

const mockDecide = vi.fn()
const mockDismiss = vi.fn()
vi.mock("../../../hooks/useServer", () => ({ useServer: () => ({ baseUrl: "http://legato" }) }))
vi.mock("../../../lib/approvals", async (importOriginal) => ({
  ...(await importOriginal<typeof import("../../../lib/approvals")>()),
  decideApproval: (...args: unknown[]) => mockDecide(...args),
  dismissApproval: (...args: unknown[]) => mockDismiss(...args),
}))

afterEach(() => {
  cleanup()
  mockDecide.mockReset()
  mockDismiss.mockReset()
})

function approval(questions: unknown[], extra: Partial<Approval> = {}): Approval {
  return {
    id: "ap-1", source: "native_question", source_key: "k", task_id: "task-7", title: "Approval title",
    details: { agent_kind: "Claude", task_title: "Sync design", questions: questions as Approval["details"]["questions"] },
    options: [], blocking: true, status: "pending", revision: 1, created_at: "",
    ...extra,
  }
}

const pick = {
  id: "colours", header: "Palette", question: "Which colours?", multiSelect: true,
  options: [{ label: "Red", description: "Warm" }, { label: "Blue", preview: "#00f" }, { label: "Green" }],
}
const store = { id: "store", question: "Which store?", options: [{ label: "SQLite" }, { label: "Postgres (Recommended)" }] }
const closed = { id: "closed", question: "Ship it?", allowOther: false, options: [{ label: "Yes" }, { label: "No" }] }

function renderFocus(approvals: Approval[], { waiting = approvals.length, onLeave = vi.fn(), onDone = vi.fn().mockResolvedValue(undefined) } = {}) {
  render(<MemoryRouter><QuestionFocus approvals={approvals} waiting={waiting} onLeave={onLeave} onDone={onDone} /></MemoryRouter>)
  return { focus: screen.getByRole("dialog", { name: "Focus mode" }), onLeave, onDone }
}
const press = (target: HTMLElement, key: string, extra: Record<string, boolean> = {}) => fireEvent.keyDown(target, { key, ...extra })
const pressed = (focus: HTMLElement, name: RegExp) => within(focus).getByRole("button", { name }).getAttribute("aria-pressed")

describe("QuestionFocus", () => {
  it("renders nothing without an approval", () => {
    const { container } = render(<MemoryRouter><QuestionFocus approvals={[]} waiting={0} onLeave={() => {}} onDone={async () => {}} /></MemoryRouter>)
    expect(container.innerHTML).toBe("")
  })

  it("says who asks, how many wait, and links to the agent", () => {
    const { focus, onLeave } = renderFocus([approval([store]), approval([store], { id: "ap-2" })], { waiting: 3 })
    expect(within(focus).getByText("3 need you")).toBeTruthy()
    expect(within(focus).getByText("2 approvals ask")).toBeTruthy()
    expect(within(focus).getByText("Claude")).toBeTruthy()
    expect(within(focus).getByText("Sync design")).toBeTruthy()
    const link = within(focus).getByRole("link", { name: "Open agent" })
    expect(link.getAttribute("href")).toBe("/agents?task=task-7")
    fireEvent.click(link)
    expect(onLeave).toHaveBeenCalled()
  })

  it("falls back to the approval's title and a generic agent, with one waiting", () => {
    const { focus } = renderFocus([approval([store], { task_id: undefined, details: { questions: [store] as Approval["details"]["questions"] } })])
    expect(within(focus).getByText("1 needs you")).toBeTruthy()
    expect(within(focus).getByText("1 approval asks")).toBeTruthy()
    expect(within(focus).getByText("The agent")).toBeTruthy()
    expect(within(focus).getByText("Approval title")).toBeTruthy()
    expect(within(focus).queryByRole("link", { name: "Open agent" })).toBeNull()
  })

  it("toggles several choices, shows a chosen option's preview, and joins an answer in the person's words", async () => {
    const { focus } = renderFocus([approval([pick, store])])
    expect(within(focus).getByText("Palette")).toBeTruthy()
    expect(within(focus).getByText("Choose any that apply.")).toBeTruthy()
    expect(within(focus).getByText("Warm")).toBeTruthy()
    press(focus, "1")
    press(focus, "2")
    expect(pressed(focus, /Red/)).toBe("true")
    expect(pressed(focus, /Blue/)).toBe("true")
    expect(within(focus).getByText("#00f")).toBeTruthy()
    press(focus, "2")
    expect(pressed(focus, /Blue/)).toBe("false")
    expect(within(focus).queryByText("#00f")).toBeNull()
    // Arrows do not move through a question with several answers.
    press(focus, "ArrowDown")
    expect(pressed(focus, /Green/)).toBe("false")
    const own = within(focus).getByRole("textbox", { name: "Add an answer in your own words" })
    fireEvent.change(own, { target: { value: "Teal" } })
    // A typed answer keeps the choices of a question with several answers.
    expect(pressed(focus, /Red/)).toBe("true")
    press(own, "Enter")
    expect(within(focus).getByText("Question 2 of 2")).toBeTruthy()
    press(focus, "ArrowDown")
    expect(pressed(focus, /SQLite/)).toBe("true")
    press(focus, "ArrowDown")
    expect(pressed(focus, /Postgres/)).toBe("true")
    press(focus, "ArrowDown")
    expect(pressed(focus, /SQLite/)).toBe("true")
    press(focus, "Enter")
    const review = within(focus).getByRole("region", { name: "Review your answers" })
    expect(within(review).getByText("Red, Teal")).toBeTruthy()
    expect(within(review).getByText("SQLite")).toBeTruthy()
    expect(within(focus).getByRole("button", { name: /Send answers to Claude/ })).toBeTruthy()
    press(focus, "Enter")
    await waitFor(() => expect(mockDecide).toHaveBeenCalledWith("http://legato", "ap-1", { decision: "answer", answers: { colours: ["Red", "Teal"], store: ["SQLite"] } }))
  })

  it("starts arrow selection at the end going up, and a typed answer replaces a single choice", () => {
    const { focus } = renderFocus([approval([store, pick])])
    press(focus, "ArrowUp")
    expect(pressed(focus, /Postgres/)).toBe("true")
    expect(within(within(focus).getByRole("button", { name: /Postgres/ })).getByText("Recommended")).toBeTruthy()
    const own = within(focus).getByRole("textbox", { name: "Or answer in your own words" })
    fireEvent.change(own, { target: { value: "DuckDB" } })
    expect(pressed(focus, /Postgres/)).toBe("false")
    // Escape in the box gives the keys back to the dialog.
    press(own, "Escape")
    expect(document.activeElement).toBe(focus)
  })

  it("asks for a missing answer, and returns to it from review", async () => {
    const { focus } = renderFocus([approval([store, pick])])
    expect(within(focus).getByRole("button", { name: /Back/ })).toHaveProperty("disabled", true)
    press(focus, "Enter")
    press(focus, "1")
    expect(within(focus).getByRole("button", { name: "Review answers" })).toBeTruthy()
    press(focus, "Enter")
    const review = within(focus).getByRole("region", { name: "Review your answers" })
    expect(within(review).getAllByText("Not answered")).toHaveLength(1)
    press(focus, "Enter")
    await waitFor(() => expect(within(focus).getByRole("alert").textContent).toBe("This one still needs an answer."))
    expect(within(focus).getByText("Question 1 of 2")).toBeTruthy()
    expect(mockDecide).not.toHaveBeenCalled()
  })

  it("goes back from review to the last question, and changes a chosen answer from review", () => {
    const { focus } = renderFocus([approval([store, closed])])
    press(focus, "1")
    press(focus, "Enter")
    expect(within(focus).queryByRole("textbox")).toBeNull()
    press(focus, "1")
    press(focus, "Enter")
    press(focus, "ArrowLeft")
    expect(within(focus).getByText("Question 2 of 2")).toBeTruthy()
    press(focus, "Enter")
    fireEvent.click(within(focus).getAllByRole("button", { name: "Change" })[0])
    expect(within(focus).getByText("Question 1 of 2")).toBeTruthy()
  })

  it("shows why sending failed, and dismisses the approval", async () => {
    mockDecide.mockRejectedValueOnce(new Error("Already decided somewhere else."))
    const { focus, onDone } = renderFocus([approval([closed])])
    expect(within(focus).getByRole("button", { name: "Send answer" })).toBeTruthy()
    press(focus, "1")
    press(focus, "Enter", { ctrlKey: true })
    expect(await within(focus).findByRole("alert")).toHaveProperty("textContent", "Already decided somewhere else.")
    expect(onDone).not.toHaveBeenCalled()

    mockDismiss.mockResolvedValueOnce(undefined)
    fireEvent.click(within(focus).getByRole("button", { name: "Dismiss" }))
    await waitFor(() => expect(mockDismiss).toHaveBeenCalledWith("http://legato", "ap-1"))
    await waitFor(() => expect(onDone).toHaveBeenCalled())
  })

  it("shows why dismissing failed, and ignores keys while busy", async () => {
    mockDismiss.mockRejectedValueOnce("offline")
    const { focus, onLeave } = renderFocus([approval([closed])])
    fireEvent.click(within(focus).getByRole("button", { name: "Dismiss" }))
    expect(screen.getByRole("status").textContent).toBe("Sending…")
    press(focus, "Escape")
    expect(onLeave).not.toHaveBeenCalled()
    expect(await within(focus).findByRole("alert")).toHaveProperty("textContent", "offline")
    press(focus, "Escape")
    expect(onLeave).toHaveBeenCalled()
  })

  it("lists the keys at its foot", () => {
    const { focus } = renderFocus([approval([store])])
    const keys = within(focus).getByRole("contentinfo", { name: "Keys" })
    expect(keys.textContent).toBe("1–9choose↑ ↓move↵next←back/type your own⌘↵sendEscleave focus")
  })
})
