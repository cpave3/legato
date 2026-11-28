import { describe, expect, it } from "vitest"
import { activeSlashIndex, filterSlashItems, slashCompletion, slashListKey, slashQueryOf } from "../slashCompletion"
import type { SlashItem } from "../slashMenu"

const command: SlashItem = { type: "command", name: "review", description: "", body: "Please review", source: "user" }
const bare: SlashItem = { type: "command", name: "plain", description: "", source: "user" }
const skill: SlashItem = { type: "skill", name: "tdd", description: "", source: "user" }

describe("slashCompletion", () => {
  it("searches the browser's query, else the name after a slash that is the whole draft", () => {
    expect(slashQueryOf("/rev", false, "")).toBe("rev")
    expect(slashQueryOf("/", false, "")).toBe("")
    expect(slashQueryOf("/rev more", false, "")).toBeUndefined()
    expect(slashQueryOf("hello", false, "")).toBeUndefined()
    expect(slashQueryOf("hello", true, "td")).toBe("td")
  })

  it("filters by kind only in the browser", () => {
    expect(filterSlashItems([command, skill], true, "skill")).toEqual([skill])
    expect(filterSlashItems([command, skill], true, "all")).toEqual([command, skill])
    expect(filterSlashItems([command, skill], false, "skill")).toEqual([command, skill])
  })

  it("keeps the highlight while its list stands, within the list", () => {
    expect(slashListKey([{ item: command } as never, { item: skill } as never])).toBe("command:review\nskill:tdd")
    expect(activeSlashIndex({ list: "a", index: 3 }, "a", 2)).toBe(1)
    expect(activeSlashIndex({ list: "a", index: 1 }, "b", 2)).toBe(0)
    expect(activeSlashIndex({ list: "a", index: 1 }, "a", 0)).toBe(0)
  })

  it("expands a command with a body, and makes anything else /name", () => {
    expect(slashCompletion(command, "/rev", false)).toBe("Please review")
    expect(slashCompletion(bare, "/pl", false)).toBe("/plain ")
    expect(slashCompletion(skill, "/t", false)).toBe("/tdd ")
  })

  it("joins what was written when picked from the browser", () => {
    expect(slashCompletion(skill, "fix the bug", true)).toBe("/tdd fix the bug")
    expect(slashCompletion(bare, "fix the bug", true)).toBe("/plain fix the bug")
    expect(slashCompletion(command, "fix the bug", true)).toBe("fix the bug\nPlease review")
    expect(slashCompletion(command, "", true)).toBe("Please review")
  })
})
