// @vitest-environment jsdom
// @vitest-environment-options {"url":"http://localhost:5173/settings"}
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { createDraftStore, memoryArea } from "../scope-drafts"
import type { PinTarget, ScopeDraft } from "../scope-types"
import { createScopeOverlay, type CapturedPin, type ScopeOverlay, type ScopeOverlayDeps } from "./scope-overlay"

const origin = "http://localhost:5173"
let overlay: ScopeOverlay | undefined

function draft(id: string, comment: string, path = "/settings", selectors = ["#save"]): ScopeDraft {
  return {
    id,
    createdAt: "2026-11-19T10:00:00Z",
    hasScreenshot: false,
    pin: {
      comment,
      page: { url: `${origin}${path}`, title: "Settings", viewport: { width: 1280, height: 800 }, scroll: { x: 0, y: 0 }, dpr: 1 },
      target: { selectors, text: "Save", box: { x: 100, y: 400, width: 80, height: 30 } },
      styles: { computed: {}, classes: [], inline: "", parent: {} },
    },
  }
}

function setup(stored: ScopeDraft[] = []) {
  const area = memoryArea({ [`scopeDrafts:${origin}`]: stored })
  const store = createDraftStore(area, origin)
  let ids = 0
  const deps = {
    store,
    capture: vi.fn(async (element: Element): Promise<CapturedPin> => ({
      pin: {
        page: { url: location.href, title: document.title, viewport: { width: 1280, height: 800 }, scroll: { x: 0, y: 0 }, dpr: 1 },
        target: { selectors: [`#${element.id}`], text: element.textContent ?? "", box: { x: 1, y: 2, width: 3, height: 4 } },
        styles: { computed: {}, classes: [], inline: "", parent: {} },
      },
      screenshot: "data:image/jpeg;base64,AAAA",
    })),
    locate: vi.fn((target: PinTarget) => document.querySelector(target.selectors[0])),
    newId: () => `d${++ids}`,
    now: () => "2026-11-19T12:00:00Z",
  } satisfies ScopeOverlayDeps
  overlay = createScopeOverlay(document, window, deps)
  return { deps, store, overlay, root: overlay.root }
}

const flush = () => new Promise((resolve) => setTimeout(resolve, 0))

beforeEach(() => {
  document.body.innerHTML = `<main><button id="save">Save</button><p id="intro">Hello</p></main>`
})
afterEach(() => {
  overlay?.destroy()
  overlay = undefined
})

describe("pin mode", () => {
  it("keeps the page's own handlers from hearing presses and clicks", async () => {
    const { overlay } = setup()
    const page = vi.fn()
    for (const type of ["pointerdown", "mousedown", "mouseup", "click"]) document.querySelector("#save")!.addEventListener(type, page)
    document.addEventListener("click", page, { capture: true })
    overlay.setPinMode(true)
    for (const type of ["pointerdown", "mousedown", "mouseup", "click"]) {
      const event = new MouseEvent(type, { bubbles: true, cancelable: true, composed: true })
      document.querySelector("#save")!.dispatchEvent(event)
      expect(event.defaultPrevented).toBe(true)
    }
    expect(page).not.toHaveBeenCalled()
    await flush()
  })

  it("places a numbered marker on the clicked element and stores the draft when the comment is saved", async () => {
    const { overlay, root, store, deps } = setup()
    overlay.setPinMode(true)
    document.querySelector("#save")!.dispatchEvent(new MouseEvent("click", { bubbles: true, cancelable: true }))
    await vi.waitFor(() => expect(root.querySelector(".popover textarea")).not.toBeNull())
    expect(deps.capture).toHaveBeenCalledWith(document.querySelector("#save"))
    expect(root.querySelector(".marker")?.textContent).toBe("1")
    const text = root.querySelector<HTMLTextAreaElement>(".popover textarea")!
    text.value = "Too much padding"
    text.dispatchEvent(new Event("input"))
    root.querySelector<HTMLButtonElement>(".popover [data-action='save']")!.click()
    await vi.waitFor(async () => expect(await store.list()).toHaveLength(1))
    const [saved] = await store.list()
    expect(saved).toMatchObject({ id: "d1", createdAt: "2026-11-19T12:00:00Z", hasScreenshot: true, pin: { comment: "Too much padding", target: { selectors: ["#save"] } } })
    expect(await store.screenshot("d1")).toBe("data:image/jpeg;base64,AAAA")
    expect(root.querySelector(".popover")).toBeNull()
  })

  it("will not save a pin without a comment, and Cancel drops it", async () => {
    const { overlay, root, store } = setup()
    overlay.setPinMode(true)
    document.querySelector("#save")!.dispatchEvent(new MouseEvent("click", { bubbles: true, cancelable: true }))
    await vi.waitFor(() => expect(root.querySelector(".popover")).not.toBeNull())
    expect(root.querySelector<HTMLButtonElement>(".popover [data-action='save']")!.disabled).toBe(true)
    root.querySelector<HTMLButtonElement>(".popover [data-action='cancel']")!.click()
    await flush()
    expect(root.querySelector(".popover")).toBeNull()
    expect(root.querySelector(".marker")).toBeNull()
    expect(await store.list()).toEqual([])
  })

  it("hides the overlay while the pin's context is captured", async () => {
    const { overlay, deps } = setup()
    let visibility = ""
    deps.capture.mockImplementationOnce(async () => {
      visibility = overlay.host.style.visibility
      return { pin: draft("x", "").pin, screenshot: "" }
    })
    overlay.setPinMode(true)
    document.querySelector("#intro")!.dispatchEvent(new MouseEvent("click", { bubbles: true, cancelable: true }))
    await vi.waitFor(() => expect(deps.capture).toHaveBeenCalled())
    await flush()
    expect(visibility).toBe("hidden")
    expect(overlay.host.style.visibility).toBe("")
  })

  it("outlines the element under the pointer", () => {
    const { overlay, root } = setup()
    overlay.setPinMode(true)
    document.querySelector("#intro")!.dispatchEvent(new MouseEvent("pointermove", { bubbles: true, composed: true }))
    expect(root.querySelector<HTMLElement>(".outline")!.style.display).toBe("block")
  })

  it("leaves on Escape, after which the page hears clicks again", () => {
    const { overlay } = setup()
    const page = vi.fn()
    document.querySelector("#save")!.addEventListener("click", page)
    overlay.setPinMode(true)
    window.dispatchEvent(new KeyboardEvent("keydown", { key: "Escape" }))
    expect(overlay.pinMode()).toBe(false)
    document.querySelector<HTMLButtonElement>("#save")!.click()
    expect(page).toHaveBeenCalledOnce()
  })

  it("toggles, and lets clicks on its own tray through", async () => {
    const { overlay, root } = setup()
    expect(overlay.setPinMode()).toBe(true)
    await flush()
    const pin = root.querySelector<HTMLButtonElement>(".tray [data-action='pin-mode']")!
    pin.click()
    expect(overlay.pinMode()).toBe(false)
    expect(overlay.setPinMode()).toBe(true)
    expect(overlay.setPinMode(true)).toBe(true)
  })
})

