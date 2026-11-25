import type { ExtensionConfig } from "./config"
import type { WorkflowDiscovery } from "./legato"

export type GitHubPullRequestContext = { repository: string; pullRequestNumber: number }
/**
 * What a pull request action is for: a review run, a conversation about the
 * change, a review loop on the pull request's whole stack, or one on this
 * pull request only.
 */
export type PullRequestPurpose = "review" | "discuss" | "passes" | "passes-pr"
export type PreparedWorkflow = {
  workflowName: string
  repositoryPresetName: string
  repository: string
  pullRequestNumber: number
  primaryDir: string
  inputs: Record<string, string>
}

export function prepareGitHubWorkflow(
  config: ExtensionConfig,
  context: GitHubPullRequestContext,
  discovery: WorkflowDiscovery,
  purpose: PullRequestPurpose = "review",
): PreparedWorkflow {
  if (purpose === "passes" || purpose === "passes-pr") return prepareReviewPasses(config, context, discovery, purpose === "passes" ? "stack" : "pr")
  const workflowName = purpose === "discuss" ? config.discussWorkflowName : config.workflowName
  const inputNames = [config.repoInput, config.pullRequestInput, config.primaryDirInput]
  if (new Set(inputNames).size !== inputNames.length) throw new Error("Configured workflow input names must be distinct")

  const workflow = discovery.definitions?.find((item) => item.name === workflowName)
  if (!workflow) throw new Error(`Workflow "${workflowName}" was not found`)
  // A workflow that makes its own clone (the bundled review-pr and
  // discuss-pr) declares no primary directory: it takes the repository and
  // the number only, as a review loop does.
  if (!workflow.inputs?.[config.primaryDirInput]) return prepareClonedWorkflow(config, context, discovery, workflowName)
  const expected: Array<[string, string]> = [
    [config.repoInput, "string"],
    [config.pullRequestInput, "integer"],
    [config.primaryDirInput, "string"],
  ]
  for (const [name, type] of expected) {
    const input = workflow.inputs?.[name]
    if (!input) throw new Error(`Workflow input "${name}" was not found`)
    if (input.type !== type) throw new Error(`Workflow input "${name}" must have type ${type}`)
  }
  for (const [name, input] of Object.entries(workflow.inputs ?? {})) {
    if (input.required && input.default === undefined && !inputNames.includes(name)) {
      throw new Error(`Required workflow input "${name}" is not mapped by this action`)
    }
  }

  const repository = context.repository.toLowerCase()
  const presets = (discovery.repositories ?? []).filter((preset) => preset.repo.toLowerCase() === repository)
  if (presets.length === 0) throw new Error(`No Legato repository preset matches ${context.repository}`)
  if (presets.length > 1) throw new Error(`Multiple Legato repository presets match ${context.repository}`)
  const preset = presets[0]
  return {
    workflowName: workflow.name,
    repositoryPresetName: preset.name,
    repository: preset.repo,
    pullRequestNumber: context.pullRequestNumber,
    primaryDir: preset.primary_dir,
    inputs: {
      [config.repoInput]: preset.repo,
      [config.pullRequestInput]: String(context.pullRequestNumber),
      [config.primaryDirInput]: preset.primary_dir,
    },
  }
}

// The standard review loop workflows: Legato's built-in review-loop, and
// the review-pr-passes example an older Legato has instead (and an older
// extension stored as the name).
const REVIEW_LOOP = "review-loop"
const REVIEW_PR_PASSES = "review-pr-passes"

/**
 * The workflow a review loop is started with. A name of the operator's own
 * is used as it is. The standard one is review-loop whenever Legato lists
 * it, even when the options still name review-pr-passes, and
 * review-pr-passes on an older Legato without review-loop.
 */
export function reviewLoopWorkflowName(config: ExtensionConfig, discovery: WorkflowDiscovery): string {
  const configured = config.reviewPassesWorkflowName
  if (configured !== REVIEW_LOOP && configured !== REVIEW_PR_PASSES) return configured
  const listed = (name: string) => discovery.definitions?.some((item) => item.name === name) ?? false
  if (listed(REVIEW_LOOP)) return REVIEW_LOOP
  if (listed(REVIEW_PR_PASSES)) return REVIEW_PR_PASSES
  return configured
}

// A review loop needs the repository and the pull request's number only:
// Legato makes its own clone. A repository preset that matches names the
// repository as Legato knows it; without one, the pull request's own is used,
// since a loop can review anyone's pull request. Its scope (the whole stack,
// or this pull request) is sent only to a workflow that declares the input:
// an older Legato's reviews one pull request and takes none.
function prepareReviewPasses(config: ExtensionConfig, context: GitHubPullRequestContext, discovery: WorkflowDiscovery, scope: "stack" | "pr"): PreparedWorkflow {
  const workflowName = reviewLoopWorkflowName(config, discovery)
  const workflow = discovery.definitions?.find((item) => item.name === workflowName)
  if (!workflow && (workflowName === REVIEW_LOOP || workflowName === REVIEW_PR_PASSES)) {
    // Only an older Legato lacks review-loop, which is built in.
    throw new Error(`Workflow "${workflowName}" was not found. This Legato predates the built-in review-loop: update it, or add the old workflow with: legato workflow add review-pr-passes`)
  }
  if (!workflow) throw new Error(`Workflow "${workflowName}" was not found`)
  const prepared = prepareClonedWorkflow(config, context, discovery, workflowName)
  if (workflow.inputs?.scope?.type === "string") prepared.inputs.scope = scope
  return prepared
}

/** The inputs that name a pull request, which its runs are found by, whatever their scope. */
export function pullRequestInputs(config: ExtensionConfig, prepared: PreparedWorkflow): Record<string, string> {
  return { [config.repoInput]: prepared.inputs[config.repoInput], [config.pullRequestInput]: prepared.inputs[config.pullRequestInput] }
}

// prepareClonedWorkflow is a workflow that takes the repository and the
// pull request's number only, checking just the inputs it declares.
function prepareClonedWorkflow(config: ExtensionConfig, context: GitHubPullRequestContext, discovery: WorkflowDiscovery, workflowName: string): PreparedWorkflow {
  const workflow = discovery.definitions?.find((item) => item.name === workflowName)
  if (!workflow) throw new Error(`Workflow "${workflowName}" was not found`)
  const expected: Array<[string, string]> = [[config.repoInput, "string"], [config.pullRequestInput, "integer"]]
  for (const [name, type] of expected) {
    const input = workflow.inputs?.[name]
    if (!input) throw new Error(`Workflow input "${name}" was not found`)
    if (input.type !== type) throw new Error(`Workflow input "${name}" must have type ${type}`)
  }
  for (const [name, input] of Object.entries(workflow.inputs ?? {})) {
    if (input.required && input.default === undefined && name !== config.repoInput && name !== config.pullRequestInput) {
      throw new Error(`Required workflow input "${name}" is not mapped by this action`)
    }
  }
  const preset = (discovery.repositories ?? []).find((item) => item.repo.toLowerCase() === context.repository.toLowerCase())
  const repository = preset?.repo ?? context.repository
  return {
    workflowName,
    repositoryPresetName: preset?.name ?? "",
    repository,
    pullRequestNumber: context.pullRequestNumber,
    primaryDir: "",
    inputs: { [config.repoInput]: repository, [config.pullRequestInput]: String(context.pullRequestNumber) },
  }
}
