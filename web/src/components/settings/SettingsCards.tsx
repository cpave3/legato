import { useState } from "react"
import { Link } from "react-router-dom"
import { Download, FolderGit2, Keyboard, Palette, Plus, QrCode, RefreshCw, ScanSearch, Server, ShieldCheck, Smartphone, Sparkles, SquareTerminal, Trash2, Zap } from "lucide-react"
import { SettingsCard, SettingsRow, SettingsRows, Toggle, primaryButton, quietButton } from "./SettingsUI"
import { AppThemePicker } from "./AppThemePicker"
import { TerminalThemePicker } from "./TerminalThemePicker"
import { TintPicker } from "../TintPicker"
import { SettingSource } from "./RegistrySettings"
import type { ConfigSetting } from "../../lib/configSettings"
import { animatedAgentActivityEnabled, setAnimatedAgentActivityEnabled } from "../../lib/appearancePreferences"
import { useServer } from "../../hooks/useServer"
import { useToast } from "../../hooks/useToast"
import { apiFetch } from "../../lib/api"

// The cards of Settings that are not a registry setting or a list, drawn
// where the catalog (lib/settingsCatalog.ts) places them.

function stored(key: string, fallback: string): string {
  try { return localStorage.getItem(key) ?? fallback } catch { return fallback }
}
function store(key: string, value: string) {
  try { localStorage.setItem(key, value) } catch { /* Private windows keep nothing; the choice holds until reload. */ }
}

// App and terminal theme and motion, kept in this browser.
export function AppAppearanceCard() {
  const [glitch, setGlitch] = useState(() => stored("legato:glitch-effect", "true") !== "false")
  const [animated, setAnimated] = useState(animatedAgentActivityEnabled)
  return <div id="app-appearance">
    <SettingsRows>
      <SettingsRow icon={Palette} title="App theme" description="Colours for this device. Terminals follow unless you choose a separate theme." browser={((window as { legatoShell?: { theme?: number } }).legatoShell?.theme ?? 0) < 1} control={<AppThemePicker />} />
      <SettingsRow icon={SquareTerminal} title="Terminal theme" description="Follow the app theme, or choose separate colours for agents' terminals." browser control={<TerminalThemePicker />} />
      <SettingsRow icon={Zap} title="Glitch effect" description="A short glitch animation when a terminal syncs or you switch agents." browser
        control={<Toggle label="Glitch effect" on={glitch} onChange={() => { const next = !glitch; setGlitch(next); store("legato:glitch-effect", next ? "true" : "false") }} />} />
      <SettingsRow icon={Sparkles} title="Animated agent activity" description="A light that runs around a card while its agent works." browser
        control={<Toggle label="Animated agent activity" on={animated} onChange={() => { const next = !animated; setAnimated(next); setAnimatedAgentActivityEnabled(next) }} />} />
    </SettingsRows>
  </div>
}

// The agent switch key and prompt detection, kept in this browser.
export function KeyboardCard() {
  const [detection, setDetection] = useState(() => stored("legato:prompt-detection", "true") !== "false")
  const [modifier, setModifier] = useState(() => stored("legato:switch-modifier", /Mac|iPhone|iPad/.test(navigator.platform) ? "Alt" : "Control"))
  return <div id="keyboard">
    <SettingsRows>
      <SettingsRow icon={ScanSearch} title="Prompt detection" description="Find tool approval and plan prompts in a terminal agent's output and offer them as buttons." browser
        control={<Toggle label="Prompt detection" on={detection} onChange={() => { const next = !detection; setDetection(next); store("legato:prompt-detection", next ? "true" : "false") }} />} />
      <SettingsRow icon={Keyboard} title="Agent switch key" description="Hold this key and press a number to switch agents." browser
        control={(
          <select aria-label="Agent switch key" value={modifier} onChange={(e) => { setModifier(e.target.value); store("legato:switch-modifier", e.target.value) }}
            className="min-h-9 rounded-lg border border-lw-line-strong bg-lw-raised px-2 text-sm text-lw-text outline-none focus:border-lw-accent">
            <option value="Control">Ctrl</option>
            <option value="Alt">Alt / Option</option>
            <option value="Meta">Meta / Cmd</option>
          </select>
        )} />
    </SettingsRows>
  </div>
}

