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
    <section id="mqtt" aria-label="Home Assistant over MQTT" className="scroll-mt-20 lg:scroll-mt-6">
      <div className="mb-3 space-y-2 text-[13px] leading-relaxed text-lw-text-2">
        <p>Publishes ten read-only entities to Home Assistant over MQTT, and, if you allow it, lets Home Assistant set this Legato's theme. Set the broker and login before you turn either on.</p>
        <details>
          <summary className="w-fit cursor-pointer text-lw-link hover:underline">Broker, login, theme control and privacy</summary>
          <div className="mt-2 space-y-2 text-xs">
            <p>Theme control permits every broker-authorized writer on this server’s theme command topic to change its preferred theme. Only clients using Follow server adopt it.</p>
            <p>Use mqtts:// for verified TLS (port 8883 by default), or mqtt:// for unencrypted TCP (port 1883). Do not include a login in the URL.</p>
            <p>The login is a credential's name, not a password; empty means anonymous access. Store it with legato secret add and type userpass, or ask the Reeve for a secure form; never send it in conversation. Add a host-only credential with profile mqtt, type userpass and a secret reference in Settings › Security › Credentials.</p>
            <p>Discovery uses homeassistant by default; match Home Assistant’s prefix. Leave base topic empty for legato/&lt;persistent-instance-id&gt; and device name empty for Legato &lt;hostname&gt;. Independent instances need different base topics.</p>
            <p>For theme control, allow Legato to subscribe to &lt;base-topic&gt;/theme/set and allow only trusted controller accounts to publish there. Without theme control, keep the existing read-only ACLs. Legato rejects retained theme commands and refreshes the select after reconnecting.</p>
            <p>Reeve agent counts and Attention details are off by default. Details add up to 20 generated signal labels, not task titles, paths or prompts. Retained MQTT messages and Home Assistant history can outlive Legato: disabling MQTT or changing topic roots does not remove old retained discovery or state; remove those with your broker’s tools. Turning details off clears retained attributes after restart.</p>
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
