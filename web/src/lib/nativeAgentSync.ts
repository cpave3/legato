import type { ModelSettings, ModelCapabilities, ModelSettingsState } from "./modelSettings"

// reasoning: what the agent thought on its way to a reply or a tool call, as
// far as its model lets that be seen.
export type NativeItemRole = "user" | "assistant" | "tool" | "system" | "bang" | "reasoning"

export interface NativeBangItem {
  key: string
  command: string
  stdout: string
  stderr: string
  exitCode: number
  timedOut: boolean
}

export interface NativeSubagentActivity {
  key: string
  text: string
  callId?: string
  toolName?: string
}

export interface NativeSubagentState {
  subagentId: string
  childSessionId?: string
  purpose: string
  status: "running" | "finished" | "error"
  reason?: string
  activities: NativeSubagentActivity[]
}

export interface NativeToolItem {
  key: string
  toolName: string
  args: Record<string, unknown>
  result?: Record<string, unknown>
  summary?: string
  durationMs?: number
  skillName?: string
  skillSource?: "project" | "user" | "claude-compat"
  subagent?: NativeSubagentState
}

export type NativePermissionFinding =
  | { kind: "risk"; stageId: string; level: "safe" | "caution" | "danger" | "critical"; score?: number; message: string; factors?: string[] }
  | { kind: "path"; stageId: string; operation: "read" | "write" | "delete"; path: string; resolvedPath?: string; requiredGrant: "read" | "write"; message: string }
  | { kind: "unresolved"; stageId: string; message: string }
  | { kind: "analyzer_error"; stageId: string; message: string }
  | { kind: "blocked"; stageId: string; ruleId: string; message: string }

export interface NativePermissionItem {
  requestId: string
  tool: string
  target: "host"
  command: string
  reason?: string
  policyAnalysis?: {
    version: 1
    outcome: "pass" | "approval_required"
    findings: NativePermissionFinding[]
    stages: Array<{ id: string; status: "pass" | "findings" | "error"; findings: NativePermissionFinding[] }>
  }
  status: "pending" | "submitting" | "allowed" | "denied" | "expired"
  remembered?: boolean
  // A Markdown plan the request asks the person to approve.
  plan?: string
  // The ways the request can be allowed, when the agent offers several. A
  // request that lists none takes a plain allow.
  options?: NativePermissionOption[]
  // Which of them the allow took.
  option?: string
  error?: string
}

export interface NativePermissionOption {
  id: string
  label: string
}

export interface NativeQuestionOption {
  label: string
  description?: string
  // Shown beside the option while it is the one chosen: a mockup, a snippet.
  preview?: string
}

// One thing an agent asks the person. Any agent can ask: nothing here names
// the tool the question came from.
export interface NativeQuestion {
  // Names the question in the answers sent back.
  id: string
  header?: string
  question: string
  options: NativeQuestionOption[]
  multiSelect?: boolean
  // The person may type an answer of their own. Absent means they may.
  allowOther?: boolean
}

export interface NativeQuestionItem {
  requestId: string
  questions: NativeQuestion[]
  // expired: no longer answerable here (answered elsewhere, or its turn ended).
  status: "pending" | "submitting" | "answered" | "dismissed" | "expired"
  // What was answered, per question ID.
  answers?: Record<string, string[]>
  error?: string
}

export interface NativeConversationItem {
  key: string
  role: NativeItemRole
  text: string
  isStreaming?: boolean
  tool?: NativeToolItem
  permission?: NativePermissionItem
  question?: NativeQuestionItem
  bang?: NativeBangItem
}

export interface NativeSnapshotMessage {
  role: string
  content: string | Array<{ type: string; text?: string }>
}

export interface NativeSnapshotToolCall {
  callId: string
  name: string
  args: Record<string, unknown>
  result?: Record<string, unknown>
  startedAt?: number
  endedAt?: number
  display?: { summary?: string }
}

export interface NativeSnapshotPermission {
  requestId: string
  tool: string
  target: "host"
  command: string
  reason?: string
  cwd: string
  policyAnalysis?: NativePermissionItem["policyAnalysis"]
  plan?: string
  options?: NativePermissionOption[]
}

export interface NativeSnapshotQuestion {
  requestId: string
  questions: NativeQuestion[]
}

