import { useState, type ReactNode } from "react"
import { Link, useParams } from "react-router-dom"
import { ChevronDown, ChevronRight, Download, FileText } from "lucide-react"
import { useWorkflowRun } from "../hooks/useWorkflow"
import { useServer } from "../hooks/useServer"
import { useNow } from "../hooks/useNow"
import { cancelWorkflow, retryWorkflowStep, triggerWorkflowStep, promptWorkflowStep, fetchWorkflowArtifact, type WorkflowArtifact, type WorkflowAttempt, type WorkflowRunView, type WorkflowStepRun } from "../lib/workflow"
import { ago, attentionCopy, currentStep, elapsed, inputText, runWithAttention, producedValues, runState, runSubject, secretInputNames, sourceLabel, stepIntent, stepMark, stepPosition, stepSegments, type StepLike, type Tone } from "../lib/workflowView"
import { WorkflowMarkdownPreview } from "../components/WorkflowMarkdownPreview"
import { ManualWorkflowStepAction } from "../components/ManualWorkflowStepAction"
import { workflowArtifactFilename } from "../lib/workflow-artifact"
import { WorkflowStepRetryAction } from "../components/WorkflowStepRetryAction"
import { StatePill, StepBar } from "../components/WorkflowRunStatus"
import { toneGlyph, toneText } from "../lib/workflowTones"
import { Button } from "../components/ui/Button"
import { LoadingState } from "../components/ui/LoadingState"
import { cn } from "../lib/utils"

function parseJSON(raw: string | undefined): unknown {
  try { return JSON.parse(raw ?? "") } catch { return raw }
}

function stepTrigger(step: WorkflowStepRun): string | undefined {
  const definition = parseJSON(step.definition_json)
  return definition && typeof definition === "object" && "trigger" in definition ? String((definition as { trigger: unknown }).trigger) : undefined
}

function asStepLike(step: WorkflowStepRun): StepLike {
  return { step_id: step.step_id, title: step.title, status: step.status, trigger: stepTrigger(step) }
}

function definitionTitle(view: WorkflowRunView): string {
  const definition = parseJSON(view.run.definition_json)
  return definition && typeof definition === "object" && "title" in definition ? String((definition as { title: unknown }).title) : ""
}

function agentLink(taskId: string, runId: string) {
  return `/agents?task=${encodeURIComponent(taskId)}&workflow=${encodeURIComponent(runId)}`
}

// The values of a run's secret inputs, which nothing on the page shows, even
// where a step's inputs or outputs repeat them.
function secretValues(view: WorkflowRunView): string[] {
  const names = secretInputNames(view.run.definition_json)
  const inputs = parseJSON(view.run.inputs_json)
  if (!names.size || !inputs || typeof inputs !== "object") return []
  return [...names].map(name => (inputs as Record<string, unknown>)[name]).filter((value): value is string => typeof value === "string" && value !== "")
}

function redact(text: string, secrets: string[]): string {
  return secrets.reduce((current, secret) => current.split(secret).join("••••••"), text)
}

function formatValue(value: unknown, secrets: string[]): string {
  return redact(inputText(value), secrets)
}

function ValueList({ values, secrets, className }: { values: [string, unknown][]; secrets: string[]; className?: string }) {
  return <dl className={cn("grid grid-cols-[max-content_minmax(0,1fr)] gap-x-4 gap-y-1.5 text-[13px]", className)}>
    {values.map(([key, value]) => <div key={key} className="contents">
      <dt className="pt-px font-lw-mono text-xs text-lw-faint">{key.replaceAll("_", " ")}</dt>
      <dd className={cn("min-w-0 whitespace-pre-wrap break-words", inputText(value).startsWith("{") && "max-h-48 overflow-auto font-lw-mono text-xs")}>{formatValue(value, secrets)}</dd>
    </div>)}
  </dl>
}

