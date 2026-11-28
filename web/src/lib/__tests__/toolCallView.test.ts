import { describe, expect, it } from "vitest"
import { bashView, editView, fileName, patchLineClass, patchLines, patchView, readView, toolCallParts, writeView } from "../toolCallView"
import type { NativeToolItem } from "../nativeAgentSync"

const call = (toolName: string, args: Record<string, unknown>, result?: Record<string, unknown>, summary?: string): NativeToolItem =>
  ({ key: "k", toolName, args, result, summary })

describe("toolCallView", () => {
  it("names a file by the last part of its path", () => {
    expect(fileName("src/a/b.ts")).toBe("b.ts")
    expect(fileName("b.ts")).toBe("b.ts")
    // A path ending in a slash has no last part: the path stands.
    expect(fileName("src/")).toBe("src/")
  })

  it("gives empty parts for a call that has none", () => {
    expect(toolCallParts({ key: "k", toolName: "x", args: undefined as unknown as Record<string, unknown> })).toEqual({ args: {}, result: {}, summary: "" })
  })

  it("reads an edit", () => {
    expect(editView(call("edit", { path: "a/b.ts", old_string: "x", new_string: "y" }, { replacements: 2 }))).toEqual({
      path: "a/b.ts", filename: "b.ts", oldStr: "x", newStr: "y", replacements: 2,
    })
    expect(editView(call("edit", {}))).toEqual({ path: "", filename: "", oldStr: "", newStr: "", replacements: undefined })
  })

  it("reads a patch, its summary first", () => {
    const changes = [{ path: "a.go", kind: "update", diff: "" }]
    expect(patchView(call("patch", { changes }, { error: "conflict" }, "a.go"))).toEqual({ summary: "a.go", changes, failure: "conflict" })
    expect(patchView(call("patch", { changes: "not a list" }))).toEqual({ summary: "", changes: [], failure: undefined })
  })

  it("reads a write", () => {
    expect(writeView(call("write", { path: "d/f.txt", content: "hi" }, { bytes_written: 2, created: true }))).toEqual({
      path: "d/f.txt", filename: "f.txt", content: "hi", bytesWritten: 2, created: true,
    })
  })

  it("reads a shell command", () => {
    expect(bashView(call("bash", { command: "ls" }, { stdout: "a", stderr: "b", exit_code: 1 }))).toEqual({ command: "ls", stdout: "a", stderr: "b", exitCode: 1 })
    expect(bashView(call("bash", {}))).toEqual({ command: "", stdout: "", stderr: "", exitCode: undefined })
  })

  it("reads a read", () => {
    expect(readView(call("read", { path: "/r/README.md" }))).toEqual({ path: "/r/README.md", filename: "README.md" })
  })

  it("draws a new file as additions and a deleted one as removals", () => {
    expect(patchLines({ path: "n", kind: "add", diff: "a\nb\n" })).toEqual(["+a", "+b"])
    expect(patchLines({ path: "n", kind: "delete", diff: "a\n" })).toEqual(["-a"])
    expect(patchLines({ path: "n", kind: "update", diff: "@@\n+a\n" })).toEqual(["@@", "+a"])
  })

  it("colours a diff line by what it is", () => {
    expect(patchLineClass("@@ -1 +1 @@")).toBe("text-sky-300")
    expect(patchLineClass("+a")).toBe("bg-green-900/30 text-green-300")
    expect(patchLineClass("-a")).toBe("bg-red-900/30 text-red-300")
    expect(patchLineClass(" a")).toBe("text-zinc-400")
  })
})
