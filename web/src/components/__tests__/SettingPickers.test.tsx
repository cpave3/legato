import "@testing-library/jest-dom/vitest"
import { describe, it, expect, vi, afterEach, beforeEach } from "vitest"
import { render, screen, cleanup, fireEvent, waitFor, within } from "@testing-library/react"
import { QueryClient, QueryClientProvider } from "@tanstack/react-query"

const mockSet = vi.fn()
const mockReset = vi.fn()
vi.mock("../../lib/configSettings", async (importActual) => ({
  ...(await importActual<typeof import("../../lib/configSettings")>()),
  setConfigSetting: (...a: unknown[]) => mockSet(...a),
  resetConfigSetting: (...a: unknown[]) => mockReset(...a),
  fetchMovable: async () => ({ settings: [], entries: [] }),
}))

import { RegistrySettings } from "../settings/RegistrySettings"
import { PickerContext, pickerData } from "../../lib/settingPickers"
import type { ConfigList, ConfigSetting } from "../../lib/configSettings"

const setting = (over: Partial<ConfigSetting>): ConfigSetting => ({
  key: "x", title: "X", kind: "string", class: "change", mode: "restart", value: "", default: "", source: "default", pinned: false, ...over,
})

const lists: ConfigList[] = [
  { key: "adapters", title: "Agent tools", class: "change", mode: "restart", entries: [
    { name: "claude-code", value: { tiers: { deep: {}, quick: {} }, models: ["claude-opus-5-5"] }, source: "config.yaml", pinned: true },
    { name: "codex", value: { tiers: { fast: {} } }, source: "database", pinned: false },
  ] },
  { key: "board.columns", title: "Board columns", class: "change", mode: "restart", entries: ["Backlog", "Doing", "Review", "Done"].map((name) => ({ name, value: {}, source: "config.yaml" as const, pinned: true })) },
  { key: "workspaces", title: "Workspaces", class: "change", mode: "restart", entries: ["infra", "web"].map((name) => ({ name, value: {}, source: "database" as const, pinned: false })) },
]

function show(settings: ConfigSetting[]) {
  const data = pickerData(lists, settings, { adapters: ["claude-code", "codex"], default: "claude-code" }, ["3-tier-review", "tdd"])
  return render(<QueryClientProvider client={new QueryClient()}><PickerContext.Provider value={data}>
    <RegistrySettings baseUrl="http://legato" settings={settings} prefixes={settings.map((s) => s.key)} />
  </PickerContext.Provider></QueryClientProvider>)
}

const options = (name: string) => within(screen.getByRole("combobox", { name })).getAllByRole("option").map((o) => o.textContent)

