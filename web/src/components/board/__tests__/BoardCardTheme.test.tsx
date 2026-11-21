import { readFileSync } from "node:fs"
import { afterEach, describe, expect, it } from "vitest"
import { cleanup, render, screen } from "@testing-library/react"
import { MemoryRouter } from "react-router-dom"
import { BoardCard } from "../BoardCard"
import type { BoardCard as BoardCardType } from "../../../lib/board-types"

// The app's own stylesheets, as the browser reads them: Tailwind's
// directives are left to the build, the plain rules apply.
const css = ["src/index.css", "src/theme.css"].map(path => readFileSync(path, "utf8")).join("\n")

const card = {
  id: "T-1", title: "Linked card", priority: "", issue_type: "", status: "Doing", provider: "", has_warning: false,
  agent_active: false, agent_state: "", working_seconds: 0, waiting_seconds: 0, error_seconds: 0,
  workspace_id: null, workspace_name: "", workspace_color: "",
} as BoardCardType

afterEach(() => {
  cleanup()
  document.getElementById("app-css")?.remove()
  document.documentElement.removeAttribute("data-theme")
})

describe("the highlight ring", () => {
  it("survives a named theme's panel shadow", () => {
    const style = document.createElement("style")
    style.id = "app-css"
    style.textContent = css
    document.head.appendChild(style)
    document.documentElement.setAttribute("data-theme", "paper")
    render(<MemoryRouter><BoardCard card={card} selected={false} highlighted column="Doing" showWorkspace={false} onClick={() => {}} /></MemoryRouter>)
    const root = screen.getByText("Linked card").closest("[data-card]") as HTMLElement
    const shadow = getComputedStyle(root).boxShadow
    expect(shadow).toContain("var(--theme-focus")
    expect(shadow).not.toBe("var(--shadow-lw-panel)")
  })
})
