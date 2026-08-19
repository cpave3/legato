import { describe, expect, it, vi } from "vitest"
import { createReviewPanelController } from "./sidepanel-controller"
import type { ReviewPanelModel } from "./review-model"

const model = (id: string, poll = false): ReviewPanelModel => ({
  state: "active", status: "running", context: { repository: "owner/repo", pullRequestNumber: Number(id) },
  runs: [], selectedRunId: id, poll,
})

describe("review panel controller", () => {
  it("discards an older response after the tab context changes", async () => {
    let resolveFirst!: (value: ReviewPanelModel) => void
    const load = vi.fn()
      .mockImplementationOnce(() => new Promise<ReviewPanelModel>((resolve) => { resolveFirst = resolve }))
      .mockResolvedValueOnce(model("2"))
    const render = vi.fn()
    const controller = createReviewPanelController({ load, render, showError: vi.fn(), schedule: vi.fn(), cancel: vi.fn() })

    const first = controller.followTab(1)
    await controller.followTab(2)
    resolveFirst(model("1"))
    await first

    expect(render).toHaveBeenCalledOnce()
    expect(render).toHaveBeenCalledWith(model("2"))
  })

  it("resets run selection and reloads on a same-tab context update", async () => {
    const load = vi.fn().mockResolvedValue(model("1"))
    const controller = createReviewPanelController({ load, render: vi.fn(), showError: vi.fn(), schedule: vi.fn(), cancel: vi.fn() })
    await controller.followTab(4)
    await controller.selectRun("old")
    await controller.contextUpdated(4)

    expect(load.mock.calls.at(-1)).toEqual([4, undefined])
  })

  it("polls only when the current model requests it", async () => {
    const schedule = vi.fn()
    const cancel = vi.fn()
    const load = vi.fn().mockResolvedValueOnce(model("1", true)).mockResolvedValueOnce(model("1", false))
    const controller = createReviewPanelController({ load, render: vi.fn(), showError: vi.fn(), schedule, cancel })
    await controller.followTab(1)
    expect(schedule).toHaveBeenCalledOnce()
    await controller.refresh()
    expect(cancel).toHaveBeenCalled()
  })
})
