import { useState } from "react"
import { Check, Copy, Download, GitBranch } from "lucide-react"
import { useServer } from "../../hooks/useServer"
import { apiFetch } from "../../lib/api"
import { branchDepths, taskStackDocumentPath, type TaskStack, type TaskStackBranch } from "../../lib/task-stacks"

const stateStyle: Record<TaskStackBranch["state"], string> = {
  pushed: "bg-emerald-950 text-emerald-300",
  unpushed: "bg-amber-950 text-amber-300",
  missing: "bg-red-950 text-red-300",
  invalid: "bg-red-950 text-red-300",
}

/** The stacks of branches a task's agents built with Staccato, each with
 * the document st graph import takes once every branch is pushed. */
export function TaskStacks({ taskId, stacks }: { taskId: string; stacks: TaskStack[] }) {
  if (stacks.length === 0) return <p className="py-4 text-center text-xs text-zinc-600">No stacks</p>
  return (
    <div className="space-y-3">
      {stacks.map((stack) => (
        <StackCard key={stack.clone_id} taskId={taskId} stack={stack} />
      ))}
    </div>
  )
}

function StackCard({ taskId, stack }: { taskId: string; stack: TaskStack }) {
  return (
    <article className="rounded border border-zinc-800 bg-zinc-950 p-3">
      <header className="flex flex-wrap items-baseline gap-x-2 gap-y-1">
        <span className="text-sm font-medium text-zinc-200">{stack.repo}</span>
        <span className="text-xs text-zinc-500">on {stack.root || "?"}</span>
        <span className="text-xs text-zinc-600">read {new Date(stack.read_at).toLocaleString()}</span>
        {!stack.live && <span className="rounded bg-zinc-800 px-1.5 text-[11px] text-zinc-400">clone removed</span>}
      </header>
      {stack.problem ? (
        <p className="mt-2 text-xs text-red-300">{stack.problem}</p>
      ) : (
        <ul aria-label={`Stack in ${stack.repo}`} className="mt-2 space-y-1">
          {branchDepths(stack).map(({ branch, depth }) => (
            <li
              key={branch.name}
              aria-level={depth}
              className="flex min-h-7 items-center gap-2 text-xs"
              style={{ paddingLeft: `${(depth - 1) * 16}px` }}
            >
              <GitBranch size={13} className="shrink-0 text-zinc-500" aria-hidden />
              <span className="truncate font-mono text-zinc-300">{branch.name}</span>
              <span className={`rounded px-1.5 text-[11px] ${stateStyle[branch.state] ?? "bg-zinc-800 text-zinc-400"}`}>{branch.state}</span>
              {branch.problem && <span className="text-zinc-500">{branch.problem}</span>}
            </li>
          ))}
        </ul>
      )}
      {stack.importable && stack.document_name ? (
        <TakeHome taskId={taskId} stack={stack} name={stack.document_name} />
      ) : (
        stack.reason && !stack.problem && <p className="mt-2 text-xs text-amber-300">Not ready to take home: {stack.reason}</p>
      )}
    </article>
  )
}

function TakeHome({ taskId, stack, name }: { taskId: string; stack: TaskStack; name: string }) {
  const { baseUrl } = useServer()
  const [copied, setCopied] = useState(false)
  const command = `st graph import ~/Downloads/${name}`
  const download = async () => {
    const response = await apiFetch(baseUrl, taskStackDocumentPath(taskId, stack.clone_id))
    const url = URL.createObjectURL(await response.blob())
    const anchor = document.createElement("a")
    anchor.href = url
    anchor.download = name
    anchor.click()
    URL.revokeObjectURL(url)
  }
  const copy = async () => {
    await navigator.clipboard?.writeText(command)
    setCopied(true)
    setTimeout(() => setCopied(false), 1500)
  }
  return (
    <div className="mt-3 space-y-2">
      <button onClick={download} className="flex min-h-10 items-center gap-2 rounded bg-indigo-600 px-3 text-xs text-white">
        <Download size={14} aria-hidden />
        Download stack
      </button>
      <p className="text-xs text-zinc-500">Then, in your checkout of {stack.repo}:</p>
      <div className="flex items-center gap-2 rounded bg-zinc-900 px-2 py-1">
        <code className="min-w-0 flex-1 truncate text-xs text-zinc-300">{command}</code>
        <button onClick={copy} aria-label="Copy the import command" className="flex min-h-8 min-w-8 items-center justify-center text-zinc-400">
          {copied ? <Check size={14} aria-hidden /> : <Copy size={14} aria-hidden />}
        </button>
      </div>
    </div>
  )
}
