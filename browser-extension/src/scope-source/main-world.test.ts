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
    // The project's frames come first, then the dependencies' (which no
    // resolver places, but which are kept while there is room).
    const urls = reading.frames.map((frame) => frame.url)
    const library = urls.indexOf("http://localhost:5173/node_modules/.vite/deps/library.js?v=1")
    const app = urls.indexOf("http://localhost:5173/src/App.jsx?t=1")
    expect(app).toBe(0)
    expect(library).toBeGreaterThan(app)
    expect(reading.frames[app]).toMatchObject({ line: 2, original: false })
  })

  // In a browser React's runtime is served by the dev server too, so a
  // dependency's stacks are full of http frames. They must not use up the
  // frame budget before the owner whose stack names the project's callsite.
  it("keeps the project's callsite when nested dependency components fill the stacks", () => {
    const { jsxDEV, createRoot, flushSync } = loadReact(19)
    // Each library component makes its element from deep inside the
    // library: a chain of helpers, every frame a dependency's.
    const library = (name: string, child: string): unknown => runInThisContext(
      `(function (jsx, Child) {
        const deep = (n, make) => n === 0 ? make() : deep(n - 1, make)
        return function ${name}() { return deep(12, () => jsx(Child ?? ${JSON.stringify(child)}, {}, undefined, false, undefined, undefined)) }
      })`,
      { filename: `http://localhost:5173/node_modules/.vite/deps/${name}.js?v=1` },
    )
    const Leaf = (library("LibraryLeaf", "button") as (jsx: unknown, child: unknown) => unknown)(jsxDEV, null)
    const Middle = (library("LibraryMiddle", "") as (jsx: unknown, child: unknown) => unknown)(jsxDEV, Leaf)
    const Wrapper = (library("LibraryWrapper", "") as (jsx: unknown, child: unknown) => unknown)(jsxDEV, Middle)
    const App = runInThisContext(
      "(function (jsx, Wrapper) { return function App() {\n  return jsx(Wrapper, {}, undefined, false, undefined, undefined) } })",
      { filename: "http://localhost:5173/src/App.jsx?t=1" },
    )(jsxDEV, Wrapper)
    const container = document.createElement("div")
    document.body.append(container)
    const root = createRoot(container)
    flushSync(() => root.render(jsxDEV(App, {}, undefined, false, undefined, undefined)))
    probe(container.querySelector("button")!)
    const reading = readProbedComponent(document)!
    root.unmount()
    expect(reading).toMatchObject({ name: "LibraryLeaf", owners: ["LibraryMiddle", "LibraryWrapper", "App"] })
    expect(reading.frames.map((frame) => frame.url)).toContain("http://localhost:5173/src/App.jsx?t=1")
    expect(reading.frames[0]).toMatchObject({ url: "http://localhost:5173/src/App.jsx?t=1", line: 2 })
  })

  // React may leave the DOM node pointing at the older fibre of the pair
  // after an update; the source is read from the current one.
  it("reads the source of the element as rendered now, after an update reused its node", () => {
    const { jsxDEV, createRoot, flushSync } = loadReact(18)
    const at = (line: number) => ({ fileName: "/app/src/App.jsx", lineNumber: line, columnNumber: 5 })
    function App({ which }: { which: string }) {
      return which === "a"
        ? jsxDEV("button", { className: "a" }, undefined, false, at(3), undefined)
        : jsxDEV("button", { className: "b" }, undefined, false, at(2), undefined)
    }
    const container = document.createElement("div")
    document.body.append(container)
    const root = createRoot(container)
    flushSync(() => root.render(jsxDEV(App, { which: "a" }, undefined, false, undefined, undefined)))
    const button = container.querySelector("button")!
    flushSync(() => root.render(jsxDEV(App, { which: "b" }, undefined, false, undefined, undefined)))
    expect(container.querySelector("button")).toBe(button)
    probe(button)
    const reading = readProbedComponent(document)!
    root.unmount()
    expect(reading.frames[0]).toMatchObject({ url: "file:///app/src/App.jsx", line: 2 })
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
