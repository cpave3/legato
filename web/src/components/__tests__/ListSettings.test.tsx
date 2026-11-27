import "@testing-library/jest-dom/vitest"
import { describe, it, expect, vi, afterEach, beforeEach } from "vitest"
import { render, screen, cleanup, fireEvent, waitFor, within } from "@testing-library/react"
import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import type { ConfigList } from "../../lib/configSettings"

const mockFetch = vi.fn()
const mockSave = vi.fn()
const mockRemove = vi.fn()
vi.mock("../../lib/configSettings", async (importActual) => ({
  ...(await importActual<typeof import("../../lib/configSettings")>()),
  fetchConfigLists: (...a: unknown[]) => mockFetch(...a),
  saveConfigListEntry: (...a: unknown[]) => mockSave(...a),
  removeConfigListEntry: (...a: unknown[]) => mockRemove(...a),
}))

import { ListSettings } from "../settings/ListSettings"

const list = (over: Partial<ConfigList>): ConfigList => ({ key: "workspaces", title: "Workspaces", class: "change", mode: "restart", name_field: "name", entries: [], ...over })

const workspaces = list({
  entries: [
    { name: "Work", value: { color: "#F0B44C" }, source: "config.yaml", pinned: true },
    { name: "Home", value: { color: "#3ECF8E", path: "~/home", owner: "me" }, source: "database", pinned: false },
  ],
})

function show(lists: ConfigList[], only?: string[], meters: string[] = []) {
  mockFetch.mockResolvedValue(lists)
  return render(<QueryClientProvider client={new QueryClient()}><ListSettings baseUrl="http://legato" lists={only} meters={meters} /></QueryClientProvider>)
}

