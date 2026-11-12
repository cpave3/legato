import { useEffect, useState } from "react"
import { listen } from "@tauri-apps/api/event"
import { api, errorText, type Anchor, type KeyBackend, type ScreenId, type VoiceKeyInfo, type VoiceTargets, type VoiceWidgetSettings } from "../api"
import { recordedKey } from "../keys"

// Corners, then edges (the widget centred on them), as the widget's menu has them.
const PLACES: [Anchor, string][] = [
  ["bottom-right", "Bottom right"], ["bottom-left", "Bottom left"], ["top-right", "Top right"], ["top-left", "Top left"],
  ["top", "Top"], ["bottom", "Bottom"], ["left", "Left"], ["right", "Right"],
]

// Servers & keys: the key that starts talking to the Reeve, and which
// server's Reeve it talks to.
export function VoiceSection() {
  const [key, setKey] = useState<VoiceKeyInfo | null>(null)
  const [targets, setTargets] = useState<VoiceTargets | null>(null)
  const [widget, setWidget] = useState<VoiceWidgetSettings | null>(null)
  const [error, setError] = useState("")

  useEffect(() => {
    api.voiceKeyInfo().then(setKey).catch((e) => setError(errorText(e)))
    api.voiceTargets().then(setTargets).catch((e) => setError(errorText(e)))
    const offKey = listen<VoiceKeyInfo>("voice-key-changed", (e) => setKey(e.payload))
    const offTargets = listen<VoiceTargets>("voice-targets-changed", (e) => setTargets(e.payload))
    api.voiceWidgetSettings().then(setWidget).catch((e) => setError(errorText(e)))
    const offWidget = listen<VoiceWidgetSettings>("voice-widget-changed", (e) => setWidget(e.payload))
    return () => {
      offWidget.then((f) => f())
      offKey.then((f) => f())
      offTargets.then((f) => f())
    }
  }, [])

  const hold = (on: boolean) => {
    setError("")
    api.setHoldToTalk(on).catch((e) => setError(errorText(e)))
  }
  const change = () => {
    setError("")
    api.changeVoiceKey().catch((e) => setError(errorText(e)))
  }
  const chooseBackend = (backend: KeyBackend) => {
    setError("")
    api.setVoiceKeyBackend(backend).catch((e) => setError(errorText(e)))
  }
  const placeWidget = (next: VoiceWidgetSettings) => {
    setError("")
    setWidget(next)
    api.setVoiceWidget(next.shown, next.anchor).catch((e) => setError(errorText(e)))
  }
  const chooseScreen = (value: string) => {
    if (!widget || value === "missing") return
    setError("")
    const screen: ScreenId | null = value === "auto" ? null : widget.screens[Number(value)]?.id ?? null
    api.setVoiceScreen(screen).catch((e) => setError(errorText(e)))
  }
  const choose = (instance: string) => {
    setError("")
    api.setVoiceServer(instance).catch((e) => setError(errorText(e)))
  }

  const talkingTo = targets?.servers.find((s) => s.server === targets.talking_to)
  // One main decides by itself; the person chooses only otherwise.
  const oneMain = targets?.servers.filter((s) => s.instance?.role === "main").length === 1
  return (
    <div className="panel-section">
      <div className="section-head"><h2>Talking to the Reeve</h2></div>
      {key && (
        <>
          <p className="small">
            {key.key ? <>The key is <kbd>{key.key}</kbd>.</> : key.backend === "link" ? "This computer has no global key here." : "No key is bound yet."}
            {key.can_change && <> <button className="link" onClick={change}>Change the key…</button></>}
          </p>
          {key.problem && <p className="dim small">{key.problem}</p>}
          {key.backend === "grab" && key.talk && key.cancel && (
            <div className="row gap small">
              <KeyField label="Talk" value={key.talk} onRecorded={(k) => api.setVoiceKeys(k, key.cancel ?? null)} onError={setError} />
              <KeyField label="Cancel" value={key.cancel} onRecorded={(k) => api.setVoiceKeys(key.talk ?? null, k)} onError={setError} />
              <button className="link" onClick={() => { setError(""); api.setVoiceKeys(null, null).catch((e) => setError(errorText(e))) }}>Use the default keys</button>
            </div>
          )}
          {key.can_choose && (
            <>
              <label className="row gap small">
                How the app gets the key
                <select value={key.choice} onChange={(e) => chooseBackend(e.target.value as KeyBackend)} aria-label="How the app gets the key">
                  <option value="grab">The app grabs it (through XWayland)</option>
                  <option value="portal">The desktop binds it (shortcut portal)</option>
                </select>
              </label>
              {key.choice === "grab" && (
                <p className="hint">
                  On KDE the key reaches the app through XWayland: System Settings, Application Permissions, Legacy X11 App Support must pass keys typed with Ctrl, Alt or Meta held, as it does by default.
                </p>
              )}
              {key.choice !== key.backend && (
                <p className="hint">
                  This applies when the app next starts. <button className="link" onClick={() => api.restartApp().catch((e) => setError(errorText(e)))}>Restart now</button>
                </p>
              )}
            </>
          )}
          <label className="row gap small">
            <input type="checkbox" checked={key.hold_to_talk} onChange={(e) => hold(e.target.checked)} />
            Hold the key to talk
          </label>
          <p className="hint">
            {key.hold_to_talk
              ? "Hold the key while you speak, and let go to send."
              : "Press the key to start, and again to send. A pause also sends."}{" "}
            A desktop shortcut that opens <code>{key.link}</code> does what the key does.
          </p>
        </>
      )}
      {widget && (
        <div className="row gap small">
          <label className="row gap">
            <input type="checkbox" checked={widget.shown} onChange={(e) => placeWidget({ ...widget, shown: e.target.checked })} />
            Show the voice widget
          </label>
          <select value={widget.anchor} onChange={(e) => placeWidget({ ...widget, anchor: e.target.value as Anchor, dropped: false })} aria-label="Place">
            {PLACES.map(([a, label]) => <option key={a} value={a}>{label}{widget.dropped && a === widget.anchor ? " (where you dropped it)" : ""}</option>)}
          </select>
        </div>
      )}
      {widget && (widget.screens.length > 1 || widget.screen) && (
        <label className="row gap small">
          Screen
          <select value={screenValue(widget)} onChange={(e) => chooseScreen(e.target.value)} aria-label="Screen">
            <option value="auto">Where the desktop puts it</option>
            {widget.screens.map((s, i) => <option key={i} value={String(i)}>{s.name}</option>)}
            {widget.missing && <option value="missing">{widget.missing} (not connected)</option>}
          </select>
        </label>
      )}
      {widget?.missing && <p className="hint">{widget.missing} is not connected, so the widget is on another screen until it is.</p>}
      {widget && <p className="hint">Drag the widget to put it anywhere; it stays where you drop it.</p>}
      {targets && targets.servers.length > 1 && (
        <>
          <p className="small">The Reeve to talk to</p>
          {targets.servers.map((s) => (
            <label key={s.server} className="row gap small">
              <input
                type="radio"
                name="voice-server"
                disabled={!s.instance || oneMain}
                checked={s.server === targets.talking_to}
                onChange={() => s.instance && choose(s.instance.id)}
              />
              {s.name}
              <span className="dim">{s.instance ? s.instance.role : "not answering"}</span>
            </label>
          ))}
          <p className="hint">
            {targets.must_choose
              ? "Choose which server's Reeve the key talks to."
              : talkingTo
                ? `The key talks to the Reeve on ${talkingTo.name}${oneMain ? ", the main" : ""}.`
                : ""}
          </p>
        </>
      )}
      {error && <p className="error">{error}</p>}
    </div>
  )
}