const panelLook: Record<Tone, string> = {
  live: "border-lw-live/30 bg-lw-live-soft",
  needs: "border-lw-needs/30 bg-lw-needs-soft",
  done: "border-lw-done/30 bg-lw-done-soft",
  error: "border-lw-error/30 bg-lw-error-soft",
  idle: "border-lw-line bg-lw-surface",
}

function NowPanel({ tone, heading, children, actions }: { tone: Tone; heading: ReactNode; children?: ReactNode; actions?: ReactNode }) {
  return <section aria-label="Now" className={cn("mt-5 rounded-xl border p-4", panelLook[tone])}>
    <h2 className="flex items-center gap-2 text-[15px] font-semibold"><span aria-hidden="true" className={toneText[tone]}>{toneGlyph[tone]}</span>{heading}</h2>
    {children && <div className="mt-1 text-[13px] text-lw-text-2">{children}</div>}
    {actions && <div className="mt-3 flex flex-wrap items-center gap-2">{actions}</div>}
  </section>
}

function Now({ view, runId, refresh, now, secrets }: { view: WorkflowRunView; runId: string; refresh: () => Promise<void>; now: number; secrets: string[] }) {
  const { baseUrl } = useServer()
  const steps = view.steps ?? []
  const attempts = view.attempts ?? []
  const artifacts = view.artifacts ?? []
  const run = runWithAttention(view.run, attempts)
  const state = runState(run)
  if (run.status === "succeeded") {
    const values = producedValues(steps)
    if (artifacts.length === 0 && values.length === 0) return null
    return <NowPanel tone="done" heading="Produced">
      <div className="mt-2 grid gap-2 text-lw-text">
        {artifacts.map(artifact => <Artifact key={artifact.id} artifact={artifact} step={steps.find(step => step.step_id === artifact.step_id)?.title} />)}
        {values.length > 0 && <ValueList values={values.map(value => [value.key, value.value])} secrets={secrets} className="mt-1" />}
      </div>
    </NowPanel>
  }
  if (run.status === "failed") {
    const failed = steps.find(step => step.status === "failed")
    return <NowPanel tone="error" heading={failed ? `${failed.title} failed` : "The run failed"}
      actions={failed && <WorkflowStepRetryAction step={failed} onRetry={async force => { await retryWorkflowStep(baseUrl, runId, failed.step_id, force); await refresh() }} />}>
      {(failed?.error || run.error) && <p className="mt-1 whitespace-pre-wrap break-words rounded-lg bg-black/25 px-2.5 py-2 font-lw-mono text-xs text-lw-danger">{redact(failed?.error || run.error || "", secrets)}</p>}
    </NowPanel>
  }
  if (run.status === "cancelled") return null
  const current = currentStep(steps)
  if (!current) return null
  const attempt = [...attempts].reverse().find(item => item.step_id === current.step_id && item.status === "waiting_agent")
  if (attempt) {
    const open = attempt.agent_session_id ? <Button asChild variant="primary"><Link to={agentLink(attempt.task_id ?? "", runId)}>Open agent</Link></Button> : null
    if (attempt.needs_attention) {
      const copy = attentionCopy(attempt.attention_reason ?? "", current.title)
      return <NowPanel tone="needs" heading={copy.heading}
        actions={<>{open}<Button variant="outline" onClick={async () => { await promptWorkflowStep(baseUrl, runId, current.step_id); await refresh() }}>Prompt to continue</Button></>}>
        <p>{copy.sentence}</p>
      </NowPanel>
    }
    return <NowPanel tone={state.tone} heading={current.title} actions={open}>
      <p>{attempt.agent_kind || "The agent"} is working · {elapsed(attempt.started_at, now)}{attempt.working_dir && <> · in <span className="break-all font-lw-mono text-xs">{attempt.working_dir}</span></>}</p>
    </NowPanel>
  }
  if (current.status === "waiting_manual") {
    if (stepTrigger(current) === "agent_ended" && !run.needs_attention) {
      return <NowPanel tone="live" heading={`${current.title} starts when the agent ends`}
        actions={<>
          {run.task_id && <Button asChild variant="primary"><Link to={agentLink(run.task_id, runId)}>Open agent</Link></Button>}
          <ManualWorkflowStepAction step={current} label="End now" onTrigger={async force => { await triggerWorkflowStep(baseUrl, runId, current.step_id, force); await refresh() }} />
        </>} />
    }
    return <NowPanel tone="needs" heading={`${current.title} waits for you`}
      actions={<ManualWorkflowStepAction step={current} onTrigger={async force => { await triggerWorkflowStep(baseUrl, runId, current.step_id, force); await refresh() }} />}>
      {stepIntent(current) && <p>{stepIntent(current)}</p>}
    </NowPanel>
  }
  return <NowPanel tone={state.tone} heading={`Running ${current.title}`} />
}

