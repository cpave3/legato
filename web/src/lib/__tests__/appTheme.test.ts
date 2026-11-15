import { afterEach, expect, it } from "vitest"
import { appTheme, applyAppTheme, setAppTheme, type AppThemeId } from "../appTheme"
import { terminalTheme, setTerminalTheme } from "../terminalTheme"

function luminance(hex: string) {
  const [r, g, b] = [1, 3, 5].map(i => parseInt(hex.slice(i, i + 2), 16) / 255).map(v => v <= .04045 ? v / 12.92 : ((v + .055) / 1.055) ** 2.4)
  return r * .2126 + g * .7152 + b * .0722
}
function contrast(a: string, b: string) {
  const values = [luminance(a), luminance(b)].sort((a, b) => a - b)
  return (values[1] + .05) / (values[0] + .05)
}
afterEach(() => { localStorage.clear(); applyAppTheme() })

it.each<AppThemeId>(["ocular-light", "ocular-dark", "dracula"])("renders readable UI text and terminal ANSI in %s", id => {
  setAppTheme(id)
  const style = document.documentElement.style
  const ground = style.getPropertyValue("--color-lw-ground")
  for (const role of ["text", "text-2", "faint", "live", "needs", "error", "link"]) {
    expect(contrast(style.getPropertyValue(`--color-lw-${role}`), ground), role).toBeGreaterThanOrEqual(4.5)
  }
  setTerminalTheme(id as "ocular-light" | "ocular-dark" | "dracula")
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
