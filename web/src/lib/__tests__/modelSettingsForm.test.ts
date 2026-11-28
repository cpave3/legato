import { describe, expect, it } from "vitest"
import type { ModelSettingsState } from "../modelSettings"
import {
  changeDraft, currentThinking, defaultSettingLabel, hasSupportedSetting, needsThinking, parseNumberSetting, parseSelectedSetting,
  selectedSetting, settingOptionLabel, settingOptions, supportedCapability, unsupportedConfigured,
} from "../modelSettingsForm"

const state: ModelSettingsState = {
  model: "m", overrides: { reasoningEffort: "high" }, defaults: { thinking: "on", temperature: 1 }, effective: { thinking: "adaptive" },
  capabilities: { schemaVersion: 1, modelId: "m", profile: "p", source: "s", fingerprint: "f", settings: {
    thinking: { status: "supported", type: "enum", values: ["on", "off"] },
    reasoningEffort: { status: "supported", type: "enum", values: ["low", "high"] },
    temperature: { status: "unsupported", type: "number" },
  } },
}

describe("model settings form", () => {
  it("finds only supported capabilities", () => {
    expect(supportedCapability(state, "thinking")?.type).toBe("enum")
    expect(supportedCapability(state, "temperature")).toBeNull()
    expect(supportedCapability(null, "thinking")).toBeNull()
  })

  it("selects the draft, then nothing after reset, then the override", () => {
    expect(selectedSetting({ state, draft: { reasoningEffort: "low" }, reset: true }, "reasoningEffort")).toBe("low")
    expect(selectedSetting({ state, draft: {}, reset: true }, "reasoningEffort")).toBeNull()
    expect(selectedSetting({ state, draft: {}, reset: false }, "reasoningEffort")).toBe("high")
  })

  it("works out the thinking mode", () => {
    expect(currentThinking({ state, draft: { thinking: "off" }, reset: false })).toBe("off")
    expect(currentThinking({ state, draft: { thinking: null }, reset: false })).toBe("on")
    expect(currentThinking({ state, draft: {}, reset: true })).toBe("on")
    expect(currentThinking({ state, draft: {}, reset: false })).toBe("adaptive")
  })

  it("clears effort and budget when thinking turns off", () => {
    expect(changeDraft({ reasoningEffort: "low" }, "thinking", "off")).toEqual({ thinking: "off", reasoningEffort: null, thinkingBudgetTokens: null })
    expect(changeDraft({ reasoningEffort: "low" }, "thinking", "on")).toEqual({ thinking: "on", reasoningEffort: "low" })
    expect(needsThinking("thinkingBudgetTokens")).toBe(true)
    expect(needsThinking("verbosity")).toBe(false)
  })

  it("labels and parses values", () => {
    expect(defaultSettingLabel(undefined)).toBe("Default")
    expect(defaultSettingLabel(false)).toBe("Default (false)")
    expect(settingOptions({ status: "supported", type: "boolean" })).toEqual(["true", "false"])
    expect(settingOptions({ status: "supported", type: "enum" })).toEqual([])
    expect(settingOptionLabel("true")).toBe("On")
    expect(settingOptionLabel("false")).toBe("Off")
    expect(settingOptionLabel("max")).toBe("max")
    expect(parseSelectedSetting({ status: "supported", type: "boolean" }, "false")).toBe(false)
    expect(parseSelectedSetting({ status: "supported", type: "enum" }, "max")).toBe("max")
    expect(parseSelectedSetting({ status: "supported", type: "enum" }, "")).toBeNull()
    expect(parseNumberSetting("")).toBeNull()
    expect(parseNumberSetting("1.5")).toBe(1.5)
  })

  it("lists what the model does not take", () => {
    expect(hasSupportedSetting(state)).toBe(true)
    expect(hasSupportedSetting(null)).toBe(false)
    expect(unsupportedConfigured(state)).toEqual(["temperature"])
    expect(unsupportedConfigured(null)).toEqual([])
  })
})
