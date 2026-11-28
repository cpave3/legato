import { describe, it, expect, vi, afterEach } from "vitest"
import { render, screen, fireEvent, cleanup, waitFor } from "@testing-library/react"
import type { Macro } from "../../../lib/macros"

const mockFetchMacros = vi.fn()
vi.mock("../../../hooks/useServer", () => ({ useServer: () => ({ baseUrl: "http://legato" }) }))
vi.mock("../../../lib/macros", async (importOriginal) => ({
  ...(await importOriginal<typeof import("../../../lib/macros")>()),
  fetchMacroList: (...a: unknown[]) => mockFetchMacros(...a),
}))

import { MacroPickerButton } from "../MacroPicker"

const A: Macro = { name: "Fix CI", keys: "a", applies_to: "task", favorite: true }
const B: Macro = { name: "Review", keys: "b", applies_to: "pr", favorite: true }
const C: Macro = { name: "Plan", keys: "c" }
const D: Macro = { name: "Ticket to PR", keys: "d", applies_to: "task" }

function serve(macros: Macro[]) {
  mockFetchMacros.mockResolvedValue({ macros, suggested: { task: "", my_pr: "", others_pr: "" }, tiers: {} })
}

// The rows of the open menu, in order.
async function rows(): Promise<string[]> {
  await screen.findByRole("menu")
  await waitFor(() => expect(mockFetchMacros).toHaveBeenCalled())
  await waitFor(() => expect(screen.queryAllByRole("menuitem").length).toBeGreaterThan(0))
  return screen.getAllByRole("menuitem").map(row => row.textContent ?? "")
}

function open(props: Partial<React.ComponentProps<typeof MacroPickerButton>> = {}) {
  const onPick = vi.fn()
  render(<MacroPickerButton label="Resume agent" onPick={onPick} {...props} />)
  fireEvent.click(screen.getByRole("button", { name: "Resume agent…" }))
  return onPick
}

afterEach(() => { cleanup(); vi.clearAllMocks() })

describe("the macro picker button", () => {
  it("lists only the macros for the targets passed, favourites first", async () => {
    serve([A, B, C, D])
    open({ targets: ["pr"] })
    expect(await rows()).toEqual(["Review", "Show all (1 more)"])
    fireEvent.click(screen.getByRole("menuitem", { name: /Show all/ }))
    expect(await rows()).toEqual(["Review", "Plan"])
  })

  it("lists task, pull request and unscoped macros for both targets", async () => {
    serve([A, B, C, D])
    open({ targets: ["task", "pr"] })
    expect(await rows()).toEqual(["Fix CI", "Review", "Show all (2 more)"])
    fireEvent.click(screen.getByRole("menuitem", { name: /Show all/ }))
    expect(await rows()).toEqual(["Fix CI", "Review", "Plan", "Ticket to PR"])
  })

  it("lists every macro without targets", async () => {
    serve([A, B, C, D])
    open()
    expect(await rows()).toEqual(["Fix CI", "Review", "Show all (2 more)"])
  })

  it("keeps the unstarred behind Show all when the only star is on a macro it leaves out", async () => {
    serve([A, { ...B, favorite: false }, C])
    open({ targets: ["pr"] })
    expect(await rows()).toEqual(["Show all (2 more)"])
    fireEvent.click(screen.getByRole("menuitem", { name: /Show all/ }))
    expect(await rows()).toEqual(["Review", "Plan"])
  })

  it("puts the suggested macro first in its group", async () => {
    serve([A, B, C, D])
    open({ targets: ["task"], suggested: "Ticket to PR" })
    fireEvent.click(await screen.findByRole("menuitem", { name: /Show all/ }))
    expect(await rows()).toEqual(["Fix CI", "Ticket to PR", "Plan"])
  })

  it("gives the macro picked and closes", async () => {
    serve([A, B])
    const onPick = open({ targets: ["task", "pr"] })
    fireEvent.click(await screen.findByRole("menuitem", { name: "Fix CI" }))
    expect(onPick).toHaveBeenCalledWith(expect.objectContaining({ name: "Fix CI" }))
    expect(screen.queryByRole("menu")).toBeNull()
  })

  it("says why it has nothing to list", async () => {
    mockFetchMacros.mockRejectedValue(new Error("server down"))
    open()
    expect((await screen.findByRole("alert")).textContent).toBe("server down")
    cleanup()
    serve([A])
    open({ targets: ["pr"] })
    expect(await screen.findByText("(no macros)")).toBeTruthy()
  })
})
