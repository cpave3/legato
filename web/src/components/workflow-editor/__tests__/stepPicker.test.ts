import { describe, expect, it } from "vitest"
import { pickerGroups } from "../stepPicker"
import { executors } from "../../../lib/workflowSchema"

describe("the step picker's list", () => {
  it("groups every offered step type as Agents, Code, Board and Cleanup", () => {
    const groups = pickerGroups("")
    expect(groups.map(g => g.group)).toEqual(["Agents", "Code", "Board", "Cleanup"])
    const listed = groups.flatMap(g => g.entries.map(e => e.type)).sort()
    expect(listed).toEqual(executors.filter(e => !e.internal).map(e => e.type).sort())
    expect(groups[0].entries.map(e => e.type)).toEqual(["agent.run", "agent.dispatch", "agent.stop"])
  })

  it("never offers an internal step type", () => {
    expect(pickerGroups("learning").flatMap(g => g.entries)).toEqual([])
    expect(pickerGroups("interview").flatMap(g => g.entries)).toEqual([])
  })

  it("finds by name, type and words of the description, dropping empty groups", () => {
    expect(pickerGroups("move").flatMap(g => g.entries.map(e => e.type))).toEqual(["task.move"])
    expect(pickerGroups("github.pr").map(g => g.group)).toEqual(["Code"])
    expect(pickerGroups("WORKTREE").flatMap(g => g.entries.map(e => e.type))).toEqual(expect.arrayContaining(["worktree.remove", "github.pr.prepare"]))
    expect(pickerGroups("zzz")).toEqual([])
  })

  it("gives each a short phrase: the description's first sentence", () => {
    const run = pickerGroups("").flatMap(g => g.entries).find(e => e.type === "agent.run")!
    expect(run.phrase).toBe("Start an agent of the workflow on a prompt or a macro, and wait for it to complete the step with the outputs the step declares")
    const stop = pickerGroups("").flatMap(g => g.entries).find(e => e.type === "agent.stop")!
    expect(stop.phrase).toBe("Stop a task's agent")
  })
})
