import { cleanup, fireEvent, render, screen, within } from "@testing-library/react"
import { MemoryRouter } from "react-router-dom"
import { afterEach, describe, expect, it, vi } from "vitest"
import { RelatedWorkPanel } from "../RelatedWorkPanel"
import type { RelatedWorkItem } from "../../lib/task-activity"
import type { CardDetail } from "../../lib/board-types"
import type { RelatedAgent } from "../../lib/related-agents"

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

  it("lists a review loop's reviewer with its pass and the loop's state, and opens it while it runs", () => {
    const onOpenAgent = vi.fn()
    const reviewer: RelatedAgent = {
      agent_id: "AG-10100_reviewer", task_id: "AG-10100", slot: "reviewer", title: "Complete the Cover section", role: "reviewer", pass: 2,
      status: "running", activity: "working", loop: { kind: "handoff", task_id: "AG-10100", phase: "reviewing", link: "/board?highlight_task=AG-10100" },
    }
    show({ agents: [reviewer], onOpenAgent })
    const group = screen.getByRole("group", { name: "Agents" })
    const row = within(group).getByRole("button", { name: /Reviewer · pass 2/ })
    expect(row.textContent).toContain("reviewing")
    fireEvent.click(row)
    expect(onOpenAgent).toHaveBeenCalledWith("AG-10100_reviewer")
  })

  it("keeps an ended reviewer as a link to its loop, never selecting it", () => {
    const onOpenAgent = vi.fn()
    const reviewer: RelatedAgent = {
      agent_id: "o7_reviewer", task_id: "o7", slot: "reviewer", title: "Review loop", role: "reviewer", pass: 1,
      status: "ended", loop: { kind: "workflow", task_id: "o7", phase: "waiting_for_author", run_id: "wr-1", link: "/workflows/runs/wr-1" },
    }
    show({ agents: [reviewer], onOpenAgent })
    const group = screen.getByRole("group", { name: "Agents" })
    expect(within(group).queryByRole("button")).toBeNull()
    const row = within(group).getByRole("link", { name: /Reviewer · pass 1/ })
    expect(row.textContent).toContain("waiting for author")
    expect(row.textContent).toContain("ended")
    expect(row.getAttribute("href")).toBe("/workflows/runs/wr-1")
    fireEvent.click(row)
    expect(onOpenAgent).not.toHaveBeenCalled()
  })

  it("lists a reviewer's author, and a linked task with no agent as a link to the board", () => {
    const onOpenAgent = vi.fn()
    const author: RelatedAgent = {
      agent_id: "LEG-423", task_id: "LEG-423", slot: "main", title: "Related sidebar", role: "author", status: "running", activity: "working",
      loop: { kind: "workflow", task_id: "o7ooq1l4", phase: "reviewing", run_id: "wr-9b9973c2af81", link: "/workflows/runs/wr-9b9973c2af81" },
    }
    const linked: RelatedAgent = {
      task_id: "LEG-500", slot: "main", title: "Another card", role: "linked_task", status: "none",
      loop: { kind: "workflow", task_id: "o8", phase: "reviewing", run_id: "wr-2", link: "/workflows/runs/wr-2" },
    }
    show({ agents: [author, linked], onOpenAgent })
    const group = screen.getByRole("group", { name: "Agents" })
    const row = within(group).getByRole("button", { name: /Author/ })
    expect(row.textContent).toContain("Related sidebar")
    expect(row.textContent).toContain("working")
    fireEvent.click(row)
    expect(onOpenAgent).toHaveBeenCalledWith("LEG-423")
    const task = within(group).getByRole("link", { name: /Linked task · LEG-500/ })
    expect(task.getAttribute("href")).toBe("/board?highlight_task=LEG-500")
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