export interface NativeSnapshot {
  mode?: string
  model?: { providerId?: string; modelId?: string; settings?: ModelSettings; settingsDefaults?: ModelSettings; capabilities?: ModelCapabilities }
  modelSettingsOverrides?: Record<string, ModelSettings>
  status?: string
  messages?: NativeSnapshotMessage[]
  toolCalls?: NativeSnapshotToolCall[]
  pendingPermissions?: NativeSnapshotPermission[]
  pendingQuestions?: NativeSnapshotQuestion[]
  contextWindow?: number
  usedContextTokens?: number
}

export interface NativeEvent {
  settingsState?: ModelSettingsState
  eventId: string
  type: string
  id?: string
  content?: string
  text?: string
  delta?: string
  callId?: string
  name?: string
  args?: Record<string, unknown>
  result?: Record<string, unknown>
  display?: { summary?: string }
  requestId?: string
  tool?: string
  target?: "host"
  command?: string
  decision?: "allow" | "deny"
  remembered?: boolean
  plan?: string
  options?: NativePermissionOption[]
  option?: string
  questions?: NativeQuestion[]
  answers?: Record<string, string[]>
  dismissed?: boolean
  policyAnalysis?: NativePermissionItem["policyAnalysis"]
  error?: string
  contextWindow?: number
  usedContextTokens?: number
  skillName?: string
  source?: "project" | "user" | "claude-compat"
  subagentId?: string
  parentCallId?: string
  childSessionId?: string
  purpose?: string
  reason?: string
  from?: string
  to?: string
  effectiveModel?: string
  event?: Omit<NativeEvent, "eventId">
}

const nativeEventHistoryLimit = 1000

export interface NativeSyncState {
  settingsState?: ModelSettingsState
  items: NativeConversationItem[]
  cursor: string
  eventIds: Set<string>
  eventOrder: string[]
  callIds: Set<string>
  streamText: Map<string, string>
  busy: boolean
  error: string | null
  contextUsage: { used: number; window: number } | null
  model: string
}

export function createNativeSyncState(items: NativeConversationItem[] = [], callIds: Iterable<string> = []): NativeSyncState {
  return {
    items,
    cursor: "",
    eventIds: new Set(),
    eventOrder: [],
    callIds: new Set(callIds),
    streamText: new Map(),
    busy: false,
    error: null,
    contextUsage: null,
    model: "",
  }
}

const bangResultPattern = /^!(.+) \(exit (-?\d+)(?:, timed out)?\)\n([\s\S]*)$/

function parseBangResult(text: string): { command: string; exitCode: number; timedOut: boolean; stdout: string; stderr: string } | null {
  const m = text.match(bangResultPattern)
  if (!m) return null
  const [, command, exitStr, rest] = m
  // Split rest into stdout/stderr — stderr lines typically go to stderr,
  // but since the server concatenates them, we treat all remaining output as stdout.
  return {
    command,
    exitCode: parseInt(exitStr, 10),
    timedOut: text.includes(", timed out"),
    stdout: rest || "",
    stderr: "",
  }
}

function permissionConversationItem(permission: NativeSnapshotPermission): NativeConversationItem {
  return {
    key: permission.requestId,
    role: "system",
    text: "",
    permission: {
      requestId: permission.requestId,
      tool: permission.tool,
      target: permission.target,
      command: permission.command,
      reason: permission.reason,
      policyAnalysis: permission.policyAnalysis,
      plan: permission.plan,
      options: permission.options,
      status: "pending",
    },
  }
}

function questionConversationItem(question: NativeSnapshotQuestion): NativeConversationItem {
  return {
    key: question.requestId,
    role: "system",
    text: "",
    question: { requestId: question.requestId, questions: question.questions, status: "pending" },
  }
}

