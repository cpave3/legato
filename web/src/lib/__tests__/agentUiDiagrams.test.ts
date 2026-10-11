import { afterEach, describe, expect, it } from "vitest"
import { buildSandboxDocument, buildStandaloneDocument } from "../agentUi"

// The frame's own script is tested by running it: the document Legato builds
// is written into a frame of the test page, with a stand-in for the Mermaid
// library, which needs a real browser to lay a diagram out.

interface FakeMermaid {
  initialize: (config: Record<string, unknown>) => void
  render: (id: string, source: string) => Promise<{ svg: string }>
  config?: Record<string, unknown>
  drawn: string[]
}

// What Mermaid 11 draws for a flowchart, as far as the frame's script reads
// it: one group per node, whose id holds the node's name in the source.
function fakeMermaid(): FakeMermaid {
  const fake: FakeMermaid = {
    drawn: [],
    initialize(config) { fake.config = config },
    async render(id, source) {
      if (source.includes("not a diagram")) throw new Error("Parse error on line 1")
      fake.drawn.push(source)
      const names = [...source.matchAll(/\b([A-Za-z][\w-]*)\[/g)].map((match) => match[1])
      const nodes = names.map((name, index) => `<g class="node default" id="${id}-flowchart-${name}-${index}"><rect></rect></g>`).join("")
      return { svg: `<svg id="${id}" viewBox="0 0 400 100" style="max-width: 400px;">${nodes}</svg>` }
    },
  }
  return fake
}

interface Frame {
  window: Window & { mermaid?: FakeMermaid; legato: { diagrams: (options?: unknown) => Promise<void> } }
  document: Document
}

const DOMAINS = ["cdn.jsdelivr.net", "unpkg.com"]

function openFrame(html: string, options: { mermaid?: FakeMermaid; resourceDomains?: string[]; saved?: boolean } = {}): Frame {
  const frame = document.createElement("iframe")
  document.body.appendChild(frame)
  const frameWindow = frame.contentWindow as unknown as Frame["window"]
  if (options.mermaid) frameWindow.mermaid = options.mermaid
  const frameDocument = frame.contentDocument!
  frameDocument.open()
  const resourceDomains = options.resourceDomains ?? DOMAINS
  frameDocument.write(options.saved
    ? buildStandaloneDocument({ html, title: "Saved", resourceDomains })
    : buildSandboxDocument({ html, resourceDomains, fill: true }))
  frameDocument.close()
  return { window: frameWindow, document: frameDocument }
}

afterEach(() => {
  document.body.innerHTML = ""
})

describe("diagrams in the sandboxed frame", () => {
  it("draws a mermaid block as a diagram when the page asks", async () => {
    const mermaid = fakeMermaid()
    const frame = openFrame(`<pre class="mermaid" id="map">flowchart LR; Client[Client] --> API[API]</pre><script>window.done = legato.diagrams()</script>`, { mermaid })

    await (frame.window as unknown as { done: Promise<void> }).done

    expect(mermaid.drawn).toEqual(["flowchart LR; Client[Client] --> API[API]"])
    const block = frame.document.getElementById("map")!
    expect(block.querySelector("svg")).not.toBeNull()
    expect(block.textContent).not.toContain("flowchart LR")
  })

  it("loads Mermaid by itself, from the first allowed host that serves it", async () => {
    const frame = openFrame(`<pre class="mermaid" id="map">flowchart LR; Client[Client] --> API[API]</pre><script>window.done = legato.diagrams()</script>`)

    const script = frame.document.querySelector<HTMLScriptElement>("head script[src]")
    expect(script?.src).toBe("https://cdn.jsdelivr.net/npm/mermaid@11.16.0/dist/mermaid.min.js")

    // The library arrives.
    const mermaid = fakeMermaid()
    frame.window.mermaid = mermaid
    script!.dispatchEvent(new Event("load"))
    await (frame.window as unknown as { done: Promise<void> }).done
    expect(frame.document.querySelector("#map svg")).not.toBeNull()

    const other = openFrame(`<pre class="mermaid">flowchart LR; A[A]</pre><script>legato.diagrams()</script>`, { resourceDomains: ["unpkg.com"] })
    expect(other.document.querySelector<HTMLScriptElement>("head script[src]")?.src).toBe("https://unpkg.com/mermaid@11.16.0/dist/mermaid.min.js")
  })

  it("draws in Legato's theme, with what a diagram in a frame needs switched on", async () => {
    const mermaid = fakeMermaid()
    const frame = openFrame(`<pre class="mermaid">flowchart LR; Client[Client]</pre><script>window.done = legato.diagrams()</script>`, { mermaid })
    await (frame.window as unknown as { done: Promise<void> }).done

    expect(mermaid.config).toMatchObject({
      startOnLoad: false,
      theme: "base",
      // The frame is the sandbox: a label may hold markup and a diagram's own click lines work.
      securityLevel: "loose",
      // A diagram Mermaid cannot read must not leave its error drawing on the page.
      suppressErrorRendering: true,
      themeVariables: { background: "#18181b", primaryColor: "#27272a", primaryTextColor: "#e4e4e7" },
    })
    // The colors a node can be given by name.
    for (const hue of ["blue", "green", "amber", "red", "purple", "pink", "cyan", "gray"]) {
      expect(mermaid.config!.themeCSS).toContain(`.node.${hue}`)
    }
  })

  it("tells the page which node the person chose, by the name the agent gave it", async () => {
    const frame = openFrame(
      `<pre class="mermaid" id="map">flowchart LR; Client[Client] --> api-gateway[API]</pre>
       <script>window.chosen = []; window.done = legato.diagrams({ onNode: function (id, node) { window.chosen.push(id + ":" + node.tagName) } })</script>`,
      { mermaid: fakeMermaid() },
    )
    await (frame.window as unknown as { done: Promise<void> }).done
    const chosen = (frame.window as unknown as { chosen: string[] }).chosen
    const [client, gateway] = [...frame.document.querySelectorAll<SVGGElement>("#map g.node")]

    // A node is a button: reachable and usable from the keyboard.
    expect(client.getAttribute("role")).toBe("button")
    expect(client.getAttribute("tabindex")).toBe("0")
    expect(client.getAttribute("data-node-id")).toBe("Client")

    client.dispatchEvent(new MouseEvent("click", { bubbles: true }))
    expect(chosen).toEqual(["Client:g"])
    expect(client.getAttribute("aria-pressed")).toBe("true")

    gateway.dispatchEvent(new KeyboardEvent("keydown", { key: "Enter", bubbles: true }))
    expect(chosen).toEqual(["Client:g", "api-gateway:g"])
    expect(gateway.getAttribute("aria-pressed")).toBe("true")
    expect(client.getAttribute("aria-pressed")).toBe("false")
  })

  it("keeps a wide diagram readable in a narrow panel: it scrolls sideways instead of shrinking to nothing", async () => {
    const frame = openFrame(`<pre class="mermaid" id="map">flowchart LR; Client[Client]</pre><script>window.done = legato.diagrams()</script>`, { mermaid: fakeMermaid() })
    await (frame.window as unknown as { done: Promise<void> }).done

    // The stand-in draws 400px wide. Mermaid lets a diagram shrink with its
    // container without limit; Legato stops it where 14px text is 10px.
    expect(frame.document.querySelector<SVGElement>("#map svg")!.style.minWidth).toBe("280px")
    expect(frame.window.getComputedStyle(frame.document.getElementById("map")!).overflowX).toBe("auto")
  })

  it("brings the chosen node into view, for a diagram wider than the panel", async () => {
    const frame = openFrame(
      `<pre class="mermaid" id="map">flowchart LR; Client[Client] --> API[API]</pre><script>window.done = legato.diagrams({ onNode: function () {} })</script>`,
      { mermaid: fakeMermaid() },
    )
    await (frame.window as unknown as { done: Promise<void> }).done
    const api = frame.document.querySelector<SVGGElement>('#map [data-node-id="API"]')!
    const scrolled: unknown[] = []
    api.scrollIntoView = (how?: boolean | ScrollIntoViewOptions) => { scrolled.push(how) }

    api.dispatchEvent(new MouseEvent("click", { bubbles: true }))

    // Sideways only: choosing a node must not move the page up or down.
    expect(scrolled).toEqual([{ block: "nearest", inline: "nearest", behavior: "smooth" }])
  })

  it("does not show a diagram's source while the diagram is on its way", async () => {
    const frame = openFrame(`<pre class="mermaid" id="map">flowchart LR; Client[Client]</pre><script>window.done = legato.diagrams()</script>`)
    const block = frame.document.getElementById("map")!
    expect(frame.window.getComputedStyle(block).visibility).toBe("hidden")

    frame.window.mermaid = fakeMermaid()
    frame.document.querySelector("head script[src]")!.dispatchEvent(new Event("load"))
    await (frame.window as unknown as { done: Promise<void> }).done
    expect(frame.window.getComputedStyle(block).visibility).not.toBe("hidden")
  })

  it("shows the source again when the diagram will never come", async () => {
    const frame = openFrame(`<pre class="mermaid" id="map">flowchart LR; Client[Client]</pre><script>window.done = legato.diagrams()</script>`)
    frame.document.querySelector("head script[src]")!.dispatchEvent(new Event("error"))
    await (frame.window as unknown as { done: Promise<void> }).done

    expect(frame.window.getComputedStyle(frame.document.getElementById("map")!).visibility).not.toBe("hidden")
  })

  it("draws the diagrams of a page saved from Legato too", async () => {
    const frame = openFrame(
      `<pre class="mermaid" id="map">flowchart LR; Client[Client]</pre><script>window.chosen = []; window.done = legato.diagrams({ onNode: function (id) { window.chosen.push(id) } })</script>`,
      { mermaid: fakeMermaid(), saved: true },
    )
    await (frame.window as unknown as { done: Promise<void> }).done

    frame.document.querySelector("#map g.node")!.dispatchEvent(new MouseEvent("click", { bubbles: true }))
    expect((frame.window as unknown as { chosen: string[] }).chosen).toEqual(["Client"])
  })

  it("lets the page's own script choose a node, as a click on it would", async () => {
    const frame = openFrame(
      `<pre class="mermaid" id="map">flowchart LR; Client[Client] --> API[API]</pre>
       <script>window.chosen = []; window.done = legato.diagrams({ onNode: function (id) { window.chosen.push(id) } }).then(function () { legato.selectNode("API"); legato.selectNode("Nothing") })</script>`,
      { mermaid: fakeMermaid() },
    )
    await (frame.window as unknown as { done: Promise<void> }).done

    expect((frame.window as unknown as { chosen: string[] }).chosen).toEqual(["API"])
    expect(frame.document.querySelector('#map [data-node-id="API"]')!.getAttribute("aria-pressed")).toBe("true")
  })

  it("leaves nodes alone when the page did not ask to hear of them", async () => {
    const frame = openFrame(`<pre class="mermaid" id="map">flowchart LR; Client[Client]</pre><script>window.done = legato.diagrams()</script>`, { mermaid: fakeMermaid() })
    await (frame.window as unknown as { done: Promise<void> }).done

    expect(frame.document.querySelector("#map g.node")!.hasAttribute("role")).toBe(false)
  })

  it("draws the other diagrams when one cannot be read, and leaves that one as its text", async () => {
    const frame = openFrame(
      `<pre class="mermaid" id="bad">not a diagram</pre><pre class="mermaid" id="good">flowchart LR; Client[Client]</pre><script>window.done = legato.diagrams()</script>`,
      { mermaid: fakeMermaid() },
    )
    await (frame.window as unknown as { done: Promise<void> }).done

    expect(frame.document.querySelector("#good svg")).not.toBeNull()
    const bad = frame.document.getElementById("bad")!
    expect(bad.textContent).toContain("not a diagram")
    // The person is told why they are reading source and not a diagram.
    expect(bad.getAttribute("title")).toContain("Parse error on line 1")
  })

  it("draws the diagrams of a page that never asked, once the page has loaded", async () => {
    const mermaid = fakeMermaid()
    const frame = openFrame(`<pre class="mermaid" id="map">flowchart LR; Client[Client]</pre>`, { mermaid: undefined })
    // No Mermaid of the page's own: Legato fetches it.
    frame.window.dispatchEvent(new Event("load"))
    const script = frame.document.querySelector<HTMLScriptElement>("head script[src]")!
    frame.window.mermaid = mermaid
    script.dispatchEvent(new Event("load"))

    await expect.poll(() => frame.document.querySelector("#map svg")).not.toBeNull()
  })

  it("does not draw for a page that brought a Mermaid of its own and runs it itself", async () => {
    const mermaid = fakeMermaid()
    const frame = openFrame(`<pre class="mermaid" id="map">flowchart LR; Client[Client]</pre>`, { mermaid })
    frame.window.dispatchEvent(new Event("load"))
    await new Promise((resolve) => setTimeout(resolve, 20))

    expect(mermaid.drawn).toEqual([])
    expect(frame.document.querySelector("head script[src]")).toBeNull()
  })

  it("leaves a block as its text when no allowed host serves Mermaid, and the page's script carries on", async () => {
    const frame = openFrame(
      `<pre class="mermaid" id="map">flowchart LR; Client[Client] --> API[API]</pre><script>window.done = legato.diagrams().then(function () { window.after = "ran" })</script>`,
      { resourceDomains: ["fonts.googleapis.com"] },
    )

    await (frame.window as unknown as { done: Promise<void> }).done

    expect(frame.document.querySelector("head script[src]")).toBeNull()
    expect(frame.document.getElementById("map")!.textContent).toContain("flowchart LR")
    expect((frame.window as unknown as { after: string }).after).toBe("ran")
  })
})
