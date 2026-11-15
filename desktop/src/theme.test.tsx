import { afterEach, expect, it, vi } from "vitest"
import "@testing-library/jest-dom/vitest"
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react"
import palettes from "../../themes/palettes.json"
import { renderedThemes } from "../../themes/render"
import { ThemeSection, initializeTheme } from "./theme"

const { invoke, listeners } = vi.hoisted(() => ({ invoke: vi.fn(), listeners: new Set<(event: { payload: string }) => void>() }))
vi.mock("@tauri-apps/api/event", () => ({ listen: vi.fn(async (_name: string, callback: (event: { payload: string }) => void) => {
  listeners.add(callback)
  return () => listeners.delete(callback)
}) }))
vi.mock("@tauri-apps/api/core", () => ({ invoke }))
afterEach(() => { cleanup(); listeners.clear(); vi.clearAllMocks() })

it.each([
  ["ocular-light", "Ocular Light"], ["ocular-dark", "Ocular Dark"],
  ["ocular-light-vivid", "Ocular Light Vivid"], ["ocular-dark-vivid", "Ocular Dark Vivid"],
  ["dracula", "Dracula"],
])("restores and selects %s through the native picker", async (id, name) => {
  invoke.mockResolvedValue(id)
  await initializeTheme()
  const root = document.documentElement
  const palette = palettes.themes.find(t => t.id === id)
  const { material } = renderedThemes.find(t => t.id === id)!
  const accent = material.inks[id === "dracula" ? "focus" : "keyword"]
  const identity = material.inks[id === "dracula" ? "link" : "keyword"]
  expect(root.dataset.theme).toBe(id)
  expect(root.style.colorScheme).toBe(palette?.polarity ?? "dark")
  expect(root.style.getPropertyValue("--ground")).toBe(palette?.tokens.canvas ?? "")
  for (const role of ["accent", "accent2", "theme-accent", "theme-focus"]) {
    expect(root.style.getPropertyValue(`--${role}`)).toBe(accent)
  }
  expect(root.style.getPropertyValue("--accent-glow")).toBe(`color-mix(in srgb, ${accent} 45%, transparent)`)
  expect(root.style.getPropertyValue("--accent-glow-soft")).toBe(`color-mix(in srgb, ${accent} 20%, transparent)`)
  expect(root.style.getPropertyValue("--theme-identity")).toBe(identity)
  expect(root.style.getPropertyValue("--theme-voice-start")).toBe(id === "dracula" ? material.inks.success : accent)
  expect(root.style.getPropertyValue("--link")).toBe(material.inks.link)
  expect(root.style.getPropertyValue("--green")).toBe(material.inks.success)
  expect(root.style.getPropertyValue("--text")).toBe(material.inks.text)
  for (const [role, value] of Object.entries(material.colors)) {
    expect(root.style.getPropertyValue(`--material-${role}`)).toBe(value)
  }
  render(<ThemeSection />)
  const picker = screen.getByLabelText("App theme")
  await waitFor(() => expect(picker).toBeEnabled())
  expect(screen.getByRole("option", { name })).toHaveValue(id)
  expect(picker).toHaveValue(id)
  fireEvent.change(picker, { target: { value: "legato" } })
  await waitFor(() => expect(picker).toHaveValue("legato"))
  for (const role of ["accent", "theme-accent", "theme-focus", "theme-identity", "theme-voice-start", ...Object.keys(material.colors).map(role => `material-${role}`)]) {
    expect(root.style.getPropertyValue(`--${role}`)).toBe("")
  }
  fireEvent.change(picker, { target: { value: id } })
  await waitFor(() => expect(invoke).toHaveBeenLastCalledWith("set_app_theme", { theme: id }))
  await waitFor(() => expect(picker).toHaveValue(id))
  expect(root.dataset.theme).toBe(id)
  expect(root.style.getPropertyValue("--accent")).toBe(accent)
  expect(root.style.getPropertyValue("--theme-identity")).toBe(identity)
})

