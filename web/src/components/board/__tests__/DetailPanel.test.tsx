import { describe, expect, it, afterEach, vi } from "vitest"
import { render, screen, cleanup, fireEvent, within } from "@testing-library/react"
import { DetailPanel } from "../DetailPanel"
import type { CardDetail } from "../../../lib/board-types"
vi.mock("../TaskAgentAction",()=>({TaskAgentAction:({ taskId, worktreePath, hasChimeraSession }: { taskId: string; worktreePath?: string; hasChimeraSession?: boolean })=> <button data-task-primary-action data-task-id={taskId} data-worktree-path={worktreePath} data-has-chimera-session={hasChimeraSession ? "true" : "false"} className="min-h-9 w-full" type="button">Open agent</button>}))
vi.mock("../TaskActivityTabs",()=>({TaskActivityTabs:()=>null}))

afterEach(() => {
  cleanup()
})

function makeCard(overrides: Partial<CardDetail> = {}): CardDetail {
  return {
    id: "T-1",
    title: "Test title",
    description_md: "Desc text",
    local_description_md: "Desc text",
    remote_description_md: "",
    status: "Doing",
    priority: "High",
    provider: "",
    remote_id: "",
    remote_meta: {},
    workspace_id: null,
    pr_meta: null,
    code_context: null,
    has_chimera_session: false,
    swarm_active_step: 0,
    swarm_step_names: [],
    created_at: "2025-01-01",
    updated_at: "2025-01-01",
    ...overrides,
  }
}

function renderPanel(card = makeCard(), overrides: Partial<React.ComponentProps<typeof DetailPanel>> = {}) {
  const props: React.ComponentProps<typeof DetailPanel> = {
    card, onClose: vi.fn(), onEditTitle: vi.fn(), onEditDescription: vi.fn(), onMove: vi.fn(), onDelete: vi.fn(),
    onLinkPR: vi.fn(), onRefreshPR: vi.fn(), onRediscoverPR: vi.fn(), onCopyDescription: vi.fn(), onCopyFull: vi.fn(),
    onOpenURL: vi.fn(), onCancelSwarm: vi.fn(), ...overrides,
  }
  return { ...render(<DetailPanel {...props} />), props }
}