describe("drafts on the page", () => {
  it("shows a marker for each draft of this page, and lists every draft of the site in the tray", async () => {
    const { root, overlay } = setup([draft("a", "Too much padding"), draft("b", "Wrong colour", "/billing"), draft("c", "Bigger", "/settings", ["#intro"])])
    await overlay.refresh()
    expect([...root.querySelectorAll(".marker")].map((m) => m.textContent)).toEqual(["1", "3"])
    const rows = [...root.querySelectorAll(".tray .draft")]
    expect(rows.map((row) => row.querySelector(".page")?.textContent)).toEqual(["/settings", "/billing", "/settings"])
    expect(root.querySelector(".tray")?.textContent).toContain("3 drafts")
  })

  it("edits and deletes drafts from the tray", async () => {
    const { root, overlay, store } = setup([draft("a", "Too much padding"), draft("b", "Wrong colour", "/billing")])
    await overlay.refresh()
    const text = root.querySelector<HTMLTextAreaElement>(".tray .draft textarea")!
    text.value = "Less padding"
    text.dispatchEvent(new Event("change"))
    await vi.waitFor(async () => expect((await store.list())[0].pin.comment).toBe("Less padding"))
    root.querySelectorAll<HTMLButtonElement>(".tray .draft [data-action='delete']")[1].click()
    await vi.waitFor(async () => expect((await store.list()).map((d) => d.id)).toEqual(["a"]))
    await vi.waitFor(() => expect(root.querySelectorAll(".tray .draft")).toHaveLength(1))
  })

  it("collapses the tray", async () => {
    const { root, overlay } = setup([draft("a", "Too much padding")])
    await overlay.refresh()
    root.querySelector<HTMLButtonElement>(".tray [data-action='collapse']")!.click()
    expect(root.querySelector<HTMLElement>(".tray")!.dataset.collapsed).toBe("true")
  })

  it("opens a draft's marker to edit or delete it", async () => {
    const { root, overlay, store } = setup([draft("a", "Too much padding")])
    await overlay.refresh()
    root.querySelector<HTMLButtonElement>(".marker")!.click()
    expect(root.querySelector<HTMLTextAreaElement>(".popover textarea")!.value).toBe("Too much padding")
    root.querySelector<HTMLButtonElement>(".popover [data-action='delete']")!.click()
    await vi.waitFor(async () => expect(await store.list()).toEqual([]))
  })

  it("keeps a marker on its element as the page scrolls, and at its saved place, marked lost, when the element is gone", async () => {
    const { root, overlay } = setup([draft("a", "On save"), draft("b", "Gone", "/settings", ["#missing"])])
    let top = 300
    document.querySelector("#save")!.getBoundingClientRect = () => ({ left: 50, top, width: 80, height: 30, right: 130, bottom: top + 30, x: 50, y: top, toJSON: () => ({}) })
    await overlay.refresh()
    const [onSave, gone] = [...root.querySelectorAll<HTMLElement>(".marker")]
    expect(onSave.style.top).toBe("290px")
    expect(onSave.style.left).toBe("40px")
    expect(gone.dataset.lost).toBe("true")
    expect(gone.style.top).toBe("390px")
    top = 100
    window.dispatchEvent(new Event("scroll"))
    await vi.waitFor(() => expect(onSave.style.top).toBe("90px"))
  })
})

describe("a page that changes", () => {
  it("finds a lost marker's element when it appears", async () => {
    document.querySelector("#save")!.remove()
    const { root, overlay } = setup([draft("a", "On save")])
    await overlay.refresh()
    expect(root.querySelector<HTMLElement>(".marker")!.dataset.lost).toBe("true")
    document.body.insertAdjacentHTML("beforeend", `<button id="save">Save</button>`)
    overlay.relocate()
    expect(root.querySelector<HTMLElement>(".marker")!.dataset.lost).toBe("false")
  })
})
