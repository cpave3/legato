import { afterEach, expect, it } from "vitest"
import { appTheme, applyAppTheme, setAppTheme, type AppThemeId } from "../appTheme"
import { terminalTheme, setTerminalTheme } from "../terminalTheme"
import compatibility from "./fixtures/ocular-compatibility.json"

function luminance(hex: string) {
  const [r, g, b] = [1, 3, 5].map(i => parseInt(hex.slice(i, i + 2), 16) / 255).map(v => v <= .04045 ? v / 12.92 : ((v + .055) / 1.055) ** 2.4)
  return r * .2126 + g * .7152 + b * .0722
}
function contrast(a: string, b: string) {
  const values = [luminance(a), luminance(b)].sort((a, b) => a - b)
  return (values[1] + .05) / (values[0] + .05)
}
afterEach(() => { localStorage.clear(); applyAppTheme() })

it.each<AppThemeId>(["ocular-light", "ocular-dark", "dracula", "ocular-light-vivid", "ocular-dark-vivid"])("renders readable UI text and terminal ANSI in %s", id => {
  setAppTheme(id)
  const style = document.documentElement.style
  const ground = style.getPropertyValue("--color-lw-ground")
  for (const role of ["text", "text-2", "faint", "live", "needs", "error", "link"]) {
    expect(contrast(style.getPropertyValue(`--color-lw-${role}`), ground), role).toBeGreaterThanOrEqual(4.5)
  }
  setTerminalTheme(id)
  const terminal = terminalTheme().theme
  for (const role of ["foreground", "black", "red", "green", "yellow", "blue", "magenta", "cyan", "white", "brightBlack", "brightRed", "brightGreen", "brightYellow", "brightBlue", "brightMagenta", "brightCyan", "brightWhite"] as const) {
    expect(contrast(terminal[role]!, terminal.background!), `ANSI ${role}`).toBeGreaterThanOrEqual(4.5)
  }
  if (terminal.selectionForeground) {
    expect(contrast(terminal.selectionForeground, terminal.selectionBackground!)).toBeGreaterThanOrEqual(4.5)
  } else {
    for (const ink of [terminal.foreground, terminal.black, terminal.red, terminal.green, terminal.yellow, terminal.blue, terminal.magenta, terminal.cyan]) {
      expect(contrast(ink!, terminal.selectionBackground!)).toBeGreaterThanOrEqual(4.5)
    }
  }
  expect(contrast(terminal.cursor!, terminal.cursorAccent!)).toBeGreaterThanOrEqual(4.5)
})

it("keeps defaults and falls back safely from an invalid persisted app selection", () => {
  localStorage.setItem("legato:app-theme", "unknown")
  applyAppTheme()
  expect(appTheme().id).toBe("legato")
  expect(document.documentElement.style.getPropertyValue("--color-lw-ground")).toBe("")
  expect(terminalTheme().id).toBe("legato")
})

it.each<AppThemeId>(["ocular-light", "ocular-dark", "dracula", "ocular-light-vivid", "ocular-dark-vivid"])("gives %s distinct structural, reading, panel and raised layers without weakening text or control boundaries", async id => {
  await setAppTheme(id)
  const style = document.documentElement.style
  const layers = ["rail", "ground", "surface", "raised"].map(role => style.getPropertyValue(`--color-lw-${role}`))
  expect(new Set(layers).size).toBe(4)
  expect(style.getPropertyValue("--color-lw-text-2")).not.toBe(style.getPropertyValue("--color-lw-text"))
  const backgrounds = [...layers, style.getPropertyValue("--color-lw-selected"),
    style.getPropertyValue("--material-identitySurface"), style.getPropertyValue("--material-noticeSurface"),
    ...["live", "needs", "done", "error"].map(role => style.getPropertyValue(`--color-lw-${role}-soft`))]
  for (const background of backgrounds) {
    for (const ink of ["text", "text-2", "error", "live", "needs", "link", "accent-text"]) {
      expect(contrast(style.getPropertyValue(`--color-lw-${ink}`), background), `${ink} on ${background}`).toBeGreaterThanOrEqual(4.5)
    }
  }
  expect(contrast(style.getPropertyValue("--color-lw-accent"), layers[1])).toBeGreaterThanOrEqual(4.5)
  for (const background of backgrounds) {
    // Dracula's existing accent is a focus boundary, not contracted text ink.
    expect(contrast(style.getPropertyValue("--color-lw-accent"), background)).toBeGreaterThanOrEqual(id === "dracula" ? 3 : 4.5)
  }
  for (const background of layers) {
    expect(contrast(style.getPropertyValue("--color-lw-line-strong"), background)).toBeGreaterThanOrEqual(3)
  }
  expect(style.getPropertyValue("--color-lw-line")).not.toBe(style.getPropertyValue("--color-lw-line-strong"))
  expect(style.getPropertyValue("--shadow-lw-panel")).toContain("1px 2px")
})


it("preserves the pre-refinement terminal palettes, neutral layers and Dracula UI", async () => {
  setTerminalTheme("app")
  for (const [id, expected] of Object.entries(compatibility.terminals)) {
    await setAppTheme(id as AppThemeId)
    expect(terminalTheme().theme, `${id} ANSI`).toEqual(expected)
    const style = document.documentElement.style
    for (const [role, color] of Object.entries(compatibility.neutralLayers[id as keyof typeof compatibility.neutralLayers])) {
      expect(style.getPropertyValue(`--color-lw-${role}`), `${id} ${role}`).toBe(color)
    }
    if (id === "dracula") for (const [key, value] of Object.entries(compatibility.draculaVariables)) {
      expect(style.getPropertyValue(key), key).toBe(value)
    }
  }
})

it.each<AppThemeId>(["ocular-light", "ocular-dark", "dracula", "ocular-light-vivid", "ocular-dark-vivid"])("tells a stage track's lit bars from its unlit ones in %s", id => {
  setAppTheme(id)
  const style = document.documentElement.style
  const off = style.getPropertyValue("--color-lw-track-off")
  expect(off).not.toBe("")
  expect(contrast(style.getPropertyValue("--color-lw-text"), off)).toBeGreaterThanOrEqual(3)
})
