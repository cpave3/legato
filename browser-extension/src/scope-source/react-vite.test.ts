// @vitest-environment jsdom
// The whole lookup against real React and a real Vite dev server: the
// fixture app is served by Vite under React 18 and React 19, its App.jsx is
// evaluated under the dev server's URL (so React's stacks name it as a
// browser would) and rendered by react-dom in jsdom; then the MAIN-world
// reader reads the pinned element's fibre, and the service worker's side
// fetches the module and its source map from the dev server.
import { afterAll, afterEach, beforeAll, describe, expect, it, vi } from "vitest"
import { fetchTextOn } from "./fetch-text"
import { resolvePinSource } from "./lookup"
import { readProbedComponent } from "./main-world"
import { PROBE_ATTRIBUTE } from "./types"
import { evaluateModule, FIXTURE_ROOT, lineOf, loadReact, serveFixture, type Fixture, type ReactVersion } from "./testing/vite-fixture"

describe.each([18, 19] as ReactVersion[])("React %i on Vite", (version) => {
  let fixture: Fixture
  let container: HTMLElement
  let unmount: () => void
  let moduleUrl: string

  beforeAll(async () => {
    fixture = await serveFixture(version)
    // The page is the dev server's.
    ;(globalThis as unknown as { jsdom: { reconfigure: (options: { url: string }) => void } }).jsdom.reconfigure({ url: `${fixture.origin}/` })
    const react = loadReact(version)
    const app = await evaluateModule(fixture.origin, "/src/App.jsx", { "jsx-dev-runtime": react })
    moduleUrl = app.url
    container = document.createElement("div")
    document.body.append(container)
    const root = react.createRoot(container)
    react.flushSync(() => root.render(react.jsxDEV(app.exports.default, {}, undefined, false, undefined, undefined)))
    unmount = () => root.unmount()
  })

  afterAll(async () => {
    unmount()
    container.remove()
    await fixture.close()
  })

  afterEach(() => {
    vi.restoreAllMocks()
    for (const element of document.querySelectorAll(`[${PROBE_ATTRIBUTE}]`)) element.removeAttribute(PROBE_ATTRIBUTE)
  })

  // A browser lists the modules it fetched; jsdom fetched none.
  const browserLoaded = (): void => {
    vi.spyOn(performance, "getEntriesByType").mockImplementation(((type: string) => type === "resource"
      ? [{ name: `${fixture.origin}/src/main.jsx` }, { name: moduleUrl }]
      : []) as unknown as typeof performance.getEntriesByType)
  }

  const lookup = async (selector: string) => {
    container.querySelector(selector)!.setAttribute(PROBE_ATTRIBUTE, "")
    const reading = readProbedComponent(document)
    return resolvePinSource(reading, { origin: fixture.origin, fetchText: fetchTextOn(fixture.origin) })
  }

  it.each([
    { selector: "button.save", name: "SaveButton", marker: "<button", owners: ["App"] },
    { selector: "div.toolbar", name: "Toolbar", marker: "<div className=\"toolbar\"", owners: ["App"] },
    { selector: "span.badge", name: "Badge", marker: "<span className=\"badge\"", owners: ["App"] },
    { selector: "h1", name: "App", marker: "<h1", owners: [] },
  ])("names $name and the line of its JSX for $selector", async ({ selector, name, marker, owners }) => {
    browserLoaded()
    const source = await lookup(selector)
    expect(source).toMatchObject({ name, file: "src/App.jsx", line: await lineOf("src/App.jsx", marker), confidence: "exact" })
    expect(source.column).toBeGreaterThan(0)
    expect(source.owners ?? []).toEqual(owners)
  })

  it.runIf(version === 18)("gives the file on disk when the page lists no module for it", async () => {
    vi.spyOn(performance, "getEntriesByType").mockReturnValue([])
    const source = await lookup("button.save")
    expect(source).toMatchObject({ name: "SaveButton", file: `${FIXTURE_ROOT}/src/App.jsx`, line: await lineOf("src/App.jsx", "<button"), confidence: "exact" })
  })

  it("gives no file and no name for an element React did not render", async () => {
    const outside = document.createElement("p")
    document.body.append(outside)
    outside.setAttribute(PROBE_ATTRIBUTE, "")
    const source = await resolvePinSource(readProbedComponent(document), { origin: fixture.origin, fetchText: fetchTextOn(fixture.origin) })
    outside.remove()
    expect(source).toEqual({ confidence: "none" })
  })
})
