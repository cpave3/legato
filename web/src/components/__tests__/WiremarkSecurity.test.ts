import { describe, expect, it } from "vitest"
import { render } from "@wiremark/core"

describe("pinned Wiremark SVG contract", () => {
  it("renders plan-authored content without executable, external, or navigable DOM", () => {
    const source = `Wireframe #start mobile
  Typography "<script>alert(1)</script> onload=\\"run()\\""
  Img src="https://evil.example/tracker.png"
  Button "Continue" contained to=#done
Wireframe #done mobile
  Typography "Done"`

    const { svg } = render(source, { theme: "dark", interactive: true })
    const host = document.createElement("div")
    host.innerHTML = svg
    const attributes = [...host.querySelectorAll("*")].flatMap(element => [...element.attributes])

    expect(host.querySelector("script")).toBeNull()
    expect(attributes.filter(attribute => /^on/i.test(attribute.name))).toEqual([])
    expect(host.querySelector("a[href], image[href], image[xlink\\:href], use[href], use[xlink\\:href]")).toBeNull()
    expect(attributes.filter(attribute => /^(?:href|xlink:href|src)$/i.test(attribute.name))).toEqual([])
    expect(host.querySelector("[data-wm-to='done']")).toBeTruthy()
    expect(host.textContent).toContain('<script>alert(1)</script> onload="run()"')
  })
})