describe("List forms", () => {
  beforeEach(() => {
    mockFetch.mockReset()
    mockSave.mockReset(); mockSave.mockResolvedValue(undefined)
    mockRemove.mockReset(); mockRemove.mockResolvedValue(undefined)
  })
  afterEach(() => cleanup())

  it("saves a usage profile with adapter tiers and nested reviewer intent", async () => {
    show([list({ key: "usage.profiles", title: "Usage profiles", mode: "live" }), list({ key: "adapters", title: "Adapters", entries: [
      { name: "codex", value: { tiers: { fast: {} } }, source: "database", pinned: false },
      { name: "chimera", value: { tiers: { review: {} } }, source: "database", pinned: false },
    ] })], ["usage.profiles"])
    fireEvent.click(await screen.findByRole("button", { name: "New profile" }))
    fireEvent.change(screen.getByRole("textbox", { name: "Name" }), { target: { value: "Economy" } })
    fireEvent.change(screen.getByRole("combobox", { name: "Agent" }), { target: { value: "codex" } })
    fireEvent.change(screen.getByRole("combobox", { name: "Tier" }), { target: { value: "fast" } })
    fireEvent.change(screen.getByRole("combobox", { name: "Reviewer agent" }), { target: { value: "chimera" } })
    fireEvent.change(screen.getByRole("combobox", { name: "Reviewer tier" }), { target: { value: "review" } })
    fireEvent.click(screen.getByRole("button", { name: "Save" }))
    await waitFor(() => expect(mockSave).toHaveBeenCalledWith("http://legato", "usage.profiles", "Economy", { agent: "codex", tier: "fast", reviewer: { agent: "chimera", tier: "review" } }))
  })

  it("saves independent author and reviewer Claude accounts", async () => {
    show([list({ key: "usage.profiles", title: "Usage profiles", mode: "live" }), list({ key: "claude_accounts", title: "Claude accounts", entries: [
      { name: "work", value: { label: "Work subscription" }, source: "database", pinned: false },
      { name: "review", value: { label: "Review subscription" }, source: "database", pinned: false },
    ] })], ["usage.profiles"])
    fireEvent.click(await screen.findByRole("button", { name: "New profile" }))
    fireEvent.change(screen.getByRole("textbox", { name: "Name" }), { target: { value: "Split" } })
    fireEvent.change(screen.getByRole("combobox", { name: "Agent" }), { target: { value: "claude-code" } })
    fireEvent.change(screen.getByRole("combobox", { name: "Account" }), { target: { value: "work" } })
    fireEvent.change(screen.getByRole("combobox", { name: "Reviewer agent" }), { target: { value: "claude-code" } })
    fireEvent.change(screen.getByRole("combobox", { name: "Reviewer account" }), { target: { value: "review" } })
    fireEvent.click(screen.getByRole("button", { name: "Save" }))
    await waitFor(() => expect(mockSave).toHaveBeenCalledWith("http://legato", "usage.profiles", "Split", { agent: "claude-code", account: "work", reviewer: { agent: "claude-code", account: "review" } }))
  })

  it("orders usage rules lower first and retains numeric rule intent on a refused save", async () => {
    mockSave.mockRejectedValue(new Error("Choose an existing profile"))
    show([list({ key: "usage.rules", title: "Usage rules", mode: "live", entries: [
      { name: "Late", value: { order: 20 }, source: "database", pinned: false },
      { name: "First", value: { order: 1 }, source: "config.yaml", pinned: true },
    ] }), list({ key: "usage.profiles", title: "Usage profiles", entries: [{ name: "Economy", value: { agent: "codex" }, source: "database", pinned: false }] })], ["usage.rules"], ["claude.five_hour", "claude.weekly", "codex.weekly", "opencode.monthly"])
    const rules = await screen.findByRole("list", { name: "Usage rules" })
    expect(within(rules).getAllByRole("listitem")[0]).toHaveTextContent("First")
    fireEvent.click(screen.getByRole("button", { name: "New rule" }))
    fireEvent.change(screen.getByRole("textbox", { name: "Name" }), { target: { value: "Busy" } })
    fireEvent.change(screen.getByRole("spinbutton", { name: "Order" }), { target: { value: "2" } })
    fireEvent.change(screen.getByRole("spinbutton", { name: "At least (%)" }), { target: { value: "90" } })
    fireEvent.change(screen.getByRole("combobox", { name: "Meter" }), { target: { value: "claude.weekly" } })
    fireEvent.change(screen.getByRole("combobox", { name: "Profile" }), { target: { value: "Economy" } })
    fireEvent.click(screen.getByRole("button", { name: "Save" }))
    expect(await screen.findByRole("alert")).toHaveTextContent("Choose an existing profile")
    expect(mockSave).toHaveBeenCalledWith("http://legato", "usage.rules", "Busy", { order: 2, at_least: 90, meter: "claude.weekly", profile: "Economy" })
    expect(screen.getByRole("spinbutton", { name: "Order" })).toHaveValue(2)
  })

  it("shows a config.yaml entry read-only with a lock, and removes one kept here", async () => {
    show([workspaces])
    const work = await screen.findByTestId("entry-workspaces-Work")
    expect(within(work).getByLabelText("Pinned by config.yaml")).toBeTruthy()
    expect(within(work).queryByRole("button", { name: /Edit|Remove/ })).toBeNull()
    fireEvent.click(within(screen.getByTestId("entry-workspaces-Home")).getByRole("button", { name: "Remove Home" }))
    await waitFor(() => expect(mockRemove).toHaveBeenCalledWith("http://legato", "workspaces", "Home"))
  })

  it("says a list is empty and offers its first entry", async () => {
    show([list({})])
    expect(await screen.findByText("No workspaces.")).toBeTruthy()
    expect(screen.getByRole("button", { name: "New workspace" })).toBeTruthy()
  })

  it("creates a workspace with its name, a colour swatch and a folder", async () => {
    show([list({})])
    fireEvent.click(await screen.findByRole("button", { name: "New workspace" }))
    fireEvent.change(screen.getByRole("textbox", { name: "Name" }), { target: { value: "Side" } })
    fireEvent.click(screen.getByRole("radio", { name: "Teal" }))
    fireEvent.change(screen.getByRole("textbox", { name: "Folder" }), { target: { value: "~/side" } })
    fireEvent.click(screen.getByRole("button", { name: "Save" }))
    await waitFor(() => expect(mockSave).toHaveBeenCalledWith("http://legato", "workspaces", "Side", { color: "#2DD4BF", path: "~/side" }))
    expect(await screen.findByText("Saved. It applies when Legato restarts.")).toBeTruthy()
  })

  it("edits a workspace and keeps the fields the form does not show", async () => {
    show([workspaces])
    fireEvent.click(within(await screen.findByTestId("entry-workspaces-Home")).getByRole("button", { name: "Edit Home" }))
    expect(screen.getByRole("textbox", { name: "Name" })).toBeDisabled()
    fireEvent.click(screen.getByRole("radio", { name: "Blue" }))
    fireEvent.click(screen.getByRole("button", { name: "Save" }))
    await waitFor(() => expect(mockSave).toHaveBeenCalledWith("http://legato", "workspaces", "Home", { color: "#4A9EEF", path: "~/home", owner: "me" }))
  })

  it("edits an entry as YAML in config.yaml's shape, and round-trips a field the form does not know", async () => {
    show([workspaces])
    fireEvent.click(within(await screen.findByTestId("entry-workspaces-Home")).getByRole("button", { name: "Edit Home" }))
    fireEvent.click(screen.getByRole("button", { name: "Edit as YAML" }))
    const yaml = screen.getByRole("textbox", { name: "Workspaces entry as YAML" }) as HTMLTextAreaElement
    expect(yaml.value).toContain("name: Home")
    expect(yaml.value).toContain("owner: me")
    fireEvent.change(yaml, { target: { value: yaml.value.replace("owner: me", "owner: you") } })
    fireEvent.click(screen.getByRole("button", { name: "Back to the form" }))
    expect(screen.getByRole("textbox", { name: "Folder" })).toHaveValue("~/home")
    fireEvent.click(screen.getByRole("button", { name: "Save" }))
    await waitFor(() => expect(mockSave).toHaveBeenCalledWith("http://legato", "workspaces", "Home", { color: "#3ECF8E", path: "~/home", owner: "you" }))
  })

  it("keeps the draft with the server's reason when an entry is refused", async () => {
    mockSave.mockRejectedValue(new Error('workspaces "work": config.yaml already has an entry of this name'))
    show([workspaces])
    fireEvent.click(await screen.findByRole("button", { name: "New workspace" }))
    fireEvent.change(screen.getByRole("textbox", { name: "Name" }), { target: { value: "work" } })
    fireEvent.click(screen.getByRole("button", { name: "Save" }))
    expect((await screen.findByRole("alert")).textContent).toContain("already has an entry")
    expect(screen.getByRole("textbox", { name: "Name" })).toHaveValue("work")
  })

  it("edits a tmux option: its name and value", async () => {
    show([list({ key: "agents.tmux_options", title: "tmux options for agents", mode: "new_agents", name_field: undefined, entries: [{ name: "mouse", value: "on", source: "database", pinned: false }] })])
    expect(await screen.findByText("on")).toBeTruthy()
    fireEvent.click(screen.getByRole("button", { name: "New option" }))
    fireEvent.change(screen.getByRole("textbox", { name: "Option" }), { target: { value: "history-limit" } })
    fireEvent.change(screen.getByRole("textbox", { name: "Value" }), { target: { value: "5000" } })
    fireEvent.click(screen.getByRole("button", { name: "Save" }))
    await waitFor(() => expect(mockSave).toHaveBeenCalledWith("http://legato", "agents.tmux_options", "history-limit", "5000"))
    fireEvent.click(screen.getByRole("button", { name: "Edit mouse" }))
    fireEvent.change(screen.getByRole("textbox", { name: "Value" }), { target: { value: "off" } })
    fireEvent.click(screen.getByRole("button", { name: "Save" }))
    await waitFor(() => expect(mockSave).toHaveBeenCalledWith("http://legato", "agents.tmux_options", "mouse", "off"))
  })

  it("adds a sandbox folder with read or write access", async () => {
    show([list({ key: "agents.sandbox.mounts", title: "Sandbox mounts", name_field: "path" })])
    expect(await screen.findByText("No extra folders.")).toBeTruthy()
    fireEvent.click(screen.getByRole("button", { name: "Add a folder" }))
    fireEvent.change(screen.getByRole("textbox", { name: "Folder" }), { target: { value: "~/notes" } })
    fireEvent.change(screen.getByRole("combobox", { name: "Access" }), { target: { value: "write" } })
    fireEvent.click(screen.getByRole("button", { name: "Save" }))
    await waitFor(() => expect(mockSave).toHaveBeenCalledWith("http://legato", "agents.sandbox.mounts", "~/notes", { access: "write" }))
  })

  it("adds a pull request rule: a repository or pattern and its mode", async () => {
    show([list({ key: "pull_requests.rules", title: "Pull requests", mode: "live", name_field: "match", entries: [{ name: "cpave3/*", value: { mode: "draft" }, source: "database", pinned: false }] })])
    expect(await screen.findByText("cpave3/*")).toBeTruthy()
    fireEvent.click(screen.getByRole("button", { name: "Add a rule" }))
    fireEvent.change(screen.getByRole("textbox", { name: "Repository" }), { target: { value: "cpave3/legato" } })
    expect(within(screen.getByRole("combobox", { name: "Pull requests" })).getAllByRole("option").map((o) => o.textContent)).toEqual(["Choose…", "As drafts", "Ready for review", "The agent chooses"])
    fireEvent.change(screen.getByRole("combobox", { name: "Pull requests" }), { target: { value: "ready" } })
    fireEvent.click(screen.getByRole("button", { name: "Save" }))
    await waitFor(() => expect(mockSave).toHaveBeenCalledWith("http://legato", "pull_requests.rules", "cpave3/legato", { mode: "ready" }))
  })

  it("offers a draft rule to mark a handoff's stack ready once reviewed, and drops it from a rule that is not draft", async () => {
    show([list({ key: "pull_requests.rules", title: "Pull requests", mode: "live", name_field: "match", entries: [] })])
    fireEvent.click(await screen.findByRole("button", { name: "Add a rule" }))
    fireEvent.change(screen.getByRole("textbox", { name: "Repository" }), { target: { value: "acme/*" } })
    expect(screen.queryByRole("checkbox", { name: "Mark ready once reviewed" })).toBeNull()
    fireEvent.change(screen.getByRole("combobox", { name: "Pull requests" }), { target: { value: "draft" } })
    fireEvent.click(screen.getByRole("checkbox", { name: "Mark ready once reviewed" }))
    fireEvent.change(screen.getByRole("combobox", { name: "Pull requests" }), { target: { value: "ready" } })
    expect(screen.queryByRole("checkbox", { name: "Mark ready once reviewed" })).toBeNull()
    fireEvent.click(screen.getByRole("button", { name: "Save" }))
    await waitFor(() => expect(mockSave).toHaveBeenCalledWith("http://legato", "pull_requests.rules", "acme/*", { mode: "ready" }))
  })

  it("saves a draft rule that marks a handoff's stack ready once reviewed", async () => {
    show([list({ key: "pull_requests.rules", title: "Pull requests", mode: "live", name_field: "match", entries: [{ name: "acme/*", value: { mode: "draft" }, source: "database", pinned: false }] })])
    fireEvent.click(await screen.findByRole("button", { name: "Edit acme/*" }))
    const box = screen.getByRole("checkbox", { name: "Mark ready once reviewed" })
    expect(box).not.toBeChecked()
    fireEvent.click(box)
    fireEvent.click(screen.getByRole("button", { name: "Save" }))
    await waitFor(() => expect(mockSave).toHaveBeenCalledWith("http://legato", "pull_requests.rules", "acme/*", { mode: "draft", ready_after_review: true }))
  })

  it("adds and removes a credential source by vault and profile", async () => {
    show([list({ key: "credential_sources", title: "Credential sources", name_field: "vault", entries: [{ name: "Old", value: { profile: "ssh" }, source: "database", pinned: false }] })])
    fireEvent.click(await screen.findByRole("button", { name: "Add a vault" }))
    expect(screen.getByRole("textbox", { name: "Vault" })).toHaveAttribute("placeholder", "op://Work")
    fireEvent.change(screen.getByRole("textbox", { name: "Vault" }), { target: { value: "op://Work" } })
    expect(within(screen.getByRole("combobox", { name: "Profile" })).getAllByRole("option").map((o) => o.textContent)).toEqual(["SSH keys and secrets", "SSH keys only", "Secrets only"])
    fireEvent.change(screen.getByRole("combobox", { name: "Profile" }), { target: { value: "ssh" } })
    fireEvent.click(screen.getByRole("button", { name: "Save" }))
    await waitFor(() => expect(mockSave).toHaveBeenCalledWith("http://legato", "credential_sources", "op://Work", { profile: "ssh" }))
    fireEvent.click(screen.getByRole("button", { name: "Remove Old" }))
    await waitFor(() => expect(mockRemove).toHaveBeenCalledWith("http://legato", "credential_sources", "Old"))
  })

  it("asks which version to use when an entry changes elsewhere while edited", async () => {
    const client = new QueryClient()
    const view = () => <QueryClientProvider client={client}><ListSettings baseUrl="http://legato" /></QueryClientProvider>
    mockFetch.mockResolvedValue([list({ entries: [{ name: "Home", value: { color: "#3ECF8E" }, source: "database", pinned: false }] })])
    const { rerender } = render(view())
    fireEvent.click(await screen.findByRole("button", { name: "Edit Home" }))
    fireEvent.click(screen.getByRole("radio", { name: "Blue" }))
    mockFetch.mockResolvedValue([list({ entries: [{ name: "Home", value: { color: "#EF6461" }, source: "database", pinned: false }] })])
    await client.invalidateQueries()
    rerender(view())
    expect(await screen.findByText("This entry changed elsewhere while you edited it.")).toBeTruthy()
    expect(screen.getByRole("button", { name: "Save" })).toBeDisabled()
    fireEvent.click(screen.getByRole("button", { name: "Keep mine" }))
    fireEvent.click(screen.getByRole("button", { name: "Save" }))
    await waitFor(() => expect(mockSave).toHaveBeenCalledWith("http://legato", "workspaces", "Home", { color: "#4A9EEF" }))
  })

  it("shows only the lists asked for", async () => {
    show([workspaces, list({ key: "credential_sources", title: "Credential sources", name_field: "vault" })], ["credential_sources"])
    expect(await screen.findByText("No credential sources.")).toBeTruthy()
    expect(screen.queryByText("Home")).toBeNull()
  })
})

