import { readdirSync, readFileSync, statSync } from "node:fs"
import { join, relative } from "node:path"
import { describe, expect, it } from "vitest"

// The graph library is loaded only by the pages that draw a workflow graph:
// React Flow and dagre are imported by the graph component alone, and the
// component only through a dynamic import, so they stay in its own chunk.
const src = join(__dirname, "..", "..", "..")
const graphDir = join("components", "workflow-graph")

function sources(dir: string): string[] {
  return readdirSync(dir).flatMap(name => {
    const path = join(dir, name)
    if (statSync(path).isDirectory()) return name === "__tests__" || name === "test" ? [] : sources(path)
    return /\.(ts|tsx)$/.test(name) && !/\.test\.tsx?$/.test(name) ? [path] : []
  })
}

describe("the workflow graph chunk", () => {
  it("is the only code that imports React Flow or dagre", () => {
    const offenders = sources(src).filter(path => !relative(src, path).startsWith(graphDir)).filter(path => /from\s+["'](@xyflow\/react|@dagrejs\/dagre)/.test(readFileSync(path, "utf8")))
    expect(offenders.map(path => relative(src, path))).toEqual([])
  })

  it("is reached only through a dynamic import", () => {
    const offenders = sources(src).filter(path => /^import\s+(?!type\b).*workflow-graph\/WorkflowGraph["']/m.test(readFileSync(path, "utf8")))
    expect(offenders.map(path => relative(src, path))).toEqual([])
  })
})