function stepDuration(step: WorkflowStepRun, now: number): string {
  if (!step.started_at) return ""
  return elapsed(step.started_at, step.completed_at ?? now)
}

function objectEntries(raw: string): [string, unknown][] {
  const value = parseJSON(raw)
  return value && typeof value === "object" && !Array.isArray(value) ? Object.entries(value as Record<string, unknown>) : []
}

function StepRow({ step, run, attempts, artifacts, now, secrets }: { step: WorkflowStepRun; run: WorkflowRunView["run"]; attempts: WorkflowAttempt[]; artifacts: WorkflowArtifact[]; now: number; secrets: string[] }) {
  const [open, setOpen] = useState(step.status === "failed")
  const mark = stepMark(asStepLike(step), run)
  const inputs = objectEntries(step.resolved_inputs_json)
  const outputs = objectEntries(step.outputs_json).filter(([key]) => !artifacts.some(artifact => artifact.output_key === key))
  const agentAttempts = attempts.filter(attempt => attempt.agent_kind || attempt.working_dir)
  return <li className="border-t border-lw-line first:border-t-0">
    <button aria-expanded={open} onClick={() => setOpen(!open)} className="grid min-h-11 w-full grid-cols-[1.25rem_minmax(0,1fr)_auto_0.75rem] items-center gap-3 px-3.5 py-2.5 text-left hover:bg-lw-raised sm:grid-cols-[1.25rem_minmax(0,1fr)_auto_auto_0.75rem]">
      <span aria-hidden="true" className={cn("text-center text-[13px]", mark.tone ? toneText[mark.tone] : "text-lw-faint")}>{mark.glyph}</span>
      <span className={cn("min-w-0 font-medium", step.status === "pending" && "text-lw-text-2")}>{step.title}</span>
      <span className="hidden font-lw-mono text-xs text-lw-faint sm:inline">{step.executor_type}</span>
      <span className="min-w-12 text-right font-lw-mono text-xs text-lw-faint">{stepDuration(step, now)}</span>
      <span aria-hidden="true" className="text-lw-faint">{open ? <ChevronDown size={12} /> : <ChevronRight size={12} />}</span>
    </button>
    {open && <div className="space-y-3 px-3.5 pb-3.5 pl-11 text-[13px]">
      <p className="font-lw-mono text-xs text-lw-faint sm:hidden">{step.executor_type}</p>
      {step.error && step.status !== "failed" && <p className="text-lw-error">{redact(step.error, secrets)}</p>}
      {inputs.length > 0 && <div><h3 className="mb-1 text-xs text-lw-text-2">Inputs</h3><ValueList values={inputs} secrets={secrets} /></div>}
      {outputs.length > 0 && <div><h3 className="mb-1 text-xs text-lw-text-2">Outputs</h3><ValueList values={outputs} secrets={secrets} /></div>}
      {artifacts.length > 0 && run.status !== "succeeded" && <div className="grid gap-2">{artifacts.map(artifact => <Artifact key={artifact.id} artifact={artifact} />)}</div>}
      {agentAttempts.length > 0 && <div><h3 className="mb-1 text-xs text-lw-text-2">{agentAttempts.length === 1 ? "Agent" : "Attempts"}</h3>
        <ul className="space-y-1">{agentAttempts.map(attempt => <li key={attempt.id} className="text-lw-text-2">
          {agentAttempts.length > 1 && <span className="text-lw-text">Attempt {attempt.attempt_number} · </span>}
          {attempt.agent_kind || attempt.executor_type}{attempt.tier && ` (${attempt.tier})`} · {attempt.status.replaceAll("_", " ")}
          {attempt.working_dir && <span className="block break-all font-lw-mono text-xs text-lw-faint">{attempt.working_dir}</span>}
          {attempt.error && <span className="block text-lw-error">{redact(attempt.error, secrets)}</span>}
        </li>)}</ul></div>}
      {inputs.length === 0 && outputs.length === 0 && artifacts.length === 0 && agentAttempts.length === 0 && !step.error && <p className="text-lw-faint">No inputs or outputs</p>}
    </div>}
  </li>
}