export function stateFromNativeSnapshot(snapshot: NativeSnapshot): NativeSyncState {
  const items: NativeConversationItem[] = []
  const calls = snapshot.toolCalls ?? []
  let callIndex = 0
  for (let index = 0; index < (snapshot.messages ?? []).length; index++) {
    const message = snapshot.messages![index]
    if (index === 0 && message.role === "user") continue
    if (typeof message.content === "string") {
      // Render user messages that are bang command results as bang items
      if (message.role === "user") {
        const bang = parseBangResult(message.content)
        if (bang) {
          items.push({ key: `snap-msg-${index}`, role: "bang", text: "", bang: { key: `snap-msg-${index}`, ...bang } })
          continue
        }
      }
      if (message.content) items.push({ key: `snap-msg-${index}`, role: message.role as NativeItemRole, text: message.content })
      continue
    }
    const text = message.content.filter((part) => part.type === "text" && part.text).map((part) => part.text).join("")
    // What the agent thought comes before what the same message said and did.
    const reasoning = message.content.filter((part) => part.type === "reasoning" && part.text).map((part) => part.text).join("\n\n")
    if (reasoning) items.push({ key: `snap-reasoning-${index}`, role: "reasoning", text: reasoning })
    // Render assistant messages that are bang command results as bang items
    if (message.role === "assistant") {
      const bang = parseBangResult(text)
      if (bang) {
        items.push({ key: `snap-msg-${index}`, role: "bang", text: "", bang: { key: `snap-msg-${index}`, ...bang } })
        continue
      }
    }
    if (text) items.push({ key: `snap-msg-${index}`, role: message.role as NativeItemRole, text })
    const toolCount = message.content.filter((part) => part.type === "tool-call").length
    for (let offset = 0; offset < toolCount && callIndex < calls.length; offset++, callIndex++) {
      const call = calls[callIndex]
      items.push({
        key: call.callId || `snap-call-${callIndex}`,
        role: "tool",
        text: "",
        tool: {
          key: call.callId || `snap-call-${callIndex}`,
          toolName: call.name,
          args: call.args,
          result: call.result,
          summary: call.display?.summary,
          durationMs: call.startedAt && call.endedAt ? call.endedAt - call.startedAt : undefined,
        },
      })
    }
  }
  for (const permission of snapshot.pendingPermissions ?? []) {
    items.push(permissionConversationItem(permission))
  }
  for (const question of snapshot.pendingQuestions ?? []) {
    items.push(questionConversationItem(question))
  }
  const state = createNativeSyncState(items, calls.map((call) => call.callId).filter(Boolean))
  state.busy = snapshot.status === "running" || snapshot.status === "waiting_for_permission" || snapshot.status === "waiting_for_input"
  if (snapshot.contextWindow && snapshot.usedContextTokens != null) {
    state.contextUsage = { used: snapshot.usedContextTokens, window: snapshot.contextWindow }
  }
  if (snapshot.model) {
    state.model = `${snapshot.model.providerId ?? ""}/${snapshot.model.modelId ?? ""}`
    if (snapshot.model.capabilities) state.settingsState = {
      model: state.model, effective: snapshot.model.settings ?? {}, defaults: snapshot.model.settingsDefaults ?? {},
      capabilities: snapshot.model.capabilities, overrides: snapshot.modelSettingsOverrides?.[state.model] ?? {},
    }
  }
  return state
}

export function reconcileNativeSnapshot(
  snapshot: NativeSnapshot,
  previous: NativeSyncState,
  queuedEvents: NativeEvent[],
): NativeSyncState {
  let next = stateFromNativeSnapshot(snapshot)
  const previousToolState = new Map(previous.items.flatMap((item) => item.tool
    ? [[item.tool.key, { skillName: item.tool.skillName, skillSource: item.tool.skillSource, subagent: item.tool.subagent }] as const]
    : []))
  next.items = next.items.map((item) => {
    if (!item.tool) return item
    const preserved = previousToolState.get(item.tool.key)
    if (!preserved) return item
    return {
      ...item,
      tool: {
        ...item.tool,
        ...(preserved.skillName ? { skillName: preserved.skillName, skillSource: preserved.skillSource } : {}),
        ...(preserved.subagent ? {
          subagent: {
            ...preserved.subagent,
            ...subagentCompletionFromResult(item.tool.result),
          },
        } : {}),
      },
    }
  })
  const previousPermissionItems = previous.items.filter((item) => item.permission)
  if (snapshot.pendingPermissions === undefined) {
    next.items.push(...previousPermissionItems)
  } else {
    next.items.push(...previousPermissionItems.filter((item) =>
      item.permission && item.permission.status !== "pending" && item.permission.status !== "submitting"))
  }
  // A settled question is on record as the transcript's own tool call, so
  // only open ones are kept, and only when the snapshot cannot say which are.
  if (snapshot.pendingQuestions === undefined) {
    next.items.push(...previous.items.filter((item) =>
      item.question && (item.question.status === "pending" || item.question.status === "submitting")))
  }
  next.cursor = previous.cursor
  next.eventIds = new Set(previous.eventIds)
  next.eventOrder = [...previous.eventOrder]
  const snapshotUserTexts = new Set(next.items.filter((item) => item.role === "user").map((item) => item.text))
  const snapshotAssistantTexts = new Set(next.items.filter((item) => item.role === "assistant").map((item) => item.text))
  // A snapshot joins the thoughts of one message, so a replayed thought is
  // looked for inside its texts.
  const snapshotReasoning = next.items.filter((item) => item.role === "reasoning").map((item) => item.text)
  for (const event of queuedEvents) {
    if (event.type === "user_message" && event.content && snapshotUserTexts.delete(event.content)) {
      next = acceptWithoutRendering(next, event)
      continue
    }
    if (event.type === "assistant_text_done" && event.text && snapshotAssistantTexts.delete(event.text)) {
      next = acceptWithoutRendering(next, event)
      continue
    }
    if (event.type === "assistant_text_delta" && event.delta) {
      const delta = event.delta
      if ([...snapshotAssistantTexts].some((text) => text.includes(delta))) {
        next = acceptWithoutRendering(next, event)
        continue
      }
    }
    const thought = event.type === "reasoning_text_done" ? event.text : event.type === "reasoning_text_delta" ? event.delta : undefined
    if (thought && snapshotReasoning.some((text) => text.includes(thought))) {
      next = acceptWithoutRendering(next, event)
      continue
    }
    next = reduceNativeEvent(next, event)
  }
  // Events replayed over a snapshot can ask and settle a question that the
  // snapshot already has on record as a tool call, earlier in the feed.
  next.items = next.items.filter((item) =>
    !item.question || item.question.status === "pending" || item.question.status === "submitting")
  return next
}

