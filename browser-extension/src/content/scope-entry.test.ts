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
  vi.stubGlobal("chrome", {
    runtime: { id: "", sendMessage: vi.fn(), onMessage: { addListener: (listener: Listener) => listeners.push(listener) } },
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
  return { send, listeners, changeListeners, data }
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
