import type { ReactNode } from "react"

// A box that loosens what the agent may do, drawn as a warning once ticked.
function RiskToggle({ checked, onChange, title, children }: { checked: boolean; onChange: (checked: boolean) => void; title: string; children: ReactNode }) {
  return (
    <label className={`flex min-h-11 cursor-pointer items-start gap-2.5 rounded-[10px] border px-3 py-2.5 text-sm sm:min-h-0 sm:rounded-md sm:text-xs ${checked ? "border-lw-needs/50 bg-lw-needs-soft text-lw-text" : "border-lw-line-strong bg-lw-ground text-lw-text-2"}`}>
      <input type="checkbox" checked={checked} onChange={(event) => onChange(event.target.checked)} className="mt-0.5 accent-lw-accent" />
      <span>
        <span className="font-medium text-lw-text">{title}</span>
        <span className="block text-lw-faint">{children}</span>
      </span>
    </label>
  )
}

interface SandboxTogglesProps {
  // Legato's sandbox is on, so an agent can be started outside it.
  sandboxed: boolean
  unsandboxed: boolean
  setUnsandboxed: (value: boolean) => void
  // Only for an agent in the sandbox, with Docker in it.
  hostNetworkOffered: boolean
  hostNetwork: boolean
  setHostNetwork: (value: boolean) => void
}

// Out of Legato's sandbox for this one agent, and the host's network for
// its containers: both asked afresh each time.
export function SandboxToggles({ sandboxed, unsandboxed, setUnsandboxed, hostNetworkOffered, hostNetwork, setHostNetwork }: SandboxTogglesProps) {
  return (
    <>
      {sandboxed && (
        <RiskToggle checked={unsandboxed} onChange={setUnsandboxed} title="Run without sandbox">
          This agent sees your whole home folder and its credentials, so folders can be given to it while it runs.
        </RiskToggle>
      )}
      {hostNetworkOffered && (
        <RiskToggle checked={hostNetwork} onChange={setHostNetwork} title="Allow the host network for its containers">
          Its containers may use network_mode: host, and reach what this machine reaches, Legato's own local services included.
        </RiskToggle>
      )}
    </>
  )
}
