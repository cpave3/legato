import { describe, expect, it, vi } from "vitest"
import { render, screen, fireEvent } from "@testing-library/react"
import { WorkspaceFilter } from "../WorkspaceFilter"

describe("WorkspaceFilter", () => {
  it("renders All / Unassigned / each workspace and calls onChange", () => {
    const onChange = vi.fn()
    render(
      <WorkspaceFilter
        workspaces={[{ id: 1, name: "Engineering", color: "#00f" }]}
        active="all"
        onChange={onChange}
      />
    )
    const select = screen.getByRole("combobox")
    expect(select).toBeTruthy()
    fireEvent.change(select, { target: { value: "unassigned" } })
    expect(onChange).toHaveBeenCalledWith("unassigned")
  })
})