// A key the person sets by pressing it: click, then press the new key with
// its modifiers; Escape alone gives up. The app checks it and says why it
// cannot take one.
function KeyField({ label, value, onRecorded, onError }: { label: string; value: string; onRecorded: (key: string) => Promise<void>; onError: (e: string) => void }) {
  const [recording, setRecording] = useState(false)
  const onKeyDown = (e: React.KeyboardEvent<HTMLButtonElement>) => {
    if (!recording) return
    e.preventDefault()
    e.stopPropagation()
    if (e.code === "Escape" && !e.ctrlKey && !e.altKey && !e.metaKey && !e.shiftKey) return setRecording(false)
    const key = recordedKey(e)
    if (!key) return
    setRecording(false)
    onError("")
    onRecorded(key).catch((err) => onError(errorText(err)))
  }
  return (
    <label className="row gap">
      {label}
      <button className="key-field" aria-label={`${label} key`} onClick={() => setRecording(true)} onKeyDown={onKeyDown} onBlur={() => setRecording(false)}>
        {recording ? "Press the new key…" : <kbd>{value}</kbd>}
      </button>
    </label>
  )
}

// The screen select's value: the chosen screen's place in the list, "missing"
// when it is not connected, or "auto".
function screenValue(w: VoiceWidgetSettings): string {
  if (!w.screen) return "auto"
  if (w.chosen != null) return String(w.chosen)
  return w.missing ? "missing" : "auto"
}
