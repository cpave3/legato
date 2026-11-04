import { useState } from "react"
import { api, errorText, initials, tintColor, type Server } from "../api"
import { Download, Lock } from "./Icons"

type Props = { server: Server; onTrusted: () => void; onCheckAgain: () => void }

// On a Mac, a server's page is drawn by macOS's web view, which trusts only
// the certificates the Mac trusts. The app pinned this server's CA when it
// paired, so it offers that CA here: trusted in one step (macOS asks for the
// password), or saved to install by hand.
export function TrustScreen({ server, onTrusted, onCheckAgain }: Props) {
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState("")
  const [saved, setSaved] = useState("")
  const host = server.url.replace(/^https:\/\//, "").replace(/[:/].*$/, "")

  const trust = async () => {
    setBusy(true); setError("")
    try {
      if (await api.trustServerCertificate(server.id)) onTrusted()
      else setError("macOS still does not trust it. Try the steps below, then Check again.")
    } catch (e) {
      setError(errorText(e))
    } finally {
      setBusy(false)
    }
  }

  const save = async () => {
    setError("")
    try { setSaved(await api.saveServerCertificate(server.id)) } catch (e) { setError(errorText(e)) }
  }

  return (
    <section className="trust">
      <span className="mini-tile big" style={{ "--tint": tintColor(server.tint) } as React.CSSProperties}>{initials(server.name)}</span>
      <h1>This Mac doesn't trust {server.name}'s certificate yet</h1>
      <p className="dim">
        {server.name} uses a certificate from its own authority, <b>Legato Local CA</b>. The app trusts it already: it pinned it when it paired.
        But macOS draws the server's page, and macOS checks its own list, so the page stays blank until the Mac trusts it too.
      </p>

      <div className="card">
        <div className="row gap wrap">
          <button className="btn primary" disabled={busy} onClick={trust}>
            <Lock size={14} /> {busy ? "Waiting for macOS…" : "Trust it on this Mac"}
          </button>
          <button className="btn" onClick={save}><Download size={14} /> Save the certificate</button>
          <span className="spacer" />
          <button className="btn" onClick={onCheckAgain}>Check again</button>
        </div>
        <p className="hint">
          macOS asks for your password. The certificate is trusted for <code>{host}</code> only, so it cannot vouch for any other site.
        </p>
        {error && <p className="error">{error}</p>}
      </div>

      <details className="card steps" open={!!saved}>
        <summary>Or install it yourself</summary>
        {saved && (
          <p className="hint">
            Saved as <code>{saved}</code>.{" "}
            <button className="link" onClick={() => api.openCertificate(saved).catch((e) => setError(errorText(e)))}>Open it in Keychain Access</button>
          </p>
        )}
        <ol>
          <li><b>Save the certificate</b> above, and double-click it. Keychain Access adds it to your <b>login</b> keychain.</li>
          <li>In Keychain Access, find <b>Legato Local CA</b> under login › Certificates, and double-click it.</li>
          <li>Open <b>Trust</b>, and set <b>When using this certificate</b> to <b>Always Trust</b>.</li>
          <li>Close the window, and enter your password.</li>
          <li>Come back here and click <b>Check again</b>.</li>
        </ol>
        <p className="dim small">
          Trusting it this way trusts it for every site, not only {host}. The button above is narrower.
        </p>
      </details>
    </section>
  )
}
