import type { HandoffPacket } from "../../lib/handoff"
export function handoffPacket(): HandoffPacket {
  return {
    id: "hp-1", revision: 1, created_at: "2026-09-01", repository: "github.com/team/repo", plan_id: "pl-1", plan_revision: "pr-1", plan_markdown: "## Agreed behavior\nKeep questions durable.", outdated: false, outdated_reason: "",
    input: {
      schema_version: 1, expected_revision: 0, pass_id: "rp-1", base_sha: "base", head_sha: "head", changes: "Initial handoff",
      overview: { before: "Questions were lost", after: "Questions persist", why: "Offline support", non_goals: "Guaranteed delivery", decision: "Save first", alternative: "Send first", risk: "Duplicate delivery", verification_gaps: "No live delivery check", review_question: "Is persistence atomic?", rollback: "Disable delivery" },
      concepts: [
        { id: "durability", title: "Durable questions", claim: "Save before delivery", flow: ["Request", "Store", "Queue"], invariant: "Persist once", failure_case: "Agent offline", change_guide: "Start at queue", source_ids: ["src"], evidence_ids: ["check"], chapter_ids: ["S-1"], routes: ["owner"] },
        { id: "operations", title: "Delivery operations", claim: "Retries are bounded", flow: [], invariant: "Bound retries", failure_case: "Queue full", change_guide: "Inspect worker", source_ids: [], evidence_ids: [], chapter_ids: [], routes: ["maintainer"] },
      ],
      sources: [], evidence: [{ id: "check", description: "Queue tests", source_ids: ["src"], command: "go test ./queue", head_sha: "old-head", recorded_at: "2026-08-31", outcome: "passed", reporter: "human", limitations: "Unit tests only" }],
      scenarios: [{ id: "offline", concept_id: "durability", prompt: "What happens offline?", explanation: "The question stays queued.", source_ids: ["src"] }],
      coverage: [{ path: "queue.go", category: "behavior" }, { path: "other.go", category: "uncategorized" }],
    },
    sources: [{ id: "src", path: "queue.go", side: "head", line_start: 2, line_end: 3, commit_sha: "head", digest: "sha256:abc", text: '<script>alert("source")</script>' }],
    chapters: [{ id: "S-1", title: "Refresh expired tokens", narration: "Captured narration" }],
    note: { revision: 0, body: "", readiness: "", answers: {}, skipped: [] },
    comparison: { previous_revision: 0, safe: false, reason: "First packet", changed_concepts: [], changed_files: [], evidence_to_recheck: [] },
  }
}