function formatBytes(size: number): string {
  if (size < 1024) return `${size} B`
  if (size < 1024 * 1024) return `${(size / 1024).toFixed(1)} KB`
  return `${(size / 1024 / 1024).toFixed(1)} MB`
}

function Artifact({ artifact, step }: { artifact: WorkflowArtifact; step?: string }) {
  const { baseUrl } = useServer()
  const [text, setText] = useState("")
  const [error, setError] = useState("")
  const filename = workflowArtifactFilename(artifact.original_path, artifact.output_key)
  async function load() { try { const blob = await fetchWorkflowArtifact(baseUrl, artifact.id); setText(await blob.text()) } catch (c) { setError(c instanceof Error ? c.message : String(c)) } }
  async function download() { try { const blob = await fetchWorkflowArtifact(baseUrl, artifact.id); const url = URL.createObjectURL(blob); const link = document.createElement("a"); link.href = url; link.download = filename; link.click(); URL.revokeObjectURL(url) } catch (c) { setError(c instanceof Error ? c.message : String(c)) } }
  const markdown = artifact.media_type === "text/markdown"
  return <div className="rounded-lg border border-lw-line bg-lw-surface">
    <div className="flex flex-wrap items-center gap-3 px-3 py-2.5">
      <FileText size={16} aria-hidden="true" className="shrink-0 text-lw-text-2" />
      <div className="min-w-0 flex-1 basis-40"><p className="break-all font-medium">{filename}</p><p className="text-xs text-lw-faint">{step && `from ${step} · `}{markdown ? "Markdown" : artifact.media_type} · {formatBytes(artifact.size_bytes)}</p></div>
      <Button variant="outline" size="sm" onClick={download}><Download size={14} />Download</Button>
    </div>
    {markdown && <details className="border-t border-lw-line" onToggle={event => { if (event.currentTarget.open && !text) void load() }}>
      <summary className="min-h-11 cursor-pointer content-center px-3 text-sm text-lw-link">Preview</summary>
      {error ? <p className="px-3 pb-3 text-sm text-lw-error">{error}</p> : <div className="overflow-x-auto px-4 pb-4 sm:px-6"><WorkflowMarkdownPreview markdown={text} /></div>}
    </details>}
    {!markdown && error && <p className="px-3 pb-3 text-sm text-lw-error">{error}</p>}
  </div>
}

