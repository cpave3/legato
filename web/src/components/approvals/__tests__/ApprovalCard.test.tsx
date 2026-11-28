import { afterEach, describe, expect, it, vi } from "vitest"
import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react"
import { MemoryRouter } from "react-router-dom"
import type { Approval } from "../../../lib/approvals"
import { ApprovalCard } from "../ApprovalTray"

const mockDecide = vi.fn()
vi.mock("../../../hooks/useServer", () => ({ useServer: () => ({ baseUrl: "http://legato" }) }))
vi.mock("../../../hooks/usePasskeyDevice", () => ({ usePasskeyDevice: () => undefined }))
vi.mock("../../../lib/approvals", async (importOriginal) => ({
  ...(await importOriginal<typeof import("../../../lib/approvals")>()),
  decideApproval: (...args: unknown[]) => mockDecide(...args),
}))

afterEach(() => {
  cleanup()
  mockDecide.mockReset()
  delete (window as { legatoShell?: unknown }).legatoShell
  delete (window as { ReactNativeWebView?: unknown }).ReactNativeWebView
})

function approval(extra: Partial<Approval> = {}, details: Approval["details"] = {}): Approval {
  return {
    id: "ap-1", source: "agent_permission", source_key: "k", task_id: "task-9", title: "Run a command",
    details: { agent_kind: "Codex", task_title: "Fix sync", ...details },
    options: [{ id: "once", label: "Once", kind: "allow", description: "Just this time" }, { id: "always", label: "Always", kind: "allow" }, { id: "no", label: "No", kind: "deny" }],
    blocking: true, status: "pending", revision: 1, created_at: "",
    ...extra,
  }
}

function renderCard(item: Approval, { focused = false, onDone = vi.fn().mockResolvedValue(undefined) } = {}) {
  render(<MemoryRouter><ApprovalCard approval={item} focused={focused} onDone={onDone} /></MemoryRouter>)
  return { card: screen.getByRole("article"), onDone }
}