describe("DetailPanel", () => {
  it("places the agent action before mobile content while preserving its desktop Work placement", () => {
    const worktree = { primary_dir: "/repo", path: "/trees/t-1", branch: "t-1", base_branch: "main" }
    const { container } = renderPanel(makeCard({ worktree, has_chimera_session: true }), { worktreesEnabled: true })
    const dialog = screen.getByRole("dialog", { name: "Test title" })
    const layout = container.querySelector("[data-task-detail-layout]")
    const mobileAction = container.querySelector("[data-task-agent-action='mobile']")
    const desktopAction = container.querySelector("[data-task-agent-action='desktop']")

    expect(dialog.className).toContain("max-w-5xl")
    expect(layout?.className).toContain("lg:grid-cols-[minmax(0,1fr)_18rem]")
    expect(mobileAction?.className).toContain("lg:hidden")
    expect(mobileAction?.nextElementSibling).toBe(layout)
    expect(desktopAction?.className).toContain("hidden")
    expect(desktopAction?.className).toContain("lg:block")
    expect(screen.getByRole("main", { name: "Task content" })).toBeTruthy()
    expect(screen.getByRole("complementary", { name: "Task properties and actions" })).toBeTruthy()
    expect(within(screen.getByRole("heading", { name: "Work" }).parentElement as HTMLElement).getByRole("button", { name: "Open agent" })).toBeTruthy()
    expect(screen.getByRole("heading", { name: "Edit" })).toBeTruthy()
    expect(screen.getByRole("heading", { name: "More actions" })).toBeTruthy()

    for (const placement of [mobileAction, desktopAction]) {
      const action = placement?.querySelector("[data-task-primary-action]")
      expect(action?.getAttribute("data-task-id")).toBe("T-1")
      expect(action?.getAttribute("data-worktree-path")).toBe("/trees/t-1")
      expect(action?.getAttribute("data-has-chimera-session")).toBe("true")
    }
    const actions = container.querySelector("[data-task-actions]")
    for (const action of Array.from(actions?.querySelectorAll("button") ?? [])) {
      expect(action.className).toContain("w-full")
    }
    expect(screen.getByRole("button", { name: "Delete task" }).className).toContain("text-red")
  })

  it("offers creation only when enabled and displays an existing worktree", () => {
    const onCreateWorktree = vi.fn()
    const rendered = renderPanel(makeCard(), { worktreesEnabled: true, onCreateWorktree })
    fireEvent.click(screen.getByRole("button", { name: "Create worktree" }))
    expect(onCreateWorktree).toHaveBeenCalledOnce()

    const worktree = { primary_dir: "/repo", path: "/trees/t-1", branch: "t-1", base_branch: "main" }
    rendered.rerender(<DetailPanel {...rendered.props} card={makeCard({ worktree })} worktreesEnabled />)
    expect(screen.queryByRole("button", { name: "Create worktree" })).toBeNull()
    expect(screen.getByText("/trees/t-1")).toBeTruthy()
  })

  it("shows context and invokes targeted PR actions", () => {
    const onRefreshPR = vi.fn()
    const onRediscoverPR = vi.fn()
    renderPanel(makeCard({
      code_context: { repo: "acme/widget", branch: "feature/T-1", head_sha: "", working_dir: "", base_branch: "main", repo_source: "worktree", branch_source: "worktree" },
      pr_meta: { repo: "acme/widget", branch: "feature/T-1", pr_number: 42, pr_url: "https://github.com/acme/widget/pull/42", state: "OPEN", is_draft: false, review_decision: "", check_status: "", comment_count: 0, pinned: false, match_method: "branch", sync_outcome: "linked", sync_error: "" },
    }), { onRefreshPR, onRediscoverPR })
    expect(screen.getByText("acme/widget")).toBeTruthy()
    expect(screen.getByText("feature/T-1")).toBeTruthy()
    fireEvent.click(screen.getByRole("button", { name: "Refresh pull request" }))
    fireEvent.click(screen.getByRole("button", { name: "Rediscover pull request" }))
    expect(onRefreshPR).toHaveBeenCalledOnce()
    expect(onRediscoverPR).toHaveBeenCalledOnce()
  })

  it("stays below action modals in the board stacking order", () => {
    const { container } = render(
      <DetailPanel card={makeCard()} onClose={() => {}} onEditTitle={() => {}} onEditDescription={() => {}} onMove={() => {}} onDelete={() => {}} onLinkPR={() => {}} onCopyDescription={() => {}} onCopyFull={() => {}} onOpenURL={() => {}} onCancelSwarm={() => {}} />
    )
    expect(container.firstElementChild?.className).toContain("z-40")
  })

  it("renders title", () => {
    render(<DetailPanel card={makeCard()} onClose={() => {}} onEditTitle={() => {}} onEditDescription={() => {}} onMove={() => {}} onDelete={() => {}} onLinkPR={() => {}} onCopyDescription={() => {}} onCopyFull={() => {}} onOpenURL={() => {}} onCancelSwarm={() => {}} />)
    expect(screen.getByText("Test title")).toBeTruthy()
  })

  it("shows edit buttons for local task", () => {
    render(<DetailPanel card={makeCard({ provider: "" })} onClose={() => {}} onEditTitle={() => {}} onEditDescription={() => {}} onMove={() => {}} onDelete={() => {}} onLinkPR={() => {}} onCopyDescription={() => {}} onCopyFull={() => {}} onOpenURL={() => {}} onCancelSwarm={() => {}} />)
    expect(screen.getByText(/Edit title/)).toBeTruthy()
    expect(screen.getByText(/Edit desc/)).toBeTruthy()
  })

  it("shows remote and Legato details as independent expanded accordions", () => {
    const { container } = renderPanel(makeCard({
      provider: "jira",
      description_md: "## Remote acceptance\n\n- A long remote description\n\n## Legato approach\n\n1. A long local description",
      remote_description_md: "## Remote acceptance\n\n- A long remote description",
      local_description_md: "## Legato approach\n\n1. A long local description",
    }))

    const remote = container.querySelector("[data-description-section='remote']") as HTMLDetailsElement
    const local = container.querySelector("[data-description-section='local']") as HTMLDetailsElement
    expect(remote.open).toBe(true)
    expect(local.open).toBe(true)
    expect(within(remote).getByRole("heading", { name: "Remote acceptance" })).toBeTruthy()
    expect(within(remote).getByRole("list").tagName).toBe("UL")
    expect(within(local).getByRole("heading", { name: "Legato approach" })).toBeTruthy()
    expect(within(local).getByRole("list").tagName).toBe("OL")
    expect(screen.queryByText(/## Legato approach/)).toBeNull()

    fireEvent.click(screen.getByText("Remote ticket details"))
    expect(remote.open).toBe(false)
    expect(local.open).toBe(true)
    fireEvent.click(screen.getByText("Legato details"))
    expect(local.open).toBe(false)
  })

  it("shows empty Legato details separately from provider content", () => {
    renderPanel(makeCard({ provider: "jira", description_md: "Provider only", remote_description_md: "Provider only", local_description_md: "" }))
    expect(screen.getByText("Provider only")).toBeTruthy()
    expect(screen.getByText("No Legato details")).toBeTruthy()
  })

  it("offers Legato detail editing but not title editing for a remote task", () => {
    const onEditDescription = vi.fn()
    render(<DetailPanel card={makeCard({ provider: "jira" })} onClose={() => {}} onEditTitle={() => {}} onEditDescription={onEditDescription} onMove={() => {}} onDelete={() => {}} onLinkPR={() => {}} onCopyDescription={() => {}} onCopyFull={() => {}} onOpenURL={() => {}} onCancelSwarm={() => {}} />)
    expect(screen.queryByText(/Edit title/)).toBeNull()
    const buttons = screen.getAllByRole("button", { name: "Edit Legato details" })
    expect(buttons.length).toBeGreaterThan(0)
    fireEvent.click(buttons[0])
    expect(onEditDescription).toHaveBeenCalledOnce()
  })

  it("shows cancel swarm button when swarm_working_dir is present", () => {
    render(<DetailPanel card={makeCard({ swarm_working_dir: "/tmp/work" })} onClose={() => {}} onEditTitle={() => {}} onEditDescription={() => {}} onMove={() => {}} onDelete={() => {}} onLinkPR={() => {}} onCopyDescription={() => {}} onCopyFull={() => {}} onOpenURL={() => {}} onCancelSwarm={() => {}} />)
    expect(screen.getByText(/Cancel swarm/)).toBeTruthy()
  })

  it("shows cancel swarm button when swarm_stats is present", () => {
    render(<DetailPanel card={makeCard({ swarm_stats: { total: 2, done: 1, in_review: 0, building: 0, queued: 1, rejected: 0 } })} onClose={() => {}} onEditTitle={() => {}} onEditDescription={() => {}} onMove={() => {}} onDelete={() => {}} onLinkPR={() => {}} onCopyDescription={() => {}} onCopyFull={() => {}} onOpenURL={() => {}} onCancelSwarm={() => {}} />)
    expect(screen.getByText(/Cancel swarm/)).toBeTruthy()
  })

  it("hides cancel swarm button when no swarm state", () => {
    render(<DetailPanel card={makeCard()} onClose={() => {}} onEditTitle={() => {}} onEditDescription={() => {}} onMove={() => {}} onDelete={() => {}} onLinkPR={() => {}} onCopyDescription={() => {}} onCopyFull={() => {}} onOpenURL={() => {}} onCancelSwarm={() => {}} />)
    expect(screen.queryByText(/Cancel swarm/)).toBeNull()
  })
})
