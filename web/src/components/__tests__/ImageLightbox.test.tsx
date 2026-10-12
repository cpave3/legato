import { cleanup, fireEvent, render, screen } from "@testing-library/react"
import { afterEach, describe, expect, it, vi } from "vitest"
import { ImageLightbox } from "../ImageLightbox"

afterEach(cleanup)

const IMAGES = [
  { src: "http://legato.test/agent-ui-images/t/a.png", alt: "The board", caption: "Before the restyle" },
  { src: "http://legato.test/agent-ui-images/t/b.png", alt: "The agents view" },
  { src: "http://legato.test/agent-ui-images/t/c.png", alt: "" },
]

describe("ImageLightbox", () => {
  it("shows one image over the whole window, with its caption and its place among the others", () => {
    render(<ImageLightbox images={IMAGES} index={0} onIndex={vi.fn()} onClose={vi.fn()} />)

    const dialog = screen.getByRole("dialog", { name: "The board, full size" })
    expect(dialog.getAttribute("aria-modal")).toBe("true")
    expect((screen.getByAltText("The board") as HTMLImageElement).src).toBe(IMAGES[0].src)
    expect(screen.getByText("Before the restyle")).toBeTruthy()
    expect(screen.getByText("1 of 3")).toBeTruthy()
    // The keyboard goes to it, so Escape and the arrows work at once.
    expect(document.activeElement).toBe(dialog)
  })

  it("steps to the next and the previous image, around the ends", () => {
    const onIndex = vi.fn()
    const { rerender } = render(<ImageLightbox images={IMAGES} index={0} onIndex={onIndex} onClose={vi.fn()} />)

    fireEvent.click(screen.getByRole("button", { name: "Next image" }))
    expect(onIndex).toHaveBeenLastCalledWith(1)
    fireEvent.click(screen.getByRole("button", { name: "Previous image" }))
    expect(onIndex).toHaveBeenLastCalledWith(2)

    rerender(<ImageLightbox images={IMAGES} index={2} onIndex={onIndex} onClose={vi.fn()} />)
    fireEvent.keyDown(screen.getByRole("dialog"), { key: "ArrowRight" })
    expect(onIndex).toHaveBeenLastCalledWith(0)
    fireEvent.keyDown(screen.getByRole("dialog"), { key: "ArrowLeft" })
    expect(onIndex).toHaveBeenLastCalledWith(1)
    // An image with nothing to say about itself is still named.
    expect(screen.getByRole("dialog", { name: "Image 3, full size" })).toBeTruthy()
  })

  it("has nothing to step through when there is one image", () => {
    render(<ImageLightbox images={[IMAGES[0]]} index={0} onIndex={vi.fn()} onClose={vi.fn()} />)

    expect(screen.queryByRole("button", { name: "Next image" })).toBeNull()
    expect(screen.queryByText("1 of 1")).toBeNull()
  })

  it("closes on Escape, on its button and on a click beside the image, and keeps Escape from what is behind it", () => {
    const onClose = vi.fn()
    const behind = vi.fn()
    render(
      <div onKeyDown={behind}>
        <ImageLightbox images={IMAGES} index={0} onIndex={vi.fn()} onClose={onClose} />
      </div>,
    )

    fireEvent.keyDown(screen.getByRole("dialog"), { key: "Escape" })
    fireEvent.click(screen.getByRole("button", { name: "Close image" }))
    fireEvent.click(screen.getByTestId("lightbox-stage"))
    expect(onClose).toHaveBeenCalledTimes(3)
    // A sheet behind it would close on the same Escape.
    expect(behind).not.toHaveBeenCalled()
  })

  it("switches between the image fitted to the window and at its own size on a click", () => {
    const onClose = vi.fn()
    render(<ImageLightbox images={IMAGES} index={0} onIndex={vi.fn()} onClose={onClose} />)

    const image = screen.getByAltText("The board")
    expect(image.getAttribute("data-size")).toBe("fit")
    fireEvent.click(image)
    expect(image.getAttribute("data-size")).toBe("actual")
    fireEvent.click(image)
    expect(image.getAttribute("data-size")).toBe("fit")
    expect(onClose).not.toHaveBeenCalled()
  })
})
