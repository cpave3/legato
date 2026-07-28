import { apiFetch } from "./api"

export type AttentionAction = { id: string; label: string; kind: "navigate" | "execute"; intent?: string; requires_text?: boolean; requires_confirmation?: boolean }
export type AttentionNavigation = { kind: string; target: string }
export type AttentionItem = {
  id: string
  producer: string
  source_key: string
  source_revision: string
  task_id?: string
  title: string
  reason: string
  detail?: string
  category: string
  urgency: string
  navigation: AttentionNavigation
  actions: AttentionAction[]
  status: string
  created_at: string
  updated_at: string
}

export async function fetchAttention(baseUrl: string): Promise<AttentionItem[]> {
  const resp = await apiFetch(baseUrl, "/api/attention")
  if (!resp.ok) throw new Error(await resp.text())
  const body = await resp.json()
  return body.items ?? []
}

export async function executeAttentionAction(baseUrl: string, id: string, actionId: string, revision: string, opts?: { confirmed?: boolean; text?: string }): Promise<void> {
  const resp = await apiFetch(baseUrl, `/api/attention/${encodeURIComponent(id)}/actions/${encodeURIComponent(actionId)}`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ expected_revision: revision, confirmed: opts?.confirmed ?? false, text: opts?.text ?? "" }),
  })
  if (!resp.ok && resp.status !== 204) throw new Error(await resp.text())
}

export async function dismissAttentionItem(baseUrl: string, id: string, revision: string): Promise<void> {
  const resp = await apiFetch(baseUrl, `/api/attention/${encodeURIComponent(id)}/dismiss`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ expected_revision: revision }),
  })
  if (!resp.ok && resp.status !== 204) throw new Error(await resp.text())
}
