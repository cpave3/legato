import { useMemo, useState } from "react"
import { useNavigate } from "react-router-dom"
import { mapTags, tagView, type WorkflowMap } from "../../lib/workflowMap"
import { cn } from "../../lib/utils"
import { LazyWorkflowMapCanvas } from "./LazyWorkflowMapCanvas"

// WorkflowMapView is the Workflows tab's map (LEG-710): every workflow as a
// box and a line from each workflow to each it calls, filtered by tag.
export function WorkflowMapView({ map }: { map: WorkflowMap }) {
  const navigate = useNavigate()
  const [tag, setTag] = useState("")
  const tags = useMemo(() => mapTags(map), [map])
  const view = useMemo(() => tagView(map, tags.includes(tag) ? tag : ""), [map, tag, tags])
  const chip = (value: string, label: string) => <button key={value || "all"} type="button" aria-pressed={tag === value} onClick={() => setTag(value)}
    className={cn("inline-flex min-h-9 items-center rounded-full border px-3 text-[13px]", tag === value ? "border-lw-accent bg-lw-selected text-lw-text" : "border-lw-line-strong bg-lw-surface text-lw-text-2 hover:text-lw-text")}>{label}</button>
  return <section aria-label="Workflow map" className="mt-4 flex flex-col gap-3">
    {tags.length > 0 && <div role="group" aria-label="Tag" className="flex flex-wrap gap-2">
      {chip("", "All")}
      {tags.map(item => chip(item, item))}
    </div>}
    <p className="text-xs text-lw-text-2">Lines run from a workflow to each one it calls, dashed for one of an input's choices. Hover or tap a workflow to light its calls; click to open it.</p>
    <div className="h-[70vh] min-h-[24rem] overflow-hidden rounded-xl border border-lw-line bg-lw-ground">
      {view.map.nodes.length > 0
        ? <LazyWorkflowMapCanvas map={view.map} context={view.context} onOpen={name => navigate(`/workflows/${encodeURIComponent(name)}`)} />
        : <p className="p-4 text-sm text-lw-text-2">No workflows to map</p>}
    </div>
  </section>
}
