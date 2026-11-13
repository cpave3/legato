import { render, screen } from "@testing-library/react"
import { describe, expect, it } from "vitest"
import type { Approval } from "../lib/approvals"
import { ChangeSetCard } from "./ChangeSetCard"

describe("ChangeSetCard", () => {
  it("lists the values a config edit keeps in the database", () => {
    const approval = {
      id: "ap-1",
      source: "reeve_change_set",
      details: {
        id: "cs-1",
        level: "approve",
        calls: [{
          kind: "config_edit", class: "risky", description: "Edit config.yaml", level: "approve",
          diff: "- swarm:\n-   worker:\n-     agent: claude-code\n", check: "loads",
          keeps: ['kept in the database: swarm.worker.agent: "claude-code" (the database had "chimera")'],
        }],
      },
    } as unknown as Approval
    render(<ChangeSetCard approval={approval} />)
    expect(screen.getByTestId("config-keeps").textContent).toContain('swarm.worker.agent: "claude-code" (the database had "chimera")')
  })
})
