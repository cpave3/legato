import { describe, expect, it, vi } from "vitest"
import { fireEvent, render, screen, within } from "@testing-library/react"
import { TileDeck, type DeckTile } from "../TileDeck"

const tiles: DeckTile[] = [
  { id: "a", title: "Map call sites", state: "done", startedAt: 0, endedAt: 72_000, summary: "9 call sites", body: <p>map body</p> },
  { id: "b", title: "Audit tests", meta: "general-purpose", state: "running", startedAt: 0, count: "11 tools", summary: "Reading tests", body: <p>audit body</p>, detail: <p>audit prompt</p> },
  { id: "c", title: "Check docs", state: "running", summary: "Reading docs", body: <p>docs body</p> },
  { id: "d", title: "Search CI", state: "error", summary: "auth required", body: <p>ci body</p> },
]

function deck(props: Partial<Parameters<typeof TileDeck>[0]> = {}) {
  const onFocus = vi.fn()
  render(<TileDeck title="Sub-agents" tiles={tiles} layout="grid" focused={null} onFocus={onFocus} {...props} />)
  return { onFocus }
}

describe("TileDeck", () => {
  it("lays the running tiles out live and puts finished ones on a shelf", () => {
    deck()

    const live = screen.getByRole("list", { name: "Running" })
    expect(within(live).getAllByRole("article").map((tile) => tile.getAttribute("aria-label"))).toEqual(["Audit tests, running", "Check docs, running"])
    expect(within(live).getByText("audit body")).not.toBeNull()

    const shelf = screen.getByRole("list", { name: "Finished" })
    expect(within(shelf).getByRole("button", { name: /Map call sites/ }).textContent).toContain("9 call sites")
    expect(within(shelf).getByRole("button", { name: /Search CI/ }).textContent).toContain("auth required")
    expect(screen.queryByText("map body")).toBeNull()
  })

  it("asks to open a tile when one is chosen", () => {
    const { onFocus } = deck()

    fireEvent.click(screen.getByRole("button", { name: "Open Audit tests" }))
    fireEvent.click(screen.getByRole("button", { name: /Search CI/ }))

    expect(onFocus.mock.calls).toEqual([["b"], ["d"]])
  })

  it("shows an opened tile in full, with its detail, and closes it on request or Escape", () => {
    const { onFocus } = deck({ focused: "b" })

    const opened = screen.getByRole("article", { name: "Audit tests, running" })
    expect(within(opened).getByText("audit prompt")).not.toBeNull()
    expect(within(opened).getByText("audit body")).not.toBeNull()
    expect(screen.queryByText("docs body")).toBeNull()

    // The others stay one click away.
    fireEvent.click(screen.getByRole("button", { name: /Check docs/ }))
    fireEvent.click(screen.getByRole("button", { name: "All tiles" }))
    fireEvent.keyDown(document, { key: "Escape" })

    expect(onFocus.mock.calls).toEqual([["c"], [null], [null]])
  })

  it("keeps the finished shelf short: the newest three, the rest on request, and folds away", () => {
    const finished = (id: string, endedAt: number): DeckTile =>
      ({ id, title: `Job ${id}`, state: "done", startedAt: 0, endedAt, summary: "ok", body: <p>{id}</p> })
    render(<TileDeck title="Sub-agents" layout="grid" focused={null} onFocus={() => {}}
      tiles={[finished("1", 10_000), finished("2", 50_000), finished("3", 20_000), finished("4", 40_000), finished("5", 30_000)]} />)

    const titles = () => within(screen.getByRole("list", { name: "Finished" })).getAllByRole("button").map((button) => button.textContent?.match(/Job \d/)?.[0])
    expect(titles()).toEqual(["Job 2", "Job 4", "Job 5"])

    fireEvent.click(screen.getByRole("button", { name: "Show 2 more" }))
    expect(titles()).toEqual(["Job 2", "Job 4", "Job 5", "Job 3", "Job 1"])

    fireEvent.click(screen.getByRole("button", { name: /Finished/, expanded: true }))
    expect(screen.queryByRole("list", { name: "Finished" })).toBeNull()
    expect(screen.getByRole("button", { name: /Finished/, expanded: false }).textContent).toContain("5")
  })

  it("shows how long a tile ran", () => {
    deck()

    expect(screen.getByRole("button", { name: /Map call sites/ }).textContent).toContain("1m 12s")
  })

  it("on a phone, lists every tile in a strip, running first", () => {
    deck({ layout: "strip" })

    const strip = screen.getByRole("list", { name: "Sub-agents" })
    expect(within(strip).getAllByRole("button").map((button) => button.getAttribute("aria-label"))).toEqual([
      "Open Audit tests", "Open Check docs", "Open Map call sites", "Open Search CI",
    ])
    expect(screen.queryByRole("dialog")).toBeNull()
  })

  it("on a phone, opens a tile as a sheet that steps to the next one", () => {
    const { onFocus } = deck({ layout: "strip", focused: "c" })

    const sheet = screen.getByRole("dialog", { name: "Check docs" })
    expect(within(sheet).getByText("docs body")).not.toBeNull()

    fireEvent.click(within(sheet).getByRole("button", { name: "Next" }))
    fireEvent.click(within(sheet).getByRole("button", { name: "Previous" }))
    fireEvent.click(within(sheet).getByRole("button", { name: "Close" }))

    // Order is the strip's: Audit tests, Check docs, Map call sites, Search CI.
    expect(onFocus.mock.calls).toEqual([["a"], ["b"], [null]])
  })
})
