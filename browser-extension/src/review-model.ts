import type { GitHubPullRequestContext } from "./actions"
import type { ExtensionConfig } from "./config"
import { LegatoClient, type WorkflowDiscovery, type WorkflowRun, type WorkflowRunView } from "./legato"
import { hasOriginPermission } from "./permissions"
import { loadReviewContext } from "./review-context"
import { buildReviewLookup, summarizeReviewRun, type ReviewRunSummary } from "./reviews"
import { loadConfig } from "./storage"

export type ReviewPanelModel = ReviewRunSummary & {
  context: GitHubPullRequestContext
  runs: WorkflowRun[]
  selectedRunId?: string
  run?: WorkflowRun
  steps?: WorkflowRunView["steps"]
  attempts?: WorkflowRunView["attempts"]
  artifactId?: string
  markdown?: string
  poll: boolean
  runUrl?: string
}

type ReviewModelDependencies = {
  loadConfig: () => Promise<ExtensionConfig>
  hasPermission: (baseUrl: string) => Promise<boolean>
  loadContext: (tabId: number) => Promise<GitHubPullRequestContext | null>
  discover: (config: ExtensionConfig) => Promise<WorkflowDiscovery>
  listRuns: (config: ExtensionConfig, workflow: string, inputs: Record<string, string>) => Promise<WorkflowRun[]>
  getRun: (config: ExtensionConfig, id: string) => Promise<WorkflowRunView>
  getArtifact: (config: ExtensionConfig, id: string) => Promise<string>
}

function runMatchesLookup(run: WorkflowRun, workflowName: string, inputs: Record<string, string>): boolean {
  if (run.workflow_name !== workflowName) return false
  let actual: Record<string, unknown>
  try { actual = JSON.parse(run.inputs_json) as Record<string, unknown> } catch { return false }
  return Object.entries(inputs).every(([key, value]) => String(actual[key]) === value)
}

const defaults: ReviewModelDependencies = {
  loadConfig,
  hasPermission: hasOriginPermission,
  loadContext: loadReviewContext,
  discover: (config) => new LegatoClient(config.baseUrl, config.token).discover(),
  listRuns: (config, workflow, inputs) => new LegatoClient(config.baseUrl, config.token).listWorkflowRuns(workflow, inputs),
  getRun: (config, id) => new LegatoClient(config.baseUrl, config.token).workflowRun(id),
  getArtifact: (config, id) => new LegatoClient(config.baseUrl, config.token).artifactText(id),
}

export async function loadReviewModel(tabId: number, selectedRunId?: string, deps: ReviewModelDependencies = defaults): Promise<ReviewPanelModel> {
  const config = await deps.loadConfig()
  if (!(await deps.hasPermission(config.baseUrl))) throw new Error("Open extension options and grant access to the configured Legato origin")
  const context = await deps.loadContext(tabId)
  if (!context) throw new Error("No GitHub pull request context is available for this tab")
  const lookup = buildReviewLookup(config, context, await deps.discover(config))
  const listedRuns = await deps.listRuns(config, lookup.workflowName, lookup.inputs)
  const runs = listedRuns.filter((run) => runMatchesLookup(run, lookup.workflowName, lookup.inputs))
  if (runs.length === 0) return { context, runs, state: "empty", status: "", poll: false }
  const selected = selectedRunId ? runs.find((run) => run.id === selectedRunId) : runs[0]
  if (!selected) throw new Error("Selected run does not match this pull request")
  const view = await deps.getRun(config, selected.id)
  const summary = summarizeReviewRun(view, config.reviewArtifactKey)
  const historical = selected.id !== runs[0].id
  const model: ReviewPanelModel = {
    ...summary,
    context,
    runs,
    selectedRunId: selected.id,
    run: view.run,
    steps: view.steps,
    attempts: view.attempts,
    poll: !historical && summary.state === "active",
    runUrl: `${config.baseUrl}/workflows/runs/${encodeURIComponent(selected.id)}`,
  }
  if (summary.artifactId) model.markdown = await deps.getArtifact(config, summary.artifactId)
  return model
}
