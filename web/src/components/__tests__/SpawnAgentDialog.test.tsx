import { QueryClientProvider } from "@tanstack/react-query"
import { createQueryClient } from "../../lib/query"
import type { ComponentProps } from "react"
import { cleanup, fireEvent, render, screen, within } from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { SpawnAgentDialog } from "../SpawnAgentDialog"
import type { SpawnAgentRequest } from "../../lib/spawnRequest"

// The dialog itself, driven through its props: what it asks, and the request
// it hands onSubmit. The modals that wrap it have their own tests of what
// they send to the server.

const { apiFetch } = vi.hoisted(() => ({ apiFetch: vi.fn() }))

vi.mock("../../hooks/useServer", () => ({ useServer: () => ({ baseUrl: "" }) }))
const { settingsData } = vi.hoisted(() => ({
  settingsData: {
    working_dir: "/default", sandbox_enabled: false, sandbox_docker_enabled: false, fresh_clones_enabled: false,
    repositories: [
      { name: "Legato", repo: "cpave3/legato", primary_dir: "/projects/legato", base_branch: "main" },
      { name: "Autopilot", repo: "autopilot-dev/autopilot", primary_dir: "", base_branch: "develop" },
    ],
  },
}))
vi.mock("../../hooks/useSettings", () => ({ useSettings: () => ({ data: settingsData }) }))
vi.mock("../../lib/api", async (importOriginal) => ({ ...await importOriginal<typeof import("../../lib/api")>(), apiFetch }))

const presets = {
  presets: [
    { name: "autopilot", path: "/home/me/Development/autopilot", display: "~/Development/autopilot", saved: false },
    { name: "legato", path: "/home/me/Projects/legato", display: "~/Projects/legato", saved: true },
  ],
  default: { path: "/home/me/Projects/legato", display: "~/Projects/legato" },
}

const inside = {
  query: "~/Development/", resolved: "/home/me/Development", exists: true,
  base: "~/Development/", prefix: "", total: 5,
  items: [
    { name: "autopilot", path: "/home/me/Development/autopilot", display: "~/Development/autopilot/", git: true, preset: 1 },
    { name: "automation", path: "/home/me/Development/automation", display: "~/Development/automation/", git: false, preset: 0 },
    { name: "zeta", path: "/home/me/Development/zeta", display: "~/Development/zeta/", git: false, preset: 0 },
  ],
}

function json(body: unknown, status = 200) {
  return Promise.resolve(new Response(JSON.stringify(body), { status }))
}

function serve(overrides: Record<string, () => Promise<Response>> = {}) {
  apiFetch.mockImplementation((_: string, path: string, init?: RequestInit) => {
    const key = `${init?.method ?? "GET"} ${path.split("?")[0]}`
    if (overrides[key]) return overrides[key]()
    if (key === "GET /api/job-clones") return json({ clones: [] })
    if (key === "GET /api/adapters") return json({ adapters: ["chimera", "claude-code", "codex"], default: "chimera" })
    if (key === "GET /api/directories/presets") return json(presets)
    if (key === "GET /api/directories") {
      const query = new URLSearchParams(path.split("?")[1]).get("q") ?? ""
      return json(query === inside.query ? inside : { query, resolved: "", exists: false, base: "", prefix: "", total: 0, items: [] })
    }
    if (key === "GET /api/resources") return json({ resources: [] })
    if (key === "GET /api/macros") return json({ macros: [] })
    if (key === "GET /api/engrams") return json({ enabled: false, engrams: [] })
    return json({}, 200)
  })
}

let submitted: SpawnAgentRequest[] = []
let onSubmit = vi.fn(async (request: SpawnAgentRequest) => { submitted.push(request) })