function acceptWithoutRendering(state: NativeSyncState, event: NativeEvent): NativeSyncState {
  if (!event.eventId) return state
  const { eventIds, eventOrder } = addEventID(state, event.eventId)
  return { ...state, eventIds, eventOrder, cursor: event.eventId }
}

export function reduceNativeEvent(state: NativeSyncState, event: NativeEvent): NativeSyncState {
  if (event.eventId && state.eventIds.has(event.eventId)) return state

  const accepted = event.eventId ? addEventID(state, event.eventId) : {
    eventIds: new Set(state.eventIds),
    eventOrder: [...state.eventOrder],
  }
  const next: NativeSyncState = {
    ...state,
    items: [...state.items],
    eventIds: accepted.eventIds,
    eventOrder: accepted.eventOrder,
    callIds: new Set(state.callIds),
    streamText: new Map(state.streamText),
    cursor: event.eventId || state.cursor,
  }

  switch (event.type) {
    case "session_started":
      next.busy = true
      break
    case "user_message":
      next.busy = true
      if (event.content) next.items.push({ key: event.eventId, role: "user", text: event.content })
      break
    case "assistant_text_delta": {
      const streamID = event.id || event.eventId
      if (!event.delta) break
      const text = (next.streamText.get(streamID) ?? "") + event.delta
      next.streamText.set(streamID, text)
      upsertStream(next.items, "assistant", streamID, text, true)
      break
    }
    case "assistant_text_done": {
      const streamID = event.id || event.eventId
      const text = event.text ?? next.streamText.get(streamID) ?? ""
      next.streamText.delete(streamID)
      if (text) upsertStream(next.items, "assistant", streamID, text, false)
      break
    }
    case "reasoning_text_delta": {
      // A thought and a reply may carry the same ID, so each has its own.
      const streamID = `reasoning-${event.id || event.eventId}`
      if (!event.delta) break
      const text = (next.streamText.get(streamID) ?? "") + event.delta
      next.streamText.set(streamID, text)
      upsertStream(next.items, "reasoning", streamID, text, true)
      break
    }
    case "reasoning_text_done": {
      const streamID = `reasoning-${event.id || event.eventId}`
      const text = event.text ?? next.streamText.get(streamID) ?? ""
      next.streamText.delete(streamID)
      if (text) upsertStream(next.items, "reasoning", streamID, text, false)
      // A thought that ends with nothing to show leaves no item behind.
      else next.items = next.items.filter((item) => item.key !== `stream-${streamID}`)
      break
    }
    case "tool_call_start": {
      const callID = event.callId || event.eventId
      if (next.callIds.has(callID)) break
      next.callIds.add(callID)
      if (event.name) {
        next.items.push({
          key: callID,
          role: "tool",
          text: "",
          tool: { key: callID, toolName: event.name, args: event.args ?? {}, summary: event.display?.summary },
        })
      }
      break
    }
    case "skill_activated": {
      if (!event.skillName) break
      const index = next.items.findLastIndex((item) => item.tool != null)
      const tool = index >= 0 ? next.items[index].tool : undefined
      if (tool?.toolName === "read") {
        next.items[index] = {
          ...next.items[index],
          tool: { ...tool, skillName: event.skillName, skillSource: event.source },
        }
      }
      break
    }
    case "tool_call_result":
      if (event.callId) {
        next.items = next.items.map((item) => {
          if (!item.tool || item.tool.key !== event.callId) return item
          return {
            ...item,
            tool: { ...item.tool, result: event.result, summary: event.display?.summary || item.tool.summary },
          }
        })
      }
      break
    case "subagent_spawned":
      if (event.parentCallId && event.subagentId) {
        updateTool(next.items, event.parentCallId, (tool) => ({
          ...tool,
          subagent: {
            subagentId: event.subagentId!,
            childSessionId: event.childSessionId,
            purpose: event.purpose || String(tool.args.purpose || "Subagent"),
            status: "running",
            activities: tool.subagent?.activities ?? [],
          },
        }))
      }
      break
    case "subagent_event":
      if (event.subagentId && event.event) {
        updateSubagent(next.items, event.subagentId, (subagent) => applySubagentEvent(subagent, event.event!))
      }
      break
    case "subagent_finished":
      if (event.subagentId) {
        updateSubagent(next.items, event.subagentId, (subagent) => ({
          ...subagent,
          status: event.reason === "stop" ? "finished" : "error",
          reason: event.reason,
        }))
      }
      break
    case "permission_request":
      next.busy = false
      if (event.requestId && event.tool && event.target && event.command) {
        const permissionItem = permissionConversationItem({
          requestId: event.requestId,
          tool: event.tool,
          target: event.target,
          command: event.command,
          reason: event.reason,
          cwd: "",
          policyAnalysis: event.policyAnalysis,
          plan: event.plan,
          options: event.options,
        })
        const existingIndex = next.items.findIndex((item) => item.permission?.requestId === event.requestId)
        if (existingIndex >= 0) next.items[existingIndex] = permissionItem
        else next.items.push(permissionItem)
      }
      break
    case "permission_resolved":
      if (event.requestId && event.decision) {
        updatePermission(next.items, event.requestId, (permission) => ({
          ...permission,
          status: event.decision === "allow" ? "allowed" : "denied",
          remembered: event.remembered,
          option: event.option,
          error: undefined,
        }))
      }
      break
    case "permission_timeout":
      if (event.requestId) {
        updatePermission(next.items, event.requestId, (permission) => ({ ...permission, status: "expired", error: undefined }))
      }
      break
    case "question_request":
      next.busy = false
      if (event.requestId && event.questions?.length) {
        const questionItem = questionConversationItem({ requestId: event.requestId, questions: event.questions })
        const existingIndex = next.items.findIndex((item) => item.question?.requestId === event.requestId)
        if (existingIndex >= 0) next.items[existingIndex] = questionItem
        else next.items.push(questionItem)
      }
      break
    case "question_resolved":
      if (event.requestId) {
        updateQuestion(next.items, event.requestId, (question) => ({
          ...question,
          status: event.dismissed ? "dismissed" : "answered",
          answers: event.answers,
          error: undefined,
        }))
      }
      break
    case "question_timeout":
      if (event.requestId) {
        updateQuestion(next.items, event.requestId, (question) => ({ ...question, status: "expired", error: undefined }))
      }
      break
    case "run_finished":
      next.busy = false
      break
    case "mode_changed":
      next.model = event.effectiveModel || next.model
      if (next.settingsState?.model !== next.model) next.settingsState = undefined
      break
    case "model_changed":
      next.model = event.to || next.model
      if (next.settingsState?.model !== next.model) next.settingsState = undefined
      break
    case "model_settings_changed":
      if (event.settingsState?.model === next.model) next.settingsState = event.settingsState
      break
    case "usage_updated":
      if (event.contextWindow && event.usedContextTokens != null) {
        next.contextUsage = { used: event.usedContextTokens, window: event.contextWindow }
      }
      break
    case "error":
      if (event.error) next.error = event.error
      break
  }

  return next
}

