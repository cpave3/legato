import { afterEach, describe, expect, it } from "vitest"
import { buildSandboxDocument, buildStandaloneDocument, itemImages, resolveImageReferences, type ItemImage } from "../agentUi"

// An image file an agent showed reaches a page as an address: Legato keeps
// its own copy of the file and serves it. The frame's own script is tested by
// running it, as the diagram script is.

const BOARD = "http://legato.test:3080/agent-ui-images/task-1/" + "a".repeat(64) + ".png"
const CHART = "http://legato.test:3080/agent-ui-images/task-1/" + "b".repeat(64) + ".jpg"
const IMAGES: ItemImage[] = [{ name: "board", address: BOARD }, { name: "chart", address: CHART }]

function policyOf(doc: string): string {
  return /http-equiv="Content-Security-Policy" content="([^"]*)"/.exec(doc)?.[1] ?? ""
}

function directive(doc: string, name: string): string {
  return policyOf(doc).split("; ").find((entry) => entry.startsWith(`${name} `)) ?? ""
}

interface Frame {
  window: Window & { legato: { image: (name: string) => string } }
  document: Document
}

function openFrame(doc: string): Frame {
  const frame = document.createElement("iframe")
  document.body.appendChild(frame)
  const frameDocument = frame.contentDocument!
  frameDocument.open()
  frameDocument.write(doc)
  frameDocument.close()
  return { window: frame.contentWindow as unknown as Frame["window"], document: frameDocument }
}

// What the frame asked its host for, as the host hears it.
function requests(): { method: string; params: Record<string, unknown> }[] {
  const heard: { method: string; params: Record<string, unknown> }[] = []
  window.addEventListener("message", (event) => {
    const data = event.data as { method?: string; params?: Record<string, unknown> }
    if (data?.method) heard.push({ method: data.method, params: data.params ?? {} })
  })
  return heard
}

const delivered = () => new Promise((resolve) => setTimeout(resolve, 0))

afterEach(() => {
  document.body.innerHTML = ""
})

describe("itemImages", () => {
  it("says where each of an item's images is, at the server the page talks to", () => {
    const images = [{ name: "board", url: "/agent-ui-images/task-1/abc.png" }]

    expect(itemImages("https://legato.example:3080", { images })).toEqual([
      { name: "board", address: "https://legato.example:3080/agent-ui-images/task-1/abc.png" },
    ])
    // No server of its own: the one the page came from.
    expect(itemImages("", { images })).toEqual([
      { name: "board", address: `${window.location.origin}/agent-ui-images/task-1/abc.png` },
    ])
    expect(itemImages("", {})).toEqual([])
  })

  it("leaves out an address that is not one of Legato's stored images", () => {
    const images = [
      { name: "elsewhere", url: "https://evil.example/agent-ui-images/x.png" },
      { name: "api", url: "/api/agents/task-1/image?path=/etc/passwd" },
      { name: "script", url: "javascript:alert(1)" },
    ]

    expect(itemImages("https://legato.example:3080", { images })).toEqual([])
  })
})

describe("resolveImageReferences", () => {
  it("puts an image's address where the content names it", () => {
    const html = `<img src="legato-image:board"><div style="background: url(legato-image:chart)"></div>`

    expect(resolveImageReferences(html, IMAGES)).toBe(`<img src="${BOARD}"><div style="background: url(${CHART})"></div>`)
    expect(resolveImageReferences("![The board](legato-image:board)", IMAGES)).toBe(`![The board](${BOARD})`)
  })

  it("leaves a name it does not know as it was written", () => {
    expect(resolveImageReferences(`<img src="legato-image:boardroom">`, IMAGES)).toBe(`<img src="legato-image:boardroom">`)
  })
})

