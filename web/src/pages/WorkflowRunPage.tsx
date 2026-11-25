import { useState, type ReactNode } from "react"
import { Link, useParams } from "react-router-dom"
import { ChevronDown, ChevronRight, Download, FileText } from "lucide-react"
import { useWorkflowRun } from "../hooks/useWorkflow"
import { useServer } from "../hooks/useServer"
import { useNow } from "../hooks/useNow"
import { anotherReviewPass, resumeWorkflowAgent, workflowStepAction, type GateDetails, type MergeWaitDetails, type WorkflowCallDetails, cancelWorkflow, retryWorkflowStep, resumeWorkflowStep, triggerWorkflowStep, promptWorkflowStep, fetchWorkflowArtifact, type ReviewRunDetails, type WorkflowArtifact, type WorkflowAttempt, type WorkflowRunView, type WorkflowStepRun } from "../lib/workflow"
import { ago, attentionCopy, currentStep, elapsed, inputText, runWithAttention, producedValues, runState, runSubject, sourceLabel, stepIntent, stepMark, stepPosition, stepSegments, type StepLike, type Tone } from "../lib/workflowView"
import { WorkflowMarkdownPreview } from "../components/WorkflowMarkdownPreview"
import { ManualWorkflowStepAction } from "../components/ManualWorkflowStepAction"
import { workflowArtifactFilename } from "../lib/workflow-artifact"
import { WorkflowStepRetryAction } from "../components/WorkflowStepRetryAction"
import { WorkflowStepResumeAction } from "../components/WorkflowStepResumeAction"
import { StatePill, StepBar } from "../components/WorkflowRunStatus"
import { toneGlyph, toneText } from "../lib/workflowTones"
import { Button } from "../components/ui/Button"
import { LoadingState } from "../components/ui/LoadingState"
import { cn } from "../lib/utils"
import { useMediaQuery } from "../hooks/useMediaQuery"
import { LazyWorkflowGraph } from "../components/workflow-graph/LazyWorkflowGraph"
import { StepYaml } from "../components/StepYaml"
import { redactSecrets, runStateWords, stepTypeLabel, triggerName, yamlText, type GraphNode } from "../lib/workflowGraph"
import { type RunNeedsYouRow, type RunRowStep } from "../lib/runNeedsYou"
import { useApprovals } from "../hooks/useApprovals"
import { agentLink, graphLink, secretValues, useRunGraphExtras } from "../hooks/useRunGraph"
import { useQueryClient } from "@tanstack/react-query"
import { queryKeys } from "../lib/query"
import { dismissAttentionItem, executeAttentionAction, type AttentionItem } from "../lib/attention"
import { NeedsYouRow } from "../components/needsYou/NeedsYouRow"
import { MacroPickerButton } from "../components/MacroPicker"
import { ApprovalCard } from "../components/ApprovalTray"
import { NodeSheet } from "../components/NodeSheet"

function parseJSON(raw: string | undefined): unknown {
  try { return JSON.parse(raw ?? "") } catch { return raw }
}

function stepTrigger(step: WorkflowStepRun): string | undefined {
  // A step says start; one a run kept before schema version 2 says trigger.
  const definition = parseJSON(step.definition_json)
  if (!definition || typeof definition !== "object") return undefined
  const { start, trigger } = definition as { start?: unknown; trigger?: unknown }
  const value = start ?? trigger
  return value === undefined ? undefined : String(value)
}

function asStepLike(step: WorkflowStepRun): StepLike {
  return { step_id: step.step_id, title: step.title, status: step.status, trigger: stepTrigger(step) }
}

// attemptAgent is the agent an attempt's session is: an interview's attempt
// keeps the run's task, so the agent's own ID comes first.
function attemptAgent(attempt: WorkflowAttempt): string {
  return attempt.agent_id || attempt.task_id || ""
}

