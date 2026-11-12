import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import "@testing-library/jest-dom/vitest"
import { act, cleanup, fireEvent, render, screen, within } from "@testing-library/react"
import { approvalLine, detailOf, howToDecide, type WidgetApproval } from "./approvals"

// The app's events and commands, as Rust emits and answers them.
const handlers = new Map<string, (e: { payload: unknown }) => void>()
const invoked: { cmd: string; args?: unknown }[] = []
let pending: WidgetApproval[] = []
let answers: Record<string, (args: unknown) => Promise<unknown>> = {}

vi.mock("@tauri-apps/api/event", () => ({
  listen: (name: string, handler: (e: { payload: unknown }) => void) => {
    handlers.set(name, handler)
    return Promise.resolve(() => handlers.delete(name))
  },
}))
vi.mock("@tauri-apps/api/core", () => ({
  invoke: (cmd: string, args?: unknown) => {
    invoked.push({ cmd, args })
    if (cmd === "voice_approvals") return Promise.resolve(pending)
    if (answers[cmd]) return answers[cmd](args)
    return Promise.resolve(null)
  },
}))

const { WidgetApprovals } = await import("./Approvals")

function emit(name: string, payload: unknown) {
  act(() => handlers.get(name)?.({ payload }))
}

const hostCommand: WidgetApproval = {
  server: "s1",
  server_name: "tessitura",
  approval: {
    id: "ap-1",
    source: "reeve_change_set",
    title: "Run a command on the host",
    summary: "1 change",
    signed: true,
    options: [{ id: "allow", label: "Approve", kind: "allow" }, { id: "deny", label: "Deny", kind: "deny" }],
    details: { id: "cs-1", instance: "tessitura", level: "signed", calls: [{ kind: "host_shell", command: "dnf upgrade -y", class: "risky", description: "Upgrade the packages", level: "signed", as_root: true }] },
  },
}

const credential: WidgetApproval = {
  server: "s1",
  server_name: "tessitura",
  approval: {
    id: "ap-2",
    source: "credential",
    title: "Use github",
    signed: false,
    options: [
      { id: "30m", label: "for 30 minutes", kind: "allow" },
      { id: "once", label: "once", kind: "allow" },
      { id: "deny", label: "Deny", kind: "deny" },
    ],
    details: { credential: "github", operations: ["push"], resources: ["cpave3/legato"], reason: "Push the fix", agent_kind: "Claude Code", task_title: "Fix the login" },
  },
}

const permission: WidgetApproval = {
  server: "s2",
  server_name: "erebus",
  approval: {
    id: "ap-3",
    source: "agent_permission",
    title: "Run a command",
    summary: "rm -rf build",
    signed: false,
    options: [{ id: "once", label: "Allow once", kind: "allow" }, { id: "deny", label: "Deny", kind: "deny" }],
    details: { command: "rm -rf build", reason: "Clean the build", cwd: "/src/app", agent_kind: "Codex", task_title: "Tidy" },
  },
}

const question: WidgetApproval = {
  server: "s2",
  server_name: "erebus",
  approval: {
    id: "ap-4",
    source: "agent_question",
    title: "Which database?",
    signed: false,
    options: [],
    details: { agent_kind: "Claude Code", questions: [{ id: "q", question: "Which database?", options: [{ label: "SQLite" }] }] },
  },
}

