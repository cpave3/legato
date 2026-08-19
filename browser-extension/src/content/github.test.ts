// @vitest-environment jsdom
import { beforeEach, describe, expect, it, vi } from "vitest"
import { parseGitHubPullRequest, reconcileGitHubAction } from "./github"

describe("parseGitHubPullRequest", () => {
  it("extracts canonical context only from positive-number GitHub PR URLs", () => {
    expect(parseGitHubPullRequest("https://github.com/Owner/Repo/pull/123/files")).toEqual({ repository: "Owner/Repo", pullRequestNumber: 123 })
    expect(parseGitHubPullRequest("https://github.com/Owner/Repo/issues/123")).toBeNull()
    expect(parseGitHubPullRequest("https://github.com/Owner/Repo/pull/0")).toBeNull()
    expect(parseGitHubPullRequest("https://example.com/Owner/Repo/pull/1")).toBeNull()
  })
})

describe("reconcileGitHubAction", () => {
  beforeEach(() => { document.body.innerHTML = '<div data-testid="issue-metadata-fixed"></div>' })

  it("renders a body-level floating action independent of GitHub page containers", () => {
    document.body.innerHTML = `
      <div class="discussion-sidebar-item sidebar-assignee js-discussion-sidebar-item"></div>
      <div data-testid="issue-metadata-fixed"></div>
    `
    const send = vi.fn().mockResolvedValue({ ok: true, review: { runs: [] } })
    reconcileGitHubAction(document, "https://github.com/cpave3/legato/pull/12", send)

    const action = document.querySelector<HTMLElement>("#legato-pr-action")!
    expect(action.parentElement).toBe(document.body)
    expect(action.style.position).toBe("fixed")
    expect(action.style.right).toBe("24px")
    expect(action.style.bottom).toBe("24px")
    expect(action.textContent).toContain("LEGATO")
    expect(action.textContent).toContain("Pull request review")
    expect(action.textContent).toContain("cpave3/legato · #12")
  })

  it("inserts one accessible action and removes it outside a PR", () => {
    const send = vi.fn().mockImplementation((message: { type: string }) => Promise.resolve(message.type === "detectGitHubReview"
      ? { ok: true, review: { runs: [] } }
      : { ok: true, prepared: {
        workflowName: "review-pr", repositoryPresetName: "Legato", repository: "cpave3/legato",
        pullRequestNumber: 12, primaryDir: "/projects/legato", inputs: {},
      } }))
    reconcileGitHubAction(document, "https://github.com/cpave3/legato/pull/12", send)
    reconcileGitHubAction(document, "https://github.com/cpave3/legato/pull/12", send)
    expect(document.querySelectorAll("#legato-pr-action")).toHaveLength(1)
    expect(document.querySelector("button")?.textContent).toBe("Prepare Legato review")
    expect(Array.from(document.querySelectorAll("button"), (item) => item.textContent)).toContain("View review")

    reconcileGitHubAction(document, "https://github.com/cpave3/legato/issues/12", send)
    expect(document.querySelector("#legato-pr-action")).toBeNull()
  })

  it("shows only View review when a matching review is detected", async () => {
    const send = vi.fn().mockResolvedValue({ ok: true, review: { runs: [{ id: "run-1" }] } })
    reconcileGitHubAction(document, "https://github.com/cpave3/legato/pull/12", send)

    await vi.waitFor(() => expect(document.querySelector<HTMLButtonElement>("button")?.style.display).toBe("none"))
    expect(Array.from(document.querySelectorAll<HTMLButtonElement>("button")).filter((item) => item.style.display !== "none").map((item) => item.textContent)).toEqual(["View review"])
  })

  it("requests review opening from the explicit user gesture", async () => {
    const send = vi.fn().mockImplementation((message: { type: string }) => Promise.resolve(message.type === "detectGitHubReview"
      ? { ok: true, review: { runs: [{ id: "run-1" }] } }
      : { ok: true }))
    reconcileGitHubAction(document, "https://github.com/cpave3/legato/pull/12", send)
    const view = Array.from(document.querySelectorAll<HTMLButtonElement>("button")).find((item) => item.textContent === "View review")!
    view.click()
    await vi.waitFor(() => expect(send).toHaveBeenCalledWith({
      type: "openReviewPanel",
      context: { repository: "cpave3/legato", pullRequestNumber: 12 },
    }))
  })

  it("requires confirmation after displaying resolved details and suppresses duplicate starts", async () => {
    let resolvePrepare!: (value: unknown) => void
    const send = vi.fn().mockImplementation((message: { type: string }) => {
      if (message.type === "detectGitHubReview") return Promise.resolve({ ok: true, review: { runs: [] } })
      if (message.type === "prepareGitHubWorkflow") return new Promise((resolve) => { resolvePrepare = resolve })
      return Promise.resolve({ ok: true, runUrl: "http://localhost:3080/workflows/runs/run-1" })
    })
    reconcileGitHubAction(document, "https://github.com/cpave3/legato/pull/12", send)
    const button = document.querySelector<HTMLButtonElement>("button")!
    await vi.waitFor(() => expect(button.style.display).toBe("block"))
    send.mockClear()
    button.click()
    button.click()
    expect(send).toHaveBeenCalledTimes(1)
    resolvePrepare({ ok: true, prepared: {
      workflowName: "review-pr", repositoryPresetName: "Legato", repository: "cpave3/legato",
      pullRequestNumber: 12, primaryDir: "/projects/legato", inputs: {},
    } })
    await vi.waitFor(() => expect(button.textContent).toBe("Confirm and start"))
    expect(document.querySelector("[role=status]")?.textContent).toContain("/projects/legato")

    button.click()
    button.click()
    await vi.waitFor(() => expect(send).toHaveBeenCalledTimes(2))
    expect(send.mock.calls[1][0]).toMatchObject({ type: "startPreparedWorkflow" })
  })
})
