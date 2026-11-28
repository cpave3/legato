import { useCallback, useEffect, useState } from "react"
import { SettingsCard } from "./settings/SettingsUI"
import { Fingerprint } from "lucide-react"
import { removeDevice, type DeviceList } from "../lib/devices"
import { useDevices } from "../hooks/useDevices"
import { onShellEnroll, shellApprovals } from "../lib/mobileShell"
import { enrollPasskey, passkeysSupported } from "../lib/passkeys"
import { reasonText } from "../lib/nativeAgentView"

const levelLabel: Record<string, string> = { strongbox: "StrongBox", tee: "secure hardware", "synced passkey": "Synced passkey", passkey: "Passkey" }

type Device = DeviceList["devices"][number]
type ShellEnrollResult = Parameters<Parameters<typeof onShellEnroll>[0]>[0]

// DevicesSettings lists the phones and passkeys that sign approval
// decisions. Removing one is itself an approval to sign. Inside the Legato
// app it offers to enroll this phone; in a browser, a passkey for this page.
export function DevicesSettings({ baseUrl }: { baseUrl: string }) {
  // Shared with the tray; approvals_changed reads it again, so a device
  // allowed or removed on a phone shows here without a reload.
  const devicesQuery = useDevices({ baseUrl })
  const list: DeviceList | null = devicesQuery.data ?? null
  const app = shellApprovals()
  const { refetch } = devicesQuery
  const load = useCallback(async () => { await refetch() }, [refetch])
  const actions = useDeviceActions(baseUrl, load)
  const error = actions.error ?? devicesQuery.error?.message ?? null
  useComeIntoView(list)

  return (
    <SettingsCard id="devices" label="Devices" icon={Fingerprint} title="Approval devices" description="Phones and passkeys that sign approval decisions after your fingerprint, PIN or password manager. Approvals that need a signature can be allowed only from them, so an agent with this server's token cannot allow them. A passkey belongs to the page it was made on.">
      <div className="flex flex-col gap-3">
        <DeviceMessages error={error} notice={actions.notice} />
        {list && !list.enrolled && <NotEnrolledWarning inApp={!!app} />}
        <DeviceRows devices={list?.devices ?? []} onRemove={(d) => void actions.remove(d.id, d.name)} />
        {app ? <EnrollThisDevice onEnroll={() => { actions.clear(); app.enroll() }} />
          : passkeysSupported() && <AddPasskey adding={actions.addingPasskey} onAdd={() => void actions.addPasskey()} />}
      </div>
    </SettingsCard>
  )
}

// useComeIntoView scrolls to the card once the devices are listed when the
// address names it, as Attention's link after a device reset does.
function useComeIntoView(list: DeviceList | null) {
  useEffect(() => {
    if (list !== null && window.location.hash === "#devices") {
      document.getElementById("devices")?.scrollIntoView?.({ block: "start" })
    }
  }, [list])
}

// useDeviceActions adds a passkey, removes a device and follows the app's
// enrollment, each saying how it went.
function useDeviceActions(baseUrl: string, load: () => Promise<void>) {
  const [error, setError] = useState<string | null>(null)
  const [notice, setNotice] = useState<string | null>(null)
  const [addingPasskey, setAddingPasskey] = useState(false)
  const clear = () => { setError(null); setNotice(null) }

  useEffect(() => onShellEnroll((result) => {
    shellEnrollMessage(result, setNotice, setError)
    void load()
  }), [load])

  // A passkey for this computer, in the browser's or a password manager's
  // store; it belongs to this page's host name.
  const addPasskey = async () => {
    clear()
    setAddingPasskey(true)
    try {
      const device = await enrollPasskey(baseUrl, `Passkey on ${window.location.hostname}`)
      setNotice(device.status === "pending"
        ? "Approve adding it on your phone: the approval names this page."
        : "This computer's passkey now signs approvals.")
      await load()
    } catch (reason) {
      setError(reasonText(reason))
    } finally {
      setAddingPasskey(false)
    }
  }

  const remove = async (id: string, name: string) => {
    clear()
    try {
      const result = await removeDevice(baseUrl, id)
      setNotice(result.approval_id ? `Approve removing ${name} on your phone.` : `${name} was removed.`)
      await load()
    } catch (reason) {
      setError(reasonText(reason))
    }
  }
  return { error, notice, addingPasskey, clear, addPasskey, remove }
}