describe("Settings pickers", () => {
  beforeEach(() => { mockSet.mockReset(); mockReset.mockReset(); mockSet.mockResolvedValue({}); mockReset.mockResolvedValue({}) })
  afterEach(() => cleanup())

  it("chooses an agent and its model or tier side by side, as one setting in two keys", async () => {
    show([
      setting({ key: "swarm.worker.agent", title: "Workers", default_label: "claude-code" }),
      setting({ key: "swarm.worker.model", title: "Workers' model", default_label: "The tool's default" }),
    ])
    expect(options("Workers agent")).toEqual(["Default agent · Claude Code", "Claude Code", "Codex"])
    expect(options("Workers model or tier")).toEqual(["The tool's default", "Tier: deep", "Tier: quick", "claude-opus-5-5"])
    fireEvent.change(screen.getByRole("combobox", { name: "Workers model or tier" }), { target: { value: "tier:deep" } })
    await waitFor(() => expect(mockSet).toHaveBeenCalledWith("http://legato", "swarm.worker.model", "deep"))
    fireEvent.change(screen.getByRole("combobox", { name: "Workers agent" }), { target: { value: "codex" } })
    await waitFor(() => expect(mockSet).toHaveBeenCalledWith("http://legato", "swarm.worker.agent", "codex"))
    expect(document.getElementById("setting-swarm.worker.model")).toBeTruthy()
  })

  it("puts a reviewer's model or tier in the key that holds it, clearing the other", async () => {
    show([
      setting({ key: "review_passes.reviewer.agent", title: "Reviewer", value: "claude-code", source: "database" }),
      setting({ key: "review_passes.reviewer.model", title: "Reviewer's model", value: "claude-opus-5-5", source: "database" }),
      setting({ key: "review_passes.reviewer.tier", title: "Reviewer's tier" }),
    ])
    fireEvent.change(screen.getByRole("combobox", { name: "Reviewer model or tier" }), { target: { value: "tier:quick" } })
    await waitFor(() => expect(mockSet).toHaveBeenCalledWith("http://legato", "review_passes.reviewer.tier", "quick"))
    // The model is cleared first: the server refuses a model beside a tier.
    expect(mockReset).toHaveBeenCalledWith("http://legato", "review_passes.reviewer.model")
    expect(mockReset.mock.invocationCallOrder[0]).toBeLessThan(mockSet.mock.invocationCallOrder[0])
  })

  it("saves nothing more when clearing the old choice is refused, and says why on the row", async () => {
    mockReset.mockRejectedValue(new Error("review_passes.reviewer.model is pinned by config.yaml"))
    show([
      setting({ key: "review_passes.reviewer.agent", title: "Reviewer", value: "claude-code", source: "database" }),
      setting({ key: "review_passes.reviewer.model", title: "Reviewer's model", value: "claude-opus-5-5", source: "database" }),
      setting({ key: "review_passes.reviewer.tier", title: "Reviewer's tier" }),
    ])
    fireEvent.change(screen.getByRole("combobox", { name: "Reviewer model or tier" }), { target: { value: "tier:quick" } })
    const row = within(screen.getByTestId("setting-review_passes.reviewer.agent"))
    expect(await row.findByText(/pinned by config.yaml/)).toBeTruthy()
    expect(mockSet).not.toHaveBeenCalled()
  })

  it("offers the workers' agent's tiers to a conductor that takes the workers' agent", async () => {
    show([
      setting({ key: "swarm.worker.agent", title: "Workers", value: "codex", source: "database" }),
      setting({ key: "swarm.worker.model", title: "Workers' model" }),
      setting({ key: "swarm.conductor.agent", title: "Conductor", default_label: "Workers' agent" }),
      setting({ key: "swarm.conductor.model", title: "Conductor's model", default_label: "The tool's default" }),
    ])
    expect(options("Conductor agent")[0]).toBe("Workers' agent · Codex")
    expect(options("Conductor model or tier")).toEqual(["The tool's default", "Tier: fast"])
  })

  it("clears a tier of the old agent when the agent changes", async () => {
    show([
      setting({ key: "learning.curator_adapter", title: "Curator", value: "claude-code", source: "database" }),
      setting({ key: "learning.curator_tier", title: "Curator's tier", value: "deep", source: "database" }),
    ])
    expect(options("Curator model or tier")).toEqual(["None", "Tier: deep", "Tier: quick"].slice(0, 0).concat(["The tool's default", "Tier: deep", "Tier: quick"]).slice(0, 3).length === 3 ? options("Curator model or tier") : [])
    fireEvent.change(screen.getByRole("combobox", { name: "Curator agent" }), { target: { value: "codex" } })
    await waitFor(() => expect(mockReset).toHaveBeenCalledWith("http://legato", "learning.curator_tier"))
  })

  it("shows a saved agent this Legato lacks as an error, not a blank", () => {
    show([setting({ key: "integrations.argus.agent", title: "Agent for its briefs", value: "codex-old", source: "database" })])
    const menu = screen.getByRole("combobox", { name: "Agent for its briefs" }) as HTMLSelectElement
    expect(menu.value).toBe("codex-old")
    expect(screen.getByRole("alert")).toHaveTextContent("codex-old isn't an agent here")
  })

  it("chooses a column, and shows one the board lacks as an error", async () => {
    show([
      setting({ key: "review_passes.review_column", title: "Review column", value: "Review", default: "Review" }),
      setting({ key: "burnlite.columns.ready", title: "Ready column", value: "Todo", source: "database" }),
    ])
    expect(options("Review column")).toEqual(["Backlog", "Doing", "Review", "Done"])
    fireEvent.change(screen.getByRole("combobox", { name: "Review column" }), { target: { value: "Doing" } })
    await waitFor(() => expect(mockSet).toHaveBeenCalledWith("http://legato", "review_passes.review_column", "Doing"))
    expect(screen.getByRole("alert")).toHaveTextContent("Todo isn't a column here")
  })

  it("chooses several columns", async () => {
    show([setting({ key: "learning.transition_columns", title: "Reflect when a card moves to", kind: "strings", value: ["Done"], source: "database" })])
    expect(options("Add to Reflect when a card moves to")).toEqual(["Add…", "Backlog", "Doing", "Review"])
    fireEvent.change(screen.getByRole("combobox", { name: "Add to Reflect when a card moves to" }), { target: { value: "Review" } })
    await waitFor(() => expect(mockSet).toHaveBeenCalledWith("http://legato", "learning.transition_columns", ["Done", "Review"]))
  })

  it("chooses a workspace, an imported task's with Same as new tasks", async () => {
    show([
      setting({ key: "default_workspace", title: "New tasks go to" }),
      setting({ key: "default_import_workspace", title: "Imported tasks go to", default_label: "Same as new tasks" }),
    ])
    expect(options("New tasks go to")).toEqual(["None", "infra", "web"])
    expect(options("Imported tasks go to")).toEqual(["Same as new tasks", "infra", "web"])
    fireEvent.change(screen.getByRole("combobox", { name: "Imported tasks go to" }), { target: { value: "web" } })
    await waitFor(() => expect(mockSet).toHaveBeenCalledWith("http://legato", "default_import_workspace", "web"))
  })

  it("chooses a skill", async () => {
    show([setting({ key: "review_passes.reviewer.skill", title: "Reviewer's skill" })])
    expect(options("Reviewer's skill")).toEqual(["None", "3-tier-review", "tdd"])
    fireEvent.change(screen.getByRole("combobox", { name: "Reviewer's skill" }), { target: { value: "tdd" } })
    await waitFor(() => expect(mockSet).toHaveBeenCalledWith("http://legato", "review_passes.reviewer.skill", "tdd"))
  })

  it("edits a list of words as tags: Enter adds, × removes", async () => {
    show([setting({ key: "groups.defaults", title: "Agent groups", kind: "strings", value: ["review"], source: "database" })])
    const input = screen.getByRole("textbox", { name: "Add to Agent groups" })
    fireEvent.change(input, { target: { value: "infra" } })
    fireEvent.keyDown(input, { key: "Enter" })
    await waitFor(() => expect(mockSet).toHaveBeenCalledWith("http://legato", "groups.defaults", ["review", "infra"]))
    fireEvent.click(screen.getByRole("button", { name: "Remove review" }))
    await waitFor(() => expect(mockSet).toHaveBeenCalledWith("http://legato", "groups.defaults", []))
  })

  it("saves a one-key model or tier by its name, reads it back as whichever it is, and clears it", async () => {
    show([
      setting({ key: "reeve.agent", title: "Reeve", value: "claude-code", source: "database" }),
      setting({ key: "reeve.model", title: "Reeve's model", value: "deep", source: "database" }),
    ])
    const second = screen.getByRole("combobox", { name: "Reeve model or tier" }) as HTMLSelectElement
    expect(second.value).toBe("tier:deep")
    expect(options("Reeve model or tier")[0]).toBe("Default model")
    fireEvent.change(second, { target: { value: "model:claude-opus-5-5" } })
    await waitFor(() => expect(mockSet).toHaveBeenCalledWith("http://legato", "reeve.model", "claude-opus-5-5"))
    fireEvent.change(second, { target: { value: "" } })
    await waitFor(() => expect(mockReset).toHaveBeenCalledWith("http://legato", "reeve.model"))
  })

  it("reads a one-key model as a model, and names one the agent lacks", () => {
    show([
      setting({ key: "reeve.agent", title: "Reeve", value: "codex", source: "database" }),
      setting({ key: "reeve.model", title: "Reeve's model", value: "gpt-old", source: "database" }),
    ])
    expect((screen.getByRole("combobox", { name: "Reeve model or tier" }) as HTMLSelectElement).value).toBe("model:gpt-old")
    expect(screen.getByRole("alert")).toHaveTextContent("gpt-old isn't a model or tier of Codex")
  })

  it("clears both keys of a reviewer when its model or tier goes back to the default", async () => {
    show([
      setting({ key: "review_passes.reviewer.agent", title: "Reviewer", value: "claude-code", source: "database" }),
      setting({ key: "review_passes.reviewer.model", title: "Reviewer's model" }),
      setting({ key: "review_passes.reviewer.tier", title: "Reviewer's tier", value: "deep", source: "database" }),
    ])
    const second = screen.getByRole("combobox", { name: "Reviewer model or tier" }) as HTMLSelectElement
    expect(second.value).toBe("tier:deep")
    expect(document.getElementById("setting-review_passes.reviewer.tier")).toBeTruthy()
    fireEvent.change(second, { target: { value: "model:claude-opus-5-5" } })
    await waitFor(() => expect(mockSet).toHaveBeenCalledWith("http://legato", "review_passes.reviewer.model", "claude-opus-5-5"))
    expect(mockReset).toHaveBeenCalledWith("http://legato", "review_passes.reviewer.tier")
    mockSet.mockClear(); mockReset.mockClear()
    fireEvent.change(second, { target: { value: "" } })
    await waitFor(() => expect(mockReset).toHaveBeenCalledWith("http://legato", "review_passes.reviewer.tier"))
    expect(mockSet).not.toHaveBeenCalled()
  })

  it("keeps a tier the new agent has too, and clears the agent back to the default", async () => {
    show([
      setting({ key: "swarm.worker.agent", title: "Workers", value: "codex", source: "database" }),
      setting({ key: "swarm.worker.model", title: "Workers' model", value: "deep", source: "database" }),
    ])
    fireEvent.change(screen.getByRole("combobox", { name: "Workers agent" }), { target: { value: "" } })
    await waitFor(() => expect(mockReset).toHaveBeenCalledWith("http://legato", "swarm.worker.agent"))
    expect(mockReset).not.toHaveBeenCalledWith("http://legato", "swarm.worker.model")
  })

  it("changes no agent when clearing its old tier is refused", async () => {
    mockReset.mockRejectedValue(new Error("pinned"))
    show([
      setting({ key: "learning.curator_adapter", title: "Curator", value: "claude-code", source: "database" }),
      setting({ key: "learning.curator_tier", title: "Curator's tier", value: "deep", source: "database" }),
    ])
    fireEvent.change(screen.getByRole("combobox", { name: "Curator agent" }), { target: { value: "codex" } })
    await waitFor(() => expect(mockReset).toHaveBeenCalledWith("http://legato", "learning.curator_tier"))
    expect(mockSet).not.toHaveBeenCalled()
  })

  it("holds a pinned model or tier, and names an agent setting's own unset meaning", () => {
    show([
      setting({ key: "swarm.worker.agent", title: "Workers", default_label: "Ask each time" }),
      setting({ key: "swarm.worker.model", title: "Workers' model", pinned: true, value: "quick", source: "config.yaml" }),
      setting({ key: "integrations.argus.agent", title: "Briefs", default_label: "Same as the default agent" }),
    ])
    expect(screen.getByRole("combobox", { name: "Workers model or tier" })).toBeDisabled()
    expect(screen.getByRole("combobox", { name: "Workers agent" })).not.toBeDisabled()
    expect(options("Workers agent")[0]).toBe("Ask each time")
    expect(options("Briefs")[0]).toBe("Default agent · Claude Code")
  })

  it("adds no tag twice or blank, and shows none when it cannot change", async () => {
    show([setting({ key: "groups.defaults", title: "Agent groups", kind: "strings", value: ["review"], source: "database" })])
    const input = screen.getByRole("textbox", { name: "Add to Agent groups" })
    fireEvent.change(input, { target: { value: " review " } })
    fireEvent.keyDown(input, { key: "Enter" })
    fireEvent.change(input, { target: { value: "  " } })
    fireEvent.keyDown(input, { key: "Enter" })
    fireEvent.keyDown(input, { key: "a" })
    expect(mockSet).not.toHaveBeenCalled()
    cleanup()
    show([setting({ key: "groups.defaults", title: "Agent groups", kind: "strings", value: [], pinned: true, source: "config.yaml" })])
    expect(screen.getByText("None")).toBeTruthy()
    expect(screen.queryByRole("textbox", { name: "Add to Agent groups" })).toBeNull()
  })

  it("marks chosen columns the board lacks", () => {
    show([setting({ key: "learning.transition_columns", title: "Reflect on", kind: "strings", value: ["Gone", "Done"], source: "database" })])
    expect(screen.getByRole("alert")).toHaveTextContent("Gone isn't a column here")
    expect(screen.getByText("Gone").className).toContain("border-lw-error")
  })
})
