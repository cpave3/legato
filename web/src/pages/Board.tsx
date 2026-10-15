import { useState, useEffect, useCallback, useMemo, useRef } from "react"
import { useSearchParams } from "react-router-dom"
import { useServer } from "../hooks/useServer"
import { useBoard } from "../hooks/useBoard"
import {
  createTask,
  patchTask,
  deleteTask,
  archiveDone,
  fetchCard,
  fetchCardFull,
  ProviderSyncConflictError,
  syncBoard,
  type ProviderSyncResult,
} from "../lib/board"
import type { BoardCard, BoardColumn, CardDetail } from "../lib/board-types"
import { BoardGrid } from "../components/board/BoardGrid"
import { isColumnCollapsed } from "../lib/boardColumns"
import { MobileBoardList } from "../components/board/MobileBoardList"
import { Plus, RefreshCw, Search } from "lucide-react"
import { WorkspaceFilter } from "../components/board/WorkspaceFilter"
import { DetailPanel } from "../components/board/DetailPanel"
import { BoardTaskPanel } from "../components/board/BoardTaskPanel"
import { useMediaQuery } from "../hooks/useMediaQuery"
import { CreateTaskModal } from "../components/board/CreateTaskModal"
import { MoveTaskModal } from "../components/board/MoveTaskModal"
import { DeleteTaskModal } from "../components/board/DeleteTaskModal"
import { ArchiveDoneModal } from "../components/board/ArchiveDoneModal"
import { ArchivedTasksModal } from "../components/board/ArchivedTasksModal"
import { SearchModal } from "../components/board/SearchModal"
import { LinkPRModal } from "../components/board/LinkPRModal"
import { ImportRemoteModal } from "../components/board/ImportRemoteModal"
import { EditTitleModal } from "../components/board/EditTitleModal"
import { EditDescriptionModal } from "../components/board/EditDescriptionModal"
import { HelpModal } from "../components/board/HelpModal"
import { OpenURLPicker } from "../components/board/OpenURLPicker"
import { CancelSwarmModal } from "../components/board/CancelSwarmModal"
import { WorktreeModal } from "../components/board/WorktreeModal"
import { CardContextMenu, type CardContextAction } from "../components/board/CardContextMenu"
import { cardWorkflowOptions, type CardWorkflowOption } from "../lib/card-workflows"
import { RunWorkflowDialog } from "../components/RunWorkflowDialog"
import { useWorkflows } from "../hooks/useWorkflow"
import { useToast } from "../hooks/useToast"
import { useVoiceEnabled } from "../hooks/useVoiceEnabled"
import { useSettings } from "../hooks/useSettings"
import { apiFetch } from "../lib/api"
import { WorkflowTriggerInputModal } from "../components/WorkflowTriggerInputModal"
import type { WorkflowTriggerRequest } from "../lib/board"
import type { RepositoryPreset } from "../components/RepositoryPresetSelect"
import { LoadingState } from "../components/ui/LoadingState"

type MoveCard = Pick<BoardCard, "id" | "title" | "status" | "workspace_id">

type Overlay =
  | { kind: "none" }
  | { kind: "create" }
  | { kind: "move"; card: MoveCard }
  | { kind: "delete"; card: BoardCard }
  | { kind: "archive" }
  | { kind: "archived" }
  | { kind: "search" }
  | { kind: "linkPR"; card: BoardCard }
  | { kind: "import" }
  | { kind: "editTitle"; card: BoardCard }
  | { kind: "editDescription"; card: BoardCard }
  | { kind: "help" }
  | { kind: "openURL"; providerURL: string; prURL: string }
  | { kind: "cancelSwarm"; card: BoardCard }
  | { kind: "worktree" }
  | { kind: "workflowInput"; request: WorkflowTriggerRequest }

function errorMessage(error: unknown, fallback: string): string {
  if (error instanceof Error && error.message.trim()) return error.message
  return fallback
}

function providerSyncMessage(result: ProviderSyncResult): { message: string; type: "success" | "error" | "info" } {
  const sources = [["Jira", result.jira], ["GitHub", result.github]] as const
  const failed = sources.filter(([, source]) => source.status === "failed")
  const completed = sources.filter(([, source]) => source.status === "completed")
  if (failed.length > 0) {
    const details = failed.map(([name, source]) => `${name}: ${source.error || "failed"}`).join("; ")
    const prefix = completed.length > 0 ? "Provider sync partially completed" : "Provider sync failed"
    return { message: `${prefix} — ${details}`, type: "error" }
  }
  if (completed.length === 0) return { message: "No providers are configured", type: "info" }
  return { message: `${completed.map(([name]) => name).join(" + ")} sync completed`, type: "success" }
}

const showEmptyKey = "legato:board-show-empty"

function initialShowEmpty(): boolean {
  try {
    return localStorage.getItem(showEmptyKey) === "true"
  } catch {
    return false
  }
}

// The next column the cursor can rest in, from one step in the given
// direction: a folded column is passed over. Stays put at either end.
function stepColumn(columns: BoardColumn[], from: number, step: -1 | 1, showEmpty: boolean): number {
  for (let index = from + step; index >= 0 && index < columns.length; index += step) {
    if (!isColumnCollapsed(columns, index, showEmpty)) return index
  }
  return from
}

