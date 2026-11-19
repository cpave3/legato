// @vitest-environment jsdom
// @vitest-environment-options {"url":"http://localhost:5173/"}
import { createRequire } from "node:module"
import { runInThisContext } from "node:vm"
import { afterEach, describe, expect, it } from "vitest"
import { readProbedComponent } from "./main-world"
import { loadReact } from "./testing/vite-fixture"
import { PROBE_ATTRIBUTE, type FrameworkAdapter } from "./types"

const require = createRequire(import.meta.url)

// React 19's production build: the packages choose it by NODE_ENV as they
// load, so they are loaded afresh under "production".
function productionReact() {
  const forget = (): void => {
    for (const key of Object.keys(require.cache)) if (/[\\/]react(-dom)?[\\/]/.test(key)) delete require.cache[key]
  }
  const environment = process.env.NODE_ENV
  process.env.NODE_ENV = "production"
  forget()
  try {
    return {
      React: require("react19") as { createElement: (...args: unknown[]) => unknown },
      flushSync: (require("react-dom19") as { flushSync: (fn: () => void) => void }).flushSync,
      createRoot: (require("react-dom19/client") as { createRoot: (node: Element) => { render: (node: unknown) => void; unmount: () => void } }).createRoot,
    }
  } finally {
    process.env.NODE_ENV = environment
    forget()
  }
}

afterEach(() => {
  document.body.innerHTML = ""
})

function probe(element: Element): void {
  element.setAttribute(PROBE_ATTRIBUTE, "")
}

describe("readProbedComponent", () => {
  it("installs no React DevTools hook", () => {
    expect("__REACT_DEVTOOLS_GLOBAL_HOOK__" in window).toBe(false)
  })

  it("reads nothing on a page without React", () => {
    document.body.innerHTML = `<main><button>Save</button></main>`
    probe(document.querySelector("button")!)
    expect(readProbedComponent(document)).toBeNull()
  })

  it("reads nothing when no element is marked", () => {
    expect(readProbedComponent(document)).toBeNull()
  })

  it("gives the next adapter its turn when one throws", () => {
    document.body.innerHTML = `<button>Save</button>`
    probe(document.querySelector("button")!)
    const broken: FrameworkAdapter = { framework: "broken", read: () => { throw new Error("page changed Object.keys") } }
    const other: FrameworkAdapter = { framework: "other", read: () => ({ framework: "other", name: "Save", owners: [], frames: [] }) }
    expect(readProbedComponent(document, [broken, other])).toEqual({ framework: "other", name: "Save", owners: [], frames: [] })
  })

  it("follows with where each owner was written, for an element a dependency's component drew", () => {
    const { jsxDEV, createRoot, flushSync } = loadReact(19)
    // A dependency's component and the app that uses it, evaluated under the
    // URLs a Vite dev server gives them.
    const LibraryButton = runInThisContext(
      "(function (jsx) { return function LibraryButton() { return jsx('button', {}, undefined, false, undefined, undefined) } })",
      { filename: "http://localhost:5173/node_modules/.vite/deps/library.js?v=1" },
    )(jsxDEV)
    const App = runInThisContext(
      "(function (jsx, Button) { return function App() {\n  return jsx(Button, {}, undefined, false, undefined, undefined) } })",
      { filename: "http://localhost:5173/src/App.jsx?t=1" },
    )(jsxDEV, LibraryButton)
    const container = document.createElement("div")
    document.body.append(container)
    const root = createRoot(container)
    flushSync(() => root.render(jsxDEV(App, {}, undefined, false, undefined, undefined)))
    probe(container.querySelector("button")!)
    const reading = readProbedComponent(document)!
    root.unmount()
    expect(reading).toMatchObject({ name: "LibraryButton", owners: ["App"] })
    const urls = reading.frames.map((frame) => frame.url)
    const library = urls.indexOf("http://localhost:5173/node_modules/.vite/deps/library.js?v=1")
    const app = urls.indexOf("http://localhost:5173/src/App.jsx?t=1")
    expect(library).toBeGreaterThanOrEqual(0)
    expect(app).toBeGreaterThan(library)
    expect(reading.frames[app]).toMatchObject({ line: 2, original: false })
  })

  it("reads only the names from a production build of React", () => {
    const { React, createRoot, flushSync } = productionReact()
    function SaveButton() { return React.createElement("button", null, "Save") }
    function Toolbar() { return React.createElement("div", null, React.createElement(SaveButton)) }
    const container = document.createElement("div")
    document.body.append(container)
    const root = createRoot(container)
    flushSync(() => root.render(React.createElement(Toolbar)))
    probe(container.querySelector("button")!)
    expect(readProbedComponent(document)).toEqual({ framework: "react", name: "SaveButton", owners: ["Toolbar"], frames: [] })
    root.unmount()
  })
})
