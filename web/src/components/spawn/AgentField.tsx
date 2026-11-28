import { accountPlaceholder } from "../../lib/spawnAgent"
import { fieldLabel, focusRing } from "./styles"
import type { AgentChoice } from "./useAgentChoice"

// The agent tool: the usage profile's default, each tool, and a shell.
export function AgentField({ agent }: { agent: AgentChoice }) {
  return (
    <div className="flex flex-col gap-1.5">
      <span className={fieldLabel}>Agent</span>
      <div role="radiogroup" aria-label="Agent" className="flex rounded-[10px] border border-lw-line-strong bg-lw-ground p-[3px] sm:rounded-md sm:p-0.5">
        {agent.options.map((option) => {
          const checked = option.value === agent.agentKind
          return (
            <button
              key={option.value}
              type="button"
              role="radio"
              aria-checked={checked}
              onClick={() => agent.pickAgent(option.value)}
              // Each takes the room its own label needs, then a share of what is
              // left: four equal parts cut the longest label short.
              className={`h-10 min-w-0 flex-auto truncate rounded-lg px-2 text-sm transition-colors sm:h-[30px] sm:rounded sm:text-[13px] ${
                checked ? "bg-lw-selected font-medium text-lw-text" : "text-lw-text-2 hover:text-lw-text"
              }`}
            >
              {option.label}
              {option.note && <> <span className="hidden font-normal text-lw-faint sm:inline">{option.note}</span></>}
            </button>
          )
        })}
      </div>
    </div>
  )
}

// Claude Code's account. A resumed session keeps the account it was saved
// with, so the list waits until a new session is asked for.
export function AccountField({ agent }: { agent: AgentChoice }) {
  const { agentKind, resuming } = agent
  return (
    <label className="flex flex-col gap-1.5">
      <span className={fieldLabel}>Account</span>
      <select aria-label="Account" value={agent.account} onChange={(event) => agent.setAccount(event.target.value)} disabled={resuming}
        className={`h-[46px] w-full min-w-0 rounded-[10px] border border-lw-line-strong bg-lw-ground px-3 text-base text-lw-text sm:h-9 sm:rounded-md sm:text-sm ${focusRing}`}>
        <option value="">{accountPlaceholder(resuming, agentKind)}</option>
        {!agentKind && <option value="default">Default account</option>}
        {agent.adapters?.accounts?.map((item) => <option key={item.name} value={item.name}>{item.label || item.name}</option>)}
      </select>
    </label>
  )
}