export function updatePermissionState(
  state: NativeSyncState,
  requestId: string,
  update: (permission: NativePermissionItem) => NativePermissionItem,
): NativeSyncState {
  const items = [...state.items]
  updatePermission(items, requestId, update)
  return { ...state, items }
}

function updatePermission(
  items: NativeConversationItem[],
  requestId: string,
  update: (permission: NativePermissionItem) => NativePermissionItem,
) {
  const index = items.findIndex((item) => item.permission?.requestId === requestId)
  if (index < 0 || !items[index].permission) return
  items[index] = { ...items[index], permission: update(items[index].permission!) }
}

export function updateQuestionState(
  state: NativeSyncState,
  requestId: string,
  update: (question: NativeQuestionItem) => NativeQuestionItem,
): NativeSyncState {
  const items = [...state.items]
  updateQuestion(items, requestId, update)
  return { ...state, items }
}

function updateQuestion(
  items: NativeConversationItem[],
  requestId: string,
  update: (question: NativeQuestionItem) => NativeQuestionItem,
) {
  const index = items.findIndex((item) => item.question?.requestId === requestId)
  if (index < 0 || !items[index].question) return
  items[index] = { ...items[index], question: update(items[index].question!) }
}

function updateTool(items: NativeConversationItem[], callId: string, update: (tool: NativeToolItem) => NativeToolItem) {
  const index = items.findIndex((item) => item.tool?.key === callId)
  if (index < 0 || !items[index].tool) return
  items[index] = { ...items[index], tool: update(items[index].tool!) }
}

