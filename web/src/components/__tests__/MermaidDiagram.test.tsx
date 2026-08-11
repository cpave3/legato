import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react"
import { afterEach, describe, expect, it, vi } from "vitest"
import { MermaidDiagram } from "../MermaidDiagram"

const { initialize, mermaidConfig, renderDiagram } = vi.hoisted(() => {
  const mermaidConfig: { suppressErrorRendering?: boolean } = {}
  return {
    initialize: vi.fn((config: { suppressErrorRendering?: boolean }) => Object.assign(mermaidConfig, config)),
    mermaidConfig,
    renderDiagram: vi.fn(),
  }
})

vi.mock("mermaid", () => ({
  default: { initialize, render: renderDiagram },
}))

afterEach(() => {
  cleanup()
  renderDiagram.mockReset()
})

function deferred<T>() {
  let resolve!: (value: T) => void
  let reject!: (reason?: unknown) => void
  const promise = new Promise<T>((resolvePromise, rejectPromise) => {
    resolve = resolvePromise
    reject = rejectPromise
  })
  return { promise, resolve, reject }
}

describe("MermaidDiagram", () => {
  it("initializes Mermaid safely once and displays its SVG in an accessible container", async () => {
    renderDiagram.mockResolvedValue({ svg: '<svg aria-label="flow"><text>Rendered flow</text></svg>' })

    const { rerender } = render(<MermaidDiagram source={"graph TD\n  A --> B"} />)

    const diagram = await screen.findByRole("img", { name: "Mermaid diagram" })
    await waitFor(() => expect(diagram.querySelector("svg")?.textContent).toBe("Rendered flow"))
    expect(initialize).toHaveBeenCalledTimes(1)
    expect(initialize).toHaveBeenCalledWith(expect.objectContaining({
      startOnLoad: false,
      theme: "dark",
      securityLevel: "strict",
      suppressErrorRendering: true,
    }))

    rerender(<MermaidDiagram source={"graph LR\n  B --> C"} />)
    await waitFor(() => expect(renderDiagram).toHaveBeenCalledTimes(2))
    expect(initialize).toHaveBeenCalledTimes(1)
  })

  it("expands on click and collapses by clicking again or pressing Escape", async () => {
    renderDiagram.mockResolvedValue({ svg: "<svg><text>Wide flow</text></svg>" })
    render(<MermaidDiagram source="graph LR\nA-->B" />)

    await screen.findByText("Wide flow")
    const toggle = screen.getByRole("button", { name: "Expand Mermaid diagram" })
    fireEvent.click(toggle)
    expect(screen.getByRole("button", { name: "Collapse Mermaid diagram" }).getAttribute("aria-pressed")).toBe("true")
    expect(screen.getByRole("dialog", { name: "Expanded Mermaid diagram" })).toBeTruthy()

    fireEvent.keyDown(document, { key: "Escape" })
    expect(screen.getByRole("button", { name: "Expand Mermaid diagram" }).getAttribute("aria-pressed")).toBe("false")

    fireEvent.click(screen.getByRole("button", { name: "Expand Mermaid diagram" }))
    fireEvent.click(screen.getByRole("button", { name: "Collapse Mermaid diagram" }))
    expect(screen.queryByRole("dialog", { name: "Expanded Mermaid diagram" })).toBeNull()
  })

  it("uses distinct render IDs and binds interactions to each owned container", async () => {
    const bindFunctions = vi.fn()
    renderDiagram.mockResolvedValue({ svg: "<svg></svg>", bindFunctions })

    render(<><MermaidDiagram source="graph TD\nA-->B"/><MermaidDiagram source="graph TD\nA-->B"/></>)

    await waitFor(() => expect(renderDiagram).toHaveBeenCalledTimes(2))
    expect(renderDiagram.mock.calls[0][0]).not.toBe(renderDiagram.mock.calls[1][0])
    expect(renderDiagram.mock.calls[0][0]).toMatch(/^mermaid-diagram-\d+$/)
    await waitFor(() => expect(bindFunctions).toHaveBeenCalledTimes(2))
    const containers = screen.getAllByRole("img", { name: "Mermaid diagram" })
    expect(bindFunctions).toHaveBeenNthCalledWith(1, expect.any(HTMLElement))
    expect(bindFunctions.mock.calls.map(([container]) => container)).toEqual(expect.arrayContaining(containers))
  })

  it("shows the original source without leaking Mermaid error DOM across failed renders", async () => {
    renderDiagram.mockImplementation((renderID: string) => {
      const temporaryContainer = document.createElement("div")
      temporaryContainer.id = `d${renderID}`
      temporaryContainer.innerHTML = '<svg class="mermaid-error"><text>Syntax error in text</text></svg>'
      document.body.append(temporaryContainer)
      if (mermaidConfig.suppressErrorRendering) temporaryContainer.remove()
      return Promise.reject(new Error("Parse error"))
    })

    const { rerender } = render(<MermaidDiagram source={"not valid mermaid"} />)

    expect(await screen.findByText("Diagram could not be rendered. Mermaid source:" )).toBeTruthy()
    expect(screen.getByText("not valid mermaid")).toBeTruthy()
    expect(document.querySelectorAll('[id^="dmermaid-diagram-"]')).toHaveLength(0)
    expect(document.querySelector("svg.mermaid-error")).toBeNull()

    rerender(<MermaidDiagram source={"still not valid mermaid"} />)

    expect(await screen.findByText("still not valid mermaid")).toBeTruthy()
    await waitFor(() => expect(renderDiagram).toHaveBeenCalledTimes(2))
    expect(document.querySelectorAll('[id^="dmermaid-diagram-"]')).toHaveLength(0)
    expect(document.querySelector("svg.mermaid-error")).toBeNull()
  })

  it("ignores stale output after the source changes", async () => {
    const oldRender = deferred<{ svg: string }>()
    renderDiagram
      .mockReturnValueOnce(oldRender.promise)
      .mockResolvedValueOnce({ svg: "<svg><text>New diagram</text></svg>" })

    const { rerender } = render(<MermaidDiagram source="graph TD\nOld-->Output" />)
    rerender(<MermaidDiagram source="graph TD\nNew-->Output" />)

    expect(await screen.findByText("New diagram")).toBeTruthy()
    oldRender.resolve({ svg: "<svg><text>Stale diagram</text></svg>" })
    await oldRender.promise

    expect(screen.queryByText("Stale diagram")).toBeNull()
    expect(screen.getByText("New diagram")).toBeTruthy()
  })

  it("does not install output or bindings after unmount", async () => {
    const pending = deferred<{ svg: string; bindFunctions: (element: Element) => void }>()
    const bindFunctions = vi.fn()
    renderDiagram.mockReturnValue(pending.promise)

    const { unmount } = render(<MermaidDiagram source="graph TD\nA-->B" />)
    unmount()
    pending.resolve({ svg: "<svg><text>Late diagram</text></svg>", bindFunctions })
    await pending.promise
    await Promise.resolve()

    expect(bindFunctions).not.toHaveBeenCalled()
    expect(screen.queryByText("Late diagram")).toBeNull()
  })
})
