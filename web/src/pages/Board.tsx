import { useState, useEffect, useCallback, useRef } from "react"
import { useServer } from "../hooks/useServer"
import { useBoard } from "../hooks/useBoard"
import {
  createTask,
  patchTask,
  deleteTask,
  archiveDone,
  fetchCard,
  fetchCardFull,
} from "../lib/board"
import type { BoardCard, CardDetail } from "../lib/board-types"
import { BoardGrid } from "../components/board/BoardGrid"
import { WorkspaceFilter } from "../components/board/WorkspaceFilter"
import { DetailPanel } from "../components/board/DetailPanel"
import { CreateTaskModal } from "../components/board/CreateTaskModal"
import { MoveTaskModal } from "../components/board/MoveTaskModal"
import { DeleteTaskModal } from "../components/board/DeleteTaskModal"
import { ArchiveDoneModal } from "../components/board/ArchiveDoneModal"
import { SearchModal } from "../components/board/SearchModal"
import { LinkPRModal } from "../components/board/LinkPRModal"
import { ImportRemoteModal } from "../components/board/ImportRemoteModal"
import { EditTitleModal } from "../components/board/EditTitleModal"
import { EditDescriptionModal } from "../components/board/EditDescriptionModal"
import { HelpModal } from "../components/board/HelpModal"
import { OpenURLPicker } from "../components/board/OpenURLPicker"
import { CancelSwarmModal } from "../components/board/CancelSwarmModal"

type Overlay =
  | { kind: "none" }
  | { kind: "create" }
  | { kind: "move"; card: BoardCard }
  | { kind: "delete"; card: BoardCard }
  | { kind: "archive" }
  | { kind: "search" }
  | { kind: "linkPR"; card: BoardCard }
  | { kind: "import" }
  | { kind: "editTitle"; card: BoardCard }
  | { kind: "editDescription"; card: BoardCard }
  | { kind: "help" }
  | { kind: "openURL"; providerURL: string; prURL: string }
  | { kind: "cancelSwarm"; card: BoardCard }

