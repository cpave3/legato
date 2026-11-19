// @vitest-environment jsdom
import { beforeEach, describe, expect, it, vi } from "vitest"
import { renderScopeOrigins } from "./scope-origins-view"

let container: HTMLElement
beforeEach(() => {
  document.body.innerHTML = `<div id="scope"></div>`
  container = document.querySelector("#scope")!
})

describe("the options page's Scope sites", () => {
  it("lists each allowed origin with Remove, and says when Chrome's access is gone", () => {
    const onRemove = vi.fn()
    renderScopeOrigins(container, { origins: [{ origin: "http://localhost:5173", allowed: true }, { origin: "https://gone.test", allowed: false }] }, onRemove)
    const rows = [...container.querySelectorAll("li")]
    expect(rows.map((row) => row.querySelector("code")?.textContent)).toEqual(["http://localhost:5173", "https://gone.test"])
    expect(rows[0].textContent).not.toContain("Access removed")
    expect(rows[1].textContent).toContain("Access removed")
    rows[1].querySelector("button")!.click()
    expect(onRemove).toHaveBeenCalledWith("https://gone.test")
  })

  it("explains how to add one when there are none, and shows an error", () => {
    renderScopeOrigins(container, { origins: [], error: "boom" }, vi.fn())
    expect(container.textContent).toContain("toolbar button")
    expect(container.querySelector(".scope-origins-error")?.textContent).toBe("boom")
  })
})
