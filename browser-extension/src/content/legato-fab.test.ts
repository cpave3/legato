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
    expect(fab.root.style.background).toContain("linear-gradient")
    expect(fab.root.textContent).toContain("LEGATO")
    expect(fab.title.textContent).toBe("Pull request review")
    expect(fab.subtitle.textContent).toBe("owner/repo · #42")
    expect(fab.button.textContent).toBe("Prepare review")
    expect(fab.button.className).toBe("")
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
