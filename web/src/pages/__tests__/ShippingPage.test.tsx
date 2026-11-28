import "@testing-library/jest-dom/vitest"
import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react"
import { MemoryRouter } from "react-router-dom"
import { afterEach, describe, expect, it, vi } from "vitest"
import { ShippingPage } from "../ShippingPage"
import type { ShippingLoop } from "../../lib/shipping"

const { apiFetch, refresh, shipping } = vi.hoisted(() => ({ apiFetch: vi.fn(), refresh: vi.fn(), shipping: { loops: [] as unknown[] } }))

vi.mock("../../lib/api", () => ({ apiFetch }))
vi.mock("../../hooks/useServer", () => ({ useServer: () => ({ baseUrl: "" }) }))
vi.mock("../../hooks/useShipping", () => ({ useShipping: () => ({ data: shipping, loading: false, error: null, refresh }) }))

const minutesAgo = (n: number) => new Date(Date.now() - n * 60_000).toISOString()

const loop = (over: Partial<ShippingLoop>): ShippingLoop => ({
  loop_id: "rl-1", kind: "child_run", caller: "ticket-to-merged-pr", caller_title: "LEG-7 to a merged pull request", run_id: "wr-child", step: "steward[0]/watch",
  link: "/workflows/runs/wr-child", task: { id: "LEG-7", title: "Fix login" }, repo: "acme/widget",
  layers: [{ number: 12, url: "https://github.com/acme/widget/pull/12", title: "Fix the login form", state: "open", draft: false, mergeable: "MERGEABLE" }],
  phase: "watching", summary: "Stewarding", pass: 1, max_passes: 5, checks: "passing",
  steward: { held: false, last_wake: "pushed", last_wake_at: minutesAgo(5), reentries: 1, max_reentries: 3 },
  author: { task: "LEG-7", title: "Fix login", state: "working" }, reviewer: "codex:high", actions: ["stop_watching", "end"],
  needs_you: false, open: true, ...over,
})

const watching = loop({})
const reviewing = loop({ loop_id: "rl-2", task: { id: "board-2", title: "Add dark mode" }, phase: "reviewing", pass: 2, max_passes: 5, steward: undefined, actions: [], run_id: "wr-2", step: "",
  caller: "review-loop", kind: "run", layers: [
    { number: 21, url: "https://github.com/acme/widget/pull/21", title: "Theme tokens", state: "open", draft: false },
    { number: 22, url: "https://github.com/acme/widget/pull/22", title: "Dark palette", state: "open", draft: true },
  ] })
const held = loop({ loop_id: "rl-3", task: { id: "board-3", title: "Retry uploads" }, phase: "held", needs_you: true, actions: ["watch_again", "end"], run_id: "wr-3", step: "steward[0]/watch",
  steward: { held: true, reentries: 0, max_reentries: 3 } })
const failing = loop({ loop_id: "rl-4", task: { id: "board-4", title: "Cache headers" }, needs_you: true, checks: "failing", run_id: "wr-4",
  layers: [{ number: 40, url: "https://github.com/acme/widget/pull/40", state: "open", draft: false, mergeable: "BEHIND", failing_checks: ["go"] }] })
const merged = loop({ loop_id: "rl-5", task: { id: "board-5", title: "Old work" }, phase: "ended", summary: "Merged", outcome: "merged", open: false, actions: [], closed_at: minutesAgo(60) })

function open(loops: ShippingLoop[]) {
  shipping.loops = loops
  render(<MemoryRouter><ShippingPage /></MemoryRouter>)
}
const rowOf = (title: string) => screen.getAllByRole("listitem").find(item => within(item).queryByRole("link", { name: title }))!

afterEach(() => { cleanup(); apiFetch.mockReset(); refresh.mockReset() })

