import { useEffect, useMemo, useState } from "react"
import wordList from "../../src-tauri/resources/pairing-words.txt?raw"
import { api, errorText, type Server } from "../api"

// Each list word is known by its first three letters, as the server and
// the phone read a phrase.
const byPrefix = new Map(wordList.split("\n").filter(Boolean).map((w) => [w.slice(0, 3), w]))

function readWords(text: string): { typed: string; word?: string }[] {
  return text
    .split(/[^A-Za-z]+/)
    .filter(Boolean)
    .map((t) => ({ typed: t, word: t.length >= 3 ? byPrefix.get(t.toLowerCase().slice(0, 3)) : undefined }))
}

type Props = { onPaired: (s: Server) => void; onCancel?: () => void }

// Pairing by phrase: the server shows four words (`legato pair`, or Settings
// › Pair a device in its web UI); the app proves it knows them without
// sending them, and the server sends its token and certificate, sealed.
export function PairForm({ onPaired, onCancel }: Props) {
  const [url, setUrl] = useState("https://")
  const [phrase, setPhrase] = useState("")
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState("")
  const words = useMemo(() => readWords(phrase), [phrase])
  const ready = /^https?:\/\/[^/\s]+/.test(url) && words.length === 4 && words.every((w) => w.word)

  // Servers announcing themselves on the network, as the phone lists them.
  const [nearby, setNearby] = useState<{ name: string; url: string }[]>([])
  useEffect(() => {
    let stop = false
    const look = () => api.discovered().then((list) => { if (!stop) setNearby(list) }).catch(() => {})
    look()
    const id = setInterval(look, 1500)
    return () => { stop = true; clearInterval(id) }
  }, [])

  const pair = async () => {
    setBusy(true)
    setError("")
    try {
      onPaired(await api.pairServer(url, words.map((w) => w.word).join(" ")))
    } catch (e) {
      setError(errorText(e))
    } finally {
      setBusy(false)
    }
  }

  return (
    <form className="pair" onSubmit={(e) => { e.preventDefault(); if (ready && !busy) pair() }}>
      <div className="nearby">
        <span className="nearby-head">On this network</span>
        {nearby.length ? nearby.map((n) => (
          <button key={n.url} type="button" className={"nearby-item" + (n.url === url ? " chosen" : "")} onClick={() => setUrl(n.url)}>
            <span className="nearby-name">{n.name}</span><span className="mono dim">{n.url}</span>
          </button>
        )) : <span className="dim small">Looking… servers can take a few seconds to answer. Some networks block this; then type the address.</span>}
      </div>
      <label className="field">
        <span>Server address</span>
        <input value={url} onChange={(e) => setUrl(e.target.value)} placeholder="https://legato.local:3080" spellCheck={false} autoFocus />
      </label>
      <label className="field">
        <span>Pairing phrase</span>
        <input value={phrase} onChange={(e) => setPhrase(e.target.value)} placeholder="four words" spellCheck={false} autoComplete="off" />
      </label>
      <div className="phrase-words" aria-live="polite">
        {Array.from({ length: 4 }, (_, i) => {
          const w = words[i]
          return (
            <span key={i} className={"phrase-word" + (w ? (w.word ? " known" : " unknown") : "")}>
              {w ? w.word ?? w.typed : "·"}
            </span>
          )
        })}
      </div>
      <p className="hint">
        On the server, run <code>legato pair</code>, or open Settings › Pair a device in its web UI. The words are never sent;
        the server sends its certificate sealed, and the app trusts only that certificate for it.
      </p>
      {error && <p className="error">{error}</p>}
      <div className="row gap">
        <button className="btn primary" disabled={!ready || busy} type="submit">{busy ? "Pairing…" : "Pair"}</button>
        {onCancel && <button className="btn" type="button" onClick={onCancel}>Cancel</button>}
      </div>
    </form>
  )
}
