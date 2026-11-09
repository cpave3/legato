import { describe, it, expect, vi, afterEach } from "vitest"
import { render, screen, cleanup } from "@testing-library/react"

vi.mock("../../lib/reeve", () => ({
  fetchChangeSetLines: () => Promise.resolve([
    { id: "cs-1", reason: "Rename the column", state: "pending", calls: 2, ran: 0 },
    { id: "cs-2", reason: "Tidy", state: "done", calls: 3, ran: 3 },
  ]),
}))

import { ChangeSetCard } from "../ChangeSetCard"
import { ChangeSetLines } from "../ChangeSetLines"
import type { Approval } from "../../lib/approvals"

const approval: Approval = {
  id: "ap-1", source: "reeve_change_set", source_key: "cs-1", title: "Turn reviews off", options: [], blocking: false,
  status: "pending", revision: 1, created_at: "", signed: true,
  details: {
    id: "cs-1", instance: "legato-1", level: "signed", reason: "Turn reviews off",
    calls: [
      { kind: "api", method: "PUT", path: "/api/config/settings/agents.reviews", class: "change", description: "Change the setting agents.reviews (Reviews)", level: "approve", setting: "agents.reviews", before: true, after: false, applies_at_restart: true },
      { kind: "restart", class: "risky", description: "Restart Legato", level: "signed" },
    ],
  },
}

describe("ChangeSetCard", () => {
  afterEach(() => cleanup())

  it("shows the source, ID, count and level, each change with its kind and diff, and that Legato runs exactly this", () => {
    render(<ChangeSetCard approval={approval} />)
    const card = screen.getByTestId("change-set")
    expect(card.textContent).toContain("The Reeve on legato-1 · cs-1 · 2 changes · needs a signature")
    const first = screen.getByTestId("change-1")
    expect(first.textContent).toContain("change")
    expect(first.textContent).toContain("PUT /api/config/settings/agents.reviews")
    expect(first.textContent).toContain("true→false")
    expect(first.textContent).toContain("Applies at restart")
    expect(screen.getByTestId("change-2").textContent).toContain("risky")
    expect(card.textContent).toContain("Legato runs exactly this, in order, stopping at the first failure.")
  })
})

describe("ChangeSetLines", () => {
  afterEach(() => cleanup())

  it("shows each change set as a compact line with its state", async () => {
    render(<ChangeSetLines baseUrl="" taskId="rv1" />)
    const list = await screen.findByRole("list", { name: "Change sets" })
    expect(list.textContent).toContain("Rename the column")
    expect(list.textContent).toContain("waiting for your approval")
    expect(list.textContent).toContain("done, 3 of 3 ran")
  })
})

describe("ChangeSetCard: what each kind of call runs", () => {
  afterEach(() => cleanup())

  it("shows a legato command's every argument, a shell command, a file and a service in full", () => {
    render(<ChangeSetCard approval={{
      ...approval,
      details: {
        id: "cs-9", instance: "legato-1", level: "verified", reason: "Fix the proxy",
        calls: [
          { kind: "legato", args: ["agent", "message", "T-1", "please rebase onto main"], class: "change", description: "Message an agent", level: "approve" },
          { kind: "host_shell", command: "journalctl -u caddy -n 50", class: "risky", description: "Run on the host: journalctl -u caddy -n 50", level: "approve" },
          { kind: "edit_file", file: "/etc/caddy/Caddyfile", diff: "- old\n+ new\n", class: "risky", description: "Write the file /etc/caddy/Caddyfile", level: "approve" },
          { kind: "manage_service", unit: "caddy.service", action: "restart", system: true, as_root: true, class: "risky", description: "Restart the system service caddy.service", level: "verified" },
        ],
      },
    }} />)
    const card = screen.getByTestId("change-set")
    expect(card.textContent).not.toContain("undefined")
    expect(screen.getByTestId("change-1").textContent).toContain(`legato agent message T-1 "please rebase onto main"`)
    expect(screen.getByTestId("change-2").textContent).toContain("$ journalctl -u caddy -n 50")
    expect(screen.getByTestId("change-3").textContent).toContain("write /etc/caddy/Caddyfile")
    const service = screen.getByTestId("change-4").textContent ?? ""
    expect(service).toContain("restart caddy.service")
    expect(service).toContain("system service")
    expect(service).toContain("host as root")
  })
})
