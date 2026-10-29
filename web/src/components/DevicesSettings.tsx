import { useCallback, useEffect, useState } from "react"
import { Fingerprint } from "lucide-react"
import { listDevices, removeDevice, type DeviceList } from "../lib/devices"
import { onShellEnroll, shellApprovals } from "../lib/mobileShell"

const levelLabel: Record<string, string> = { strongbox: "StrongBox", tee: "secure hardware" }

// DevicesSettings lists the phones that sign approval decisions. Removing
// one is itself an approval to sign on a phone. Inside the Legato app it
// offers to enroll this phone.
export function DevicesSettings({ baseUrl }: { baseUrl: string }) {
  const [list, setList] = useState<DeviceList | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [notice, setNotice] = useState<string | null>(null)
  const app = shellApprovals()

  const load = useCallback(async () => {
    try {
      setList(await listDevices(baseUrl))
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : String(reason))
    }
  }, [baseUrl])

  useEffect(() => { void load() }, [load])
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
    <section id="devices" aria-label="Devices" className="max-w-lg mb-8 scroll-mt-4">
      <h2 className="text-sm font-medium uppercase tracking-wider text-zinc-500 mb-3">Devices</h2>
      <div className="rounded-lg border border-zinc-800 bg-zinc-900 p-4">
        <div className="flex items-start gap-3">
          <Fingerprint size={20} className="mt-0.5 shrink-0 text-indigo-400" />
          <div className="flex min-w-0 flex-1 flex-col gap-3">
            <p className="text-sm text-zinc-300">
              Phones that sign approval decisions with your fingerprint or PIN. Approvals that need a signature can be allowed only from them, so an agent with this server's token cannot allow them.
            </p>
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
                      <span className="text-xs text-zinc-500">{d.last_used_at ? `Last signed ${new Date(d.last_used_at).toLocaleString()}` : "Not used yet"}</span>
                    </div>
                    <button onClick={() => void remove(d.id, d.name)} className="min-h-11 shrink-0 rounded px-3 text-xs text-zinc-400 hover:bg-zinc-800 hover:text-zinc-200">
                      Remove
                    </button>
                  </li>
                ))}
              </ul>
            )}
            {app && (
              <button onClick={() => { setError(null); setNotice(null); app.enroll() }} className="min-h-11 rounded-lg bg-indigo-600 px-4 text-sm font-medium text-white hover:bg-indigo-500">
                Sign approvals with this phone
              </button>
            )}
          </div>
        </div>
      </div>
    </section>
  )
}
