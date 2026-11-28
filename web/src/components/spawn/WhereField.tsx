import { CloneFields } from "./CloneFields"
import { PathField } from "./PathField"
import { PresetList } from "./PresetList"
import { fieldLabel } from "./styles"
import type { CloneChoice } from "./useCloneChoice"
import type { DirectoryField } from "./useDirectoryField"

const places = [
  { value: "folder", label: "Folder" },
  { value: "clone", label: "Clone" },
] as const

// Folder or clone, offered when there is a repository to clone.
function PlaceToggle({ clone, dir }: { clone: CloneChoice; dir: DirectoryField }) {
  return (
    <div role="radiogroup" aria-label="Where" className="flex rounded-[10px] border border-lw-line-strong bg-lw-ground p-[3px] sm:rounded-md sm:p-0.5">
      {places.map((option) => {
        const checked = option.value === clone.whereKind
        return (
          <button
            key={option.value}
            type="button"
            role="radio"
            aria-checked={checked}
            onClick={() => { clone.setWhereKind(option.value); dir.closeList() }}
            className={`h-10 min-w-0 flex-1 truncate rounded-lg px-2 text-sm transition-colors sm:h-[30px] sm:rounded sm:text-[13px] ${
              checked ? "bg-lw-selected font-medium text-lw-text" : "text-lw-text-2 hover:text-lw-text"
            }`}
          >
            {option.label}
          </button>
        )
      })}
    </div>
  )
}

interface WhereFieldProps {
  dir: DirectoryField
  clone: CloneChoice
  listId: string
  // The path field shows on a phone too: a custom path was asked for, or
  // there are no presets.
  customPathShown: boolean
  // Changes whenever something above the path field may have moved it.
  layoutKey: string
}

// WhereField is where the agent starts: a clone's repository, clone and
// branches, or a folder's presets and typed path.
export function WhereField({ dir, clone, listId, customPathShown, layoutKey }: WhereFieldProps) {
  const { inClone, repository } = clone
  return (
    <div className="flex flex-col gap-1.5">
      <span className={fieldLabel}>Where</span>
      {clone.repositories.length > 0 && <PlaceToggle clone={clone} dir={dir} />}
      {inClone && repository && <CloneFields clone={clone} repo={repository.repo} />}
      {!inClone && dir.presets.length > 0 && <PresetList dir={dir} />}

      <PathField dir={dir} listId={listId} visibility={inClone ? "hidden" : customPathShown ? "block" : "hidden sm:block"} layoutKey={layoutKey} />
    </div>
  )
}
