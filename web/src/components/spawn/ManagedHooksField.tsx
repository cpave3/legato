import { fieldLabel } from "./styles"
import type { ManagedHooksChoice } from "./useManagedHooks"

// The managed hooks the agent would be given, each ticked; unticking one
// starts this agent without it. Nothing when none would be.
export function ManagedHooksField({ choice }: { choice: ManagedHooksChoice }) {
  if (choice.hooks.length === 0) return null
  return (
    <fieldset className="flex flex-col gap-1.5">
      <legend className={`${fieldLabel} mb-1.5`}>Managed hooks</legend>
      <div className="flex flex-wrap gap-1.5">
        {choice.hooks.map((hook) => {
          const on = choice.isOn(hook.name)
          return (
            <label
              key={hook.name}
              title={hook.description}
              className={`flex min-h-11 cursor-pointer items-center gap-2 rounded-[10px] border px-3 text-sm transition-colors sm:min-h-0 sm:rounded-md sm:px-2.5 sm:py-1.5 sm:text-xs ${
                on ? "border-lw-accent bg-lw-selected text-lw-text" : "border-lw-line-strong bg-lw-ground text-lw-text-2 hover:border-lw-faint"
              }`}
            >
              <input type="checkbox" aria-label={hook.name} checked={on} onChange={() => choice.toggle(hook.name)} className="accent-lw-accent" />
              {hook.name}
            </label>
          )
        })}
      </div>
    </fieldset>
  )
}