describe("what an approval says in the widget", () => {
  it("is one line: who asks, and what", () => {
    expect(approvalLine(hostCommand.approval)).toEqual({ who: "Reeve", what: "Run a command on the host" })
    expect(approvalLine(credential.approval)).toEqual({ who: "Claude Code", what: "Use github" })
    expect(approvalLine({ ...permission.approval, details: {} })).toEqual({ who: "An agent", what: "Run a command" })
  })

  it("is decided by a click, a signature, or in the main window", () => {
    expect(howToDecide(credential.approval)).toEqual({ kind: "choose", allows: credential.approval.options.slice(0, 2) })
    expect(howToDecide(hostCommand.approval)).toEqual({ kind: "sign", allows: [hostCommand.approval.options[0]] })
    // An answer or a secret is typed, which the widget cannot take.
    expect(howToDecide(question.approval).kind).toBe("open")
    expect(howToDecide({ ...credential.approval, source: "reeve_secret" }).kind).toBe("open")
    expect(howToDecide({ ...permission.approval, details: { plan: "1. Do it" } }).kind).toBe("open")
  })

  it("shows what the tray shows: the command, the scope and the reason", () => {
    expect(detailOf(permission.approval)).toEqual([
      { label: "command", value: "rm -rf build", code: true },
      { label: "in", value: "/src/app", code: true },
      { label: "reason", value: "Clean the build" },
    ])
    expect(detailOf(credential.approval)).toEqual([
      { label: "operation", value: "push", code: true },
      { label: "resource", value: "cpave3/legato", code: true },
      { label: "reason", value: "Push the fix" },
    ])
    expect(detailOf(hostCommand.approval)).toEqual([
      { label: "host as root", value: "$ dnf upgrade -y", code: true, note: "Upgrade the packages", warn: true },
    ])
  })
})

