import { describe, expect, it, vi } from "vitest"
import { render, screen, fireEvent, within } from "@testing-library/react"
import { WorkspaceFilter } from "../WorkspaceFilter"

describe("WorkspaceFilter", () => {
  it("offers All, Unassigned and each workspace, and says which is shown", () => {
    const onChange = vi.fn()
    render(
      <WorkspaceFilter
        workspaces={[{ id: 1, name: "Engineering", color: "#00f" }]}
        active="unassigned"
        onChange={onChange}
      />
    )
    const group = screen.getByRole("group", { name: "Workspace" })
    expect(within(group).getAllByRole("button").map((button) => button.textContent)).toEqual(["All", "Unassigned", "Engineering"])
    expect(within(group).getByRole("button", { name: "Unassigned" }).getAttribute("aria-pressed")).toBe("true")
    fireEvent.click(within(group).getByRole("button", { name: "Engineering" }))
    expect(onChange).toHaveBeenCalledWith("1")
  })
})
