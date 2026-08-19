export type WorkflowInput = { type: string; required?: boolean; default?: unknown }
export type WorkflowDefinition = { name: string; inputs?: Record<string, WorkflowInput> }
export type RepositoryPreset = { name: string; repo: string; primary_dir: string }
export type WorkflowDiscovery = {
  definitions: WorkflowDefinition[] | null
  errors: Array<{ path: string; error: string }> | null
  repositories: RepositoryPreset[] | null
}
export type WorkflowRunView = { run: { id: string } }

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

  private async request<T>(path: string, init: RequestInit = {}): Promise<T> {
    const headers = new Headers(init.headers)
    if (this.token) headers.set("Authorization", `Bearer ${this.token}`)
    const response = await this.fetcher.call(globalThis, `${this.baseUrl}${path}`, { ...init, headers: Object.fromEntries(headers) })
    if (!response.ok) {
      const detail = (await response.text()).trim()
      throw new Error(detail || `Legato request failed (${response.status})`)
    }
    return response.json() as Promise<T>
  }
}