describe("the Shipping page", () => {
  it("shows each loop's task, pull requests, phase, CI, steward, author and reviewer", () => {
    open([watching, reviewing])
    const row = rowOf("Fix login")
    expect(within(row).getByRole("link", { name: "Fix login" })).toHaveAttribute("href", "/board?highlight_task=LEG-7")
    expect(within(row).getByRole("link", { name: "#12" })).toHaveAttribute("href", "https://github.com/acme/widget/pull/12")
    expect(row).toHaveTextContent("acme/widget")
    expect(row).toHaveTextContent("Steward watching")
    expect(row).toHaveTextContent("Checks passing")
    expect(row).toHaveTextContent("Last woke: a push · 5m ago")
    expect(row).toHaveTextContent("Author: LEG-7 · working")
    expect(row).toHaveTextContent("Reviewer: codex:high")
    expect(row).toHaveTextContent("ticket-to-merged-pr")
    expect(within(row).getByRole("link", { name: "Open run" })).toHaveAttribute("href", "/workflows/runs/wr-child")

    // A stack lists every layer, bottom first.
    const stack = rowOf("Add dark mode")
    expect(within(stack).getAllByRole("link", { name: /^#\d+$/ }).map(link => link.textContent)).toEqual(["#21", "#22"])
    expect(stack).toHaveTextContent("Reviewing · pass 2 of 5")
  })

  it("filters by Needs you, Active and Recently ended, with counts", () => {
    open([watching, reviewing, held, failing, merged])
    const active = screen.getByRole("button", { name: "Active 4" })
    expect(active).toHaveAttribute("aria-pressed", "true")
    expect(screen.queryByRole("link", { name: "Old work" })).toBeNull()

    fireEvent.click(screen.getByRole("button", { name: "Needs you 2" }))
    expect(screen.getAllByRole("listitem")).toHaveLength(2)
    expect(rowOf("Retry uploads")).toHaveTextContent("Steward held")
    expect(rowOf("Cache headers")).toHaveTextContent("1 check failing: go · Behind its base")

    fireEvent.click(screen.getByRole("button", { name: "Recently ended 1" }))
    expect(screen.getAllByRole("listitem")).toHaveLength(1)
    expect(rowOf("Old work")).toHaveTextContent("Merged")
    expect(within(rowOf("Old work")).queryByRole("button")).toBeNull()
  })

  it("takes a loop's actions through its waiting step, End only once confirmed", async () => {
    apiFetch.mockResolvedValue(new Response("{}", { status: 200 }))
    open([watching])
    fireEvent.click(screen.getByRole("button", { name: "Stop watching" }))
    await waitFor(() => expect(apiFetch).toHaveBeenCalledWith("", "/api/workflow-runs/wr-child/steps/steward%5B0%5D%2Fwatch/action", expect.objectContaining({ method: "POST", body: JSON.stringify({ action: "stop_watching" }) })))
    await waitFor(() => expect(refresh).toHaveBeenCalled())

    apiFetch.mockClear()
    fireEvent.click(screen.getByRole("button", { name: "End" }))
    const dialog = await screen.findByRole("dialog", { name: "End the steward?" })
    fireEvent.click(within(dialog).getByRole("button", { name: "Cancel" }))
    expect(apiFetch).not.toHaveBeenCalled()
    fireEvent.click(screen.getByRole("button", { name: "End" }))
    fireEvent.click(within(await screen.findByRole("dialog", { name: "End the steward?" })).getByRole("button", { name: "End" }))
    await waitFor(() => expect(apiFetch).toHaveBeenCalledWith("", "/api/workflow-runs/wr-child/steps/steward%5B0%5D%2Fwatch/action", expect.objectContaining({ body: JSON.stringify({ action: "end" }) })))
  })

  it("says why an action was refused", async () => {
    apiFetch.mockResolvedValue(new Response("the loop is closed", { status: 409 }))
    open([held])
    fireEvent.click(screen.getByRole("button", { name: "Watch again" }))
    expect(await within(rowOf("Retry uploads")).findByRole("alert")).toHaveTextContent("the loop is closed")
  })

  it("says when nothing is open", () => {
    open([])
    expect(screen.getByText("No review loops are open.")).toBeInTheDocument()
  })
})