function updateSubagent(
  items: NativeConversationItem[],
  subagentId: string,
  update: (subagent: NativeSubagentState) => NativeSubagentState,
) {
  const index = items.findIndex((item) => item.tool?.subagent?.subagentId === subagentId)
  if (index < 0 || !items[index].tool?.subagent) return
  items[index] = {
    ...items[index],
    tool: {
      ...items[index].tool,
      subagent: update(items[index].tool.subagent),
    },
  }
}

function subagentCompletionFromResult(result?: Record<string, unknown>): Pick<NativeSubagentState, "status" | "reason"> | Record<string, never> {
  const reason = typeof result?.reason === "string" ? result.reason : undefined
  if (!reason) return {}
  return { status: reason === "stop" ? "finished" : "error", reason }
}

function applySubagentEvent(subagent: NativeSubagentState, event: Omit<NativeEvent, "eventId">): NativeSubagentState {
  const activities = [...subagent.activities]
  if ((event.type === "assistant_text_done" || event.type === "reasoning_text_done") && event.text) {
    activities.push({ key: `${subagent.subagentId}-${activities.length}`, text: event.text })
  } else if (event.type === "tool_call_start" && event.name) {
    const summary = event.display?.summary
    activities.push({
      key: `${subagent.subagentId}-${event.callId || activities.length}`,
      callId: event.callId,
      toolName: event.name,
      text: summary ? `${event.name}: ${summary}` : event.name,
    })
  } else if (event.type === "tool_call_result" && event.callId && event.display?.summary) {
    const index = activities.findIndex((activity) => activity.callId === event.callId)
    if (index >= 0) {
      const activity = activities[index]
      activities[index] = {
        ...activity,
        text: activity.toolName ? `${activity.toolName}: ${event.display.summary}` : event.display.summary,
      }
    }
  } else if (event.type === "tool_call_error" && event.error) {
    activities.push({ key: `${subagent.subagentId}-${activities.length}`, text: `Tool error: ${event.error}` })
  } else if (event.type === "run_finished" && event.reason && event.reason !== "stop") {
    activities.push({ key: `${subagent.subagentId}-${activities.length}`, text: event.error || `Run ${event.reason}` })
  }
  return { ...subagent, activities }
}

function addEventID(state: NativeSyncState, eventID: string) {
  const eventIds = new Set(state.eventIds)
  const eventOrder = [...state.eventOrder, eventID]
  eventIds.add(eventID)
  if (eventOrder.length > nativeEventHistoryLimit) {
    eventIds.delete(eventOrder.shift()!)
  }
  return { eventIds, eventOrder }
}

// upsertStream sets the text of the item a stream is writing, adding the item
// when the stream has none yet.
function upsertStream(items: NativeConversationItem[], role: "assistant" | "reasoning", streamID: string, text: string, streaming: boolean) {
  const key = `stream-${streamID}`
  const index = items.findIndex((item) => item.key === key)
  if (index >= 0) {
    items[index] = { ...items[index], text, isStreaming: streaming }
  } else {
    items.push({ key, role, text, isStreaming: streaming })
  }
}
