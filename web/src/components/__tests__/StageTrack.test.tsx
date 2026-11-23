import { cleanup, render, screen } from "@testing-library/react"
import { afterEach, describe, expect, it } from "vitest"
import { StageLine, StageTrack } from "../StageTrack"

afterEach(cleanup)

describe("the stage track", () => {
  it("is named by its stage's word when it stands alone", () => {
    render(<StageTrack stage="reviewing" />)
    const track = screen.getByRole("img", { name: "Reviewing" })
    expect(track.getAttribute("title")).toBe("Reviewing")
  })

  it("takes another name when given one", () => {
    render(<StageTrack stage="implementing" label="Stage: Implementing" />)
    expect(screen.getByRole("img", { name: "Stage: Implementing" })).toBeTruthy()
  })

  it("is left out of the accessible tree beside its word", () => {
    render(<StageTrack stage="planning" label="" />)
    expect(screen.queryByRole("img")).toBeNull()
  })

  it("draws nothing for an agent with no stage", () => {
    const { container } = render(<StageTrack stage={undefined} />)
    expect(container.textContent).toBe("")
    expect(container.firstChild).toBeNull()
  })

  it("comes with its word and reason on a stage line", () => {
    render(<StageLine agent={{ stage: "finished", stage_reason: "PR #212 merged" }} />)
    expect(screen.getByText("Finished")).toBeTruthy()
    expect(screen.getByText("· PR #212 merged")).toBeTruthy()
    expect(screen.getByRole("img", { name: "Finished" })).toBeTruthy()
  })
})
