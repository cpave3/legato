import { NameOptions } from "./NameOptions"
import { fieldLabel, focusRing } from "./styles"
import type { MacroChoice } from "./useMacroChoice"

// "Start with macro": the favourites grouped first when there are others,
// and the tier the macro chosen names.
export function MacroField({ macro }: { macro: MacroChoice }) {
  const { favorites, rest } = macro.macros
  return (
    <label className="flex flex-col gap-1.5">
      <span className={fieldLabel}>Start with macro <span className="text-lw-faint">· optional</span></span>
      <select aria-label="Start with macro" value={macro.macroName} onChange={(event) => macro.choose(event.target.value)}
        className={`min-h-11 rounded-[10px] border border-lw-line-strong bg-lw-ground px-3 text-base text-lw-text sm:min-h-9 sm:rounded-md sm:text-sm ${focusRing}`}>
        <option value="">None</option>
        {rest.length === 0
          ? <NameOptions items={favorites} />
          : <>
            <optgroup label="Favourites"><NameOptions items={favorites} /></optgroup>
            <optgroup label="Other macros"><NameOptions items={rest} /></optgroup>
          </>}
      </select>
      {macro.tier && <span className="text-xs text-lw-text-2">Model tier: {macro.tier}</span>}
    </label>
  )
}
