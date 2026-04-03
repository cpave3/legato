import { useEffect, useState } from "react"
import { ShieldCheck, Download, Zap } from "lucide-react"

interface SettingsData {
  ca_cert_available: boolean
}

export function SettingsPage() {
  const [settings, setSettings] = useState<SettingsData | null>(null)
  const [glitchEnabled, setGlitchEnabled] = useState(() => {
    return localStorage.getItem("legato:glitch-effect") !== "false"
  })

  useEffect(() => {
    fetch("/api/settings")
      .then((r) => r.json())
      .then(setSettings)
      .catch(() => setSettings(null))
  }, [])

  const toggleGlitch = () => {
    const next = !glitchEnabled
    setGlitchEnabled(next)
    localStorage.setItem("legato:glitch-effect", next ? "true" : "false")
  }

  return (
    <div className="flex-1 overflow-y-auto p-6">
      <h1 className="text-lg font-semibold text-zinc-100 mb-6">Settings</h1>

      <section className="max-w-lg mb-8">
        <h2 className="text-sm font-medium uppercase tracking-wider text-zinc-500 mb-3">
          Appearance
        </h2>
        <div className="rounded-lg border border-zinc-800 bg-zinc-900 p-4">
          <div className="flex items-center justify-between">
            <div className="flex items-center gap-3">
              <Zap size={20} className="text-indigo-400 shrink-0" />
              <div>
                <p className="text-sm text-zinc-300">Glitch effect</p>
                <p className="text-xs text-zinc-500">
                  Sci-fi glitch animation on terminal sync events
                </p>
              </div>
            </div>
            <button
              onClick={toggleGlitch}
              className={`relative h-6 w-11 rounded-full transition-colors ${glitchEnabled ? "bg-indigo-600" : "bg-zinc-700"}`}
            >
              <span
                className={`absolute top-0.5 left-0.5 h-5 w-5 rounded-full bg-white transition-transform ${glitchEnabled ? "translate-x-5" : "translate-x-0"}`}
              />
            </button>
          </div>
        </div>
      </section>

      <section className="max-w-lg">
        <h2 className="text-sm font-medium uppercase tracking-wider text-zinc-500 mb-3">
          TLS Certificate
        </h2>
        <div className="rounded-lg border border-zinc-800 bg-zinc-900 p-4">
          <div className="flex items-start gap-3">
            <ShieldCheck size={20} className="text-indigo-400 mt-0.5 shrink-0" />
            <div className="flex flex-col gap-2">
              <p className="text-sm text-zinc-300">
                Install the CA certificate on your device to trust this server
                and enable PWA installation.
              </p>
              <p className="text-xs text-zinc-500">
                On iOS: install the profile, then enable full trust in Settings
                &rarr; General &rarr; About &rarr; Certificate Trust Settings.
              </p>
              <p className="text-xs text-zinc-500">
                On Android: Settings &rarr; Security &rarr; Install a
                certificate &rarr; CA certificate.
              </p>
              {settings?.ca_cert_available ? (
                <a
                  href="/api/ca-cert"
                  download="legato-ca.pem"
                  className="mt-2 inline-flex w-fit items-center gap-2 rounded-md bg-indigo-600 px-3 py-1.5 text-sm font-medium text-white transition-colors hover:bg-indigo-500"
                >
                  <Download size={14} />
                  Download CA Certificate
                </a>
              ) : (
                <p className="mt-1 text-xs text-zinc-600">
                  No auto-generated CA certificate available. Using custom TLS
                  config.
                </p>
              )}
            </div>
          </div>
        </div>
      </section>
    </div>
  )
}
