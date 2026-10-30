import { cleanup, fireEvent, render, screen, within } from "@testing-library/react"
import { MemoryRouter, Route, Routes } from "react-router-dom"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { Layout } from "../Layout"

const { state } = vi.hoisted(() => ({
  state: {
    summary: { agents: 0, attention: 0, review: 0, plans: 0, workflow_attention: 0, learning: 0 },
    connected: true,
    lost: false,
    servers: [] as { name: string; url: string }[],
    setActiveServer: (() => {}) as (url: string) => void,
  },
}))

vi.mock("../../hooks/useNavigationSummary", () => ({ useNavigationSummary: () => ({ data: state.summary }) }))
vi.mock("../../hooks/useWebSocket", () => ({ useWebSocket: () => ({ connected: state.connected, lost: state.lost }) }))
vi.mock("../../hooks/useServer", () => ({
  useServer: () => ({ servers: state.servers, activeServerName: "Local", baseUrl: "", setActiveServer: state.setActiveServer }),
}))
vi.mock("../PlanApprovalModal", () => ({ PlanApprovalModal: () => null }))
vi.mock("../OfflineDialog", () => ({ OfflineDialog: () => null }))
vi.mock("../../hooks/useAgentSelection", () => ({ AgentSelectionProvider: ({ children }: { children: React.ReactNode }) => children }))

function renderAt(entry: string) {
  return render(
    <MemoryRouter initialEntries={[entry]}>
      <Routes>
        <Route element={<Layout />}>
          <Route path="*" element={<h1>Page</h1>} />
        </Route>
      </Routes>
    </MemoryRouter>,
  )
}

const rail = () => screen.getByRole("navigation", { name: "Primary" })
const tabs = () => screen.getByRole("navigation", { name: "Primary navigation" })

beforeEach(() => {
  state.summary = { agents: 0, attention: 0, review: 0, plans: 0, workflow_attention: 0, learning: 0 }
  state.connected = true
  state.lost = false
  state.servers = []
  state.setActiveServer = vi.fn()
  localStorage.clear()
})

afterEach(cleanup)

describe("the rail", () => {
  it("lists the sections in the design's groups, with Settings at the foot", () => {
    renderAt("/board")

    const names = within(rail()).getAllByRole("link").map((link) => link.textContent)
    expect(names).toEqual(["Attention", "Agents", "Ensemble", "Board", "Review", "Plans", "Flows", "Learning", "Reports", "Audit", "Settings"])
    expect(within(rail()).getAllByRole("separator")).toHaveLength(3)
    expect(within(rail()).getByRole("link", { name: "Board" }).getAttribute("aria-current")).toBe("page")
  })

  it("opens the agents as tiles from Ensemble, which is current only there", () => {
    state.summary.agents = 2
    renderAt("/agents?view=ensemble")

    const ensemble = within(rail()).getByRole("link", { name: "Ensemble: 2 non-idle" })
    expect(ensemble.getAttribute("href")).toBe("/agents?view=ensemble")
    expect(ensemble.getAttribute("aria-current")).toBe("page")
    expect(within(rail()).getByRole("link", { name: "Agents" }).getAttribute("aria-current")).toBeNull()
  })

  it("counts what waits in each section", () => {
    state.summary = { agents: 0, attention: 24, review: 1, plans: 8, workflow_attention: 3, learning: 5 }
    renderAt("/board")

    expect(within(rail()).getByRole("link", { name: "Attention: 24 items" })).toBeTruthy()
    expect(within(rail()).getByRole("link", { name: "Review: 1 to review" })).toBeTruthy()
    expect(within(rail()).getByRole("link", { name: "Plans: 8 to decide" })).toBeTruthy()
    expect(within(rail()).getByRole("link", { name: "Flows: 3 need attention" })).toBeTruthy()
    expect(within(within(rail()).getByRole("link", { name: "Learning: 5 to decide" })).getByText("5")).toBeTruthy()
  })

  it("says whether the page is in touch with Legato", () => {
    renderAt("/board")
    expect(within(rail()).getByText("sync")).toBeTruthy()
  })

  it("says when it is not, once the connection is lost", () => {
    state.connected = false
    state.lost = true
    renderAt("/board")
    expect(within(rail()).getByText("offline")).toBeTruthy()
  })

  it("keeps saying sync while it reconnects", () => {
    state.connected = false
    renderAt("/board")
    expect(within(rail()).getByText("sync")).toBeTruthy()
  })
})

describe("the phone's tab bar", () => {
  it("keeps five tabs, with what More holds counted on it", () => {
    state.summary = { agents: 2, attention: 10, review: 1, plans: 8, workflow_attention: 0, learning: 3 }
    renderAt("/attention")

    const names = within(tabs()).getAllByRole("link").map((link) => link.getAttribute("aria-label"))
    expect(names).toEqual(["Attention: 10 items", "Board", "Agents: 2 non-idle", "Review: 1 to review"])
    expect(within(tabs()).getByRole("button", { name: "More: 11 waiting" })).toBeTruthy()
  })
})

describe("the More sheet", () => {
  it("groups the other sections as the design does", () => {
    state.summary.plans = 8
    state.summary.learning = 2
    renderAt("/board")
    fireEvent.click(within(tabs()).getByRole("button", { name: /More/ }))
    const sheet = screen.getByRole("dialog", { name: "More" })

    const queues = within(sheet).getByRole("group", { name: "Queues" })
    expect(within(queues).getAllByRole("link").map((link) => link.textContent)).toEqual(["Plans8", "Learning2"])
    const tools = within(sheet).getByRole("group", { name: "Tools" })
    expect(within(tools).getAllByRole("link").map((link) => link.textContent)).toEqual(["Workflows", "Reports", "Audit log"])
    expect(within(sheet).getByRole("link", { name: "Pair with QR code" }).getAttribute("href")).toBe("/settings?pair=qr")
    expect(within(sheet).getByRole("link", { name: "All settings" }).getAttribute("href")).toBe("/settings")
  })

  it("switches the settings it shows, as Settings does", () => {
    renderAt("/board")
    fireEvent.click(within(tabs()).getByRole("button", { name: /More/ }))
    const sheet = screen.getByRole("dialog", { name: "More" })

    const detection = within(sheet).getByRole("switch", { name: "Prompt detection" })
    expect(detection.getAttribute("aria-checked")).toBe("true")
    fireEvent.click(detection)
    expect(localStorage.getItem("legato:prompt-detection")).toBe("false")
    expect(detection.getAttribute("aria-checked")).toBe("false")

    fireEvent.click(within(sheet).getByRole("switch", { name: "Animated agent activity" }))
    expect(localStorage.getItem("legato:animated-agent-activity")).toBe("false")
  })

  it("names the server in use and switches to another", () => {
    state.servers = [{ name: "Laptop", url: "http://laptop:3080" }]
    renderAt("/board")
    fireEvent.click(within(tabs()).getByRole("button", { name: /More/ }))
    const sheet = screen.getByRole("dialog", { name: "More" })

    expect(within(sheet).getByText("Local")).toBeTruthy()
    fireEvent.click(within(sheet).getByRole("button", { name: "Switch server" }))
    fireEvent.click(within(sheet).getByRole("button", { name: "Laptop" }))
    expect(state.setActiveServer).toHaveBeenCalledWith("http://laptop:3080")
  })
})
