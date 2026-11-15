import { useEffect, useRef, useState } from "react"
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

type ThemeChoice = AppTheme | "server"
type Appearance = { id: ThemeChoice, effective: AppTheme }

export function ThemeSection() {
  const [appearance, setAppearance] = useState<Appearance | null>(null)
  const revision = useRef(0)
  const mounted = useRef(false)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState("")
  useEffect(() => {
    let active = true
    mounted.current = true
    let changed = false
    const unlisten = listen<Appearance>("app-theme-selection-changed", event => {
      revision.current++
      changed = true
      if (active) setAppearance(event.payload)
    })
    unlisten.then(() => invoke<Appearance>("app_theme_selection")).then(saved => {
      if (active && !changed) setAppearance(saved)
    }).catch(e => { if (active) setError(errorText(e)) })
    return () => { active = false; mounted.current = false; void unlisten.then(stop => stop()).catch(() => {}) }
  }, [])
  const readSaved = async () => {
    const before = revision.current
    const saved = await invoke<Appearance>("app_theme_selection")
    if (mounted.current && revision.current === before) { setAppearance(saved); applyTheme(saved.effective) }
  }
  return <div className="panel-section">
    <div className="section-head"><h2>Appearance</h2></div>
    <label className="field">App theme
      <select value={appearance?.id ?? "legato"} disabled={appearance === null || busy} onChange={async e => {
        const next = e.target.value as ThemeChoice
        setBusy(true); setError("")
        try {
          await invoke("set_app_theme", { theme: next })
          await readSaved()
        } catch (e) {
          setError(errorText(e))
          // Delivery can fail after saving; reread both mode and effective theme.
          try { await readSaved() }
          catch (readError) { setError(`${errorText(e)}; ${errorText(readError)}`) }
        } finally { setBusy(false) }
      }}>
        <option value="server">Follow server</option>
        {themes.map(t => <option key={t.id} value={t.id}>{t.name}</option>)}
      </select>
    </label>
    {appearance?.id === "server" && <p role="status">Using {themes.find(t => t.id === appearance.effective)?.name}</p>}
    {error && <p className="error" role="alert">{error}</p>}
  </div>
}
