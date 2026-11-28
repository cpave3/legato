import { fieldLabel } from "./styles"
import type { AgentChoice } from "./useAgentChoice"

// Whether to resume the tool's saved session for the task, offered when it
// has one. name groups the two radios.
export function SessionPolicyField({ agent, name }: { agent: AgentChoice; name: string }) {
  const { toolLabel, sessionPolicy } = agent
  const choices = [
    { value: "resume", label: "Resume previous session", note: `Continue with the previous ${toolLabel} context.` },
    { value: "new", label: "Start new session", note: "Start fresh for this Legato task." },
  ] as const
  return (
    <fieldset className="flex flex-col gap-1.5">
      <legend className={`${fieldLabel} mb-1.5`}>Existing {toolLabel} session</legend>
      <div className="overflow-hidden rounded-[10px] border border-lw-line-strong bg-lw-ground sm:rounded-md">
        {choices.map((choice) => (
          <label
            key={choice.value}
            className={`flex min-h-[54px] cursor-pointer items-center gap-3 border-b border-lw-line px-3.5 last:border-b-0 sm:min-h-0 sm:px-3 sm:py-2 ${sessionPolicy === choice.value ? "bg-lw-selected" : ""}`}
          >
            <input
              type="radio"
              name={name}
              value={choice.value}
              aria-label={choice.label}
              checked={sessionPolicy === choice.value}
              onChange={() => agent.setSessionPolicy(choice.value)}
              className="accent-lw-accent"
            />
            <span className="flex flex-col gap-px">
              <span className="font-medium">{choice.label}</span>
              <span className="text-xs text-lw-text-2">{choice.note}</span>
            </span>
          </label>
        ))}
      </div>
    </fieldset>
  )
}
