import { useCallback, useEffect, useState } from "react"
import { SettingsCard } from "./settings/SettingsUI"
import { Fingerprint } from "lucide-react"
import { removeDevice, type DeviceList } from "../lib/devices"
import { useDevices } from "../hooks/useDevices"
import { onShellEnroll, shellApprovals } from "../lib/mobileShell"
import { enrollPasskey, passkeysSupported } from "../lib/passkeys"

const levelLabel: Record<string, string> = { strongbox: "StrongBox", tee: "secure hardware", "synced passkey": "Synced passkey", passkey: "Passkey" }

// DevicesSettings lists the phones and passkeys that sign approval
// decisions. Removing one is itself an approval to sign. Inside the Legato
// app it offers to enroll this phone; in a browser, a passkey for this page.
export function DevicesSettings({ baseUrl }: { baseUrl: string }) {
  // Shared with the tray; approvals_changed reads it again, so a device
  // allowed or removed on a phone shows here without a reload.
  const devicesQuery = useDevices({ baseUrl })
  const list: DeviceList | null = devicesQuery.data ?? null
  const [actionError, setError] = useState<string | null>(null)
  const error = actionError ?? devicesQuery.error?.message ?? null
  const [notice, setNotice] = useState<string | null>(null)
  const app = shellApprovals()
  const { refetch } = devicesQuery
  const load = useCallback(async () => { await refetch() }, [refetch])
  // Attention links here after a device reset; come into view once listed.
  useEffect(() => {
    if (list !== null && window.location.hash === "#devices") {
      document.getElementById("devices")?.scrollIntoView?.({ block: "start" })
    }
  }, [list])
  useEffect(() => onShellEnroll((result) => {
    if (result.status === "enrolled") setNotice("This phone now signs approvals.")
    else if (result.status === "pending") setNotice("Approve this phone on a phone that is already enrolled.")
    else if (result.status === "error") setError(result.error || "The phone could not enroll.")
    void load()
  }), [load])

  // A passkey for this computer, in the browser's or a password manager's
  // store; it belongs to this page's host name.
  const [addingPasskey, setAddingPasskey] = useState(false)
  const addPasskey = async () => {
    setError(null)
    setNotice(null)
    setAddingPasskey(true)
    try {
      const device = await enrollPasskey(baseUrl, `Passkey on ${window.location.hostname}`)
      setNotice(device.status === "pending"
        ? "Approve adding it on your phone: the approval names this page."
        : "This computer's passkey now signs approvals.")
      await load()
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : String(reason))
    } finally {
      setAddingPasskey(false)
    }
  }

  const remove = async (id: string, name: string) => {
    setError(null)
    setNotice(null)
    try {
      const result = await removeDevice(baseUrl, id)
      setNotice(result.approval_id ? `Approve removing ${name} on your phone.` : `${name} was removed.`)
      await load()
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : String(reason))
    }
  }

  const devices = list?.devices ?? []
  return (
    <SettingsCard id="devices" label="Devices" icon={Fingerprint} title="Approval devices" description="Phones and passkeys that sign approval decisions after your fingerprint, PIN or password manager. Approvals that need a signature can be allowed only from them, so an agent with this server's token cannot allow them. A passkey belongs to the page it was made on.">
      <div className="flex flex-col gap-3">
        {error && <p role="alert" className="rounded border border-red-900 bg-red-950/60 px-2 py-1.5 text-xs text-red-300">{error}</p>}
        {notice && <p role="status" className="rounded border border-indigo-900 bg-indigo-950/50 px-2 py-1.5 text-xs text-indigo-200">{notice}</p>}
        {list && !list.enrolled && (
          <p className="rounded border border-amber-900/70 bg-amber-950/40 px-2 py-1.5 text-xs text-amber-200">
            No phone signs approvals yet. Until one does, any client with this server's token can allow every approval, including an agent on this machine.
            {!app && " To enroll your phone, open this server in the Legato app and come back here."}
          </p>
        )}
        {devices.length > 0 && (
          <ul className="divide-y divide-zinc-800 rounded border border-zinc-800">
            {devices.map((d) => (
              <li key={d.id} className="flex items-center gap-3 px-3 py-2">
                <div className="flex min-w-0 flex-1 flex-col gap-0.5">
                  <div className="flex flex-wrap items-center gap-2">
                    <span className="text-sm text-zinc-200">{d.name}</span>
                    {d.security_level && <span className="rounded bg-zinc-800 px-1.5 py-0.5 text-[10px] text-zinc-400">{levelLabel[d.security_level] ?? d.security_level}</span>}
                    {d.status === "pending" && <span className="rounded bg-amber-950 px-1.5 py-0.5 text-[10px] text-amber-300">waiting for approval</span>}
                  </div>
                  {d.origin && <span className="truncate font-mono text-[11px] text-zinc-500">{d.origin}</span>}
                  <span className="text-xs text-zinc-500">{d.last_used_at ? `Last signed ${new Date(d.last_used_at).toLocaleString()}` : "Not used yet"}</span>
                </div>
                <button onClick={() => void remove(d.id, d.name)} className="min-h-11 shrink-0 rounded px-3 text-xs text-zinc-400 hover:bg-zinc-800 hover:text-zinc-200">
                  Remove
                </button>
              </li>
            ))}
          </ul>
        )}
        {!app && passkeysSupported() && (
          <button disabled={addingPasskey} onClick={() => void addPasskey()} className="min-h-11 w-fit rounded-lg border border-zinc-700 px-4 text-sm font-medium text-zinc-200 hover:bg-zinc-800 disabled:opacity-50">
            {addingPasskey ? "Waiting for your passkey…" : "Add a passkey for this computer"}
          </button>
        )}
        {app && (
          <button onClick={() => { setError(null); setNotice(null); app.enroll() }} className="min-h-11 rounded-lg bg-indigo-600 px-4 text-sm font-medium text-white hover:bg-indigo-500">
            Sign approvals with this phone
          </button>
        )}
      </div>
    </SettingsCard>
  )
}