// The tint: its own picker, with the registry's source beside it.
export function TintCard({ tint, setting, onChanged }: { tint?: string; setting?: ConfigSetting; onChanged: () => void }) {
  return <div id="setting-web.tint">
    <SettingsRows>
      <SettingsRow icon={Palette} title={setting?.title ?? "Tint"}
        description="A colour for this Legato, along the top of the window and down the rail. Every browser and phone that uses it shows it."
        control={<div className="flex flex-col items-end gap-1">
          <TintPicker tint={tint} pinned={setting?.pinned} onChanged={onChanged} />
          {setting && <SettingSource setting={setting} />}
        </div>} />
    </SettingsRows>
  </div>
}

export function ManagedClonesCard() {
  return <SettingsCard id="job-clones" icon={FolderGit2} title="Managed clones" description="View and remove clones Legato made for agents on the Remote page.">
    <Link to="/remote" className={quietButton}>Open Remote</Link>
  </SettingsCard>
}

export function BoardColumnsCard() {
  return <SettingsCard id="list-board.columns" icon={Server} title="Board columns" description="Column names and ticket status mappings come from board.columns in config.yaml.">
    <p className="mb-3 text-sm text-lw-text-2">Edit the columns in this server’s config.yaml, then run <code>legato columns sync</code> on that server.</p>
    <Link to="/board" className={quietButton}>Open board</Link>
  </SettingsCard>
}

// This browser's servers: the one it shows, and the others it knows.
export function ServersCard() {
  const { baseUrl, servers, addServer, removeServer, setActiveServer } = useServer()
  const [name, setName] = useState("")
  const [url, setUrl] = useState("")
  const isActive = (u: string) => (u ? baseUrl === u : !baseUrl)
  const add = () => {
    const n = name.trim(), u = url.trim().replace(/\/$/, "")
    if (!n || !u) return
    addServer(n, u)
    setName("")
    setUrl("")
  }
  return <SettingsCard id="servers" icon={Server} title="Servers" browser description="The Legato server this browser shows. Switch to another, or add one by its address.">
    <div className="divide-y divide-lw-line overflow-hidden rounded-lg border border-lw-line">
      {[{ name: "Local", url: "" }, ...servers].map((s) => (
        <div key={s.url || "local"} className={`flex items-center gap-3 px-3 ${isActive(s.url) ? "bg-lw-selected" : ""}`}>
          <button onClick={() => setActiveServer(s.url)} className="flex min-h-12 min-w-0 flex-1 items-center gap-3 text-left">
            <span className={`h-2 w-2 shrink-0 rounded-full ${isActive(s.url) ? "bg-lw-live" : "bg-lw-line-strong"}`} />
            <div className="min-w-0 flex-1">
              <p className="text-sm text-lw-text">{s.name}</p>
              <p className="truncate font-lw-mono text-xs text-lw-faint">{s.url || window.location.origin}</p>
            </div>
            {isActive(s.url) && <span className="font-lw-mono text-[10px] uppercase tracking-wide text-lw-live">in use</span>}
          </button>
          {s.url && (
            <button onClick={() => removeServer(s.url)} title="Remove server" aria-label={`Remove ${s.name}`}
              className="flex h-10 w-10 items-center justify-center rounded-lg text-lw-faint transition-colors hover:bg-lw-raised hover:text-lw-danger">
              <Trash2 size={14} />
            </button>
          )}
        </div>
      ))}
    </div>
    <div className="mt-3 grid grid-cols-1 gap-2 sm:grid-cols-[8rem_minmax(0,1fr)_auto]">
      <input type="text" value={name} onChange={(e) => setName(e.target.value)} placeholder="Name" aria-label="Server name"
        className="min-h-10 rounded-lg border border-lw-line-strong bg-lw-raised px-3 text-sm text-lw-text outline-none placeholder:text-lw-faint focus:border-lw-accent" />
      <input type="text" value={url} onChange={(e) => setUrl(e.target.value)} placeholder="https://hostname:3080" aria-label="Server address"
        className="min-h-10 rounded-lg border border-lw-line-strong bg-lw-raised px-3 font-lw-mono text-sm text-lw-text outline-none placeholder:text-lw-faint focus:border-lw-accent" />
      <button onClick={add} disabled={!name.trim() || !url.trim()} className={quietButton}><Plus size={14} />Add</button>
    </div>
  </SettingsCard>
}

