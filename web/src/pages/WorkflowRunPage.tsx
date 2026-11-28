import { createContext, useContext, useState, type ReactNode } from "react"
import { Link, useParams } from "react-router-dom"
import { ChevronDown, ChevronRight, Download, FileText } from "lucide-react"
import { useWorkflowRun } from "../hooks/useWorkflow"
import { useServer } from "../hooks/useServer"
import { useNow } from "../hooks/useNow"
import { anotherReviewPass, resumeWorkflowAgent, stopTaskAgent, workflowStepAction, type ReviewLoopAction, type GateDetails, type MergeWaitDetails, type WorkflowCallDetails, cancelWorkflow, retryWorkflowStep, resumeWorkflowStep, triggerWorkflowStep, promptWorkflowStep, fetchWorkflowArtifact, type ReviewRunDetails, type WorkflowArtifact, type WorkflowAttempt, type WorkflowRunView, type WorkflowStepRun } from "../lib/workflow"
import { ago, attentionCopy, currentStep, elapsed, runWithAttention, producedValues, runState, runSubject, sourceLabel, stepIntent, stepMark, stepPosition, stepSegments, type StepLike, type Tone } from "../lib/workflowView"
import { WorkflowMarkdownPreview } from "../components/WorkflowMarkdownPreview"
import { ManualWorkflowStepAction } from "../components/ManualWorkflowStepAction"
import { workflowArtifactFilename } from "../lib/workflow-artifact"
import { WorkflowStepRetryAction } from "../components/WorkflowStepRetryAction"
import { WorkflowStepResumeAction } from "../components/WorkflowStepResumeAction"
import { StatePill, StepBar } from "../components/workflow-status/WorkflowRunStatus"
import { toneGlyph, toneText } from "../lib/workflowTones"
import { Button } from "../components/ui/Button"
import { LoadingState } from "../components/ui/LoadingState"
import { cn } from "../lib/utils"
import { useMediaQuery } from "../hooks/useMediaQuery"
import { useConfirm } from "../components/ui/useConfirm"
import { StepYaml } from "../components/StepYaml"
import { redactSecrets, runStateWords, stepTypeLabel, triggerName, yamlText } from "../lib/workflowGraph"
import { type GraphNode } from "../lib/workflow"
import { type RunNeedsYouRow, type RunRowStep } from "../lib/runNeedsYou"
import { isTopStep, runStepTree, stepPathOf, type RunStepNode } from "../lib/workflowRunTree"
import { instancesOf, templatePath } from "../lib/workflowRunGraph"
import { useApprovals } from "../hooks/useApprovals"
import { agentLink, graphLink, secretValues, useRunGraphExtras } from "../hooks/useRunGraph"
import { useQueryClient } from "@tanstack/react-query"
import { queryKeys } from "../lib/query"
import { dismissAttentionItem, executeAttentionAction, type AttentionItem } from "../lib/attention"
import { NeedsYouRow } from "../components/needsYou/NeedsYouRow"
import { MacroPickerButton } from "../components/macros/MacroPicker"
import { ApprovalCard } from "../components/approvals/ApprovalTray"
import { DockCard, PanelClose } from "../components/workflow-canvas/DockCard"
import { cardLook, cardTitle, floatingPanel, redact } from "../components/workflow-canvas/canvasStyles"
import { ValueList } from "../components/workflow-canvas/ValueList"
import { useCardFolds, useLargeScreen } from "../components/workflow-canvas/useCardFolds"
import { CanvasHeader, WorkflowCanvas } from "../components/workflow-canvas/WorkflowCanvas"
import { ReviewLoopFacts } from "../components/ReviewLoopFacts"
import { endConfirmation, reviewLoopActionLabels } from "../lib/reviewLoopActions"

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


// NowFold is set over a run's graph, where the Now panel is a card that
// folds; the step list's page draws it as it always has.
const NowFold = createContext<{ folded: boolean; toggle: () => void } | null>(null)