export function WorkflowRunPage() {
  const { runId = "" } = useParams()
  const { data, loading, error, refresh } = useWorkflowRun(runId)
  const { baseUrl } = useServer()
  const now = useNow(15_000)
  if (loading) return <main className="flex min-h-0 flex-1 flex-col"><LoadingState label="Loading the workflow run…" /></main>
  if (error || !data) return <main className="p-6 font-lw text-lw-error">{error || "Run not found"}</main>
  const steps = data.steps ?? []
  const attempts = data.attempts ?? []
  const artifacts = data.artifacts ?? []
  const run = runWithAttention(data.run, attempts)
  const state = runState(run)
  const stepLikes = steps.map(asStepLike)
  const position = stepPosition(run, stepLikes)
  const workflow = definitionTitle(data)
  const subject = run.related_task_id ? "" : runSubject(run)
  const live = run.status === "running" || run.status === "waiting"
  const since = (time: string) => { const value = ago(time, now); return value === "now" ? "just now" : `${value} ago` }
  const when = run.completed_at ? `${run.status === "succeeded" ? "finished" : "ended"} ${since(run.completed_at)}` : run.created_at ? `started ${since(run.created_at)}` : ""
  let where = ""
  if (run.status === "succeeded") where = `${steps.length} ${steps.length === 1 ? "step" : "steps"}${run.started_at && run.completed_at ? ` · took ${elapsed(run.started_at, run.completed_at)}` : ""}`
  else if (position) where = `${run.status === "failed" ? "at step" : "Step"} ${position.number} of ${position.total} · ${position.title}`
  const inputs = objectEntries(run.inputs_json).filter(([, value]) => value !== "" && value !== null)
  const secrets = secretValues(data)
  return <main className="min-h-0 flex-1 overflow-y-auto px-4 py-4 font-lw text-lw-text sm:px-6 md:py-6">
    <div className="mx-auto max-w-3xl">
      <Link to="/workflows?tab=runs" className="text-sm text-lw-link">← Runs</Link>
      <div className="mt-2 flex flex-col gap-2 sm:flex-row sm:items-start sm:justify-between">
        <div className="min-w-0">
          <h1 className="break-words text-xl font-semibold">{run.workflow_title}</h1>
          <p className="mt-1 text-[13px] text-lw-text-2">
            {workflow && workflow !== run.workflow_title && <>{workflow} · </>}
            {run.related_task_id && <>for <Link to={`/board?highlight_task=${encodeURIComponent(run.related_task_id)}`} className="text-lw-link">{run.related_task_id}</Link>{run.related_task_title && ` ${run.related_task_title}`} · </>}
            {subject && <>{subject}{when && " · "}</>}
            {when}
          </p>
        </div>
        {live && <Button variant="ghost" size="sm" className="self-start" onClick={async () => { await cancelWorkflow(baseUrl, runId); await refresh() }}>Cancel run</Button>}
      </div>
      <div className="mt-4 flex flex-wrap items-center gap-3 text-[13px] text-lw-text-2"><StatePill tone={state.tone} label={state.label} />{where && <span>{where}</span>}</div>
      {steps.length > 0 && <StepBar segments={stepSegments(run, stepLikes)} label={where || state.label} className="mt-2" />}
      {run.status === "cancelled" && run.error && <p className="mt-3 text-[13px] text-lw-text-2">{redact(run.error, secrets)}</p>}
      <Now view={data} runId={runId} refresh={refresh} now={now} secrets={secrets} />
      <h2 className="mb-2 mt-7 text-xs font-semibold uppercase tracking-wide text-lw-text-2">Steps</h2>
      <ul aria-label="Steps" className="overflow-hidden rounded-xl border border-lw-line bg-lw-surface">
        {steps.map(step => <StepRow key={step.step_id} step={step} run={run} attempts={attempts.filter(attempt => attempt.step_id === step.step_id)} artifacts={artifacts.filter(artifact => artifact.step_id === step.step_id)} now={now} secrets={secrets} />)}
      </ul>
      {inputs.length > 0 && <>
        <h2 className="mb-2 mt-7 text-xs font-semibold uppercase tracking-wide text-lw-text-2">Inputs</h2>
        <ValueList secrets={secrets} values={inputs} />
      </>}
      <p className="mt-7 break-all font-lw-mono text-xs text-lw-faint">{run.id} · <span title={run.definition_source}>{sourceLabel(run.definition_source ?? "")}</span></p>
    </div>
  </main>
}
