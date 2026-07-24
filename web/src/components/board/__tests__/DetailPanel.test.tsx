import { describe, expect, it, afterEach } from "vitest"
import { render, screen, cleanup } from "@testing-library/react"
import { DetailPanel } from "../DetailPanel"
import type { CardDetail } from "../../../lib/board-types"

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
    swarm_active_step: 0,
    swarm_step_names: [],
    created_at: "2025-01-01",
    updated_at: "2025-01-01",
    ...overrides,
  }
}

describe("DetailPanel", () => {
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
