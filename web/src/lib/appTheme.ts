import { renderedThemes, type ThemeTokens, type ThemeMaterial } from "../../../themes/render"
import { activeThemeConnection, cachedServerTheme, SERVER_THEME_CACHE_KEY } from "./serverTheme"
export type { ThemeTokens } from "../../../themes/render"

export type AppThemeId = "legato" | "dracula" | "ocular-light" | "ocular-dark" | "ocular-light-vivid" | "ocular-dark-vivid"
export const APP_THEME_KEY = "legato:app-theme"
export const THEME_CHANGED = "legato:theme-changed"
export const appThemes = [{ id: "legato", name: "Legato", polarity: "dark", tokens: null }, ...renderedThemes]

export const THEME_MODE_KEY = "legato:theme-mode"
export const EFFECTIVE_THEME_KEY = "legato:effective-app-theme"

function stored(key: string): string | null {
  try { return localStorage.getItem(key) } catch { return null }
}

export function shellThemeCapability(): number {
  return (window as { legatoShell?: { theme?: number } }).legatoShell?.theme ?? 0
}

export function themeMode(): "server" | "local" {
  return shellThemeCapability() !== 1 && stored(THEME_MODE_KEY) === "server" ? "server" : "local"
}

export function appThemeChoice(): string {
  return themeMode() === "server" ? "server" : localAppTheme().id
}

function localAppTheme() {
  return appThemes.find(theme => theme.id === stored(APP_THEME_KEY)) ?? appThemes[0]
}

export function appTheme() {
  const shell = shellThemeCapability()
  const id = shell >= 2 ? stored(EFFECTIVE_THEME_KEY)
    : themeMode() === "server" ? cachedServerTheme(activeThemeConnection())?.id : null
  return appThemes.find(theme => theme.id === id) ?? localAppTheme()
}

// These are role mappings, not another palette. Platform adapters use the same source.
export function themeVariables(t: ThemeTokens, material: ThemeMaterial): Record<string, string> {
  const m = material.colors
  const ink = material.inks
  const roles: Record<string, string> = {
    rail: m.rail, ground: t.canvas, surface: m.panel, raised: m.raised,
    selected: t.selection, line: m.divider, "line-strong": t.border,
    text: ink.text, "text-2": ink.muted, faint: ink.muted,
    accent: material.accent, "accent-ring": material.accent, link: ink.link,
    "accent-soft": m.identitySurface, "accent-text": material.identity,
    live: ink.success, "live-soft": m.successSurface, needs: ink.warning, "needs-soft": m.warningSurface,
    done: ink.info, "done-soft": m.infoSurface, error: ink.error, "error-soft": m.errorSurface,
    "idle-soft": m.panel, danger: ink.error, "danger-line": t.border, record: ink.error,
  }
  const vars = Object.fromEntries(Object.entries(roles).map(([role, value]) => [`--color-lw-${role}`, value]))
  vars["--shadow-lw-panel"] = material.panelShadow
  vars["--shadow-lw-control"] = material.controlShadow
  for (const [role, value] of Object.entries(m)) vars[`--material-${role}`] = value
  for (const [role, value] of Object.entries(ink)) vars[`--theme-${role}`] = value
  vars["--theme-accent"] = material.accent
  vars["--theme-focus"] = material.accent
  vars["--theme-identity"] = material.identity
  vars["--theme-voice-start"] = material.voiceStart
  vars["--theme-notice"] = material.notice
  vars["--theme-surface"] = m.panel
  // Older screens still use Tailwind's neutral scales. Keep their semantics in
  // both polarities without changing the incumbent Legato palette.
  for (const scale of ["zinc", "gray", "slate", "neutral"]) {
    for (const n of [50, 100, 200, 300]) vars[`--color-${scale}-${n}`] = t.text
    for (const n of [400, 500, 600]) vars[`--color-${scale}-${n}`] = t.muted
    vars[`--color-${scale}-700`] = t.border
    vars[`--color-${scale}-800`] = m.raised
    vars[`--color-${scale}-900`] = m.panel
    vars[`--color-${scale}-950`] = t.canvas
  }
  return vars
}

const applied = new Set<string>()
export function applyAppTheme(): void {
  const theme = appTheme()
  const root = document.documentElement
  for (const key of applied) root.style.removeProperty(key)
  applied.clear()
  if (theme.tokens) for (const [key, value] of Object.entries(themeVariables(theme.tokens, theme.material))) {
    root.style.setProperty(key, value)
    applied.add(key)
  }
  if (!shellThemeCapability()) {
    try { localStorage.setItem(EFFECTIVE_THEME_KEY, theme.id) } catch { /* Rendering does not require storage. */ }
  }
  root.dataset.theme = theme.id
  root.style.colorScheme = theme.polarity
}

export async function setAppTheme(id: AppThemeId | "server"): Promise<void> {
  const shell = (window as { legatoShell?: { theme?: number; key?: string } }).legatoShell
  const bridge = window.ReactNativeWebView
  if (shell?.theme === 1 && id === "server") throw new Error("Update the app to follow the server theme.")
  if (shell && (shell.theme ?? 0) >= 1) {
    if (!shell.key || !bridge?.postMessage) throw new Error("This app cannot save a theme from the page. Use Appearance in the app's settings.")
    await new Promise<void>((resolve, reject) => {
      const finish = (error?: string) => {
        clearTimeout(timer)
        window.removeEventListener("legato:shell-theme", answer)
        if (error) reject(new Error(error)); else resolve()
      }
      const answer = (event: Event) => {
        const result = (event as CustomEvent<{ id: string; error?: string }>).detail
        if (result?.id === id) finish(result.error)
      }
      const timer = setTimeout(() => finish("The app did not save the theme. Try again in the app's Appearance settings."), 10_000)
      window.addEventListener("legato:shell-theme", answer)
      bridge.postMessage!(JSON.stringify({ legatoShell: "theme", type: "theme", key: shell.key, id }))
    })
  } else {
    if (id !== "server") localStorage.setItem(APP_THEME_KEY, id)
    localStorage.setItem(THEME_MODE_KEY, id === "server" ? "server" : "local")
  }
  applyAppTheme()
  window.dispatchEvent(new Event(THEME_CHANGED))
}

export function subscribeTheme(handler: () => void): () => void {
  const storage = (event: StorageEvent) => {
    if (event.key === null || event.key === APP_THEME_KEY || event.key === "legato:terminal-theme" || event.key === THEME_MODE_KEY || event.key === EFFECTIVE_THEME_KEY || event.key === SERVER_THEME_CACHE_KEY || event.key === "legato:active-server") {
      applyAppTheme()
      handler()
    }
  }
  window.addEventListener(THEME_CHANGED, handler)
  window.addEventListener("storage", storage)
  return () => {
    window.removeEventListener(THEME_CHANGED, handler)
    window.removeEventListener("storage", storage)
  }
}
