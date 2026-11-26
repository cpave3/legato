import { afterEach, describe, expect, it } from "vitest"
import { cleanup, render, screen, within } from "@testing-library/react"
import { CardPullRequests } from "../PullRequests"
import type { BoardCard, PRMetaView } from "../../../lib/board-types"

// The PR chip says stewarding or held while a review loop's steward looks
// after the pull request (spec LEG-559 story 13, AC 39; LEG-594).

function pr(number: number, overrides: Partial<PRMetaView> = {}): PRMetaView {
  return {
    repo: "acme/app", branch: `branch-${number}`, pr_number: number, pr_url: `https://github.com/acme/app/pull/${number}`,
    state: "OPEN", is_draft: false, review_decision: "APPROVED", check_status: "pass", comment_count: 0,
    pinned: false, match_method: "branch", sync_outcome: "linked", sync_error: "",
    ...overrides,
  }
}

const card = (prs: PRMetaView[]) => ({ id: "T-1", title: "Greeting", status: "Doing", pull_requests: prs }) as unknown as BoardCard

describe("the steward on a card's pull requests", () => {
  afterEach(cleanup)

  it("says stewarding on a pull request the steward watches", () => {
    render(<CardPullRequests card={card([pr(42, { steward: "stewarding" })])} />)
    const chip = within(screen.getByLabelText("Pull request #42")).getByText("stewarding")
    expect(chip.getAttribute("title")).toContain("Legato's steward")
  })

  it("says held when the operator stopped it watching", () => {
    render(<CardPullRequests card={card([pr(42, { steward: "held" })])} />)
    expect(within(screen.getByLabelText("Pull request #42")).getByText("held")).toBeTruthy()
  })

  it("says nothing without a steward", () => {
    render(<CardPullRequests card={card([pr(42)])} />)
    expect(screen.getByLabelText("Pull request #42").textContent).not.toMatch(/stewarding|held/)
  })

  it("puts a stack's steward beside its title", () => {
    render(<CardPullRequests card={card([pr(41, { steward: "held" }), pr(42, { base: "branch-41", steward: "held" })])} />)
    expect(within(screen.getByTestId("pr-stack")).getByText("held")).toBeTruthy()
  })
})
