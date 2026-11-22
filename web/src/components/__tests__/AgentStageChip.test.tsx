import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { AgentChatHeader } from "../AgentChatHeader"

vi.mock("../../hooks/useServer", () => ({ useServer: () => ({ baseUrl: "" }) }))

let calls: { url: string; init?: RequestInit }[] = []

beforeEach(() => {
  calls = []
  vi.stubGlobal("fetch", vi.fn(async (url: string, init?: RequestInit) => {
    calls.push({ url, init })
    if (url === "/api/agents/LEG-409/stage-history") {
      return new Response(JSON.stringify({ events: [
        { stage: "implementing", from_stage: "exploring", reason: "edited internal/server/board.go", source: "evidence", at: "2026-11-22T10:21:00Z" },
        { stage: "exploring", reason: "", source: "start", at: "2026-11-22T09:58:00Z" },
      ] }))
    }
    if (url === "/api/agents/LEG-409/stage" && init?.method === "PUT") {
      return new Response(JSON.stringify({ stage: "finished", stage_source: "operator" }))
    }
    return new Response("{}")
  }))
})

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

const stage = { agentId: "LEG-409", stage: "implementing", reason: "6 files changed", source: "evidence" }

describe("the agent's stage in its chat header", () => {
  it("names the stage and why beside the state", () => {
    render(<AgentChatHeader state="running" title="T" taskId="LEG-409" menu={[]} stage={stage} />)
    const chip = within(screen.getByRole("banner")).getByRole("button", { name: /Stage: Implementing/ })
    expect(within(chip).getByText("Implementing")).toBeTruthy()
    expect(within(chip).getByText("· 6 files changed")).toBeTruthy()
  })

  it("says when the operator set it", () => {
    render(<AgentChatHeader state="running" title="T" taskId="LEG-409" menu={[]} stage={{ ...stage, reason: "", source: "operator" }} />)
    expect(screen.getByRole("button", { name: /Stage: Implementing/ }).textContent).toContain("set by you")
  })

  it("opens the stage history, newest first", async () => {
    render(<AgentChatHeader state="running" title="T" taskId="LEG-409" menu={[]} stage={stage} />)
    fireEvent.click(screen.getByRole("button", { name: /Stage: Implementing/ }))
    const panel = screen.getByRole("dialog", { name: "Stage" })
    await waitFor(() => expect(within(panel).getAllByRole("listitem")).toHaveLength(2))
    const items = within(panel).getAllByRole("listitem").map((item) => item.textContent)
    expect(items[0]).toContain("Implementing")
    expect(items[0]).toContain("edited internal/server/board.go")
    expect(items[1]).toContain("Exploring")
  })

  it("sets the stage from the ⋯ menu", async () => {
    render(<AgentChatHeader state="running" title="T" taskId="LEG-409" menu={[{ label: "Kill agent", onSelect: () => {}, danger: true }]} stage={stage} />)
    fireEvent.click(screen.getByRole("button", { name: "More actions" }))
    fireEvent.click(screen.getByRole("menuitem", { name: "Set stage" }))
    const panel = screen.getByRole("dialog", { name: "Stage" })
    expect(within(panel).getByRole("radio", { name: "Implementing" }).getAttribute("aria-checked")).toBe("true")
    fireEvent.click(within(panel).getByRole("radio", { name: "Finished" }))
    await waitFor(() => expect(calls.some((call) => call.init?.method === "PUT")).toBe(true))
    const put = calls.find((call) => call.init?.method === "PUT")!
    expect(put.url).toBe("/api/agents/LEG-409/stage")
    expect(JSON.parse(String(put.init?.body))).toEqual({ stage: "finished" })
  })

  it("has no stage for an agent without one, such as the Reeve", () => {
    render(<AgentChatHeader state="running" title="T" taskId="R" menu={[{ label: "Kill agent", onSelect: () => {}, danger: true }]} />)
    expect(screen.queryByRole("button", { name: /Stage:/ })).toBeNull()
    fireEvent.click(screen.getByRole("button", { name: "More actions" }))
    expect(screen.queryByRole("menuitem", { name: "Set stage" })).toBeNull()
  })
})
