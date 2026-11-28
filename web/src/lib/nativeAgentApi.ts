import { apiFetch } from "./api"

// postAgentJson posts a JSON body to one of a native agent's endpoints
// (model, modes, bang, ...), under /api/agents/<agent>/chimera/.
export function postAgentJson(baseUrl: string, agentId: string, endpoint: string, body: unknown): Promise<Response> {
  return apiFetch(baseUrl, `/api/agents/${agentId}/chimera/${endpoint}`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body),
  })
}
