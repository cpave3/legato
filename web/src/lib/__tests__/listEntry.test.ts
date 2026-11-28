import { describe, expect, it } from "vitest"
import type { ConfigList, ConfigListEntry } from "../configSettings"
import type { FormField, ListForm } from "../listForms"
import {
  asObject, duplicate, entrySummary, entryValue, fieldValue, firstError, parsedValue, resolveField, savedNotice, setEntryField, sortedEntries, tierRows, tierRowsError, tiersOf,
  unusedName, writeField,
} from "../listEntry"

const entry = (name: string, value: unknown): ConfigListEntry => ({ name, value, source: "database", pinned: false })
const list = (key: string, entries: ConfigListEntry[], mode: ConfigList["mode"] = "live"): ConfigList => ({ key, title: key, class: "change", mode, entries })
const form = (fields: FormField[], extra: Partial<ListForm> = {}): ListForm => ({ name: { label: "Name" }, fields, empty: "", add: "Add", ...extra })

describe("list entries", () => {
  it("reads and writes dotted fields, dropping emptied objects", () => {
    expect(asObject([1])).toEqual({})
    expect(asObject(null)).toEqual({})
    expect(fieldValue({ a: { b: 1 } }, "a.b")).toBe(1)
    expect(fieldValue("x", "a.b")).toBeUndefined()
    expect(writeField({ a: { b: 1, c: 2 } }, "a.b", undefined)).toEqual({ a: { c: 2 } })
    expect(writeField({ a: { b: 1 } }, "a.b", undefined)).toEqual({})
    expect(writeField({}, "a.b", 3)).toEqual({ a: { b: 3 } })
  })

  it("names rows", () => {
    expect(unusedName("tier", ["tier-2"])).toBe("tier-3")
    expect(unusedName("tier", [])).toBe("tier-1")
    expect(duplicate([" a", "b", "a "])).toBe("a")
    expect(duplicate(["a", "", ""])).toBe("")
  })

  it("edits a value as its form does", () => {
    expect(entryValue(form([], { scalar: { key: "", label: "Value", kind: "text" } }), 3)).toBe("")
    expect(entryValue(form([], { scalar: { key: "", label: "Value", kind: "text" } }), "on")).toBe("on")
    expect(entryValue(form([]), "on")).toEqual({})
    expect(parsedValue(form([], { scalar: { key: "", label: "Value", kind: "text" } }), 5)).toBe("5")
    expect(parsedValue(form([], { scalar: { key: "", label: "Value", kind: "text" } }), null)).toBe("")
    expect(parsedValue(form([]), { a: 1 })).toEqual({ a: 1 })
    expect(firstError({ a: "", b: "two", c: "three" })).toBe("two")
    expect(firstError({})).toBe("")
  })

  it("clears a profile's tier with its agent, and fields that stop applying", () => {
    expect(setEntryField("usage.profiles", form([]), "p", { agent: "a", tier: "t" }, "agent", "b")).toEqual({ agent: "b" })
    expect(setEntryField("usage.profiles", form([]), "p", { reviewer: { agent: "a", tier: "t" } }, "reviewer.agent", "b")).toEqual({ reviewer: { agent: "b" } })
    expect(setEntryField("other", form([]), "p", { agent: "a", tier: "t" }, "agent", "b")).toEqual({ agent: "b", tier: "t" })
    const when = form([{ key: "ready", label: "R", kind: "check", when: { key: "mode", is: "draft" } }])
    expect(setEntryField("rules", when, "r", { mode: "draft", ready: true }, "mode", "ready")).toEqual({ mode: "ready" })
  })

  it("fills in choices from elsewhere", () => {
    const lists = [
      list("claude_accounts", [entry("work", { label: "Work" }), entry("home", {})]),
      list("usage.profiles", [entry("Economy", {})]),
      list("adapters", [entry("codex", { tiers: { fast: {} } })]),
    ]
    const field = (optionsFrom: FormField["optionsFrom"], agentKey?: string): FormField => ({ key: "k", label: "K", kind: "choice", optionsFrom, agentKey })
    expect(resolveField({ key: "k", label: "K", kind: "text" }, {}, lists, []).choices).toBeUndefined()
    expect(resolveField(field("claude_accounts"), {}, lists, []).choices).toEqual([["work", "Work"], ["home", "home"]])
    expect(resolveField(field("usage_meters"), {}, lists, ["m"]).choices).toEqual([["m", "m"]])
    expect(resolveField(field("profiles"), {}, lists, []).choices).toEqual([["Economy", "Economy"]])
    expect(resolveField(field("tiers"), { agent: "codex" }, lists, []).choices).toEqual([["fast", "fast"]])
    expect(resolveField(field("tiers", "reviewer.agent"), { reviewer: { agent: "none" } }, lists, []).choices).toEqual([])
    expect(resolveField(field("profiles"), {}, [], []).choices).toEqual([])
  })

  it("summarises an entry", () => {
    const fields: FormField[] = [
      { key: "mode", label: "Mode", kind: "choice", choices: [["d", "Draft"]] },
      { key: "color", label: "Colour", kind: "swatches" },
      { key: "ready", label: "Ready", kind: "check" },
      { key: "args", label: "Args", kind: "lines" },
      { key: "path", label: "Path", kind: "text" },
    ]
    expect(entrySummary(form(fields), entry("e", { mode: "d", color: "#fff", ready: true, args: ["a", "b"], path: "" }))).toBe("Draft · Ready · a b")
    expect(entrySummary(form(fields), entry("e", { mode: "x", ready: false }))).toBe("x")
    expect(entrySummary(form([], { scalar: { key: "", label: "V", kind: "text" } }), entry("e", "on"))).toBe("on")
    expect(entrySummary(form([], { scalar: { key: "", label: "V", kind: "text" } }), entry("e", 1))).toBe("")
    expect(entrySummary(form([], { summary: (name) => `is ${name}` }), entry("e", {}))).toBe("is e")
  })

  it("orders usage rules and words a save by its mode", () => {
    const rules = list("usage.rules", [entry("b", { order: 2 }), entry("c", { order: 1 }), entry("a", { order: 2 })])
    expect(sortedEntries(rules).map((e) => e.name)).toEqual(["c", "a", "b"])
    const other = list("x", [entry("b", {}), entry("a", {})])
    expect(sortedEntries(other)).toBe(other.entries)
    expect(savedNotice("restart")).toBe("Saved. It applies when Legato restarts.")
    expect(savedNotice("new_agents")).toBe("Saved. It applies to agents started from now on.")
    expect(savedNotice("live")).toBe("Saved.")
  })

  it("turns tiers into rows and back, keeping what a row does not show", () => {
    const rows = tierRows({ deep: { description: "Hard", launch_args: ["--a", "--b"], keep: 1 }, quick: {} })
    expect(rows[0]).toMatchObject({ name: "deep", description: "Hard", args: "--a\n--b" })
    expect(tierRowsError(rows)).toBe("")
    expect(tiersOf(rows)).toEqual({ deep: { keep: 1, description: "Hard", launch_args: ["--a", "--b"] }, quick: {} })
    expect(tiersOf([{ ...rows[0], name: " deep ", description: " ", args: "\n" }])).toEqual({ deep: { keep: 1 } })
    expect(tiersOf([])).toBeUndefined()
    expect(tierRowsError([...rows, { ...rows[1] }])).toBe("Two tiers are named quick.")
    expect(tierRowsError([{ ...rows[0], name: " " }])).toBe("Give every tier a name.")
  })
})
