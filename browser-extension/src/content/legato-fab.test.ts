// @vitest-environment jsdom
import { beforeEach, describe, expect, it } from "vitest"
import { createLegatoFab } from "./legato-fab"

describe("createLegatoFab", () => {
  beforeEach(() => document.body.replaceChildren())

  it("renders a consistent branded floating panel independent of the host site", () => {
    const fab = createLegatoFab(document, {
      id: "test-legato-fab",
      title: "Pull request review",
      subtitle: "owner/repo · #42",
      actionLabel: "Prepare review",
    })
    document.body.append(fab.root)

    expect(fab.root.dataset.legatoFab).toBe("true")
    expect(fab.root.style.position).toBe("fixed")
    expect(fab.root.style.width).toBe("56px")
    expect(fab.root.style.borderRadius).toBe("50%")
    expect(fab.root.getAttribute("aria-expanded")).toBe("false")
    expect(fab.root.style.background).toContain("linear-gradient")
    expect(fab.root.textContent).toContain("LEGATO")
    expect(fab.root.querySelector("[aria-hidden='true']")?.textContent).toBe("L")
    expect(fab.title.textContent).toBe("Pull request review")
    expect(fab.subtitle.textContent).toBe("owner/repo · #42")
    expect(fab.button.textContent).toBe("Prepare review")
    expect(fab.button.className).toBe("")
  })

  it("expands on hover or keyboard focus and collapses after leaving", () => {
    const fab = createLegatoFab(document, { id: "test-legato-fab", title: "Jira import", subtitle: "AG-9804", actionLabel: "Import ticket" })
    document.body.append(fab.root)
    const secondary = fab.addSecondaryAction("Open board")
    fab.setState("success", "Ticket imported.")

    fab.root.dispatchEvent(new MouseEvent("mouseenter", { bubbles: true }))
    expect(fab.root.dataset.expanded).toBe("true")
    expect(fab.root.getAttribute("aria-expanded")).toBe("true")
    expect(fab.root.style.borderRadius).toBe("12px")
    expect(fab.root.style.maxHeight).toBe("min(560px, calc(100vh - 48px))")
    expect(fab.status.style.display).toBe("block")

    secondary.focus()
    fab.root.dispatchEvent(new MouseEvent("mouseleave", { bubbles: true }))
    expect(fab.root.dataset.expanded).toBe("true")
    secondary.blur()
    fab.root.dispatchEvent(new FocusEvent("focusout", { bubbles: true, relatedTarget: document.body }))
    expect(fab.root.dataset.expanded).toBe("false")
    expect(fab.root.style.width).toBe("56px")
  })

  it("collapses when Escape releases focus", () => {
    const fab = createLegatoFab(document, { id: "test-legato-fab", title: "PR review", subtitle: "#42", actionLabel: "Start" })
    document.body.append(fab.root)
    fab.button.focus()
    fab.root.dispatchEvent(new FocusEvent("focusin", { bubbles: true }))
    expect(fab.root.dataset.expanded).toBe("true")

    fab.root.dispatchEvent(new KeyboardEvent("keydown", { key: "Escape", cancelable: true }))
    expect(document.activeElement).not.toBe(fab.button)
    expect(fab.root.dataset.expanded).toBe("false")
    expect(fab.root.getAttribute("aria-expanded")).toBe("false")
  })

  it("can hide the primary action when only a secondary action applies", () => {
    const fab = createLegatoFab(document, { id: "test-legato-fab", title: "PR review", subtitle: "#42", actionLabel: "Start" })
    const secondary = fab.addSecondaryAction("View review")

    fab.setPrimaryVisible(false)

    expect(fab.button.style.display).toBe("none")
    expect(secondary.style.display).not.toBe("none")
  })

  it("uses shared visual states for loading, success, and error feedback", () => {
    const fab = createLegatoFab(document, { id: "test-legato-fab", title: "Jira import", subtitle: "AG-9804", actionLabel: "Import ticket" })

    fab.setState("loading", "Importing ticket…")
    expect(fab.button.disabled).toBe(true)
    expect(fab.status.dataset.state).toBe("loading")
    expect(fab.status.style.color).toBe("rgb(196, 181, 253)")

    fab.setState("success", "Ticket imported.")
    expect(fab.button.disabled).toBe(false)
    expect(fab.status.dataset.state).toBe("success")
    expect(fab.status.style.color).toBe("rgb(103, 232, 249)")

    fab.setState("error", "Import failed.")
    expect(fab.status.dataset.state).toBe("error")
    expect(fab.status.style.color).toBe("rgb(252, 165, 165)")
  })
})
