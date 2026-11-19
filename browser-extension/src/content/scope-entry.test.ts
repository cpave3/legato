// @vitest-environment jsdom
// @vitest-environment-options {"url":"http://localhost:5173/settings"}
import { afterEach, expect, it, vi } from "vitest"
import { createDraftStore } from "../scope-drafts"
import type { ScopeDraft } from "../scope-types"

type Listener = (message: unknown, sender: unknown, sendResponse: (response: unknown) => void) => boolean | void

afterEach(() => {
  document.documentElement.querySelector("legato-scope")?.remove()
  delete (window as unknown as Record<string, unknown>).__legatoScope
  vi.unstubAllGlobals()
  vi.resetModules()
})

function stubChrome(stored: Record<string, unknown>) {
  const listeners: Listener[] = []
  const changeListeners: Array<(changes: Record<string, unknown>, area: string) => void> = []
  const data = { ...stored }
  // The service worker: draft changes go to the drafts as its handler makes
  // them; anything else answers as a capture.
  const sendMessage = vi.fn<(message: unknown) => Promise<unknown>>(async (message) => {
    const request = message as { type?: string; draft?: ScopeDraft; screenshot?: string; id?: string; comment?: string; ids?: string[] }
    const drafts = createDraftStore(chrome.storage.local, "http://localhost:5173")
    switch (request.type) {
      case "scopeDraftAdd": await drafts.add(request.draft!, request.screenshot); return { ok: true }
      case "scopeDraftUpdate": await drafts.update(request.id!, request.comment!); return { ok: true }
      case "scopeDraftRemove": await drafts.removeMany(request.ids!); return { ok: true }
      default: return { ok: true, screenshot: "data:image/jpeg;base64,AAAA" }
    }
  })
  vi.stubGlobal("chrome", {
    runtime: { id: "", sendMessage, onMessage: { addListener: (listener: Listener) => listeners.push(listener) } },
    storage: {
      local: {
        get: vi.fn(async (keys: string | string[]) => Object.fromEntries((Array.isArray(keys) ? keys : [keys]).filter((k) => k in data).map((k) => [k, data[k]]))),
        set: vi.fn(async (items: Record<string, unknown>) => Object.assign(data, items)),
        remove: vi.fn(async () => undefined),
      },
      onChanged: { addListener: (listener: (changes: Record<string, unknown>, area: string) => void) => changeListeners.push(listener) },
    },
  })
  const send = (message: unknown) => new Promise((resolve) => {
    for (const listener of listeners) listener(message, {}, resolve)
  })
  return { send, listeners, changeListeners, data, sendMessage }
}

it("answers the toolbar button's toggle, even when it comes before the overlay is ready", async () => {
  const { send } = stubChrome({ scopeOrigins: ["http://localhost:5173"] })
  const { bootScope } = await import("./scope")
  const ready = bootScope(document, window)
  const answer = send({ type: "scopeTogglePin", force: true })
  const overlay = await ready
  expect(await answer).toEqual({ ok: true, pinMode: true })
  expect(overlay?.pinMode()).toBe(true)
  expect(await send({ type: "scopeTogglePin" })).toEqual({ ok: true, pinMode: false })
  overlay?.destroy()
})

it("does nothing on an origin that is not one of Scope's", async () => {
  const { send } = stubChrome({ scopeOrigins: ["http://localhost:3000"] })
  const { bootScope } = await import("./scope")
  expect(await bootScope(document, window)).toBeNull()
  expect(await send({ type: "scopeTogglePin" })).toEqual({ ok: false, error: "Scope is not allowed on http://localhost:5173" })
  expect(document.querySelector("legato-scope")).toBeNull()
})