describe("Agent, role prompt and rule forms", () => {
  beforeEach(() => {
    mockFetch.mockReset()
    mockSave.mockReset(); mockSave.mockResolvedValue(undefined)
    mockRemove.mockReset(); mockRemove.mockResolvedValue(undefined)
  })
  afterEach(() => cleanup())

  const adapters = list({ key: "adapters", title: "Agent tools", name_field: undefined, entries: [
    { name: "claude-code", value: { tiers: { deep: { description: "Hard work", launch_args: ["--model", "opus"] }, quick: {} }, models: ["claude-opus-5-5", "claude-sonnet-5-5"], future: 1 }, source: "database", pinned: false },
    { name: "chimera", value: { launch_args: ["--sandbox"] }, source: "config.yaml", pinned: true },
  ] })

  it("shows each agent as a card with a summary, and a config.yaml agent read-only", async () => {
    show([adapters])
    const claude = await screen.findByTestId("entry-adapters-claude-code")
    expect(claude).toHaveTextContent("Claude Code")
    expect(claude).toHaveTextContent("2 tiers · 2 models · connected to Legato")
    const chimera = screen.getByTestId("entry-adapters-chimera")
    expect(chimera).toHaveTextContent("launch: --sandbox")
    expect(within(chimera).queryByRole("button", { name: /Edit/ })).toBeNull()
  })

  it("adds, edits and removes tiers, and saves models and the bridge switch, keeping what the form does not show", async () => {
    show([adapters])
    fireEvent.click(await screen.findByRole("button", { name: "Edit claude-code" }))
    const tiers = within(screen.getByRole("table", { name: "Tiers" }))
    fireEvent.change(tiers.getByRole("textbox", { name: "Tier 1 description" }), { target: { value: "Design and hard bugs" } })
    fireEvent.click(tiers.getByRole("button", { name: "Remove tier quick" }))
    fireEvent.click(screen.getByRole("button", { name: "Add tier" }))
    fireEvent.change(screen.getByRole("textbox", { name: "Tier 2 name" }), { target: { value: "fast" } })
    const args = screen.getByRole("textbox", { name: "Tier 2 launch arguments" })
    fireEvent.change(args, { target: { value: "--model\n" } })
    expect(args).toHaveValue("--model\n")
    fireEvent.change(args, { target: { value: "--model\nhaiku" } })
    fireEvent.change(screen.getByRole("textbox", { name: "Models in the model menu" }), { target: { value: "claude-opus-5-5" } })
    fireEvent.change(screen.getByRole("combobox", { name: "Connect to Legato" }), { target: { value: "false" } })
    expect(screen.queryByText("Modes")).toBeNull()
    fireEvent.click(screen.getByRole("button", { name: "Save" }))
    await waitFor(() => expect(mockSave).toHaveBeenCalledWith("http://legato", "adapters", "claude-code", {
      tiers: { deep: { description: "Design and hard bugs", launch_args: ["--model", "opus"] }, fast: { launch_args: ["--model", "haiku"] } },
      models: ["claude-opus-5-5"], native_bridge: false, future: 1,
    }))
  })

  it("keeps an argument with a space whole, and refuses two tiers of one name until one is renamed", async () => {
    show([list({ key: "adapters", title: "Agent tools", name_field: undefined, entries: [
      { name: "codex", value: { tiers: { "tier-1": { launch_args: ["--append-system-prompt", "be terse"] }, "tier-3": { description: "Keep me" } } }, source: "database", pinned: false },
    ] })])
    fireEvent.click(await screen.findByRole("button", { name: "Edit codex" }))
    expect(screen.getByRole("textbox", { name: "Tier 1 launch arguments" })).toHaveValue("--append-system-prompt\nbe terse")
    fireEvent.click(screen.getByRole("button", { name: "Add tier" }))
    expect(screen.getByRole("textbox", { name: "Tier 3 name" })).toHaveValue("tier-4")
    fireEvent.change(screen.getByRole("textbox", { name: "Tier 3 name" }), { target: { value: "tier-3" } })
    expect(screen.getByRole("alert")).toHaveTextContent("Two tiers are named tier-3.")
    expect(screen.getByRole("button", { name: "Save" })).toBeDisabled()
    fireEvent.change(screen.getByRole("textbox", { name: "Tier 3 name" }), { target: { value: "fast" } })
    fireEvent.click(screen.getByRole("button", { name: "Save" }))
    await waitFor(() => expect(mockSave).toHaveBeenCalledWith("http://legato", "adapters", "codex", {
      tiers: { "tier-1": { launch_args: ["--append-system-prompt", "be terse"] }, "tier-3": { description: "Keep me" }, fast: {} },
    }))
  })

  it("starts the tiers table again from their value after Use theirs", async () => {
    const client = new QueryClient()
    const codex = (tiers: Record<string, unknown>) => [list({ key: "adapters", title: "Agent tools", name_field: undefined, entries: [{ name: "codex", value: { tiers }, source: "database", pinned: false }] })]
    mockFetch.mockResolvedValue(codex({ fast: { launch_args: ["--old"] } }))
    render(<QueryClientProvider client={client}><ListSettings baseUrl="http://legato" /></QueryClientProvider>)
    fireEvent.click(await screen.findByRole("button", { name: "Edit codex" }))
    fireEvent.change(screen.getByRole("textbox", { name: "Tier 1 description" }), { target: { value: "mine" } })
    mockFetch.mockResolvedValue(codex({ fast: { launch_args: ["--new"] }, deep: { description: "theirs" } }))
    await client.invalidateQueries()
    fireEvent.click(await screen.findByRole("button", { name: "Use theirs" }))
    expect(screen.getByRole("textbox", { name: "Tier 1 launch arguments" })).toHaveValue("--new")
    fireEvent.change(screen.getByRole("textbox", { name: "Tier 2 description" }), { target: { value: "theirs, edited" } })
    fireEvent.click(screen.getByRole("button", { name: "Save" }))
    await waitFor(() => expect(mockSave).toHaveBeenCalledWith("http://legato", "adapters", "codex", {
      tiers: { fast: { launch_args: ["--new"] }, deep: { description: "theirs, edited" } },
    }))
  })

  it("forgets a duplicate tier's refusal once YAML puts the tiers right", async () => {
    show([list({ key: "adapters", title: "Agent tools", name_field: undefined, entries: [{ name: "codex", value: { tiers: { fast: {} } }, source: "database", pinned: false }] })])
    fireEvent.click(await screen.findByRole("button", { name: "Edit codex" }))
    fireEvent.click(screen.getByRole("button", { name: "Add tier" }))
    fireEvent.change(screen.getByRole("textbox", { name: "Tier 2 name" }), { target: { value: "fast" } })
    expect(screen.getByRole("button", { name: "Save" })).toBeDisabled()
    fireEvent.click(screen.getByRole("button", { name: "Edit as YAML" }))
    fireEvent.click(screen.getByRole("button", { name: "Back to the form" }))
    expect(screen.queryByRole("alert")).toBeNull()
    expect(screen.getByRole("button", { name: "Save" })).not.toBeDisabled()
  })

  it("offers Chimera's modes only for Chimera", async () => {
    show([list({ key: "adapters", title: "Agent tools", name_field: undefined })])
    fireEvent.click(await screen.findByRole("button", { name: "Set up an agent" }))
    fireEvent.change(screen.getByRole("textbox", { name: "Agent" }), { target: { value: "chimera" } })
    expect(screen.queryByRole("textbox", { name: "Models in the model menu" })).toBeNull()
    fireEvent.click(screen.getByRole("button", { name: "Add role" }))
    fireEvent.change(screen.getByRole("textbox", { name: "Role 1" }), { target: { value: "worker" } })
    fireEvent.change(screen.getByRole("textbox", { name: "Mode 1" }), { target: { value: "legato-worker" } })
    fireEvent.click(screen.getByRole("button", { name: "Save" }))
    await waitFor(() => expect(mockSave).toHaveBeenCalledWith("http://legato", "adapters", "chimera", { modes: { worker: "legato-worker" } }))
  })

  it("saves a role prompt for an agent", async () => {
    show([list({ key: "swarm.prompts", title: "Swarm role prompts", name_field: undefined })])
    expect(await screen.findByText(/every role uses the built-in prompt/)).toBeTruthy()
    fireEvent.click(screen.getByRole("button", { name: "Add a role prompt" }))
    fireEvent.change(screen.getByRole("textbox", { name: "Role" }), { target: { value: "backend" } })
    fireEvent.change(screen.getByRole("textbox", { name: "Codex's prompt" }), { target: { value: "You own the API." } })
    fireEvent.click(screen.getByRole("button", { name: "Save" }))
    await waitFor(() => expect(mockSave).toHaveBeenCalledWith("http://legato", "swarm.prompts", "backend", { codex: "You own the API." }))
  })

  it("keeps the Reeve's rules in order, says the last match wins, and sends a change for signature", async () => {
    mockSave.mockResolvedValue({ status: "needs_signature" })
    show([list({ key: "reeve.rules", title: "The Reeve's rules", mode: "live", name_field: "pattern", entries: [
      { name: "task.*", value: { level: "auto" }, source: "config.yaml", pinned: true },
      { name: "config.*", value: { level: "signed" }, source: "database", pinned: false },
    ] })])
    expect(await screen.findByText(/the last that matches wins/)).toBeTruthy()
    const items = within(screen.getByRole("list", { name: "The Reeve's rules" })).getAllByRole("listitem")
    expect(items.map((i) => i.getAttribute("data-testid"))).toEqual(["entry-reeve.rules-task.*", "entry-reeve.rules-config.*"])
    expect(items[0]).toHaveTextContent("1task.*")
    fireEvent.click(screen.getByRole("button", { name: "Edit config.*" }))
    fireEvent.change(screen.getByRole("combobox", { name: "Needs" }), { target: { value: "approve" } })
    fireEvent.click(screen.getByRole("button", { name: "Save" }))
    await waitFor(() => expect(mockSave).toHaveBeenCalledWith("http://legato", "reeve.rules", "config.*", { level: "approve" }))
    expect(await screen.findByText(/Awaiting approval/)).toBeTruthy()
  })
})
