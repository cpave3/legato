import { originalPositionFor, TraceMap, type SourceMapInput } from "@jridgewell/trace-mapping"
import type { RawFrame, ResolveContext, ResolvedSource, SourceResolver } from "./types"

// Vite's resolver. A Vite dev server serves a project file at its path
// from the project's root (/src/App.tsx), a file outside the root under
// /@fs/<absolute path>, and pre-bundled dependencies under node_modules;
// it adds ?t=… after a hot update and ?v=… to dependencies. Each module it
// serves ends with an inline source map.

// The project file a module URL path names, or null for what is not one.
export function viteFileOf(pathname: string): string | null {
  let path: string
  try {
    path = decodeURIComponent(pathname)
  } catch {
    return null
  }
  if (path.includes("/node_modules/") || /^\/@(?!fs\/)/.test(path) || path.startsWith("/__")) return null
  if (path.startsWith("/@fs/")) {
    const file = path.slice("/@fs".length)
    // /@fs/C:/project/… on Windows.
    return /^\/[A-Za-z]:\//.test(file) ? file.slice(1) : file
  }
  return path.length > 1 ? path.slice(1) : null
}

function fileUrlPath(url: URL): string | null {
  let path: string
  try {
    path = decodeURIComponent(url.pathname)
  } catch {
    return null
  }
  if (path.includes("/node_modules/")) return null
  return /^\/[A-Za-z]:\//.test(path) ? path.slice(1) : path
}

const SOURCE_MAP_COMMENT = /\/\/[#@]\s*sourceMappingURL=(\S+)\s*$/

// The module's source map: inline (a data: URL), or a file on the same
// origin.
async function sourceMapOf(code: string, moduleUrl: string, context: ResolveContext): Promise<SourceMapInput | null> {
  const lines = code.trimEnd().split("\n")
  const match = SOURCE_MAP_COMMENT.exec(lines[lines.length - 1] ?? "")
  if (!match) return null
  const reference = match[1]
  let text: string
  if (reference.startsWith("data:")) {
    const comma = reference.indexOf(",")
    if (comma < 0) return null
    const header = reference.slice(0, comma)
    const body = reference.slice(comma + 1)
    if (header.endsWith(";base64")) {
      const binary = atob(body)
      text = new TextDecoder().decode(Uint8Array.from(binary, (char) => char.charCodeAt(0)))
    } else {
      text = decodeURIComponent(body)
    }
  } else {
    const mapUrl = new URL(reference, moduleUrl)
    if (mapUrl.origin !== context.origin) return null
    text = await context.fetchText(mapUrl.href)
  }
  return JSON.parse(text) as SourceMapInput
}

export const viteResolver: SourceResolver = {
  name: "vite",
  async resolve(frame: RawFrame, context: ResolveContext): Promise<ResolvedSource | null> {
    let url: URL
    try {
      url = new URL(frame.url)
    } catch {
      return null
    }
    if (url.protocol === "file:") {
      const file = frame.original ? fileUrlPath(url) : null
      return file ? { file, line: frame.line, column: frame.column || undefined } : null
    }
    if (url.origin !== context.origin) return null
    const file = viteFileOf(url.pathname)
    if (!file) return null
    if (frame.original) return { file, line: frame.line, column: frame.column || undefined }
    url.hash = ""
    let map: SourceMapInput | null
    try {
      map = await sourceMapOf(await context.fetchText(url.href), url.href, context)
    } catch {
      return { file }
    }
    if (!map) return { file }
    try {
      // Stack columns count from 1, source map columns from 0.
      const position = originalPositionFor(new TraceMap(map, url.href), { line: frame.line, column: Math.max(0, frame.column - 1) })
      if (position.line === null) return { file }
      return { file, line: position.line, column: position.column + 1 }
    } catch {
      return { file }
    }
  },
}
