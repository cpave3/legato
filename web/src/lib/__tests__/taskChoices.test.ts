import { describe, expect, it } from "vitest"
import { completedWord, offeredTasks, taskGroups, taskText, type SelectableTask } from "../taskChoices"

const task = (id: string, title: string, workspace_name: string, status = "Backlog", archived?: boolean) => ({ id, title, workspace_name, status, ...(archived ? { archived } : {}) }) as unknown as SelectableTask

describe("taskChoices", () => {
  const active = [task("B", "Beta", "Web"), task("D", "Done one", "Web", "DONE"), task("A", "Alpha", "")]
  const archived = [task("Z", "Gone", "Web", "Done", true)]

  it("offers the open tasks, or every task and the archived ones", () => {
    expect(offeredTasks(active, archived, false).map(t => t.id)).toEqual(["B", "A"])
    expect(offeredTasks(active, archived, true).map(t => t.id)).toEqual(["B", "D", "A", "Z"])
  })

  it("groups by workspace, A to Z with Unassigned last, each task once and A to Z", () => {
    const groups = taskGroups([...active, task("B", "Beta", "Web"), task("C", "Core", "Api")])
    expect(groups.map(([name, tasks]) => [name, tasks.map(t => t.id)])).toEqual([["Api", ["C"]], ["Web", ["B", "D"]], ["Unassigned", ["A"]]])
  })

  it("names a task and says when it is done or archived", () => {
    expect(taskText(active[0])).toBe("B · Beta")
    expect(completedWord(active[0])).toBeNull()
    expect(completedWord(active[1])).toBe("Done")
    expect(completedWord(archived[0])).toBe("Archived")
  })
})
