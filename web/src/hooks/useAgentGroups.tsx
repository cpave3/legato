import { useCallback, useEffect, useMemo, useState, type ReactNode } from "react"
import type { AgentInfo } from "./useWebSocket"
import { useServer } from "./useServer"
import { useToast } from "./useToast"
import { useConfirm } from "./useConfirm"
import { apiFetch } from "../lib/api"
import { agentGroupLayout, groupedListOrder, hasGroups } from "../lib/agentGroups"
import { agentListOrder } from "../lib/agentList"
import { GroupNameDialog, MoveToGroupDialog } from "../components/AgentGroupDialogs"

export type AgentListMode = "group" | "status"

// AgentGrouping is what the agent list needs to show agents by group: the
// groups, which are collapsed, and the actions on them.
export interface AgentGrouping {
  mode: AgentListMode
  setMode: (mode: AgentListMode) => void
  // The groups listed even when empty: the defaults, then those made here.
  known: string[]
  // groups.defaults, which always exist.
  defaults: string[]
  // name is null for the agents in no group.
  isCollapsed: (name: string | null) => boolean
  toggle: (name: string | null) => void
  collapseOthers: (name: string | null) => void
  move: (agentId: string, group: string | null) => void
  newGroup: () => void
  renameGroup: (name: string) => void
  deleteGroup: (name: string) => void
}

const COLLAPSED_KEY = "legato:agent-groups:collapsed"
const MODE_KEY = "legato:agent-groups:mode"
const MADE_KEY = "legato:agent-groups:made"
// The key of the agents in no group, which no group name can be.
const UNGROUPED = "\u0000"

function readList(key: string): string[] {
  try {
    const value = JSON.parse(localStorage.getItem(key) ?? "[]")
    return Array.isArray(value) ? value.filter((item): item is string => typeof item === "string") : []
  } catch {
    return []
  }
}

function writeList(key: string, list: string[]) {
  try {
    localStorage.setItem(key, JSON.stringify(list))
  } catch {
    // Not remembered: a private window or blocked storage.
  }
}

function readMode(): AgentListMode | null {
  try {
    const mode = localStorage.getItem(MODE_KEY)
    return mode === "group" || mode === "status" ? mode : null
  } catch {
    return null
  }
}

// useStoredList is a list of names kept in this browser.
function useStoredList(key: string): [string[], (update: (list: string[]) => string[]) => void] {
  const [list, setList] = useState(() => readList(key))
  const update = useCallback((change: (list: string[]) => string[]) => {
    setList((current) => {
      const next = change(current)
      writeList(key, next)
      return next
    })
  }, [key])
  return [list, update]
}

async function check(response: Response) {
  if (!response.ok) {
    const body = await response.json().catch(() => null)
    throw new Error(body?.error ?? `Request failed (${response.status})`)
  }
}

type Naming = { kind: "new" } | { kind: "rename"; from: string }

