import type { GitHubPullRequestContext } from "./actions"
import type { ExtensionConfig } from "./config"
import type { WorkflowArtifact, WorkflowDiscovery, WorkflowRunView } from "./legato"

export type ReviewLookup = { workflowName: string; inputs: Record<string, string> }
export type ReviewRunSummary = {
  state: "empty" | "ready" | "active" | "failed" | "missing_artifact"
  status: string
  artifactId?: string
  error?: string
}

export function buildReviewLookup(config: ExtensionConfig, context: GitHubPullRequestContext, discovery: WorkflowDiscovery): ReviewLookup {
  const presets = (discovery.repositories ?? []).filter((preset) => preset.repo.toLowerCase() === context.repository.toLowerCase())
  if (presets.length === 0) throw new Error(`No Legato repository preset matches ${context.repository}`)
  if (presets.length > 1) throw new Error(`Multiple Legato repository presets match ${context.repository}`)
  return {
    workflowName: config.workflowName,
    inputs: {
      [config.repoInput]: presets[0].repo,
      [config.pullRequestInput]: String(context.pullRequestNumber),
    },
  }
}

export function selectReviewArtifact(artifacts: WorkflowArtifact[], configuredKey: string): WorkflowArtifact | null {
  const markdown = artifacts.filter((item) => item.media_type === "text/markdown")
  const configured = markdown.find((item) => item.output_key === configuredKey)
  if (configured) return configured
  if (markdown.length === 1) return markdown[0]
  if (markdown.length > 1) throw new Error(`Multiple Markdown artifacts exist; configure the review output key`)
  return null
}

export function summarizeReviewRun(view: WorkflowRunView, artifactKey: string): ReviewRunSummary {
  const artifact = selectReviewArtifact(view.artifacts ?? [], artifactKey)
  if (artifact) return { state: "ready", status: view.run.status, artifactId: artifact.id }
  if (["failed", "cancelled"].includes(view.run.status)) return { state: "failed", status: view.run.status, error: view.run.error }
  if (["succeeded"].includes(view.run.status)) return { state: "missing_artifact", status: view.run.status }
  return { state: "active", status: view.run.status }
}