function boardKeys(showEmpty: boolean, canStartAgent: boolean): [string, string][] {
  return [
    ["h/l", "column"],
    ["j/k", "card"],
    ["↵", "open"],
    ...(canStartAgent ? [["s", "start agent"] as [string, string]] : []),
    ["/", "search"],
    ["z", showEmpty ? "fold empty columns" : "show empty columns"],
    ["?", "keys"],
  ]
}

function CardFilter({ value, onChange, className, autoFocus }: { value: string; onChange: (value: string) => void; className?: string; autoFocus?: boolean }) {
  return (
    <label className={`flex h-8 shrink-0 items-center gap-1.5 rounded-md border border-lw-line bg-lw-surface px-2.5 text-lw-faint focus-within:border-lw-accent-ring ${className ?? ""}`}>
      <Search size={13} aria-hidden="true" />
      <input
        aria-label="Filter cards"
        placeholder="Filter cards"
        value={value}
        autoFocus={autoFocus}
        onChange={(event) => onChange(event.target.value)}
        onKeyDown={(event) => {
          if (event.key !== "Escape") return
          event.preventDefault()
          onChange("")
          event.currentTarget.blur()
        }}
        className="w-full min-w-0 bg-transparent text-[13px] text-lw-text outline-none placeholder:text-lw-faint"
      />
    </label>
  )
}

function filterColumns(columns: BoardColumn[], query: string): BoardColumn[] {
  const needle = query.trim().toLowerCase()
  if (!needle) return columns
  return columns.map((column) => ({
    ...column,
    cards: column.cards.filter((card) => card.title.toLowerCase().includes(needle) || card.id.toLowerCase().includes(needle)),
  }))
}

function cardCountLabel(cards: BoardCard[], shown: BoardCard[]): string {
  const live = cards.filter((card) => card.agent_active).length
  const count = shown.length === cards.length ? `${cards.length}` : `${shown.length} of ${cards.length}`
  const total = `${count} ${cards.length === 1 ? "card" : "cards"}`
  return live > 0 ? `${total} · ${live} with live agents` : total
}

function detailMoveCard(card: CardDetail): MoveCard {
  return {
    id: card.id,
    title: card.title,
    status: card.status,
    workspace_id: card.workspace_id,
  }
}