// useAgentGroups keeps the agent list's groups: it reads the default groups,
// remembers in this browser which groups are collapsed, which list the person
// chose and the groups they made that have no agent yet, and moves, renames
// and deletes groups through the server.
export function useAgentGroups({ agents, onChanged }: { agents: AgentInfo[]; onChanged: () => void }) {
  const { baseUrl } = useServer()
  const { addToast } = useToast()
  const { confirm, dialog: confirmDialog } = useConfirm()
  const [defaults, setDefaults] = useState<string[]>([])
  const [made, updateMade] = useStoredList(MADE_KEY)
  const [collapsed, updateCollapsed] = useStoredList(COLLAPSED_KEY)
  const [chosenMode, setChosenMode] = useState(readMode)
  const [naming, setNaming] = useState<Naming | null>(null)
  const [moving, setMoving] = useState<AgentInfo | null>(null)

  useEffect(() => {
    let live = true
    // Without the defaults the list still works: it shows the groups in use.
    Promise.resolve()
      .then(() => apiFetch(baseUrl, "/api/agent-groups"))
      .then((response) => (response.ok ? response.json() : null))
      .then((body) => { if (live && Array.isArray(body?.defaults)) setDefaults(body.defaults) })
      .catch(() => {})
    return () => { live = false }
  }, [baseUrl])

  const known = useMemo(() => [...defaults, ...made.filter((name) => !defaults.includes(name))], [defaults, made])
  const mode: AgentListMode = chosenMode ?? (hasGroups(agents) ? "group" : "status")
  const keyOf = (name: string | null) => name ?? UNGROUPED
  const isCollapsed = useCallback((name: string | null) => collapsed.includes(keyOf(name)), [collapsed])

  const fail = useCallback((error: unknown) => {
    addToast(error instanceof Error ? error.message : String(error), "error")
  }, [addToast])

  const moveTo = useCallback(async (agentId: string, group: string | null) => {
    await check(await apiFetch(baseUrl, `/api/agents/${encodeURIComponent(agentId)}/group`, {
      method: "PUT",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ group }),
    }))
    if (group) updateCollapsed((list) => list.filter((key) => key !== group))
    onChanged()
  }, [baseUrl, onChanged, updateCollapsed])

  const rename = useCallback(async (from: string, to: string) => {
    await check(await apiFetch(baseUrl, `/api/agent-groups/${encodeURIComponent(from)}`, {
      method: "PUT",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ name: to }),
    }))
    updateMade((list) => [...new Set(list.map((name) => (name === from ? to : name)))])
    updateCollapsed((list) => [...new Set(list.map((key) => (key === from ? to : key)))])
    onChanged()
  }, [baseUrl, onChanged, updateMade, updateCollapsed])

  const remove = useCallback(async (name: string) => {
    const count = agents.filter((agent) => agent.group === name).length
    const isDefault = defaults.includes(name)
    const ok = await confirm({
      title: `Delete ${name}?`,
      message: (
        <>
          {count > 0 && <p>Its {count === 1 ? "agent becomes" : `${count} agents become`} Ungrouped.</p>}
          {isDefault && <p>{name} is in the groups.defaults setting, so its empty header stays until you take it out there.</p>}
        </>
      ),
      confirmLabel: "Delete group",
      destructive: true,
    })
    if (!ok) return
    try {
      await check(await apiFetch(baseUrl, `/api/agent-groups/${encodeURIComponent(name)}`, { method: "DELETE" }))
      updateMade((list) => list.filter((item) => item !== name))
      updateCollapsed((list) => list.filter((key) => key !== name))
      onChanged()
    } catch (error) {
      fail(error)
    }
  }, [agents, defaults, confirm, baseUrl, updateMade, updateCollapsed, onChanged, fail])

  const chooseMode = useCallback((next: AgentListMode) => {
    setChosenMode(next)
    try { localStorage.setItem(MODE_KEY, next) } catch { /* not remembered */ }
  }, [])

  const grouping: AgentGrouping = {
    mode,
    setMode: chooseMode,
    known,
    defaults,
    isCollapsed,
    toggle: (name) => updateCollapsed((list) => (list.includes(keyOf(name)) ? list.filter((key) => key !== keyOf(name)) : [...list, keyOf(name)])),
    collapseOthers: (name) => {
      const others = agentGroupLayout(agents, known, { withUngrouped: true }).map((block) => keyOf(block.name)).filter((key) => key !== keyOf(name))
      updateCollapsed(() => others)
    },
    move: (agentId, group) => { moveTo(agentId, group).catch(fail) },
    newGroup: () => setNaming({ kind: "new" }),
    renameGroup: (name) => setNaming({ kind: "rename", from: name }),
    deleteGroup: (name) => { void remove(name) },
  }

  // The order the list shows agents in, which the number keys switch by.
  const listOrder = useCallback((list: AgentInfo[]) => (
    mode === "group" ? groupedListOrder(agentGroupLayout(list, known), isCollapsed) : agentListOrder(list)
  ), [mode, known, isCollapsed])

  const allGroups = useMemo(() => agentGroupLayout(agents, known).flatMap((block) => (block.name ? [block.name] : [])), [agents, known])

  const dialogs: ReactNode = (
    <>
      {naming?.kind === "new" && (
        <GroupNameDialog
          title="New group"
          action="Create"
          taken={allGroups}
          onSubmit={(name) => {
            updateMade((list) => (list.includes(name) ? list : [...list, name]))
            chooseMode("group")
          }}
          onClose={() => setNaming(null)}
        />
      )}
      {naming?.kind === "rename" && (
        <GroupNameDialog
          title={`Rename ${naming.from}`}
          action="Rename"
          initial={naming.from}
          taken={[naming.from]}
          note={defaults.includes(naming.from) ? `${naming.from} is in the groups.defaults setting, so an empty ${naming.from} header stays.` : undefined}
          onSubmit={(name) => rename(naming.from, name)}
          onClose={() => setNaming(null)}
        />
      )}
      {moving && (
        <MoveToGroupDialog
          agentLabel={moving.task_title || moving.task_id}
          current={moving.group ?? ""}
          groups={allGroups}
          onMove={(group) => moveTo(moving.task_id, group)}
          onClose={() => setMoving(null)}
        />
      )}
      {confirmDialog}
    </>
  )

  return { grouping, dialogs, listOrder, openMove: setMoving }
}
