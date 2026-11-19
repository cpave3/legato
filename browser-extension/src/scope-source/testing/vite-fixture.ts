// Test helpers: the fixture app (fixtures/scope-react-app) served by a real
// Vite dev server under React 18 or React 19, and its modules evaluated so
// that stack frames name the dev server's URLs, as they do in a browser.
import { mkdtemp, rm } from "node:fs/promises"
import { createRequire } from "node:module"
import { tmpdir } from "node:os"
import { join, resolve } from "node:path"
import { runInThisContext } from "node:vm"
import react from "@vitejs/plugin-react"
import { createServer, type ViteDevServer } from "vite"

export type ReactVersion = 18 | 19

export const FIXTURE_ROOT = resolve(import.meta.dirname, "../../../fixtures/scope-react-app")

const require = createRequire(import.meta.url)

export type Fixture = { server: ViteDevServer; origin: string; close: () => Promise<void> }

// Serves the fixture app. The packages are installed as react18/react19 and
// react-dom18/react-dom19; the app imports react and react-dom.
export async function serveFixture(version: ReactVersion): Promise<Fixture> {
  const cacheDir = await mkdtemp(join(tmpdir(), `legato-scope-vite${version}-`))
  const server = await createServer({
    root: FIXTURE_ROOT,
    configFile: false,
    logLevel: "silent",
    // Each React version pre-bundles its own copy of the packages.
    cacheDir,
    // Without HMR, @vitejs/plugin-react leaves Fast Refresh out, so a
    // module evaluates without the refresh runtime.
    server: { port: 0, host: "127.0.0.1", hmr: false, ws: false },
    plugins: [react()],
    resolve: {
      alias: [
        { find: /^react-dom(\/.*)?$/, replacement: `react-dom${version}$1` },
        { find: /^react(\/.*)?$/, replacement: `react${version}$1` },
      ],
    },
    optimizeDeps: { noDiscovery: true, include: [] },
  })
  await server.listen()
  const address = server.httpServer?.address()
  if (!address || typeof address === "string") throw new Error("Vite did not listen")
  return { server, origin: `http://127.0.0.1:${address.port}`, close: async () => {
    await server.close()
    await rm(cacheDir, { recursive: true, force: true })
  } }
}

// The fixture file's 1-based line that holds a marker string.
export async function lineOf(file: string, marker: string): Promise<number> {
  const { readFile } = await import("node:fs/promises")
  const lines = (await readFile(resolve(FIXTURE_ROOT, file), "utf8")).split("\n")
  const index = lines.findIndex((line) => line.includes(marker))
  if (index < 0) throw new Error(`${marker} is not in ${file}`)
  return index + 1
}

// React's packages for a version, loaded through Node's require. react-dom 18
// declares react as a peer, which the install resolves to React 19, so its
// requests for react are sent to React 18 here.
export function loadReact(version: ReactVersion): {
  jsxDEV: (...args: unknown[]) => unknown
  createRoot: (container: Element) => { render: (node: unknown) => void; unmount: () => void }
  flushSync: (fn: () => void) => void
} {
  const Module = require("node:module") as { _resolveFilename: (request: string, parent: { filename?: string } | undefined, ...rest: unknown[]) => string }
  const original = Module._resolveFilename
  const react18 = resolve(require.resolve("react18/package.json"), "..")
  if (version === 18) {
    Module._resolveFilename = function (request, parent, ...rest) {
      if ((request === "react" || request.startsWith("react/")) && parent?.filename?.includes("react-dom@18")) {
        return original.call(this, request.replace(/^react/, react18), parent, ...rest)
      }
      return original.call(this, request, parent, ...rest)
    }
  }
  try {
    const runtime = require(`react${version}/jsx-dev-runtime`) as { jsxDEV: (...args: unknown[]) => unknown }
    const client = require(`react-dom${version}/client`) as { createRoot: (container: Element) => { render: (node: unknown) => void; unmount: () => void } }
    const dom = require(`react-dom${version}`) as { flushSync: (fn: () => void) => void }
    return { jsxDEV: runtime.jsxDEV, createRoot: client.createRoot, flushSync: dom.flushSync }
  } finally {
    Module._resolveFilename = original
  }
}

// Evaluates a module as the dev server serves it, under its URL, so the
// stack frames of its functions read as a browser's would. Its imports are
// given by specifier (only the JSX runtime, for the fixture's App.jsx); only
// import lines and the export line change, so every other line keeps its
// columns. The source map comment is dropped, so nothing maps the frames
// before the code under test does.
export async function evaluateModule(origin: string, path: string, deps: Record<string, unknown>): Promise<{ url: string; exports: Record<string, unknown> }> {
  const url = `${origin}${path}?t=1700000000000`
  const response = await fetch(`${origin}${path}`)
  if (!response.ok) throw new Error(`${path}: ${response.status}`)
  const dependency = (specifier: string): string => {
    const key = Object.keys(deps).find((dep) => specifier.includes(dep))
    if (!key) throw new Error(`No dependency given for ${specifier}`)
    return `__deps[${JSON.stringify(key)}]`
  }
  // Imports bind before the module runs: they move to the wrapper's first
  // line, and each import line is left empty.
  const imports: string[] = []
  const code = (await response.text())
    .replace(/^import\s*\{([^}]*)\}\s*from\s*"([^"]+)";?/gm, (_line, names: string, specifier: string) => {
      const bindings = names.split(",").map((name) => name.trim()).filter(Boolean).map((name) => name.replace(/\s+as\s+/, ": "))
      imports.push(`const { ${bindings.join(", ")} } = ${dependency(specifier)};`)
      return ""
    })
    .replace(/^import\s+([\w$]+)\s+from\s*"([^"]+)";?/gm, (_line, name: string, specifier: string) => {
      imports.push(`const ${name} = ${dependency(specifier)};`)
      return ""
    })
    .replace(/^export default (\w+);?$/m, "__exports.default = $1;")
    .replace(/^\/\/# sourceMappingURL=.*$/m, "")
  if (/^\s*(import|export)\s/m.test(code)) throw new Error(`${path} has imports or exports the test cannot evaluate:\n${code}`)
  const exports: Record<string, unknown> = {}
  const run = runInThisContext(`(function (__deps, __exports) {${imports.join(" ")}${code}\n})`, { filename: url }) as (deps: Record<string, unknown>, exports: Record<string, unknown>) => void
  run(deps, exports)
  return { url, exports }
}