function definitionTitle(view: WorkflowRunView): string {
  const definition = parseJSON(view.run.definition_json)
  return definition && typeof definition === "object" && "title" in definition ? String((definition as { title: unknown }).title) : ""
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
    // The run's own declared outputs (LEG-489), and those that did not
    // resolve.
    const runOutputs = objectEntries(run.outputs_json ?? "")
    const unresolved = view.output_warnings ?? []
    // Agent steps that finished with no output (LEG-532).
    const warned = steps.filter(step => step.output_warning)
    const warnings = (warned.length > 0 || unresolved.length > 0) && <>
      {unresolved.length > 0 && <ul aria-label="Outputs not resolved" className="mt-2 space-y-1 text-lw-needs">
        {unresolved.map(warning => <li key={warning}>{warning}</li>)}
      </ul>}
      {warned.length > 0 && <ul aria-label="Steps with no output" className="mt-2 space-y-1 text-lw-needs">
        {warned.map(step => <li key={step.step_id}><span className="font-medium">{step.title}:</span> {step.output_warning}</li>)}
      </ul>}
    </>
    if (artifacts.length === 0 && values.length === 0 && runOutputs.length === 0) return warnings ? <NowPanel tone="needs" heading="No output">{warnings}</NowPanel> : null
    return <NowPanel tone="done" heading="Produced">
      {warnings}
      <div className="mt-2 grid gap-2 text-lw-text">
        {runOutputs.length > 0 && <section aria-label="Run outputs"><ValueList values={runOutputs} secrets={secrets} className="mt-1" /></section>}
        {artifacts.map(artifact => <Artifact key={artifact.id} artifact={artifact} step={steps.find(step => step.step_id === artifact.step_id)?.title} />)}
        {runOutputs.length === 0 && values.length > 0 && <ValueList values={values.map(value => [value.key, value.value])} secrets={secrets} className="mt-1" />}
      </div>
    </NowPanel>
  }
  // Resume goes on with a step whose agent Legato stopped with, on the
  // same attempt; Retry starts it afresh.
  const resumable = (step: WorkflowStepRun) => attempts.some(attempt => attempt.step_id === step.step_id && attempt.resumable)
  const resumeAction = (step: WorkflowStepRun) => resumable(step) && <WorkflowStepResumeAction onResume={async () => { await resumeWorkflowStep(baseUrl, runId, step.step_id); await refresh() }} />
  if (run.status === "suspended") {
    const suspended = steps.find(step => step.status === "suspended")
    return <NowPanel tone="idle" heading={suspended ? `${suspended.title} is suspended` : "The run is suspended"}
      actions={suspended && <>{resumeAction(suspended)}<WorkflowStepRetryAction step={suspended} onRetry={async force => { await retryWorkflowStep(baseUrl, runId, suspended.step_id, force); await refresh() }} /></>}>
      <p>Legato stopped while its agent was working. Resume goes on with the agent's conversation, in the same folder; Retry starts the step afresh.</p>
    </NowPanel>
  }
  if (run.status === "failed") {
    const failed = steps.find(step => step.status === "failed")
    return <NowPanel tone="error" heading={failed ? `${failed.title} failed` : "The run failed"}
      actions={failed && <>{resumeAction(failed)}<WorkflowStepRetryAction step={failed} onRetry={async force => { await retryWorkflowStep(baseUrl, runId, failed.step_id, force); await refresh() }} /></>}>
      {(failed?.error || run.error) && <p className="mt-1 whitespace-pre-wrap break-words rounded-lg bg-[var(--theme-surface,rgba(0,0,0,0.25))] px-2.5 py-2 font-lw-mono text-xs text-lw-danger">{redact(failed?.error || run.error || "", secrets)}</p>}
    </NowPanel>
  }
  if (run.status === "cancelled" || run.status === "stopped") return null
  // A step paused after a failure (on_failure: pause): its error, the link
  // it carries (such as the run of the loop that holds the PR), and Retry,
  // Skip or Stop.
  const paused = steps.find(step => step.status === "paused")
  if (paused) return <PausedNow step={paused} runId={runId} refresh={refresh} secrets={secrets} />
  const current = currentStep(steps)
  if (!current) return null
  if (current.status === "waiting" && current.executor_type === "gate") {
    return <GateNow view={view} step={current} runId={runId} refresh={refresh} />
  }
  if (current.status === "waiting" && current.executor_type === "workflow.run" && current.child_run_id) {
    const child = view.details?.[current.step_id] as WorkflowCallDetails | undefined
    return <NowPanel tone={state.tone} heading={`${current.title} waits on its child run`}
      actions={<Button asChild variant="primary"><Link to={runLink(current.child_run_id)}>Open child run</Link></Button>}>
      {child && <p>{child.title || child.workflow} · {child.status.replaceAll("_", " ")}</p>}
    </NowPanel>
  }
  const attempt = [...attempts].reverse().find(item => item.step_id === current.step_id && item.status === "waiting_agent")
  if (attempt) {
    const open = attempt.agent_session_id ? <>
      <Button asChild variant="primary"><Link to={agentLink(attemptAgent(attempt), runId)}>Open agent</Link></Button>
      <Button asChild variant="outline"><Link to={graphLink(attemptAgent(attempt), runId)}>Show on agents graph</Link></Button>
    </> : null
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
  if (current.status === "waiting" && current.executor_type === "github.pr.review_passes") {
    return <ReviewLoopNow view={view} step={current} runId={runId} refresh={refresh} />
  }
  const resume = <ResumeAgentControls view={view} runId={runId} refresh={refresh} />
  if (current.status === "waiting" && current.executor_type === "github.pr.wait") {
    return <MergeWaitNow view={view} step={current} actions={resume} />
  }
  if (current.status === "waiting_manual") {
    if (stepTrigger(current) === "agent_ended" && !run.needs_attention) {
      return <NowPanel tone="live" heading={`${current.title} starts when the agent ends`}
        actions={<>
          {run.task_id && <Button asChild variant="primary"><Link to={agentLink(run.task_id, runId)}>Open agent</Link></Button>}
          {run.task_id && <Button asChild variant="outline"><Link to={graphLink(run.task_id, runId)}>Show on agents graph</Link></Button>}
          <ManualWorkflowStepAction step={current} label="End now" onTrigger={async force => { await triggerWorkflowStep(baseUrl, runId, current.step_id, force); await refresh() }} />
        </>} />
    }
    return <NowPanel tone="needs" heading={`${current.title} waits for you`}
      actions={<><ManualWorkflowStepAction step={current} onTrigger={async force => { await triggerWorkflowStep(baseUrl, runId, current.step_id, force); await refresh() }} />{resume}</>}>
      {stepIntent(current) && <p>{stepIntent(current)}</p>}
    </NowPanel>
  }
  return <NowPanel tone={state.tone} heading={`Running ${current.title}`} actions={view.can_resume_agent ? resume : undefined} />
}

