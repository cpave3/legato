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

  it("appends the action to the bottom of the reviewers sidebar", () => {
    document.body.innerHTML = `
      <div class="discussion-sidebar-item sidebar-assignee js-discussion-sidebar-item">
        <form id="reviewers-form"></form>
      </div>
      <div data-testid="issue-metadata-fixed"></div>
    `
    const send = vi.fn()
    reconcileGitHubAction(document, "https://github.com/cpave3/legato/pull/12", send)

    const sidebar = document.querySelector(".discussion-sidebar-item.sidebar-assignee")!
    expect(sidebar.lastElementChild?.id).toBe("legato-pr-action")
    expect(document.querySelector('[data-testid="issue-metadata-fixed"]')?.children).toHaveLength(0)
  })

  it("inserts one accessible action and removes it outside a PR", () => {
    const send = vi.fn().mockResolvedValue({ ok: true, prepared: {
      workflowName: "review-pr", repositoryPresetName: "Legato", repository: "cpave3/legato",
      pullRequestNumber: 12, primaryDir: "/projects/legato", inputs: {},
    } })
    reconcileGitHubAction(document, "https://github.com/cpave3/legato/pull/12", send)
    reconcileGitHubAction(document, "https://github.com/cpave3/legato/pull/12", send)
    expect(document.querySelectorAll("#legato-pr-action")).toHaveLength(1)
    expect(document.querySelector("button")?.textContent).toBe("Prepare Legato review")

    reconcileGitHubAction(document, "https://github.com/cpave3/legato/issues/12", send)
    expect(document.querySelector("#legato-pr-action")).toBeNull()
  })

  it("requires confirmation after displaying resolved details and suppresses duplicate starts", async () => {
    let resolvePrepare!: (value: unknown) => void
    const send = vi.fn().mockImplementation((message: { type: string }) => {
      if (message.type === "prepareGitHubWorkflow") return new Promise((resolve) => { resolvePrepare = resolve })
      return Promise.resolve({ ok: true, runUrl: "http://localhost:3080/workflows/runs/run-1" })
    })
    reconcileGitHubAction(document, "https://github.com/cpave3/legato/pull/12", send)
    const button = document.querySelector<HTMLButtonElement>("button")!
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
