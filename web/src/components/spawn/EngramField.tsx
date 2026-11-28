import { ChevronDown } from "lucide-react"
import type { EngramChoice } from "../../lib/engrams"
import { engramOptionLabel } from "../../lib/spawnEngram"
import { fieldLabel, focusRing } from "./styles"
import type { EngramChoiceState } from "./useEngramChoice"

function plainOptions(choices: EngramChoice[]) {
  return choices.map((choice) => <option key={choice.name} value={choice.name}>{choice.name}</option>)
}

// No engram, then this place's engrams with their marks, then the rest; with
// none of this place, the rest ungrouped.
function EngramOptions({ engram }: { engram: EngramChoiceState }) {
  const { place, other } = engram.groups
  if (place.length === 0) return <>{plainOptions(other)}</>
  return (
    <>
      <optgroup label={engram.words.word}>
        {place.map((choice) => <option key={choice.name} value={choice.name}>{engramOptionLabel(choice)}</option>)}
      </optgroup>
      {other.length > 0 && <optgroup label="Other engrams">{plainOptions(other)}</optgroup>}
    </>
  )
}

// Binds the pick to the place and makes it the default, or with no engram
// clears the place's default.
function RememberEngram({ engram, label }: { engram: EngramChoiceState; label: string }) {
  const { noun } = engram.words
  return (
    <label className="flex cursor-pointer items-start gap-2.5 text-sm text-lw-text-2 sm:text-xs">
      <input
        type="checkbox"
        checked={engram.remember}
        onChange={(event) => engram.setRemember(event.target.checked)}
        className="mt-0.5 accent-lw-accent"
      />
      <span>
        <span className="text-lw-text">{engram.engram ? `Remember for this ${noun}` : `Clear this ${noun}'s default`}</span>
        {engram.engram && (
          <span className="block text-lw-faint">Bind {engram.engram} to {label} and make it the default.</span>
        )}
      </span>
    </label>
  )
}

// EngramField picks the engram the agent starts as.
export function EngramField({ engram }: { engram: EngramChoiceState }) {
  const place = engram.picker?.place
  return (
    <div className="flex flex-col gap-1.5">
      <label className="flex flex-col gap-1.5">
        <span className={fieldLabel}>Engram</span>
        <span className="relative flex">
          <select
            aria-label="Engram"
            value={engram.engram}
            onChange={(event) => engram.pick(event.target.value)}
            className={`h-[46px] w-full min-w-0 appearance-none truncate rounded-[10px] border border-lw-line-strong bg-lw-ground pl-3 pr-9 text-base text-lw-text outline-none sm:h-9 sm:rounded-md sm:text-sm ${focusRing}`}
          >
            <option value="">No engram</option>
            <EngramOptions engram={engram} />
          </select>
          <ChevronDown aria-hidden="true" size={16} className="pointer-events-none absolute right-3 top-1/2 -translate-y-1/2 text-lw-faint" />
        </span>
      </label>
      {engram.choice?.summary && <p className="text-xs text-lw-faint">{engram.choice.summary}</p>}
      {engram.canRemember && place && <RememberEngram engram={engram} label={place.label} />}
    </div>
  )
}
