import { describe, expect, it } from "vitest"
import { advertCanClone, handoffBlock, handoffTools, missingOnWorker, workerOnlyRepositories, type Worker, type WorkerAdvert } from "../workers"

const advert: WorkerAdvert = {
  version: "dev", features: [], sandbox: true, mcp_tools: true, mcp_connections: ["burnlite"], skills: ["tdd"],
  repositories: ["acme/*", "cpave3/legato"], default_agent: "codex",
  agents: [
    { name: "claude-code", installed: true, tiers: [], prompts_off: true },
    { name: "chimera", installed: false, tiers: [], prompts_off: true },
    { name: "codex", installed: true, tiers: ["fast"], prompts_off: true },
  ],
}
const worker = (over: Partial<Worker> = {}): Worker => ({
  instance_id: "srv-1", address: "https://vm-1", name: "vm-1", main_name: "workstation", sandbox: true, handoff_version: 2,
  paired_at: "2026-11-05T09:00:00Z", advert, ...over,
})

describe("advertCanClone", () => {
  it("matches owner/name and owner/*, ignoring case", () => {
    expect(advertCanClone(advert, "acme/widget")).toBe(true)
    expect(advertCanClone(advert, "Cpave3/Legato")).toBe(true)
    expect(advertCanClone(advert, "cpave3/other")).toBe(false)
  })
  it("assumes a worker with no advert, or no list, can clone anything: it says so itself", () => {
    expect(advertCanClone(undefined, "anyone/anything")).toBe(true)
    expect(advertCanClone({ ...advert, repositories: null }, "anyone/anything")).toBe(true)
  })
})

describe("workerOnlyRepositories", () => {
  it("lists the worker's named repositories the main does not offer, never a pattern", () => {
    expect(workerOnlyRepositories(advert, ["acme/widget"])).toEqual(["cpave3/legato"])
    expect(workerOnlyRepositories(advert, ["cpave3/legato"])).toEqual([])
  })
})

describe("handoffBlock", () => {
  it("says why a worker can't take handoffs", () => {
    expect(handoffBlock(worker())).toBeNull()
    expect(handoffBlock(worker({ sandbox: false }))).toMatch(/sandbox off/)
    expect(handoffBlock(worker({ cannot_take_handoffs: "can't take handoffs: its board has no columns" }))).toBe("can't take handoffs: its board has no columns")
  })
})

describe("handoffTools", () => {
  it("lists the worker's installed tools that run unattended, its default first", () => {
    expect(handoffTools(advert, ["claude-code", "chimera"])).toEqual(["codex", "claude-code"])
  })
  it("falls back to this Legato's tools without an advert", () => {
    expect(handoffTools(undefined, ["claude-code", "chimera"])).toEqual(["claude-code", "chimera"])
  })
})

describe("missingOnWorker", () => {
  it("names the skills and MCP connections this Legato has and the worker lacks", () => {
    expect(missingOnWorker(advert, { skills: ["tdd", "pr"], mcpConnections: ["burnlite", "linear"] })).toEqual(["skill pr", "MCP connection linear"])
  })
  it("says the Legato tools are off there", () => {
    expect(missingOnWorker({ ...advert, mcp_tools: false }, { skills: [], mcpConnections: [] })).toEqual(["Legato's agent tools (add_task_note and the rest)"])
  })
  it("says nothing without an advert", () => {
    expect(missingOnWorker(undefined, { skills: ["pr"], mcpConnections: [] })).toEqual([])
  })
})
