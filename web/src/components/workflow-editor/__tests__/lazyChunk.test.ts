import { readdirSync, readFileSync, statSync } from "node:fs"
import { join, relative } from "node:path"
import { describe, expect, it } from "vitest"

// The YAML editor is loaded only by the pages that edit a workflow:
// CodeMirror is imported by the editor component alone, and the component
// only through a dynamic import, so it and the schema stay in its chunk.
const src = join(__dirname, "..", "..", "..")
const editorDir = join("components", "workflow-editor")

function sources(dir: string): string[] {
  return readdirSync(dir).flatMap(name => {
    const path = join(dir, name)
    if (statSync(path).isDirectory()) return name === "__tests__" || name === "test" ? [] : sources(path)
    return /\.(ts|tsx)$/.test(name) && !/\.test\.tsx?$/.test(name) ? [path] : []
  })
}

describe("the workflow editor chunk", () => {
  it("is the only code that imports CodeMirror", () => {
    const offenders = sources(src).filter(path => !relative(src, path).startsWith(editorDir)).filter(path => /from\s+["']@codemirror\//.test(readFileSync(path, "utf8")))
    expect(offenders.map(path => relative(src, path))).toEqual([])
  })

  it("is reached only through a dynamic import", () => {
    const offenders = sources(src).filter(path => /^import\s+(?!type\b).*workflow-editor\/(WorkflowYamlEditor|WorkflowBuilder)["']/m.test(readFileSync(path, "utf8")))
    expect(offenders.map(path => relative(src, path))).toEqual([])
  })

  it("keeps the schema out of the pages", () => {
    const offenders = sources(join(src, "pages")).filter(path => /^import\s+(?!type\b).*lib\/workflow(Schema|Expressions|Document)["']/m.test(readFileSync(path, "utf8")))
    expect(offenders.map(path => relative(src, path))).toEqual([])
  })
})
