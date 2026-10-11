import { cleanup, fireEvent, render, screen } from "@testing-library/react"
import { afterEach, describe, expect, it, vi } from "vitest"
import { SandboxedHtml } from "../SandboxedHtml"

afterEach(() => {
  cleanup()
  vi.restoreAllMocks()
})

function frame(): HTMLIFrameElement {
  return screen.getByTitle("Weather in Brisbane") as HTMLIFrameElement
}

// A message as the frame's own bootstrap script posts it.
function fromFrame(method: string, params: unknown) {
  fireEvent(window, new MessageEvent("message", { data: { jsonrpc: "2.0", method, params }, source: frame().contentWindow }))
}

describe("SandboxedHtml", () => {
  it("draws the agent's HTML in a frame that may run scripts and nothing else", () => {
    render(<SandboxedHtml html="<b>27 C</b>" title="Weather in Brisbane" resourceDomains={[]} />)

    // No allow-same-origin: the content gets an origin of its own, so it
    // cannot read Legato's token or call its API as the person.
    expect(frame().getAttribute("sandbox")).toBe("allow-scripts")
    expect(frame().getAttribute("srcdoc")).toContain("<b>27 C</b>")
    expect(frame().getAttribute("srcdoc")).toContain("Content-Security-Policy")
    expect(frame().getAttribute("referrerpolicy")).toBe("no-referrer")
    expect(screen.getByText("Made by the agent")).toBeTruthy()
  })

  it("grows to the height its content reports, up to a limit", () => {
    render(<SandboxedHtml html="<b>27 C</b>" title="Weather in Brisbane" resourceDomains={[]} maxHeight={400} />)

    fromFrame("ui/notifications/size-changed", { width: 300, height: 180 })
    expect(frame().style.height).toBe("180px")

    fromFrame("ui/notifications/size-changed", { width: 300, height: 5000 })
    expect(frame().style.height).toBe("400px")
  })

  it("tells its owner when the content is taller than the limit, so it can offer a larger view", () => {
    const onOverflow = vi.fn()
    render(<SandboxedHtml html="<b>27 C</b>" title="Weather in Brisbane" resourceDomains={[]} maxHeight={400} onOverflow={onOverflow} />)

    fromFrame("ui/notifications/size-changed", { width: 300, height: 200 })
    expect(onOverflow).toHaveBeenLastCalledWith(false)
    fromFrame("ui/notifications/size-changed", { width: 300, height: 900 })
    expect(onOverflow).toHaveBeenLastCalledWith(true)
  })

  it("hands a proposed message to its owner instead of sending anything", () => {
    const onProposeMessage = vi.fn()
    render(<SandboxedHtml html="<b>27 C</b>" title="Weather in Brisbane" resourceDomains={[]} onProposeMessage={onProposeMessage} />)

    fromFrame("ui/message", { role: "user", content: { type: "text", text: "Show the 7 day forecast" } })

    expect(onProposeMessage).toHaveBeenCalledWith("Show the 7 day forecast")
  })

  it("opens a web link only after the person agrees", () => {
    const open = vi.spyOn(window, "open").mockReturnValue(null)
    const confirm = vi.spyOn(window, "confirm").mockReturnValue(false)
    render(<SandboxedHtml html="<a href='https://example.com'>x</a>" title="Weather in Brisbane" resourceDomains={[]} />)

    fromFrame("ui/open-link", { url: "https://example.com/forecast" })
    expect(confirm).toHaveBeenCalledTimes(1)
    expect(open).not.toHaveBeenCalled()

    confirm.mockReturnValue(true)
    fromFrame("ui/open-link", { url: "https://example.com/forecast" })
    expect(open).toHaveBeenCalledWith("https://example.com/forecast", "_blank", "noopener,noreferrer")
  })

  it.each(["javascript:alert(1)", "data:text/html,<script>alert(1)</script>", "file:///etc/passwd", "not a url"])("never opens %s", (url) => {
    const open = vi.spyOn(window, "open").mockReturnValue(null)
    const confirm = vi.spyOn(window, "confirm").mockReturnValue(true)
    render(<SandboxedHtml html="<b>x</b>" title="Weather in Brisbane" resourceDomains={[]} />)

    fromFrame("ui/open-link", { url })

    expect(confirm).not.toHaveBeenCalled()
    expect(open).not.toHaveBeenCalled()
  })

  it("ignores messages that do not come from its own frame", () => {
    const onProposeMessage = vi.fn()
    const open = vi.spyOn(window, "open").mockReturnValue(null)
    vi.spyOn(window, "confirm").mockReturnValue(true)
    render(<SandboxedHtml html="<b>27 C</b>" title="Weather in Brisbane" resourceDomains={[]} onProposeMessage={onProposeMessage} />)

    for (const source of [window, null]) {
      fireEvent(window, new MessageEvent("message", { data: { jsonrpc: "2.0", method: "ui/message", params: { content: { type: "text", text: "rm -rf /" } } }, source }))
      fireEvent(window, new MessageEvent("message", { data: { jsonrpc: "2.0", method: "ui/open-link", params: { url: "https://evil.example.com" } }, source }))
      fireEvent(window, new MessageEvent("message", { data: { jsonrpc: "2.0", method: "ui/notifications/size-changed", params: { height: 999 } }, source }))
    }

    expect(onProposeMessage).not.toHaveBeenCalled()
    expect(open).not.toHaveBeenCalled()
    expect(frame().style.height).not.toBe("999px")
  })

  it("fills its container when asked to, leaving scrolling to the content", () => {
    render(<SandboxedHtml html="<b>27 C</b>" title="Weather in Brisbane" resourceDomains={[]} fill />)

    fromFrame("ui/notifications/size-changed", { width: 300, height: 180 })

    expect(frame().style.height).toBe("")
    expect(frame().className).toContain("h-full")
  })
})
