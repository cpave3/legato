export type NativeItemRole = "user" | "assistant" | "tool" | "system" | "bang"

export interface NativeBangItem {
  key: string
  command: string
  stdout: string
  stderr: string
  exitCode: number
  timedOut: boolean
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
}

export interface NativeConversationItem {
  key: string
  role: NativeItemRole
  text: string
  isStreaming?: boolean
  tool?: NativeToolItem
  toolName?: string
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

export interface NativeSnapshot {
  mode?: string
  status?: string
  messages?: NativeSnapshotMessage[]
  toolCalls?: NativeSnapshotToolCall[]
  contextWindow?: number
  usedContextTokens?: number
}

export interface NativeEvent {
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
  toolName?: string
  error?: string
  contextWindow?: number
  usedContextTokens?: number
  skillName?: string
  source?: "project" | "user" | "claude-compat"
}

const nativeEventHistoryLimit = 1000

export interface NativeSyncState {
  items: NativeConversationItem[]
  cursor: string
  eventIds: Set<string>
  eventOrder: string[]
  callIds: Set<string>
  streamText: Map<string, string>
  busy: boolean
  error: string | null
  contextUsage: { used: number; window: number } | null
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
  const state = createNativeSyncState(items, calls.map((call) => call.callId).filter(Boolean))
  state.busy = snapshot.status === "running"
  if (snapshot.contextWindow && snapshot.usedContextTokens != null) {
    state.contextUsage = { used: snapshot.usedContextTokens, window: snapshot.contextWindow }
  }
  return state
}

export function reconcileNativeSnapshot(
  snapshot: NativeSnapshot,
  previous: NativeSyncState,
  queuedEvents: NativeEvent[],
): NativeSyncState {
  let next = stateFromNativeSnapshot(snapshot)
  const previousSkills = new Map(previous.items.flatMap((item) => item.tool?.skillName
    ? [[item.tool.key, { skillName: item.tool.skillName, skillSource: item.tool.skillSource }] as const]
    : []))
  next.items = next.items.map((item) => {
    if (!item.tool) return item
    const skill = previousSkills.get(item.tool.key)
    return skill ? { ...item, tool: { ...item.tool, ...skill } } : item
  })
  next.cursor = previous.cursor
  next.eventIds = new Set(previous.eventIds)
  next.eventOrder = [...previous.eventOrder]
  const snapshotUserTexts = new Set(next.items.filter((item) => item.role === "user").map((item) => item.text))
  const snapshotAssistantTexts = new Set(next.items.filter((item) => item.role === "assistant").map((item) => item.text))
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
    next = reduceNativeEvent(next, event)
  }
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
      upsertAssistant(next.items, streamID, text, true)
      break
    }
    case "assistant_text_done": {
      const streamID = event.id || event.eventId
      const text = event.text ?? next.streamText.get(streamID) ?? ""
      next.streamText.delete(streamID)
      if (text) upsertAssistant(next.items, streamID, text, false)
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
    case "permission_request":
      next.busy = false
      if (event.requestId) next.items.push({ key: event.requestId, role: "system", text: "", toolName: event.toolName })
      break
    case "run_finished":
      next.busy = false
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

function addEventID(state: NativeSyncState, eventID: string) {
  const eventIds = new Set(state.eventIds)
  const eventOrder = [...state.eventOrder, eventID]
  eventIds.add(eventID)
  if (eventOrder.length > nativeEventHistoryLimit) {
    eventIds.delete(eventOrder.shift()!)
  }
  return { eventIds, eventOrder }
}

function upsertAssistant(items: NativeConversationItem[], streamID: string, text: string, streaming: boolean) {
  const key = `stream-${streamID}`
  const index = items.findIndex((item) => item.key === key)
  if (index >= 0) {
    items[index] = { ...items[index], text, isStreaming: streaming }
  } else {
    items.push({ key, role: "assistant", text, isStreaming: streaming })
  }
}
