import { useEffect, useState } from "react"
import { listen } from "@tauri-apps/api/event"
import { api, errorText, type VoiceKeyInfo, type VoiceTargets } from "../api"

// Servers & keys: the key that starts talking to the Reeve, and which
// server's Reeve it talks to.
export function VoiceSection() {
  const [key, setKey] = useState<VoiceKeyInfo | null>(null)
  const [targets, setTargets] = useState<VoiceTargets | null>(null)
  const [error, setError] = useState("")

  useEffect(() => {
    api.voiceKeyInfo().then(setKey).catch((e) => setError(errorText(e)))
    api.voiceTargets().then(setTargets).catch((e) => setError(errorText(e)))
    const offKey = listen<VoiceKeyInfo>("voice-key-changed", (e) => setKey(e.payload))
    const offTargets = listen<VoiceTargets>("voice-targets-changed", (e) => setTargets(e.payload))
    return () => {
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
