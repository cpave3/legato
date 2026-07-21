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
    <div className="min-w-0 space-y-4 font-mono text-xs">
      {files.map((file, fileIndex) => {
        const path = file.old_path !== file.new_path
          ? `${file.old_path} → ${file.new_path}`
          : file.new_path || file.old_path
        const selectionPath = file.new_path || file.old_path
        return (
          <section key={`${path}-${fileIndex}`} className="rounded border border-zinc-800 bg-zinc-950">
            <header data-sticky-file-header className="sticky top-[27px] z-10 flex items-center justify-between rounded-t border-b border-zinc-800 bg-zinc-900 px-3 py-2 shadow-md shadow-black/30">
              <span className="truncate text-zinc-200">{path}</span>
              <span className="ml-3 rounded bg-zinc-800 px-1.5 py-0.5 text-[10px] uppercase text-zinc-400">{file.status}</span>
            </header>
            <div data-diff-lines className="max-w-full overflow-x-auto">
            {file.hunks.map((hunk, hunkIndex) => {
              const hunkKey = `${selectionPath}:${hunk.anchor}`
              const notesForHunk = hunkNotes.filter((note) => note.hunk_anchor === hunk.anchor && (note.file_path === file.new_path || note.file_path === file.old_path))
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
                {notesForHunk
                  .filter((note) => !note.line_start || !note.line_end)
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
                {!viewed && <HunkLines
                  lines={hunk.lines}
                  hunkAnchor={hunk.anchor}
                  selectionPath={selectionPath}
                  selection={selection}
                  onSelectionChange={onSelectionChange}
                  notesForHunk={notesForHunk}
                  dragOriginRef={dragOriginRef}
                  suppressClickRef={suppressClickRef}
                />}
              </div>
              )
            })}
            </div>
          </section>
        )
      })}
    </div>
  )
}

interface HunkLinesProps {
  lines: FileDiff["hunks"][number]["lines"]
  hunkAnchor: string
  selectionPath: string
  selection?: DiffSelection | null
  onSelectionChange?: (selection: DiffSelection | null) => void
  notesForHunk: ReviewHunkNote[]
  dragOriginRef: React.MutableRefObject<{ filePath: string; hunkAnchor: string; lineIndex: number; moved: boolean } | null>
  suppressClickRef: React.MutableRefObject<boolean>
}

interface NoteGroup {
  note: ReviewHunkNote
  startIndex: number
  endIndex: number
}

function groupRangeNotes(notes: ReviewHunkNote[]): NoteGroup[] {
  const groups: NoteGroup[] = []
  notes.forEach((note) => {
    if (note.line_start && note.line_end) {
      groups.push({ note, startIndex: note.line_start - 1, endIndex: note.line_end - 1 })
    }
  })
  groups.sort((a, b) => a.startIndex - b.startIndex)
  return groups
}

function HunkLines({ lines, hunkAnchor, selectionPath, selection, onSelectionChange, notesForHunk, dragOriginRef, suppressClickRef }: HunkLinesProps) {
  const groups = groupRangeNotes(notesForHunk)
  const selectedRange =
    selection?.file_path === selectionPath && selection.hunk_anchor === hunkAnchor
      ? { start: selection.start, end: selection.end }
      : null
  const result: React.ReactNode[] = []
  let processed = 0

  while (processed < lines.length) {
    if (selectedRange && selectedRange.start === processed) {
      result.push(
        <SelectionBlock
          key={`sel-${processed}`}
          lines={lines}
          startIndex={selectedRange.start}
          endIndex={selectedRange.end}
          hunkAnchor={hunkAnchor}
          selectionPath={selectionPath}
          selection={selection}
          onSelectionChange={onSelectionChange}
          dragOriginRef={dragOriginRef}
          suppressClickRef={suppressClickRef}
        />
      )
      processed = selectedRange.end + 1
      continue
    }
    const group = groups.find((g) => g.startIndex === processed)
    if (!group) {
      result.push(
        <DiffLine
          key={processed}
          lineIndex={processed}
          line={lines[processed]}
          hunkAnchor={hunkAnchor}
          selectionPath={selectionPath}
          selection={selection}
          onSelectionChange={onSelectionChange}
          dragOriginRef={dragOriginRef}
          suppressClickRef={suppressClickRef}
          rangeNote={null}
        />
      )
      processed++
      continue
    }

    result.push(
      <RangeBlock
        key={`range-${processed}`}
        note={group.note}
        lines={lines}
        startIndex={group.startIndex}
        endIndex={group.endIndex}
        hunkAnchor={hunkAnchor}
        selectionPath={selectionPath}
        selection={selection}
        onSelectionChange={onSelectionChange}
        dragOriginRef={dragOriginRef}
        suppressClickRef={suppressClickRef}
      />
    )
    processed = group.endIndex + 1
  }

  return result
}

interface DiffLineProps {
  lineIndex: number
  line: FileDiff["hunks"][number]["lines"][number]
  hunkAnchor: string
  selectionPath: string
  selection?: DiffSelection | null
  onSelectionChange?: (selection: DiffSelection | null) => void
  dragOriginRef: React.MutableRefObject<{ filePath: string; hunkAnchor: string; lineIndex: number; moved: boolean } | null>
  suppressClickRef: React.MutableRefObject<boolean>
  rangeNote?: ReviewHunkNote | null
  inRange?: boolean
}

