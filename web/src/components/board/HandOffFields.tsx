import { Link } from "react-router-dom"

import { Button } from "../ui/Button"
import { DialogFooter } from "../ui/Dialog"
import { agentKindLabel } from "../../lib/agentKind"
import type { ReviewOverride, ReviewPassesAdvert } from "../../lib/away"
import { repositoryOptionLabel, reviewFieldValues } from "../../lib/handoffForm"
import { advertCanClone, handoffBlock, type Worker, type WorkerAdvert } from "../../lib/workers"
import type { RepositoryPreset } from "../RepositoryPresetSelect"

// The Hand off form's fields and notices, one section each. See
// HandOffModal.tsx.

export const fieldLabel = "text-xs text-lw-text-2"
// 16px on a phone: a smaller field makes iOS zoom the page when it is focused.
export const control = "min-h-11 w-full rounded-md border border-lw-line-strong bg-lw-ground px-3 text-base text-lw-text outline-none focus:border-lw-accent sm:min-h-10 sm:text-sm"

export function Choices<T extends string>({ name, legend, value, options, onChange }: {
  name: string
  legend: string
  value: T
  options: { value: T; label: string; note: string }[]
  onChange: (value: T) => void
}) {
  return (
    <fieldset className="flex flex-col gap-1.5">
      <legend className={`${fieldLabel} mb-1.5`}>{legend}</legend>
      <div className="flex flex-col overflow-hidden rounded-md border border-lw-line-strong bg-lw-ground sm:flex-row">
        {options.map((option) => (
          <label key={option.value} className={`flex min-h-11 flex-1 basis-0 cursor-pointer items-start gap-2.5 border-b border-lw-line px-3 py-2.5 last:border-b-0 sm:border-b-0 sm:border-r sm:last:border-r-0 ${value === option.value ? "bg-lw-selected" : ""}`}>
            <input type="radio" name={name} value={option.value} checked={value === option.value} onChange={() => onChange(option.value)} className="mt-1 accent-lw-accent" />
            <span className="flex min-w-0 flex-col gap-px">
              <span className="text-sm font-medium text-lw-text">{option.label}</span>
              <span className="break-words font-lw-mono text-xs text-lw-text-2">{option.note}</span>
            </span>
          </label>
        ))}
      </div>
    </fieldset>
  )
}

// The notices above the fields: an agent running here, or no worker paired.
export function HandOffBlockers({ agentHere, workers }: { agentHere: boolean; workers: Worker[] | null }) {
  return (
    <>
      {agentHere && (
        <p className="rounded-md border border-lw-needs/40 bg-lw-needs-soft px-3 py-2 text-xs text-lw-needs">
          Stop this task's agent before handing it off: two agents never work on one task.
        </p>
      )}
      {workers && workers.length === 0 && (
        <p className="text-sm text-lw-text-2">
          No worker is paired. <Link to="/settings#workers" className="text-lw-link hover:underline">Add a worker</Link> in Settings › Connections › Workers first.
        </p>
      )}
    </>
  )
}

function workerOptionLabel(w: Worker): string {
  const block = handoffBlock(w)
  return block === null ? w.name : `${w.name} · ${block}`
}

export function WorkerField({ worker, workers, onChange }: { worker: string; workers: Worker[] | null; onChange: (worker: string) => void }) {
  return (
    <label className="flex flex-col gap-1.5">
      <span className={fieldLabel}>Worker</span>
      <select aria-label="Worker" value={worker} onChange={(event) => onChange(event.target.value)} className={control}>
        {worker === "" && <option value="">{workers ? "No worker can take it" : "Reading the workers…"}</option>}
        {(workers ?? []).map((w) => (
          <option key={w.instance_id} value={w.instance_id} disabled={handoffBlock(w) !== null}>
            {workerOptionLabel(w)}
          </option>
        ))}
      </select>
    </label>
  )
}

export function BranchField({ branch, onChange }: { branch: string; onChange: (branch: string) => void }) {
  return (
    <label className="flex flex-col gap-1.5">
      <span className={fieldLabel}>Branch</span>
      <input aria-label="Branch" value={branch} onChange={(event) => onChange(event.target.value)} placeholder="the branch's name on GitHub" autoComplete="off" spellCheck={false} className={`${control} font-lw-mono`} />
      <span className="text-xs text-lw-text-2">The agent reads its commits first, then only adds commits to it and reuses its PR. A branch not on GitHub fails the handoff.</span>
    </label>
  )
}

// The repositories offered, as a list when Legato offers some (those the
// worker can't clone disabled, and those only it has after them), else a
// field for an owner/name.
export function RepositoryField({ repo, offered, workerOnly, advert, workerName, onChange }: {
  repo: string
  offered: RepositoryPreset[]
  workerOnly: string[]
  advert: WorkerAdvert | undefined
  workerName: string
  onChange: (repo: string) => void
}) {
  return (
    <label className="flex flex-col gap-1.5">
      <span className={fieldLabel}>Repository</span>
      {offered.length > 0 ? (
        <select aria-label="Repository" required value={repo} onChange={(event) => onChange(event.target.value)} className={control}>
          <option value="" disabled>Choose a repository…</option>
          {offered.map((repository) => {
            const label = repositoryOptionLabel(repository.name, repository.repo)
            const clonable = advertCanClone(advert, repository.repo)
            return <option key={repository.repo} value={repository.repo} disabled={!clonable}>{clonable ? label : `${label} · ${workerName} can't clone it`}</option>
          })}
          {workerOnly.map((r) => <option key={r} value={r}>{`${r} · on ${workerName} only`}</option>)}
        </select>
      ) : (
        <input aria-label="Repository" required value={repo} onChange={(event) => onChange(event.target.value)} placeholder="owner/name on GitHub" className={`${control} font-lw-mono`} />
      )}
    </label>
  )
}

