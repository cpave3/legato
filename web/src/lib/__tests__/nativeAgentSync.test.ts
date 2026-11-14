import { describe, expect, it, vi } from "vitest"
import { createNativeSyncState, reconcileNativeSnapshot, reduceNativeEvent, stateFromNativeSnapshot, subagentStatus } from "../nativeAgentSync"
import subagentFixture from "../../../../contracts/chimera-subagents.json"
import type { NativeEvent, NativeSnapshot } from "../nativeAgentSync"
import fixture from "../../../../contracts/model-settings-v1.json"
import type { ModelSettingsState } from "../modelSettings"

describe("native agent synchronization", () => {
  it("replays foreground and idle-background success, failure and interruption through snapshot repair", () => {
    let state = createNativeSyncState()
    for (const stage of subagentFixture.stages) {
      for (const event of stage.nativeEvents) state = reduceNativeEvent(state, event as NativeEvent)
      state = reconcileNativeSnapshot(stage.snapshot as NativeSnapshot, state, [])
      const statuses = Object.fromEntries(state.items.flatMap(item => item.tool?.subagent ? [[item.tool.subagent.subagentId, item.tool.subagent.status]] : []))
      expect(statuses, stage.label).toEqual(stage.states)
      const reloaded = stateFromNativeSnapshot(stage.snapshot as NativeSnapshot)
      expect(Object.fromEntries(reloaded.items.flatMap(item => item.tool?.subagent ? [[item.tool.subagent.subagentId, item.tool.subagent.status]] : [])), stage.label + " reload").toEqual(Object.fromEntries(Object.entries(stage.states).filter(([id]) => (stage.snapshot as NativeSnapshot).toolCalls?.some(call => call.result?.subagent_id === id))))
      for (const item of reloaded.items) if (item.tool) expect(subagentStatus(item.tool)).not.toBe("spawning")
    }
  })
  it("finishes a failed launch without spawned SSE and a tool exception without a result", () => {
    let state = reduceNativeEvent(createNativeSyncState(), { eventId: "start", type: "tool_call_start", callId: "spawn", name: "spawn_agent", args: { purpose: "Fail" } })
    expect(subagentStatus(state.items[0].tool!)).toBe("spawning")
    state = reduceNativeEvent(state, { eventId: "error", type: "tool_call_result", callId: "spawn", result: { subagent_id: "bad", session_id: "", reason: "error", result: "handshake failed" } })
    expect(subagentStatus(state.items[0].tool!)).toBe("error")
    state = reduceNativeEvent(state, { eventId: "start2", type: "tool_call_start", callId: "spawn2", name: "spawn_agent", args: {} })
    state = reduceNativeEvent(state, { eventId: "error2", type: "tool_call_error", callId: "spawn2", error: "cancelled before handshake" })
    expect(subagentStatus(state.items[1].tool!)).toBe("error")
  })
  it("does not resurrect a completed child when snapshot repair replays its spawn", () => {
    const final = subagentFixture.stages.at(-1)!.snapshot as NativeSnapshot
    const spawn = subagentFixture.stages[2].nativeEvents.find(event => event.type === "subagent_spawned")! as NativeEvent
    const repaired = reconcileNativeSnapshot(final, createNativeSyncState(), [spawn])
    expect(repaired.items.find(item => item.tool?.key === "spawn-bg")?.tool?.subagent?.status).toBe("finished")
  })
  it.each([false, true].flatMap(background => [
    { background, reason: "stop", status: "finished" },
    { background, reason: "error", status: "error" },
    { background, reason: "interrupted", status: "interrupted" },
  ]))("settles $background background child as $status", ({ background, reason, status }) => {
    let state = reduceNativeEvent(createNativeSyncState(), { eventId: "start", type: "tool_call_start", callId: "spawn", name: "spawn_agent", args: { background } })
    state = reduceNativeEvent(state, { eventId: "spawned", type: "subagent_spawned", parentCallId: "spawn", subagentId: "child" })
    expect(subagentStatus(state.items[0].tool!)).toBe("running")
    state = reduceNativeEvent(state, { eventId: "finish", type: "subagent_finished", parentCallId: "spawn", subagentId: "child", reason })
    state = reduceNativeEvent(state, { eventId: "result", type: "tool_call_result", callId: "spawn", result: background ? { subagent_id: "child", status: "running" } : { subagent_id: "child", reason } })
    expect(subagentStatus(state.items[0].tool!)).toBe(status)
  })
  it("joins mod completion to a live child before the parent tool result is saved", () => {
    let state = reduceNativeEvent(createNativeSyncState(), { eventId: "start", type: "tool_call_start", callId: "spawn", name: "spawn_agent", args: {} })
    state = reduceNativeEvent(state, { eventId: "spawned", type: "subagent_spawned", parentCallId: "spawn", subagentId: "child" })
    state = reconcileNativeSnapshot({ id: "parent", messages: [{ role: "assistant", content: [{ type: "tool-call" }] }], toolCalls: [{ callId: "spawn", name: "spawn_agent", args: {} }], subagentStates: [{ sessionId: "parent", subagentId: "child", status: "interrupted", reason: "interrupted" }] }, state, [])
    expect(subagentStatus(state.items[0].tool!)).toBe("interrupted")
  })
  it.each([true, false])("keeps a child interrupt distinct from its parent tool exception (finish first: %s)", finishFirst => {
    let state = reduceNativeEvent(createNativeSyncState(), { eventId: "start", type: "tool_call_start", callId: "spawn", name: "spawn_agent", args: {} })
    state = reduceNativeEvent(state, { eventId: "spawned", type: "subagent_spawned", parentCallId: "spawn", subagentId: "child" })
    const finish: NativeEvent = { eventId: "finish", type: "subagent_finished", subagentId: "child", reason: "interrupted" }
    const error: NativeEvent = { eventId: "error", type: "tool_call_error", callId: "spawn", error: "Aborted" }
    for (const event of finishFirst ? [finish, error] : [error, finish]) state = reduceNativeEvent(state, event)
    expect(subagentStatus(state.items[0].tool!)).toBe("interrupted")
  })
  it("recovers a background launch from the tool result when spawned SSE was missed", () => {
    let state = createNativeSyncState()
    state = reduceNativeEvent(state, { eventId: "start", type: "tool_call_start", callId: "spawn-bg", name: "spawn_agent", args: { purpose: "Check docs", background: true } })
    state = reduceNativeEvent(state, { eventId: "result", type: "tool_call_result", callId: "spawn-bg", result: { subagent_id: "bg-1", session_id: "child-bg", status: "running" } })
    expect(state.items[0]?.tool?.subagent).toMatchObject({ subagentId: "bg-1", childSessionId: "child-bg", status: "running" })
    const repaired = reconcileNativeSnapshot({
      messages: [{ role: "assistant", content: [{ type: "tool-call" }] }],
      toolCalls: [{ callId: "spawn-bg", name: "spawn_agent", args: { purpose: "Check docs", background: true }, result: { subagent_id: "bg-1", session_id: "child-bg", status: "running" } }],
    }, createNativeSyncState(), [])
    expect(repaired.items[0]?.tool?.subagent?.status).toBe("running")
  })
  it("preserves shared settings on reconnect and rejects stale model settings events", () => {
    const settings = fixture.state as ModelSettingsState
    let state = stateFromNativeSnapshot({ model: { providerId: "manticore", modelId: "sol", settings: settings.effective, settingsDefaults: settings.defaults, capabilities: settings.capabilities }, modelSettingsOverrides: { "manticore/sol": settings.overrides }, status: "waiting_for_permission" })
    expect(state.busy).toBe(true)
    expect(state.settingsState).toEqual(settings)
    expect(state.settingsState?.capabilities.inputModalities).toEqual(["text", "image"])
    state = reduceNativeEvent(state, { eventId: "model", type: "model_changed", to: "manticore/kimi" })
    state = reduceNativeEvent(state, { eventId: "stale", type: "model_settings_changed", settingsState: settings })
    expect(state.model).toBe("manticore/kimi")
    expect(state.settingsState).toBeUndefined()
    state = reduceNativeEvent(state, { eventId: "mode", type: "mode_changed", effectiveModel: "manticore/sol" })
    expect(state.model).toBe("manticore/sol")
  })
  it("keeps interleaved assistant streams and tools in first-seen order", () => {
    let state = createNativeSyncState()
    state = reduceNativeEvent(state, { eventId: "1", type: "assistant_text_delta", id: "a", delta: "Alpha" })
    state = reduceNativeEvent(state, { eventId: "2", type: "tool_call_start", callId: "call-1", name: "read", args: {} })
    state = reduceNativeEvent(state, { eventId: "3", type: "assistant_text_delta", id: "b", delta: "Beta" })
    state = reduceNativeEvent(state, { eventId: "4", type: "assistant_text_delta", id: "a", delta: " continued" })

    expect(state.items.map((item) => item.tool?.toolName ?? item.text)).toEqual([
      "Alpha continued",
      "read",
      "Beta",
    ])
  })

  it("deduplicates only snapshot overlap and preserves later repeated replies", () => {
    let state = reconcileNativeSnapshot({
      messages: [
        { role: "user", content: "system" },
        { role: "assistant", content: [{ type: "text", text: "Same reply" }] },
      ],
      toolCalls: [],
    }, createNativeSyncState(), [
      { eventId: "overlap", type: "assistant_text_done", id: "a", text: "Same reply" },
    ])
    state = reduceNativeEvent(state, { eventId: "later", type: "assistant_text_done", id: "b", text: "Same reply" })

    expect(state.items.filter((item) => item.text === "Same reply")).toHaveLength(2)
  })

  it("attaches skill activation metadata to the most recent read tool", () => {
    let state = createNativeSyncState()
    state = reduceNativeEvent(state, {
      eventId: "read-1",
      type: "tool_call_start",
      callId: "call-1",
      name: "read",
      args: { path: "/skills/coworker-walkthrough/SKILL.md" },
    })
    state = reduceNativeEvent(state, {
      eventId: "skill-1",
      type: "skill_activated",
      skillName: "coworker-walkthrough",
      source: "user",
    })

    expect(state.items[0]?.tool).toMatchObject({
      skillName: "coworker-walkthrough",
      skillSource: "user",
    })
  })

  it("preserves skill metadata when an authoritative snapshot replaces live items", () => {
    let state = createNativeSyncState()
    state = reduceNativeEvent(state, {
      eventId: "read-1",
      type: "tool_call_start",
      callId: "call-1",
      name: "read",
      args: { path: "/skills/coworker-walkthrough/SKILL.md" },
    })
    state = reduceNativeEvent(state, {
      eventId: "skill-1",
      type: "skill_activated",
      skillName: "coworker-walkthrough",
      source: "user",
    })

    state = reconcileNativeSnapshot({
      messages: [
        { role: "user", content: "system" },
        { role: "assistant", content: [{ type: "tool-call" }] },
      ],
      toolCalls: [{
        callId: "call-1",
        name: "read",
        args: { path: "/skills/coworker-walkthrough/SKILL.md" },
      }],
    }, state, [])

    expect(state.items[0]?.tool?.skillName).toBe("coworker-walkthrough")
  })

  it("preserves live subagent activity when a snapshot reconciles the parent tool call", () => {
    let state = createNativeSyncState()
    state = reduceNativeEvent(state, {
      eventId: "call",
      type: "tool_call_start",
      callId: "spawn-1",
      name: "spawn_agent",
      args: { purpose: "Review", prompt: "Inspect changes" },
    })
    state = reduceNativeEvent(state, {
      eventId: "spawned",
      type: "subagent_spawned",
      subagentId: "sub-1",
      parentCallId: "spawn-1",
      childSessionId: "child-1",
      purpose: "Review",
    })
    state = reduceNativeEvent(state, {
      eventId: "activity",
      type: "subagent_event",
      subagentId: "sub-1",
      event: { type: "assistant_text_done", text: "Found a regression" },
    })

    state = reconcileNativeSnapshot({
      messages: [
        { role: "user", content: "system" },
        { role: "assistant", content: [{ type: "tool-call" }] },
      ],
      toolCalls: [{
        callId: "spawn-1",
        name: "spawn_agent",
        args: { purpose: "Review", prompt: "Inspect changes" },
        result: { reason: "stop", steps: 2, tool_calls_count: 1 },
      }],
    }, state, [])

    expect(state.items[0]?.tool?.subagent).toMatchObject({
      subagentId: "sub-1",
      status: "finished",
      reason: "stop",
      activities: [{ text: "Found a regression" }],
    })
  })

  it("restores a subagent and what it did from a snapshot", () => {
    const state = stateFromNativeSnapshot({
      messages: [
        { role: "system", content: "" },
        { role: "assistant", content: [{ type: "tool-call" }] },
      ],
      toolCalls: [{
        callId: "spawn-1",
        name: "spawn_agent",
        args: { purpose: "Find the loader" },
        subagent: {
          subagentId: "agent-7", purpose: "Find the loader", status: "running", startedAt: 1_000,
          events: [
            { type: "assistant_text_done", text: "Looking in config/." },
            { type: "tool_call_start", callId: "c1", name: "Grep", display: { summary: "yaml.Unmarshal" } },
          ],
        },
      }],
    })

    expect(state.items[0]?.tool?.subagent).toMatchObject({
      subagentId: "agent-7",
      purpose: "Find the loader",
      status: "running",
      startedAt: 1_000,
      activities: [{ text: "Looking in config/." }, { text: "Grep: yaml.Unmarshal", toolName: "Grep" }],
    })
  })

  it("records when a subagent started and ended", () => {
    let state = reduceNativeEvent(createNativeSyncState(), {
      eventId: "call", type: "tool_call_start", callId: "spawn-1", name: "spawn_agent", args: {},
    })
    state = reduceNativeEvent(state, {
      eventId: "spawned", type: "subagent_spawned", subagentId: "sub-1", parentCallId: "spawn-1", purpose: "Review", startedAt: 5_000,
    })
    state = reduceNativeEvent(state, { eventId: "done", type: "subagent_finished", subagentId: "sub-1", reason: "stop", endedAt: 9_000 })

    expect(state.items[0]?.tool?.subagent).toMatchObject({ startedAt: 5_000, endedAt: 9_000, status: "finished" })
  })

  it("dates a subagent by when its events arrived when the agent sends no times", () => {
    vi.useFakeTimers()
    try {
      vi.setSystemTime(2_000)
      let state = reduceNativeEvent(createNativeSyncState(), {
        eventId: "call", type: "tool_call_start", callId: "spawn-1", name: "spawn_agent", args: {},
      })
      state = reduceNativeEvent(state, { eventId: "spawned", type: "subagent_spawned", subagentId: "sub-1", parentCallId: "spawn-1" })
      vi.setSystemTime(7_000)
      state = reduceNativeEvent(state, { eventId: "done", type: "subagent_finished", subagentId: "sub-1", reason: "error" })

      expect(state.items[0]?.tool?.subagent).toMatchObject({ startedAt: 2_000, endedAt: 7_000, status: "error" })
    } finally {
      vi.useRealTimers()
    }
  })

  it("does not repeat subagent activity that the snapshot already holds", () => {
    const snapshot = {
      messages: [
        { role: "system", content: "" },
        { role: "assistant", content: [{ type: "tool-call" }] },
      ],
      toolCalls: [{
        callId: "spawn-1", name: "spawn_agent", args: {},
        subagent: {
          subagentId: "agent-7", purpose: "Look", status: "running" as const,
          events: [
            { type: "assistant_text_done", text: "Looking." },
            { type: "tool_call_start", callId: "c1", name: "Grep", display: { summary: "x" } },
          ],
        },
      }],
    }
    const replayed = [
      { eventId: "e1", type: "subagent_event", subagentId: "agent-7", event: { type: "assistant_text_done", text: "Looking." } },
      { eventId: "e2", type: "subagent_event", subagentId: "agent-7", event: { type: "tool_call_start", callId: "c1", name: "Grep", display: { summary: "x" } } },
      { eventId: "e3", type: "subagent_event", subagentId: "agent-7", event: { type: "tool_call_start", callId: "c2", name: "Read", display: { summary: "y" } } },
    ]

    const state = reconcileNativeSnapshot(snapshot, createNativeSyncState(), replayed)

    expect(state.items[0]?.tool?.subagent?.activities.map((activity) => activity.text)).toEqual(["Looking.", "Grep: x", "Read: y"])
  })

  it("renders forwarded child reasoning, updated tool summaries, and failures", () => {
    let state = createNativeSyncState()
    state = reduceNativeEvent(state, {
      eventId: "call", type: "tool_call_start", callId: "spawn-1", name: "spawn_agent", args: {},
    })
    state = reduceNativeEvent(state, {
      eventId: "spawned", type: "subagent_spawned", subagentId: "sub-1", parentCallId: "spawn-1", purpose: "Review",
    })
    state = reduceNativeEvent(state, {
      eventId: "reasoning", type: "subagent_event", subagentId: "sub-1",
      event: { type: "reasoning_text_done", text: "Checking the lifecycle" },
    })
    state = reduceNativeEvent(state, {
      eventId: "tool-start", type: "subagent_event", subagentId: "sub-1",
      event: { type: "tool_call_start", callId: "child-tool", name: "bash", display: { summary: "go test ./..." } },
    })
    state = reduceNativeEvent(state, {
      eventId: "tool-result", type: "subagent_event", subagentId: "sub-1",
      event: { type: "tool_call_result", callId: "child-tool", display: { summary: "exit 1" } },
    })
    state = reduceNativeEvent(state, {
      eventId: "tool-error", type: "subagent_event", subagentId: "sub-1",
      event: { type: "tool_call_error", error: "test failed" },
    })
    state = reduceNativeEvent(state, {
      eventId: "run-error", type: "subagent_event", subagentId: "sub-1",
      event: { type: "run_finished", reason: "error", error: "child stopped" },
    })

    expect(state.items[0]?.tool?.subagent?.activities.map((activity) => activity.text)).toEqual([
      "Checking the lifecycle",
      "bash: exit 1",
      "Tool error: test failed",
      "child stopped",
    ])
  })

  it("applies duplicate event and tool call IDs exactly once", () => {
    let state = createNativeSyncState()
    const message = { eventId: "1", type: "assistant_text_done", id: "a", text: "Done" }
    const tool = { eventId: "2", type: "tool_call_start", callId: "call-1", name: "write", args: {} }

    state = reduceNativeEvent(state, message)
    state = reduceNativeEvent(state, message)
    state = reduceNativeEvent(state, tool)
    state = reduceNativeEvent(state, { ...tool, eventId: "3" })

    expect(state.items).toHaveLength(2)
    expect(state.cursor).toBe("3")
  })

  it("hydrates structured pending permissions from a snapshot", () => {
    const state = stateFromNativeSnapshot({
      status: "waiting_for_permission",
      messages: [],
      toolCalls: [],
      pendingPermissions: [{
        requestId: "req-1",
        tool: "bash",
        target: "host",
        command: "rm -rf build-cache",
        reason: "Runs outside the sandbox",
        cwd: "/tmp",
        policyAnalysis: {
          version: 1,
          outcome: "approval_required",
          stages: [],
          findings: [{
            kind: "risk",
            stageId: "risk",
            level: "danger",
            message: "dangerous command",
          }],
        },
      }],
    })

    expect(state.items).toHaveLength(1)
    expect(state.items[0]?.permission).toMatchObject({
      requestId: "req-1",
      command: "rm -rf build-cache",
      reason: "Runs outside the sandbox",
      status: "pending",
      policyAnalysis: { findings: [{ message: "dangerous command" }] },
    })
  })

  it("retains structured permission details and finalizes matching requests", () => {
    let state = createNativeSyncState()
    state = reduceNativeEvent(state, {
      eventId: "permission-1",
      type: "permission_request",
      requestId: "req-1",
      tool: "bash",
      target: "host",
      command: "rm -rf build-cache",
      reason: "Runs outside the sandbox",
      policyAnalysis: {
        version: 1,
        outcome: "approval_required",
        stages: [{ id: "paths", status: "findings", findings: [] }],
        findings: [{
          kind: "path",
          stageId: "paths",
          operation: "delete",
          path: "build-cache",
          resolvedPath: "/tmp/build-cache",
          requiredGrant: "write",
          message: "Deletes a path outside configured roots",
        }],
      },
    })

    expect(state.items[0]?.permission).toMatchObject({
      requestId: "req-1",
      tool: "bash",
      target: "host",
      command: "rm -rf build-cache",
      reason: "Runs outside the sandbox",
      status: "pending",
      policyAnalysis: { findings: [{ kind: "path", resolvedPath: "/tmp/build-cache" }] },
    })

    state = reduceNativeEvent(state, {
      eventId: "resolved-1",
      type: "permission_resolved",
      requestId: "req-1",
      decision: "deny",
      remembered: false,
    })
    expect(state.items[0]?.permission).toMatchObject({ status: "denied", remembered: false })
  })

  it("retains a live permission card across snapshot repair", () => {
    let state = createNativeSyncState()
    state = reduceNativeEvent(state, {
      eventId: "permission-1", type: "permission_request", requestId: "req-1",
      tool: "bash", target: "host", command: "echo retained",
    })

    state = reconcileNativeSnapshot({ messages: [], toolCalls: [] }, state, [])

    expect(state.items[0]?.permission).toMatchObject({ requestId: "req-1", status: "pending" })
  })

  it("uses field presence to reconcile stale permission cards compatibly", () => {
    let state = createNativeSyncState()
    for (const requestId of ["pending", "resolved"]) {
      state = reduceNativeEvent(state, {
        eventId: `request-${requestId}`,
        type: "permission_request",
        requestId,
        tool: "bash",
        target: "host",
        command: `echo ${requestId}`,
      })
    }
    state = reduceNativeEvent(state, {
      eventId: "resolution",
      type: "permission_resolved",
      requestId: "resolved",
      decision: "allow",
      remembered: false,
    })

    const olderSnapshot = reconcileNativeSnapshot({ messages: [], toolCalls: [] }, state, [])
    expect(olderSnapshot.items.map((item) => item.permission?.requestId)).toEqual(["pending", "resolved"])

    const authoritativeEmpty = reconcileNativeSnapshot({
      messages: [],
      toolCalls: [],
      pendingPermissions: [],
    }, state, [])
    expect(authoritativeEmpty.items.map((item) => item.permission?.requestId)).toEqual(["resolved"])
    expect(authoritativeEmpty.items[0]?.permission?.status).toBe("allowed")
  })

  it("deduplicates snapshot overlap before applying a queued resolution", () => {
    const pendingPermission = {
      requestId: "req-1",
      tool: "bash",
      target: "host" as const,
      command: "rm -rf cache",
      cwd: "/tmp",
    }
    const state = reconcileNativeSnapshot({
      messages: [],
      toolCalls: [],
      pendingPermissions: [pendingPermission],
    }, createNativeSyncState(), [
      {
        eventId: "request-1",
        type: "permission_request",
        requestId: "req-1",
        tool: "bash",
        target: "host",
        command: "rm -rf cache",
      },
      {
        eventId: "resolved-1",
        type: "permission_resolved",
        requestId: "req-1",
        decision: "deny",
        remembered: false,
      },
    ])

    expect(state.items.filter((item) => item.permission?.requestId === "req-1")).toHaveLength(1)
    expect(state.items[0]?.permission?.status).toBe("denied")
  })

  it("expires only the matching permission request", () => {
    let state = createNativeSyncState()
    for (const requestId of ["req-1", "req-2"]) {
      state = reduceNativeEvent(state, {
        eventId: `event-${requestId}`, type: "permission_request", requestId,
        tool: "bash", target: "host", command: `echo ${requestId}`,
      })
    }
    state = reduceNativeEvent(state, { eventId: "timeout", type: "permission_timeout", requestId: "req-2" })

    expect(state.items.map((item) => item.permission?.status)).toEqual(["pending", "expired"])
  })

  it("extracts the current model ref from the session snapshot", () => {
    const state = stateFromNativeSnapshot({
      mode: "build",
      model: { providerId: "manticore", modelId: "glm" },
      messages: [],
      toolCalls: [],
    })
    expect(state.model).toBe("manticore/glm")
  })

  it("handles model_changed events by updating the model ref", () => {
    let state = createNativeSyncState()
    state.model = "manticore/glm"
    state = reduceNativeEvent(state, {
      eventId: "model-1",
      type: "model_changed",
      from: "manticore/glm",
      to: "manticore/gpt",
    })
    expect(state.model).toBe("manticore/gpt")
  })

  it("preserves the model when a snapshot reconciles after a model_changed event", () => {
    let state = createNativeSyncState()
    state.model = "manticore/glm"
    state = reduceNativeEvent(state, {
      eventId: "model-1",
      type: "model_changed",
      from: "manticore/glm",
      to: "manticore/gpt",
    })
    state = reconcileNativeSnapshot({
      model: { providerId: "manticore", modelId: "gpt" },
      messages: [],
      toolCalls: [],
    }, state, [])
    expect(state.model).toBe("manticore/gpt")
  })
})
