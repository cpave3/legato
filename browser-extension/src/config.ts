export type ExtensionConfig = {
  baseUrl: string
  token: string
  workflowName: string
  discussWorkflowName: string
  repoInput: string
  pullRequestInput: string
  primaryDirInput: string
  reviewArtifactKey: string
}

export const DEFAULT_CONFIG: ExtensionConfig = {
  baseUrl: "http://localhost:3080",
  token: "",
  workflowName: "review-pr",
  discussWorkflowName: "discuss-pr",
  repoInput: "repo",
  pullRequestInput: "pr_number",
  primaryDirInput: "primary_dir",
  reviewArtifactKey: "review",
}

export function normalizeConfig(config: ExtensionConfig): ExtensionConfig {
  let url: URL
  try {
    url = new URL(config.baseUrl.trim())
  } catch {
    throw new Error("Legato URL must be a valid HTTP or HTTPS origin")
  }
  if ((url.protocol !== "http:" && url.protocol !== "https:") || url.username || url.password) {
    throw new Error("Legato URL must be a valid HTTP or HTTPS origin")
  }
  if (url.pathname !== "/" || url.search || url.hash) {
    throw new Error("Legato URL must contain only an origin")
  }
  const names: Array<[keyof ExtensionConfig, string]> = [
    ["workflowName", "Review workflow name"],
    ["discussWorkflowName", "Discussion workflow name"],
    ["repoInput", "Repository input name"],
    ["pullRequestInput", "Pull request input name"],
    ["primaryDirInput", "Primary directory input name"],
    ["reviewArtifactKey", "Review artifact output key"],
  ]
  const normalized = { ...config, baseUrl: url.origin, token: config.token.trim() }
  for (const [key, label] of names) {
    normalized[key] = normalized[key].trim()
    if (!normalized[key]) throw new Error(`${label} is required`)
  }
  return normalized
}

export function hostPermissionFor(baseUrl: string): string {
  return `${new URL(baseUrl).origin}/*`
}
