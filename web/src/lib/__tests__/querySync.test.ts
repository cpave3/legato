import { describe, expect, it } from "vitest"
import type { WSMessage } from "../../hooks/useWebSocket"
import { invalidationsFor } from "../querySync"

const base = "https://legato.local"
const keysOf = (message: WSMessage) => invalidationsFor(message, base).map(({ queryKey, exact }) => ({ queryKey: [...queryKey], exact }))

describe("invalidationsFor", () => {
  it("refreshes a run's own page and the run lists for a change of that run", () => {
    expect(keysOf({ type: "workflow_changed", workflow_run_id: "wr-1", workflow_step_id: "implement", kind: "step_started" })).toEqual([
      { queryKey: [base, "workflow-runs"], exact: true },
      { queryKey: [base, "workflow-stats"], exact: false },
      { queryKey: [base, "workflow-runs-of"], exact: false },
      { queryKey: [base, "workflow-run", "wr-1"], exact: true },
      { queryKey: [base, "navigation-summary"], exact: true },
    ])
  })

  it("never reads the definitions again for a run's change", () => {
    for (const kind of [undefined, "", "advanced", "completed_step", "review_loop", "step_completed"]) {
      const keys = keysOf({ type: "workflow_changed", workflow_run_id: "wr-1", kind }).map((key) => key.queryKey[1])
      expect(keys).not.toContain("workflow-definitions")
      expect(keys).not.toContain("workflow-run-of")
    }
  })

  it("reads the definitions and their trigger states again when a trigger or a file changes", () => {
    for (const kind of ["definitions", "trigger"]) {
      expect(keysOf({ type: "workflow_changed", kind })).toEqual([
        { queryKey: [base, "workflow-runs"], exact: true },
        { queryKey: [base, "workflow-stats"], exact: false },
        { queryKey: [base, "workflow-runs-of"], exact: false },
        { queryKey: [base, "navigation-summary"], exact: true },
        { queryKey: [base, "workflow-definitions"], exact: true },
      ])
    }
  })

  it("refreshes the review list, the tour and its handoff for a review change", () => {
    expect(keysOf({ type: "review_changed", tour_id: "tour-1" })).toEqual([
      { queryKey: [base, "review-queue", false], exact: true },
      { queryKey: [base, "review-queue", true], exact: true },
      { queryKey: [base, "review-tour", "tour-1"], exact: true },
      { queryKey: [base, "handoff", "tour-1"], exact: true },
      { queryKey: [base, "navigation-summary"], exact: true },
    ])
    expect(keysOf({ type: "review_changed" })).toEqual([
      { queryKey: [base, "review-queue", false], exact: true },
      { queryKey: [base, "review-queue", true], exact: true },
      { queryKey: [base, "navigation-summary"], exact: true },
    ])
  })

  it("keeps what the other messages refresh", () => {
    expect(keysOf({ type: "plan_changed", plan_id: "p-1" })).toEqual([
      { queryKey: [base, "plan-queue"], exact: true },
      { queryKey: [base, "plan", "p-1"], exact: true },
      { queryKey: [base, "navigation-summary"], exact: true },
    ])
    expect(keysOf({ type: "attention_changed" })).toEqual([
      { queryKey: [base, "attention"], exact: true },
      { queryKey: [base, "board"], exact: false },
      { queryKey: [base, "attention", "rail"], exact: true },
      { queryKey: [base, "workflow-run"], exact: false },
      { queryKey: [base, "navigation-summary"], exact: true },
    ])
    expect(keysOf({ type: "approvals_changed" })).toEqual([
      { queryKey: [base, "approvals"], exact: true },
      { queryKey: [base, "reeve", "auto"], exact: true },
      { queryKey: [base, "peer-approvals"], exact: true },
      { queryKey: [base, "devices"], exact: true },
      { queryKey: [base, "navigation-summary"], exact: true },
    ])
    expect(keysOf({ type: "agent_ui_changed", task_id: "t-1", item_id: "i-1" })).toEqual([
      { queryKey: [base, "agent-ui-items", "t-1"], exact: true },
      { queryKey: [base, "agent-ui-item", "i-1"], exact: false },
    ])
    expect(keysOf({ type: "cards_changed" })).toEqual([
      { queryKey: [base, "board"], exact: false },
      { queryKey: [base, "task"], exact: false },
    ])
    expect(keysOf({ type: "agent_stage_changed" })).toEqual([
      { queryKey: [base, "board"], exact: false },
      { queryKey: [base, "agent-stage-history"], exact: false },
    ])
    expect(keysOf({ type: "agents_changed" })).toEqual([
      { queryKey: [base, "job-clones"], exact: true },
      { queryKey: [base, "reeve"], exact: true },
      { queryKey: [base, "navigation-summary"], exact: true },
      { queryKey: [base, "board"], exact: false },
      { queryKey: [base, "preparing-agents"], exact: true },
      { queryKey: [base, "interrupted-agents"], exact: true },
      { queryKey: [base, "sessions"], exact: false },
      { queryKey: [base, "session"], exact: false },
    ])
    expect(keysOf({ type: "server_theme_changed" })).toEqual([
      { queryKey: [base, "meta"], exact: false },
      { queryKey: [base, "theme-preference"], exact: true },
      { queryKey: [base, "settings"], exact: true },
    ])
    expect(keysOf({ type: "settings_changed" })).toEqual([
      { queryKey: [base, "settings"], exact: true },
      { queryKey: [base, "config-settings"], exact: true },
      { queryKey: [base, "config-lists"], exact: true },
      { queryKey: [base, "attention", "rail"], exact: true },
      { queryKey: [base, "macros"], exact: true },
    ])
    expect(keysOf({ type: "update_changed" })).toEqual([{ queryKey: [base, "settings"], exact: true }])
  })

  it("asks for nothing again for messages that carry their own state", () => {
    expect(keysOf({ type: "repository_cache" })).toEqual([])
    expect(keysOf({ type: "agent_list" })).toEqual([])
    expect(keysOf({ type: "agent_output" })).toEqual([])
  })
})
