import { readdirSync, readFileSync, statSync } from "node:fs"
import { join, relative } from "node:path"
import { describe, expect, it } from "vitest"

// The graph library is loaded only by the pages that draw a graph (a
// workflow's, or the agents'): React Flow and dagre are imported by the
// graph components alone, and each component only through a dynamic
// import, so they stay in their own chunks.
const src = join(__dirname, "..", "..", "..")
const graphDirs = [join("components", "workflow-graph"), join("components", "agent-graph"), join("components", "workflow-map")]

function sources(dir: string): string[] {
  return readdirSync(dir).flatMap(name => {
    const path = join(dir, name)
    if (statSync(path).isDirectory()) return name === "__tests__" || name === "test" ? [] : sources(path)
    return /\.(ts|tsx)$/.test(name) && !/\.test\.tsx?$/.test(name) ? [path] : []
  })
}

describe("the graph chunks", () => {
  it("is the only code that imports React Flow or dagre", () => {
    const offenders = sources(src).filter(path => !graphDirs.some(dir => relative(src, path).startsWith(dir))).filter(path => /from\s+["'](@xyflow\/react|@dagrejs\/dagre)/.test(readFileSync(path, "utf8")))
    expect(offenders.map(path => relative(src, path))).toEqual([])
  })

  it("is reached only through a dynamic import", () => {
    const offenders = sources(src).filter(path => /^import\s+(?!type\b).*(workflow-graph\/WorkflowGraph|agent-graph\/AgentGraphCanvas|workflow-map\/WorkflowMapCanvas)["']/m.test(readFileSync(path, "utf8")))
    expect(offenders.map(path => relative(src, path))).toEqual([])
  })
})
