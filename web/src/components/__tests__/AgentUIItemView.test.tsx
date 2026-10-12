import { cleanup, fireEvent, render, screen, within } from "@testing-library/react"
import { afterEach, describe, expect, it, vi } from "vitest"
import { AgentUIItemView } from "../AgentUIItemView"
import type { AgentUIItem } from "../../lib/agentUi"

vi.mock("../../hooks/useServer", () => ({
  useServer: () => ({ baseUrl: "http://legato.test:3080" }),
}))

afterEach(cleanup)

const BOARD = "http://legato.test:3080/agent-ui-images/task-1/" + "a".repeat(64) + ".png"
const CHART = "http://legato.test:3080/agent-ui-images/task-1/" + "b".repeat(64) + ".png"

function item(overrides: Partial<AgentUIItem>): AgentUIItem {
  return {
    id: "ui-0123456789", task_id: "task-1", placement: "panel", format: "html", title: "Screenshots",
    revision: 1, oldest_revision: 1, content: "", resource_domains: [],
    images: [
      { name: "board", url: "/agent-ui-images/task-1/" + "a".repeat(64) + ".png" },
      { name: "chart", url: "/agent-ui-images/task-1/" + "b".repeat(64) + ".png" },
    ],
    created_at: "2026-10-12 01:00:00", updated_at: "2026-10-12 01:00:00", ...overrides,
  }
}

describe("AgentUIItemView with image files", () => {
  it("draws the images of an HTML item from the server the page talks to", () => {
    render(<AgentUIItemView item={item({ content: `<img src="legato-image:board">` })} fill />)

    const frame = screen.getByTitle("Screenshots") as HTMLIFrameElement
    expect(frame.getAttribute("srcdoc")).toContain(`<img src="${BOARD}">`)
  })

  it("draws the images of a Markdown item, and opens one full size on a click", () => {
    render(<AgentUIItemView item={item({ format: "markdown", content: "# Shots\n\n![The board](legato-image:board)\n\n![Builds](legato-image:chart)" })} fill />)

    expect((screen.getByAltText("The board") as HTMLImageElement).src).toBe(BOARD)

    fireEvent.click(screen.getByAltText("Builds"))

    const dialog = screen.getByRole("dialog", { name: "Builds, full size" })
    expect((within(dialog).getByAltText("Builds") as HTMLImageElement).src).toBe(CHART)
    expect(within(dialog).getByText("2 of 2")).toBeTruthy()
  })

  it("leaves an image from elsewhere in Markdown as it is", () => {
    render(<AgentUIItemView item={item({ format: "markdown", content: "![A logo](https://example.com/logo.png)" })} fill />)

    fireEvent.click(screen.getByAltText("A logo"))

    expect(screen.queryByRole("dialog")).toBeNull()
  })

  it("draws the image components of a component item", () => {
    const path = "/home/me/shots/board.png"
    render(<AgentUIItemView fill item={item({
      format: "components",
      content: JSON.stringify([{ type: "image", path, alt: "The board" }]),
      images: [{ name: path, url: "/agent-ui-images/task-1/" + "a".repeat(64) + ".png" }],
    })} />)

    expect((screen.getByAltText("The board") as HTMLImageElement).src).toBe(BOARD)
  })
})
