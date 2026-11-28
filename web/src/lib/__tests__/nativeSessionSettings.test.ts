import { describe, expect, it } from "vitest"
import { effortChangeResult, effortChoices, modelChangeResult, modelChangeStands } from "../nativeSessionSettings"
import type { ModelSettingsState } from "../modelSettings"

const state = (reasoningEffort: unknown) => ({
  model: "a/m", overrides: {}, defaults: {}, effective: {},
  capabilities: { settings: { reasoningEffort } },
}) as unknown as ModelSettingsState

describe("nativeSessionSettings", () => {
  it("offers the efforts a model lists as supported choices, and none otherwise", () => {
    expect(effortChoices(state({ status: "supported", type: "enum", values: ["low", "high"] }))).toEqual(["low", "high"])
    expect(effortChoices(state({ status: "supported", type: "enum" }))).toEqual([])
    expect(effortChoices(state({ status: "unsupported", type: "enum", values: ["low"] }))).toEqual([])
    expect(effortChoices(state({ status: "supported", type: "number" }))).toEqual([])
    expect(effortChoices(undefined)).toEqual([])
  })

  it("takes the session's model, then the endpoint's, then the one asked for", () => {
    const settingsState = state(undefined)
    expect(modelChangeResult({ to: "x", settingsState }, "y")).toEqual({ model: "a/m", settingsState })
    expect(modelChangeResult({ to: "x" }, "y")).toEqual({ model: "x", settingsState: undefined })
    expect(modelChangeResult(null, "y")).toEqual({ model: "y", settingsState: undefined })
  })

  it("lets a change stand only while the session shows its models", () => {
    expect(modelChangeStands("a", "a", "b")).toBe(true)
    expect(modelChangeStands("b", "a", "b")).toBe(true)
    expect(modelChangeStands("c", "a", "b")).toBe(false)
  })

  it("reads the settings an effort change answered", () => {
    const settingsState = state(undefined)
    expect(effortChangeResult({ settingsState })).toBe(settingsState)
    expect(effortChangeResult(null)).toBeUndefined()
  })
})
