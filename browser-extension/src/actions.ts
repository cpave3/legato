import type { ExtensionConfig } from "./config"
import type { WorkflowDiscovery } from "./legato"

export type GitHubPullRequestContext = { repository: string; pullRequestNumber: number }
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
): PreparedWorkflow {
  const inputNames = [config.repoInput, config.pullRequestInput, config.primaryDirInput]
  if (new Set(inputNames).size !== inputNames.length) throw new Error("Configured workflow input names must be distinct")

  const workflow = discovery.definitions?.find((item) => item.name === config.workflowName)
  if (!workflow) throw new Error(`Workflow "${config.workflowName}" was not found`)
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
