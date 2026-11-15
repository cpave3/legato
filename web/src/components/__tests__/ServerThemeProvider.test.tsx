import { afterEach, expect, it, vi } from "vitest"
import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react"
import { QueryClientProvider } from "@tanstack/react-query"
import { ServerProvider } from "../ServerProvider"
import { ServerThemeProvider } from "../ServerThemeProvider"
import { QuerySyncProvider } from "../QuerySyncProvider"
import { AppThemePicker } from "../settings/AppThemePicker"
import { useServer } from "../../hooks/useServer"
import { createQueryClient } from "../../lib/query"
import { applyAppTheme } from "../../lib/appTheme"
import type { WSMessage } from "../../hooks/useWebSocket"

const socket = vi.hoisted(() => ({ generation: 1, handlers: new Set<(message: WSMessage) => void>(), subscribe: (handler: (message: WSMessage) => void) => { socket.handlers.add(handler); return () => { socket.handlers.delete(handler) } } }))
vi.mock("../../hooks/useWebSocket", () => ({ useWebSocket: () => ({ connectionGeneration: socket.generation, subscribe: socket.subscribe }) }))

function Switcher() {
  const { setActiveServer, removeServer } = useServer()
  return <><button onClick={() => setActiveServer("https://second.test")}>Second server</button><button onClick={() => setActiveServer("")}>Origin server</button><button onClick={() => removeServer("https://second.test")}>Remove second</button></>
}
function Page() {
  return <ServerProvider><QueryClientProvider client={client}><QuerySyncProvider><ServerThemeProvider><Switcher /><AppThemePicker /></ServerThemeProvider></QuerySyncProvider></QueryClientProvider></ServerProvider>
}
let client = createQueryClient()
afterEach(() => { cleanup(); client.clear(); client = createQueryClient(); socket.handlers.clear(); socket.generation = 1; vi.restoreAllMocks(); vi.unstubAllGlobals(); localStorage.clear(); delete (window as { legatoShell?: unknown }).legatoShell; applyAppTheme() })

it("loads metadata at the mounted entrypoint, isolates active switches and late responses, and retains offline state on reload", async () => {
  localStorage.setItem("legato:app-theme", "dracula")
  localStorage.setItem("legato:theme-mode", "server")
  localStorage.setItem("legato:server-theme-cache", JSON.stringify({ [window.location.origin]: { id: "ocular-dark", revision: 1 } }))
  applyAppTheme()
  expect(document.documentElement.dataset.theme).toBe("ocular-dark")
  let answerOrigin!: (response: Response) => void
  let answerSecond!: (response: Response) => void
  vi.stubGlobal("fetch", vi.fn((url: string) => new Promise<Response>(resolve => {
    if (url.startsWith("https://second.test")) answerSecond = resolve
    else answerOrigin = resolve
  })))
  render(<Page />)
  expect(document.documentElement.dataset.theme).toBe("ocular-dark")
  fireEvent.click(screen.getByText("Second server"))
  expect(document.documentElement.dataset.theme).toBe("dracula")
  await act(async () => { answerSecond(Response.json({ theme_preference: { id: "ocular-light", revision: 2 } })) })
  await waitFor(() => expect(document.documentElement.dataset.theme).toBe("ocular-light"))
  await act(async () => { answerOrigin(Response.json({ theme_preference: { id: "ocular-dark", revision: 99 } })) })
  expect(document.documentElement.dataset.theme).toBe("ocular-light")
  cleanup(); client.clear()
  vi.stubGlobal("fetch", vi.fn().mockRejectedValue(new Error("offline")))
  // This is the same pre-React path called by main.tsx.
  applyAppTheme()
  expect(document.documentElement.dataset.theme).toBe("ocular-light")
  render(<Page />)
  await waitFor(() => expect(fetch).toHaveBeenCalled())
  expect(document.documentElement.dataset.theme).toBe("ocular-light")
})