export function PairPhoneCard({ onPair }: { onPair: () => void }) {
  return <SettingsCard id="pair-phone" icon={Smartphone} title="Pair a phone" description="Pair the Legato app with this server by typing four words, or scanning a code.">
    <button onClick={onPair} className={primaryButton}><Smartphone size={14} />Pair a device</button>
  </SettingsCard>
}

export function PairBrowserCard({ onScan, scanned }: { onScan: () => void; scanned: string }) {
  return <SettingsCard id="pair-browser" icon={QrCode} title="Pair this browser"
    description={<>Scan the QR code another server shows with <code className="rounded bg-lw-raised px-1 font-lw-mono text-lw-text-2">legato pair</code>, and add it to this browser's servers.</>}>
    <button onClick={onScan} className={quietButton}><QrCode size={14} />Scan QR code</button>
    {scanned && <p role="status" className="mt-2 text-sm text-lw-live">{scanned}</p>}
  </SettingsCard>
}

export function TLSCard({ baseUrl, available }: { baseUrl: string; available?: boolean }) {
  return <SettingsCard id="tls" icon={ShieldCheck} title="TLS certificate" description="Install this server's CA certificate on a device to trust its HTTPS address and install the web app.">
    <ul className="mb-3 flex flex-col gap-1 text-xs text-lw-text-2">
      <li><span className="text-lw-text">Android:</span> Settings → Security → Install a certificate → CA certificate.</li>
      <li><span className="text-lw-text">iOS:</span> install the profile, then turn on full trust in Settings → General → About → Certificate Trust Settings.</li>
    </ul>
    {available ? (
      <a href={baseUrl ? `${baseUrl}/api/ca-cert` : "/api/ca-cert"} download="legato-ca.pem" className={quietButton + " w-fit"}><Download size={14} />Download CA certificate</a>
    ) : (
      <p className="text-xs text-lw-faint">This server uses a certificate of its own (web.tls.cert), so there is no CA to download.</p>
    )}
  </SettingsCard>
}

// Rebuilds the web UI under hot reload; shown only when the server runs so.
export function DevelopmentCard({ baseUrl }: { baseUrl: string }) {
  const { addToast } = useToast()
  const [starting, setStarting] = useState(false)
  const build = async () => {
    if (starting) return
    setStarting(true)
    sessionStorage.setItem("legato:post-rebuild-redirect", "/agents")
    try {
      const response = await apiFetch(baseUrl, "/api/hot-reload/build", { method: "POST" })
      if (!response.ok) throw new Error(await response.text().catch(() => "Failed to start full rebuild"))
      addToast("Full rebuild started", "success")
    } catch (error) {
      addToast(error instanceof Error ? error.message : "Failed to start full rebuild", "error")
    }
    setStarting(false)
  }
  return <div id="development">
    <SettingsCard icon={RefreshCw} title="Rebuild web application" description="This Legato runs from a checkout under hot reload. Build and embed the latest web UI, then hot reload Legato. This connection drops for a moment.">
      <button onClick={build} disabled={starting} className={primaryButton + " disabled:cursor-wait"}>
        <RefreshCw size={14} className={starting ? "animate-spin" : ""} />
        {starting ? "Starting rebuild…" : "Rebuild and hot reload"}
      </button>
    </SettingsCard>
  </div>
}
