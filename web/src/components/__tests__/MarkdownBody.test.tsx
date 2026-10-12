import { cleanup, fireEvent, render, screen, within } from "@testing-library/react"
import { afterEach, describe, expect, it } from "vitest"
import { MarkdownBody } from "../MarkdownBody"
import { documentImages } from "../../lib/documentImages"

afterEach(cleanup)

const SERVER = "http://legato.test:3080"
const BOARD = "/plan-images/pl-task-1/" + "a".repeat(64) + ".png"
const AFTER = "/plan-images/pl-task-1/" + "b".repeat(64) + ".png"
const images = documentImages(SERVER, [
  { name: "assets/board.png", url: BOARD },
  { name: "assets/after shot.png", url: AFTER },
])

describe("documentImages", () => {
  it("says where each image is at the server the page talks to", () => {
    expect(images).toEqual([
      { name: "assets/board.png", address: SERVER + BOARD },
      { name: "assets/after shot.png", address: SERVER + AFTER },
    ])
    expect(documentImages(SERVER, [{ name: "a.png", url: "/review-images/rt-task-1/" + "c".repeat(64) + ".png" }])).toHaveLength(1)
  })

  it("leaves out an address that is not one of Legato's kept images", () => {
    expect(documentImages(SERVER, [
      { name: "api", url: "/api/settings" },
      { name: "elsewhere", url: "https://example.com/plan-images/x/y.png" },
      { name: "climbs", url: "/plan-images/../api/settings" },
    ])).toEqual([])
    expect(documentImages(SERVER, undefined)).toEqual([])
  })
})

describe("MarkdownBody with a document's image files", () => {
  it("draws an image the Markdown names from Legato's copy of it", () => {
    render(<MarkdownBody body={"# Plan\n\n![The board today](assets/board.png)"} images={images} />)

    expect((screen.getByAltText("The board today") as HTMLImageElement).src).toBe(SERVER + BOARD)
  })

  it("finds an image whose path the Markdown wrote with its spaces escaped", () => {
    render(<MarkdownBody body={"![After](assets/after%20shot.png)"} images={images} />)

    expect((screen.getByAltText("After") as HTMLImageElement).src).toBe(SERVER + AFTER)
  })

  it("opens an image full size, with the document's others to step through", () => {
    render(<MarkdownBody body={"![The board today](assets/board.png)\n\nText between.\n\n![After](<assets/after shot.png>)"} images={images} />)

    fireEvent.click(screen.getByRole("button", { name: "Show After full size" }))

    const dialog = screen.getByRole("dialog", { name: "After, full size" })
    expect((within(dialog).getByAltText("After") as HTMLImageElement).src).toBe(SERVER + AFTER)
    expect(within(dialog).getByText("2 of 2")).toBeTruthy()

    fireEvent.click(within(dialog).getByRole("button", { name: "Previous image" }))
    expect(screen.getByRole("dialog", { name: "The board today, full size" })).toBeTruthy()

    fireEvent.click(screen.getByRole("button", { name: "Close image" }))
    expect(screen.queryByRole("dialog")).toBeNull()
  })

  it("says so when the copy of an image is not stored, and draws no broken picture", () => {
    render(<MarkdownBody body={"![An old shot](assets/gone.png)"} images={images} />)

    expect(screen.queryByRole("img")).toBeNull()
    expect(screen.getByText(/An old shot/).textContent).toContain("not stored")
  })

  it("leaves an image at a web address to be loaded from there", () => {
    render(<MarkdownBody body={"![A logo](https://example.com/logo.png)"} images={images} />)

    expect((screen.getByAltText("A logo") as HTMLImageElement).src).toBe("https://example.com/logo.png")
    expect(screen.queryByRole("button", { name: /full size/ })).toBeNull()
  })

  it("draws Markdown with no list of images as it did before", () => {
    render(<MarkdownBody body={"![A logo](https://example.com/logo.png)"} />)

    expect((screen.getByAltText("A logo") as HTMLImageElement).src).toBe("https://example.com/logo.png")
  })
})