// useStepAction takes an action on a step (StepAction on the server), and
// says why one was refused.
function useStepAction(runId: string, step: string, refresh: () => Promise<void>) {
  const { baseUrl } = useServer()
  const [error, setError] = useState("")
  const [busy, setBusy] = useState(false)
  async function act(action: string) {
    setError("")
    setBusy(true)
    try { await workflowStepAction(baseUrl, runId, step, action); await refresh() }
    catch (c) { setError(c instanceof Error ? c.message : String(c)) }
    finally { setBusy(false) }
  }
  return { act, busy, error }
}

// PausedNow is a step that failed under on_failure: pause.
function PausedNow({ step, runId, refresh, secrets }: { step: WorkflowStepRun; runId: string; refresh: () => Promise<void>; secrets: string[] }) {
  const { act, busy, error } = useStepAction(runId, step.step_id, refresh)
  const internal = step.error_link?.startsWith("/")
  return <NowPanel tone="needs" heading={`${step.title} failed`}
    actions={<>
      <Button variant="primary" disabled={busy} onClick={() => { void act("retry") }}>Retry</Button>
      <Button variant="outline" disabled={busy} onClick={() => { void act("skip") }}>Skip</Button>
      <Button variant="ghost" disabled={busy} onClick={() => { void act("stop") }}>Stop</Button>
    </>}>
    {step.error && <p className="mt-1 whitespace-pre-wrap break-words rounded-lg bg-[var(--theme-surface,rgba(0,0,0,0.25))] px-2.5 py-2 font-lw-mono text-xs text-lw-danger">{redact(step.error, secrets)}</p>}
    {step.error_link && <p className="mt-1">See {internal ? <Link className="text-lw-accent underline" to={step.error_link}>{step.error_link}</Link> : <a className="text-lw-accent underline" href={step.error_link} target="_blank" rel="noreferrer">{step.error_link}</a>}{step.error_code && <> · <span className="font-lw-mono text-xs">{step.error_code}</span></>}</p>}
    <p className="mt-1">Retry runs the step again, Skip goes on without its outputs, and Stop ends the run.</p>
    {error && <p role="alert" className="mt-1 text-lw-error">{error}</p>}
  </NowPanel>
}

// GateNow is a gate step asking the operator to choose.
function GateNow({ view, step, runId, refresh }: { view: WorkflowRunView; step: WorkflowStepRun; runId: string; refresh: () => Promise<void> }) {
  const { act, busy, error } = useStepAction(runId, step.step_id, refresh)
  const gate = view.details?.[step.step_id] as GateDetails | undefined
  const options = gate?.options ?? ["Continue", "Stop"]
  return <NowPanel tone="needs" heading={gate?.title || step.title}
    actions={options.map((option, i) => <Button key={option} variant={i === 0 ? "primary" : option.toLowerCase() === "stop" ? "ghost" : "outline"} disabled={busy} onClick={() => { void act(`choose:${option}`) }}>{option}</Button>)}>
    {gate?.body && <p className="whitespace-pre-wrap">{gate.body}</p>}
    {error && <p role="alert" className="mt-1 text-lw-error">{error}</p>}
  </NowPanel>
}

// MergeWaitNow is a run waiting for its card's pull request to merge: the
// pull request, whether it conflicts or CI fails, the card's agent, and
// Resume agent (LEG-614).
function MergeWaitNow({ view, step, actions }: { view: WorkflowRunView; step: WorkflowStepRun; actions: ReactNode }) {
  const wait = view.details?.[step.step_id] as MergeWaitDetails | undefined
  if (!wait) return <NowPanel tone="live" heading={step.title} actions={actions} />
  const troubles = [wait.conflicts && `PR #${wait.pr_number} conflicts with ${wait.base_branch || "main"}`, wait.check_status === "fail" && "CI failing"].filter(Boolean) as string[]
  const url = wait.url || `https://github.com/${wait.repo}/pull/${wait.pr_number}`
  return <NowPanel tone={troubles.length > 0 ? "needs" : "live"} heading={`Waiting for PR #${wait.pr_number} to merge`}
    actions={<><Button asChild variant="outline"><a href={url} target="_blank" rel="noopener noreferrer">Open PR</a></Button>{actions}</>}>
    <p>{wait.repo}{wait.head_branch && <> · <span className="font-lw-mono text-xs">{wait.head_branch}</span></>}{wait.check_status && wait.check_status !== "fail" && <> · CI {wait.check_status === "pass" ? "passing" : wait.check_status}</>}</p>
    {troubles.length > 0 && <p className="mt-1 font-medium text-lw-needs">{troubles.join(" · ")}</p>}
    <p className="mt-1">{wait.agent === "running" ? "The card's agent is running: a macro goes to it as its next message." : wait.agent === "waiting" ? "The card's agent waits on you." : "The card's agent is stopped. Resume it with a macro, such as Rebase and repush, to fix the PR. The run goes on waiting for the merge."}</p>
  </NowPanel>
}

