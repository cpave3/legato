import { useState, type FormEvent, type ReactNode } from "react"
import { Check, Plus } from "lucide-react"
import { cn } from "../lib/utils"
import { Button } from "./ui/Button"
import { Dialog, DialogBody, DialogClose, DialogFooter, DialogHeader, DialogPanel, DialogTitle } from "./ui/Dialog"

const inputClass = "w-full rounded border border-zinc-700 bg-zinc-950 px-3 py-2 text-sm text-zinc-200 outline-none focus:border-indigo-500"

// GroupNameDialog names a new group, or a group's new name.
export function GroupNameDialog({ title, action, initial = "", note, taken = [], onSubmit, onClose }: {
  title: string
  action: string
  initial?: string
  note?: ReactNode
  // Names that are not a change: the group's own name.
  taken?: string[]
  onSubmit: (name: string) => Promise<void> | void
  onClose: () => void
}) {
  const [name, setName] = useState(initial)
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState("")
  const trimmed = name.trim()
  const canSave = trimmed !== "" && !taken.includes(trimmed) && !saving

  async function submit(event: FormEvent) {
    event.preventDefault()
    if (!canSave) return
    setSaving(true)
    setError("")
    try {
      await onSubmit(trimmed)
      onClose()
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : String(cause))
      setSaving(false)
    }
  }

  return (
    <Dialog open onClose={onClose}>
      <DialogPanel className="max-w-sm">
        <form onSubmit={submit}>
          <DialogHeader>
            <DialogTitle>{title}</DialogTitle>
            <DialogClose />
          </DialogHeader>
          <DialogBody>
            {error && <div role="alert" className="rounded border border-red-800 bg-red-950/50 px-3 py-2 text-xs text-red-300">{error}</div>}
            <label className="block text-xs text-zinc-500" htmlFor="agent-group-name">Name</label>
            <input
              id="agent-group-name"
              value={name}
              onChange={(event) => setName(event.target.value)}
              className={inputClass}
              autoFocus
              onFocus={(event) => event.currentTarget.select()}
            />
            {note && <p className="text-xs text-zinc-500">{note}</p>}
          </DialogBody>
          <DialogFooter>
            <Button type="button" variant="ghost" size="sm" onClick={onClose}>Cancel</Button>
            <Button type="submit" size="sm" disabled={!canSave}>{action}</Button>
          </DialogFooter>
        </form>
      </DialogPanel>
    </Dialog>
  )
}

// MoveToGroupDialog puts one agent in a group, a new one, or none: the way to
// move an agent without dragging it, from a keyboard or a phone.
export function MoveToGroupDialog({ agentLabel, current, groups, onMove, onClose }: {
  agentLabel: string
  // The group the agent is in; empty for none.
  current: string
  groups: string[]
  onMove: (group: string | null) => Promise<void> | void
  onClose: () => void
}) {
  const [naming, setNaming] = useState(false)
  const [name, setName] = useState("")
  const [error, setError] = useState("")
  const trimmed = name.trim()

  async function move(group: string | null) {
    setError("")
    try {
      await onMove(group)
      onClose()
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : String(cause))
    }
  }

  const choice = (label: string, group: string | null) => {
    const here = (group ?? "") === current
    return (
      <button
        key={label}
        type="button"
        aria-current={here ? "true" : undefined}
        disabled={here}
        onClick={() => void move(group)}
        className={cn(
          "flex min-h-10 w-full items-center gap-2 rounded px-3 text-left text-sm hover:bg-zinc-800 focus:bg-zinc-800 focus:outline-none disabled:cursor-default disabled:hover:bg-transparent",
          here ? "text-zinc-500" : "text-zinc-200",
        )}
      >
        <span className="flex-1 truncate">{label}</span>
        {here && <Check size={14} aria-hidden="true" />}
      </button>
    )
  }

  return (
    <Dialog open onClose={onClose}>
      <DialogPanel className="max-w-sm">
        <DialogHeader>
          <DialogTitle>Move {agentLabel} to group</DialogTitle>
          <DialogClose />
        </DialogHeader>
        <DialogBody>
          {error && <div role="alert" className="rounded border border-red-800 bg-red-950/50 px-3 py-2 text-xs text-red-300">{error}</div>}
          <div role="group" aria-label="Groups" className="flex flex-col">
            {groups.map((group) => choice(group, group))}
            {choice("Ungrouped", null)}
          </div>
          {naming ? (
            <form onSubmit={(event) => { event.preventDefault(); if (trimmed) void move(trimmed) }} className="flex gap-2">
              <input aria-label="New group name" value={name} onChange={(event) => setName(event.target.value)} className={inputClass} autoFocus />
              <Button type="submit" size="sm" disabled={!trimmed}>Move</Button>
            </form>
          ) : (
            <button type="button" onClick={() => setNaming(true)} className="flex min-h-10 w-full items-center gap-2 rounded px-3 text-left text-sm text-lw-link hover:bg-zinc-800">
              <Plus size={14} aria-hidden="true" />New group…
            </button>
          )}
        </DialogBody>
        <DialogFooter>
          <Button type="button" variant="ghost" size="sm" onClick={onClose}>Cancel</Button>
        </DialogFooter>
      </DialogPanel>
    </Dialog>
  )
}