// Chrome's match patterns ignore the port: allowing http://localhost puts
// the script on http://localhost:5173 too, where it boots to nothing until
// that origin is allowed as well.
it("starts in a document it refused once its origin is allowed", async () => {
  const { send, data, listeners } = stubChrome({ scopeOrigins: ["http://localhost:3000"] })
  const { bootScope } = await import("./scope")
  expect(await bootScope(document, window)).toBeNull()
  expect(await send({ type: "scopeTogglePin", force: true })).toEqual({ ok: false, error: "Scope is not allowed on http://localhost:5173" })
  data.scopeOrigins = ["http://localhost:3000", "http://localhost:5173"]
  expect(await send({ type: "scopeTogglePin", force: true })).toEqual({ ok: true, pinMode: true })
  const overlay = await bootScope(document, window)
  expect(overlay?.pinMode()).toBe(true)
  expect(document.querySelectorAll("legato-scope")).toHaveLength(1)
  const toggles = listeners.length
  expect(await send({ type: "scopeTogglePin" })).toEqual({ ok: true, pinMode: false })
  expect(listeners).toHaveLength(toggles)
  overlay?.destroy()
})

it("boots once when the script is put in the page twice", async () => {
  stubChrome({ scopeOrigins: ["http://localhost:5173"] })
  const { bootScope } = await import("./scope")
  const first = await bootScope(document, window)
  const second = await bootScope(document, window)
  expect(second).toBe(first)
  expect(document.querySelectorAll("legato-scope")).toHaveLength(1)
  first?.destroy()
})

it("redraws when another tab changes this origin's drafts", async () => {
  const { changeListeners } = stubChrome({ scopeOrigins: ["http://localhost:5173"] })
  const { bootScope } = await import("./scope")
  const overlay = await bootScope(document, window)
  const refresh = vi.spyOn(overlay!, "refresh")
  for (const listener of changeListeners) listener({ "scopeDrafts:http://localhost:5173": {} }, "local")
  for (const listener of changeListeners) listener({ "scopeDrafts:https://other.test": {} }, "local")
  expect(refresh).toHaveBeenCalledTimes(1)
  overlay?.destroy()
})

it("captures a pin's context and asks the service worker for its screenshot", async () => {
  const { data, sendMessage } = stubChrome({ scopeOrigins: ["http://localhost:5173"] })
  document.body.innerHTML = `<div style="display: flex"><button data-testid="save" class="btn">Save</button></div>`
  const button = document.querySelector("button")!
  button.getBoundingClientRect = () => ({ left: 10, top: 20, width: 30, height: 40, right: 40, bottom: 60, x: 10, y: 20, toJSON: () => ({}) })
  const { bootScope } = await import("./scope")
  const overlay = (await bootScope(document, window))!
  overlay.setPinMode(true)
  button.dispatchEvent(new MouseEvent("click", { bubbles: true, cancelable: true }))
  await vi.waitFor(() => expect(sendMessage).toHaveBeenCalledWith({
    type: "scopeCapture",
    box: { x: 10, y: 20, width: 30, height: 40 },
    viewport: { width: window.innerWidth, height: window.innerHeight },
  }))
  const text = await vi.waitFor(() => {
    const found = overlay.root.querySelector<HTMLTextAreaElement>(".popover textarea")
    if (!found) throw new Error("no popover")
    return found
  })
  text.value = "Bigger"
  text.dispatchEvent(new Event("input"))
  overlay.root.querySelector<HTMLButtonElement>(".popover [data-action='save']")!.click()
  await vi.waitFor(() => expect(data["scopeDrafts:http://localhost:5173"]).toHaveLength(1))
  expect(sendMessage).toHaveBeenCalledWith(expect.objectContaining({ type: "scopeDraftAdd", screenshot: "data:image/jpeg;base64,AAAA" }))
  const [saved] = data["scopeDrafts:http://localhost:5173"] as Array<{ id: string; pin: { target: { selectors: string[] }; styles: { classes: string[]; parent: Record<string, string> } } }>
  expect(saved.pin.target.selectors[0]).toBe('[data-testid="save"]')
  expect(saved.pin.styles.classes).toEqual(["btn"])
  expect(saved.pin.styles.parent.display).toBe("flex")
  expect(data[`scopeShot:http://localhost:5173:${saved.id}`]).toBe("data:image/jpeg;base64,AAAA")
  overlay.destroy()
})