describe("the approvals in the widget", () => {
  beforeEach(() => {
    handlers.clear()
    invoked.length = 0
    pending = []
    answers = {}
  })
  afterEach(cleanup)

  const calls = (cmd: string) => invoked.filter((i) => i.cmd === cmd).map((i) => i.args)

  it("draws nothing while none waits, and each as it arrives", async () => {
    render(<WidgetApprovals />)
    await act(async () => {})
    expect(screen.queryByTestId("widget-approvals")).not.toBeInTheDocument()
    emit("voice-approvals", [hostCommand, credential])
    const list = screen.getByTestId("widget-approvals")
    expect(list).toHaveAttribute("data-no-drag")
    expect(within(list).getByText("2 need you")).toBeInTheDocument()
    expect(within(list).getByText("Reeve:")).toBeInTheDocument()
    expect(within(list).getByText("Run a command on the host")).toBeInTheDocument()
    emit("voice-approvals", [])
    expect(screen.queryByTestId("widget-approvals")).not.toBeInTheDocument()
  })

  it("reads those already waiting when it starts", async () => {
    pending = [permission]
    render(<WidgetApprovals />)
    expect(await screen.findByText("Run a command")).toBeInTheDocument()
    expect(screen.getByText("1 needs you")).toBeInTheDocument()
  })

  it("names the server only when approvals come from more than one", async () => {
    pending = [credential]
    render(<WidgetApprovals />)
    await screen.findByText("Use github")
    expect(screen.queryByText(/tessitura/)).not.toBeInTheDocument()
    emit("voice-approvals", [credential, permission])
    expect(screen.getByText(/tessitura/)).toBeInTheDocument()
    expect(screen.getByText(/erebus/)).toBeInTheDocument()
  })

  it("opens a line on its detail and decides it with the tray's options", async () => {
    pending = [credential]
    render(<WidgetApprovals />)
    fireEvent.click(await screen.findByText("Use github"))
    expect(screen.getByText("cpave3/legato")).toBeInTheDocument()
    expect(screen.getByText("Push the fix")).toBeInTheDocument()
    fireEvent.click(screen.getByRole("button", { name: "once" }))
    await act(async () => {})
    expect(calls("voice_approval_decide")).toEqual([{ server: "s1", approval: "ap-2", decision: "allow", option: "once" }])
    expect(screen.getByText("Allowed: once")).toBeInTheDocument()
  })

  it("denies with no note, or opens the tray for one", async () => {
    pending = [permission]
    render(<WidgetApprovals />)
    fireEvent.click(await screen.findByText("Run a command"))
    fireEvent.click(screen.getByRole("button", { name: "Deny" }))
    await act(async () => {})
    expect(calls("voice_approval_decide")).toEqual([{ server: "s2", approval: "ap-3", decision: "deny", option: null }])
    expect(screen.getByText("Denied")).toBeInTheDocument()

    cleanup()
    render(<WidgetApprovals />)
    fireEvent.click(await screen.findByText("Run a command"))
    fireEvent.click(screen.getByRole("button", { name: "Deny with a note…" }))
    expect(calls("voice_approval_open")).toEqual([{ server: "s2", approval: "ap-3" }])
  })

  it("says when another decision came first", async () => {
    pending = [permission]
    answers.voice_approval_decide = () => Promise.reject("approval is no longer pending")
    render(<WidgetApprovals />)
    fireEvent.click(await screen.findByText("Run a command"))
    fireEvent.click(screen.getByRole("button", { name: "Allow once" }))
    await act(async () => {})
    expect(screen.getByRole("alert")).toHaveTextContent("Already decided somewhere else.")
  })

  it("sends a question or a secret to the main window", async () => {
    pending = [question]
    render(<WidgetApprovals />)
    fireEvent.click(await screen.findByText("Which database?"))
    expect(screen.queryByRole("button", { name: "Deny" })).not.toBeInTheDocument()
    fireEvent.click(screen.getByRole("button", { name: "Answer in Legato" }))
    expect(calls("voice_approval_open")).toEqual([{ server: "s2", approval: "ap-4" }])
  })

  it("signs a signed one with the key, from the request the app prepared", async () => {
    pending = [hostCommand]
    let touched: () => void = () => {}
    answers.prepare = () => Promise.resolve({ request: { options: hostCommand.approval.options }, device: "dev-1", with: "security-key" })
    answers.sign = () => new Promise((resolve) => { touched = () => resolve({ device: "dev-1", message_sha256: "ab", signature: "cd", verified: false, counter: 3 }) })
    render(<WidgetApprovals />)
    fireEvent.click(await screen.findByText("Run a command on the host"))
    expect(screen.getByText("$ dnf upgrade -y")).toBeInTheDocument()
    expect(screen.getByText("host as root")).toBeInTheDocument()
    fireEvent.click(screen.getByRole("button", { name: "◈ Approve" }))
    await act(async () => {})
    expect(calls("prepare")).toEqual([{ server: "s1", approval: "ap-1" }])
    expect(calls("sign")).toEqual([{ server: "s1", approval: "ap-1", option: "allow" }])
    expect(screen.getByText("Touch your key")).toBeInTheDocument()
    await act(async () => touched())
    expect(screen.getByText("Signed")).toBeInTheDocument()
    expect(calls("voice_approval_decide")).toEqual([])
  })

  it("waits for a key to be plugged in, as the sheet does", async () => {
    vi.useFakeTimers()
    try {
      pending = [hostCommand]
      let plugged = false
      answers.prepare = () => (plugged
        ? Promise.resolve({ request: { options: hostCommand.approval.options }, device: "dev-1", with: "touch-id" })
        : Promise.reject("Plug in a key this server knows."))
      answers.sign = () => new Promise(() => {})
      render(<WidgetApprovals />)
      await act(async () => {})
      fireEvent.click(screen.getByText("Run a command on the host"))
      fireEvent.click(screen.getByRole("button", { name: "◈ Approve" }))
      await act(async () => {})
      expect(screen.getByText("Plug in your key")).toBeInTheDocument()
      plugged = true
      await act(async () => { vi.advanceTimersByTime(1500) })
      expect(screen.getByText("Use Touch ID")).toBeInTheDocument()
    } finally {
      vi.useRealTimers()
    }
  })

  it("can open the main window's sheet on a signed one", async () => {
    pending = [hostCommand]
    render(<WidgetApprovals />)
    fireEvent.click(await screen.findByText("Run a command on the host"))
    fireEvent.click(screen.getByRole("button", { name: "Open the sheet" }))
    expect(calls("voice_approval_sheet")).toEqual([{ server: "s1", approval: "ap-1" }])
  })

  it("never takes focus: a press does not move focus into the widget", async () => {
    pending = [credential]
    render(<WidgetApprovals />)
    const line = await screen.findByText("Use github")
    const down = new MouseEvent("mousedown", { bubbles: true, cancelable: true })
    line.dispatchEvent(down)
    expect(down.defaultPrevented).toBe(true)
  })
})
