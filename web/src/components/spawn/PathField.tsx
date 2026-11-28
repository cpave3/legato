import { useRef, type RefObject } from "react"
import { suggestionParts, type DirectorySuggestion } from "../../lib/spawnDirectories"
import { listFrameStyle, suggestionMarks, type ListPlace } from "../../lib/spawnSuggestions"
import { useSuggestionListPlace, type DirectoryField } from "./useDirectoryField"

function SuggestionOption({ item, index, dir, listId }: { item: DirectorySuggestion; index: number; dir: DirectoryField; listId: string }) {
  const { view, current } = dir
  const parts = suggestionParts(view.base, view.prefix, item)
  const marks = suggestionMarks(item)
  return (
    <div
      id={`${listId}-${index}`}
      role="option"
      aria-selected={index === current}
      aria-disabled={view.held}
      onMouseDown={() => { if (!view.held) dir.take(item) }}
      className={`flex justify-between gap-3 rounded px-2 py-1.5 ${view.held ? "" : "cursor-pointer"} ${index === current ? "bg-lw-selected" : view.held ? "" : "hover:bg-lw-selected/60"}`}
    >
      <span className="min-w-0 truncate">
        <span className="text-lw-faint">{parts.base}</span>
        <span className="text-lw-link">{parts.typed}</span>
        {parts.rest}
      </span>
      <span className={`shrink-0 font-lw ${marks ? "text-lw-live" : "text-lw-faint"}`}>
        {marks || "folder"}
      </span>
    </div>
  )
}

interface SuggestionListProps {
  dir: DirectoryField
  listId: string
  listRef: RefObject<HTMLDivElement | null>
  place: ListPlace | null
  listHeight: number
}

// The frame is there for as long as the field has the focus, at no height
// while there is nothing to list, so that every change of what is listed is
// a change of height it can ease.
function SuggestionList({ dir, listId, listRef, place, listHeight }: SuggestionListProps) {
  const { view, found } = dir
  const listing = view.shown.length > 0
  return (
    <div
      // mousedown here would take the focus from the field, and the list
      // with it.
      onMouseDown={(event) => event.preventDefault()}
      style={listFrameStyle(place, listing, listHeight)}
      className={`z-10 overflow-hidden rounded-lg border bg-lw-raised font-lw-mono text-xs shadow-[0_12px_30px_rgba(0,0,0,.45)] transition-[height,opacity] duration-150 ease-out motion-reduce:transition-none ${
        listing ? "border-lw-line-strong opacity-100" : "pointer-events-none border-transparent opacity-0"
      }`}
    >
      <div ref={listRef} style={{ maxHeight: place?.room }} className="overflow-y-auto p-1">
        {listing && (
          <div
            id={listId}
            role="listbox"
            aria-label="Path suggestions"
            aria-busy={view.held}
            className={`transition-opacity duration-150 motion-reduce:transition-none ${view.held ? "opacity-40" : "opacity-100"}`}
          >
            {view.shown.map((item, index) => <SuggestionOption key={item.path} item={item} index={index} dir={dir} listId={listId} />)}
          </div>
        )}
        {view.more && found && (
          <p className="px-2 py-1 font-lw text-[11px] text-lw-faint">
            {view.shown.length} of {found.total} folders · keep typing to narrow them
          </p>
        )}
      </div>
    </div>
  )
}

interface PathFieldProps {
  dir: DirectoryField
  listId: string
  // The wrapper's display: hidden in a clone, and on a phone until a custom
  // path is asked for.
  visibility: string
  // Changes whenever something above the field may have moved it.
  layoutKey: string
}

// The typed path, with what tab would add to it drawn after it, and the
// directories the server suggests under it.
export function PathField({ dir, listId, visibility, layoutKey }: PathFieldProps) {
  const fieldRef = useRef<HTMLDivElement>(null)
  const listRef = useRef<HTMLDivElement>(null)
  const { place, listHeight } = useSuggestionListPlace(fieldRef, listRef, dir, `${dir.showAll}|${dir.presets.length}|${layoutKey}`, listId)
  const { ghost, path, current } = dir
  return (
    <div className={`${visibility} sm:mt-0.5`}>
      <div
        ref={fieldRef}
        className="flex h-[46px] items-center gap-2 rounded-[10px] border border-lw-line-strong bg-lw-ground px-3 font-lw-mono text-base transition-shadow focus-within:border-lw-accent focus-within:ring-[3px] focus-within:ring-lw-accent/25 sm:h-[38px] sm:rounded-md sm:text-[13px]"
      >
        <span aria-hidden="true" className="text-lw-faint">path</span>
        <div className="relative min-w-0 flex-1">
          {ghost && (
            <div aria-hidden="true" className="pointer-events-none absolute inset-0 flex items-center overflow-hidden whitespace-pre">
              <span className="invisible">{path}</span>
              <span className="text-lw-faint">{ghost}</span>
            </div>
          )}
          <input
            type="text"
            role="combobox"
            aria-label="Path"
            aria-autocomplete="list"
            aria-expanded={dir.view.shown.length > 0}
            aria-controls={listId}
            aria-activedescendant={current >= 0 ? `${listId}-${current}` : undefined}
            autoComplete="off"
            autoCapitalize="off"
            spellCheck={false}
            value={path}
            onChange={(event) => dir.edit(event.target.value)}
            onFocus={dir.openList}
            onBlur={dir.closeList}
            onKeyDown={dir.onKeyDown}
            placeholder="~/path/to/project"
            className="relative w-full bg-transparent text-lw-text outline-none placeholder:text-lw-faint"
          />
        </div>
        {ghost && (
          <kbd className="hidden rounded border border-lw-line-strong px-1.5 font-lw text-[11px] text-lw-faint sm:block">tab</kbd>
        )}
      </div>
      {dir.suggests && <SuggestionList dir={dir} listId={listId} listRef={listRef} place={place} listHeight={listHeight} />}
    </div>
  )
}
