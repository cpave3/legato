// @vitest-environment jsdom
// @vitest-environment-options {"url":"http://localhost:5173/settings"}
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { createClearedStore, createDraftStore, memoryArea } from "../scope-drafts"
import type { ScopeBatchResult, ScopeDraft, ScopePin, SentPin } from "../scope-types"
import { createScopeOverlay, type ScopeOverlay } from "./scope-overlay"

const origin = "http://localhost:5173"
let overlay: ScopeOverlay | undefined

function draft(id: string, comment: string, screenshot = false): ScopeDraft {
  return {
    id, createdAt: "2026-11-19T10:00:00Z", hasScreenshot: screenshot,
    pin: {
      comment,
      page: { url: `${origin}/settings`, title: "Settings", viewport: { width: 1280, height: 800 }, scroll: { x: 0, y: 0 }, dpr: 1 },
      target: { selectors: ["#save"], text: "Save", box: { x: 100, y: 400, width: 80, height: 30 } },
      styles: { computed: {}, classes: [], inline: "", parent: {} },
    },
  }
}

function sent(id: string, state: SentPin["state"], extra: Partial<SentPin> = {}): SentPin {
  return {
    id, batch_id: "sb-1", origin, url: `${origin}/settings`, path: "/settings", comment: `Comment ${id}`, state,
    selectors: ["#save"], text: "Save", box: { x: 100, y: 400, width: 80, height: 30 }, has_screenshot: false,
    created_at: "2026-11-19T10:00:00Z", updated_at: "2026-11-19T11:00:00Z", ...extra,
  }
}

function setup(drafts: ScopeDraft[] = [], result: ScopeBatchResult = { batch_id: "sb-1", pin_ids: ["sp-1"], subscribed: true, delivered: true }) {
  const area = memoryArea({ [`scopeDrafts:${origin}`]: drafts })
  const store = createDraftStore(area, origin)
  const cleared = createClearedStore(area, origin)
  const actions = {
    send: vi.fn<(pins: ScopePin[]) => Promise<ScopeBatchResult>>(async () => result),
    reopen: vi.fn(async () => undefined),
    withdraw: vi.fn(async () => undefined),
    cleared,
  }
  overlay = createScopeOverlay(document, window, {
    store,
    capture: vi.fn(),
    locate: (target) => document.querySelector(target.selectors[0]),
    newId: () => "d1",
    now: () => "2026-11-19T12:00:00Z",
    sent: actions,
  })
  return { overlay, root: overlay.root, store, area, actions, cleared }
}

const tray = (root: ShadowRoot) => root.querySelector<HTMLElement>(".tray")!
const status = (root: ShadowRoot) => root.querySelector<HTMLElement>(".tray .status")!

beforeEach(() => { document.body.innerHTML = `<button id="save">Save</button>` })
afterEach(() => { overlay?.destroy(); overlay = undefined })

describe("sending drafts", () => {
  it("posts the drafts as one batch, with their screenshots, and clears them once Legato accepts", async () => {
    const { overlay, root, store, actions } = setup([draft("a", "Too much padding", true), draft("b", "Wrong colour")])
    await store.add(draft("c", "Bigger"), "data:image/jpeg;base64,AAAA")
    await overlay.refresh()
    const send = root.querySelector<HTMLButtonElement>(".tray [data-action='send']")!
    expect(send.textContent).toBe("Send 3 pins")
    send.click()
    await vi.waitFor(async () => expect(await store.list()).toEqual([]))
    const [pins] = actions.send.mock.calls[0]
    expect(pins.map((pin) => pin.comment)).toEqual(["Too much padding", "Wrong colour", "Bigger"])
    expect(pins[1].screenshot).toBeUndefined()
    expect(pins[2].screenshot).toBe("data:image/jpeg;base64,AAAA")
    await vi.waitFor(() => expect(status(root).textContent).toContain("Sent 3 pins"))
  })

  it("sends a comment edited in the panel, though its save is still under way", async () => {
    const { overlay, root, store, actions } = setup([draft("a", "Old comment")])
    const update = store.update
    store.update = (id, comment) => new Promise((resolve) => setTimeout(() => resolve(update(id, comment)), 50))
    await overlay.refresh()
    const text = root.querySelector<HTMLTextAreaElement>(".tray .draft textarea")!
    text.value = "New comment"
    text.dispatchEvent(new Event("change"))
    root.querySelector<HTMLButtonElement>(".tray [data-action='send']")!.click()
    await vi.waitFor(() => expect(actions.send).toHaveBeenCalled())
    expect(actions.send.mock.calls[0][0].map((pin) => pin.comment)).toEqual(["New comment"])
  })

  it("sends what is typed in the panel before it is saved", async () => {
    const { overlay, root, actions } = setup([draft("a", "Old comment")])
    await overlay.refresh()
    const text = root.querySelector<HTMLTextAreaElement>(".tray .draft textarea")!
    text.focus()
    text.value = "Typed"
    root.querySelector<HTMLButtonElement>(".tray [data-action='send']")!.click()
    await vi.waitFor(() => expect(actions.send).toHaveBeenCalled())
    expect(actions.send.mock.calls[0][0].map((pin) => pin.comment)).toEqual(["Typed"])
  })

  it("says the pins wait in Legato's pool when no agent has subscribed to the site", async () => {
    const { overlay, root } = setup([draft("a", "Too much padding")], { batch_id: "sb-1", pin_ids: ["sp-1"], subscribed: false, delivered: false })
    await overlay.refresh()
    root.querySelector<HTMLButtonElement>(".tray [data-action='send']")!.click()
    await vi.waitFor(() => expect(status(root).textContent).toContain(`wait in Legato's pool until an agent subscribes to ${origin}`))
  })

  it("sends at most 20 at a time and keeps the rest", async () => {
    const drafts = Array.from({ length: 22 }, (_, index) => draft(`d${index}`, `Comment ${index}`))
    const { overlay, root, store, actions } = setup(drafts)
    await overlay.refresh()
    root.querySelector<HTMLButtonElement>(".tray [data-action='send']")!.click()
    await vi.waitFor(async () => expect((await store.list()).map((d) => d.id)).toEqual(["d20", "d21"]))
    expect(actions.send.mock.calls[0][0]).toHaveLength(20)
    await vi.waitFor(() => expect(status(root).textContent).toContain("2 drafts left"))
  })

  it("keeps the drafts and shows the error when Legato refuses", async () => {
    const { overlay, root, store, actions } = setup([draft("a", "Too much padding")])
    actions.send.mockRejectedValueOnce(new Error("Could not reach Legato at http://localhost:3080. Check that Legato is running"))
    await overlay.refresh()
    root.querySelector<HTMLButtonElement>(".tray [data-action='send']")!.click()
    await vi.waitFor(() => expect(status(root).dataset.kind).toBe("error"))
    expect(status(root).textContent).toContain("Could not reach Legato")
    expect(await store.list()).toHaveLength(1)
  })
})