it("refetches on server-theme events and reconnect, falls back for older/unsupported/empty preferences, and ignores the server in local mode", async () => {
  localStorage.setItem("legato:app-theme", "dracula")
  localStorage.setItem("legato:theme-mode", "server")
  let preference: { id: string; revision: number } | undefined = { id: "ocular-light", revision: 1 }
  const request = vi.fn(async () => Response.json({ theme_preference: preference }))
  vi.stubGlobal("fetch", request)
  const page = render(<Page />)
  await waitFor(() => expect(document.documentElement.dataset.theme).toBe("ocular-light"))
  preference = { id: "ocular-dark", revision: 2 }
  act(() => socket.handlers.forEach(handler => handler({ type: "server_theme_changed" })))
  await waitFor(() => expect(document.documentElement.dataset.theme).toBe("ocular-dark"))
  preference = { id: "", revision: 3 }
  socket.generation++
  page.rerender(<Page />)
  await waitFor(() => expect(document.documentElement.dataset.theme).toBe("dracula"))
  let revision = 3
  for (const id of [undefined, "future-theme"]) {
    preference = { id: "ocular-light", revision: ++revision }
    act(() => socket.handlers.forEach(handler => handler({ type: "server_theme_changed" })))
    await waitFor(() => expect(document.documentElement.dataset.theme).toBe("ocular-light"))
    preference = id === undefined ? undefined : { id, revision: ++revision }
    act(() => socket.handlers.forEach(handler => handler({ type: "server_theme_changed" })))
    await waitFor(() => expect(document.documentElement.dataset.theme).toBe("dracula"))
  }
  fireEvent.click(screen.getByRole("radio", { name: "Ocular Light" }))
  await waitFor(() => expect(document.documentElement.dataset.theme).toBe("ocular-light"))
  preference = { id: "ocular-dark", revision: ++revision }
  act(() => socket.handlers.forEach(handler => handler({ type: "server_theme_changed" })))
  await waitFor(() => expect(localStorage.getItem("legato:server-theme-cache")).toContain(`"revision":${revision}`))
  expect(document.documentElement.dataset.theme).toBe("ocular-light")
})

it("does not independently resolve the server in a native-owned page", async () => {
  ;(window as { legatoShell?: unknown }).legatoShell = { theme: 2, key: "native" }
  localStorage.setItem("legato:app-theme", "dracula")
  localStorage.setItem("legato:theme-mode", "server")
  localStorage.setItem("legato:effective-app-theme", "ocular-dark")
  vi.stubGlobal("fetch", vi.fn(async () => Response.json({ theme_preference: { id: "ocular-light", revision: 1 } })))
  render(<Page />)
  await waitFor(() => expect(localStorage.getItem("legato:server-theme-cache")).toContain('"id":"ocular-light"'))
  expect(document.documentElement.dataset.theme).toBe("ocular-dark")
  expect(localStorage.getItem("legato:app-theme")).toBe("dracula")
})

it("uses live metadata even when offline-cache storage is full", async () => {
  localStorage.setItem("legato:app-theme", "dracula")
  localStorage.setItem("legato:theme-mode", "server")
  vi.spyOn(Storage.prototype, "setItem").mockImplementation(() => { throw new Error("Quota exceeded") })
  vi.stubGlobal("fetch", vi.fn(async () => Response.json({ theme_preference: { id: "ocular-light", revision: 1 } })))
  render(<Page />)
  await waitFor(() => expect(document.documentElement.dataset.theme).toBe("ocular-light"))
})

it("keeps this tab's active server when another tab changes the saved startup selection", async () => {
  localStorage.setItem("legato:theme-mode", "server")
  vi.stubGlobal("fetch", vi.fn(async (url: string) => Response.json({ theme_preference: {
    id: url.startsWith("https://second.test") ? "ocular-dark" : "ocular-light", revision: 1,
  } })))
  render(<Page />)
  await waitFor(() => expect(document.documentElement.dataset.theme).toBe("ocular-light"))
  fireEvent.click(screen.getByText("Second server"))
  await waitFor(() => expect(document.documentElement.dataset.theme).toBe("ocular-dark"))
  fireEvent.click(screen.getByText("Origin server"))
  await waitFor(() => expect(document.documentElement.dataset.theme).toBe("ocular-light"))
  localStorage.setItem("legato:active-server", "https://second.test")
  act(() => window.dispatchEvent(new StorageEvent("storage", { key: "legato:active-server", newValue: "https://second.test" })))
  expect(document.documentElement.dataset.theme).toBe("ocular-light")
})

it("forgets removed connection snapshots before adding the same address offline", async () => {
  localStorage.setItem("legato:app-theme", "dracula")
  localStorage.setItem("legato:theme-mode", "server")
  localStorage.setItem("legato:servers", JSON.stringify([{ name: "Second", url: "https://second.test" }]))
  vi.stubGlobal("fetch", vi.fn(async (url: string) => Response.json({ theme_preference: {
    id: url.startsWith("https://second.test") ? "ocular-light" : "", revision: 1,
  } })))
  render(<Page />)
  fireEvent.click(screen.getByText("Second server"))
  await waitFor(() => expect(document.documentElement.dataset.theme).toBe("ocular-light"))
  vi.stubGlobal("fetch", vi.fn().mockRejectedValue(new Error("offline")))
  fireEvent.click(screen.getByText("Remove second"))
  await waitFor(() => expect(document.documentElement.dataset.theme).toBe("dracula"))
  fireEvent.click(screen.getByText("Second server"))
  await waitFor(() => expect(document.documentElement.dataset.theme).toBe("dracula"))
})
