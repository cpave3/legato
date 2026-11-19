// @vitest-environment jsdom
// @vitest-environment-options {"url":"http://localhost:5173/settings"}
import { afterEach, expect, it, vi } from "vitest"

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
  const sendMessage = vi.fn<(message: unknown) => Promise<unknown>>(async () => ({ ok: true, screenshot: "data:image/jpeg;base64,AAAA" }))
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
  const [saved] = data["scopeDrafts:http://localhost:5173"] as Array<{ id: string; pin: { target: { selectors: string[] }; styles: { classes: string[]; parent: Record<string, string> } } }>
  expect(saved.pin.target.selectors[0]).toBe('[data-testid="save"]')
  expect(saved.pin.styles.classes).toEqual(["btn"])
  expect(saved.pin.styles.parent.display).toBe("flex")
  expect(data[`scopeShot:http://localhost:5173:${saved.id}`]).toBe("data:image/jpeg;base64,AAAA")
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