export function ToolField({ tool, tools, onChoose }: { tool: string; tools: string[]; onChoose: (tool: string) => void }) {
  return (
    <label className="flex flex-col gap-1.5">
      <span className={fieldLabel}>Agent tool</span>
      <select aria-label="Agent tool" value={tool} onChange={(event) => onChoose(event.target.value)} className={control}>
        {tools.length === 0 && <option value="">The worker's default</option>}
        {tools.map((kind) => <option key={kind} value={kind}>{agentKindLabel(kind)}</option>)}
      </select>
    </label>
  )
}

// The warnings below the fields: a repository the worker can't clone, and
// the skills and MCP connections it lacks.
export function WorkerWarnings({ repoBlocked, repo, lacks, workerName }: { repoBlocked: boolean; repo: string; lacks: string[]; workerName: string }) {
  return (
    <>
      {repoBlocked && (
        <p className="text-xs text-lw-needs">{workerName} can't clone {repo.trim()}: its GitHub credentials do not list it.</p>
      )}
      {lacks.length > 0 && (
        <p className="rounded-md border border-lw-needs/40 bg-lw-needs-soft px-3 py-2 text-xs text-lw-needs">
          Not on {workerName}: {lacks.join(", ")}. Its agent works without them.
        </p>
      )}
    </>
  )
}

// What the agent may do there, following the purpose.
export function HandOffNote({ workerName, makesChange }: { workerName: string; makesChange: boolean }) {
  return (
    <p className="text-xs text-lw-text-2">
      The agent works on its own in {workerName}'s sandbox
      {makesChange ? " and can push its branches and open draft PRs" : " and pushes nothing"}.
      This card is read-only here until it comes back.
    </p>
  )
}

export function HandOffFooter({ sending, canSend, onClose }: { sending: boolean; canSend: boolean; onClose: () => void }) {
  return (
    <DialogFooter className="shrink-0 pb-[max(0.75rem,env(safe-area-inset-bottom))]">
      <Button type="button" variant="outline" disabled={sending} onClick={onClose}>Cancel</Button>
      <Button type="submit" disabled={!canSend}>{sending ? "Handing off…" : "Hand off"}</Button>
    </DialogFooter>
  )
}

export function HandOffError({ error }: { error: string | null }) {
  if (!error) return null
  return <p role="alert" className="rounded-md border border-lw-danger-line bg-lw-error-soft px-3 py-2 text-xs text-lw-danger">{error}</p>
}

// ReviewPassesSection changes a worker's review passes for one handoff:
// on or off, the reviewer's tool and its model or tier, the skill, and the
// passes allowed. Each starts at the worker's own setting.
export function ReviewPassesSection({ advert, review, onChange }: { advert: ReviewPassesAdvert; review: ReviewOverride; onChange: (r: ReviewOverride) => void }) {
  const values = reviewFieldValues(advert, review)
  const set = (change: ReviewOverride) => onChange({ ...review, ...change })
  return (
    <fieldset aria-label="Review passes" className="flex flex-col gap-2 rounded-md border border-lw-line px-3 py-2.5">
      <legend className={fieldLabel}>Review passes</legend>
      <label className="flex items-center gap-2 text-sm text-lw-text">
        <input type="checkbox" aria-label="Review this handoff's pull requests" checked={values.enabled} onChange={(e) => set({ enabled: e.target.checked })} />
        A reviewer on another model reviews its pull requests before it is done
      </label>
      {values.enabled && <ReviewerFields advert={advert} values={values} set={set} />}
    </fieldset>
  )
}

function ReviewerFields({ advert, values, set }: { advert: ReviewPassesAdvert; values: ReturnType<typeof reviewFieldValues>; set: (change: ReviewOverride) => void }) {
  return (
    <div className="grid grid-cols-2 gap-2">
      <label className="flex flex-col gap-1">
        <span className="text-xs text-lw-text-2">Reviewer</span>
        <select aria-label="Reviewer" value={values.agent} onChange={(e) => set({ agent: e.target.value, tier: undefined, model: undefined })} className={control}>
          {advert.adapters.map((a) => <option key={a.name} value={a.name}>{agentKindLabel(a.name)}</option>)}
        </select>
      </label>
      {values.tiers.length > 0 ? (
        <label className="flex flex-col gap-1">
          <span className="text-xs text-lw-text-2">Tier</span>
          <select aria-label="Reviewer tier" value={values.tier} onChange={(e) => set({ tier: e.target.value || undefined })} className={control}>
            <option value="">Model below</option>
            {values.tiers.map((t) => <option key={t} value={t}>{t}</option>)}
          </select>
        </label>
      ) : <span />}
      <label className="flex flex-col gap-1">
        <span className="text-xs text-lw-text-2">Model</span>
        <input aria-label="Reviewer model" value={values.model} onChange={(e) => set({ model: e.target.value || undefined })} placeholder="the tool's" className={control} />
      </label>
      <label className="flex flex-col gap-1">
        <span className="text-xs text-lw-text-2">Skill</span>
        <select aria-label="Review skill" value={values.skill} onChange={(e) => set({ skill: e.target.value || undefined })} className={control}>
          {!advert.defaults.skill && <option value="">None</option>}
          {advert.skills.map((s) => <option key={s} value={s}>{s}</option>)}
        </select>
      </label>
      <label className="flex flex-col gap-1">
        <span className="text-xs text-lw-text-2">Passes at most</span>
        <input aria-label="Passes at most" type="number" min={1} value={values.maxPasses} onChange={(e) => set({ max_passes: Number(e.target.value) || undefined })} className={control} />
      </label>
    </div>
  )
}