describe("ApprovalCard", () => {
  it("shows a permission's kind, asker, command, reason and folder, with an allow for each option", async () => {
    const { card, onDone } = renderCard(approval({}, { command: "rm -rf build", reason: "Clean the build", cwd: "/work/legato" }), { focused: true })
    expect(card.getAttribute("data-focused")).toBe("true")
    expect(card.className).toContain("border-lw-accent")
    expect(within(card).getByText("Permission")).toBeTruthy()
    expect(within(card).getByText("· waiting")).toBeTruthy()
    expect(within(card).getByRole("link", { name: "Open agent" }).getAttribute("href")).toBe("/agents?task=task-9")
    expect(within(card).getByText("Fix sync · Codex")).toBeTruthy()
    expect(within(card).getByText("rm -rf build").tagName).toBe("PRE")
    expect(within(card).getByText("Clean the build")).toBeTruthy()
    expect(within(card).getByText("/work/legato")).toBeTruthy()
    const once = within(card).getByRole("button", { name: "Once" })
    expect(once.getAttribute("title")).toBe("Just this time")
    expect(once.className).toContain("bg-lw-accent")
    const always = within(card).getByRole("button", { name: "Always" })
    expect(always.className).toContain("border-lw-line-strong")
    expect(within(card).queryByRole("button", { name: "No" })).toBeNull()
    mockDecide.mockResolvedValueOnce({})
    fireEvent.click(always)
    await waitFor(() => expect(mockDecide).toHaveBeenCalledWith("http://legato", "ap-1", { decision: "allow", option: "always" }))
    await waitFor(() => expect(onDone).toHaveBeenCalled())
  })

  it("shows the summary when there is no command, and nothing to run without either", () => {
    renderCard(approval({ summary: "Edit two files", blocking: false, task_id: undefined }))
    expect(screen.getByText("Edit two files").tagName).toBe("PRE")
    expect(screen.queryByText("· waiting")).toBeNull()
    expect(screen.queryByRole("link", { name: "Open agent" })).toBeNull()
    cleanup()
    renderCard(approval())
    expect(document.querySelector("pre")).toBeNull()
  })

  it("folds a plan away instead of showing the command", () => {
    const { card } = renderCard(approval({ summary: "Approve the plan" }, { plan: "1. Read\n2. Write" }))
    expect(within(card).getByText("Plan")).toBeTruthy()
    expect(within(card).getByText("Show the plan")).toBeTruthy()
    expect(within(card).getByText(/1\. Read/)).toBeTruthy()
    expect(within(card).queryByText("Approve the plan")).toBeNull()
  })

  it("shows a credential's scope once, without the command or a second reason", () => {
    const { card } = renderCard(approval({ source: "credential", summary: "GitHub token" }, {
      credential: "github", operations: ["push"], resources: [], reason: "Push the fix", broader: true, signed: true,
    }))
    expect(within(card).getByText("Credential")).toBeTruthy()
    expect(within(card).getByText("push")).toBeTruthy()
    expect(within(card).getByText("Broader scope")).toBeTruthy()
    expect(within(card).getByText("needs a signed decision (not enforced yet)")).toBeTruthy()
    expect(within(card).getByText("All of github")).toBeTruthy()
    expect(within(card).getByText("The agent's reason")).toBeTruthy()
    expect(within(card).getAllByText(/Push the fix/)).toHaveLength(1)
    expect(within(card).queryByText("GitHub token")).toBeNull()
  })

  it("lists a credential's resources", () => {
    renderCard(approval({ source: "credential" }, { credential: "github", resources: ["repo:a/b", "repo:a/*"] }))
    expect(screen.getByText("repo:a/b")).toBeTruthy()
    expect(screen.getByText("repo:a/*")).toBeTruthy()
    expect(screen.queryByText(/All of/)).toBeNull()
  })

  it("shows a Reeve change set without its summary or reason", () => {
    const { card } = renderCard(approval({ source: "reeve_change_set", summary: "Two calls", task_id: undefined }, { reason: "Tidy", calls: [] }))
    expect(within(card).getByText("Change set")).toBeTruthy()
    expect(within(card).getByTestId("change-set")).toBeTruthy()
    expect(within(card).queryByText("Tidy")).toBeNull()
    expect(document.querySelector("pre")).toBeNull()
  })

  it("asks for a Reeve secret's value, with no allow", () => {
    const { card } = renderCard(approval({ source: "reeve_secret", summary: "Paste the key" }, { name: "API_KEY", reason: "To call it" } as Approval["details"]))
    expect(within(card).getByTestId("secret-request")).toBeTruthy()
    expect(within(card).queryByRole("button", { name: "Once" })).toBeNull()
    expect(within(card).getByText("To call it")).toBeTruthy()
    expect(within(card).getByRole("button", { name: "Deny…" })).toBeTruthy()
  })

  it("shows why the phone could not sign", async () => {
    ;(window as { legatoShell?: unknown }).legatoShell = { approval: 1, key: "k1" }
    ;(window as { ReactNativeWebView?: unknown }).ReactNativeWebView = { postMessage: () => {} }
    const { card, onDone } = renderCard(approval({ signed: true }))
    const sign = within(card).getByRole("button", { name: /Approve and sign/ })
    fireEvent.click(sign)
    expect(sign).toHaveProperty("disabled", true)
    window.dispatchEvent(new CustomEvent("legato:shell-approval", { detail: { id: "other", status: "error" } }))
    expect(sign).toHaveProperty("disabled", true)
    window.dispatchEvent(new CustomEvent("legato:shell-approval", { detail: { id: "ap-1", status: "error" } }))
    expect((await within(card).findByRole("alert")).textContent).toBe("The phone could not sign this decision.")
    expect(sign).toHaveProperty("disabled", false)
    window.dispatchEvent(new CustomEvent("legato:shell-approval", { detail: { id: "ap-1", status: "error", error: "Locked" } }))
    expect((await within(card).findByRole("alert")).textContent).toBe("Locked")
    expect(onDone).not.toHaveBeenCalled()
  })
})
