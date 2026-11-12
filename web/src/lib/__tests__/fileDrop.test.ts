import { afterEach, describe, expect, it } from "vitest"
import { keepFileDropsOnThePage } from "../fileDrop"

function drag(type: "dragover" | "drop", types: string[]) {
  const event = new Event(type, { bubbles: true, cancelable: true })
  Object.defineProperty(event, "dataTransfer", { value: { types, dropEffect: "move" } })
  return event as Event & { dataTransfer: { types: string[]; dropEffect: string } }
}

describe("keepFileDropsOnThePage", () => {
  let stop = () => {}
  afterEach(() => stop())

  // In the desktop app a file dropped where nothing takes it would open
  // in place of Legato.
  it("stops a file dropped outside a drop target from opening in place of the page", () => {
    stop = keepFileDropsOnThePage(window)
    const over = drag("dragover", ["Files"])
    document.body.dispatchEvent(over)
    expect(over.defaultPrevented).toBe(true)
    expect(over.dataTransfer.dropEffect).toBe("none")
    const drop = drag("drop", ["Files"])
    document.body.dispatchEvent(drop)
    expect(drop.defaultPrevented).toBe(true)
  })

  it("leaves a drop target's own handling and text drags alone", () => {
    stop = keepFileDropsOnThePage(window)
    const target = document.createElement("div")
    document.body.append(target)
    target.addEventListener("dragover", (e) => { e.preventDefault(); (e as DragEvent).dataTransfer!.dropEffect = "copy" })
    const over = drag("dragover", ["Files"])
    target.dispatchEvent(over)
    expect(over.dataTransfer.dropEffect).toBe("copy")
    const text = drag("drop", ["text/plain"])
    document.body.dispatchEvent(text)
    expect(text.defaultPrevented).toBe(false)
    target.remove()
  })
})
