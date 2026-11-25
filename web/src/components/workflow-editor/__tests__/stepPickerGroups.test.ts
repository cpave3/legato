import { describe, expect, it } from "vitest"
import { pickerGroups } from "../stepPickerGroups"
import { executors } from "../../../lib/workflowSchema"

describe("the step picker's list", () => {
  it("groups every step type as Agents, Code, Board, Chat, Learn and Cleanup", () => {
    const groups = pickerGroups("")
    expect(groups.map(g => g.group)).toEqual(["Agents", "Code", "Board", "Chat", "Learn", "Cleanup"])
    expect(groups.find(g => g.group === "Chat")?.entries.map(e => e.type)).toEqual(["chat.message.send", "chat.reaction.add", "chat.reaction.remove", "chat.thread.read"])
    const listed = groups.flatMap(g => g.entries.map(e => e.type)).sort()
    expect(listed).toEqual(executors.map(e => e.type).sort())
    expect(groups[0].entries.map(e => e.type)).toEqual(["agent.run", "agent.dispatch", "agent.stop", "agent.compact"])
  })

  it("offers Learn's steps for any workflow", () => {
    expect(pickerGroups("learning").map(g => g.group)).toEqual(["Learn"])
    expect(pickerGroups("learn").find(g => g.group === "Learn")?.entries.map(e => e.type)).toEqual(["agent.interview", "learning.packet.prepare", "learning.proposals.submit"])
  })

  it("finds the chat steps by their words", () => {
    expect(pickerGroups("slack").flatMap(g => g.entries.map(e => e.type))).toEqual(["chat.message.send", "chat.reaction.add", "chat.thread.read"])
    expect(pickerGroups("reaction").flatMap(g => g.entries.map(e => e.type))).toEqual(["chat.reaction.add", "chat.reaction.remove"])
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