describe("sent pins on the page", () => {
  it("shows each sent pin of this page with its state and the agent's note, and lists them in the tray", async () => {
    const { overlay, root } = setup()
    overlay.setSent([sent("sp-1", "fixed", { note: "Reduced to 8px" }), sent("sp-2", "delivered"), sent("sp-3", "sent", { url: `${origin}/billing`, path: "/billing" })])
    await vi.waitFor(() => expect(root.querySelectorAll(".marker[data-state]")).toHaveLength(2))
    expect([...root.querySelectorAll<HTMLElement>(".marker[data-state]")].map((m) => m.dataset.state)).toEqual(["fixed", "delivered"])
    const rows = [...root.querySelectorAll(".tray .sent")]
    expect(rows).toHaveLength(3)
    expect(rows[0].textContent).toContain("Fixed")
    expect(rows[0].textContent).toContain("Reduced to 8px")
    expect(tray(root).dataset.visible).toBe("true")
  })

  it("reopens a resolved pin with a new comment", async () => {
    const { overlay, root, actions } = setup()
    overlay.setSent([sent("sp-1", "wont_fix")])
    await vi.waitFor(() => expect(root.querySelector(".marker[data-state]")).not.toBeNull())
    root.querySelector<HTMLButtonElement>(".marker[data-state]")!.click()
    const text = root.querySelector<HTMLTextAreaElement>(".popover textarea")!
    text.value = "It is still clipped"
    text.dispatchEvent(new Event("input"))
    root.querySelector<HTMLButtonElement>(".popover [data-action='reopen']")!.click()
    await vi.waitFor(() => expect(actions.reopen).toHaveBeenCalledWith("sp-1", "It is still clipped"))
  })

  it("withdraws a pin no agent has taken, and offers no withdraw once delivered", async () => {
    const { overlay, root, actions } = setup()
    overlay.setSent([sent("sp-1", "sent"), sent("sp-2", "delivered")])
    await vi.waitFor(() => expect(root.querySelectorAll(".tray .sent")).toHaveLength(2))
    const [first, second] = [...root.querySelectorAll(".tray .sent")]
    expect(second.querySelector("[data-action='withdraw']")).toBeNull()
    first.querySelector<HTMLButtonElement>("[data-action='withdraw']")!.click()
    await vi.waitFor(() => expect(actions.withdraw).toHaveBeenCalledWith("sp-1"))
  })

  it("clears resolved pins from the page and remembers that across reloads", async () => {
    const { overlay, root, cleared } = setup()
    overlay.setSent([sent("sp-1", "fixed"), sent("sp-2", "wont_fix"), sent("sp-3", "delivered")])
    await vi.waitFor(() => expect(root.querySelectorAll(".tray .sent")).toHaveLength(3))
    root.querySelector<HTMLButtonElement>(".tray [data-action='clear-resolved']")!.click()
    await vi.waitFor(() => expect(root.querySelectorAll(".tray .sent")).toHaveLength(1))
    await vi.waitFor(async () => expect((await cleared.list()).sort()).toEqual(["sp-1", "sp-2"]))
    expect(root.querySelectorAll(".marker[data-state]")).toHaveLength(1)
  })

  it("shows a pin again when it is reopened after being cleared", async () => {
    const { overlay, root, cleared } = setup()
    await cleared.add(["sp-1"])
    overlay.setSent([sent("sp-1", "sent")])
    await vi.waitFor(() => expect(root.querySelectorAll(".tray .sent")).toHaveLength(1))
  })

  it("shows an error reading the sent pins in the tray", async () => {
    const { overlay, root } = setup()
    overlay.setSentError("Legato refused the extension's token")
    expect(status(root).textContent).toBe("Legato refused the extension's token")
    expect(status(root).dataset.kind).toBe("error")
    // It does not open the tray on a page with nothing to send.
    expect(tray(root).dataset.visible).toBe("false")
  })
})
