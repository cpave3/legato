import { existsSync, readdirSync, readFileSync, statSync } from "node:fs"
import { dirname, join, relative, sep } from "node:path"
import ts from "typescript"
import { describe, expect, it } from "vitest"

// No import cycles in web/src, between folders or between files. A folder
// that imports a folder which imports it back (directly or round a longer
// loop) has no layer of its own: shared code belongs a layer down, in lib/
// or a leaf folder, never in a re-export that keeps the loop. Type-only and
// dynamic imports count, as they do for the code-health report.
const src = join(__dirname, "..")

interface Edge {
  from: string
  to: string
  line: number
}

function sources(dir: string): string[] {
  return readdirSync(dir).flatMap(name => {
    const path = join(dir, name)
    if (statSync(path).isDirectory()) return name === "__tests__" || (dir === src && name === "test") ? [] : sources(path)
    return /\.tsx?$/.test(name) && !/\.(test|spec)\.tsx?$/.test(name) && !name.endsWith(".d.ts") ? [path] : []
  })
}

function resolve(fromFile: string, spec: string): string | undefined {
  let base: string
  if (spec.startsWith("@/")) base = join(src, spec.slice(2))
  else if (spec.startsWith(".")) base = join(dirname(fromFile), spec)
  else return undefined
  const candidates = [base, `${base}.ts`, `${base}.tsx`, join(base, "index.ts"), join(base, "index.tsx")]
  return candidates.find(path => /\.tsx?$/.test(path) && existsSync(path) && statSync(path).isFile())
}

function importEdges(): Edge[] {
  return sources(src).flatMap(file => {
    const text = readFileSync(file, "utf8")
    const { importedFiles } = ts.preProcessFile(text, true, true)
    return importedFiles.flatMap(ref => {
      const target = resolve(file, ref.fileName)
      if (!target || target === file) return []
      const line = text.slice(0, ref.pos).split("\n").length
      return [{ from: relative(src, file), to: relative(src, target), line }]
    })
  })
}

// Tarjan's strongly connected components; only those of more than one node
// are loops.
function cycles(nodes: Iterable<string>, next: (node: string) => string[]): string[][] {
  let index = 0
  const indices = new Map<string, number>()
  const low = new Map<string, number>()
  const stack: string[] = []
  const onStack = new Set<string>()
  const found: string[][] = []
  const visit = (node: string) => {
    indices.set(node, index)
    low.set(node, index)
    index++
    stack.push(node)
    onStack.add(node)
    for (const to of next(node)) {
      if (!indices.has(to)) {
        visit(to)
        low.set(node, Math.min(low.get(node)!, low.get(to)!))
      } else if (onStack.has(to)) {
        low.set(node, Math.min(low.get(node)!, indices.get(to)!))
      }
    }
    if (low.get(node) !== indices.get(node)) return
    const component: string[] = []
    let popped: string
    do {
      popped = stack.pop()!
      onStack.delete(popped)
      component.push(popped)
    } while (popped !== node)
    if (component.length > 1) found.push(component.sort())
  }
  for (const node of nodes) if (!indices.has(node)) visit(node)
  return found
}

const folderOf = (file: string) => dirname(file).split(sep).join("/")

function report(components: string[][], edges: Edge[], unit: (file: string) => string): string[] {
  return components.map(component => {
    const members = new Set(component)
    // Grouped by pair, so the message names each import that joins two
    // members of the loop without listing every one.
    const pairs = new Map<string, Edge[]>()
    for (const e of edges) {
      const from = unit(e.from)
      const to = unit(e.to)
      if (from === to || !members.has(from) || !members.has(to)) continue
      const key = `${from} -> ${to}`
      pairs.set(key, [...(pairs.get(key) ?? []), e])
    }
    const lines = [...pairs].sort(([a], [b]) => a.localeCompare(b)).map(([key, sites]) => `  ${key}: ${sites.length} import(s), first ${sites[0].from}:${sites[0].line}`)
    return [`loop: ${component.join(" <-> ")}`, ...lines].join("\n")
  })
}

function graph(edges: Edge[], unit: (file: string) => string) {
  const next = new Map<string, Set<string>>()
  for (const e of edges) {
    const from = unit(e.from)
    const to = unit(e.to)
    if (!next.has(from)) next.set(from, new Set())
    if (!next.has(to)) next.set(to, new Set())
    if (from !== to) next.get(from)!.add(to)
  }
  return cycles(next.keys(), node => [...next.get(node)!])
}

describe("the web/src import graph", () => {
  const edges = importEdges()

  it("sees the imports, type-only and dynamic ones included", () => {
    expect(edges.length).toBeGreaterThan(1000)
    expect(edges.some(e => e.from === "main.tsx" && e.to === "App.tsx")).toBe(true)
  })

  it("has no loop between folders", () => {
    expect(report(graph(edges, folderOf), edges, folderOf)).toEqual([])
  })

  it("has no loop between files", () => {
    const file = (f: string) => f
    expect(report(graph(edges, file), edges, file)).toEqual([])
  })
})
