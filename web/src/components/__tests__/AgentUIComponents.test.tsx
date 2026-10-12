import { cleanup, fireEvent, render, screen, within } from "@testing-library/react"
import { afterEach, describe, expect, it, vi } from "vitest"
import { AgentUIComponents } from "../AgentUIComponents"
import type { UIComponent } from "../../lib/agentUi"

afterEach(cleanup)

describe("AgentUIComponents", () => {
  it("draws figures, key-value rows, a table and lists from a component card", () => {
    const components: UIComponent[] = [
      { type: "heading", text: "Weather in Brisbane", level: 1 },
      { type: "text", text: "Sunny with a **light** breeze." },
      { type: "stat", label: "Now", value: "27 C", detail: "Feels like 29", tone: "positive" },
      { type: "stats", items: [{ label: "Wind", value: "12 km/h" }, { label: "Humidity", value: "61%", tone: "warning" }] },
      { type: "key_value", items: [{ label: "Sunrise", value: "05:12" }] },
      { type: "table", columns: ["Day", "High"], rows: [["Mon", "28"], ["Tue", "30"]] },
      { type: "list", ordered: true, items: [{ text: "Take a hat" }, { text: "Drink water" }] },
      { type: "badges", items: [{ text: "UV high", tone: "negative" }] },
      { type: "code", language: "sh", text: "curl wttr.in" },
      { type: "divider" },
    ]
    render(<AgentUIComponents components={components} />)

    // A card's level 1 is the top of the card, one below the page's own.
    expect(screen.getByRole("heading", { level: 2, name: "Weather in Brisbane" })).toBeTruthy()
    // Text is Markdown.
    expect(screen.getByText("light").tagName).toBe("STRONG")
    expect(screen.getByText("27 C")).toBeTruthy()
    expect(screen.getByText("Feels like 29")).toBeTruthy()
    expect(screen.getByText("12 km/h")).toBeTruthy()
    expect(screen.getByText("Sunrise")).toBeTruthy()
    const table = screen.getByRole("table")
    expect(within(table).getAllByRole("columnheader").map((cell) => cell.textContent)).toEqual(["Day", "High"])
    expect(within(table).getAllByRole("row")).toHaveLength(3)
    expect(screen.getByRole("list").tagName).toBe("OL")
    expect(screen.getByText("UV high")).toBeTruthy()
    expect(screen.getByText("curl wttr.in").closest("pre")).toBeTruthy()
    expect(screen.getByRole("separator")).toBeTruthy()
  })

  it("draws progress and a bar chart that a screen reader can read as numbers", () => {
    render(<AgentUIComponents components={[
      { type: "progress", label: "Build", fraction: 0.4, detail: "4 of 10 steps" },
      { type: "bar_chart", label: "Rain this week", items: [{ label: "Mon", number: 0 }, { label: "Tue", number: 5 }, { label: "Wed", number: 10 }] },
    ]} />)

    const progress = screen.getByRole("progressbar", { name: "Build" })
    expect(progress.getAttribute("aria-valuenow")).toBe("40")
    expect(screen.getByText("4 of 10 steps")).toBeTruthy()

    const chart = screen.getByRole("list", { name: "Rain this week" })
    const bars = within(chart).getAllByRole("listitem")
    expect(bars.map((bar) => bar.textContent)).toEqual(["Mon0", "Tue5", "Wed10"])
    // Bars are sized against the largest value.
    const widths = bars.map((bar) => (bar.querySelector("[data-bar]") as HTMLElement).style.width)
    expect(widths).toEqual(["0%", "50%", "100%"])
  })

  it("offers an action as a button that proposes its message and sends nothing", () => {
    const onProposeMessage = vi.fn()
    render(<AgentUIComponents onProposeMessage={onProposeMessage} components={[
      { type: "actions", items: [{ label: "7 day forecast", message: "Show me the 7 day forecast" }] },
    ]} />)

    fireEvent.click(screen.getByRole("button", { name: "7 day forecast" }))

    expect(onProposeMessage).toHaveBeenCalledWith("Show me the 7 day forecast")
  })

  it("skips a component it does not know instead of failing the whole card", () => {
    render(<AgentUIComponents components={[{ type: "hologram", text: "?" }, { type: "text", text: "Still here" }]} />)

    expect(screen.getByText("Still here")).toBeTruthy()
    expect(screen.queryByText("?")).toBeNull()
  })
})

describe("AgentUIComponents with image files", () => {
  const hero = "/home/me/shots/board.png"
  const before = "/home/me/shots/before.png"
  const after = "/home/me/shots/after.png"
  const images = [
    { name: hero, address: "http://legato.test/agent-ui-images/t/aaa.png" },
    { name: before, address: "http://legato.test/agent-ui-images/t/bbb.png" },
    { name: after, address: "http://legato.test/agent-ui-images/t/ccc.png" },
  ]
  const components: UIComponent[] = [
    { type: "image", path: hero, alt: "The board", caption: "After the restyle" },
    { type: "image_grid", items: [{ path: before, alt: "Before", caption: "Monday" }, { path: after, alt: "After" }] },
  ]

  it("draws an image from Legato's copy of the file, with its caption", () => {
    render(<AgentUIComponents components={components} images={images} />)

    const image = screen.getByAltText("The board") as HTMLImageElement
    expect(image.src).toBe("http://legato.test/agent-ui-images/t/aaa.png")
    expect(screen.getByText("After the restyle").tagName).toBe("FIGCAPTION")
  })

  it("opens an image full size when it is clicked", () => {
    render(<AgentUIComponents components={components} images={images} />)

    fireEvent.click(screen.getByRole("button", { name: "Show The board full size" }))

    const dialog = screen.getByRole("dialog", { name: "The board, full size" })
    expect((within(dialog).getByAltText("The board") as HTMLImageElement).src).toBe("http://legato.test/agent-ui-images/t/aaa.png")
    fireEvent.keyDown(dialog, { key: "Escape" })
    expect(screen.queryByRole("dialog")).toBeNull()
  })

  it("draws a grid of thumbnails, and steps through every image of the card from the one that was clicked", () => {
    render(<AgentUIComponents components={components} images={images} />)

    const grid = screen.getByRole("list", { name: "Images" })
    expect(within(grid).getAllByRole("listitem")).toHaveLength(2)
    expect(within(grid).getByText("Monday")).toBeTruthy()

    fireEvent.click(within(grid).getByRole("button", { name: "Show After full size" }))

    const dialog = screen.getByRole("dialog", { name: "After, full size" })
    expect(within(dialog).getByText("3 of 3")).toBeTruthy()
    fireEvent.click(within(dialog).getByRole("button", { name: "Previous image" }))
    expect(screen.getByRole("dialog", { name: "Before, full size" })).toBeTruthy()
  })

  it("says so when the copy of an image is not there, in place of a broken picture", () => {
    render(<AgentUIComponents components={[{ type: "image", path: "/home/me/gone.png", alt: "The old board" }]} images={images} />)

    expect(screen.queryByRole("img")).toBeNull()
    expect(screen.getByText(/The old board/).textContent).toContain("not stored")
  })
})
