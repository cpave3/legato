// @vitest-environment jsdom
import { beforeEach, describe, expect, it } from "vitest"
import { accessibleName, roleOf, selectorCandidates } from "./scope-capture"
import { locateTarget, resolveSelector } from "./scope-capture"

beforeEach(() => {
  document.body.innerHTML = `
    <form>
      <label for="email">Email address</label><input id="email" type="email">
      <label>Name <input type="text" class="field"></label>
      <button data-testid="save-button" id="save" class="btn btn-primary">Save</button>
      <button class="btn btn-secondary" aria-label="Close dialog">×</button>
      <button class="btn">Cancel</button>
      <button class="btn">Cancel</button>
      <a href="/help" class="link muted">Help</a>
      <div class="card"><span class="title">One</span></div>
      <div class="card"><span class="title">Two</span></div>
      <h2>Billing</h2>
      <div data-cy="cy-target">x</div>
    </form>`
})

const $ = (selector: string) => document.querySelector(selector)!

describe("roles and accessible names", () => {
  it("knows the implicit roles of common elements and an explicit role first", () => {
    expect(roleOf($("button"))).toBe("button")
    expect(roleOf($("a"))).toBe("link")
    expect(roleOf($("h2"))).toBe("heading")
    expect(roleOf($("#email"))).toBe("textbox")
    const div = $(".card")
    div.setAttribute("role", "tab")
    expect(roleOf(div)).toBe("tab")
    expect(roleOf($("span"))).toBeNull()
  })

  it("names an element from aria-label, its label, or its text", () => {
    expect(accessibleName($("[aria-label]"))).toBe("Close dialog")
    expect(accessibleName($("#email"))).toBe("Email address")
    expect(accessibleName($(".field"))).toBe("Name")
    expect(accessibleName($("#save"))).toBe("Save")
  })
})

describe("selector candidates", () => {
  it("puts a test ID first, then the id, then role and name, then classes, then the path", () => {
    const candidates = selectorCandidates($("#save"))
    expect(candidates[0]).toBe('[data-testid="save-button"]')
    expect(candidates[1]).toBe("#save")
    expect(candidates[2]).toBe('role=button[name="Save"]')
    expect(candidates).toContain("button.btn-primary")
    expect(candidates.at(-1)).toMatch(/^body > form:nth-child\(1\) > button:nth-child\(\d+\)$/)
  })

  it("reads data-cy as a test ID", () => {
    expect(selectorCandidates($("[data-cy]"))[0]).toBe('[data-cy="cy-target"]')
  })

  it("keeps only candidates that match the element alone", () => {
    const cancel = document.querySelectorAll("button")[2]
    const candidates = selectorCandidates(cancel)
    expect(candidates.some((c) => c.includes('name="Cancel"'))).toBe(false)
    expect(candidates.some((c) => c.startsWith("button.btn") && !c.includes("nth-child"))).toBe(false)
    for (const candidate of candidates) expect(resolveSelector(document, candidate)).toBe(cancel)
  })

  it("combines classes when one is not enough", () => {
    expect(selectorCandidates($("[aria-label]"))).toContain("button.btn-secondary")
    expect(selectorCandidates($("a"))).toContain("a.link")
  })

  it("quotes values that are not plain identifiers", () => {
    const odd = document.createElement("div")
    odd.id = "1st:item"
    odd.setAttribute("data-testid", 'say "hi"')
    document.body.append(odd)
    const candidates = selectorCandidates(odd)
    expect(candidates[0]).toBe('[data-testid="say \\"hi\\""]')
    expect(candidates).toContain("#\\31 st\\:item")
    for (const candidate of candidates) expect(resolveSelector(document, candidate)).toBe(odd)
  })

  it("resolves a role candidate", () => {
    expect(resolveSelector(document, 'role=link[name="Help"]')).toBe($("a"))
    expect(resolveSelector(document, 'role=heading[name="Billing"]')).toBe($("h2"))
    expect(resolveSelector(document, 'role=button[name="Cancel"]')).toBeNull()
  })
})

describe("finding a pin's element again", () => {
  const target = (selectors: string[], text: string) => ({ selectors, text, box: { x: 0, y: 0, width: 0, height: 0 } })

  it("prefers a selector whose element still has the pin's text", () => {
    const spans = document.querySelectorAll(".title")
    expect(locateTarget(document, target(["body > form:nth-child(1) > div:nth-child(9) > span:nth-child(1)", "body > form:nth-child(1) > div:nth-child(10) > span:nth-child(1)"], "Two"))).toBe(spans[1])
  })

  it("falls back to the text when no selector matches", () => {
    expect(locateTarget(document, target(["#gone"], "Billing"))).toBe($("h2"))
  })

  it("takes the first matching selector when the text changed", () => {
    expect(locateTarget(document, target(["#save"], "Store"))).toBe($("#save"))
  })

  it("finds nothing when neither selectors nor text match one element", () => {
    expect(locateTarget(document, target(["#gone"], "Cancel"))).toBeNull()
  })
})
