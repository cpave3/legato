import type { QueryKey } from "@tanstack/react-query"
import type { WSMessage } from "../hooks/useWebSocket"
import { queryKeys } from "./query"

/** A query to read again: one key exactly, or every key under a prefix. */
export interface Invalidation {
  queryKey: QueryKey
  exact: boolean
}

/**
 * The kinds of workflow_changed that are about the workflows' definitions
 * and trigger switches rather than a run: only these read the definitions
 * (and their trigger_states) again, so a busy run never rediscovers the
 * workflow files.
 */
const DEFINITION_KINDS = new Set(["definitions", "trigger"])

/**
 * The queries a push message makes stale. Messages that carry their own
 * state (repository_cache, agent_list) set it instead and ask for nothing.
 */
export function invalidationsFor(message: WSMessage, baseUrl: string): Invalidation[] {
  const keys: Invalidation[] = []
  const exact = (queryKey: QueryKey) => keys.push({ queryKey, exact: true })
  const prefix = (queryKey: QueryKey) => keys.push({ queryKey, exact: false })
  switch (message.type) {
    case "review_changed":
      exact(queryKeys.reviewQueue(baseUrl, false))
      exact(queryKeys.reviewQueue(baseUrl, true))
      if (message.tour_id) {
        exact(queryKeys.reviewTour(baseUrl, message.tour_id))
        exact(queryKeys.handoff(baseUrl, message.tour_id))
      }
      exact(queryKeys.navigationSummary(baseUrl))
      break
    case "plan_changed":
      exact(queryKeys.planQueue(baseUrl))
      if (message.plan_id) exact(queryKeys.plan(baseUrl, message.plan_id))
      exact(queryKeys.navigationSummary(baseUrl))
      break
    case "attention_changed":
      exact(queryKeys.attention(baseUrl))
      // A card says what waits on the operator (needs_you): an item
      // posted or cleared while its agent idles changes it.
      prefix([baseUrl, "board"])
      exact(queryKeys.railAttention(baseUrl))
      // A run's review loop waits on the operator or on its agent
      // author as the rail says (LEG-543): an open run reads it again.
      prefix([baseUrl, "workflow-run"])
      exact(queryKeys.navigationSummary(baseUrl))
      break
    case "approvals_changed":
      exact(queryKeys.approvals(baseUrl))
      // A set that ran without asking is an approval decided at once.
      exact(queryKeys.reeveAuto(baseUrl))
      exact(queryKeys.peerApprovals(baseUrl))
      // A decided approval may enroll or remove a device.
      exact(queryKeys.devices(baseUrl))
      exact(queryKeys.navigationSummary(baseUrl))
      break
    case "workflow_changed":
      exact(queryKeys.workflowRuns(baseUrl))
      prefix([baseUrl, "workflow-stats"])
      prefix([baseUrl, "workflow-runs-of"])
      // A run's change refreshes that run's page, never every run's.
      if (message.workflow_run_id) exact(queryKeys.workflowRun(baseUrl, message.workflow_run_id))
      exact(queryKeys.navigationSummary(baseUrl))
      if (message.kind && DEFINITION_KINDS.has(message.kind)) exact(queryKeys.workflowDefinitions(baseUrl))
      break
    case "agent_ui_changed":
      // An agent showed, replaced or lost an artifact or a card.
      if (message.task_id) exact(queryKeys.agentUIItems(baseUrl, message.task_id))
      if (message.item_id) prefix([baseUrl, "agent-ui-item", message.item_id])
      break
    case "cards_changed":
      prefix([baseUrl, "board"])
      prefix([baseUrl, "task"])
      break
    case "agent_stage_changed":
      // An agent's stage moved: its card's strip and its stage history.
      prefix([baseUrl, "board"])
      prefix([baseUrl, "agent-stage-history"])
      break
    case "agents_changed":
      exact(queryKeys.jobClones(baseUrl))
      exact(queryKeys.reeve(baseUrl))
      exact(queryKeys.navigationSummary(baseUrl))
      prefix([baseUrl, "board"])
      // An agent started in the background began, or failed.
      exact(queryKeys.preparingAgents(baseUrl))
      exact(queryKeys.interruptedAgents(baseUrl))
      // Runs start and end with agents.
      prefix([baseUrl, "sessions"])
      prefix([baseUrl, "session"])
      break
    case "server_theme_changed":
      prefix(queryKeys.meta(baseUrl))
      exact(queryKeys.themePreference(baseUrl))
      exact(queryKeys.settings(baseUrl))
      break
    case "settings_changed":
      // Another browser changed this server's settings.
      exact(queryKeys.settings(baseUrl))
      exact(queryKeys.configSettings(baseUrl))
      exact(queryKeys.configLists(baseUrl))
      // The rail's settings choose what it shows.
      exact(queryKeys.railAttention(baseUrl))
      exact(queryKeys.macros(baseUrl))
      break
    case "update_changed":
      // A check, a new mode, or an update installing: the settings
      // carry where updates stand.
      exact(queryKeys.settings(baseUrl))
      break
  }
  return keys
}

/** Whether a workflow_changed is about a run rather than the definitions. */
export function isRunChange(message: WSMessage): boolean {
  return message.type === "workflow_changed" && !(message.kind && DEFINITION_KINDS.has(message.kind))
}

/** How long a burst of push messages must stop before its refetch. */
export const COALESCE_MS = 75
/** The longest a steady stream of messages holds a refetch back. */
export const MAX_WAIT_MS = 1000

export interface Coalesced {
  (): void
  cancel(): void
}

/**
 * fn, called once a burst of calls stops for wait ms, and at least every
 * maxWait ms while the calls keep coming.
 */
export function coalesce(fn: () => void, wait = COALESCE_MS, maxWait = MAX_WAIT_MS): Coalesced {
  let timer: ReturnType<typeof setTimeout> | undefined
  let since: number | undefined
  const call = () => {
    const now = Date.now()
    if (timer) clearTimeout(timer)
    since ??= now
    timer = setTimeout(() => {
      timer = undefined
      since = undefined
      fn()
    }, Math.max(0, Math.min(wait, since + maxWait - now)))
  }
  call.cancel = () => {
    if (timer) clearTimeout(timer)
    timer = undefined
    since = undefined
  }
  return call
}
