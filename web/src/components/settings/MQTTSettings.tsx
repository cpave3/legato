import { Link } from "react-router-dom"
import type { ConfigSetting } from "../../lib/configSettings"
import { RegistrySettings } from "./RegistrySettings"
import { quietButton } from "./SettingsUI"

export function MQTTSettings({ settings, baseUrl, loading, error, onRetry }: {
  settings: ConfigSetting[]
  baseUrl: string
  loading: boolean
  error: Error | null
  onRetry: () => void
}) {
  return (
    <section id="mqtt" aria-labelledby="mqtt-title" className="scroll-mt-20 lg:scroll-mt-6">
      <h3 id="mqtt-title" className="text-sm font-medium text-lw-text">Home Assistant over MQTT</h3>
      <div className="mt-1 mb-3 space-y-2 text-xs leading-relaxed text-lw-text-2">
        <p>Publishes ten read-only aggregate entities. Off by default; Home Assistant cannot send commands.</p>
        <p>Set the broker and credential before enabling. Apply text edits explicitly. All changes require a restart; restart when ready from Settings → Server and maintenance.</p>
        <p>The credential is a name, not a password. Empty means anonymous access. Retained MQTT messages and Home Assistant history can outlive Legato.</p>
        <details>
          <summary className="w-fit cursor-pointer text-lw-link hover:underline">Broker, credentials and privacy</summary>
          <div className="mt-2 space-y-2">
            <p>Use mqtts:// for verified TLS (port 8883 by default), or mqtt:// for unencrypted TCP (port 1883). Do not include a login in the URL.</p>
            <p>For a login, ask the Reeve to open a secure secret form for username/password JSON, or use legato secret add with type userpass. Never send the login in conversation. Add a host-only credential with profile mqtt, type userpass and a secret reference in Settings → Security → Credentials.</p>
            <p>Discovery uses homeassistant by default; match Home Assistant’s prefix. Leave base topic empty for legato/&lt;persistent-instance-id&gt; and device name empty for Legato &lt;hostname&gt;. Independent instances need different base topics.</p>
            <p>Reeve agent counts and Attention details are off by default. Details add up to 20 generated signal labels, not task titles, paths or prompts. Disabling MQTT or changing topic roots does not remove old retained discovery or state; remove those with your broker’s tools. Turning details off clears retained attributes after restart. Remove unwanted Home Assistant history separately.</p>
            <p>Values pinned by config.yaml must be changed there or moved to the database. Moving a value restarts Legato.</p>
          </div>
        </details>
      </div>
      <Link to="/settings#list-credentials" className={quietButton + " mb-3"}>Edit MQTT credentials</Link>
      {loading && <p role="status" className="text-xs text-lw-text-2">Loading MQTT settings…</p>}
      {error && <div className="mb-3 space-y-2">
        <p role="alert" className="text-xs text-lw-error">Could not load MQTT settings: {error.message}</p>
        <button type="button" onClick={onRetry} className={quietButton}>Retry settings</button>
      </div>}
      <RegistrySettings settings={settings} baseUrl={baseUrl} prefixes={["integrations.mqtt"]} />
    </section>
  )
}
