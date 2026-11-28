import { ChevronDown } from "lucide-react"
import { fieldLabel, focusRing } from "./styles"
import type { CloneChoice } from "./useCloneChoice"

// The clone choice that means a new clone.
const newClone = 0

const selectClass = `h-[46px] w-full min-w-0 appearance-none truncate rounded-[10px] border border-lw-line-strong bg-lw-ground pl-3 pr-9 text-base text-lw-text outline-none sm:h-9 sm:rounded-md sm:text-sm ${focusRing}`
const branchClass = "h-[46px] rounded-[10px] border border-lw-line-strong bg-lw-ground px-3 font-lw-mono text-base text-lw-text outline-none placeholder:text-lw-faint"

function Chevron() {
  return <ChevronDown aria-hidden="true" size={16} className="pointer-events-none absolute right-3 top-1/2 -translate-y-1/2 text-lw-faint" />
}

// Which repository, and which of its clones when there is more than a new
// one to pick from.
export function CloneSelects({ clone }: { clone: CloneChoice }) {
  const { repository, repositories, choices, choice, jobClone } = clone
  const picking = choices.length > 1 || Boolean(jobClone)
  return (
    <div className={`grid grid-cols-1 gap-3 ${picking ? "sm:grid-cols-2" : ""}`}>
      <label className="flex min-w-0 flex-col gap-1.5">
        <span className={fieldLabel}>Repository</span>
        <span className="relative flex">
          <select aria-label="Repository" value={repository?.name} onChange={(event) => clone.pickRepository(event.target.value)} className={selectClass}>
            {repositories.map((option) => <option key={option.name} value={option.name}>{option.name}</option>)}
          </select>
          <Chevron />
        </span>
      </label>
      {picking && (
        <label className="flex min-w-0 flex-col gap-1.5">
          <span className={fieldLabel}>Clone</span>
          <span className="relative flex">
            <select aria-label="Clone" value={choice?.id ?? newClone} onChange={(event) => clone.setJobCloneId(Number(event.target.value))} className={selectClass}>
              {choices.map((option) => <option key={option.id} value={option.id}>{option.label}</option>)}
            </select>
            <Chevron />
          </span>
        </label>
      )}
    </div>
  )
}

// The branch the agent works on, and for a new clone the one it starts
// from. A clone an agent works in is joined on its own branch.
export function BranchFields({ clone }: { clone: CloneChoice }) {
  const { jobClone } = clone
  return (
    <div className={`grid grid-cols-1 gap-3 ${jobClone ? "" : "sm:grid-cols-2"}`}>
      <label className="flex flex-col gap-1.5">
        <span className={fieldLabel}>Branch</span>
        <input
          type="text"
          aria-label="Branch"
          autoComplete="off"
          autoCapitalize="off"
          spellCheck={false}
          disabled={jobClone?.busy}
          value={jobClone?.busy ? "" : clone.branch}
          onChange={(event) => clone.setBranch(event.target.value)}
          placeholder={jobClone ? `stay on ${jobClone.branch || "its commit"}` : "legato/<task id>"}
          className={`${branchClass} disabled:opacity-60 sm:h-9 sm:rounded-md sm:text-[13px] ${focusRing}`}
        />
      </label>
      {!jobClone && (
        <label className="flex flex-col gap-1.5">
          <span className={fieldLabel}>Base branch</span>
          <input
            type="text"
            aria-label="Base branch"
            autoComplete="off"
            autoCapitalize="off"
            spellCheck={false}
            value={clone.baseBranch}
            onChange={(event) => clone.setBaseBranch(event.target.value)}
            placeholder="default branch"
            className={`${branchClass} sm:h-9 sm:rounded-md sm:text-[13px] ${focusRing}`}
          />
        </label>
      )}
    </div>
  )
}