async function openDialog(props: Partial<ComponentProps<typeof SpawnAgentDialog>> = {}, preset = /^autopilot/) {
  render(
    <QueryClientProvider client={createQueryClient()}>
      <SpawnAgentDialog open onClose={() => {}} name="Start agent" description="Start an agent" action="Start" acting="Starting…" onSubmit={onSubmit} {...props} />
    </QueryClientProvider>,
  )
  const dialog = await screen.findByRole("dialog", { name: "Start agent" })
  await screen.findByRole("button", { name: preset })
  await vi.waitFor(() => expect((screen.getByRole("button", { name: /^Start/ }) as HTMLButtonElement).disabled).toBe(false))
  return dialog
}

async function submit(name: RegExp | string = /^Start/) {
  fireEvent.click(screen.getByRole("button", { name }))
  await vi.waitFor(() => expect(submitted).toHaveLength(1))
  return submitted[0]
}

function pathField() {
  return screen.getByRole("combobox", { name: "Path" }) as HTMLInputElement
}

describe("SpawnAgentDialog", () => {
  beforeEach(() => {
    apiFetch.mockReset()
    serve()
    submitted = []
    onSubmit = vi.fn(async (request: SpawnAgentRequest) => { submitted.push(request) })
  })
  afterEach(cleanup)

  it("hands over the default choices: no title, the default agent and the default folder", async () => {
    await openDialog()
    expect(screen.queryByRole("textbox", { name: "Title" })).toBeNull()
    expect(await submit("Start in legato")).toEqual({ title: "", agentKind: "", workingDir: "~/Projects/legato" })
  })

  it("trims the title it asks for, and starts in the folder it was given", async () => {
    await openDialog({ titled: true, initialWorkingDir: "~/Development/autopilot" })
    expect(pathField().value).toBe("~/Development/autopilot")
    fireEvent.change(screen.getByRole("textbox", { name: "Title" }), { target: { value: "  fix it  " } })
    expect(await submit("Start in autopilot")).toEqual({ title: "fix it", agentKind: "", workingDir: "~/Development/autopilot" })
  })

  it("names a typed folder that is no preset by its last part, and shows the path field for it", async () => {
    await openDialog({ initialWorkingDir: "/srv/work/thing" })
    expect(screen.queryByRole("button", { pressed: true })).toBeNull()
    expect((await submit("Start in thing")).workingDir).toBe("/srv/work/thing")
  })

  describe("directory suggestions", () => {
    it("highlights nothing inside a directory until the arrow keys pick, then enter takes the pick", async () => {
      await openDialog()
      fireEvent.focus(pathField())
      fireEvent.change(pathField(), { target: { value: "~/Development/" } })
      const list = await screen.findByRole("listbox", { name: "Path suggestions" })
      const options = within(list).getAllByRole("option")
      expect(options.map((option) => option.getAttribute("aria-selected"))).toEqual(["false", "false", "false"])
      expect(pathField().getAttribute("aria-activedescendant")).toBeNull()
      expect(screen.getByText("3 of 5 folders · keep typing to narrow them")).toBeTruthy()

      fireEvent.keyDown(pathField(), { key: "ArrowDown" })
      fireEvent.keyDown(pathField(), { key: "ArrowDown" })
      fireEvent.keyDown(pathField(), { key: "ArrowDown" })
      fireEvent.keyDown(pathField(), { key: "ArrowUp" })
      const selected = within(list).getAllByRole("option").map((option) => option.getAttribute("aria-selected"))
      expect(selected).toEqual(["false", "true", "false"])
      expect(pathField().getAttribute("aria-activedescendant")).toBe(within(list).getAllByRole("option")[1].id)

      fireEvent.keyDown(pathField(), { key: "Enter" })
      expect(pathField().value).toBe("~/Development/automation/")
      expect(submitted).toHaveLength(0)
    })

    it("stops at the ends of the list", async () => {
      await openDialog()
      fireEvent.focus(pathField())
      fireEvent.change(pathField(), { target: { value: "~/Development/" } })
      await screen.findByRole("listbox", { name: "Path suggestions" })
      fireEvent.keyDown(pathField(), { key: "ArrowUp" })
      expect(screen.getAllByRole("option")[0].getAttribute("aria-selected")).toBe("true")
      for (let i = 0; i < 5; i++) fireEvent.keyDown(pathField(), { key: "ArrowDown" })
      expect(screen.getAllByRole("option")[2].getAttribute("aria-selected")).toBe("true")
    })

    it("takes the first suggestion on tab when none is picked, and shows what it would add", async () => {
      await openDialog()
      fireEvent.focus(pathField())
      fireEvent.change(pathField(), { target: { value: "~/Development/" } })
      await screen.findByRole("listbox", { name: "Path suggestions" })
      fireEvent.keyDown(pathField(), { key: "Tab" })
      expect(pathField().value).toBe("~/Development/autopilot/")
      expect(screen.getByRole("button", { name: /autopilot/, pressed: true })).toBeTruthy()
    })

    it("lets shift+tab and enter without a pick through", async () => {
      await openDialog()
      fireEvent.focus(pathField())
      fireEvent.change(pathField(), { target: { value: "~/Development/" } })
      await screen.findByRole("listbox", { name: "Path suggestions" })
      fireEvent.keyDown(pathField(), { key: "Tab", shiftKey: true })
      fireEvent.keyDown(pathField(), { key: "Enter" })
      expect(pathField().value).toBe("~/Development/")
    })

    it("closes the list on escape and on blur", async () => {
      await openDialog()
      fireEvent.focus(pathField())
      fireEvent.change(pathField(), { target: { value: "~/Development/" } })
      await screen.findByRole("listbox", { name: "Path suggestions" })
      fireEvent.keyDown(pathField(), { key: "Escape" })
      expect(screen.queryByRole("listbox")).toBeNull()
      expect(pathField().getAttribute("aria-expanded")).toBe("false")
      fireEvent.focus(pathField())
      await screen.findByRole("listbox", { name: "Path suggestions" })
      fireEvent.blur(pathField())
      expect(screen.queryByRole("listbox")).toBeNull()
    })

    it("asks for no suggestions from a server without a directory service", async () => {
      serve({ "GET /api/directories/presets": () => json({ error: "no" }, 404) })
      await openDialog({}, /^Legato/)
      expect(pathField().value).toBe("/default")
      fireEvent.focus(pathField())
      fireEvent.change(pathField(), { target: { value: "~/Development/" } })
      await new Promise((resolve) => setTimeout(resolve, 200))
      expect(apiFetch.mock.calls.some(([, path]) => path.startsWith("/api/directories?"))).toBe(false)
      expect((screen.getByRole("checkbox", { name: "Save path as preset" }) as HTMLInputElement).disabled).toBe(true)
    })

    it("saves a typed path as a preset before it hands the request over", async () => {
      const saved: string[] = []
      serve({ "POST /api/directories/presets": () => { saved.push("x"); return json({ name: "x", path: "/x", display: "/x", saved: true }, 201) } })
      await openDialog()
      fireEvent.change(pathField(), { target: { value: "  ~/Development/zeta  " } })
      fireEvent.click(screen.getByRole("checkbox", { name: "Save path as preset" }))
      expect((await submit("Start in zeta")).workingDir).toBe("~/Development/zeta")
      expect(saved).toHaveLength(1)
    })
  })

  describe("the session policy", () => {
    it("offers to resume the tool's saved session, and hands over the choice", async () => {
      await openDialog({ resumableAgentKinds: ["chimera"] })
      const group = screen.getByRole("group", { name: "Existing Chimera session" })
      expect((within(group).getByRole("radio", { name: "Resume previous session" }) as HTMLInputElement).checked).toBe(true)
      expect(within(group).getByText("Continue with the previous Chimera context.")).toBeTruthy()
      fireEvent.click(within(group).getByRole("radio", { name: "Start new session" }))
      expect(await submit("Start in legato")).toEqual({ title: "", agentKind: "", workingDir: "~/Projects/legato", sessionPolicy: "new" })
    })

    it("resumes unless told otherwise", async () => {
      await openDialog({ resumableAgentKinds: ["chimera"] })
      expect((await submit("Start in legato")).sessionPolicy).toBe("resume")
    })

    it("asks nothing for a tool with no saved session", async () => {
      await openDialog({ resumableAgentKinds: ["chimera"] })
      fireEvent.click(screen.getByRole("radio", { name: "Codex" }))
      expect(screen.queryByRole("group", { name: /session/ })).toBeNull()
      expect(await submit("Start in legato")).toEqual({ title: "", agentKind: "codex", workingDir: "~/Projects/legato" })
    })

    it("keeps the saved session's account while resuming, and sends the chosen one for a new session", async () => {
      serve({ "GET /api/adapters": () => json({ adapters: ["claude-code"], default: "claude-code", accounts: [{ name: "work", label: "Work", provider: "claude" }] }) })
      await openDialog({ resumableAgentKinds: ["claude-code"] })
      const account = screen.getByRole("combobox", { name: "Account" }) as HTMLSelectElement
      expect(account.disabled).toBe(true)
      expect(account.options[0].textContent).toBe("Saved session account")
      fireEvent.click(screen.getByRole("radio", { name: "Start new session" }))
      expect(account.disabled).toBe(false)
      expect(Array.from(account.options).map((option) => option.textContent)).toEqual(["Follow usage profile", "Default account", "Work"])
      fireEvent.change(account, { target: { value: "work" } })
      expect(await submit("Start in legato")).toEqual({
        title: "", agentKind: "claude-code", account: "work", workingDir: "~/Projects/legato", sessionPolicy: "new",
      })
    })

    it("sends no account while resuming the saved session", async () => {
      serve({ "GET /api/adapters": () => json({ adapters: ["claude-code"], default: "claude-code", accounts: [{ name: "work", label: "Work", provider: "claude" }] }) })
      await openDialog({ resumableAgentKinds: ["claude-code"] })
      fireEvent.click(screen.getByRole("radio", { name: "Start new session" }))
      fireEvent.change(screen.getByRole("combobox", { name: "Account" }), { target: { value: "work" } })
      fireEvent.click(screen.getByRole("radio", { name: "Resume previous session" }))
      expect(await submit("Start in legato")).toEqual({ title: "", agentKind: "", workingDir: "~/Projects/legato", sessionPolicy: "resume" })
    })

    it("names an explicit tool's default account, and forgets the account when the tool changes", async () => {
      serve({ "GET /api/adapters": () => json({ adapters: ["claude-code", "codex"], default: "codex", accounts: [{ name: "work", label: "", provider: "claude" }] }) })
      await openDialog()
      expect(screen.queryByRole("combobox", { name: "Account" })).toBeNull()
      fireEvent.click(screen.getByRole("radio", { name: "Claude Code" }))
      const account = screen.getByRole("combobox", { name: "Account" }) as HTMLSelectElement
      expect(Array.from(account.options).map((option) => option.textContent)).toEqual(["Default account", "work"])
      fireEvent.change(account, { target: { value: "work" } })
      fireEvent.click(screen.getByRole("radio", { name: "Codex" }))
      fireEvent.click(screen.getByRole("radio", { name: "Claude Code" }))
      expect((screen.getByRole("combobox", { name: "Account" }) as HTMLSelectElement).value).toBe("")
    })
  })

  describe("folder or clone", () => {
    beforeEach(() => { settingsData.fresh_clones_enabled = true })
    afterEach(() => { settingsData.fresh_clones_enabled = false })

    it("offers no choice of where when nothing can be cloned", async () => {
      settingsData.fresh_clones_enabled = false
      await openDialog()
      expect(screen.queryByRole("radiogroup", { name: "Where" })).toBeNull()
    })

    it("starts in a folder, and in a new clone on the branch it was given when Clone is picked", async () => {
      await openDialog({ defaultBranch: "legato/abc" })
      const where = screen.getByRole("radiogroup", { name: "Where" })
      expect(within(where).getByRole("radio", { name: "Folder" }).getAttribute("aria-checked")).toBe("true")
      fireEvent.click(within(where).getByRole("radio", { name: "Clone" }))
      expect((screen.getByRole("textbox", { name: "Branch" }) as HTMLInputElement).value).toBe("legato/abc")
      expect(screen.getByText(/A new clone of cpave3\/legato/)).toBeTruthy()
      fireEvent.change(screen.getByRole("textbox", { name: "Base branch" }), { target: { value: " release " } })
      expect(await submit("Start in Legato")).toEqual({
        title: "", agentKind: "", workingDir: "", clone: { repo: "cpave3/legato", baseBranch: "release", branch: "legato/abc" },
      })
    })

    it("goes to the task's own clone first and joins it", async () => {
      serve({ "GET /api/job-clones": () => json({ clones: [
        { id: 7, repo: "autopilot-dev/autopilot", path: "/data/jobs/ex-7/autopilot", branch: "feat/a", task_ids: ["T1"], busy: false, last_used_at: "2026-11-02T10:00:00Z", unpushed: 0 },
      ] }) })
      await openDialog({ taskId: "T1" })
      fireEvent.click(screen.getByRole("radio", { name: "Clone" }))
      const repository = await screen.findByRole("combobox", { name: "Repository" }) as HTMLSelectElement
      await vi.waitFor(() => expect(repository.value).toBe("Autopilot"))
      const clone = screen.getByRole("combobox", { name: "Clone" }) as HTMLSelectElement
      expect(Array.from(clone.options).map((option) => option.textContent)).toEqual(["feat/a · this task's"])
      expect(screen.getByText(/Continues in \/data\/jobs\/ex-7\/autopilot as it was left/)).toBeTruthy()
      expect(await submit("Start in Autopilot")).toEqual({ title: "", agentKind: "", workingDir: "", joinClone: { id: 7, branch: "" } })
    })

    it("asks for no engrams of a folder while in a clone", async () => {
      await openDialog()
      fireEvent.click(screen.getByRole("radio", { name: "Clone" }))
      await vi.waitFor(() => expect(apiFetch.mock.calls.some(([, path]) => path.startsWith("/api/engrams") && path.includes("repo=cpave3"))).toBe(true))
    })
  })

  describe("managed hooks", () => {
    const hook = (name: string, more: Record<string, unknown> = {}) => ({
      name, description: `${name}.`, events: ["stop"], agent_tools: [], read_only: false, path: `/hooks/${name}`, enabled: true, ...more,
    })
    const hooksServed = (supported = ["claude-code"]) => serve({ "GET /api/settings/managed-hooks": () => json({
      dir: "/hooks", supported_tools: supported, repositories: [],
      hooks: [
        hook("codeeval"),
        hook("lint", { agent_tools: ["claude-code"] }),
        hook("codex-only", { agent_tools: ["codex"] }),
        hook("off-for-all", { enabled: false }),
        hook("broken", { error: "no events" }),
      ],
    }) })
    const hookBoxes = () => within(screen.queryByRole("group", { name: "Managed hooks" }) ?? document.createElement("div"))
      .queryAllByRole("checkbox").map((box) => [box.getAttribute("aria-label") ?? box.closest("label")?.textContent, (box as HTMLInputElement).checked])

    it("ticks each hook that would apply to the chosen tool, and follows the tool", async () => {
      hooksServed()
      await openDialog()
      fireEvent.click(screen.getByRole("radio", { name: "Claude Code" }))
      await vi.waitFor(() => expect(hookBoxes()).toEqual([["codeeval", true], ["lint", true]]))

      // Codex runs no managed hooks yet, and neither does Chimera.
      fireEvent.click(screen.getByRole("radio", { name: "Codex" }))
      expect(screen.queryByRole("group", { name: "Managed hooks" })).toBeNull()
      fireEvent.click(screen.getByRole("radio", { name: "Chimera" }))
      expect(screen.queryByRole("group", { name: "Managed hooks" })).toBeNull()
      expect((await submit("Start in legato")).hooksOff).toBeUndefined()
    })

    it("sends a hook unticked as hooks_off, and sends nothing while every box is ticked", async () => {
      hooksServed()
      await openDialog()
      fireEvent.click(screen.getByRole("radio", { name: "Claude Code" }))
      const lint = await screen.findByRole("checkbox", { name: "lint" })
      fireEvent.click(lint)
      expect(hookBoxes()).toEqual([["codeeval", true], ["lint", false]])
      expect((await submit("Start in legato")).hooksOff).toEqual(["lint"])
    })

    it("sends no off for a hook unticked under another tool", async () => {
      hooksServed(["claude-code", "codex"])
      await openDialog()
      fireEvent.click(screen.getByRole("radio", { name: "Claude Code" }))
      fireEvent.click(await screen.findByRole("checkbox", { name: "lint" }))
      fireEvent.click(screen.getByRole("radio", { name: "Codex" }))
      await vi.waitFor(() => expect(hookBoxes()).toEqual([["codeeval", true], ["codex-only", true]]))
      expect((await submit("Start in legato")).hooksOff).toBeUndefined()
    })

    it("shows no section without a hook that applies, or on a server without managed hooks", async () => {
      serve({ "GET /api/settings/managed-hooks": () => json({ error: "managed hooks are not available here" }, 503) })
      await openDialog()
      fireEvent.click(screen.getByRole("radio", { name: "Claude Code" }))
      await vi.waitFor(() => expect(apiFetch.mock.calls.some(([, path]) => path === "/api/settings/managed-hooks")).toBe(true))
      expect(screen.queryByRole("group", { name: "Managed hooks" })).toBeNull()
      cleanup()

      // An older server names no tool that runs them.
      serve({ "GET /api/settings/managed-hooks": () => json({ dir: "/hooks", hooks: [hook("codeeval")] }) })
      await openDialog()
      fireEvent.click(screen.getByRole("radio", { name: "Claude Code" }))
      await vi.waitFor(() => expect(apiFetch.mock.calls.some(([, path]) => path === "/api/settings/managed-hooks")).toBe(true))
      expect(screen.queryByRole("group", { name: "Managed hooks" })).toBeNull()
    })
  })

  it("gives the agent the resources picked, and takes back one unpicked", async () => {
    serve({ "GET /api/resources": () => json({ resources: [
      { name: "Vault", configured: true, paths: [{ path: "/home/me/Vault", access: "write" }] },
      { id: 2, name: "Specs", configured: false, paths: [{ path: "/srv/specs", access: "read" }] },
    ] }) })
    await openDialog()
    const vault = await screen.findByRole("checkbox", { name: /Vault/ })
    expect(vault.closest("label")?.getAttribute("title")).toBe("/home/me/Vault (read and write)")
    fireEvent.click(vault)
    fireEvent.click(screen.getByRole("checkbox", { name: /Specs/ }))
    fireEvent.click(vault)
    expect((await submit("Start in legato")).resources).toEqual(["Specs"])
  })

  it("starts with a macro: its message, tool, account and tier", async () => {
    serve({ "GET /api/macros": () => json({ macros: [
      { name: "Plan", keys: "Plan this task.\n", applies_to: "task" },
      { name: "Work", keys: "Do the work.", account: "work", tier: "smart" },
    ] }) })
    await openDialog()
    const macro = await screen.findByRole("combobox", { name: "Start with macro" })
    await vi.waitFor(() => expect(screen.getByRole("option", { name: "Work" })).toBeTruthy())
    fireEvent.change(macro, { target: { value: "Work" } })
    expect((screen.getByRole("textbox", { name: "First message" }) as HTMLTextAreaElement).value).toBe("Do the work.")
    expect(screen.getByText("Model tier: smart")).toBeTruthy()
    expect(screen.getByRole("radio", { name: "Claude Code" }).getAttribute("aria-checked")).toBe("true")
    expect(await submit("Start in legato")).toEqual({
      title: "", agentKind: "claude-code", account: "work", workingDir: "~/Projects/legato", prompt: "Do the work.", tier: "smart",
    })
  })

  it("clears the tier when None is picked again", async () => {
    serve({ "GET /api/macros": () => json({ macros: [{ name: "Deep", keys: "Go deep.", agent: "codex", tier: "smart" }] }) })
    await openDialog()
    const macro = await screen.findByRole("combobox", { name: "Start with macro" })
    await vi.waitFor(() => expect(screen.getByRole("option", { name: "Deep" })).toBeTruthy())
    fireEvent.change(macro, { target: { value: "Deep" } })
    fireEvent.change(macro, { target: { value: "" } })
    expect(screen.queryByText(/Model tier/)).toBeNull()
    expect(await submit("Start in legato")).toEqual({ title: "", agentKind: "codex", workingDir: "~/Projects/legato", prompt: "Go deep." })
  })

  it("trims the first message, and sends none that is only spaces", async () => {
    await openDialog()
    fireEvent.change(screen.getByRole("textbox", { name: "First message" }), { target: { value: "   " } })
    expect((await submit("Start in legato")).prompt).toBeUndefined()
  })

  describe("the sandbox toggles", () => {
    beforeEach(() => { settingsData.sandbox_enabled = true; settingsData.sandbox_docker_enabled = true })
    afterEach(() => { settingsData.sandbox_enabled = false; settingsData.sandbox_docker_enabled = false })

    it("hands over the host network for the agent's containers", async () => {
      await openDialog()
      fireEvent.click(screen.getByRole("checkbox", { name: /host network/ }))
      expect(await submit("Start in legato")).toEqual({ title: "", agentKind: "", workingDir: "~/Projects/legato", dockerHostNetwork: true })
    })

    it("drops the host network for an agent outside the sandbox", async () => {
      await openDialog()
      fireEvent.click(screen.getByRole("checkbox", { name: /host network/ }))
      fireEvent.click(screen.getByRole("checkbox", { name: /Run without sandbox/ }))
      expect(screen.queryByRole("checkbox", { name: /host network/ })).toBeNull()
      expect(await submit("Start in legato")).toEqual({ title: "", agentKind: "", workingDir: "~/Projects/legato", sandbox: false })
    })
  })

  it("hands over the engram and asks to remember it", async () => {
    serve({ "GET /api/engrams": () => json({
      enabled: true,
      place: { key: "dir:/x", label: "~/Projects/legato", kind: "folder", default: "" },
      engrams: [{ name: "argus", summary: "Watches pull requests" }],
    }) })
    await openDialog()
    fireEvent.change(await screen.findByRole("combobox", { name: "Engram" }), { target: { value: "argus" } })
    fireEvent.click(screen.getByRole("checkbox", { name: /Remember for this folder/ }))
    expect(await submit("Start in legato")).toEqual({
      title: "", agentKind: "", workingDir: "~/Projects/legato", engram: "argus", rememberEngram: true,
    })
  })

  it("shows why the agent was not started and stays open, with the button free again", async () => {
    onSubmit = vi.fn(async () => { throw new Error("no such directory") })
    await openDialog()
    fireEvent.click(screen.getByRole("button", { name: "Start in legato" }))
    expect((await screen.findByRole("alert")).textContent).toBe("no such directory")
    expect((screen.getByRole("button", { name: "Start in legato" }) as HTMLButtonElement).disabled).toBe(false)
  })

  it("closes from its button and its backdrop", async () => {
    const onClose = vi.fn()
    const dialog = await openDialog({ onClose })
    fireEvent.click(screen.getByRole("button", { name: "Close" }))
    fireEvent.mouseDown(dialog.parentElement!)
    expect(onClose).toHaveBeenCalledTimes(2)
  })

  it("renders nothing while closed", () => {
    render(
      <QueryClientProvider client={createQueryClient()}>
        <SpawnAgentDialog open={false} onClose={() => {}} name="Start agent" description="" action="Start" acting="Starting…" onSubmit={onSubmit} />
      </QueryClientProvider>,
    )
    expect(screen.queryByRole("dialog")).toBeNull()
  })
})
