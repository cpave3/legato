export type WorkflowInput = { type: string; required?: boolean; default?: unknown }
export type WorkflowDefinition = { name: string; inputs?: Record<string, WorkflowInput> }
export type RepositoryPreset = { name: string; repo: string; primary_dir: string }
export type WorkflowDiscovery = {
  definitions: WorkflowDefinition[] | null
  errors: Array<{ path: string; error: string }> | null
  repositories: RepositoryPreset[] | null
}
export type WorkflowRun = {
  id: string
  workflow_name: string
  workflow_title: string
  inputs_json: string
  status: string
  current_step_id: string
  error?: string
  needs_attention?: boolean
  attention_reason?: string
  created_at: string
  started_at?: string
  completed_at?: string
}
export type WorkflowStepRun = { step_id: string; title: string; status: string; error?: string }
export type WorkflowAttempt = { id: string; step_id: string; status: string; error?: string; needs_attention?: boolean; attention_reason?: string }
export type WorkflowArtifact = {
  id: string; run_id: string; step_id: string; attempt_id: string; output_key: string
  original_path?: string; media_type: string; size_bytes: number; digest: string; created_at: string
}
export type WorkflowRunView = {
  run: WorkflowRun
  steps?: WorkflowStepRun[] | null
  attempts?: WorkflowAttempt[] | null
  artifacts?: WorkflowArtifact[] | null
}
export type ImportedTask = { id: string }

export class LegatoClient {
  constructor(
    private readonly baseUrl: string,
    private readonly token: string,
    private readonly fetcher: typeof fetch = fetch,
  ) {}

  discover(): Promise<WorkflowDiscovery> {
    return this.request<WorkflowDiscovery>("/api/workflows")
  }

  startWorkflow(name: string, inputs: Record<string, string>): Promise<WorkflowRunView> {
    return this.request<WorkflowRunView>(`/api/workflows/${encodeURIComponent(name)}/runs`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ inputs }),
    })
  }

  listWorkflowRuns(workflowName: string, inputs: Record<string, string>, limit = 100): Promise<WorkflowRun[]> {
    const query = new URLSearchParams({ workflow_name: workflowName, limit: String(limit) })
    for (const [key, value] of Object.entries(inputs)) query.set(`input.${key}`, value)
    return this.request<WorkflowRun[]>(`/api/workflow-runs?${query}`)
  }

  workflowRun(id: string): Promise<WorkflowRunView> {
    return this.request<WorkflowRunView>(`/api/workflow-runs/${encodeURIComponent(id)}`)
  }

  artifactText(id: string): Promise<string> {
    return this.requestText(`/api/workflow-artifacts/${encodeURIComponent(id)}`)
  }

  importRemoteTicket(ticketId: string): Promise<ImportedTask> {
    return this.request<ImportedTask>("/api/remote/import", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ ticket_id: ticketId }),
    })
  }

  private async fetch(path: string, init: RequestInit = {}): Promise<Response> {
    const headers = new Headers(init.headers)
    if (this.token) headers.set("Authorization", `Bearer ${this.token}`)
    const response = await this.fetcher.call(globalThis, `${this.baseUrl}${path}`, { ...init, headers: Object.fromEntries(headers) })
    if (!response.ok) {
      const detail = (await response.text()).trim()
      throw new Error(detail || `Legato request failed (${response.status})`)
    }
    return response
  }

  private async request<T>(path: string, init: RequestInit = {}): Promise<T> {
    return this.fetch(path, init).then((response) => response.json() as Promise<T>)
  }

  private async requestText(path: string): Promise<string> {
    return this.fetch(path).then((response) => response.text())
  }
}
