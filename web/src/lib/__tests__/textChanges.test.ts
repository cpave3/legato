import { describe, expect, it } from "vitest"
import { applyChanges, textChanges } from "../textChanges"
import { renameStep, setField } from "../workflowDocument"

const file = `schema_version: 1
name: x
title: X
steps:
  - id: first
    title: First # kept
    type: agent.run
    with:
      prompt: hi
  - id: second
    title: Second
    type: task.note
    with:
      body: \${{ steps.first.outputs.summary }}
`

describe("textChanges", () => {
  it("is nothing for the same text", () => {
    expect(textChanges(file, file)).toEqual([])
  })

  it("is one small change for a field the document model set", () => {
    const after = setField(file, ["steps", 1, "title"], "Later").text
    const changes = textChanges(file, after)
    expect(changes).toEqual([{ from: file.indexOf("Second"), to: file.indexOf("Second") + "Sec".length + 3, insert: "Later" }])
    expect(applyChanges(file, changes)).toBe(after)
  })

  it("keeps apart changes far from each other, as a rename makes", () => {
    const after = renameStep(file, "first", "prep").text
    const changes = textChanges(file, after)
    expect(changes.length).toBe(2)
    expect(applyChanges(file, changes)).toBe(after)
    // Between them, nothing is replaced: a cursor there stays put.
    expect(changes[0].to).toBeLessThan(file.indexOf("title: Second"))
    expect(changes[1].from).toBeGreaterThan(file.indexOf("title: Second"))
  })

  it("gives the text back for any two texts", () => {
    const pairs = [["", "abc"], ["abc", ""], ["a\nb\nc\n", "a\nc\n"], ["a\nc\n", "a\nb\nc\n"], ["x\ny\nz", "q\ny\nw"], ["same\n", "same\nmore\n"]]
    for (const [before, after] of pairs) expect(applyChanges(before, textChanges(before, after)), JSON.stringify([before, after])).toBe(after)
  })
})
