import { describe, expect, it } from "vitest"
import type { EngramPicker } from "../engrams"
import { canRememberEngram, engramGroups, engramOffered, engramOptionLabel, engramPlaceWords, engramPreselect } from "../spawnEngram"

const picker = (fields: Partial<EngramPicker> = {}): EngramPicker => ({
  enabled: true,
  place: { key: "repo:x", label: "github.com/x", kind: "repo", default: "argus" },
  engrams: [
    { name: "mneme", summary: "", mark: "last" },
    { name: "argus", summary: "", mark: "default", bound: true },
    { name: "hestia", summary: "", bound: true },
    { name: "clio", summary: "" },
  ],
  ...fields,
})

describe("engramGroups", () => {
  it("puts the place's engrams first, the rest after", () => {
    const groups = engramGroups(picker())
    expect(groups.place.map((choice) => choice.name)).toEqual(["mneme", "argus", "hestia"])
    expect(groups.other.map((choice) => choice.name)).toEqual(["clio"])
  })
  it("has none before an answer", () => {
    expect(engramGroups(null)).toEqual({ place: [], other: [] })
  })
})

it("names a folder's place, and a repository's otherwise", () => {
  expect(engramPlaceWords(picker({ place: { key: "dir:x", label: "~/x", kind: "folder" } }))).toEqual({ word: "This folder", noun: "folder" })
  expect(engramPlaceWords(picker())).toEqual({ word: "This repo", noun: "repo" })
  expect(engramPlaceWords(null)).toEqual({ word: "This repo", noun: "repo" })
})

it("offers engrams for a tool that takes a launch prompt", () => {
  expect(engramOffered(picker(), "codex")).toBe(true)
  expect(engramOffered(picker(), "shell")).toBe(false)
  expect(engramOffered(picker({ enabled: false }), "codex")).toBe(false)
  expect(engramOffered(null, "codex")).toBe(false)
})

describe("canRememberEngram", () => {
  it("offers to bind an engram that is not the default", () => {
    expect(canRememberEngram(true, picker(), "mneme")).toBe(true)
    expect(canRememberEngram(true, picker(), "argus")).toBe(false)
  })
  it("offers to clear a default, and nothing where there is none", () => {
    expect(canRememberEngram(true, picker(), "")).toBe(true)
    expect(canRememberEngram(true, picker({ place: { key: "k", label: "l", kind: "repo" } }), "")).toBe(false)
  })
  it("offers nothing while hidden or without a place", () => {
    expect(canRememberEngram(false, picker(), "mneme")).toBe(false)
    expect(canRememberEngram(true, picker({ place: undefined }), "mneme")).toBe(false)
  })
})

it("labels an engram with its mark", () => {
  expect(engramOptionLabel({ name: "mneme", summary: "", mark: "last" })).toBe("mneme · last on this task")
  expect(engramOptionLabel({ name: "clio", summary: "" })).toBe("clio")
})

it("picks first what the answer pre-selects, when engrams are offered", () => {
  expect(engramPreselect(picker({ preselect: "mneme" }))).toBe("mneme")
  expect(engramPreselect(picker())).toBe("")
  expect(engramPreselect(picker({ enabled: false, preselect: "mneme" }))).toBe("")
})
