import { describe, expect, it } from "vitest"
import type { JobClone } from "../jobClones"
import { cloneLoadError, cloneNote, cloneRepositoriesOf, firstCloneable, ownCloneStart, pickedClone } from "../spawnClones"

const repositories = [
  { name: "Notes", repo: "", base_branch: "" },
  { name: "Legato", repo: "cpave3/legato", base_branch: "main" },
  { name: "Autopilot", repo: "autopilot-dev/autopilot" },
]

const clone = (id: number, repo: string, fields: Partial<JobClone> = {}): JobClone => ({
  id, repo, path: `/jobs/${id}`, branch: `b${id}`, task_ids: [`t${id}`], busy: false, last_used_at: "2026-11-01T00:00:00Z", unpushed: 0, ...fields,
})

describe("firstCloneable", () => {
  it("is the first repository with a GitHub name", () => {
    expect(firstCloneable(repositories)).toEqual({ name: "Legato", baseBranch: "main" })
  })
  it("is nothing without one", () => {
    expect(firstCloneable(null)).toEqual({ name: "", baseBranch: "" })
    expect(firstCloneable([{ name: "Notes", repo: "" }])).toEqual({ name: "", baseBranch: "" })
  })
})

describe("ownCloneStart", () => {
  it("is the task's own clone's repository, under its configured name", () => {
    expect(ownCloneStart([clone(1, "x/y"), clone(2, "autopilot-dev/autopilot")], "t2", repositories)).toEqual({ name: "Autopilot", baseBranch: "" })
  })
  it("names a repository no longer configured by its GitHub name", () => {
    expect(ownCloneStart([clone(3, "acme/widget")], "t3", undefined)).toEqual({ name: "acme/widget", baseBranch: "" })
  })
  it("is nothing for no task, or a task with no clone", () => {
    expect(ownCloneStart([clone(1, "x/y")], "", repositories)).toBeUndefined()
    expect(ownCloneStart([clone(1, "x/y")], "t9", repositories)).toBeUndefined()
  })
})

describe("cloneRepositoriesOf", () => {
  it("offers the repositories with a GitHub name, then those only clones remember", () => {
    expect(cloneRepositoriesOf(true, repositories, [clone(1, "cpave3/legato"), clone(2, "acme/widget"), clone(3, "acme/widget")])).toEqual([
      { name: "Legato", repo: "cpave3/legato", baseBranch: "main", offered: true },
      { name: "Autopilot", repo: "autopilot-dev/autopilot", baseBranch: "", offered: true },
      { name: "acme/widget", repo: "acme/widget", baseBranch: "", offered: false },
    ])
  })
  it("offers none of the settings' repositories while clones are off", () => {
    expect(cloneRepositoriesOf(false, repositories, [])).toEqual([])
    expect(cloneRepositoriesOf(true, null, [])).toEqual([])
  })
})

describe("pickedClone", () => {
  const offered = cloneRepositoriesOf(true, repositories, [])
  it("picks the repository named, else the first, and its first choice", () => {
    expect(pickedClone(offered, "Autopilot", [], null, "")).toMatchObject({ repository: offered[1], choices: [{ id: 0, label: "New clone" }], choice: { id: 0 } })
    expect(pickedClone(offered, "nothing", [], null, "").repository).toBe(offered[0])
  })
  it("picks the clone chosen, which exists", () => {
    const existing = clone(4, "cpave3/legato")
    const picked = pickedClone(offered, "Legato", [existing], 4, "")
    expect(picked.jobClone).toBe(existing)
    expect(picked.choices.map((choice) => choice.label)).toEqual(["New clone", "b4"])
  })
  it("picks nothing without a repository", () => {
    expect(pickedClone([], "", [], null, "")).toEqual({ repository: undefined, choices: [], choice: undefined, jobClone: undefined })
  })
})

describe("cloneNote", () => {
  it("says what a new clone, a busy clone and an idle clone do", () => {
    expect(cloneNote(undefined, "o/r")).toBe("A new clone of o/r. A branch that exists on GitHub is checked out; otherwise it is made from the base branch.")
    expect(cloneNote(clone(5, "o/r", { busy: true, branch: "" }), "o/r")).toBe("An agent is working in this clone, so this one joins it on its commit. The two can message each other.")
    expect(cloneNote(clone(6, "o/r"), "o/r")).toBe("Continues in /jobs/6 as it was left, with its build output. A branch named here is made at its current commit, stacked on b6.")
    expect(cloneNote(clone(6, "o/r", { branch: "" }), "o/r")).toContain("stacked on it.")
  })
})

it("says why clones could not be loaded", () => {
  expect(cloneLoadError(new Error("down"))).toBe("Could not load clones: down")
  expect(cloneLoadError("gone")).toBe("Could not load clones: gone")
})