describe("images in the sandboxed frame", () => {
  it("lets the frame load the item's images and nothing else from Legato", () => {
    const doc = buildSandboxDocument({ html: `<img src="legato-image:board">`, resourceDomains: [], fill: true, images: IMAGES })

    expect(doc).toContain(`<img src="${BOARD}">`)
    // Only the folder of stored images, not the API beside it.
    expect(directive(doc, "img-src")).toBe("img-src data: blob: http://legato.test:3080/agent-ui-images/")
    expect(directive(doc, "connect-src")).toBe("connect-src 'none'")
    expect(directive(doc, "script-src")).not.toContain("legato.test")
  })

  it("allows nothing more when the item has no images", () => {
    const doc = buildSandboxDocument({ html: "<b>27 C</b>", resourceDomains: [], fill: true })

    expect(directive(doc, "img-src")).toBe("img-src data: blob:")
  })

  it("gives a script the address of an image by its name", () => {
    const frame = openFrame(buildSandboxDocument({ html: "<p>x</p>", resourceDomains: [], fill: true, images: IMAGES }))

    expect(frame.window.legato.image("chart")).toBe(CHART)
    expect(frame.window.legato.image("missing")).toBe("")
  })

  it("asks the host to show an image full size when the person clicks it, with the page's others to step through", async () => {
    const heard = requests()
    const frame = openFrame(buildSandboxDocument({
      html: `<img id="one" src="legato-image:board" alt="The board">
        <figure><img id="two" src="legato-image:chart" alt="Builds"><figcaption>Builds this week</figcaption></figure>
        <img id="inline" src="data:image/png;base64,AAAA" alt="An icon">`,
      resourceDomains: [], fill: true, images: IMAGES,
    }))
    frame.document.dispatchEvent(new Event("DOMContentLoaded"))

    frame.document.getElementById("two")!.dispatchEvent(new MouseEvent("click", { bubbles: true, cancelable: true }))
    // An image that is not one of the item's files is the page's own business.
    frame.document.getElementById("inline")!.dispatchEvent(new MouseEvent("click", { bubbles: true, cancelable: true }))
    await delivered()

    expect(heard.filter((request) => request.method === "legato/show-image")).toEqual([{
      method: "legato/show-image",
      params: { index: 1, images: [{ url: BOARD, alt: "The board", caption: "" }, { url: CHART, alt: "Builds", caption: "Builds this week" }] },
    }])
    // The person can see which images open, and reach them with the keyboard.
    expect(frame.document.getElementById("one")!.getAttribute("data-legato-zoom")).toBe("on")
    expect(frame.document.getElementById("one")!.getAttribute("tabindex")).toBe("0")
    expect(frame.document.getElementById("inline")!.hasAttribute("data-legato-zoom")).toBe(false)
  })

  it("opens an image from the keyboard", async () => {
    const heard = requests()
    const frame = openFrame(buildSandboxDocument({ html: `<img id="one" src="legato-image:board" alt="The board">`, resourceDomains: [], fill: true, images: IMAGES }))
    frame.document.dispatchEvent(new Event("DOMContentLoaded"))

    frame.document.getElementById("one")!.dispatchEvent(new KeyboardEvent("keydown", { key: "Enter", bubbles: true, cancelable: true }))
    await delivered()

    expect(heard.filter((request) => request.method === "legato/show-image")).toHaveLength(1)
  })

  it("leaves alone an image the page marked, and one that is a link or a button of the page's own", async () => {
    const heard = requests()
    const frame = openFrame(buildSandboxDocument({
      html: `<img id="off" src="legato-image:board" data-legato-zoom="off">
        <a href="#detail"><img id="linked" src="legato-image:board"></a>
        <button><img id="pressed" src="legato-image:chart"></button>
        <div data-legato-zoom="off"><img id="inside" src="legato-image:chart"></div>`,
      resourceDomains: [], fill: true, images: IMAGES,
    }))
    frame.document.dispatchEvent(new Event("DOMContentLoaded"))
    // The page's link is its own to follow; jsdom cannot follow one, and says so.
    frame.document.querySelector("a")!.addEventListener("click", (event) => event.preventDefault())

    for (const id of ["off", "linked", "pressed", "inside"]) {
      frame.document.getElementById(id)!.dispatchEvent(new MouseEvent("click", { bubbles: true, cancelable: true }))
    }
    await delivered()

    expect(heard.filter((request) => request.method === "legato/show-image")).toEqual([])
    expect(frame.document.getElementById("off")!.getAttribute("data-legato-zoom")).toBe("off")
    expect(frame.document.getElementById("linked")!.hasAttribute("tabindex")).toBe(false)
  })

  it("has a ready-made grid for a page's images", () => {
    const doc = buildSandboxDocument({ html: `<div class="legato-image-grid"></div>`, resourceDomains: [], fill: true, images: IMAGES })

    expect(doc).toMatch(/\.legato-image-grid \{[^}]*display: grid/)
  })
})

describe("images in a saved page", () => {
  it("carries the images inside the file, so it needs no Legato to open", () => {
    const inline: ItemImage[] = [{ name: "board", address: "data:image/png;base64,iVBORw0KGgo=" }]
    const doc = buildStandaloneDocument({ html: `<img id="one" src="legato-image:board">`, title: "Shots", resourceDomains: [], images: inline })

    expect(doc).toContain(`<img id="one" src="data:image/png;base64,iVBORw0KGgo=">`)
    expect(directive(doc, "img-src")).toBe("img-src data: blob:")
    const frame = openFrame(doc)
    frame.document.dispatchEvent(new Event("DOMContentLoaded"))
    expect(frame.window.legato.image("board")).toBe("data:image/png;base64,iVBORw0KGgo=")
    // Nothing out there shows an image full size, so none is offered.
    expect(frame.document.getElementById("one")!.hasAttribute("data-legato-zoom")).toBe(false)
  })

  it("may still load an image from Legato when it could not be put in the file", () => {
    const doc = buildStandaloneDocument({ html: `<img src="legato-image:board">`, title: "Shots", resourceDomains: [], images: IMAGES })

    expect(directive(doc, "img-src")).toBe("img-src data: blob: http://legato.test:3080/agent-ui-images/")
  })
})
