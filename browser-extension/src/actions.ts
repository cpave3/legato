import type { ExtensionConfig } from "./config"
import type { WorkflowDiscovery } from "./legato"

export type GitHubPullRequestContext = { repository: string; pullRequestNumber: number }
/** What a pull request action is for: a review run, or a conversation about the change. */
export type PullRequestPurpose = "review" | "discuss"
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
  const workflowName = purpose === "discuss" ? config.discussWorkflowName : config.workflowName
  const inputNames = [config.repoInput, config.pullRequestInput, config.primaryDirInput]
  if (new Set(inputNames).size !== inputNames.length) throw new Error("Configured workflow input names must be distinct")

  const workflow = discovery.definitions?.find((item) => item.name === workflowName)
  if (!workflow) throw new Error(`Workflow "${workflowName}" was not found`)
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