it("restores the saved shell appearance, changes it through settings and restores Legato defaults", async () => {
  invoke.mockImplementation((command: string) => Promise.resolve(command === "app_theme" ? "ocular-light" : undefined))
  await initializeTheme()
  const light = palettes.themes.find(t => t.id === "ocular-light")!
  expect(document.documentElement.style.getPropertyValue("--ground")).toBe(light.tokens.canvas)
  expect(document.documentElement.style.getPropertyValue("--text2")).toBe(light.tokens.muted)
  expect(document.documentElement.style.colorScheme).toBe("light")
  render(<ThemeSection />)
  const picker = await screen.findByLabelText("App theme")
  await waitFor(() => expect(picker).toHaveValue("ocular-light"))
  fireEvent.change(picker, { target: { value: "dracula" } })
  await waitFor(() => expect(invoke).toHaveBeenCalledWith("set_app_theme", { theme: "dracula" }))
  await waitFor(() => expect(document.documentElement.style.getPropertyValue("--ground")).toBe(palettes.themes.find(t => t.id === "dracula")!.tokens.canvas))
  fireEvent.change(picker, { target: { value: "legato" } })
  await waitFor(() => expect(document.documentElement.style.getPropertyValue("--ground")).toBe(""))
  expect(document.documentElement.style.colorScheme).toBe("dark")
})

it("shows save failures and keeps the stored selection", async () => {
  invoke.mockImplementation((command: string) => command === "app_theme" ? Promise.resolve("legato") : Promise.reject("Could not write state.json"))
  render(<ThemeSection />)
  const picker = screen.getByLabelText("App theme")
  await waitFor(() => expect(picker).toBeEnabled())
  fireEvent.change(picker, { target: { value: "ocular-dark" } })
  expect(await screen.findByRole("alert")).toHaveTextContent("Could not write state.json")
  expect(picker).toHaveValue("legato")
})

it("keeps the shell on the saved choice if a retained page could not be updated", async () => {
  let stored = "legato"
  invoke.mockImplementation((command: string, args?: { theme: string }) => {
    if (command === "app_theme") return Promise.resolve(stored)
    stored = args!.theme
    return Promise.reject("two: page unavailable")
  })
  render(<ThemeSection />)
  const picker = screen.getByLabelText("App theme")
  await waitFor(() => expect(picker).toBeEnabled())
  fireEvent.change(picker, { target: { value: "ocular-dark" } })
  expect(await screen.findByRole("alert")).toHaveTextContent("page unavailable")
  await waitFor(() => expect(picker).toHaveValue("ocular-dark"))
  expect(document.documentElement.style.getPropertyValue("--ground")).toBe(palettes.themes.find(t => t.id === "ocular-dark")!.tokens.canvas)
})

it("updates the live shell with settings closed when a server page chooses a theme", async () => {
  invoke.mockResolvedValue("legato")
  await initializeTheme()
  for (const callback of listeners) callback({ payload: "dracula" })
  const root = document.documentElement
  expect(root.dataset.theme).toBe("dracula")
  expect(root.style.getPropertyValue("--text2")).not.toBe("")
  expect(root.style.getPropertyValue("--text2")).not.toBe(root.style.getPropertyValue("--text"))
  expect(root.style.getPropertyValue("--line-strong")).toBe("#BD93F9")
})

it("updates an already open picker when the native theme event arrives", async () => {
  invoke.mockResolvedValue("legato")
  render(<ThemeSection />)
  const picker = screen.getByLabelText("App theme")
  await waitFor(() => expect(picker).toBeEnabled())
  for (const callback of listeners) callback({ payload: "ocular-dark" })
  await waitFor(() => expect(picker).toHaveValue("ocular-dark"))
})

it.each(["ocular-light", "ocular-dark", "ocular-light-vivid", "ocular-dark-vivid", "dracula"])("restores %s with distinct chrome, panel and raised materials", async id => {
  invoke.mockResolvedValue(id)
  await initializeTheme()
  const style = document.documentElement.style
  const planes = ["chrome", "ground", "panel", "raised"].map(role => style.getPropertyValue(`--${role}`))
  expect(planes.every(Boolean)).toBe(true)
  expect(new Set(planes).size).toBe(4)
  expect(style.getPropertyValue("--line")).not.toBe(style.getPropertyValue("--line-strong"))
  expect(style.getPropertyValue("--shadow-panel")).toContain("1px 2px")
})