function NowPanel({ tone, heading, children, actions }: { tone: Tone; heading: ReactNode; children?: ReactNode; actions?: ReactNode }) {
  const fold = useContext(NowFold)
  if (fold) return <DockCard label="Now" tone={tone} folded={fold.folded} onFold={fold.toggle}
    heading={<h2 className="flex items-start gap-2 py-1 text-[14px] font-semibold leading-snug"><span aria-hidden="true" className={toneText[tone]}>{toneGlyph[tone]}</span>{heading}</h2>}>
    {(children || actions) && <div className="px-4 pb-4 pl-10">
      {children && <div className="text-[13px] text-lw-text-2">{children}</div>}
      {actions && <div className="mt-3 flex flex-wrap items-center gap-2">{actions}</div>}
    </div>}
  </DockCard>
  return <section aria-label="Now" className={cn("mt-5 rounded-xl border p-4", cardLook[tone])}>
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
  // A step is known by its path: a loop's passes have steps of one ID.
  const ofStep = (item: { step_id: string; path?: string }, step: WorkflowStepRun) => stepPathOf(item) === stepPathOf(step)
  if (run.status === "succeeded") {
    const values = producedValues(steps.filter(isTopStep))
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
        {warned.map(step => <li key={stepPathOf(step)}><span className="font-medium">{step.title}:</span> {step.output_warning}</li>)}
      </ul>}
    </>
    if (artifacts.length === 0 && values.length === 0 && runOutputs.length === 0) return warnings ? <NowPanel tone="needs" heading="No output">{warnings}</NowPanel> : null
    return <NowPanel tone="done" heading="Produced">
      {warnings}
      <div className="mt-2 grid gap-2 text-lw-text">
        {runOutputs.length > 0 && <section aria-label="Run outputs"><ValueList values={runOutputs} secrets={secrets} className="mt-1" /></section>}
        {artifacts.map(artifact => <Artifact key={artifact.id} artifact={artifact} step={steps.find(step => ofStep(artifact, step))?.title} />)}
        {runOutputs.length === 0 && values.length > 0 && <ValueList values={values.map(value => [value.key, value.value])} secrets={secrets} className="mt-1" />}
      </div>
    </NowPanel>
  }
  // Resume goes on with a step whose agent Legato stopped with, on the
  // same attempt; Retry starts it afresh.
  const resumable = (step: WorkflowStepRun) => attempts.some(attempt => ofStep(attempt, step) && attempt.resumable)
  const resumeAction = (step: WorkflowStepRun) => resumable(step) && <WorkflowStepResumeAction onResume={async () => { await resumeWorkflowStep(baseUrl, runId, stepPathOf(step)); await refresh() }} />
  // The step the run is at: in a loop, its pass's step.
  const at = (status: string) => steps.find(step => stepPathOf(step) === run.current_step_id && step.status === status) ?? steps.find(step => step.status === status)
  if (run.status === "suspended") {
    const suspended = at("suspended")
    return <NowPanel tone="idle" heading={suspended ? `${suspended.title} is suspended` : "The run is suspended"}
      actions={suspended && <>{resumeAction(suspended)}<WorkflowStepRetryAction step={suspended} onRetry={async force => { await retryWorkflowStep(baseUrl, runId, stepPathOf(suspended), force); await refresh() }} /></>}>
      <p>Legato stopped while its agent was working. Resume goes on with the agent's conversation, in the same folder; Retry starts the step afresh.</p>
    </NowPanel>
  }
  if (run.status === "failed") {
    const failed = at("failed")
    return <NowPanel tone="error" heading={failed ? `${failed.title} failed` : "The run failed"}
      actions={failed && <>{resumeAction(failed)}<WorkflowStepRetryAction step={failed} onRetry={async force => { await retryWorkflowStep(baseUrl, runId, stepPathOf(failed), force); await refresh() }} /></>}>
      {(failed?.error || run.error) && <p className="mt-1 whitespace-pre-wrap break-words rounded-lg bg-[var(--theme-surface,rgba(0,0,0,0.25))] px-2.5 py-2 font-lw-mono text-xs text-lw-danger">{redact(failed?.error || run.error || "", secrets)}</p>}
    </NowPanel>
  }
  if (run.status === "cancelled" || run.status === "stopped") return null
  // A step paused after a failure (on_failure: pause): its error, the link
  // it carries (such as the run of the loop that holds the PR), and Retry,
  // Skip or Stop.
  const paused = at("paused")
  if (paused) return <PausedNow step={paused} runId={runId} refresh={refresh} secrets={secrets} />
  const current = steps.find(step => stepPathOf(step) === run.current_step_id) ?? currentStep(steps.filter(isTopStep))
  if (!current) return null
  if (current.status === "waiting" && current.executor_type === "gate") {
    return <GateNow view={view} step={current} runId={runId} refresh={refresh} />
  }
  if (current.status === "waiting" && current.executor_type === "workflow.run" && current.child_run_id) {
    const child = view.details?.[stepPathOf(current)] as WorkflowCallDetails | undefined
    return <NowPanel tone={state.tone} heading={`${current.title} waits on its child run`}
      actions={<Button asChild variant="primary"><Link to={runLink(current.child_run_id)}>Open child run</Link></Button>}>
      {child && <p>{child.title || child.workflow} · {child.status.replaceAll("_", " ")}</p>}
    </NowPanel>
  }
  const attempt = [...attempts].reverse().find(item => ofStep(item, current) && item.status === "waiting_agent")
  if (attempt) {
    const open = attempt.agent_session_id ? <>
      <Button asChild variant="primary"><Link to={agentLink(attemptAgent(attempt), runId)}>Open agent</Link></Button>
      <Button asChild variant="outline"><Link to={graphLink(attemptAgent(attempt), runId)}>Show on agents graph</Link></Button>
    </> : null
    if (attempt.needs_attention) {
      const copy = attentionCopy(attempt.attention_reason ?? "", current.title)
      return <NowPanel tone="needs" heading={copy.heading}
        actions={<>{open}<Button variant="outline" onClick={async () => { await promptWorkflowStep(baseUrl, runId, stepPathOf(current)); await refresh() }}>Prompt to continue</Button></>}>
        <p>{copy.sentence}</p>
      </NowPanel>
    }
    return <NowPanel tone={state.tone} heading={current.title} actions={open}>
      <p>{attempt.agent_kind || "The agent"} is working · {elapsed(attempt.started_at, now)}{attempt.working_dir && <> · in <span className="break-all font-lw-mono text-xs">{attempt.working_dir}</span></>}</p>
    </NowPanel>
  }
  if (current.status === "waiting" && reviewLoopSteps.has(current.executor_type)) {
    return <ReviewLoopNow view={view} step={current} runId={runId} refresh={refresh} now={now} />
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
          <ManualWorkflowStepAction step={current} label="End now" onTrigger={async force => { await triggerWorkflowStep(baseUrl, runId, stepPathOf(current), force); await refresh() }} />
        </>} />
    }
    return <NowPanel tone="needs" heading={`${current.title} waits for you`}
      actions={<><ManualWorkflowStepAction step={current} onTrigger={async force => { await triggerWorkflowStep(baseUrl, runId, stepPathOf(current), force); await refresh() }} />{resume}</>}>
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
  const { act, busy, error } = useStepAction(runId, stepPathOf(step), refresh)
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
  const { act, busy, error } = useStepAction(runId, stepPathOf(step), refresh)
  const gate = view.details?.[stepPathOf(step)] as GateDetails | undefined
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
  const wait = view.details?.[stepPathOf(step)] as MergeWaitDetails | undefined
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

