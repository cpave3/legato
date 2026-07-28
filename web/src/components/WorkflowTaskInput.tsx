import { useEffect, useId, useMemo, useRef, useState } from "react"
import { Check, ChevronDown } from "lucide-react"
import { useBoard } from "../hooks/useBoard"
import { useServer } from "../hooks/useServer"
import { fetchArchivedTasks } from "../lib/board"
import type { BoardCard } from "../lib/board-types"

type SelectableTask = BoardCard & { archived?: boolean }

function isDone(task: BoardCard) {
  return task.status.toLowerCase() === "done"
}

export function WorkflowTaskInput({ label, required, value, onChange }: { label: string; required?: boolean; value: string; onChange: (value: string) => void }) {
  const board = useBoard()
  const { baseUrl } = useServer()
  const listboxID = useId()
  const triggerRef = useRef<HTMLButtonElement>(null)
  const listboxRef = useRef<HTMLDivElement>(null)
  const [open, setOpen] = useState(false)
  const [showCompleted, setShowCompleted] = useState(false)
  const [activeIndex, setActiveIndex] = useState(0)
  const [archived, setArchived] = useState<SelectableTask[]>([])
  const [archiveError, setArchiveError] = useState("")
  const active = useMemo(() => board.columns.flatMap(column => column.cards), [board.columns])

  useEffect(() => {
    setArchived([])
    setArchiveError("")
    if (!showCompleted) return
    let current = true
    fetchArchivedTasks(baseUrl)
      .then(tasks => { if (current) setArchived(tasks.map(task => ({ ...task, archived: true }))) })
      .catch(error => { if (current) setArchiveError(error instanceof Error ? error.message : String(error)) })
    return () => { current = false }
  }, [baseUrl, showCompleted])

  const tasks = showCompleted ? [...active, ...archived] : active.filter(task => !isDone(task))
  const groups = useMemo(() => {
    const grouped = new Map<string, SelectableTask[]>()
    for (const task of tasks) {
      const workspace = task.workspace_name || "Unassigned"
      const existing = grouped.get(workspace) ?? []
      if (!existing.some(item => item.id === task.id)) existing.push(task)
      grouped.set(workspace, existing)
    }
    return [...grouped.entries()]
      .sort(([a], [b]) => a === "Unassigned" ? 1 : b === "Unassigned" ? -1 : a.localeCompare(b))
      .map(([workspace, workspaceTasks]) => [workspace, workspaceTasks.sort((a, b) => a.title.localeCompare(b.title))] as const)
  }, [tasks])
  const orderedTasks = useMemo(() => groups.flatMap(([, workspaceTasks]) => workspaceTasks), [groups])
  const selected = [...active, ...archived].find(task => task.id === value)

  useEffect(() => {
    if (!open) return
    const selectedIndex = orderedTasks.findIndex(task => task.id === value)
    setActiveIndex(selectedIndex >= 0 ? selectedIndex : 0)
    requestAnimationFrame(() => listboxRef.current?.focus())
  }, [open, orderedTasks, value])

  function close() {
    setOpen(false)
    requestAnimationFrame(() => triggerRef.current?.focus())
  }

  function choose(task: SelectableTask) {
    onChange(task.id)
    close()
  }

  function handleListboxKeyDown(event: React.KeyboardEvent<HTMLDivElement>) {
    if (event.key === "Escape") {
      event.preventDefault()
      close()
      return
    }
    if (event.key === "ArrowDown" || event.key === "ArrowUp") {
      event.preventDefault()
      const delta = event.key === "ArrowDown" ? 1 : -1
      setActiveIndex(current => Math.max(0, Math.min(current + delta, orderedTasks.length - 1)))
      return
    }
    if ((event.key === "Enter" || event.key === " ") && orderedTasks[activeIndex]) {
      event.preventDefault()
      choose(orderedTasks[activeIndex])
    }
  }

  return <div className="relative mt-1">
    <button ref={triggerRef} type="button" role="combobox" aria-label={`${label}: ${selected ? `${selected.id} · ${selected.title}` : "Select a task"}`} aria-haspopup="listbox" aria-expanded={open} aria-controls={listboxID} aria-required={required} onClick={() => setOpen(current => !current)} onKeyDown={event => { if (event.key === "ArrowDown") { event.preventDefault(); setOpen(true) } }} className="flex min-h-11 w-full items-center justify-between gap-3 rounded-lg border border-zinc-700 bg-zinc-950 px-3 text-left">
      <span className={selected ? "min-w-0 truncate text-zinc-100" : "text-zinc-500"}>{selected ? `${selected.id} · ${selected.title}` : "Select a task"}</span><ChevronDown className="shrink-0 text-zinc-500" size={16}/>
    </button>
    {open && <div className="absolute z-20 mt-1 w-full overflow-hidden rounded-lg border border-zinc-700 bg-zinc-950 shadow-xl" onKeyDown={event => { if (event.key === "Escape") close() }}>
      <label className="flex min-h-11 items-center gap-3 border-b border-zinc-800 px-3 text-xs text-zinc-300"><input type="checkbox" checked={showCompleted} onChange={event => setShowCompleted(event.target.checked)} className="size-4"/>Show done and archived tasks</label>
      <div ref={listboxRef} id={listboxID} role="listbox" aria-label={label} aria-activedescendant={orderedTasks[activeIndex] ? `${listboxID}-${orderedTasks[activeIndex].id}` : undefined} tabIndex={0} onKeyDown={handleListboxKeyDown} className="max-h-72 overflow-y-auto p-1 outline-none">
        {groups.map(([workspace, workspaceTasks]) => <div key={workspace} role="group" aria-label={workspace}>
          <div className="sticky top-0 bg-zinc-950 px-2 py-1.5 text-[11px] font-semibold uppercase tracking-wide text-zinc-500">{workspace}</div>
          {workspaceTasks.map(task => { const index = orderedTasks.findIndex(item => item.id === task.id); return <div id={`${listboxID}-${task.id}`} role="option" aria-selected={task.id === value} key={`${task.archived ? "archived" : "active"}-${task.id}`} onMouseEnter={() => setActiveIndex(index)} onClick={() => choose(task)} className={`flex min-h-11 w-full cursor-pointer items-center gap-2 rounded px-2 py-2 text-left ${index === activeIndex ? "bg-zinc-800" : "hover:bg-zinc-800"}`}>
            <span className="min-w-0 flex-1"><span className="block truncate text-sm text-zinc-200">{task.id} · {task.title}</span>{(isDone(task) || task.archived) && <span className="text-xs text-zinc-500">{task.archived ? "Archived" : "Done"}</span>}</span>{task.id === value && <Check className="shrink-0 text-indigo-300" size={16}/>} 
          </div>})}
        </div>)}
        {groups.length === 0 && <p className="px-3 py-6 text-center text-sm text-zinc-500">No matching tasks</p>}
      </div>
      {archiveError && <p role="alert" className="border-t border-zinc-800 px-3 py-2 text-xs text-red-400">Could not load archived tasks: {archiveError}</p>}
    </div>}
  </div>
}
