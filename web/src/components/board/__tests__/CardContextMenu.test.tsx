import { cleanup, fireEvent, render, screen, within } from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { CardContextMenu } from "../CardContextMenu"
import { cardWorkflowOptions } from "../../../lib/card-workflows"
import type { BoardCard } from "../../../lib/board-types"

const card = { id: "T-1", title: "Context card" } as BoardCard
const workflow = { definition: { schema_version: 1, name: "triage", title: "Triage", source: "triage.yaml", steps: [] }, taskInput: "subject" } as never

beforeEach(() => {
  Object.defineProperty(window, "innerWidth", { value: 1024, configurable: true })
  vi.spyOn(HTMLElement.prototype, "getBoundingClientRect").mockImplementation(function (this: HTMLElement) {
    if (this.getAttribute('role') === 'menu') return new DOMRect(Number.parseFloat(this.style.left) || 0, Number.parseFloat(this.style.top) || 0, this.getAttribute('aria-label')?.startsWith('Workflows') ? 280 : 240, 196)
    if (this.getAttribute('aria-haspopup') === 'menu') return new DOMRect(Number.parseFloat(this.parentElement!.style.left), 100, 232, 40)
    return new DOMRect()
  })
})
afterEach(() => { cleanup(); vi.restoreAllMocks() })

