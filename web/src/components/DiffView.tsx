import { useEffect, useRef, useState } from "react"
import { cn } from "../lib/utils"
import type { DiffSelection, FileDiff, ReviewHunkNote } from "../lib/review"

interface DiffViewProps {
  files: FileDiff[]
  hunkNotes?: ReviewHunkNote[]
  selection?: DiffSelection | null
  onSelectionChange?: (selection: DiffSelection | null) => void
}

export function DiffView({ files, hunkNotes = [], selection, onSelectionChange }: DiffViewProps) {
  const [viewedHunks, setViewedHunks] = useState<Set<string>>(() => new Set())
  const dragOriginRef = useRef<{ filePath: string; hunkAnchor: string; lineIndex: number; moved: boolean } | null>(null)
  const suppressClickRef = useRef(false)

  useEffect(() => {
    const endDrag = () => {
      suppressClickRef.current = dragOriginRef.current?.moved ?? false
      dragOriginRef.current = null
    }
    window.addEventListener("pointerup", endDrag)
    window.addEventListener("pointercancel", endDrag)
    return () => {
      window.removeEventListener("pointerup", endDrag)
      window.removeEventListener("pointercancel", endDrag)
    }
  }, [])

  if (files.length === 0) {
    return <div className="rounded border border-zinc-800 p-6 text-center text-sm text-zinc-500">No file changes in this step.</div>
  }

  return (
    <div className="space-y-4 font-mono text-xs">
      {files.map((file, fileIndex) => {
        const path = file.old_path !== file.new_path
          ? `${file.old_path} → ${file.new_path}`
          : file.new_path || file.old_path
        const selectionPath = file.new_path || file.old_path
        return (
          <section key={`${path}-${fileIndex}`} className="rounded border border-zinc-800 bg-zinc-950">
            <header data-sticky-file-header className="sticky top-7 z-10 flex items-center justify-between rounded-t border-b border-zinc-800 bg-zinc-900 px-3 py-2 shadow-md shadow-black/30">
              <span className="truncate text-zinc-200">{path}</span>
              <span className="ml-3 rounded bg-zinc-800 px-1.5 py-0.5 text-[10px] uppercase text-zinc-400">{file.status}</span>
            </header>
            {file.hunks.map((hunk, hunkIndex) => {
              const hunkKey = `${selectionPath}:${hunk.anchor}`
              const viewed = viewedHunks.has(hunkKey)
              const toggleViewed = () => {
                setViewedHunks((current) => {
                  const next = new Set(current)
                  if (viewed) next.delete(hunkKey)
                  else next.add(hunkKey)
                  return next
                })
                if (!viewed && selection?.file_path === selectionPath && selection.hunk_anchor === hunk.anchor) {
                  onSelectionChange?.(null)
                }
              }
              return (
              <div key={`${hunk.header}-${hunkIndex}`} data-hunk-anchor={hunk.anchor}>
                {hunkNotes
                  .filter((note) => note.hunk_anchor === hunk.anchor && (note.file_path === file.new_path || note.file_path === file.old_path))
                  .map((note) => (
                    <div key={note.id} className="border-b border-amber-800 bg-amber-950/40 px-3 py-2 font-sans text-sm text-amber-100">
                      {note.body}
                    </div>
                  ))}
                <div className="flex items-center justify-between border-y border-zinc-800 bg-indigo-950/40 px-3 py-1 text-indigo-300">
                  <span>{hunk.header}</span>
                  <label className="flex cursor-pointer items-center gap-1.5 font-sans text-[10px] text-zinc-400 hover:text-zinc-200">
                    <input type="checkbox" checked={viewed} onChange={toggleViewed} aria-label={`Viewed ${hunk.header}`} className="accent-indigo-500" />
                    Viewed
                  </label>
                </div>
                {!viewed && hunk.lines.map((line, lineIndex) => {
                  const selected = selection?.file_path === selectionPath
                    && selection.hunk_anchor === hunk.anchor
                    && lineIndex >= selection.start && lineIndex <= selection.end
                  const selectLine = () => {
                    if (suppressClickRef.current) {
                      suppressClickRef.current = false
                      return
                    }
                    if (!onSelectionChange) return
                    if (selection?.file_path === selectionPath && selection.hunk_anchor === hunk.anchor) {
                      onSelectionChange({ ...selection, start: Math.min(selection.start, lineIndex), end: Math.max(selection.end, lineIndex) })
                    } else {
                      onSelectionChange({ file_path: selectionPath, hunk_anchor: hunk.anchor, start: lineIndex, end: lineIndex })
                    }
                  }
                  const startDrag = (event: React.PointerEvent) => {
                    if (event.button !== 0 || !onSelectionChange) return
                    dragOriginRef.current = { filePath: selectionPath, hunkAnchor: hunk.anchor, lineIndex, moved: false }
                    onSelectionChange({ file_path: selectionPath, hunk_anchor: hunk.anchor, start: lineIndex, end: lineIndex })
                  }
                  const extendDrag = () => {
                    const origin = dragOriginRef.current
                    if (!origin || origin.filePath !== selectionPath || origin.hunkAnchor !== hunk.anchor) return
                    origin.moved ||= origin.lineIndex !== lineIndex
                    onSelectionChange?.({
                      file_path: selectionPath,
                      hunk_anchor: hunk.anchor,
                      start: Math.min(origin.lineIndex, lineIndex),
                      end: Math.max(origin.lineIndex, lineIndex),
                    })
                  }
                  const gutter = (side: "old" | "new", lineNo: number) => lineNo ? (
                    <button
                      type="button"
                      aria-label={`Select ${side} line ${lineNo}`}
                      onClick={selectLine}
                      onPointerDown={startDrag}
                      className="select-none touch-none border-r border-zinc-800 px-2 text-right text-zinc-600 hover:bg-indigo-900/50 hover:text-indigo-200"
                    >
                      {lineNo}
                    </button>
                  ) : <span className="border-r border-zinc-800" />
                  return (
                    <div
                      key={lineIndex}
                      data-diff-line
                      data-selected={selected ? "true" : undefined}
                      onPointerEnter={extendDrag}
                      className={cn(
                        "grid grid-cols-[3rem_3rem_1fr] leading-5",
                        line.kind === "add" && "bg-emerald-950/40 text-emerald-200",
                        line.kind === "del" && "bg-red-950/40 text-red-200",
                        line.kind === "ctx" && "text-zinc-400",
                        selected && "bg-indigo-900/60 text-indigo-100 ring-1 ring-inset ring-indigo-600",
                      )}
                    >
                      {gutter("old", line.old_no)}
                      {gutter("new", line.new_no)}
                      <span className="whitespace-pre px-2"><span aria-hidden>{line.kind === "add" ? "+" : line.kind === "del" ? "-" : " "}</span>{line.text}</span>
                    </div>
                  )
                })}
              </div>
              )
            })}
          </section>
        )
      })}
    </div>
  )
}
