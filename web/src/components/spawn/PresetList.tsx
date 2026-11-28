import { presetsHiddenOnPhone, presetsShownOnPhone } from "../../lib/spawnSuggestions"
import type { DirectoryField } from "./useDirectoryField"

function PresetButton({ preset, pressed, waits, onPick }: { preset: DirectoryField["presets"][number]; pressed: boolean; waits: boolean; onPick: () => void }) {
  return (
    <button
      type="button"
      aria-pressed={pressed}
      onClick={onPick}
      className={`${waits ? "hidden sm:flex" : "flex"} min-h-[54px] w-full min-w-0 items-center gap-3 border-b border-lw-line px-3.5 text-left transition-colors max-sm:last:border-b-0 sm:min-h-0 sm:rounded-lg sm:border sm:px-2.5 sm:py-2 ${
        pressed ? "bg-lw-selected sm:border-lw-accent" : "sm:border-lw-line-strong sm:bg-lw-ground sm:hover:border-lw-faint"
      }`}
    >
      <span aria-hidden="true" className={`h-[18px] w-[18px] shrink-0 rounded-full sm:hidden ${pressed ? "border-[5px] border-lw-accent" : "border-[1.5px] border-lw-faint"}`} />
      <span className="flex min-w-0 flex-col gap-px">
        <span className="truncate text-[15px] font-medium sm:text-[13px]">{preset.name}</span>
        <span className="truncate font-lw-mono text-[11px] text-lw-faint">{preset.display}</span>
      </span>
    </button>
  )
}

// The folders kept as presets: a list on a phone, where those past the
// third wait behind a row with the custom path, and a grid on a wide screen.
export function PresetList({ dir }: { dir: DirectoryField }) {
  const { presets, selected, showAll } = dir
  const hiddenOnPhone = presetsHiddenOnPhone(presets, selected, showAll)
  return (
    <div className="overflow-hidden rounded-[10px] border border-lw-line-strong bg-lw-ground sm:grid sm:grid-cols-2 sm:gap-1.5 sm:overflow-visible sm:rounded-none sm:border-0 sm:bg-transparent">
      {presets.map((preset, index) => {
        const pressed = preset === selected
        return (
          <PresetButton
            key={preset.path}
            preset={preset}
            pressed={pressed}
            waits={!showAll && index >= presetsShownOnPhone && !pressed}
            onPick={() => { dir.setPath(preset.display); dir.closeList() }}
          />
        )
      })}
      {!showAll && (
        <button
          type="button"
          aria-expanded="false"
          onClick={() => dir.setShowAll(true)}
          className="flex min-h-[50px] w-full items-center justify-between px-3.5 text-left text-sm text-lw-text-2 sm:hidden"
        >
          <span>{hiddenOnPhone > 0 ? `${hiddenOnPhone} more · Custom path` : "Custom path"}</span>
          <span aria-hidden="true">›</span>
        </button>
      )}
    </div>
  )
}
