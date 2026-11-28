import { useEffect, useId, useMemo, useRef, useState } from "react"
import { Check, ChevronDown } from "lucide-react"
import { useBoard } from "../hooks/useBoard"
import { useServer } from "../hooks/useServer"
import { fetchArchivedTasks } from "../lib/board"
import { completedWord, offeredTasks, taskGroups, taskText, type SelectableTask } from "../lib/taskChoices"

// loadArchived loads the archived tasks, telling them (or why they could
// not be loaded) only while the ask is current.
function loadArchived(baseUrl: string, isCurrent: () => boolean, onTasks: (tasks: SelectableTask[]) => void, onError: (message: string) => void) {
  fetchArchivedTasks(baseUrl)
    .then(tasks => { if (isCurrent()) onTasks(tasks.map(task => ({ ...task, archived: true }))) })
    .catch(error => { if (isCurrent()) onError(error instanceof Error ? error.message : String(error)) })
}

// useArchivedTasks loads the archived tasks while they are asked for, and
// why they could not be loaded.
function useArchivedTasks(baseUrl: string, showCompleted: boolean) {
  const [archived, setArchived] = useState<SelectableTask[]>([])
  const [archiveError, setArchiveError] = useState("")
  useEffect(() => {
    setArchived([])
    setArchiveError("")
    if (!showCompleted) return
    let current = true
    loadArchived(baseUrl, () => current, setArchived, setArchiveError)
    return () => { current = false }
  }, [baseUrl, showCompleted])
  return { archived, archiveError }
}

type ListAction = { kind: "close" } | { kind: "move"; delta: number } | { kind: "choose" } | null

// listAction is what a key does in the list: Escape closes it, the arrows
// move the active option, and Enter or Space chooses it, if there is one.
function listAction(key: string, hasActive: boolean): ListAction {
  if (key === "Escape") return { kind: "close" }
  if (key === "ArrowDown" || key === "ArrowUp") return { kind: "move", delta: key === "ArrowDown" ? 1 : -1 }
  return (key === "Enter" || key === " ") && hasActive ? { kind: "choose" } : null
}

// stepIndex is the active option moved by delta, kept within the list.
const stepIndex = (current: number, delta: number, count: number) => Math.max(0, Math.min(current + delta, count - 1))

type OptionProps = { task: SelectableTask; optionId: string; chosen: boolean; active: boolean; onActive: () => void; onChoose: () => void }

function TaskOption({ task, optionId, chosen, active, onActive, onChoose }: OptionProps) {
  const word = completedWord(task)
  return <div id={optionId} role="option" aria-selected={chosen} onMouseEnter={onActive} onClick={onChoose} className={`flex min-h-11 w-full cursor-pointer items-center gap-2 rounded px-2 py-2 text-left ${active ? "bg-zinc-800" : "hover:bg-zinc-800"}`}>
    <span className="min-w-0 flex-1"><span className="block truncate text-sm text-zinc-200">{task.id} · {task.title}</span>{word && <span className="text-xs text-zinc-500">{word}</span>}</span>{chosen && <Check className="shrink-0 text-indigo-300" size={16}/>}
  </div>
}

type PopupProps = {
  label: string
  listboxID: string
  listboxRef: React.RefObject<HTMLDivElement | null>
  groups: ReturnType<typeof taskGroups>
  orderedTasks: SelectableTask[]
  value: string
  activeIndex: number
  onActive: (index: number) => void
  onChoose: (task: SelectableTask) => void
  showCompleted: boolean
  onShowCompleted: (show: boolean) => void
  archiveError: string
  onKeyDown: (event: React.KeyboardEvent<HTMLDivElement>) => void
  onClose: () => void
}

