import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react"
import { afterEach, describe, expect, it, vi } from "vitest"
import { VisualMarkdownPre } from "../VisualMarkdownPre"
import { WiremarkDiagram } from "../WiremarkDiagram"

const { renderWiremark } = vi.hoisted(() => ({ renderWiremark: vi.fn() }))

vi.mock("@wiremark/core", () => ({ render: renderWiremark }))

afterEach(() => {
  cleanup()
  renderWiremark.mockReset()
})

describe("WiremarkDiagram", () => {
  it("renders canonical wireframe fences with dark inert SVG", async () => {
    renderWiremark.mockReturnValue({ svg: "<svg><text>Rendered mockup</text></svg>", diagnostics: [] })

    render(<VisualMarkdownPre><code className="language-wireframe">{"Wireframe mobile\n  Button \"Save\" contained\n"}</code></VisualMarkdownPre>)

    expect(await screen.findByText("Rendered mockup")).toBeTruthy()
    expect(renderWiremark).toHaveBeenCalledWith("Wireframe mobile\n  Button \"Save\" contained\n", {
      theme: "dark",
      interactive: true,
    })
    expect(screen.getByRole("img", { name: "Wiremark mockup" })).toBeTruthy()
  })

  it("expands and collapses with its toggle or Escape", async () => {
    renderWiremark.mockReturnValue({ svg: "<svg><text>Wide mockup</text></svg>", diagnostics: [] })
    render(<WiremarkDiagram source="Wireframe landscape" />)

    await screen.findByText("Wide mockup")
    fireEvent.click(screen.getByRole("button", { name: "Expand Wiremark mockup" }))
    expect(screen.getByRole("dialog", { name: "Expanded Wiremark mockup" })).toBeTruthy()

    fireEvent.keyDown(document, { key: "Escape" })
    expect(screen.queryByRole("dialog", { name: "Expanded Wiremark mockup" })).toBeNull()
  })

  it("shows the exact source and render error after a hard failure", async () => {
    renderWiremark.mockImplementation(() => { throw new Error("Unknown component FancyPanel at line 2") })
    render(<WiremarkDiagram source={"Wireframe mobile\n  FancyPanel"} />)

    expect((await screen.findByRole("alert")).textContent).toContain("Unknown component FancyPanel at line 2")
    expect(screen.getByRole("alert").querySelector("code")?.textContent).toBe("Wireframe mobile\n  FancyPanel")
    expect(screen.getByRole("alert").parentElement?.querySelector("svg")).toBeNull()
  })

  it("recovers when invalid source is replaced", async () => {
    renderWiremark
      .mockImplementationOnce(() => { throw new Error("Invalid source") })
      .mockReturnValueOnce({ svg: "<svg><text>Recovered mockup</text></svg>", diagnostics: [] })
    const { rerender } = render(<WiremarkDiagram source="invalid" />)
    expect(await screen.findByRole("alert")).toBeTruthy()

    rerender(<WiremarkDiagram source="Wireframe mobile" />)

    expect(await screen.findByText("Recovered mockup")).toBeTruthy()
    expect(screen.queryByRole("alert")).toBeNull()
  })

  it("namespaces generated SVG IDs across duplicate mockups", async () => {
    renderWiremark.mockReturnValue({
      svg: '<svg><defs><clipPath id="wm-clip-0"><rect/></clipPath></defs><g clip-path="url(#wm-clip-0)">Content</g></svg>',
      diagnostics: [],
    })

    render(<><WiremarkDiagram source="Wireframe mobile"/><WiremarkDiagram source="Wireframe mobile"/></>)

    await waitFor(() => expect(screen.getAllByRole("img", { name: "Wiremark mockup" }).every(container => container.querySelector("svg"))).toBe(true))
    const ids = screen.getAllByRole("img", { name: "Wiremark mockup" }).map(container => container.querySelector("clipPath")?.id)
    expect(new Set(ids).size).toBe(2)
    for (const container of screen.getAllByRole("img", { name: "Wiremark mockup" })) {
      const id = container.querySelector("clipPath")?.id
      expect(container.querySelector("g")?.getAttribute("clip-path")).toBe(`url(#${id})`)
    }
  })

  it("ignores output after the source changes or component unmounts", async () => {
    renderWiremark.mockImplementation(source => ({ svg: `<svg><text>${source}</text></svg>`, diagnostics: [] }))

    const { rerender, unmount } = render(<WiremarkDiagram source="Old mockup" />)
    rerender(<WiremarkDiagram source="Current mockup" />)
    expect(await screen.findByText("Current mockup")).toBeTruthy()
    expect(screen.queryByText("Old mockup")).toBeNull()

    unmount()
    expect(screen.queryByText("Current mockup")).toBeNull()
  })

  it("dispatches only canonical case-insensitive fenced languages", async () => {
    renderWiremark.mockReturnValue({ svg: "<svg><text>Canonical</text></svg>", diagnostics: [] })
    const { rerender } = render(<VisualMarkdownPre><code className="language-WIREFRAME">Wireframe mobile</code></VisualMarkdownPre>)
    expect(await screen.findByText("Canonical")).toBeTruthy()

    for (const className of ["language-wiremark", "language-go", undefined]) {
      rerender(<VisualMarkdownPre><code className={className}>Wireframe mobile</code></VisualMarkdownPre>)
      await waitFor(() => expect(screen.queryByRole("img", { name: "Wiremark mockup" })).toBeNull())
    }
  })
})
