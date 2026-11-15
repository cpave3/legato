import { renderedThemes, type ThemeTokens, type ThemeMaterial } from "../../../themes/render"
export type { ThemeTokens } from "../../../themes/render"

export type AppThemeId = "legato" | "dracula" | "ocular-light" | "ocular-dark"
export const APP_THEME_KEY = "legato:app-theme"
export const THEME_CHANGED = "legato:theme-changed"
export const appThemes = [{ id: "legato", name: "Legato", polarity: "dark", tokens: null }, ...renderedThemes]

export function appTheme() {
  let id: string | null = null
  try { id = localStorage.getItem(APP_THEME_KEY) } catch { /* Storage unavailable. */ }
  return appThemes.find(theme => theme.id === id) ?? appThemes[0]
}

// These are role mappings, not another palette. Platform adapters use the same source.
export function themeVariables(t: ThemeTokens, material: ThemeMaterial): Record<string, string> {
  const m = material.colors
  const ink = material.inks
  const roles: Record<string, string> = {
    rail: m.rail, ground: t.canvas, surface: m.panel, raised: m.raised,
    selected: t.selection, line: m.divider, "line-strong": t.border,
    text: ink.text, "text-2": ink.muted, faint: ink.muted,
    accent: t.focus, "accent-ring": t.focus, link: ink.link,
    "accent-soft": t.selection, "accent-text": ink.link,
    live: ink.success, "live-soft": m.successSurface, needs: ink.warning, "needs-soft": m.warningSurface,
    done: ink.info, "done-soft": m.infoSurface, error: ink.error, "error-soft": m.errorSurface,
    "idle-soft": m.panel, danger: ink.error, "danger-line": t.border, record: ink.error,
  }
  const vars = Object.fromEntries(Object.entries(roles).map(([role, value]) => [`--color-lw-${role}`, value]))
  vars["--shadow-lw-panel"] = material.panelShadow
  vars["--shadow-lw-control"] = material.controlShadow
  for (const [role, value] of Object.entries(m)) vars[`--material-${role}`] = value
  for (const [role, value] of Object.entries(ink)) vars[`--theme-${role}`] = value
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
  root.dataset.theme = theme.id
  root.style.colorScheme = theme.polarity
}

export async function setAppTheme(id: AppThemeId): Promise<void> {
  const shell = (window as { legatoShell?: { theme?: number; key?: string } }).legatoShell
  const bridge = window.ReactNativeWebView
  if (shell?.theme === 1) {
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
    localStorage.setItem(APP_THEME_KEY, id)
  }
  applyAppTheme()
  window.dispatchEvent(new Event(THEME_CHANGED))
}

export function subscribeTheme(handler: () => void): () => void {
  const storage = (event: StorageEvent) => {
    if (event.key === null || event.key === APP_THEME_KEY || event.key === "legato:terminal-theme") {
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