// The step types whose waiting step is a review loop: the inline step of an
// old copy, and the steps of review-loop that wait on their loop (LEG-594).
const reviewLoopSteps = new Set(["github.pr.review_passes", "github.pr.review.pass", "github.pr.review.wait", "github.pr.review.confirm"])

const actionLabels = reviewLoopActionLabels

// ReviewLoopNow is a run's review loop while its step waits on it: where it
// stands, its layers and the steward, the reviews posted, and the actions
// the server says the step takes now (Run another pass, Message the author,
// Stop watching, Watch again, End). Details from a server that names no
// actions offer Run another pass while the loop waits for its author or is
// stuck.
function ReviewLoopNow({ view, step, runId, refresh, now }: { view: WorkflowRunView; step: WorkflowStepRun; runId: string; refresh: () => Promise<void>; now: number }) {
  const { baseUrl } = useServer()
  const [error, setError] = useState("")
  const [busy, setBusy] = useState(false)
  const [writing, setWriting] = useState(false)
  const [message, setMessage] = useState("")
  const { confirm, dialog } = useConfirm()
  const path = stepPathOf(step)
  const details = view.details?.[path] as ReviewRunDetails | undefined
  // End asks first, in the words Attention's End uses.
  async function end() {
    const ok = await confirm(endConfirmation(Boolean(details?.steward)))
    if (ok) await act("end")
  }
  async function act(action: ReviewLoopAction, text?: string) {
    setError("")
    setBusy(true)
    try {
      // The old inline step's another pass has a route of its own.
      if (action === "another_pass" && step.executor_type === "github.pr.review_passes") await anotherReviewPass(baseUrl, runId, path)
      else await workflowStepAction(baseUrl, runId, path, action, text)
      if (action === "message") { setMessage(""); setWriting(false) }
      await refresh()
    }
    catch (c) { setError(c instanceof Error ? c.message : String(c)) }
    finally { setBusy(false) }
  }
  const stuck = details?.phase === "stuck"
  const waiting = !details || details.phase === "waiting_for_author"
  const actions: ReviewLoopAction[] = details?.actions ?? (waiting || stuck ? ["another_pass"] : [])
  const needs = details?.needs_you || details?.steward?.held || stuck
  return <NowPanel tone={needs ? "needs" : "live"} heading={details?.summary || step.title}
    actions={actions.length > 0 && <>{actions.map((action, i) => <Button key={action} variant={action === "end" ? "ghost" : i === 0 ? "primary" : "outline"} disabled={busy}
      onClick={() => { if (action === "message") setWriting(!writing); else if (action === "end") void end(); else void act(action) }}>{actionLabels[action] ?? action}</Button>)}</>}>
    {waiting && <p>The next pass starts when the author pushes, or replies to a blocking finding.</p>}
    {details?.loop_id && <ReviewLoopFacts details={details} now={now} />}
    {details?.reviews && details.reviews.length > 0 && <ul aria-label="Reviews posted" className="mt-1 space-y-0.5">
      {details.reviews.map(review => <li key={`${review.pass}-${review.pr}`}><a className="text-lw-accent underline" href={review.url} target="_blank" rel="noreferrer">Pass {review.pass} review of #{review.pr}</a></li>)}
    </ul>}
    {writing && <div className="mt-2 flex flex-col gap-2">
      <textarea aria-label="Message to the author" value={message} onChange={event => setMessage(event.target.value)} rows={3} className="w-full rounded-md border border-lw-line bg-lw-raised p-2 text-sm text-lw-text" />
      <div className="flex gap-2">
        <Button size="sm" disabled={busy || message.trim() === ""} onClick={() => { void act("message", message) }}>Send</Button>
        <Button variant="ghost" size="sm" disabled={busy} onClick={() => setWriting(false)}>Cancel</Button>
      </div>
    </div>}
    {error && <p role="alert" className="mt-1 text-lw-error">{error}</p>}
    {dialog}
  </NowPanel>
}

// AuthorLeftRunning says a review loop's author agent still runs after the
// loop went stuck and was ended, was ended, cancelled, or its run failed
// (spec LEG-559 story 23): Legato stops it only after a merge, a close or
// idleness. Stop stops it.
function AuthorLeftRunning({ view }: { view: WorkflowRunView }) {
  const { baseUrl } = useServer()
  const [state, setState] = useState<"" | "busy" | "stopped">("")
  const [error, setError] = useState("")
  const loop = Object.values(view.details ?? {}).find(details => (details as ReviewRunDetails | null)?.author_left_running) as ReviewRunDetails | undefined
  if (!loop?.author_task) return null
  const task = loop.author_task
  async function stop() {
    setError("")
    setState("busy")
    try { await stopTaskAgent(baseUrl, task); setState("stopped") }
    catch (c) { setError(c instanceof Error ? c.message : String(c)); setState("") }
  }
  return <section aria-label="Author agent" className="mt-3 rounded-xl border border-lw-line bg-lw-surface p-4 text-[13px] text-lw-text-2">
    <p><span className="font-medium text-lw-text">{task}'s agent was left running.</span> The review loop ended {loop.outcome === "ended" ? "by End" : loop.outcome === "stuck" ? "stuck" : loop.outcome === "cancelled" ? "cancelled" : "with its run failed"}, so Legato did not stop the author.</p>
    <div className="mt-3 flex flex-wrap items-center gap-2">
      <Button asChild variant="outline" size="sm"><Link to={agentLink(task, view.run.id)}>Open agent</Link></Button>
      {state === "stopped" ? <span role="status">Stopped.</span> : <Button variant="ghost" size="sm" disabled={state === "busy"} onClick={() => { void stop() }}>Stop</Button>}
      {error && <span role="alert" className="text-lw-error">{error}</span>}
    </div>
  </section>
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

// StepRows lists steps as the run has them: under a loop step, each pass
// with its steps (LEG-490).
function StepRows({ nodes, keyPrefix, run, attempts, artifacts, now, secrets }: { nodes: RunStepNode<WorkflowStepRun>[]; keyPrefix: string; run: WorkflowRunView["run"]; attempts: WorkflowAttempt[]; artifacts: WorkflowArtifact[]; now: number; secrets: string[] }) {
  return <>{nodes.map(({ step, passes }) => {
    const path = stepPathOf(step)
    return <StepRow key={`${keyPrefix}:${path}`} step={step} run={run} attempts={attempts.filter(attempt => stepPathOf(attempt) === path)} artifacts={artifacts.filter(artifact => stepPathOf(artifact) === path)} now={now} secrets={secrets}>
      {passes.length > 0 && <div className="space-y-2 px-3.5 pb-3 pl-11">
        {passes.map(pass => <section key={pass.pass}>
          <h3 className="mb-1 text-xs text-lw-text-2">Pass {pass.pass + 1}</h3>
          <ul aria-label={`Pass ${pass.pass + 1}`} className="overflow-hidden rounded-lg border border-lw-line">
            <StepRows nodes={pass.steps} keyPrefix={keyPrefix} run={run} attempts={attempts} artifacts={artifacts} now={now} secrets={secrets} />
          </ul>
        </section>)}
      </div>}
    </StepRow>
  })}</>
}

function StepRow({ step, run, attempts, artifacts, now, secrets, children }: { step: WorkflowStepRun; run: WorkflowRunView["run"]; attempts: WorkflowAttempt[]; artifacts: WorkflowArtifact[]; now: number; secrets: string[]; children?: ReactNode }) {
  const [open, setOpen] = useState(step.status === "failed" || !!step.output_warning)
  const mark = stepMark(asStepLike(step), run)
  const inputs = objectEntries(step.resolved_inputs_json)
  const outputs = objectEntries(step.outputs_json).filter(([key]) => !artifacts.some(artifact => artifact.output_key === key))
  const agentAttempts = attempts.filter(attempt => attempt.agent_kind || attempt.working_dir)
  return <li className="border-t border-lw-line first:border-t-0">
    <button aria-expanded={open} onClick={() => setOpen(!open)} className="grid min-h-11 w-full grid-cols-[1.25rem_minmax(0,1fr)_auto_0.75rem] items-center gap-3 px-3.5 py-2.5 text-left hover:bg-lw-raised sm:grid-cols-[1.25rem_minmax(0,1fr)_auto_auto_0.75rem]">
      <span aria-hidden="true" className={cn("text-center text-[13px]", mark.tone ? toneText[mark.tone] : "text-lw-faint")}>{mark.glyph}</span>
      <span className={cn("min-w-0 font-medium", step.status === "pending" && "text-lw-text-2")}>{step.title || step.step_id}</span>
      <span className="hidden font-lw-mono text-xs text-lw-faint sm:inline">{step.executor_type || "loop"}</span>
      <span className="min-w-12 text-right font-lw-mono text-xs text-lw-faint">{stepDuration(step, now)}</span>
      <span aria-hidden="true" className="text-lw-faint">{open ? <ChevronDown size={12} /> : <ChevronRight size={12} />}</span>
    </button>
    {open && <div className="space-y-3 px-3.5 pb-3.5 pl-11 text-[13px]">
      <p className="font-lw-mono text-xs text-lw-faint sm:hidden">{step.executor_type || "loop"}</p>
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
    {children}
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
  const step = steps.find(item => stepPathOf(item) === run.step)
  if (!step) return null
  return <div className="mt-1.5 flex flex-wrap items-center gap-2" onClick={event => event.stopPropagation()}>
    {run.control === "retry"
      ? <WorkflowStepRetryAction step={step} onRetry={async force => { await retryWorkflowStep(baseUrl, runId, stepPathOf(step), force); await refresh() }} />
      : <ManualWorkflowStepAction step={step} label={run.label} onTrigger={async force => { await triggerWorkflowStep(baseUrl, runId, stepPathOf(step), force); await refresh() }} />}
  </div>
}

// NeedsYouList is everything the run waits on the operator for, drawn with
// the needs-you rail's own approval card and row, above the graph. A click
// on a row shows its node.
function NeedsYouList({ rows, steps, runId, refresh, onFocus, card }: { rows: RunNeedsYouRow[]; steps: WorkflowStepRun[]; runId: string; refresh: () => Promise<void>; onFocus: (node: string | null) => void; card?: { folded: boolean; onFold: () => void } }) {
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
      // A review-loop step's actions all go through the step's action route.
      if (row?.kind === "item" && row.run?.stepActions) { void act(item, () => workflowStepAction(baseUrl, runId, step, actionId, text)); return }
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
  const list = <div className={cn("grid gap-2", !card && "md:grid-cols-2")}>
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
  const errorLine = error && <p role="alert" className="mb-2 text-[13px] text-lw-error">{error}</p>
  if (card) return <DockCard label="Needs you" folded={card.folded} onFold={card.onFold}
    heading={<h2 className={cn(cardTitle, "text-lw-needs")}>Needs you<span className="font-medium text-lw-faint">{shown.length}</span></h2>}>
    <div className="px-3 pb-3">{errorLine}{list}</div>
  </DockCard>
  return <section aria-label="Needs you" className="mt-5">
    <h2 className="mb-2 flex items-baseline gap-2 text-xs font-semibold uppercase tracking-wide text-lw-needs">Needs you<span className="font-medium text-lw-faint">{shown.length}</span></h2>
    {errorLine}
    {list}
  </section>
}

function clock(time?: string): string {
  return time ? time.replace("T", " ").replace(/(:\d\d)(\.\d+)?Z?$/, "$1") : ""
}

// RunStepPanel is one step of a run, beside its graph: what went in and
// came out, every attempt, its artifacts, a review loop's passes, and its
// YAML from the run's own copy, with the run's secrets hidden.
function RunStepPanel({ view, node, runId, secrets, state, onClose, now }: { view: WorkflowRunView; node: GraphNode; runId: string; secrets: string[]; state?: string; onClose?: () => void; now: number }) {
  const graphStep = node.step!
  // A loop's body's step is shown at its newest pass (graph version 2).
  const instances = instancesOf(view, graphStep.path || graphStep.id)
  const step = instances[instances.length - 1]
  const path = step ? stepPathOf(step) : graphStep.path || graphStep.id
  const attempts = (view.attempts ?? []).filter(attempt => stepPathOf(attempt) === path)
  const artifacts = (view.artifacts ?? []).filter(artifact => stepPathOf(artifact) === path)
  const inputs = step ? objectEntries(step.resolved_inputs_json) : []
  const outputs = step ? objectEntries(step.outputs_json).filter(([key]) => !artifacts.some(artifact => artifact.output_key === key)) : []
  const details = view.details?.[path] as ReviewRunDetails | undefined
  const yaml = redact(yamlText(redactSecrets(graphStep.definition, new Set(secrets))), secrets)
  return <aside aria-label={graphStep.title} className={cn("flex min-w-0 flex-col gap-4 rounded-xl border border-lw-line bg-lw-surface p-4 text-[13px]", onClose && floatingPanel)}>
    <div>
      <div className="flex items-start gap-2">
        <div className="min-w-0 flex-1">
          <p className="text-[11px] font-medium uppercase tracking-wide text-lw-text-2">Step · {stepTypeLabel(graphStep.type)}{state && <> · {state}</>}</p>
          <h2 className="mt-0.5 text-[15px] font-semibold">{graphStep.title}</h2>
        </div>
        <PanelClose onClose={onClose} />
      </div>
      {step?.pass != null && <p className="text-lw-text-2">Pass {step.pass + 1}{instances.length > 1 && <> of the {instances.length} it ran in</>}</p>}
      {step?.error && <p className="mt-1 whitespace-pre-wrap break-words text-lw-error">{redact(step.error, secrets)}</p>}
      {step?.output_warning && <p className="mt-1 text-lw-needs">{step.output_warning}</p>}
      {step?.child_run_id && <p className="mt-1 text-lw-text-2">Child run <Link className="font-lw-mono text-xs text-lw-link" to={runLink(step.child_run_id)}>{step.child_run_id}</Link></p>}
    </div>
    {inputs.length > 0 && <div><h3 className="mb-1 text-xs text-lw-text-2">Inputs</h3><ValueList values={inputs} secrets={secrets} clamp={Boolean(onClose)} /></div>}
    {outputs.length > 0 && <div><h3 className="mb-1 text-xs text-lw-text-2">Outputs</h3><ValueList values={outputs} secrets={secrets} clamp={Boolean(onClose)} /></div>}
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
      {details.loop_id ? <div className="text-lw-text-2"><ReviewLoopFacts details={details} now={now} /></div> : <p className="text-lw-text-2">Pass {details.pass} · {details.blocking_open} blocking</p>}
      {details.reviews && details.reviews.length > 0 && <ul aria-label="Reviews posted" className="mt-1 space-y-0.5">
        {details.reviews.map(review => <li key={`${review.pass}-${review.pr}`}><a className="text-lw-link underline" href={review.url} target="_blank" rel="noreferrer">Pass {review.pass} review of #{review.pr}</a></li>)}
      </ul>}
    </div>}
    <StepYaml yaml={yaml} source={view.run.definition_source} />
  </aside>
}

function RunTriggerPanel({ node, started, onClose }: { node: GraphNode; started: boolean; onClose?: () => void }) {
  const trigger = node.trigger!
  const summary = trigger.summary || triggerName(trigger.kind)
  return <aside aria-label={summary} className={cn("flex flex-col gap-2 rounded-xl border border-lw-line bg-lw-surface p-4 text-[13px]", onClose && floatingPanel)}>
    <div className="flex items-start gap-2">
      <div className="min-w-0 flex-1">
        <p className="text-[11px] font-medium uppercase tracking-wide text-lw-text-2">Trigger · {triggerName(trigger.kind)}</p>
        <h2 className="text-[15px] font-semibold">{summary}</h2>
      </div>
      <PanelClose onClose={onClose} />
    </div>
    <p className="text-lw-text-2">{started ? "This trigger started this run." : "This run was not started by this trigger."}</p>
    {trigger.definition && <pre className="overflow-x-auto rounded-lg border border-lw-line bg-lw-ground p-3 font-lw-mono text-xs">{yamlText(trigger.definition)}</pre>}
  </aside>
}

// StepOutline is the run's steps as a list over its graph: each one's
// state, title and time; choosing one selects its node and centres it.
function StepOutline({ steps, run, now, selected, onPick }: { steps: WorkflowStepRun[]; run: WorkflowRunView["run"]; now: number; selected: string | null; onPick: (node: string) => void }) {
  const rows: ReactNode[] = []
  const walk = (nodes: RunStepNode<WorkflowStepRun>[], depth: number) => {
    for (const { step, passes } of nodes) {
      const node = `step:${templatePath(stepPathOf(step))}`
      const mark = stepMark(asStepLike(step), run)
      rows.push(<li key={stepPathOf(step)}>
        <button type="button" onClick={() => onPick(node)} style={{ paddingLeft: `${0.5 + depth}rem` }} className={cn("grid min-h-8 w-full grid-cols-[1rem_minmax(0,1fr)_auto] items-center gap-2 rounded-md py-1 pr-2 text-left text-[13px] hover:bg-lw-raised", node === selected && "bg-lw-selected", step.status === "pending" && "text-lw-text-2")}>
          <span aria-hidden="true" className={cn("text-center", mark.tone ? toneText[mark.tone] : "text-lw-faint")}>{mark.glyph}</span>
          <span className="truncate">{step.title || step.step_id}</span>
          <span className="font-lw-mono text-[11px] text-lw-faint">{step.status === "skipped" ? "skipped" : stepDuration(step, now)}</span>
        </button>
      </li>)
      for (const pass of passes) {
        rows.push(<li key={`${stepPathOf(step)}/${pass.pass}`} style={{ paddingLeft: `${1.5 + depth}rem` }} className="pt-1 text-xs text-lw-text-2">Pass {pass.pass + 1}</li>)
        walk(pass.steps, depth + 1)
      }
    }
  }
  walk(runStepTree(steps), 0)
  return <ul aria-label="Steps outline" className="max-h-[50vh] overflow-y-auto px-1.5 pb-2">{rows}</ul>
}

export function WorkflowRunPage() {
  const { runId = "" } = useParams()
  const { data, loading, refreshing, error, refresh } = useWorkflowRun(runId)
  const { baseUrl } = useServer()
  const now = useNow(15_000)
  // On a wide screen the run's graph is the page, with the run's details as
  // cards over it (LEG-651). At phone width the step list stays, with a
  // Graph toggle to the graph, which then runs top to bottom with the run's
  // cards in a sheet and a node's details in another.
  const narrow = useMediaQuery("(max-width: 767px)")
  const large = useLargeScreen()
  const [graphOn, setGraphOn] = useState(false)
  const [selected, setSelected] = useState<string | null>(null)
  const [centerOn, setCenterOn] = useState<{ id: string; n: number } | null>(null)
  // Inputs is open by default on a large screen only (LEG-651); Steps
  // starts folded, as the graph already shows them.
  const folds = useCardFolds("legato:run-page:folds", { inputs: !large, steps: true })
  const { graph, needsYou, extras } = useRunGraphExtras(runId, data, now)
  const canvas = Boolean(graph) && (!narrow || graphOn)
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
  // The bar and the position count the run's own steps; a loop's passes
  // are under it in the list.
  const topSteps = steps.filter(isTopStep)
  const stepLikes = topSteps.map(asStepLike)
  const position = stepPosition(run, stepLikes)
  const workflow = definitionTitle(data)
  const subject = run.related_task_id ? "" : runSubject(run)
  const live = run.status === "running" || run.status === "waiting" || run.status === "suspended"
  const since = (time: string) => { const value = ago(time, now); return value === "now" ? "just now" : `${value} ago` }
  const when = run.completed_at ? `${run.status === "succeeded" ? "finished" : "ended"} ${since(run.completed_at)}` : run.created_at ? `started ${since(run.created_at)}` : ""
  let where = ""
  if (run.status === "succeeded") where = `${topSteps.length} ${topSteps.length === 1 ? "step" : "steps"}${run.started_at && run.completed_at ? ` · took ${elapsed(run.started_at, run.completed_at)}` : ""}`
  else if (position) where = `${run.status === "failed" ? "at step" : "Step"} ${position.number} of ${position.total} · ${position.title}`
  const inputs = objectEntries(run.inputs_json).filter(([, value]) => value !== "" && value !== null)
  const secrets = secretValues(data)
  const selectedNode = graph?.nodes.find(node => node.id === selected)
  const closePanel = () => setSelected(null)
  const close = canvas ? closePanel : undefined
  const panel = selectedNode?.kind === "step"
    ? <RunStepPanel view={data} node={selectedNode} runId={runId} secrets={secrets} now={now} state={extras?.[selectedNode.id]?.state && runStateWords[extras[selectedNode.id].state!]} onClose={narrow ? undefined : close} />
    : selectedNode?.kind === "trigger" ? <RunTriggerPanel node={selectedNode} started={extras?.[selectedNode.id]?.trigger === "started"} onClose={narrow ? undefined : close} /> : null
  const subtitle = <>
    {workflow && workflow !== run.workflow_title && <>{workflow} · </>}
    {run.parent_run_id && <span>called by <Link to={runLink(run.parent_run_id)} className="text-lw-link">{run.parent_run_id}</Link>{run.parent_step_path && <> at <span className="font-lw-mono text-xs">{run.parent_step_path}</span></>} · </span>}
    {run.related_task_id && <>for <Link to={`/board?highlight_task=${encodeURIComponent(run.related_task_id)}`} className="text-lw-link">{run.related_task_id}</Link>{run.related_task_title && ` ${run.related_task_title}`} · </>}
    {subject && <>{subject}{when && " · "}</>}
    {when}
  </>
  // A failed run keeps what it holds for Retry; Cancel gives it up.
  const cancel = (live || run.status === "failed") && <Button variant="ghost" size="sm" className="shrink-0 self-start sm:self-center" onClick={async () => { await cancelWorkflow(baseUrl, runId); await refresh() }}>Cancel run</Button>
  const refreshFailed = error && <div role="alert" className="rounded-lg border border-lw-line bg-lw-surface p-3 text-sm shadow-[var(--shadow-lw-panel)]">
    <p className="text-lw-needs">Showing saved run. Refresh failed: {error}</p>
    <Button variant="outline" size="sm" className="mt-2" disabled={refreshing} onClick={() => { void refresh() }}>Retry</Button>
  </div>
  const changed = (data.definition_changed || data.definition_missing) && <p role="note" className="rounded-lg border border-lw-line bg-lw-surface px-3 py-2 text-[13px] text-lw-text-2">
    {data.definition_missing ? "This workflow's file is gone" : "This workflow has changed since this run"}<span className="text-lw-faint"> · the graph is the run's own copy</span>
  </p>
  const ended = (run.status === "cancelled" || run.status === "stopped") && run.error && <p className="text-[13px] text-lw-text-2">{redact(run.error, secrets)}</p>
  const nowPanel = <Now key={`${baseUrl}:${runId}`} view={data} runId={runId} refresh={refresh} now={now} secrets={secrets} />

  if (canvas && graph) {
    const current = run.current_step_id ? `step:${templatePath(run.current_step_id)}` : null
    const focusId = current && graph.nodes.some(node => node.id === current) ? current : null
    const nowFold = { folded: folds.folded("now"), toggle: () => folds.toggle("now") }
    const cards = <>
      <NowFold.Provider value={nowFold}>{nowPanel}</NowFold.Provider>
      <AuthorLeftRunning key={`author:${baseUrl}:${runId}`} view={data} />
      <NeedsYouList rows={needsYou.rows} steps={steps} runId={runId} refresh={refresh} onFocus={focusNode} card={{ folded: folds.folded("needs"), onFold: () => folds.toggle("needs") }} />
      {inputs.length > 0 && <DockCard label="Inputs" folded={folds.folded("inputs")} onFold={() => folds.toggle("inputs")} heading={<h2 className={cardTitle}>Inputs<span className="font-medium text-lw-faint">{inputs.length}</span></h2>}>
        <ValueList secrets={secrets} values={inputs} clamp className="px-4 pb-3" />
      </DockCard>}
      {!narrow && <DockCard label="Steps" folded={folds.folded("steps")} onFold={() => folds.toggle("steps")} heading={<h2 className={cardTitle}>Steps<span className="font-medium text-lw-faint">{steps.length}</span></h2>}>
        <StepOutline steps={steps} run={run} now={now} selected={selected} onPick={focusNode} />
      </DockCard>}
    </>
    const notices = (refreshFailed || changed || ended) && <>{refreshFailed}{changed}{ended && <div className="rounded-lg border border-lw-line bg-lw-surface px-3 py-2">{ended}</div>}</>
    return <main className="flex min-h-0 flex-1 flex-col overflow-hidden font-lw text-lw-text">
      <CanvasHeader back={{ to: "/workflows?tab=runs", label: "Runs" }} title={run.workflow_title}
        subtitle={<>{subtitle} · <span className="font-lw-mono text-[11px] text-lw-faint">{run.id} · <span title={run.definition_source}>{sourceLabel(run.definition_source ?? "")}</span></span></>}>
        {/* The run's state keeps its words: on a narrow window it wraps under
            the title rather than being cut. */}
        <div className="flex min-w-0 max-w-full shrink-0 items-center gap-3 text-[13px] text-lw-text-2">
          <StatePill tone={state.tone} label={state.label} />
          {where && <span className="min-w-0 max-w-[18rem] truncate" title={where}>{where}</span>}
          {topSteps.length > 0 && <StepBar segments={stepSegments(run, stepLikes)} label={where || state.label} className="w-32 shrink-0 lg:w-44" />}
        </div>
        {narrow && <div role="group" aria-label="View" className="flex rounded-lg border border-lw-line bg-lw-ground p-0.5">
          <button type="button" aria-pressed={false} onClick={() => setGraphOn(false)} className="h-8 rounded-md px-3 text-xs text-lw-text-2">List</button>
          <button type="button" aria-pressed={true} className="h-8 rounded-md bg-lw-selected px-3 text-xs font-medium text-lw-text">Graph</button>
        </div>}
        {cancel}
      </CanvasHeader>
      <WorkflowCanvas label="Run graph" narrow={narrow} focusId={focusId} cards={cards} notices={notices}
        graph={{ graph, extras, run: true, selectedId: selected, onSelect: setSelected, centerOn, direction: narrow ? "TB" : "LR" }}
        panel={panel} panelTitle={selectedNode?.step?.title ?? selectedNode?.trigger?.summary ?? ""} selected={selected !== null} onClosePanel={closePanel}
        hiddenPill={<><span aria-hidden="true" className={toneText[state.tone]}>{toneGlyph[state.tone]}</span>{state.label}</>} />
    </main>
  }

  return <main className="min-h-0 flex-1 overflow-y-auto px-4 py-4 font-lw text-lw-text sm:px-6 md:py-6">
    <div className="mx-auto max-w-3xl">
      <Link to="/workflows?tab=runs" className="text-sm text-lw-link">← Runs</Link>
      <div className="mt-2 flex flex-col gap-2 sm:flex-row sm:items-start sm:justify-between">
        <div className="min-w-0">
          <h1 className="break-words text-xl font-semibold">{run.workflow_title}</h1>
          <p className="mt-1 text-[13px] text-lw-text-2">{subtitle}</p>
        </div>
        {cancel}
      </div>
      <div className="mt-4 flex flex-wrap items-center gap-3 text-[13px] text-lw-text-2"><StatePill tone={state.tone} label={state.label} />{where && <span>{where}</span>}</div>
      {topSteps.length > 0 && <StepBar segments={stepSegments(run, stepLikes)} label={where || state.label} className="mt-2" />}
      {ended && <div className="mt-3">{ended}</div>}
      {refreshFailed && <div className="mt-4">{refreshFailed}</div>}
      {changed && <div className="mt-4">{changed}</div>}
      {nowPanel}
      <AuthorLeftRunning key={`author:${baseUrl}:${runId}`} view={data} />
      {graph && <NeedsYouList rows={needsYou.rows} steps={steps} runId={runId} refresh={refresh} onFocus={focusNode} />}
      {graph && <div className="mt-7 flex justify-end">
        <Button variant="outline" size="sm" aria-pressed={graphOn} onClick={() => setGraphOn(true)}>Graph</Button>
      </div>}
      <h2 className="mb-2 mt-7 text-xs font-semibold uppercase tracking-wide text-lw-text-2">Steps</h2>
      <ul aria-label="Steps" className="overflow-hidden rounded-xl border border-lw-line bg-lw-surface">
        <StepRows nodes={runStepTree(steps)} keyPrefix={`${baseUrl}:${runId}`} run={run} attempts={attempts} artifacts={artifacts} now={now} secrets={secrets} />
      </ul>
      {inputs.length > 0 && <>
        <h2 className="mb-2 mt-7 text-xs font-semibold uppercase tracking-wide text-lw-text-2">Inputs</h2>
        <ValueList secrets={secrets} values={inputs} />
      </>}
      <p className="mt-7 break-all font-lw-mono text-xs text-lw-faint">{run.id} · <span title={run.definition_source}>{sourceLabel(run.definition_source ?? "")}</span></p>
    </div>
  </main>
}
