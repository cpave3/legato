import { describe, it, expect, vi, beforeEach, afterEach } from "vitest"
import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react"
import { useState } from "react"
import { MemoryRouter } from "react-router-dom"
import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import type { AttentionItem } from "../../lib/attention"

const railView = vi.hoisted(() => ({ current: { items: [] as unknown[], hidden: 0, hiddenNames: [] as string[], snoozeSeconds: undefined as number | undefined } }))
vi.mock("../../lib/needsYou", async (importOriginal) => ({
  ...(await importOriginal<typeof import("../../lib/needsYou")>()),
  fetchRailAttention: () => Promise.resolve(railView.current),
}))
const peers = vi.hoisted(() => ({ current: [] as unknown[] }))
vi.mock("../usePeerApprovals", () => ({ usePeerApprovals: () => peers.current }))
vi.mock("../useNavigationSummary", () => ({ useNavigationSummary: () => ({ data: { peer_approvals: peers.current.length } }) }))
vi.mock("../useServer", () => ({ useServer: () => ({ baseUrl: "http://legato" }) }))
const selection = vi.hoisted(() => ({ current: null as string | null }))
vi.mock("../useAgentSelection", () => ({ useAgentSelection: () => ({ selectedAgentId: selection.current }) }))

import { useSheetAutoOpen } from "../useSheetAutoOpen"
import { resetSnoozeMemory } from "../../lib/railSnooze"
import { setFocusMode } from "../../lib/focusMode"

function item(id: string, signal_kind: string, task = "task-1"): AttentionItem {
  return {
    id, producer: "test", signal_kind, source_key: id, source_revision: "1", title: id, reason: "",
    category: "test", urgency: "normal", task_id: task, navigation: { kind: "agent", target: task },
    updated_at: "2026-11-21T10:00:00Z",
  } as AttentionItem
}

// What the layout does on a phone: the tab toggles the sheet, the sheet's
// Close closes it, and the hook opens it by itself.
function Phone({ blocked = false }: { blocked?: boolean }) {
  const [open, setOpen] = useState(false)
  useSheetAutoOpen({ open, blocked, onOpen: () => setOpen(true) })
  return (
    <>
      <label>Message<input /></label>
      <button type="button" onClick={() => setOpen((was) => !was)}>Needs you</button>
      {open && <div role="dialog" aria-modal="true" aria-label="Needs you sheet"><button type="button" onClick={() => setOpen(false)}>Close</button></div>}
    </>
  )
}

let client: QueryClient
function page(props: { blocked?: boolean; path?: string } = {}) {
  client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(
    <QueryClientProvider client={client}>
      <MemoryRouter initialEntries={[props.path ?? "/board"]}><Phone blocked={props.blocked} /></MemoryRouter>
    </QueryClientProvider>,
  )
}

// reload is a fresh page on the same browser: storage stays.
function reload(props: Parameters<typeof page>[0] = {}) {
  cleanup()
  resetSnoozeMemory()
  return page(props)
}

async function arrive(...items: AttentionItem[]) {
  railView.current = { ...railView.current, items }
  await act(async () => { await client.invalidateQueries() })
}

const sheet = () => screen.queryByRole("dialog", { name: "Needs you sheet" })
// Lets the rail view load and the hook settle.
const settle = () => act(async () => { await new Promise((resolve) => setTimeout(resolve, 20)) })