export function BoardPage() {
  const { baseUrl } = useServer()
  const [workspaceFilter, setWorkspaceFilter] = useState<string>("all")
  const { columns, workspaces, loading, error, refresh } = useBoard(workspaceFilter)
  const [cursorCol, setCursorCol] = useState(0)
  const [cursorRow, setCursorRow] = useState(0)
  const [overlay, setOverlay] = useState<Overlay>({ kind: "none" })
  const [detailCard, setDetailCard] = useState<CardDetail | null>(null)
  const [detailId, setDetailId] = useState<string | null>(null)
  const boardRef = useRef<HTMLDivElement>(null)
  const movedCardIdRef = useRef<string | null>(null)

  const columnNames = columns.map((c) => c.name)
  const allCards = columns.flatMap((c) => c.cards)
  const currentCard = columns[cursorCol]?.cards?.[cursorRow] ?? null
  const activeColumn = columns[cursorCol]
  const doneColumn = columns.find((c) => c.name.toLowerCase() === "done")
  const doneCount = doneColumn?.cards.length ?? 0

  // Focus board on mount
  useEffect(() => {
    boardRef.current?.focus()
  }, [])

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
    const maxRow = (columns[cursorCol]?.cards.length ?? 0) - 1
    if (cursorRow > maxRow && maxRow >= 0) {
      setCursorRow(maxRow)
    }
  }, [columns, cursorCol, cursorRow])

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
        if (e.key === "e" && !detailCard.provider) {
          e.preventDefault()
          setOverlay({ kind: "editDescription", card: detailAsCard })
          return
        }
        if (e.key === "m") {
          e.preventDefault()
          setOverlay({ kind: "move", card: detailAsCard })
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
          } catch {}
          return
        }
        if (e.key === "Y") {
          e.preventDefault()
          try {
            if (detailId) {
              const text = await fetchCardFull(baseUrl, detailId)
              await navigator.clipboard.writeText(text)
            }
          } catch {}
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

      if (e.key === "Enter") {
        if (currentCard) {
          e.preventDefault()
          openDetail(currentCard.id)
        }
        return
      }

      // Navigation
      if (e.key === "h" || e.key === "ArrowLeft") {
        e.preventDefault()
        setCursorCol((c) => {
          const n = Math.max(0, c - 1)
          setCursorRow(0)
          return n
        })
        return
      }
      if (e.key === "l" || e.key === "ArrowRight") {
        e.preventDefault()
        setCursorCol((c) => {
          const n = Math.min(columns.length - 1, c + 1)
          setCursorRow(0)
          return n
        })
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
  }, [columns, cursorCol, cursorRow, currentCard, overlay, detailCard, detailId, workspaceFilter, workspaces, doneCount, baseUrl])

  const openDetail = useCallback(
    async (id: string) => {
      setDetailId(id)
      try {
        const card = await fetchCard(baseUrl, id)
        setDetailCard(card)
      } catch {
        setDetailId(null)
      }
    },
    [baseUrl]
  )

  const handleCardClick = useCallback(
    (col: number, row: number) => {
      setCursorCol(col)
      setCursorRow(row)
      const card = columns[col]?.cards[row]
      if (card) {
        openDetail(card.id)
      }
    },
    [columns, openDetail]
  )

  const showWorkspace = workspaceFilter === "all"

  const handleCreate = async (values: {
    title: string
    description: string
    column: string
    priority: string
    workspace_id: number | null
  }) => {
    try {
      await createTask(baseUrl, {
        ...values,
        workspace_id: values.workspace_id,
      })
      setOverlay({ kind: "none" })
      refresh()
    } catch (e) {
      // Error could be surfaced if we added a toast; silently failing for now
    }
  }

  const handleMove = async (targetColumn: string) => {
    if (!currentCard) return
    try {
      await patchTask(baseUrl, currentCard.id, { column: targetColumn })
      setOverlay({ kind: "none" })
      movedCardIdRef.current = currentCard.id
      refresh()
    } catch (e) {}
  }

  const handleDelete = async () => {
    if (!currentCard) return
    try {
      await deleteTask(baseUrl, currentCard.id)
      setOverlay({ kind: "none" })
      refresh()
      if (detailCard?.id === currentCard.id) {
        setDetailCard(null)
        setDetailId(null)
      }
    } catch (e) {}
  }

  const handleArchiveDone = async () => {
    try {
      await archiveDone(baseUrl)
      setOverlay({ kind: "none" })
      refresh()
    } catch (e) {}
  }

  const handleSearchSelect = (card: BoardCard) => {
    setOverlay({ kind: "none" })
    const colIdx = columns.findIndex((c) => c.cards.some((ca) => ca.id === card.id))
    const rowIdx = columns[colIdx]?.cards.findIndex((ca) => ca.id === card.id) ?? 0
    if (colIdx >= 0) {
      setCursorCol(colIdx)
      setCursorRow(rowIdx)
      openDetail(card.id)
    }
  }

  const handleSaveTitle = async (title: string) => {
    if (!currentCard) return
    try {
      await patchTask(baseUrl, currentCard.id, { title })
      setOverlay({ kind: "none" })
      if (detailCard && detailCard.id === currentCard.id) {
        setDetailCard({ ...detailCard, title })
      }
      refresh()
    } catch (e) {}
  }

  const handleSaveDescription = async (description: string) => {
    if (!currentCard) return
    try {
      await patchTask(baseUrl, currentCard.id, { description })
      setOverlay({ kind: "none" })
      if (detailCard && detailCard.id === currentCard.id) {
        setDetailCard({ ...detailCard, description_md: description })
      }
      refresh()
    } catch (e) {}
  }

  const handleCopyDesc = async () => {
    try {
      if (detailCard?.description_md) {
        await navigator.clipboard.writeText(detailCard.description_md)
      }
    } catch {}
  }

  const handleCopyFull = async () => {
    try {
      if (detailId) {
        const text = await fetchCardFull(baseUrl, detailId)
        await navigator.clipboard.writeText(text)
      }
    } catch {}
  }

  const handleOpenURL = (url: string) => {
    window.open(url, "_blank")
    setOverlay({ kind: "none" })
  }

  const activeWorkspace = workspaces.find((w) => String(w.id) === workspaceFilter)
  const filterLabel = activeWorkspace?.name || (workspaceFilter === "unassigned" ? "Unassigned" : "All")

  return (
    <div
      ref={boardRef}
      className="flex h-full flex-col overflow-hidden outline-none"
      tabIndex={0}
    >
      {/* Toolbar */}
      <div className="flex items-center justify-between border-b border-zinc-800 px-4 py-2">
        <div className="flex items-center gap-3">
          <span className="text-xs font-bold uppercase tracking-wider text-zinc-500">Board</span>
          <span className="text-[10px] text-zinc-600">{allCards.length} cards</span>
        </div>
        <div className="flex items-center gap-2">
          <WorkspaceFilter
            workspaces={workspaces}
            active={workspaceFilter}
            onChange={(v) => {
              setWorkspaceFilter(v)
              setCursorCol(0)
              setCursorRow(0)
            }}
          />
          <button
            onClick={() => setOverlay({ kind: "create" })}
            className="rounded border border-zinc-700 bg-zinc-900 px-2.5 py-1.5 text-xs text-zinc-300 transition-colors hover:bg-zinc-800"
          >
            + New
          </button>
        </div>
      </div>

      {/* Board */}
      <div className="flex-1 overflow-hidden">
        {loading && columns.length === 0 && (
          <div className="flex h-full items-center justify-center">
            <p className="text-sm text-zinc-600">Loading...</p>
          </div>
        )}
        {error && columns.length === 0 && (
          <div className="flex h-full items-center justify-center">
            <p className="text-sm text-red-400">{error}</p>
          </div>
        )}
        {columns.length > 0 && (
          <BoardGrid
            columns={columns}
            cursorCol={cursorCol}
            cursorRow={cursorRow}
            showWorkspace={showWorkspace}
            onCardClick={handleCardClick}
          />
        )}
        {!loading && !error && columns.length === 0 && (
          <div className="flex h-full items-center justify-center">
            <p className="text-sm text-zinc-600">No columns available</p>
          </div>
        )}
      </div>

      {/* Status bar */}
      <div className="flex items-center gap-3 border-t border-zinc-800 px-4 py-1.5 text-[10px] text-zinc-600">
        <span>h/l columns · j/k scroll · enter detail · ? help</span>
        {workspaceFilter !== "all" && (
          <span className="rounded bg-zinc-800 px-1.5 py-0.5 text-zinc-500">{filterLabel}</span>
        )}
      </div>

      {/* Overlays */}
      <CreateTaskModal
        open={overlay.kind === "create"}
        columns={columnNames}
        currentColumn={activeColumn?.name || (columnNames[0] ?? "")}
        workspaces={workspaces}
        onClose={() => setOverlay({ kind: "none" })}
        onSubmit={handleCreate}
      />

      <MoveTaskModal
        open={overlay.kind === "move"}
        taskId={overlay.kind === "move" ? overlay.card.id : ""}
        taskTitle={overlay.kind === "move" ? overlay.card.title : ""}
        columns={columnNames}
        currentColumn={activeColumn?.name || ""}
        onClose={() => setOverlay({ kind: "none" })}
        onMove={handleMove}
      />

      <DeleteTaskModal
        open={overlay.kind === "delete"}
        taskId={overlay.kind === "delete" ? overlay.card.id : ""}
        taskTitle={overlay.kind === "delete" ? overlay.card.title : ""}
        isRemote={!!(overlay.kind === "delete" && overlay.card.provider)}
        onClose={() => setOverlay({ kind: "none" })}
        onConfirm={handleDelete}
      />

      <ArchiveDoneModal
        open={overlay.kind === "archive"}
        count={doneCount}
        onClose={() => setOverlay({ kind: "none" })}
        onConfirm={handleArchiveDone}
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
          if (detailId) {
            fetchCard(baseUrl, detailId).then(setDetailCard).catch(() => {})
          }
        }}
      />

      <ImportRemoteModal
        open={overlay.kind === "import"}
        onClose={() => setOverlay({ kind: "none" })}
        onImported={() => {
          setOverlay({ kind: "none" })
          refresh()
        }}
      />

      <EditTitleModal
        open={overlay.kind === "editTitle"}
        currentTitle={overlay.kind === "editTitle" ? overlay.card.title : ""}
        onClose={() => setOverlay({ kind: "none" })}
        onSave={handleSaveTitle}
      />

      <EditDescriptionModal
        open={overlay.kind === "editDescription"}
        currentDescription={detailCard?.description_md || ""}
        onClose={() => setOverlay({ kind: "none" })}
        onSave={handleSaveDescription}
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
            if (detailCard && !detailCard.provider) {
              setOverlay({ kind: "editDescription", card: { id: detailCard.id, title: detailCard.title } as BoardCard })
            }
          }}
          onMove={() => {
            if (detailCard) {
              setOverlay({ kind: "move", card: { id: detailCard.id, title: detailCard.title } as BoardCard })
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
        }}
      />
    </div>
  )
}
