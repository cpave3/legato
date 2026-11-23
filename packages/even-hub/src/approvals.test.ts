import { describe, expect, it } from "vitest"
import { getTextWidth } from "@evenrealities/pretext"
import { measureTextWrap } from "@evenrealities/pretext"
import { APPROVAL_BODY_LINES, actionable, approvalPage, approvalsPage, decisionPage } from "./approvals"
import { BADGE_WIDTH, BORDER, FRAME_WIDTH, ITEM_TEXT_WIDTH, inner } from "./layout"
import type { Approval } from "./legato"

const permission: Approval = {
  id: "ap-1",
  source: "agent_permission",
  task_id: "LEG-7",
  title: "Bash: git push origin main",
  summary: "git push origin main",
  options: [
    { id: "once", label: "Allow once", kind: "allow" },
    { id: "always", label: "Always allow", kind: "allow" },
    { id: "deny", label: "Deny", kind: "deny" },
  ],
  details: { reason: "Push the branch for review" },
}
const signed: Approval = { ...permission, id: "ap-2", source: "credential", title: "Use the GitHub token", signed: true }
const question: Approval = { ...permission, id: "ap-3", source: "agent_question", title: "Which repo?", options: [] }

const hint = (text: string) => text.trim()

describe("actionable", () => {
  it("is an approval with options that needs no signature", () => {
    expect(actionable(permission)).toBe(true)
    expect(actionable(signed)).toBe(false)
    expect(actionable(question)).toBe(false)
  })
})

describe("approvalsPage", () => {
  it("lists what waits, one line each, marking what must be signed or answered elsewhere", () => {
    const page = approvalsPage([permission, signed, question], "Desk")

    expect(page.badge).toBe("● to approve")
    expect(page.title).toBe("3 waiting · Desk")
    expect(page.items).toEqual([
      { text: "● LEG-7 Bash: git push origin main", detail: "ap-1" },
      { text: "◇ LEG-7 Use the GitHub token", detail: "ap-2" },
      { text: "○ LEG-7 Which repo?", detail: "ap-3" },
    ])
    expect(hint(page.hint)).toBe("Tap: open · Double tap: back")
  })

  it("names the Reeve for its change sets, not its session's hidden task", () => {
    const set = { ...permission, source: "reeve_change_set", task_id: "rv-8c2e1f", title: "Start an agent on LEG-212" }

    expect(approvalsPage([set], "Desk").items[0].text).toBe("● Reeve Start an agent on LEG-212")
  })

  it("cuts a line to the list's width", () => {
    const long = { ...permission, title: "Bash: go test ./internal/service/... -run TestEverythingThatMatters -count=1 -race" }
    const [item] = approvalsPage([long], "Desk").items

    expect(item.text.endsWith("…")).toBe(true)
    expect(getTextWidth(item.text)).toBeLessThanOrEqual(ITEM_TEXT_WIDTH)
  })

  it("says when nothing waits", () => {
    expect(approvalsPage([], "Desk").items).toEqual([{ text: "Nothing waits for you." }])
  })
})

describe("approvalPage", () => {
  it("shows what an approval is, with its options to choose from", () => {
    const page = approvalPage(permission)

    expect(page).toMatchObject({
      badge: "● approve?",
      title: "LEG-7",
      body: "Bash: git push origin main",
      options: ["Allow once", "Always allow", "Deny"],
    })
    expect(hint(page.hint)).toBe("Tap: choose · Double tap: back")
  })

  it("cuts what it is to the lines above the options", () => {
    const long = { ...permission, title: `Bash: ${"go test ./internal/service/... -run TestEverything ".repeat(4)}` }
    const { body } = approvalPage(long)

    expect(body.endsWith("…")).toBe(true)
    expect(measureTextWrap(body, inner(FRAME_WIDTH)).lineCount).toBeLessThanOrEqual(APPROVAL_BODY_LINES)
  })

  it("offers no options for one that must be signed, and says where to sign it", () => {
    const page = approvalPage(signed)

    expect(page.badge).toBe("◇ signed")
    expect(page.options).toEqual([])
    expect(page.body.endsWith("It needs your signature: approve it on your phone.")).toBe(true)
    expect(hint(page.hint)).toBe("Double tap: back")
  })

  it("sends a question to the phone or Legato to answer", () => {
    const page = approvalPage(question)

    expect(page.badge).toBe("○ question")
    expect(page.body.endsWith("Answer it on your phone or in Legato.")).toBe(true)
  })
})

describe("decisionPage", () => {
  it("asks to confirm the option chosen, then says how it went", () => {
    const allow = permission.options[0]
    const deny = permission.options[2]

    expect(decisionPage(permission, { step: "confirm", option: allow })).toMatchObject({
      badge: "Allow?",
      body: "Allow once\n\nBash: git push origin main\n\ngit push origin main\n\nPush the branch for review",
    })
    expect(hint(decisionPage(permission, { step: "confirm", option: allow }).hint)).toBe("Tap: confirm · Double tap: back")
    expect(decisionPage(permission, { step: "confirm", option: deny }).badge).toBe("Deny?")
    expect(decisionPage(permission, { step: "deciding", option: allow }).badge).toBe("Deciding")
    expect(decisionPage(permission, { step: "done", option: allow }).badge).toBe("Allowed")
    expect(decisionPage(permission, { step: "done", option: deny }).badge).toBe("Denied")
    const failed = decisionPage(permission, { step: "problem", option: allow, problem: "It was already decided somewhere else." })
    expect(failed.badge).toBe("▲ Not decided")
    expect(failed.body).toBe("It was already decided somewhere else.")
    expect(hint(failed.hint)).toBe("Tap: try again · Double tap: back")
  })
})

describe("the badges", () => {
  it("all fit the badge box", () => {
    const allow = permission.options[0]
    const badges = [
      approvalsPage([], "Desk").badge,
      ...[permission, signed, question].map((a) => approvalPage(a).badge),
      ...(["confirm", "deciding", "done"] as const).map((step) => decisionPage(permission, { step, option: allow }).badge),
      decisionPage(permission, { step: "confirm", option: permission.options[2] }).badge,
      decisionPage(permission, { step: "problem", option: allow, problem: "x" }).badge,
    ]
    for (const badge of badges) expect(getTextWidth(badge), badge).toBeLessThanOrEqual(inner(BADGE_WIDTH, BORDER))
  })
})