function DiffLine({ lineIndex, line, hunkAnchor, selectionPath, selection, onSelectionChange, dragOriginRef, suppressClickRef, rangeNote, inRange = false }: DiffLineProps) {
  const selected = selection?.file_path === selectionPath
    && selection.hunk_anchor === hunkAnchor
    && lineIndex >= selection.start && lineIndex <= selection.end

  const selectLine = () => {
    if (suppressClickRef.current) {
      suppressClickRef.current = false
      return
    }
    if (!onSelectionChange) return
    if (selection?.file_path === selectionPath && selection.hunk_anchor === hunkAnchor) {
      onSelectionChange({ ...selection, start: Math.min(selection.start, lineIndex), end: Math.max(selection.end, lineIndex) })
    } else {
      onSelectionChange({ file_path: selectionPath, hunk_anchor: hunkAnchor, start: lineIndex, end: lineIndex })
    }
  }
  const startDrag = (event: React.PointerEvent) => {
    if (event.button !== 0 || !onSelectionChange) return
    dragOriginRef.current = { filePath: selectionPath, hunkAnchor, lineIndex, moved: false }
    onSelectionChange({ file_path: selectionPath, hunk_anchor: hunkAnchor, start: lineIndex, end: lineIndex })
  }
  const extendDrag = () => {
    const origin = dragOriginRef.current
    if (!origin || origin.filePath !== selectionPath || origin.hunkAnchor !== hunkAnchor) return
    origin.moved ||= origin.lineIndex !== lineIndex
    onSelectionChange?.({
      file_path: selectionPath,
      hunk_anchor: hunkAnchor,
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
      data-diff-line
      data-selected={selected ? "true" : undefined}
      data-line-note-range={rangeNote?.id}
      onPointerEnter={extendDrag}
      className={cn(
        "grid grid-cols-[3rem_3rem_1fr] leading-5",
        !inRange && line.kind === "add" && "bg-emerald-950/40 text-emerald-200",
        !inRange && line.kind === "del" && "bg-red-950/40 text-red-200",
        inRange && !selected && line.kind === "add" && "range-tinted-add bg-emerald-950/[0.25] text-emerald-100",
        inRange && !selected && line.kind === "del" && "range-tinted-del bg-red-950/[0.25] text-red-100",
        inRange && !selected && line.kind === "ctx" && "bg-amber-950/20 text-amber-100",
        line.kind === "ctx" && !inRange && "text-zinc-400",
        selected && !inRange && "bg-indigo-900/60 text-indigo-100",
      )}
    >
      {gutter("old", line.old_no)}
      {gutter("new", line.new_no)}
      <span className="whitespace-pre px-2"><span aria-hidden>{line.kind === "add" ? "+" : line.kind === "del" ? "-" : " "}</span>{line.text}</span>
    </div>
  )
}

interface BaseBlockProps {
  lines: FileDiff["hunks"][number]["lines"]
  startIndex: number
  endIndex: number
  hunkAnchor: string
  selectionPath: string
  selection?: DiffSelection | null
  onSelectionChange?: (selection: DiffSelection | null) => void
  dragOriginRef: React.MutableRefObject<{ filePath: string; hunkAnchor: string; lineIndex: number; moved: boolean } | null>
  suppressClickRef: React.MutableRefObject<boolean>
}

type SelectionBlockProps = BaseBlockProps

function SelectionBlock({ lines, startIndex, endIndex, hunkAnchor, selectionPath, selection, onSelectionChange, dragOriginRef, suppressClickRef }: SelectionBlockProps) {
  return (
    <div data-diff-block="selection" className="border-x border-y border-indigo-600 bg-indigo-900/40">
      {lines.slice(startIndex, endIndex + 1).map((line, offset) => {
        const lineIndex = startIndex + offset
        return (
          <DiffLine
            key={lineIndex}
            lineIndex={lineIndex}
            line={line}
            hunkAnchor={hunkAnchor}
            selectionPath={selectionPath}
            selection={selection}
            onSelectionChange={onSelectionChange}
            dragOriginRef={dragOriginRef}
            suppressClickRef={suppressClickRef}
            inRange
          />
        )
      })}
    </div>
  )
}

interface RangeBlockProps extends BaseBlockProps {
  note: ReviewHunkNote
}

function RangeBlock({ note, lines, startIndex, endIndex, hunkAnchor, selectionPath, selection, onSelectionChange, dragOriginRef, suppressClickRef }: RangeBlockProps) {
  return (
    <div data-diff-block="range" className="border-x border-y border-amber-700/70 bg-amber-950/20">
      <div className="border-b border-amber-800 bg-amber-950/40 px-3 py-2 font-sans text-sm text-amber-100">
        <span className="mr-2 font-mono text-[10px] text-amber-400">Lines {note.line_start}-{note.line_end}</span>
        {note.body}
      </div>
      {lines.slice(startIndex, endIndex + 1).map((line, offset) => {
        const lineIndex = startIndex + offset
        return (
          <DiffLine
            key={lineIndex}
            lineIndex={lineIndex}
            line={line}
            hunkAnchor={hunkAnchor}
            selectionPath={selectionPath}
            selection={selection}
            onSelectionChange={onSelectionChange}
            dragOriginRef={dragOriginRef}
            suppressClickRef={suppressClickRef}
            rangeNote={note}
            inRange
          />
        )
      })}
    </div>
  )
}
