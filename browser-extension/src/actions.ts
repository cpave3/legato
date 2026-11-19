import type { ExtensionConfig } from "./config"
import type { WorkflowDiscovery } from "./legato"

export type GitHubPullRequestContext = { repository: string; pullRequestNumber: number }
/** What a pull request action is for: a review run, a conversation about the change, or a review loop. */
export type PullRequestPurpose = "review" | "discuss" | "passes"
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
  if (purpose === "passes") return prepareReviewPasses(config, context, discovery)
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

// A review loop needs the repository and the pull request's number only:
// Legato makes its own clone. A repository preset that matches names the
// repository as Legato knows it; without one, the pull request's own is used,
// since a loop can review anyone's pull request.
function prepareReviewPasses(config: ExtensionConfig, context: GitHubPullRequestContext, discovery: WorkflowDiscovery): PreparedWorkflow {
  const workflowName = config.reviewPassesWorkflowName
  if (!discovery.definitions?.some((item) => item.name === workflowName)) throw new Error(`Workflow "${workflowName}" was not found. Add it with: legato workflow add review-pr-passes`)
  return prepareClonedWorkflow(config, context, discovery, workflowName)
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
