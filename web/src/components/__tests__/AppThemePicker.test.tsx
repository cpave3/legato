import { afterEach, expect, it, vi } from "vitest"
import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react"
import { AppThemePicker } from "../settings/AppThemePicker"
import { applyAppTheme, appTheme, APP_THEME_KEY } from "../../lib/appTheme"
import { terminalTheme, TERMINAL_THEME_KEY } from "../../lib/terminalTheme"

afterEach(() => { cleanup(); vi.useRealTimers(); vi.restoreAllMocks(); localStorage.clear(); applyAppTheme(); delete (window as { legatoShell?: unknown }).legatoShell; delete window.ReactNativeWebView })

it.each([
  { name: "Ocular Light", id: "ocular-light", canvas: "#F5F3ED", polarity: "light", accent: "#68418A", green: "#235D45" },
  { name: "Ocular Dark", id: "ocular-dark", canvas: "#20252B", polarity: "dark", accent: "#C5ADE3", green: "#A3C9A8" },
  { name: "Ocular Light Vivid", id: "ocular-light-vivid", canvas: "#F5F3ED", polarity: "light", accent: "#7533A8", green: "#167347" },
  { name: "Ocular Dark Vivid", id: "ocular-dark-vivid", canvas: "#20252B", polarity: "dark", accent: "#CFABFF", green: "#61DD8A" },
])("selects $name, persists on reload and follows in terminals without overwriting a saved terminal choice", async ({ name, id, canvas, polarity, accent, green }) => {
  localStorage.setItem(TERMINAL_THEME_KEY, "night")
  render(<AppThemePicker />)
  fireEvent.click(screen.getByRole("radio", { name }))
  await waitFor(() => expect(document.documentElement.dataset.theme).toBe(id))
  const style = document.documentElement.style
  expect(style.getPropertyValue("--color-lw-ground")).toBe(canvas)
  expect(style.getPropertyValue("--color-lw-accent")).toBe(accent)
  expect(style.getPropertyValue("--color-lw-live")).toBe(green)
  expect(style.getPropertyValue("--theme-focus")).toBe(accent)
  expect(style.getPropertyValue("--color-lw-accent-soft")).not.toBe(style.getPropertyValue("--color-lw-selected"))
  expect(style.colorScheme).toBe(polarity)
  expect(localStorage.getItem(APP_THEME_KEY)).toBe(id)
  expect(terminalTheme().id).toBe("night")
  localStorage.removeItem(TERMINAL_THEME_KEY)
  expect(terminalTheme().theme.background).toBe(canvas)
  expect(terminalTheme().theme.green).toBe(green)
  cleanup()
  applyAppTheme()
  render(<AppThemePicker />)
  expect(appTheme().id).toBe(id)
  expect(screen.getByRole("radio", { name }).getAttribute("aria-checked")).toBe("true")
})

it("asks a capable native shell to persist before changing the page, and surfaces a refused save", async () => {
  ;(window as { legatoShell?: unknown }).legatoShell = { theme: 1, key: "page-key" }
  const messages: string[] = []
  window.ReactNativeWebView = { postMessage: message => { messages.push(message) } }
  render(<AppThemePicker />)
  fireEvent.click(screen.getByRole("radio", { name: "Ocular Dark" }))
  expect(JSON.parse(messages[0])).toEqual({ legatoShell: "theme", type: "theme", key: "page-key", id: "ocular-dark" })
  expect(localStorage.getItem(APP_THEME_KEY)).toBeNull()
  await act(async () => {
    window.dispatchEvent(new CustomEvent("legato:shell-theme", { detail: { id: "ocular-dark", error: "Could not save app theme" } }))
  })
  expect(screen.getByRole("alert").textContent).toContain("Could not save app theme")
  expect(appTheme().id).toBe("legato")
  fireEvent.click(screen.getByRole("radio", { name: "Ocular Dark" }))
  await act(async () => {
    localStorage.setItem(APP_THEME_KEY, "ocular-dark")
    window.dispatchEvent(new Event("legato:theme-changed"))
    window.dispatchEvent(new CustomEvent("legato:shell-theme", { detail: { id: "ocular-dark" } }))
  })
  await waitFor(() => expect(screen.getByRole("radio", { name: "Ocular Dark" }).getAttribute("aria-checked")).toBe("true"))
  expect(document.documentElement.dataset.theme).toBe("ocular-dark")
})

it("supports radio keyboard selection and reports unavailable persistence", async () => {
  render(<AppThemePicker />)
  const initial = screen.getByRole("radio", { name: "Legato" })
  initial.focus()
  await act(async () => { fireEvent.keyDown(initial, { key: "ArrowRight" }) })
  expect(document.activeElement).toBe(screen.getByRole("radio", { name: "Ocular Light" }))
  expect(appTheme().id).toBe("ocular-light")
  vi.spyOn(Storage.prototype, "setItem").mockImplementation(() => { throw new Error("Storage is disabled") })
  fireEvent.click(screen.getByRole("radio", { name: "Dracula" }))
  expect((await screen.findByRole("alert")).textContent).toBe("Storage is disabled")
  expect(appTheme().id).toBe("ocular-light")
})

it("does not save a page-only choice when a native app cannot receive it", async () => {
  ;(window as { legatoShell?: unknown }).legatoShell = { theme: 1, key: "page-key" }
  render(<AppThemePicker />)
  fireEvent.click(screen.getByRole("radio", { name: "Ocular Dark" }))
  expect((await screen.findByRole("alert")).textContent).toContain("Use Appearance in the app's settings")
  expect(localStorage.getItem(APP_THEME_KEY)).toBeNull()
})

it("reports an unresponsive native app without pretending the theme was saved", async () => {
  vi.useFakeTimers()
  ;(window as { legatoShell?: unknown }).legatoShell = { theme: 1, key: "page-key" }
  window.ReactNativeWebView = { postMessage: () => {} }
  render(<AppThemePicker />)
  fireEvent.click(screen.getByRole("radio", { name: "Ocular Dark" }))
  await act(async () => { await vi.advanceTimersByTimeAsync(10_000) })
  expect(screen.getByRole("alert").textContent).toContain("The app did not save the theme")
  expect(localStorage.getItem(APP_THEME_KEY)).toBeNull()
  expect((screen.getByRole("radio", { name: "Ocular Dark" }) as HTMLButtonElement).disabled).toBe(false)
})