// ResumeAgentControls are Open agent while the run's card has an agent, and
// Resume agent… (Send a macro… while it runs), which starts or messages it
// with the macro picked.
function ResumeAgentControls({ view, runId, refresh }: { view: WorkflowRunView; runId: string; refresh: () => Promise<void> }) {
  const { baseUrl } = useServer()
  const [error, setError] = useState("")
  const [notice, setNotice] = useState("")
  const [busy, setBusy] = useState(false)
  const card = view.resume_task_id
  if (!card) return null
  async function pick(name: string) {
    setError("")
    setNotice("")
    setBusy(true)
    try {
      const result = await resumeWorkflowAgent(baseUrl, runId, name)
      setNotice(result.queued ? `${name} will be the agent's next message.` : `Started the agent with ${name}.`)
      await refresh()
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : String(reason))
    } finally {
      setBusy(false)
    }
  }
  return <>
    {view.resume_agent_state && <Button asChild variant="primary"><Link to={agentLink(card, runId)}>Open agent</Link></Button>}
    {view.can_resume_agent && <MacroPickerButton label={view.resume_agent_state === "running" ? "Send a macro" : "Resume agent"} disabled={busy} targets={["task", "pr"]} onPick={macro => { void pick(macro.name) }}
      className="inline-flex min-h-9 items-center rounded-md border border-lw-line-strong/40 bg-lw-raised px-3 text-[13px] font-medium text-lw-text hover:bg-lw-selected disabled:opacity-50" />}
    {notice && <span role="status" className="text-xs text-lw-text-2">{notice}</span>}
    {error && <span role="alert" className="text-xs text-lw-error">{error}</span>}
  </>
}