// TaskPopup is the open list: done and archived tasks on request, the
// tasks by workspace, and why archived tasks could not be loaded.
function TaskPopup({ label, listboxID, listboxRef, groups, orderedTasks, value, activeIndex, onActive, onChoose, showCompleted, onShowCompleted, archiveError, onKeyDown, onClose }: PopupProps) {
  const activeTask = orderedTasks[activeIndex]
  return <div className="absolute z-20 mt-1 w-full overflow-hidden rounded-lg border border-zinc-700 bg-zinc-950 shadow-xl" onKeyDown={event => { if (event.key === "Escape") onClose() }}>
    <label className="flex min-h-11 items-center gap-3 border-b border-zinc-800 px-3 text-xs text-zinc-300"><input type="checkbox" checked={showCompleted} onChange={event => onShowCompleted(event.target.checked)} className="size-4"/>Show done and archived tasks</label>
    <div ref={listboxRef} id={listboxID} role="listbox" aria-label={label} aria-activedescendant={activeTask ? `${listboxID}-${activeTask.id}` : undefined} tabIndex={0} onKeyDown={onKeyDown} className="max-h-72 overflow-y-auto p-1 outline-none">
      {groups.map(([workspace, workspaceTasks]) => <div key={workspace} role="group" aria-label={workspace}>
        <div className="sticky top-0 bg-zinc-950 px-2 py-1.5 text-[11px] font-semibold uppercase tracking-wide text-zinc-500">{workspace}</div>
        {workspaceTasks.map(task => {
          const index = orderedTasks.findIndex(item => item.id === task.id)
          return <TaskOption key={`${task.archived ? "archived" : "active"}-${task.id}`} task={task} optionId={`${listboxID}-${task.id}`} chosen={task.id === value} active={index === activeIndex} onActive={() => onActive(index)} onChoose={() => onChoose(task)} />
        })}
      </div>)}
      {groups.length === 0 && <p className="px-3 py-6 text-center text-sm text-zinc-500">No matching tasks</p>}
    </div>
    {archiveError && <p role="alert" className="border-t border-zinc-800 px-3 py-2 text-xs text-red-400">Could not load archived tasks: {archiveError}</p>}
  </div>
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
  const { archived, archiveError } = useArchivedTasks(baseUrl, showCompleted)
  const active = useMemo(() => board.columns.flatMap(column => column.cards), [board.columns])

  const tasks = useMemo(() => offeredTasks(active, archived, showCompleted), [showCompleted, active, archived])
  const groups = useMemo(() => taskGroups(tasks), [tasks])
  const orderedTasks = useMemo(() => groups.flatMap(([, workspaceTasks]) => workspaceTasks), [groups])
  const selected = [...active, ...archived].find(task => task.id === value)
  const activeTask = orderedTasks[activeIndex]

  useEffect(() => {
    if (!open) return
    setActiveIndex(Math.max(0, orderedTasks.findIndex(task => task.id === value)))
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
    const action = listAction(event.key, Boolean(activeTask))
    if (!action) return
    event.preventDefault()
    if (action.kind === "close") close()
    else if (action.kind === "move") setActiveIndex(current => stepIndex(current, action.delta, orderedTasks.length))
    else choose(activeTask!)
  }

  const shown = selected ? taskText(selected) : "Select a task"
  return <div className="relative mt-1">
    <button ref={triggerRef} type="button" role="combobox" aria-label={`${label}: ${shown}`} aria-haspopup="listbox" aria-expanded={open} aria-controls={listboxID} aria-required={required} onClick={() => setOpen(current => !current)} onKeyDown={event => { if (event.key === "ArrowDown") { event.preventDefault(); setOpen(true) } }} className="flex min-h-11 w-full items-center justify-between gap-3 rounded-lg border border-zinc-700 bg-zinc-950 px-3 text-left">
      <span className={selected ? "min-w-0 truncate text-zinc-100" : "text-zinc-500"}>{shown}</span><ChevronDown className="shrink-0 text-zinc-500" size={16}/>
    </button>
    {open && <TaskPopup label={label} listboxID={listboxID} listboxRef={listboxRef} groups={groups} orderedTasks={orderedTasks} value={value} activeIndex={activeIndex} onActive={setActiveIndex} onChoose={choose}
      showCompleted={showCompleted} onShowCompleted={setShowCompleted} archiveError={archiveError} onKeyDown={handleListboxKeyDown} onClose={close} />}
  </div>
}
