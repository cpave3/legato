import { cleanup, fireEvent, render, screen, within } from "@testing-library/react"
import { MemoryRouter } from "react-router-dom"
import { afterEach, describe, expect, it, vi } from "vitest"
import { RelatedWorkPanel } from "../RelatedWorkPanel"
import type { RelatedWorkItem } from "../../lib/task-activity"
import type { CardDetail } from "../../lib/board-types"

afterEach(cleanup)

const items: RelatedWorkItem[] = [
  { kind: "review", id: "rt-1", title: "Default review", status: "capturing", href: "/review/rt-1" },
  { kind: "plan", id: "pl-1", title: "Add search", status: "proposed", href: "/plans/pl-1" },
]

const card = {
  id: "AG-10100",
  title: "Complete the Cover section",
  status: "Doing",
  pr_meta: { repo: "acme/site", branch: "cover-blocks", pr_number: 1334, pr_url: "https://github.com/acme/site/pull/1334", state: "OPEN", is_draft: false, review_decision: "APPROVED", check_status: "fail", comment_count: 12 },
} as unknown as CardDetail

function show(props: Partial<Parameters<typeof RelatedWorkPanel>[0]> = {}) {
  return render(
    <MemoryRouter>
      <RelatedWorkPanel items={items} card={card} cardId="AG-10100" onClose={() => {}} mobile={false} {...props} />
    </MemoryRouter>,
  )
}

describe("RelatedWorkPanel", () => {
  it("lists what is still under way first", () => {
    show()
    const progress = screen.getByRole("group", { name: "In progress" })
    const review = within(progress).getByRole("link", { name: /Default review/ })
    expect(review.getAttribute("href")).toBe("/review/rt-1")
    expect(review.textContent).toContain("capturing")
    expect(review.textContent).toContain("Recording changes as the agent works")
  })

  it("links the agent's card, its pull request and the rest of its work", () => {
    show()
    const linked = screen.getByRole("group", { name: "Linked" })
    const cardLink = within(linked).getByRole("link", { name: /Card · AG-10100/ })
    expect(cardLink.getAttribute("href")).toBe("/board?highlight_task=AG-10100")
    expect(cardLink.textContent).toContain("Doing")
    expect(cardLink.textContent).toContain("Complete the Cover section")

    const pr = within(linked).getByRole("link", { name: /Pull request · #1334/ })
    expect(pr.getAttribute("href")).toBe("https://github.com/acme/site/pull/1334")
    expect(pr.textContent).toContain("CI failed")
    expect(pr.textContent).toContain("approved")
    expect(pr.textContent).toContain("12 comments")

    expect(within(linked).getByRole("link", { name: /Add search/ }).getAttribute("href")).toBe("/plans/pl-1")
  })

  it("still links the card before its details have loaded, and says where new work will appear", () => {
    show({ items: [], card: null })
    expect(screen.queryByRole("group", { name: "In progress" })).toBeNull()
    expect(screen.getByRole("link", { name: /Card · AG-10100/ }).getAttribute("href")).toBe("/board?highlight_task=AG-10100")
    expect(screen.getByText("Plans, reviews and PRs this agent creates appear here.")).toBeTruthy()
  })

  it("closes", () => {
    const onClose = vi.fn()
    show({ onClose })
    fireEvent.click(screen.getByRole("button", { name: "Close related work" }))
    expect(onClose).toHaveBeenCalledOnce()
  })

  it("covers the screen on a phone", () => {
    show({ mobile: true })
    expect(screen.getByRole("dialog", { name: "Related work" })).toBeTruthy()
  })
})