// ReviewLoopNow is a run's review loop while its step waits on it: where it
// stands, the reviews posted, and Run another pass while it waits for its
// author.
function ReviewLoopNow({ view, step, runId, refresh }: { view: WorkflowRunView; step: WorkflowStepRun; runId: string; refresh: () => Promise<void> }) {
  const { baseUrl } = useServer()
  const [error, setError] = useState("")
  const [busy, setBusy] = useState(false)
  const details = view.details?.[step.step_id] as ReviewRunDetails | undefined
  async function another() {
    setError("")
    setBusy(true)
    try { await anotherReviewPass(baseUrl, runId, step.step_id); await refresh() }
    catch (c) { setError(c instanceof Error ? c.message : String(c)) }
    finally { setBusy(false) }
  }
  const stuck = details?.phase === "stuck"
  const waiting = !details || details.phase === "waiting_for_author"
  return <NowPanel tone={details?.needs_you ? "needs" : "live"} heading={details?.summary || step.title}
    actions={(waiting || stuck) && <Button variant="outline" disabled={busy} onClick={another}>Run another pass</Button>}>
    {waiting && <p>The next pass starts when the author pushes, or replies to a blocking finding.</p>}
    {details?.reviews && details.reviews.length > 0 && <ul aria-label="Reviews posted" className="mt-1 space-y-0.5">
      {details.reviews.map(review => <li key={`${review.pass}-${review.pr}`}><a className="text-lw-accent underline" href={review.url} target="_blank" rel="noreferrer">Pass {review.pass} review of #{review.pr}</a></li>)}
    </ul>}
    {error && <p role="alert" className="mt-1 text-lw-error">{error}</p>}
  </NowPanel>
}

function stepDuration(step: WorkflowStepRun, now: number): string {
  if (!step.started_at) return ""
  return elapsed(step.started_at, step.completed_at ?? now)
}

const runLink = (id: string) => `/workflows/runs/${encodeURIComponent(id)}`

function objectEntries(raw: string): [string, unknown][] {
  const value = parseJSON(raw)
  return value && typeof value === "object" && !Array.isArray(value) ? Object.entries(value as Record<string, unknown>) : []
}

function StepRow({ step, run, attempts, artifacts, now, secrets }: { step: WorkflowStepRun; run: WorkflowRunView["run"]; attempts: WorkflowAttempt[]; artifacts: WorkflowArtifact[]; now: number; secrets: string[] }) {
  const [open, setOpen] = useState(step.status === "failed" || !!step.output_warning)
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
      {step.output_warning && <p className="text-lw-needs">{step.output_warning}</p>}
      {step.child_run_id && <p className="text-lw-text-2">Child run <Link className="font-lw-mono text-xs text-lw-link" to={runLink(step.child_run_id)}>{step.child_run_id}</Link></p>}
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
  const [text, setText] = useState<string | null>(null)
  const [error, setError] = useState("")
  const [loading, setLoading] = useState(false)
  const [downloadError, setDownloadError] = useState("")
  const [downloading, setDownloading] = useState(false)
  const filename = workflowArtifactFilename(artifact.original_path, artifact.output_key)
  async function load() {
    setError("")
    setLoading(true)
    try { const blob = await fetchWorkflowArtifact(baseUrl, artifact.id); setText(await blob.text()) }
    catch (c) { setError(c instanceof Error ? c.message : String(c)) }
    finally { setLoading(false) }
  }
  async function download() {
    setDownloadError("")
    setDownloading(true)
    try { const blob = await fetchWorkflowArtifact(baseUrl, artifact.id); const url = URL.createObjectURL(blob); const link = document.createElement("a"); link.href = url; link.download = filename; link.click(); URL.revokeObjectURL(url) }
    catch (c) { setDownloadError(c instanceof Error ? c.message : String(c)) }
    finally { setDownloading(false) }
  }
  const markdown = artifact.media_type === "text/markdown"
  return <div className="rounded-lg border border-lw-line bg-lw-surface">
    <div className="flex flex-wrap items-center gap-3 px-3 py-2.5">
      <FileText size={16} aria-hidden="true" className="shrink-0 text-lw-text-2" />
      <div className="min-w-0 flex-1 basis-40"><p className="break-all font-medium">{filename}</p><p className="text-xs text-lw-faint">{step && `from ${step} · `}{markdown ? "Markdown" : artifact.media_type} · {formatBytes(artifact.size_bytes)}</p></div>
      <Button variant="outline" size="sm" disabled={downloading} onClick={download}><Download size={14} />Download</Button>
    </div>
    {markdown && <details className="border-t border-lw-line" onToggle={event => { if (event.currentTarget.open && text === null && !loading) void load() }}>
      <summary className="min-h-11 cursor-pointer content-center px-3 text-sm text-lw-link">Preview</summary>
      {loading ? <LoadingState label="Loading preview…" size="inline" className="px-3 pb-3" /> : error ? <div role="alert" className="space-y-2 px-3 pb-3 text-sm text-lw-error"><p>{error}</p><Button variant="outline" size="sm" onClick={() => { void load() }}>Retry preview</Button></div> : <div className="overflow-x-auto px-4 pb-4 sm:px-6"><WorkflowMarkdownPreview markdown={text ?? ""} /></div>}
    </details>}
    {downloadError && <p role="alert" className="px-3 pb-3 text-sm text-lw-error">Download failed: {downloadError}</p>}
  </div>
}

// CleanupControl is a worktree removal's own button in a Needs you row: the
// same controls as the Now panel, which ask first, show the path and offer
// force, so the row never removes a worktree without asking.
function CleanupControl({ run, steps, runId, refresh }: { run: RunRowStep; steps: WorkflowStepRun[]; runId: string; refresh: () => Promise<void> }) {
  const { baseUrl } = useServer()
  const step = steps.find(item => item.step_id === run.step)
  if (!step) return null
  return <div className="mt-1.5 flex flex-wrap items-center gap-2" onClick={event => event.stopPropagation()}>
    {run.control === "retry"
      ? <WorkflowStepRetryAction step={step} onRetry={async force => { await retryWorkflowStep(baseUrl, runId, step.step_id, force); await refresh() }} />
      : <ManualWorkflowStepAction step={step} label={run.label} onTrigger={async force => { await triggerWorkflowStep(baseUrl, runId, step.step_id, force); await refresh() }} />}
  </div>
}

// NeedsYouList is everything the run waits on the operator for, drawn with
// the needs-you rail's own approval card and row, above the graph. A click
// on a row shows its node.
function NeedsYouList({ rows, steps, runId, refresh, onFocus }: { rows: RunNeedsYouRow[]; steps: WorkflowStepRun[]; runId: string; refresh: () => Promise<void>; onFocus: (node: string | null) => void }) {
  const { baseUrl } = useServer()
  const queryClient = useQueryClient()
  const approvals = useApprovals(false)
  const [pending, setPending] = useState<string | null>(null)
  const [hidden, setHidden] = useState<Set<string>>(new Set())
  const [error, setError] = useState("")
  const shown = rows.filter(row => row.kind === "approval" || !hidden.has(row.item.id))
  if (shown.length === 0) return null
  const act = async (item: AttentionItem, call: () => Promise<unknown>) => {
    setPending(item.id)
    setError("")
    try {
      await call()
      await Promise.all([refresh(), queryClient.invalidateQueries({ queryKey: queryKeys.attention(baseUrl) })])
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : String(reason))
    } finally {
      setPending(null)
    }
  }
  const onAction = (item: AttentionItem, actionId: string, text?: string) => {
    const row = rows.find(each => each.kind === "item" && each.item.id === item.id)
    const step = row?.kind === "item" ? row.run?.step : undefined
    if (step) {
      const calls: Record<string, () => Promise<unknown>> = {
        resume_agent: () => resumeWorkflowAgent(baseUrl, runId, text ?? ""),
        start: () => triggerWorkflowStep(baseUrl, runId, step, false),
        end_now: () => triggerWorkflowStep(baseUrl, runId, step, false),
        prompt: () => promptWorkflowStep(baseUrl, runId, step),
        retry: () => retryWorkflowStep(baseUrl, runId, step, false),
        another_pass: () => anotherReviewPass(baseUrl, runId, step),
        skip: () => workflowStepAction(baseUrl, runId, step, "skip"),
        stop: () => workflowStepAction(baseUrl, runId, step, "stop"),
      }
      if (actionId.startsWith("choose:")) { void act(item, () => workflowStepAction(baseUrl, runId, step, actionId)); return }
      if (calls[actionId]) void act(item, calls[actionId])
      return
    }
    const action = item.actions?.find(each => each.id === actionId)
    const confirmed = action?.requires_confirmation ?? false
    void act(item, () => executeAttentionAction(baseUrl, item.id, actionId, item.source_revision, text === undefined ? { confirmed } : { confirmed, text }))
  }
  const onDismiss = (item: AttentionItem) => {
    if (item.producer === "run") { setHidden(current => new Set(current).add(item.id)); return }
    void act(item, () => dismissAttentionItem(baseUrl, item.id, item.source_revision))
  }
  return <section aria-label="Needs you" className="mt-5">
    <h2 className="mb-2 flex items-baseline gap-2 text-xs font-semibold uppercase tracking-wide text-lw-needs">Needs you<span className="font-medium text-lw-faint">{shown.length}</span></h2>
    {error && <p role="alert" className="mb-2 text-[13px] text-lw-error">{error}</p>}
    <div className="grid gap-2 md:grid-cols-2">
      {shown.map(row => <div key={row.kind === "approval" ? row.approval.id : row.item.id} data-run-row="" className="cursor-pointer" onClick={event => {
        // A tap on the row shows its step; one on a control in it (a button,
        // a link, Reply…'s field) is that control's alone.
        if ((event.target as Element).closest("button, a, input, textarea, select, label, [role=dialog]")) return
        onFocus(row.node)
      }}>
        {row.kind === "approval"
          ? <ApprovalCard approval={row.approval} focused={false} onDone={async () => { await approvals.refresh(); await refresh() }} />
          : <NeedsYouRow item={row.item} onAction={onAction} onDismiss={onDismiss} pending={pending === row.item.id} />}
        {row.kind === "item" && row.run?.control && <CleanupControl run={row.run} steps={steps} runId={runId} refresh={refresh} />}
      </div>)}
    </div>
  </section>
}

function clock(time?: string): string {
  return time ? time.replace("T", " ").replace(/(:\d\d)(\.\d+)?Z?$/, "$1") : ""
}

// RunStepPanel is one step of a run, beside its graph: what went in and
// came out, every attempt, its artifacts, a review loop's passes, and its
// YAML from the run's own copy, with the run's secrets hidden.
function RunStepPanel({ view, node, runId, secrets, state }: { view: WorkflowRunView; node: GraphNode; runId: string; secrets: string[]; state?: string }) {
  const graphStep = node.step!
  const step = (view.steps ?? []).find(item => item.step_id === graphStep.id)
  const attempts = (view.attempts ?? []).filter(attempt => attempt.step_id === graphStep.id)
  const artifacts = (view.artifacts ?? []).filter(artifact => artifact.step_id === graphStep.id)
  const inputs = step ? objectEntries(step.resolved_inputs_json) : []
  const outputs = step ? objectEntries(step.outputs_json).filter(([key]) => !artifacts.some(artifact => artifact.output_key === key)) : []
  const details = view.details?.[graphStep.id] as ReviewRunDetails | undefined
  const yaml = redact(yamlText(redactSecrets(graphStep.definition, new Set(secrets))), secrets)
  return <aside aria-label={graphStep.title} className="flex min-w-0 flex-col gap-4 rounded-xl border border-lw-line bg-lw-surface p-4 text-[13px]">
    <div>
      <p className="text-[11px] font-medium uppercase tracking-wide text-lw-text-2">Step · {stepTypeLabel(graphStep.type)}{state && <> · {state}</>}</p>
      <h2 className="mt-0.5 text-[15px] font-semibold">{graphStep.title}</h2>
      {step?.error && <p className="mt-1 whitespace-pre-wrap break-words text-lw-error">{redact(step.error, secrets)}</p>}
      {step?.output_warning && <p className="mt-1 text-lw-needs">{step.output_warning}</p>}
      {step?.child_run_id && <p className="mt-1 text-lw-text-2">Child run <Link className="font-lw-mono text-xs text-lw-link" to={runLink(step.child_run_id)}>{step.child_run_id}</Link></p>}
    </div>
    {inputs.length > 0 && <div><h3 className="mb-1 text-xs text-lw-text-2">Inputs</h3><ValueList values={inputs} secrets={secrets} /></div>}
    {outputs.length > 0 && <div><h3 className="mb-1 text-xs text-lw-text-2">Outputs</h3><ValueList values={outputs} secrets={secrets} /></div>}
    {attempts.length > 0 && <div><h3 className="mb-1 text-xs text-lw-text-2">Attempts</h3>
      <ul aria-label="Attempts" className="space-y-2">{attempts.map(attempt => <li key={attempt.id} className="rounded-lg border border-lw-line p-2.5 text-lw-text-2">
        <p className="text-lw-text">Attempt {attempt.attempt_number} · {attempt.status.replaceAll("_", " ")}</p>
        <p className="font-lw-mono text-xs">{clock(attempt.started_at)}{attempt.ended_at && <> → {clock(attempt.ended_at)}</>}</p>
        {(attempt.agent_kind || attempt.tier) && <p>{attempt.agent_kind || attempt.executor_type}{attempt.tier && ` (${attempt.tier})`}</p>}
        {attempt.working_dir && <p className="break-all font-lw-mono text-xs text-lw-faint">{attempt.working_dir}</p>}
        {attempt.error && <p className="text-lw-error">{redact(attempt.error, secrets)}</p>}
        {attempt.agent_session_id && attemptAgent(attempt) ? <Button asChild variant="outline" size="sm" className="mt-1.5"><Link to={agentLink(attemptAgent(attempt), runId)}>Open agent</Link></Button> : null}
      </li>)}</ul>
    </div>}
    {artifacts.length > 0 && <div className="grid gap-2"><h3 className="text-xs text-lw-text-2">Artifacts</h3>{artifacts.map(artifact => <Artifact key={artifact.id} artifact={artifact} />)}</div>}
    {details && <div><h3 className="mb-1 text-xs text-lw-text-2">Review loop</h3>
      <p>{details.summary || details.phase.replaceAll("_", " ")}</p>
      <p className="text-lw-text-2">Pass {details.pass} · {details.blocking_open} blocking</p>
      {details.reviews && details.reviews.length > 0 && <ul aria-label="Reviews posted" className="mt-1 space-y-0.5">
        {details.reviews.map(review => <li key={`${review.pass}-${review.pr}`}><a className="text-lw-link underline" href={review.url} target="_blank" rel="noreferrer">Pass {review.pass} review of #{review.pr}</a></li>)}
      </ul>}
    </div>}
    <StepYaml yaml={yaml} source={view.run.definition_source} />
  </aside>
}

function RunTriggerPanel({ node, started }: { node: GraphNode; started: boolean }) {
  const trigger = node.trigger!
  const summary = trigger.summary || triggerName(trigger.kind)
  return <aside aria-label={summary} className="flex flex-col gap-2 rounded-xl border border-lw-line bg-lw-surface p-4 text-[13px]">
    <p className="text-[11px] font-medium uppercase tracking-wide text-lw-text-2">Trigger · {triggerName(trigger.kind)}</p>
    <h2 className="text-[15px] font-semibold">{summary}</h2>
    <p className="text-lw-text-2">{started ? "This trigger started this run." : "This run was not started by this trigger."}</p>
    {trigger.definition && <pre className="overflow-x-auto rounded-lg border border-lw-line bg-lw-ground p-3 font-lw-mono text-xs">{yamlText(trigger.definition)}</pre>}
  </aside>
}

export function WorkflowRunPage() {
  const { runId = "" } = useParams()
  const { data, loading, refreshing, error, refresh } = useWorkflowRun(runId)
  const { baseUrl } = useServer()
  const now = useNow(15_000)
  // At phone width the step list stays, with a Graph toggle; the graph then
  // runs top to bottom and details open in a sheet.
  const narrow = useMediaQuery("(max-width: 767px)")
  const [graphOn, setGraphOn] = useState(false)
  const [selected, setSelected] = useState<string | null>(null)
  const [centerOn, setCenterOn] = useState<{ id: string; n: number } | null>(null)
  const { graph, needsYou, extras } = useRunGraphExtras(runId, data, now)
  const focusNode = (node: string | null) => {
    if (!node) return
    setSelected(node)
    setCenterOn(current => ({ id: node, n: (current?.n ?? 0) + 1 }))
  }
  if (loading) return <main className="flex min-h-0 flex-1 flex-col"><LoadingState label="Loading the workflow run…" /></main>
  if (!data) return <main className="p-6 font-lw text-lw-text">
    <Link to="/workflows?tab=runs" className="text-sm text-lw-link">← Runs</Link>
    <div role="alert" className="mt-4 space-y-2 text-sm text-lw-error"><p>{error || "Run not found"}</p><Button variant="outline" disabled={refreshing} onClick={() => { void refresh() }}>Retry</Button></div>
  </main>
  const steps = data.steps ?? []
  const attempts = data.attempts ?? []
  const artifacts = data.artifacts ?? []
  const run = runWithAttention(data.run, attempts)
  const state = runState(run)
  const stepLikes = steps.map(asStepLike)
  const position = stepPosition(run, stepLikes)
  const workflow = definitionTitle(data)
  const subject = run.related_task_id ? "" : runSubject(run)
  const live = run.status === "running" || run.status === "waiting" || run.status === "suspended"
  const since = (time: string) => { const value = ago(time, now); return value === "now" ? "just now" : `${value} ago` }
  const when = run.completed_at ? `${run.status === "succeeded" ? "finished" : "ended"} ${since(run.completed_at)}` : run.created_at ? `started ${since(run.created_at)}` : ""
  let where = ""
  if (run.status === "succeeded") where = `${steps.length} ${steps.length === 1 ? "step" : "steps"}${run.started_at && run.completed_at ? ` · took ${elapsed(run.started_at, run.completed_at)}` : ""}`
  else if (position) where = `${run.status === "failed" ? "at step" : "Step"} ${position.number} of ${position.total} · ${position.title}`
  const inputs = objectEntries(run.inputs_json).filter(([, value]) => value !== "" && value !== null)
  const secrets = secretValues(data)
  const showGraph = Boolean(graph) && (!narrow || graphOn)
  const selectedNode = graph?.nodes.find(node => node.id === selected)
  const panel = selectedNode?.kind === "step"
    ? <RunStepPanel view={data} node={selectedNode} runId={runId} secrets={secrets} state={extras?.[selectedNode.id]?.state && runStateWords[extras[selectedNode.id].state!]} />
    : selectedNode?.kind === "trigger" ? <RunTriggerPanel node={selectedNode} started={extras?.[selectedNode.id]?.trigger === "started"} /> : null
  return <main className="min-h-0 flex-1 overflow-y-auto px-4 py-4 font-lw text-lw-text sm:px-6 md:py-6">
    <div className={cn("mx-auto", showGraph ? "max-w-6xl" : "max-w-3xl")}>
      <Link to="/workflows?tab=runs" className="text-sm text-lw-link">← Runs</Link>
      <div className="mt-2 flex flex-col gap-2 sm:flex-row sm:items-start sm:justify-between">
        <div className="min-w-0">
          <h1 className="break-words text-xl font-semibold">{run.workflow_title}</h1>
          <p className="mt-1 text-[13px] text-lw-text-2">
            {workflow && workflow !== run.workflow_title && <>{workflow} · </>}
            {run.parent_run_id && <span>called by <Link to={runLink(run.parent_run_id)} className="text-lw-link">{run.parent_run_id}</Link>{run.parent_step_path && <> at <span className="font-lw-mono text-xs">{run.parent_step_path}</span></>} · </span>}
            {run.related_task_id && <>for <Link to={`/board?highlight_task=${encodeURIComponent(run.related_task_id)}`} className="text-lw-link">{run.related_task_id}</Link>{run.related_task_title && ` ${run.related_task_title}`} · </>}
            {subject && <>{subject}{when && " · "}</>}
            {when}
          </p>
        </div>
        {/* A failed run keeps what it holds for Retry; Cancel gives it up. */}
        {(live || run.status === "failed") && <Button variant="ghost" size="sm" className="self-start" onClick={async () => { await cancelWorkflow(baseUrl, runId); await refresh() }}>Cancel run</Button>}
      </div>
      <div className="mt-4 flex flex-wrap items-center gap-3 text-[13px] text-lw-text-2"><StatePill tone={state.tone} label={state.label} />{where && <span>{where}</span>}</div>
      {steps.length > 0 && <StepBar segments={stepSegments(run, stepLikes)} label={where || state.label} className="mt-2" />}
      {(run.status === "cancelled" || run.status === "stopped") && run.error && <p className="mt-3 text-[13px] text-lw-text-2">{redact(run.error, secrets)}</p>}
      {error && <div role="alert" className="mt-4 rounded-lg border border-lw-line bg-lw-surface p-3 text-sm">
        <p className="text-lw-needs">Showing saved run. Refresh failed: {error}</p>
        <Button variant="outline" size="sm" className="mt-2" disabled={refreshing} onClick={() => { void refresh() }}>Retry</Button>
      </div>}
      {(data.definition_changed || data.definition_missing) && <p role="note" className="mt-4 rounded-lg border border-lw-line bg-lw-surface px-3 py-2 text-[13px] text-lw-text-2">
        {data.definition_missing ? "This workflow's file is gone" : "This workflow has changed since this run"}<span className="text-lw-faint"> · the graph is the run's own copy</span>
      </p>}
      <Now key={`${baseUrl}:${runId}`} view={data} runId={runId} refresh={refresh} now={now} secrets={secrets} />
      {graph && <NeedsYouList rows={needsYou.rows} steps={steps} runId={runId} refresh={refresh} onFocus={focusNode} />}
      {graph && narrow && <div className="mt-7 flex justify-end">
        <Button variant="outline" size="sm" aria-pressed={graphOn} onClick={() => setGraphOn(!graphOn)}>Graph</Button>
      </div>}
      {showGraph && graph ? <div className={cn(narrow ? "mt-3" : "mt-7", "grid gap-4", selectedNode && !narrow && "lg:grid-cols-[minmax(0,1fr)_24rem]")}>
        <section aria-label="Run graph" className={cn("overflow-hidden rounded-xl border border-lw-line bg-lw-ground", narrow ? "h-[34rem]" : "h-[30rem]")}>
          <LazyWorkflowGraph graph={graph} extras={extras} run selectedId={selected} onSelect={setSelected} centerOn={centerOn} direction={narrow ? "TB" : "LR"} />
        </section>
        {!narrow && panel}
      </div> : <>
        <h2 className="mb-2 mt-7 text-xs font-semibold uppercase tracking-wide text-lw-text-2">Steps</h2>
        <ul aria-label="Steps" className="overflow-hidden rounded-xl border border-lw-line bg-lw-surface">
          {steps.map(step => <StepRow key={`${baseUrl}:${runId}:${step.step_id}`} step={step} run={run} attempts={attempts.filter(attempt => attempt.step_id === step.step_id)} artifacts={artifacts.filter(artifact => artifact.step_id === step.step_id)} now={now} secrets={secrets} />)}
        </ul>
      </>}
      {showGraph && narrow && panel && <NodeSheet title={selectedNode?.step?.title ?? selectedNode?.trigger?.summary ?? ""} onClose={() => setSelected(null)}>{panel}</NodeSheet>}
      {inputs.length > 0 && <>
        <h2 className="mb-2 mt-7 text-xs font-semibold uppercase tracking-wide text-lw-text-2">Inputs</h2>
        <ValueList secrets={secrets} values={inputs} />
      </>}
      <p className="mt-7 break-all font-lw-mono text-xs text-lw-faint">{run.id} · <span title={run.definition_source}>{sourceLabel(run.definition_source ?? "")}</span></p>
    </div>
  </main>
}