describe("CardContextMenu", () => {
  it("clamps to the viewport and dismisses with Escape or outside click", () => {
    Object.defineProperty(window, "innerWidth", { value: 320, configurable: true })
    Object.defineProperty(window, "innerHeight", { value: 300, configurable: true })
    const onClose = vi.fn()
    const { rerender } = render(<CardContextMenu card={card} position={{ x: 999, y: 999 }} onAction={() => {}} onClose={onClose} />)
    const menu = screen.getByRole("menu")
    expect(menu.style.left).toBe("72px")
    expect(menu.style.top).toBe("96px")

    fireEvent.keyDown(document.activeElement!, { key: "Escape" })
    expect(onClose).toHaveBeenCalledTimes(1)

    onClose.mockClear()
    rerender(<CardContextMenu card={card} position={{ x: 10, y: 10 }} onAction={() => {}} onClose={onClose} />)
    fireEvent.pointerDown(screen.getByTestId("card-context-backdrop"))
    expect(onClose).toHaveBeenCalledTimes(1)
  })

  it("lists the task's macros and starts the one chosen", () => {
    const onMacro = vi.fn()
    const onClose = vi.fn()
    const macros = [{ name: "Plan", keys: "p", applies_to: "task" as const }, { name: "tests", keys: "t" }]
    const { unmount } = render(<CardContextMenu card={card} position={{ x: 10, y: 10 }} macros={macros} onMacro={onMacro} onAction={() => {}} onClose={onClose} />)
    expect(screen.getByText("Macros")).toBeTruthy()
    fireEvent.click(screen.getByRole("menuitem", { name: "Macro: Plan" }))
    expect(onMacro).toHaveBeenCalledWith(macros[0])
    expect(onClose).toHaveBeenCalled()
    unmount()
    render(<CardContextMenu card={{ ...card, away: { worker: "w" } } as BoardCard} position={{ x: 10, y: 10 }} macros={macros} onMacro={onMacro} onAction={() => {}} onClose={() => {}} />)
    expect(screen.queryByRole("menuitem", { name: "Macro: Plan" })).toBeNull()
  })

  it("names the agent and tier a macro starts only when it sets them, and links to the Macros page", () => {
    const onManageMacros = vi.fn()
    const macros = [{ name: "Plan", keys: "p" }, { name: "Implement", keys: "i", agent: "codex" }, { name: "Deep", keys: "d", agent: "claude-code", tier: "large" }]
    render(<CardContextMenu card={card} position={{ x: 10, y: 10 }} macros={macros} onMacro={() => {}} onManageMacros={onManageMacros} onAction={() => {}} onClose={() => {}} />)
    expect(screen.getByRole("menuitem", { name: "Macro: Plan" }).textContent).toBe("Plan")
    expect(screen.getByRole("menuitem", { name: "Macro: Implement" }).textContent).toBe("ImplementCodex")
    expect(screen.getByRole("menuitem", { name: "Macro: Deep" }).textContent).toBe("DeepClaude Code · large tier")
    fireEvent.click(screen.getByRole("menuitem", { name: "Manage macros" }))
    expect(onManageMacros).toHaveBeenCalled()
  })

  it("offers review passes for each of a card's pull requests, and none for a card without", () => {
    const onReviewPasses = vi.fn()
    const pr = (n: number) => ({ repo: "acme/widget", branch: `b${n}`, pr_number: n, pr_url: "", state: "OPEN" }) as never
    render(<CardContextMenu card={card} position={{ x: 10, y: 10 }} reviewPasses={[pr(12), pr(13)]} onReviewPasses={onReviewPasses} onAction={() => {}} onClose={() => {}} />)
    expect(screen.getByRole("menuitem", { name: "Review passes · #12" })).toBeTruthy()
    fireEvent.click(screen.getByRole("menuitem", { name: "Review passes · #13" }))
    expect(onReviewPasses).toHaveBeenCalledWith(expect.objectContaining({ pr_number: 13 }))
    cleanup()
    render(<CardContextMenu card={card} position={{ x: 10, y: 10 }} onAction={() => {}} onClose={() => {}} />)
    expect(screen.queryByRole("menuitem", { name: /Review passes/ })).toBeNull()
  })

  it("offers the whole stack first when the card's pull requests are a stack", () => {
    const onReviewPasses = vi.fn()
    const pr = (n: number) => ({ repo: "acme/widget", branch: `b${n}`, pr_number: n, pr_url: "", state: "OPEN" }) as never
    render(<CardContextMenu card={card} position={{ x: 10, y: 10 }} reviewPasses={[pr(137), pr(138), pr(139)]} reviewStack onReviewPasses={onReviewPasses} onAction={() => {}} onClose={() => {}} />)
    const items = screen.getAllByRole("menuitem", { name: /Review passes/ }).map(item => item.textContent)
    expect(items).toEqual(["Review passes · stack #137–#139", "Review passes · #137", "Review passes · #138", "Review passes · #139"])
    fireEvent.click(screen.getByRole("menuitem", { name: "Review passes · stack #137–#139" }))
    expect(onReviewPasses).toHaveBeenCalledWith(expect.objectContaining({ pr_number: 137 }), "stack")
    cleanup()
    render(<CardContextMenu card={card} position={{ x: 10, y: 10 }} reviewPasses={[pr(137), pr(138)]} reviewStack onReviewPasses={onReviewPasses} onAction={() => {}} onClose={() => {}} />)
    fireEvent.click(screen.getByRole("menuitem", { name: "Review passes · #138" }))
    expect(onReviewPasses).toHaveBeenLastCalledWith(expect.objectContaining({ pr_number: 138 }), "pr")
    cleanup()
    // One pull request: unchanged.
    render(<CardContextMenu card={card} position={{ x: 10, y: 10 }} reviewPasses={[pr(12)]} reviewStack onReviewPasses={onReviewPasses} onAction={() => {}} onClose={() => {}} />)
    expect(screen.getAllByRole("menuitem", { name: /Review passes/ }).map(item => item.textContent)).toEqual(["Review passes · #12"])
    fireEvent.click(screen.getByRole("menuitem", { name: "Review passes · #12" }))
    expect(onReviewPasses).toHaveBeenLastCalledWith(expect.objectContaining({ pr_number: 12 }))
  })

  it("emits existing card actions", () => {
    const onAction = vi.fn()
    render(<CardContextMenu card={card} position={{ x: 10, y: 10 }} onAction={onAction} onClose={() => {}} />)
    const open = screen.getByRole("menuitem", { name: "Open details" })
    const move = screen.getByRole("menuitem", { name: "Move" })
    open.focus()
    fireEvent.keyDown(document.activeElement!, { key: "ArrowDown" })
    expect(document.activeElement).toBe(move)
    fireEvent.click(move)
    expect(onAction).toHaveBeenCalledWith("move")
  })

  it("opens a scrollable workflow flyout without closing on internal scroll", () => {
    const onClose = vi.fn()
    render(<CardContextMenu card={card} position={{ x: 10, y: 10 }} workflows={[workflow]} onAction={() => {}} onClose={onClose} />)
    fireEvent.mouseEnter(screen.getByRole("menuitem", { name: "Workflows" }))
    const flyout = screen.getByRole("menu", { name: "Workflows for Context card" })
    fireEvent.scroll(flyout)
    expect(onClose).not.toHaveBeenCalled()
    expect(screen.getByRole("menuitem", { name: "Triage" })).toBeTruthy()

    fireEvent.scroll(document.body)
    expect(onClose).not.toHaveBeenCalled()
  })

  it("flips the workflow flyout left near the viewport edge", () => {
    Object.defineProperty(window, "innerWidth", { value: 900, configurable: true })
    Object.defineProperty(window, "innerHeight", { value: 300, configurable: true })
    render(<CardContextMenu card={card} position={{ x: 890, y: 40 }} workflows={[workflow]} onAction={() => {}} onClose={() => {}} />)
    fireEvent.mouseEnter(screen.getByRole("menuitem", { name: "Workflows" }))
    const primary = screen.getByRole("menu", { name: "Actions for Context card" })
    const flyout = screen.getByRole("menu", { name: "Workflows for Context card" })
    expect(Number.parseInt(flyout.style.left)).toBeLessThan(Number.parseInt(primary.style.left))
    expect(flyout.style.maxHeight).not.toBe("")
  })

  it("enters and leaves the flyout with keyboard controls", () => {
    const onClose = vi.fn()
    const onWorkflow = vi.fn()
    render(<CardContextMenu card={card} position={{ x: 10, y: 10 }} workflows={[workflow]} onAction={() => {}} onWorkflow={onWorkflow} onClose={onClose} />)
    const trigger = screen.getByRole("menuitem", { name: "Workflows" })
    trigger.focus()
    fireEvent.keyDown(document.activeElement!, { key: "ArrowRight" })
    const item = screen.getByRole("menuitem", { name: "Triage" })
    expect(document.activeElement).toBe(item)
    fireEvent.keyDown(document.activeElement!, { key: "ArrowLeft" })
    expect(document.activeElement).toBe(trigger)
    fireEvent.keyDown(trigger, { key: "ArrowRight" })
    fireEvent.keyDown(document.activeElement!, { key: "Escape" })
    expect(onClose).not.toHaveBeenCalled()
    expect(document.activeElement).toBe(trigger)
    fireEvent.keyDown(trigger, { key: "Escape" })
    expect(onClose).toHaveBeenCalledTimes(1)
    fireEvent.keyDown(trigger, { key: "ArrowRight" })
    fireEvent.click(screen.getByRole("menuitem", { name: "Triage" }))
    expect(onWorkflow).toHaveBeenCalledWith(workflow)

  })

  it("shows only definitions with exactly one task input, including triggered definitions", () => {
    const base = { schema_version: 1, title: "Test", source: "test.yaml", steps: [] }
    const options = cardWorkflowOptions([
      { ...base, name: "one", title: "One task", inputs: { subject: { type: "task" } }, on: { task: { entered_column: { columns: ["Done"] } } } },
      { ...base, name: "zero", inputs: { label: { type: "string" } } },
      { ...base, name: "two", inputs: { source: { type: "task" }, target: { type: "task" } } },
    ] as never)
    expect(options.map(option => [option.definition.name, option.taskInput])).toEqual([["one", "subject"]])
  })

  it("offers Hand off… once a worker is paired", () => {
    const onAction = vi.fn()
    const { rerender } = render(<CardContextMenu card={card} position={{ x: 10, y: 10 }} onAction={onAction} onClose={() => {}} />)
    expect(screen.queryByRole("menuitem", { name: "Hand off…" })).toBeNull()
    rerender(<CardContextMenu card={card} position={{ x: 10, y: 10 }} canHandOff onAction={onAction} onClose={() => {}} />)
    fireEvent.click(screen.getByRole("menuitem", { name: "Hand off…" }))
    expect(onAction).toHaveBeenCalledWith("handoff")
  })

  it("offers only what an away task allows", () => {
    const onAction = vi.fn()
    const away = { ...card, away: { worker: "vm-1", url: "https://vm-1.lan:3080/board?highlight_task=w-1" } } as BoardCard
    render(<CardContextMenu card={away} position={{ x: 10, y: 10 }} canHandOff onAction={onAction} onClose={() => {}} />)
    for (const name of ["Move", "Delete", "Hand off…"]) expect(screen.queryByRole("menuitem", { name })).toBeNull()
    fireEvent.click(screen.getByRole("menuitem", { name: "Open on vm-1" }))
    expect(onAction).toHaveBeenCalledWith("openOnWorker")
  })

  it("lists each open pull request's macros, suggested first, and starts the one chosen on that PR", () => {
    const onPRMacro = vi.fn()
    const onClose = vi.fn()
    const pr = (n: number, extra = {}) => ({ repo: "acme/app", branch: `b${n}`, pr_number: n, pr_url: `https://github.com/acme/app/pull/${n}`, state: "OPEN", is_draft: false, review_decision: "", check_status: "pass", comment_count: 0, pinned: false, match_method: "", sync_outcome: "", sync_error: "", ...extra })
    const fix = { name: "Address feedback", keys: "a", applies_to: "pr" as const }
    const review = { name: "Review", keys: "r", applies_to: "pr" as const }
    const plan = { name: "Plan", keys: "p", applies_to: "task" as const }
    const prs = [pr(194), pr(200, { pinned: true })]
    render(<CardContextMenu card={card} position={{ x: 10, y: 10 }} macros={[plan, fix, review]} suggested={{ task: "Plan", my_pr: "Address feedback", others_pr: "Review" }} macroPRs={prs} onPRMacro={onPRMacro} onAction={() => {}} onClose={onClose} />)
    const triggers = screen.getAllByRole("menuitem", { name: /^Macros for #/ })
    expect(triggers.map(item => item.getAttribute("aria-label"))).toEqual(["Macros for #194", "Macros for #200"])
    fireEvent.click(triggers[1])
    const flyout = screen.getByRole("menu", { name: "Macros for #200" })
    const items = Array.from(flyout.querySelectorAll('[role="menuitem"]')).map(item => item.textContent)
    expect(items).toEqual(["Review", "Address feedback"])
    fireEvent.click(screen.getByRole("menuitem", { name: "Macro: Review" }))
    expect(onPRMacro).toHaveBeenCalledWith(prs[1], review)
    expect(onClose).toHaveBeenCalled()
  })

  it("lists the favourite macros, the task's and each pull request's, and the rest behind Show all", () => {
    const onMacro = vi.fn()
    const onPRMacro = vi.fn()
    const pr = { repo: "acme/app", branch: "b", pr_number: 7, pr_url: "", state: "OPEN", is_draft: false, review_decision: "", check_status: "", comment_count: 0, pinned: false, match_method: "", sync_outcome: "", sync_error: "" }
    const plan = { name: "Plan", keys: "p", applies_to: "task" as const, favorite: true }
    const ticket = { name: "Ticket to PR", keys: "t", applies_to: "task" as const }
    const review = { name: "Review", keys: "r", applies_to: "pr" as const, favorite: true }
    const deep = { name: "Deep review", keys: "d", applies_to: "pr" as const }
    render(<CardContextMenu card={card} position={{ x: 10, y: 10 }} macros={[plan, review, ticket, deep]} onMacro={onMacro} macroPRs={[pr]} onPRMacro={onPRMacro} onAction={() => {}} onClose={() => {}} />)
    expect(screen.getByRole("menuitem", { name: "Macro: Plan" })).toBeTruthy()
    expect(screen.queryByRole("menuitem", { name: "Macro: Ticket to PR" })).toBeNull()
    fireEvent.click(screen.getByRole("menuitem", { name: "Show all (1 more)" }))
    expect(screen.getByRole("menuitem", { name: "Macro: Ticket to PR" })).toBeTruthy()

    fireEvent.click(screen.getByRole("menuitem", { name: "Macros for #7" }))
    const flyout = screen.getByRole("menu", { name: "Macros for #7" })
    expect(within(flyout).queryByRole("menuitem", { name: "Macro: Deep review" })).toBeNull()
    fireEvent.click(within(flyout).getByRole("menuitem", { name: "Show all (1 more)" }))
    expect(within(flyout).getAllByRole("menuitem").map(item => item.textContent)).toEqual(["Review", "Deep review"])
    fireEvent.click(within(flyout).getByRole("menuitem", { name: "Macro: Deep review" }))
    expect(onPRMacro).toHaveBeenCalledWith(pr, deep)
  })

  it("keeps the unstarred behind Show all when the only star is on a pull request's macro", () => {
    const pr = { repo: "acme/app", branch: "b", pr_number: 7, pr_url: "", state: "OPEN", is_draft: false, review_decision: "", check_status: "", comment_count: 0, pinned: false, match_method: "", sync_outcome: "", sync_error: "" }
    const macros = [{ name: "Plan", keys: "p", applies_to: "task" as const }, { name: "Review", keys: "r", applies_to: "pr" as const, favorite: true }]
    render(<CardContextMenu card={card} position={{ x: 10, y: 10 }} macros={macros} onMacro={() => {}} macroPRs={[pr]} onPRMacro={() => {}} onAction={() => {}} onClose={() => {}} />)
    expect(screen.queryByRole("menuitem", { name: "Macro: Plan" })).toBeNull()
    expect(screen.queryByRole("menuitem", { name: "Macro: Review" })).toBeNull()
    fireEvent.click(screen.getByRole("menuitem", { name: "Show all (1 more)" }))
    expect(screen.getByRole("menuitem", { name: "Macro: Plan" })).toBeTruthy()
  })

  it("lists every macro while none is starred", () => {
    render(<CardContextMenu card={card} position={{ x: 10, y: 10 }} macros={[{ name: "Plan", keys: "p" }, { name: "Ticket to PR", keys: "t" }]} onMacro={() => {}} onAction={() => {}} onClose={() => {}} />)
    expect(screen.getByRole("menuitem", { name: "Macro: Ticket to PR" })).toBeTruthy()
    expect(screen.queryByRole("menuitem", { name: /show all/i })).toBeNull()
  })

  it("lists a pull request's macros on a phone, with a way back", () => {
    Object.defineProperty(window, "innerWidth", { value: 390, configurable: true })
    const pr = { repo: "acme/app", branch: "b", pr_number: 7, pr_url: "", state: "OPEN", is_draft: false, review_decision: "", check_status: "", comment_count: 0, pinned: false, match_method: "", sync_outcome: "", sync_error: "" }
    const onPRMacro = vi.fn()
    render(<CardContextMenu card={card} position={{ x: 10, y: 10 }} macros={[{ name: "Address feedback", keys: "a" }]} macroPRs={[pr]} onPRMacro={onPRMacro} onAction={() => {}} onClose={() => {}} />)
    fireEvent.click(screen.getByRole("menuitem", { name: "Macros for #7" }))
    expect(screen.getByRole("menuitem", { name: "Back" })).toBeTruthy()
    fireEvent.click(screen.getByRole("menuitem", { name: "Macro: Address feedback" }))
    expect(onPRMacro).toHaveBeenCalledWith(pr, { name: "Address feedback", keys: "a" })
  })

  it("opens the ticket at its source when the task has one", () => {
    const onAction = vi.fn()
    const { rerender } = render(<CardContextMenu card={{ ...card, provider: "burnlite", source_url: "https://burnlite.dev/LEG-386" } as BoardCard} position={{ x: 10, y: 10 }} onAction={onAction} onClose={() => {}} />)
    fireEvent.click(screen.getByRole("menuitem", { name: "Open in Burnlite" }))
    expect(onAction).toHaveBeenCalledWith("openSource")
    rerender(<CardContextMenu card={{ ...card, provider: "jira-copy", source_url: "https://acme.atlassian.net/browse/AG-1" } as BoardCard} position={{ x: 10, y: 10 }} onAction={onAction} onClose={() => {}} />)
    expect(screen.getByRole("menuitem", { name: "Open in Jira" })).toBeTruthy()
    rerender(<CardContextMenu card={{ ...card, provider: "" } as BoardCard} position={{ x: 10, y: 10 }} onAction={onAction} onClose={() => {}} />)
    expect(screen.queryByRole("menuitem", { name: /^Open in / })).toBeNull()
  })
})