export function BoardPage() {
  const { baseUrl } = useServer()
  const [searchParams, setSearchParams] = useSearchParams()
  const requestedHighlightId = searchParams.get("highlight_task")
  const [highlightedTaskId, setHighlightedTaskId] = useState<string | null>(null)
  const voiceEnabled = useVoiceEnabled(baseUrl)
  const { addToast } = useToast()
  const [workspaceFilter, setWorkspaceFilter] = useState<string>("all")
  const { columns: boardColumns, workspaces, loading, error, refresh } = useBoard(workspaceFilter)
  const [query, setQuery] = useState("")
  const [showEmpty, setShowEmpty] = useState(initialShowEmpty)
  const [mobileFilterOpen, setMobileFilterOpen] = useState(false)
  // The columns as the board shows them: every card, or those the filter
  // matches. The cursor moves over these.
  const columns = useMemo(() => filterColumns(boardColumns, query), [boardColumns, query])
  const workflowDiscovery = useWorkflows()
  const [cursorCol, setCursorCol] = useState(0)
  const [cursorRow, setCursorRow] = useState(0)
  const [overlay, setOverlay] = useState<Overlay>({ kind: "none" })
  const [contextMenu, setContextMenu] = useState<{ card: BoardCard; position: { x: number; y: number } } | null>(null)
  const [cardWorkflow, setCardWorkflow] = useState<{ card: BoardCard; workflow: CardWorkflowOption } | null>(null)
  const [detailCard, setDetailCard] = useState<CardDetail | null>(null)
  const [detailId, setDetailId] = useState<string | null>(null)
  const [pendingAction, setPendingAction] = useState<string | null>(null)
  const [syncingProviders, setSyncingProviders] = useState(false)
  const settings = useSettings()
  const repositories = (settings.data?.repositories ?? []) as RepositoryPreset[]
  const defaultWorkingDir = settings.data?.working_dir ?? ""
  const worktreesEnabled = Boolean(settings.data?.worktrees_enabled)
  // From this width the selected card is described beside the board; below
  // it a click opens the card's details, as there is no room for both.
  const wide = useMediaQuery("(min-width: 1280px)")
  const [panelHidden, setPanelHidden] = useState(false)
  const [panelDetail, setPanelDetail] = useState<CardDetail | null>(null)
  const boardRef = useRef<HTMLDivElement>(null)
  const panelRef = useRef<HTMLDivElement>(null)
  const movedCardIdRef = useRef<string | null>(null)

  const columnNames = columns.map((c) => c.name)
  const allCards = boardColumns.flatMap((c) => c.cards)
  const shownCards = columns.flatMap((c) => c.cards)
  const currentCard = columns[cursorCol]?.cards?.[cursorRow] ?? null
  const activeColumn = columns[cursorCol]
  const panelCard = wide && !panelHidden ? currentCard : null
  const panelCardId = panelCard?.id ?? null
  const doneColumn = boardColumns.find((c) => c.name.toLowerCase() === "done")
  const doneCount = doneColumn?.cards.length ?? 0

  useEffect(() => {
    try {
      localStorage.setItem(showEmptyKey, String(showEmpty))
    } catch {
      // Remembering the choice is a convenience; the board works without it.
    }
  }, [showEmpty])

  // A newly selected card shows the panel again, and the panel reads it.
  useEffect(() => {
    setPanelHidden(false)
  }, [currentCard?.id])

  useEffect(() => {
    if (!panelCardId) return
    let current = true
    fetchCard(baseUrl, panelCardId)
      .then((card) => { if (current) setPanelDetail(card) })
      .catch(() => { if (current) setPanelDetail(null) })
    return () => { current = false }
  }, [baseUrl, panelCardId])

  // Focus board on mount
  useEffect(() => {
    boardRef.current?.focus()
  }, [])

  useEffect(() => {
    if (!requestedHighlightId) {
      setHighlightedTaskId(null)
      return
    }
    for (let colIdx = 0; colIdx < columns.length; colIdx++) {
      const rowIdx = columns[colIdx].cards.findIndex((card) => card.id === requestedHighlightId)
      if (rowIdx < 0) continue
      setCursorCol(colIdx)
      setCursorRow(rowIdx)
      setHighlightedTaskId(requestedHighlightId)
      return
    }
  }, [columns, requestedHighlightId])

  useEffect(() => {
    if (!highlightedTaskId) return
    const timeout = window.setTimeout(() => {
      setHighlightedTaskId(null)
      const next = new URLSearchParams(searchParams)
      next.delete("highlight_task")
      setSearchParams(next, { replace: true })
    }, 8_000)
    return () => window.clearTimeout(timeout)
  }, [highlightedTaskId, searchParams, setSearchParams])

  useEffect(() => {
    if (!requestedHighlightId || highlightedTaskId || loading) return
    const timeout = window.setTimeout(() => {
      const next = new URLSearchParams(searchParams)
      next.delete("highlight_task")
      setSearchParams(next, { replace: true })
    }, 8_000)
    return () => window.clearTimeout(timeout)
  }, [highlightedTaskId, loading, requestedHighlightId, searchParams, setSearchParams])

  // Clamp cursor when columns/cards change; restore cursor to moved card
  useEffect(() => {
    if (movedCardIdRef.current) {
      for (let colIdx = 0; colIdx < columns.length; colIdx++) {
        const rowIdx = columns[colIdx].cards.findIndex((c) => c.id === movedCardIdRef.current)
        if (rowIdx >= 0) {
          setCursorCol(colIdx)
          setCursorRow(rowIdx)
          movedCardIdRef.current = null
          return
        }
      }
      movedCardIdRef.current = null
    }
    if (cursorCol >= columns.length && columns.length > 0) {
      setCursorCol(columns.length - 1)
      setCursorRow(0)
      return
    }
    if (isColumnCollapsed(columns, cursorCol, showEmpty)) {
      const open = stepColumn(columns, cursorCol, 1, showEmpty)
      setCursorCol(open !== cursorCol ? open : stepColumn(columns, cursorCol, -1, showEmpty))
      setCursorRow(0)
      return
    }
    const maxRow = (columns[cursorCol]?.cards.length ?? 0) - 1
    if (cursorRow > maxRow && maxRow >= 0) {
      setCursorRow(maxRow)
    }
  }, [columns, cursorCol, cursorRow, showEmpty])

  // Keyboard navigation
  useEffect(() => {
    const handler = async (e: KeyboardEvent) => {
      // Ignore when modal inputs are focused
      const target = e.target as HTMLElement
      if (target.tagName === "INPUT" || target.tagName === "TEXTAREA" || target.tagName === "SELECT") {
        return
      }

      // Detail panel overrides board keys
      if (detailCard) {
        if (e.key === "Escape") {
          e.preventDefault()
          setDetailCard(null)
          setDetailId(null)
          return
        }
        const detailAsCard = {
          id: detailCard.id,
          title: detailCard.title,
          provider: detailCard.provider,
        } as BoardCard
        if (e.key === "t" && !detailCard.provider) {
          e.preventDefault()
          setOverlay({ kind: "editTitle", card: detailAsCard })
          return
        }
        if (e.key === "e") {
          e.preventDefault()
          setOverlay({ kind: "editDescription", card: detailAsCard })
          return
        }
        if (e.key === "m") {
          e.preventDefault()
          setOverlay({ kind: "move", card: detailMoveCard(detailCard) })
          return
        }
        if (e.key === "D") {
          e.preventDefault()
          setOverlay({ kind: "delete", card: detailAsCard })
          return
        }
        if (e.key === "o") {
          e.preventDefault()
          const providerURL = detailCard.remote_meta?.url || ""
          const prURL = detailCard.pr_meta?.pr_url || ""
          if (providerURL && prURL) {
            setOverlay({ kind: "openURL", providerURL, prURL })
          } else if (providerURL || prURL) {
            window.open(providerURL || prURL, "_blank")
          }
          return
        }
        if (e.key === "y") {
          e.preventDefault()
          try {
            if (detailCard.description_md) {
              await navigator.clipboard.writeText(detailCard.description_md)
            }
          } catch (error) {
            addToast(errorMessage(error, "Could not copy the task description"), "error")
          }
          return
        }
        if (e.key === "Y") {
          e.preventDefault()
          try {
            if (detailId) {
              const text = await fetchCardFull(baseUrl, detailId)
              await navigator.clipboard.writeText(text)
            }
          } catch (error) {
            addToast(errorMessage(error, "Could not copy the full task context"), "error")
          }
          return
        }
        return
      }

      // Overlays consume Esc
      if (e.key === "Escape") {
        if (overlay.kind !== "none") {
          e.preventDefault()
          setOverlay({ kind: "none" })
          return
        }
      }

      // Don't process board nav when an overlay is open
      if (overlay.kind !== "none") {
        if (e.key === "?") {
          setOverlay(overlay.kind === "help" ? { kind: "none" } : { kind: "help" })
        }
        return
      }

      // A dialog the board does not own (one the side panel opened, such as
      // the spawn dialog) keeps the keys for itself.
      if (document.querySelector('[role="dialog"]')) return

      // Search overlay (special — not a modal component)
      if (e.key === "?") {
        e.preventDefault()
        setOverlay({ kind: "help" })
        return
      }

      if (e.key === "/") {
        e.preventDefault()
        setOverlay({ kind: "search" })
        return
      }

      if (e.key === "n") {
        e.preventDefault()
        setOverlay({ kind: "create" })
        return
      }

      if (e.key === "m" || e.key === "M") {
        if (currentCard) {
          e.preventDefault()
          setOverlay({ kind: "move", card: currentCard })
        }
        return
      }

      if (e.key === "d") {
        if (currentCard) {
          e.preventDefault()
          setOverlay({ kind: "delete", card: currentCard })
        }
        return
      }

      if (e.key === "i") {
        e.preventDefault()
        setOverlay({ kind: "import" })
        return
      }

      if (e.key === "p") {
        if (currentCard) {
          e.preventDefault()
          setOverlay({ kind: "linkPR", card: currentCard })
        }
        return
      }

      if (e.key === "w") {
        e.preventDefault()
        // Cycle workspace filter
        const opts = ["all", "unassigned", ...workspaces.map((w) => String(w.id))]
        const idx = opts.indexOf(workspaceFilter)
        setWorkspaceFilter(opts[(idx + 1) % opts.length] || "all")
        return
      }

      if (e.key === "t") {
        if (currentCard && !currentCard.provider) {
          e.preventDefault()
          setOverlay({ kind: "editTitle", card: currentCard })
        }
        return
      }

      if (e.key === "X") {
        if (doneCount > 0) {
          e.preventDefault()
          setOverlay({ kind: "archive" })
        }
        return
      }
      if (e.key === "A") {
        e.preventDefault()
        setOverlay({ kind: "archived" })
        return
      }

      if (e.key === "Enter") {
        if (currentCard) {
          e.preventDefault()
          openDetail(currentCard.id)
        }
        return
      }

      // Navigation
      if (e.key === "h" || e.key === "ArrowLeft" || e.key === "l" || e.key === "ArrowRight") {
        e.preventDefault()
        const step = e.key === "h" || e.key === "ArrowLeft" ? -1 : 1
        setCursorCol(stepColumn(columns, cursorCol, step, showEmpty))
        setCursorRow(0)
        return
      }
      if (e.key === "s") {
        // The panel's own agent button: open, resume or go to the agent.
        const start = panelRef.current?.querySelector<HTMLButtonElement>("[data-task-primary-action]")
        if (start && !start.disabled) {
          e.preventDefault()
          start.click()
        }
        return
      }
      if (e.key === "z") {
        e.preventDefault()
        setShowEmpty((value) => !value)
        return
      }
      if (e.key === "j" || e.key === "ArrowDown") {
        e.preventDefault()
        setCursorRow((r) => {
          const max = (columns[cursorCol]?.cards.length ?? 0) - 1
          return Math.min(max, r + 1)
        })
        return
      }
      if (e.key === "k" || e.key === "ArrowUp") {
        e.preventDefault()
        setCursorRow((r) => Math.max(0, r - 1))
        return
      }
      if (e.key === "g") {
        e.preventDefault()
        setCursorRow(0)
        return
      }
      if (e.key === "G") {
        e.preventDefault()
        setCursorRow(Math.max(0, (columns[cursorCol]?.cards.length ?? 0) - 1))
        return
      }
      const digit = Number(e.key)
      if (!isNaN(digit) && digit >= 1 && digit <= 5) {
        e.preventDefault()
        if (digit <= columns.length) {
          setCursorCol(digit - 1)
          setCursorRow(0)
        }
        return
      }
    }
    window.addEventListener("keydown", handler)
    return () => window.removeEventListener("keydown", handler)
  }, [columns, cursorCol, cursorRow, currentCard, overlay, detailCard, detailId, workspaceFilter, workspaces, doneCount, baseUrl, addToast, showEmpty])

  const openDetail = useCallback(
    async (id: string) => {
      setDetailId(id)
      try {
        const card = await fetchCard(baseUrl, id)
        setDetailCard(card)
      } catch (error) {
        setDetailId(null)
        addToast(errorMessage(error, "Could not load task details"), "error")
      }
    },
    [baseUrl, addToast]
  )

  const handleCardClick = useCallback(
    (col: number, row: number) => {
      setCursorCol(col)
      setCursorRow(row)
      setPanelHidden(false)
      const card = columns[col]?.cards[row]
      if (card && !wide) {
        openDetail(card.id)
      }
    },
    [columns, openDetail, wide]
  )

  const handleCardDoubleClick = useCallback(
    (col: number, row: number) => {
      const card = columns[col]?.cards[row]
      if (card) openDetail(card.id)
    },
    [columns, openDetail]
  )

  const showWorkspace = workspaceFilter === "all"

  const handleCardContextMenu = useCallback((card: BoardCard, position: { x: number; y: number }) => {
    setContextMenu({ card, position })
  }, [])

  const handleContextAction = useCallback((action: CardContextAction) => {
    if (!contextMenu) return
    const card = contextMenu.card
    setContextMenu(null)
    switch (action) {
      case "open":
        void openDetail(card.id)
        break
      case "move":
        setOverlay({ kind: "move", card })
        break
      case "delete":
        setOverlay({ kind: "delete", card })
        break
    }
  }, [contextMenu, openDetail])

  const handleCreate = async (values: {
    title: string
    description: string
    column: string
    priority: string
    workspace_id: number | null
  }) => {
    if (pendingAction) return
    setPendingAction("create")
    try {
      await createTask(baseUrl, {
        ...values,
        workspace_id: values.workspace_id,
      })
      setOverlay({ kind: "none" })
      refresh()
      addToast("Task created", "success")
    } catch (error) {
      addToast(errorMessage(error, "Could not create the task"), "error")
    } finally {
      setPendingAction(null)
    }
  }

  const handleMove = async (targetColumn: string) => {
    const card = overlay.kind === "move" ? overlay.card : currentCard
    if (!card || pendingAction || card.status === targetColumn) return
    setPendingAction("move")
    try {
      const result = await patchTask(baseUrl, card.id, { column: targetColumn })
      const waiting = result.triggers?.find(trigger => trigger.status === "waiting_input" || trigger.status === "waiting_confirmation")
      setOverlay(waiting ? { kind: "workflowInput", request: waiting } : { kind: "none" })
      movedCardIdRef.current = card.id
      refresh()
      addToast(`Moved task to ${targetColumn}`, "success")
    } catch (error) {
      addToast(errorMessage(error, "Could not move the task"), "error")
    } finally {
      setPendingAction(null)
    }
  }

  const handleMoveWorkspace = async (workspaceId: number | null) => {
    const card = overlay.kind === "move" ? overlay.card : null
    if (!card || pendingAction || card.workspace_id === workspaceId) return
    setPendingAction("move")
    try {
      await patchTask(baseUrl, card.id, { workspace_id: workspaceId })
      setOverlay({ kind: "none" })
      refresh()
      const workspaceName = workspaces.find((workspace) => workspace.id === workspaceId)?.name ?? "Unassigned"
      addToast(`Moved task to ${workspaceName}`, "success")
    } catch (error) {
      addToast(errorMessage(error, "Could not move the task to a workspace"), "error")
    } finally {
      setPendingAction(null)
    }
  }

  const handleDragMove = async (cardId: string, targetColumn: string) => {
    const card = allCards.find((candidate) => candidate.id === cardId)
    if (!card || card.status === targetColumn || pendingAction) return
    setPendingAction("move")
    try {
      const result = await patchTask(baseUrl, card.id, { column: targetColumn })
      const waiting = result.triggers?.find(trigger => trigger.status === "waiting_input" || trigger.status === "waiting_confirmation")
      if (waiting) setOverlay({ kind: "workflowInput", request: waiting })
      movedCardIdRef.current = card.id
      refresh()
      addToast(`Moved task to ${targetColumn}`, "success")
    } catch (error) {
      addToast(errorMessage(error, "Could not move the task"), "error")
    } finally {
      setPendingAction(null)
    }
  }

  const handleDelete = async () => {
    if (!currentCard || pendingAction) return
    setPendingAction("delete")
    try {
      await deleteTask(baseUrl, currentCard.id)
      setOverlay({ kind: "none" })
      refresh()
      if (detailCard?.id === currentCard.id) {
        setDetailCard(null)
        setDetailId(null)
      }
      addToast("Task deleted", "success")
    } catch (error) {
      addToast(errorMessage(error, "Could not delete the task"), "error")
    } finally {
      setPendingAction(null)
    }
  }

  const handleArchiveDone = async () => {
    if (pendingAction) return
    setPendingAction("archive")
    try {
      await archiveDone(baseUrl)
      setOverlay({ kind: "none" })
      refresh()
      addToast("Done tasks archived", "success")
    } catch (error) {
      addToast(errorMessage(error, "Could not archive done tasks"), "error")
    } finally {
      setPendingAction(null)
    }
  }

  const handleSearchSelect = (card: BoardCard) => {
    setOverlay({ kind: "none" })
    const colIdx = columns.findIndex((c) => c.cards.some((ca) => ca.id === card.id))
    const rowIdx = columns[colIdx]?.cards.findIndex((ca) => ca.id === card.id) ?? 0
    if (colIdx >= 0) {
      setCursorCol(colIdx)
      setCursorRow(rowIdx)
    }
    openDetail(card.id)
  }

  const handleSaveTitle = async (title: string) => {
    if (!currentCard || pendingAction) return
    setPendingAction("title")
    try {
      await patchTask(baseUrl, currentCard.id, { title })
      setOverlay({ kind: "none" })
      if (detailCard && detailCard.id === currentCard.id) {
        setDetailCard({ ...detailCard, title })
      }
      refresh()
      addToast("Task title updated", "success")
    } catch (error) {
      addToast(errorMessage(error, "Could not update the task title"), "error")
    } finally {
      setPendingAction(null)
    }
  }

  const handleSaveDescription = async (description: string) => {
    if (!currentCard || pendingAction) return
    setPendingAction("description")
    try {
      await patchTask(baseUrl, currentCard.id, { description })
      setOverlay({ kind: "none" })
      if (detailCard && detailCard.id === currentCard.id) {
        setDetailCard(await fetchCard(baseUrl, currentCard.id))
      }
      refresh()
      addToast("Task description updated", "success")
    } catch (error) {
      addToast(errorMessage(error, "Could not update the task description"), "error")
    } finally {
      setPendingAction(null)
    }
  }

  const handleCopyDesc = async () => {
    try {
      if (detailCard?.description_md) {
        await navigator.clipboard.writeText(detailCard.description_md)
        addToast("Description copied", "success")
      }
    } catch (error) {
      addToast(errorMessage(error, "Could not copy the task description"), "error")
    }
  }

  const handleCopyFull = async () => {
    try {
      if (detailId) {
        const text = await fetchCardFull(baseUrl, detailId)
        await navigator.clipboard.writeText(text)
        addToast("Full task context copied", "success")
      }
    } catch (error) {
      addToast(errorMessage(error, "Could not copy the full task context"), "error")
    }
  }

  const handleOpenURL = (url: string) => {
    window.open(url, "_blank")
    setOverlay({ kind: "none" })
  }

  const handleProviderSync = async () => {
    if (syncingProviders) return
    setSyncingProviders(true)
    try {
      const result = await syncBoard(baseUrl)
      await refresh()
      const feedback = providerSyncMessage(result)
      addToast(feedback.message, feedback.type)
    } catch (error) {
      if (error instanceof ProviderSyncConflictError) {
        addToast(error.message, "info")
      } else {
        addToast(errorMessage(error, "Provider sync failed"), "error")
      }
    } finally {
      setSyncingProviders(false)
    }
  }


  return (
    <div
      ref={boardRef}
      className="flex h-full flex-col overflow-hidden bg-lw-ground font-lw text-[13px] leading-5 text-lw-text outline-none"
      tabIndex={0}
    >
      <header data-testid="board-header-mobile" className="flex flex-col gap-3 px-4 pb-2.5 pt-4 md:hidden">
        <div className="flex items-center justify-between">
          <h1 className="m-0 text-2xl font-semibold leading-[30px]">Board</h1>
          <div className="flex gap-1">
            <button
              type="button"
              aria-label="Filter"
              aria-expanded={mobileFilterOpen}
              onClick={() => setMobileFilterOpen((open) => !open)}
              className={`flex h-11 w-11 items-center justify-center rounded-full border border-lw-line-strong ${mobileFilterOpen || query ? "bg-lw-selected text-lw-text" : "bg-lw-raised text-lw-text"}`}
            >
              <Search size={17} aria-hidden="true" />
            </button>
            <button
              type="button"
              aria-label="New card"
              onClick={() => setOverlay({ kind: "create" })}
              className="flex h-11 w-11 items-center justify-center rounded-full bg-lw-accent text-white"
            >
              <Plus size={20} aria-hidden="true" />
            </button>
          </div>
        </div>
        {mobileFilterOpen && (
          <div className="flex flex-col gap-2">
            <CardFilter className="h-10 w-full" autoFocus value={query} onChange={(value) => { setQuery(value); setCursorRow(0) }} />
            <WorkspaceFilter workspaces={workspaces} active={workspaceFilter} onChange={(v) => { setWorkspaceFilter(v); setCursorCol(0); setCursorRow(0) }} />
          </div>
        )}
      </header>

      <header data-testid="board-header" className="hidden items-center justify-between gap-4 border-b border-[#222228] px-5 py-3.5 md:flex">
        <div className="flex min-w-0 items-baseline gap-3">
          <h1 className="m-0 text-lg font-semibold leading-6">Board</h1>
          <span className="truncate text-[13px] text-lw-text-2">{cardCountLabel(allCards, shownCards)}</span>
        </div>
        <div className="flex min-w-0 items-center gap-2">
          <WorkspaceFilter
            className="max-w-[40vw]"
            workspaces={workspaces}
            active={workspaceFilter}
            onChange={(v) => {
              setWorkspaceFilter(v)
              setCursorCol(0)
              setCursorRow(0)
            }}
          />
          <CardFilter className="w-[220px]" value={query} onChange={(value) => { setQuery(value); setCursorRow(0) }} />
          <button
            aria-label="Sync providers"
            title="Sync providers"
            onClick={() => void handleProviderSync()}
            disabled={syncingProviders}
            className="flex h-8 w-8 items-center justify-center rounded-md text-lw-text-2 hover:bg-lw-raised hover:text-lw-text disabled:cursor-wait disabled:opacity-60"
          >
            <RefreshCw size={15} className={syncingProviders ? "animate-spin" : ""} />
          </button>
          <button
            onClick={() => setOverlay({ kind: "create" })}
            className="h-8 rounded-md bg-lw-accent px-3.5 text-[13px] font-medium text-white hover:brightness-110"
          >
            + New card
          </button>
        </div>
      </header>

      <div className="flex min-h-0 flex-1">
        <div className="min-w-0 flex-1 overflow-hidden">
          {loading && columns.length === 0 && (
            <LoadingState label="Loading the board…" />
          )}
          {error && columns.length === 0 && (
            <div className="flex h-full items-center justify-center">
              <p className="text-sm text-lw-error">{error}</p>
            </div>
          )}
          {columns.length > 0 && (
            <>
              <div className="hidden h-full md:block">
                <BoardGrid
                  columns={columns}
                  cursorCol={cursorCol}
                  cursorRow={cursorRow}
                  showWorkspace={showWorkspace}
                  showEmpty={showEmpty}
                  onToggleEmpty={() => setShowEmpty((value) => !value)}
                  highlightedTaskId={highlightedTaskId}
                  onCardClick={handleCardClick}
                  onCardDoubleClick={handleCardDoubleClick}
                  onCardContextMenu={handleCardContextMenu}
                  onCardMove={handleDragMove}
                />
              </div>
              <div className="h-full md:hidden">
                <MobileBoardList columns={columns} showWorkspace={showWorkspace} highlightedTaskId={highlightedTaskId} onCardClick={handleCardClick} onCardContextMenu={handleCardContextMenu} />
              </div>
            </>
          )}
          {!loading && !error && columns.length === 0 && (
            <div className="flex h-full items-center justify-center">
              <p className="text-sm text-lw-faint">No columns available</p>
            </div>
          )}
        </div>
        {panelCard && (
          <div ref={panelRef} className="contents">
          <BoardTaskPanel
            card={panelCard}
            column={activeColumn?.name ?? panelCard.status}
            detail={panelDetail}
            onClose={() => setPanelHidden(true)}
            onOpenDetails={() => void openDetail(panelCard.id)}
            onMove={() => setOverlay({ kind: "move", card: panelCard })}
            onOpenURL={(url) => window.open(url, "_blank")}
          />
          </div>
        )}
      </div>

      <footer data-testid="board-keys" className="hidden gap-[18px] border-t border-[#222228] px-5 py-1.5 text-xs text-lw-faint md:flex">
        {boardKeys(showEmpty, Boolean(panelCard)).map(([key, label]) => (
          <span key={key}><span className="text-[#b4b3bd]">{key}</span> {label}</span>
        ))}
      </footer>

      {contextMenu && <CardContextMenu
        card={contextMenu.card}
        position={contextMenu.position}
        workflows={cardWorkflowOptions(workflowDiscovery.data?.definitions ?? [])}
        workflowLoading={workflowDiscovery.loading}
        workflowWarning={(workflowDiscovery.data?.errors ?? []).map(item => `${item.path}: ${item.error}`).join("; ") || (workflowDiscovery.error ?? "")}
        onAction={handleContextAction}
        onWorkflow={workflow => { setCardWorkflow({ card: contextMenu.card, workflow }); setContextMenu(null) }}
        onClose={() => setContextMenu(null)}
      />}
      {cardWorkflow && <RunWorkflowDialog definition={cardWorkflow.workflow.definition} repositories={workflowDiscovery.data?.repositories ?? []} initialValues={{ [cardWorkflow.workflow.taskInput]: cardWorkflow.card.id }} readOnlyInputs={[cardWorkflow.workflow.taskInput]} onClose={() => setCardWorkflow(null)} />}

      {/* Overlays */}
      <CreateTaskModal
        open={overlay.kind === "create"}
        columns={columnNames}
        currentColumn={activeColumn?.name || (columnNames[0] ?? "")}
        workspaces={workspaces}
        baseUrl={baseUrl}
        voiceEnabled={voiceEnabled}
        defaultWorkspaceId={settings.data?.default_workspace_id ?? null}
        onClose={() => setOverlay({ kind: "none" })}
        onSubmit={handleCreate}
        loading={pendingAction === "create"}
      />

      <MoveTaskModal
        open={overlay.kind === "move"}
        taskId={overlay.kind === "move" ? overlay.card.id : ""}
        taskTitle={overlay.kind === "move" ? overlay.card.title : ""}
        columns={columnNames}
        currentColumn={overlay.kind === "move" ? overlay.card.status : ""}
        workspaces={workspaces}
        currentWorkspaceId={overlay.kind === "move" ? overlay.card.workspace_id : null}
        onClose={() => setOverlay({ kind: "none" })}
        onMoveColumn={handleMove}
        onMoveWorkspace={handleMoveWorkspace}
        loading={pendingAction === "move"}
      />

      <DeleteTaskModal
        open={overlay.kind === "delete"}
        taskId={overlay.kind === "delete" ? overlay.card.id : ""}
        taskTitle={overlay.kind === "delete" ? overlay.card.title : ""}
        isRemote={!!(overlay.kind === "delete" && overlay.card.provider)}
        onClose={() => setOverlay({ kind: "none" })}
        onConfirm={handleDelete}
        loading={pendingAction === "delete"}
      />

      <ArchiveDoneModal
        open={overlay.kind === "archive"}
        count={doneCount}
        onClose={() => setOverlay({ kind: "none" })}
        onConfirm={handleArchiveDone}
        loading={pendingAction === "archive"}
      />
      <ArchivedTasksModal
        open={overlay.kind === "archived"}
        onClose={() => setOverlay({ kind: "none" })}
        onRestored={() => {
          refresh()
          addToast("Task restored", "success")
        }}
      />

      <SearchModal
        open={overlay.kind === "search"}
        onClose={() => setOverlay({ kind: "none" })}
        onSelect={handleSearchSelect}
      />

      <LinkPRModal
        open={overlay.kind === "linkPR"}
        taskId={overlay.kind === "linkPR" ? overlay.card.id : ""}
        onClose={() => setOverlay({ kind: "none" })}
        onLinked={() => {
          setOverlay({ kind: "none" })
          refresh()
          addToast("Pull request linked", "success")
          if (detailId) {
            fetchCard(baseUrl, detailId).then(setDetailCard).catch((error) => {
              addToast(errorMessage(error, "PR linked, but task details could not be refreshed"), "error")
            })
          }
        }}
      />

      <ImportRemoteModal
        open={overlay.kind === "import"}
        workspaces={workspaces}
        defaultWorkspaceId={settings.data?.default_import_workspace_id ?? null}
        onClose={() => setOverlay({ kind: "none" })}
        onImported={() => {
          setOverlay({ kind: "none" })
          refresh()
          addToast("Remote task imported", "success")
        }}
      />

      <EditTitleModal
        open={overlay.kind === "editTitle"}
        currentTitle={overlay.kind === "editTitle" ? overlay.card.title : ""}
        onClose={() => setOverlay({ kind: "none" })}
        onSave={handleSaveTitle}
        loading={pendingAction === "title"}
      />

      <EditDescriptionModal
        open={overlay.kind === "editDescription"}
        currentDescription={detailCard?.local_description_md || ""}
        providerBacked={Boolean(detailCard?.provider)}
        baseUrl={baseUrl}
        voiceEnabled={voiceEnabled}
        onClose={() => setOverlay({ kind: "none" })}
        onSave={handleSaveDescription}
        loading={pendingAction === "description"}
      />

      <HelpModal
        open={overlay.kind === "help"}
        onClose={() => setOverlay({ kind: "none" })}
      />

      <OpenURLPicker
        open={overlay.kind === "openURL"}
        providerURL={overlay.kind === "openURL" ? overlay.providerURL : ""}
        prURL={overlay.kind === "openURL" ? overlay.prURL : ""}
        onSelect={handleOpenURL}
        onClose={() => setOverlay({ kind: "none" })}
      />

      <WorktreeModal
        open={overlay.kind === "worktree"}
        taskId={detailCard?.id ?? ""}
        defaultPrimaryDir={defaultWorkingDir}
        repositories={repositories}
        onClose={() => setOverlay({ kind: "none" })}
        onCreated={async () => {
          setOverlay({ kind: "none" })
          await refresh()
          if (detailCard) await openDetail(detailCard.id)
          addToast("Worktree created", "success")
        }}
      />

      {overlay.kind === "workflowInput" && <WorkflowTriggerInputModal baseUrl={baseUrl} request={overlay.request} repositories={repositories} onClose={()=>setOverlay({kind:"none"})} onStarted={next=>{if(next.status==="waiting_confirmation"){setOverlay({kind:"workflowInput",request:next})}else{setOverlay({kind:"none"});addToast("Workflow started","success")}}}/>}

      {/* Detail panel */}
      {detailCard && (
        <DetailPanel
          card={detailCard}
          onClose={() => {
            setDetailCard(null)
            setDetailId(null)
          }}
          onEditTitle={() => {
            if (detailCard && !detailCard.provider) {
              setOverlay({ kind: "editTitle", card: { id: detailCard.id, title: detailCard.title } as BoardCard })
            }
          }}
          onEditDescription={() => {
            if (detailCard) {
              setOverlay({ kind: "editDescription", card: { id: detailCard.id, title: detailCard.title } as BoardCard })
            }
          }}
          onMove={() => {
            if (detailCard) {
              setOverlay({ kind: "move", card: detailMoveCard(detailCard) })
            }
          }}
          onDelete={() => {
            if (detailCard) {
              setOverlay({ kind: "delete", card: { id: detailCard.id, title: detailCard.title } as BoardCard })
            }
          }}
          onLinkPR={() => {
            if (detailCard) {
              setOverlay({ kind: "linkPR", card: { id: detailCard.id, title: detailCard.title } as BoardCard })
            }
          }}
          onRefreshPR={async () => {
            const response = await apiFetch(baseUrl, `/api/tasks/${encodeURIComponent(detailCard.id)}/refresh-pr`, { method: "POST" })
            if (!response.ok) {
              addToast(await response.text(), "error")
              return
            }
            await refresh()
            await openDetail(detailCard.id)
            addToast("PR refreshed", "success")
          }}
          onRediscoverPR={async () => {
            const response = await apiFetch(baseUrl, `/api/tasks/${encodeURIComponent(detailCard.id)}/rediscover-pr`, { method: "POST" })
            if (!response.ok) {
              addToast(await response.text(), "error")
              return
            }
            await refresh()
            await openDetail(detailCard.id)
            addToast("PR rediscovered", "success")
          }}
          onCopyDescription={handleCopyDesc}
          onCopyFull={handleCopyFull}
          onOpenURL={() => {
            const providerURL = detailCard.remote_meta?.url || ""
            const prURL = detailCard.pr_meta?.pr_url || ""
            if (providerURL && prURL) {
              setOverlay({ kind: "openURL", providerURL, prURL })
            } else if (providerURL || prURL) {
              window.open(providerURL || prURL, "_blank")
            }
          }}
          onCancelSwarm={() => {
            if (detailCard) {
              setOverlay({ kind: "cancelSwarm", card: { id: detailCard.id, title: detailCard.title } as BoardCard })
            }
          }}
          worktreesEnabled={worktreesEnabled}
          onCreateWorktree={() => setOverlay({ kind: "worktree" })}
        />
      )}

      <CancelSwarmModal
        taskId={overlay.kind === "cancelSwarm" ? overlay.card.id : ""}
        taskTitle={overlay.kind === "cancelSwarm" ? overlay.card.title : ""}
        isOpen={overlay.kind === "cancelSwarm"}
        onClose={() => setOverlay({ kind: "none" })}
        onConfirm={() => {
          setOverlay({ kind: "none" })
          setDetailCard(null)
          setDetailId(null)
          refresh()
          addToast("Swarm cancelled", "success")
        }}
      />
    </div>
  )
}
