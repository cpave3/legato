import { describe, expect, it, afterEach, vi } from "vitest"
import { render, screen, cleanup, fireEvent } from "@testing-library/react"
import { DetailPanel } from "../DetailPanel"
import type { CardDetail } from "../../../lib/board-types"
vi.mock("../TaskAgentAction",()=>({TaskAgentAction:()=>null}))
vi.mock("../TaskActivityTabs",()=>({TaskActivityTabs:()=>null}))

afterEach(() => {
  cleanup()
})

function makeCard(overrides: Partial<CardDetail> = {}): CardDetail {
  return {
    id: "T-1",
    title: "Test title",
    description_md: "Desc text",
    status: "Doing",
    priority: "High",
    provider: "",
    remote_id: "",
    remote_meta: {},
    workspace_id: null,
    pr_meta: null,
    code_context: null,
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
    fireEvent.click(screen.getByText("Refresh PR"))
    fireEvent.click(screen.getByText("Rediscover PR"))
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

  it("hides edit buttons for remote task", () => {
    render(<DetailPanel card={makeCard({ provider: "jira" })} onClose={() => {}} onEditTitle={() => {}} onEditDescription={() => {}} onMove={() => {}} onDelete={() => {}} onLinkPR={() => {}} onCopyDescription={() => {}} onCopyFull={() => {}} onOpenURL={() => {}} onCancelSwarm={() => {}} />)
    expect(screen.queryByText(/Edit title/)).toBeNull()
    expect(screen.queryByText(/Edit desc/)).toBeNull()
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