describe("useSheetAutoOpen", () => {
  beforeEach(() => {
    window.localStorage.clear()
    resetSnoozeMemory()
    railView.current = { items: [], hidden: 0, hiddenNames: [], snoozeSeconds: undefined }
    peers.current = []
    selection.current = null
    setFocusMode(false)
  })
  afterEach(() => {
    cleanup()
    vi.useRealTimers()
    vi.restoreAllMocks()
  })

  it("opens the sheet on page load when an item waits", async () => {
    railView.current.items = [item("w1", "agent_waiting")]
    page()
    await waitFor(() => expect(sheet()).toBeTruthy())
  })

  it("stays closed with nothing waiting, and opens when an item arrives", async () => {
    page()
    await settle()
    expect(sheet()).toBeNull()
    await arrive(item("e1", "agent_error"))
    await waitFor(() => expect(sheet()).toBeTruthy())
  })

  it("stays closed once closed, also after a reload and when an item that holds no work arrives", async () => {
    railView.current.items = [item("w1", "agent_waiting")]
    page()
    fireEvent.click(await screen.findByRole("button", { name: "Close" }))
    await arrive(item("w1", "agent_waiting"), item("w2", "agent_waiting"), item("d1", "operator_done"))
    await settle()
    expect(sheet()).toBeNull()

    reload()
    await settle()
    expect(sheet()).toBeNull()
  })

  it("opens from the tab at any time during the snooze", async () => {
    railView.current.items = [item("w1", "agent_waiting")]
    page()
    fireEvent.click(await screen.findByRole("button", { name: "Close" }))
    await settle()
    fireEvent.click(screen.getByRole("button", { name: "Needs you" }))
    expect(sheet()).toBeTruthy()
  })

  it("closed by the tab, it snoozes too", async () => {
    railView.current.items = [item("w1", "agent_waiting")]
    page()
    await waitFor(() => expect(sheet()).toBeTruthy())
    fireEvent.click(screen.getByRole("button", { name: "Needs you" }))
    reload()
    await settle()
    expect(sheet()).toBeNull()
  })

  it("opens again when the snooze ends", async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true })
    railView.current.items = [item("w1", "agent_waiting")]
    page()
    fireEvent.click(await screen.findByRole("button", { name: "Close" }))
    await act(async () => { vi.advanceTimersByTime(29 * 60 * 1000) })
    expect(sheet()).toBeNull()
    await act(async () => { vi.advanceTimersByTime(60 * 1000 + 10) })
    await waitFor(() => expect(sheet()).toBeTruthy())
  })

  it("keeps away for as long as web.rail.snooze says", async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true })
    railView.current = { ...railView.current, items: [item("w1", "agent_waiting")], snoozeSeconds: 2 * 60 * 60 }
    page()
    fireEvent.click(await screen.findByRole("button", { name: "Close" }))
    await act(async () => { vi.advanceTimersByTime(90 * 60 * 1000) })
    expect(sheet()).toBeNull()
    await act(async () => { vi.advanceTimersByTime(30 * 60 * 1000 + 10) })
    await waitFor(() => expect(sheet()).toBeTruthy())
  })

  it("opens during the snooze for a new question, approval, plan or gate", async () => {
    for (const signal of ["operator_question", "approval", "plan_decision", "workflow_manual_gate"]) {
      window.localStorage.clear()
      resetSnoozeMemory()
      railView.current.items = [item("w1", "agent_waiting")]
      page()
      fireEvent.click(await screen.findByRole("button", { name: "Close" }))
      await arrive(item("w1", "agent_waiting"), item(`new-${signal}`, signal))
      await waitFor(() => expect(sheet(), signal).toBeTruthy())
      cleanup()
    }
  })

  it("opens during the snooze for a paired instance's new approval", async () => {
    railView.current.items = [item("w1", "agent_waiting")]
    page()
    fireEvent.click(await screen.findByRole("button", { name: "Close" }))
    await settle()
    // The peers' query refetches with the rail's on approvals_changed.
    peers.current = [{ instance_id: "inst-2", id: "cs-1" }]
    await arrive(item("w1", "agent_waiting"), item("w2", "agent_waiting"))
    await waitFor(() => expect(sheet()).toBeTruthy())
  })

  it("does not open by itself on the Attention page", async () => {
    railView.current.items = [item("q1", "operator_question")]
    page({ path: "/attention" })
    await settle()
    expect(sheet()).toBeNull()
  })

  it("does not open by itself in focus mode", async () => {
    setFocusMode(true)
    railView.current.items = [item("q1", "operator_question")]
    page()
    await settle()
    expect(sheet()).toBeNull()
    act(() => setFocusMode(false))
    await waitFor(() => expect(sheet()).toBeTruthy())
  })

  it("waits while the operator types, and opens when the text box loses focus", async () => {
    page()
    await settle()
    const box = screen.getByRole("textbox", { name: "Message" })
    act(() => box.focus())
    await arrive(item("q1", "operator_question"))
    await settle()
    expect(sheet()).toBeNull()
    act(() => box.blur())
    await waitFor(() => expect(sheet()).toBeTruthy())
  })

  it("does not open over another sheet", async () => {
    railView.current.items = [item("q1", "operator_question")]
    page({ blocked: true })
    await settle()
    expect(sheet()).toBeNull()
  })

  it("does not open for items about the agent whose page is open", async () => {
    selection.current = "task-1"
    railView.current.items = [item("q1", "operator_question", "task-1")]
    page({ path: "/agents" })
    await settle()
    expect(sheet()).toBeNull()
    await arrive(item("q1", "operator_question", "task-1"), item("q2", "operator_question", "task-2"))
    await waitFor(() => expect(sheet()).toBeTruthy())
  })

  it("opens without storage, and a close still keeps it away on this page", async () => {
    vi.spyOn(Storage.prototype, "getItem").mockImplementation(() => { throw new Error("blocked") })
    vi.spyOn(Storage.prototype, "setItem").mockImplementation(() => { throw new Error("blocked") })
    railView.current.items = [item("w1", "agent_waiting")]
    page()
    fireEvent.click(await screen.findByRole("button", { name: "Close" }))
    await arrive(item("w1", "agent_waiting"), item("w2", "agent_waiting"))
    await settle()
    expect(sheet()).toBeNull()
  })
})
