import { useEffect, useState } from "react"
import { listen } from "@tauri-apps/api/event"
import { api, errorText, type VoiceKeyInfo } from "../api"

// Servers & keys: the key that starts talking to the Reeve.
export function VoiceSection() {
  const [key, setKey] = useState<VoiceKeyInfo | null>(null)
  const [error, setError] = useState("")

  useEffect(() => {
    api.voiceKeyInfo().then(setKey).catch((e) => setError(errorText(e)))
    const off = listen<VoiceKeyInfo>("voice-key-changed", (e) => setKey(e.payload))
    return () => { off.then((f) => f()) }
  }, [])

  const hold = (on: boolean) => {
    setError("")
    api.setHoldToTalk(on).catch((e) => setError(errorText(e)))
  }
  const change = () => {
    setError("")
    api.changeVoiceKey().catch((e) => setError(errorText(e)))
  }

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
      {error && <p className="error">{error}</p>}
    </div>
  )
}