// shellEnrollMessage says what the app reported of enrolling this phone; a
// cancelled enrollment says nothing.
function shellEnrollMessage(result: ShellEnrollResult, setNotice: (text: string) => void, setError: (text: string) => void) {
  if (result.status === "enrolled") setNotice("This phone now signs approvals.")
  else if (result.status === "pending") setNotice("Approve this phone on a phone that is already enrolled.")
  else if (result.status === "error") setError(result.error || "The phone could not enroll.")
}

function DeviceMessages({ error, notice }: { error: string | null; notice: string | null }) {
  return <>
    {error && <p role="alert" className="rounded border border-red-900 bg-red-950/60 px-2 py-1.5 text-xs text-red-300">{error}</p>}
    {notice && <p role="status" className="rounded border border-indigo-900 bg-indigo-950/50 px-2 py-1.5 text-xs text-indigo-200">{notice}</p>}
  </>
}

function DeviceRows({ devices, onRemove }: { devices: Device[]; onRemove: (device: Device) => void }) {
  if (devices.length === 0) return null
  return (
    <ul className="divide-y divide-zinc-800 rounded border border-zinc-800">
      {devices.map((d) => <DeviceRow key={d.id} device={d} onRemove={() => onRemove(d)} />)}
    </ul>
  )
}

function NotEnrolledWarning({ inApp }: { inApp: boolean }) {
  return (
    <p className="rounded border border-amber-900/70 bg-amber-950/40 px-2 py-1.5 text-xs text-amber-200">
      No phone signs approvals yet. Until one does, any client with this server's token can allow every approval, including an agent on this machine.
      {!inApp && " To enroll your phone, open this server in the Legato app and come back here."}
    </p>
  )
}

function DeviceRow({ device: d, onRemove }: { device: Device; onRemove: () => void }) {
  return (
    <li className="flex items-center gap-3 px-3 py-2">
      <div className="flex min-w-0 flex-1 flex-col gap-0.5">
        <div className="flex flex-wrap items-center gap-2">
          <span className="text-sm text-zinc-200">{d.name}</span>
          {d.security_level && <span className="rounded bg-zinc-800 px-1.5 py-0.5 text-[10px] text-zinc-400">{levelLabel[d.security_level] ?? d.security_level}</span>}
          {d.status === "pending" && <span className="rounded bg-amber-950 px-1.5 py-0.5 text-[10px] text-amber-300">waiting for approval</span>}
        </div>
        {d.origin && <span className="truncate font-mono text-[11px] text-zinc-500">{d.origin}</span>}
        <span className="text-xs text-zinc-500">{d.last_used_at ? `Last signed ${new Date(d.last_used_at).toLocaleString()}` : "Not used yet"}</span>
      </div>
      <button onClick={onRemove} className="min-h-11 shrink-0 rounded px-3 text-xs text-zinc-400 hover:bg-zinc-800 hover:text-zinc-200">
        Remove
      </button>
    </li>
  )
}

function AddPasskey({ adding, onAdd }: { adding: boolean; onAdd: () => void }) {
  return (
    <button disabled={adding} onClick={onAdd} className="min-h-11 w-fit rounded-lg border border-zinc-700 px-4 text-sm font-medium text-zinc-200 hover:bg-zinc-800 disabled:opacity-50">
      {adding ? "Waiting for your passkey…" : "Add a passkey for this computer"}
    </button>
  )
}

function EnrollThisDevice({ onEnroll }: { onEnroll: () => void }) {
  return (
    <button onClick={onEnroll} className="min-h-11 rounded-lg bg-indigo-600 px-4 text-sm font-medium text-white hover:bg-indigo-500">
      Sign approvals with this device
    </button>
  )
}