it("keeps a pin whose screenshot Chrome refused, and says why it has none", async () => {
  const { sendMessage } = stubChrome({ scopeOrigins: ["http://localhost:5173"] })
  const capture = sendMessage.getMockImplementation()!
  sendMessage.mockImplementation(async (message) => (message as { type?: string }).type === "scopeCapture"
    ? { ok: false, error: "Chrome did not let Scope take a screenshot." }
    : capture(message))
  document.body.innerHTML = `<button id="save">Save</button>`
  const { bootScope } = await import("./scope")
  const overlay = (await bootScope(document, window))!
  overlay.setPinMode(true)
  document.querySelector("button")!.dispatchEvent(new MouseEvent("click", { bubbles: true, cancelable: true }))
  await vi.waitFor(() => {
    if (!overlay.root.querySelector(".popover textarea")) throw new Error("no popover")
  })
  expect(overlay.root.querySelector(".tray .status")?.textContent).toContain("No screenshot: Chrome did not let Scope take a screenshot.")
  overlay.destroy()
})

it.each([
  { found: { name: "SaveButton", file: "src/App.tsx", line: 7, column: 5, owners: ["App"], confidence: "exact" }, kept: { name: "SaveButton", file: "src/App.tsx", line: 7, column: 5, owners: ["App"], confidence: "exact" } },
  { found: { confidence: "none" }, kept: undefined },
])("keeps the component source the service worker found ($found.confidence)", async ({ found, kept }) => {
  const { data, sendMessage } = stubChrome({ scopeOrigins: ["http://localhost:5173"] })
  const worker = sendMessage.getMockImplementation()!
  sendMessage.mockImplementation(async (message) => (message as { type?: string }).type === "scopeSource"
    ? { ok: true, source: found }
    : worker(message))
  document.body.innerHTML = `<button>Save</button>`
  const { bootScope } = await import("./scope")
  const overlay = (await bootScope(document, window))!
  overlay.setPinMode(true)
  document.querySelector("button")!.dispatchEvent(new MouseEvent("click", { bubbles: true, cancelable: true }))
  const text = await vi.waitFor(() => {
    const found = overlay.root.querySelector<HTMLTextAreaElement>(".popover textarea")
    if (!found) throw new Error("no popover")
    return found
  })
  expect(sendMessage).toHaveBeenCalledWith({ type: "scopeSource" })
  text.value = "Bigger"
  text.dispatchEvent(new Event("input"))
  overlay.root.querySelector<HTMLButtonElement>(".popover [data-action='save']")!.click()
  await vi.waitFor(() => expect(data["scopeDrafts:http://localhost:5173"]).toHaveLength(1))
  const [saved] = data["scopeDrafts:http://localhost:5173"] as Array<{ pin: { source?: unknown } }>
  expect(saved.pin.source).toEqual(kept)
  overlay.destroy()
})

it("watches the site's sent pins and shows those the service worker reports", async () => {
  const { send, sendMessage } = stubChrome({ scopeOrigins: ["http://localhost:5173"] })
  document.body.innerHTML = `<button id="save">Save</button>`
  const { bootScope } = await import("./scope")
  const overlay = (await bootScope(document, window))!
  await vi.waitFor(() => expect(sendMessage).toHaveBeenCalledWith({ type: "scopeWatch" }))
  const pin = {
    id: "sp-1", batch_id: "sb-1", origin: "http://localhost:5173", url: "http://localhost:5173/settings", path: "/settings",
    comment: "Too much padding", state: "fixed", note: "Reduced", selectors: ["#save"], box: { x: 0, y: 0, width: 1, height: 1 },
    has_screenshot: false, created_at: "", updated_at: "",
  }
  void send({ type: "scopePinsChanged", origin: "https://other.test", pins: [pin] })
  void send({ type: "scopePinsChanged", origin: "http://localhost:5173", pins: [pin] })
  await vi.waitFor(() => expect(overlay.root.querySelectorAll(".marker[data-state='fixed']")).toHaveLength(1))
  overlay.destroy()
})
