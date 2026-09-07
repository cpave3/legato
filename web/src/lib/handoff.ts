import { apiFetch } from "./api"

export interface HandoffInput {
  schema_version: number
  expected_revision: number
  pass_id: string
  base_sha: string
  head_sha: string
  overview: HandoffOverview
  concepts: HandoffConcept[]
  sources: HandoffSourceInput[]
  evidence: HandoffEvidence[]
  scenarios: HandoffScenario[]
  coverage: HandoffCoverage[]
  changes: string
}

export interface HandoffOverview {
  before: string
  after: string
  why: string
  non_goals: string
  decision: string
  alternative: string
  risk: string
  verification_gaps: string
  review_question: string
  rollback: string
}

export interface HandoffConcept {
  id: string
  title: string
  claim: string
  flow: string[]
  invariant: string
  failure_case: string
  change_guide: string
  source_ids: string[]
  evidence_ids: string[]
  chapter_ids: string[]
  routes: string[]
}

export interface HandoffSourceInput {
  id: string
  path: string
  side: string
  line_start: number
  line_end: number
}

export interface HandoffSource extends HandoffSourceInput {
  commit_sha: string
  text: string
  digest: string
}

export interface HandoffEvidence {
  id: string
  description: string
  source_ids: string[]
  command: string
  head_sha: string
  recorded_at: string
  outcome: string
  reporter: string
  limitations: string
}

export interface HandoffScenario {
  id: string
  concept_id: string
  prompt: string
  explanation: string
  source_ids: string[]
}

export interface HandoffCoverage {
  path: string
  category: string
}

export interface HandoffChapter {
  id: string
  title: string
  narration: string
}

export interface HandoffNote {
  revision: number
  readiness: string
  body: string
  answers: Record<string, string>
  skipped: string[]
}

export interface HandoffPacket {
  id: string
  revision: number
  created_at: string
  repository: string
  input: HandoffInput
  sources: HandoffSource[]
  chapters: HandoffChapter[]
  plan_id: string
  plan_revision: string
  plan_markdown: string
  outdated: boolean
  outdated_reason: string
  note: HandoffNote
  comparison: HandoffComparison
}

export interface HandoffComparison {
  previous_revision: number
  safe: boolean
  reason: string
  changed_concepts: string[]
  changed_files: string[]
  evidence_to_recheck: string[]
}

export interface HandoffView {
  tour_id: string
  checked_at: string
  packets: HandoffPacket[]
}

export interface HandoffExportInput {
  packet_id: string
  format: string
  concept_ids: string[]
  include_sources: boolean
  include_notes: boolean
}

export interface HandoffExport {
  markdown: string
  filename: string
  warnings: string[]
}

const path = (tour: string) => `/api/review/tours/${encodeURIComponent(tour)}/handoff`
async function request<T>(base: string, url: string, body?: unknown): Promise<T> {
  const response = await apiFetch(base, url, body === undefined ? undefined : {
    method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify(body),
  })
  if (!response.ok) throw new Error(`${response.status === 409 ? "Save conflict — refresh before retrying. " : ""}${await response.text() || response.statusText}`)
  return response.json() as Promise<T>
}
// Go nil slices decode as null. Normalize optional collections at the HTTP boundary.
export async function fetchHandoff(base: string, tour: string): Promise<HandoffView> {
  const view = await request<HandoffView>(base, path(tour))
  return { ...view, packets: view.packets.map((packet) => ({
    ...packet,
    sources: packet.sources ?? [], chapters: packet.chapters ?? [],
    note: { ...packet.note, answers: packet.note.answers ?? {}, skipped: packet.note.skipped ?? [] },
    input: { ...packet.input,
      concepts: packet.input.concepts.map((c) => ({ ...c, flow: c.flow ?? [], source_ids: c.source_ids ?? [], evidence_ids: c.evidence_ids ?? [], chapter_ids: c.chapter_ids ?? [], routes: c.routes ?? [] })),
      sources: packet.input.sources ?? [], coverage: packet.input.coverage ?? [],
      evidence: (packet.input.evidence ?? []).map((e) => ({ ...e, source_ids: e.source_ids ?? [] })),
      scenarios: (packet.input.scenarios ?? []).map((s) => ({ ...s, source_ids: s.source_ids ?? [] })),
    },
  })) }
}
export const saveHandoffNote = (base: string, tour: string, packet: string, note: HandoffNote) => request<HandoffNote>(base, `${path(tour)}/${encodeURIComponent(packet)}/note`, note)
export const exportHandoff = (base: string, tour: string, input: HandoffExportInput) => request<HandoffExport>(base, `${path(tour)}/export`, input)
