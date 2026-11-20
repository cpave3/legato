import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import "@testing-library/jest-dom/vitest"
import { act, cleanup, fireEvent, render, screen, within } from "@testing-library/react"
import type { WidgetApproval } from "./approvals"
import type { WidgetItem } from "./items"

// The app's events and commands, as Rust emits and answers them.
const handlers = new Map<string, (e: { payload: unknown }) => void>()
const invoked: { cmd: string; args?: unknown }[] = []
let approvals: WidgetApproval[] = []
let items: WidgetItem[] = []

vi.mock("@tauri-apps/api/event", () => ({
  listen: (name: string, handler: (e: { payload: unknown }) => void) => {
    handlers.set(name, handler)
    return Promise.resolve(() => handlers.delete(name))
  },
}))
vi.mock("@tauri-apps/api/core", () => ({
  invoke: (cmd: string, args?: unknown) => {
    invoked.push({ cmd, args })
    if (cmd === "voice_approvals") return Promise.resolve(approvals)
    if (cmd === "voice_items") return Promise.resolve(items)
    return Promise.resolve(null)
  },
}))

const { Feed } = await import("./Feed")

function emit(name: string, payload: unknown) {
  act(() => handlers.get(name)?.({ payload }))
}

function item(id: string, overrides: Partial<WidgetItem["item"]> = {}, server = "s1", name = "tessitura"): WidgetItem {
  return {
    server, server_name: name,
    item: {
      id, producer: "operator_items", signal_kind: "operator_action", source_revision: "2", task_id: "LEG-363",
      title: "Type /to-tickets in LEG-363's session", reason: "Blocks 7 LEG-365 build tickets",
      navigation: { kind: "agent", target: "LEG-363" }, updated_at: "2026-11-20T08:00:00Z",
      actions: [{ id: "open", label: "Open agent", kind: "navigate" }, { id: "done", label: "Done", kind: "execute" }],
      ...overrides,
    },
  }
}

const approval: WidgetApproval = {
  server: "s1", server_name: "tessitura",
  approval: { id: "ap-1", created_at: "2026-11-20T07:00:00Z", source: "credential", title: "Use github", options: [{ id: "once", label: "once", kind: "allow" }], details: {} },
}

describe("operator items in the widget's feed", () => {
  beforeEach(() => { handlers.clear(); invoked.length = 0; approvals = []; items = [] })
  afterEach(() => cleanup())

  it("lists them below the approvals, one line each", async () => {
    approvals = [approval]
    items = [item("att-1")]
    render(<Feed />)
    const rows = await screen.findAllByRole("listitem")
    expect(rows).toHaveLength(2)
    expect(rows[0]).toHaveAttribute("data-kind", "approval")
    expect(rows[1]).toHaveAttribute("data-kind", "item")
    expect(rows[1]).toHaveTextContent("Type /to-tickets in LEG-363's session")
    expect(rows[1]).toHaveTextContent("Action")
    expect(screen.getByText("2 need you")).toBeInTheDocument()
  })

  it("names the server only when there are several", async () => {
    render(<Feed />)
    emit("voice-items", [item("att-1")])
    expect(screen.getByRole("listitem")).not.toHaveTextContent("tessitura")
    emit("voice-items", [item("att-1"), item("att-2", { title: "Merge PR #181" }, "s2", "vm-1")])
    const [first, second] = screen.getAllByRole("listitem")
    expect(first).toHaveTextContent("tessitura")
    expect(second).toHaveTextContent("vm-1")
  })

  it("opens the main window on the item, and runs Done", async () => {
    render(<Feed />)
    emit("voice-items", [item("att-1")])
    const row = screen.getByRole("listitem")
    fireEvent.click(within(row).getByRole("button", { expanded: false }))
    expect(within(row).getByText("Blocks 7 LEG-365 build tickets")).toBeInTheDocument()
    fireEvent.click(within(row).getByRole("button", { name: "Open" }))
    expect(invoked).toContainEqual({ cmd: "voice_item_open", args: { server: "s1", kind: "agent", target: "LEG-363" } })
    fireEvent.click(within(row).getByRole("button", { name: "Done" }))
    expect(invoked).toContainEqual({ cmd: "voice_item_done", args: { server: "s1", item: "att-1", revision: "2" } })
  })

  it("opens the main window for a question, which needs the keyboard", async () => {
    render(<Feed />)
    emit("voice-items", [item("att-q", {
      signal_kind: "operator_question", title: "Which database?",
      actions: [{ id: "option_1", label: "Postgres", kind: "execute" }, { id: "reply", label: "Reply", kind: "execute", requires_text: true }, { id: "done", label: "Done", kind: "execute" }],
    })])
    const row = screen.getByRole("listitem")
    fireEvent.click(within(row).getByRole("button", { expanded: false }))
    expect(within(row).queryByRole("button", { name: "Done" })).not.toBeInTheDocument()
    fireEvent.click(within(row).getByRole("button", { name: "Answer in Legato" }))
    expect(invoked).toContainEqual({ cmd: "voice_item_open", args: { server: "s1", kind: "agent", target: "LEG-363" } })
  })
})
