// @vitest-environment jsdom
import { beforeEach, describe, expect, it } from "vitest"
import { captureStyles, createStyleReader, PARENT_LAYOUT_PROPERTIES, STYLE_PROPERTIES, type StyleReader } from "./scope-styles"

const style = (values: Record<string, string>) => ({ getPropertyValue: (property: string) => values[property] ?? "" })

beforeEach(() => {
  document.body.innerHTML = `<div class="row" id="parent"><button class="btn btn-primary" style="margin-top: 3px">Save</button></div>`
})

describe("a pin's styles", () => {
  it("names about thirty properties that matter for visual bugs", () => {
    expect(STYLE_PROPERTIES.length).toBeGreaterThanOrEqual(28)
    expect(STYLE_PROPERTIES.length).toBeLessThanOrEqual(40)
    for (const property of ["display", "position", "color", "background-color", "font-size", "padding", "margin", "z-index", "opacity", "overflow"]) {
      expect(STYLE_PROPERTIES).toContain(property)
    }
    for (const property of ["display", "flex-direction", "justify-content", "align-items", "gap", "grid-template-columns", "position"]) {
      expect(PARENT_LAYOUT_PROPERTIES).toContain(property)
    }
  })

  it("keeps the computed values that differ from the browser's default for the tag, with the classes and the inline style", () => {
    const button = document.querySelector("button")!
    const reader: StyleReader = {
      computed: (element) => element === button
        ? style({ display: "inline-block", color: "rgb(255, 0, 0)", "font-size": "13.3333px", "padding": "8px 16px" })
        : style({ display: "flex", "flex-direction": "row", gap: "12px", position: "static", "align-items": "normal" }),
      defaults: (tag) => tag === "button"
        ? style({ display: "inline-block", color: "rgb(0, 0, 0)", "font-size": "13.3333px", padding: "1px 6px" })
        : style({ display: "block", "flex-direction": "row", gap: "normal", position: "static", "align-items": "normal" }),
    }
    expect(captureStyles(button, reader)).toEqual({
      computed: { color: "rgb(255, 0, 0)", padding: "8px 16px" },
      classes: ["btn", "btn-primary"],
      inline: "margin-top: 3px;",
      parent: { display: "flex", gap: "12px", position: "static" },
    })
  })

  it("keeps every value when the defaults cannot be read", () => {
    const button = document.querySelector("button")!
    const reader: StyleReader = { computed: () => style({ display: "inline-block", color: "red" }), defaults: () => null }
    const styles = captureStyles(button, reader)
    expect(styles.computed).toEqual({ display: "inline-block", color: "red" })
  })

  it("has no parent properties for the root element", () => {
    const reader: StyleReader = { computed: () => style({ display: "block" }), defaults: () => null }
    expect(captureStyles(document.documentElement, reader).parent).toEqual({})
  })
})

describe("the browser's defaults", () => {
  it("are read from a fresh element in an iframe of their own, which dispose removes", () => {
    const container = document.createElement("div")
    document.body.append(container)
    const sheet = document.createElement("style")
    sheet.textContent = "div { display: flex }"
    document.head.append(sheet)
    const reader = createStyleReader(document, container)
    expect(reader.defaults("div")?.getPropertyValue("display")).toBe("block")
    expect(container.querySelector("iframe")).not.toBeNull()
    reader.dispose()
    expect(container.querySelector("iframe")).toBeNull()
    sheet.remove()
  })
})
