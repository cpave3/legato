import { describe, expect, it } from "vitest"
import { createNativeSyncState, reconcileNativeSnapshot, reduceNativeEvent, stateFromNativeSnapshot } from "../nativeAgentSync"
import fixture from "../../../../contracts/model-settings-v1.json"
import type { ModelSettingsState } from "../modelSettings"

describe("native agent synchronization", () => {
  it("preserves shared settings on reconnect and rejects stale model settings events", () => {
    const settings = fixture.state as ModelSettingsState
    let state = stateFromNativeSnapshot({ model: { providerId: "manticore", modelId: "sol", settings: settings.effective, settingsDefaults: settings.defaults, capabilities: settings.capabilities }, modelSettingsOverrides: { "manticore/sol": settings.overrides }, status: "waiting_for_permission" })
    expect(state.busy).toBe(true)
    expect(state.settingsState).toEqual(settings)
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
