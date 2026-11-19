import type { ScopeBatch, ScopeBatchResult, SentPin } from "./scope-types"
import type { TicketProvider } from "./ticket-providers"

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
  /** The definition the run follows, as JSON. */
  definition_json?: string
  /** The task the run's agents work under. */
  task_id?: string
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

// What to say when the browser could not reach Legato at all. The browser does
// not say why (Legato not running, or a certificate it does not trust look the
// same), so for an HTTPS address the message covers both.
export function unreachableMessage(baseUrl: string): string {
  if (!baseUrl.startsWith("https:")) {
    return `Could not reach Legato at ${baseUrl}. Check that Legato is running and that this address in the extension options is right.`
  }
  return [
    `Could not reach Legato at ${baseUrl}.`,
    "Legato may not be running there, or Chrome does not trust its HTTPS certificate.",
    `To check, open ${baseUrl}/health in a tab. A certificate warning there means Chrome does not trust it.`,
    "",
    "To trust Legato's certificate on this computer:",
    "1. Get Legato's certificate authority: the file ca.pem in Legato's data folder on the computer that runs it (~/.local/share/legato/certs/ca.pem by default), or Settings › TLS Certificate in Legato's web UI.",
    "2. In Chrome, open chrome://certificate-manager, go to Local certificates › Installed by you, and import ca.pem under Trusted certificates.",
    "3. Reload the extension and this page.",
    "",
    "This is needed once per computer. It still holds when Legato renews its certificate.",
  ].join("\n")
}

// The part of a WebSocket the extension uses.
export type LiveSocketLike = {
  send?: (data: string) => void
  close: () => void
  onopen: (() => void) | null
  onmessage: ((event: { data: string }) => void) | null
  onclose: (() => void) | null
}

// Legato's WebSocket, with the token in the address as the web UI sends it.
export function socketUrl(baseUrl: string, token: string): string {
  const url = new URL("/ws", baseUrl)
  url.protocol = url.protocol === "https:" ? "wss:" : "ws:"
  if (token) url.searchParams.set("token", token)
  return url.toString()
}

// Legato's API answers most refusals as {"error": "..."}; the message is what
// the person should read.
function errorMessage(body: string): string {
  if (!body.startsWith("{")) return body
  try {
    const parsed = JSON.parse(body) as { error?: unknown }
    return typeof parsed.error === "string" && parsed.error ? parsed.error : body
  } catch {
    return body
  }
}

// A refusal from Legato, with the status it answered.
export class LegatoRequestError extends Error {
  constructor(message: string, readonly status: number) {
    super(message)
    this.name = "LegatoRequestError"
  }
}

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

  async listWorkflowRuns(workflowName: string, inputs: Record<string, string>, limit = 100): Promise<WorkflowRun[]> {
    const query = new URLSearchParams({ workflow_name: workflowName, limit: String(limit) })
    for (const [key, value] of Object.entries(inputs)) query.set(`input.${key}`, value)
    return (await this.request<WorkflowRun[] | null>(`/api/workflow-runs?${query}`)) ?? []
  }

  workflowRun(id: string): Promise<WorkflowRunView> {
    return this.request<WorkflowRunView>(`/api/workflow-runs/${encodeURIComponent(id)}`)
  }

  artifactText(id: string): Promise<string> {
    return this.requestText(`/api/workflow-artifacts/${encodeURIComponent(id)}`)
  }

  importRemoteTicket(ticketId: string, provider: string): Promise<ImportedTask> {
    return this.request<ImportedTask>("/api/remote/import", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ ticket_id: ticketId, provider }),
    })
  }

  // The connected ticket providers that have a site the extension can add
  // its import button to.
  async listTicketProviders(): Promise<TicketProvider[]> {
    return (await this.request<{ providers: TicketProvider[] | null }>("/api/ticket-providers")).providers ?? []
  }

  // Scope: pins placed on a page of the person's own site.
  sendScopeBatch(batch: ScopeBatch): Promise<ScopeBatchResult> {
    return this.request<ScopeBatchResult>("/api/scope/batches", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(batch),
    })
  }

  async listScopePins(origin: string): Promise<SentPin[]> {
    const query = new URLSearchParams({ origin })
    return (await this.request<{ pins: SentPin[] | null }>(`/api/scope/pins?${query}`)).pins ?? []
  }

  reopenScopePin(id: string, comment: string): Promise<SentPin> {
    return this.request<SentPin>(`/api/scope/pins/${encodeURIComponent(id)}/reopen`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ comment }),
    })
  }

  async withdrawScopePin(id: string): Promise<void> {
    await this.fetch(`/api/scope/pins/${encodeURIComponent(id)}/withdraw`, { method: "POST" })
  }

  private async fetch(path: string, init: RequestInit = {}): Promise<Response> {
    const headers = new Headers(init.headers)
    if (this.token) headers.set("Authorization", `Bearer ${this.token}`)
    let response: Response
    try {
      response = await this.fetcher.call(globalThis, `${this.baseUrl}${path}`, { ...init, headers: Object.fromEntries(headers) })
    } catch {
      throw new Error(unreachableMessage(this.baseUrl))
    }
    if (!response.ok) {
      const detail = errorMessage((await response.text()).trim())
      throw new LegatoRequestError(detail || `Legato request failed (${response.status})`, response.status)
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
