import { useEffect, useState } from "react"
import { invoke } from "@tauri-apps/api/core"
import { listen } from "@tauri-apps/api/event"
import { renderedThemes } from "../../themes/render"
import { errorText } from "./api"

export type AppTheme = "legato" | "dracula" | "ocular-light" | "ocular-dark" | "ocular-light-vivid" | "ocular-dark-vivid"
const themes = [{ id: "legato", name: "Legato" }, ...renderedThemes]
const applied = new Set<string>()

function applyTheme(id: AppTheme) {
  const root = document.documentElement
  for (const key of applied) root.style.removeProperty(key)
  applied.clear()
  const theme = renderedThemes.find(t => t.id === id)
  if (theme) {
    const t = theme.tokens
    const material = theme.material
    const m = material.colors
    const ink = material.inks
    const variables = {
      "theme-selection": t.selection,
      ground: t.canvas, panel: m.panel, raised: m.raised, sunk: m.rail, chrome: m.rail,
      line: m.divider, "line-strong": t.border,
      text: ink.text, text2: ink.muted, faint: ink.muted,
      accent: material.accent, accent2: material.accent, link: ink.link, green: ink.success, amber: ink.warning,
      "theme-accent": material.accent, "theme-focus": material.accent,
      "theme-identity": material.identity, "theme-voice-start": material.voiceStart,
      danger: ink.error, "danger-line": t.border, error: ink.error, "error-soft": m.errorSurface,
      "on-accent": t.canvas, "on-warning": t.canvas,
      scrim: `color-mix(in srgb, ${t.canvas} 88%, transparent)`,
      "key-body": m.raised, "key-plug": t.border, readout: t.canvas,
      "accent-glow": `color-mix(in srgb, ${material.accent} 45%, transparent)`,
      "accent-glow-soft": `color-mix(in srgb, ${material.accent} 20%, transparent)`,
      "close-hover": t.error, "on-close": t.canvas,
      "theme-surface": m.panel,
      "shadow-panel": material.panelShadow, "shadow-control": material.controlShadow,
    }
    for (const [role, value] of Object.entries(ink)) {
      root.style.setProperty(`--theme-${role}`, value)
      applied.add(`--theme-${role}`)
    }
    for (const [role, value] of Object.entries(m)) {
      root.style.setProperty(`--material-${role}`, value)
      applied.add(`--material-${role}`)
    }
    for (const [key, value] of Object.entries(variables)) {
      root.style.setProperty(`--${key}`, value)
      applied.add(`--${key}`)
    }
  }
  root.dataset.theme = id
  root.style.colorScheme = theme?.polarity ?? "dark"
}

export async function initializeTheme() {
  let changed = false
  const unlisten = await listen<AppTheme>("app-theme-changed", event => {
    changed = true
    applyTheme(event.payload)
  })
  try {
    const saved = await invoke<AppTheme>("app_theme")
    if (!changed) applyTheme(saved)
  } catch (error) { unlisten(); throw error }
  return unlisten
}

export function ThemeSection() {
  const [theme, setTheme] = useState<AppTheme | null>(null)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState("")
  useEffect(() => {
    let active = true
    let changed = false
    const unlisten = listen<AppTheme>("app-theme-changed", event => {
      changed = true
      if (active) setTheme(event.payload)
    })
    unlisten.then(() => invoke<AppTheme>("app_theme")).then(saved => {
      if (active && !changed) setTheme(saved)
    }).catch(e => { if (active) setError(errorText(e)) })
    return () => { active = false; void unlisten.then(stop => stop()).catch(() => {}) }
  }, [])
  return <div className="panel-section">
    <div className="section-head"><h2>Appearance</h2></div>
    <label className="field">App theme
      <select value={theme ?? "legato"} disabled={theme === null || busy} onChange={async e => {
        const next = e.target.value as AppTheme
        setBusy(true); setError("")
        try {
          await invoke("set_app_theme", { theme: next })
          setTheme(next); applyTheme(next)
        } catch (e) {
          setError(errorText(e))
          // Delivery can fail after saving; keep the shell on the saved choice.
          try {
            const saved = await invoke<AppTheme>("app_theme")
            setTheme(saved); applyTheme(saved)
          } catch (readError) { setError(`${errorText(e)}; ${errorText(readError)}`) }
        } finally { setBusy(false) }
      }}>
        {themes.map(t => <option key={t.id} value={t.id}>{t.name}</option>)}
      </select>
    </label>
    {error && <p className="error" role="alert">{error}</p>}
  </div>
}
