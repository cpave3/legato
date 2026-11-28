import { accessLabel, type Resource } from "../../lib/resources"
import { fieldLabel } from "./styles"
import type { ResourceChoice } from "./useResources"

function ResourceChip({ resource, on, onToggle }: { resource: Resource; on: boolean; onToggle: () => void }) {
  const paths = resource.paths.map((p) => `${p.path} (${accessLabel(p.access)})`).join("\n")
  return (
    <label
      title={paths}
      className={`flex min-h-11 cursor-pointer items-center gap-2 rounded-[10px] border px-3 text-sm transition-colors sm:min-h-0 sm:rounded-md sm:px-2.5 sm:py-1.5 sm:text-xs ${
        on ? "border-lw-accent bg-lw-selected text-lw-text" : "border-lw-line-strong bg-lw-ground text-lw-text-2 hover:border-lw-faint"
      }`}
    >
      <input type="checkbox" checked={on} onChange={onToggle} className="accent-lw-accent" />
      {resource.name}
      {resource.paths.some((p) => p.access === "write") && <span className="text-[11px] text-lw-faint">write</span>}
    </label>
  )
}

// The resources to give the agent, each a chip that names its paths.
export function ResourcesField({ choice }: { choice: ResourceChoice }) {
  return (
    <fieldset className="flex flex-col gap-1.5">
      <legend className={`${fieldLabel} mb-1.5`}>Resources</legend>
      <div className="flex flex-wrap gap-1.5">
        {choice.resources.map((resource) => (
          <ResourceChip key={resource.name} resource={resource} on={choice.picked.includes(resource.name)} onToggle={() => choice.toggle(resource.name)} />
        ))}
      </div>
    </fieldset>
  )
}
