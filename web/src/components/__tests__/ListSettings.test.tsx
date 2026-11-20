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

function show(lists: ConfigList[], only?: string[]) {
  mockFetch.mockResolvedValue(lists)
  return render(<QueryClientProvider client={new QueryClient()}><ListSettings baseUrl="http://legato" lists={only} /></QueryClientProvider>)
}

describe("List forms", () => {
  beforeEach(() => {
    mockFetch.mockReset()
    mockSave.mockReset(); mockSave.mockResolvedValue(undefined)
    mockRemove.mockReset(); mockRemove.mockResolvedValue(undefined)
  })
  afterEach(() => cleanup())

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

  it("adds and removes a credential source by vault and profile", async () => {
    show([list({ key: "credential_sources", title: "Credential sources", name_field: "vault", entries: [{ name: "Old", value: { profile: "ssh" }, source: "database", pinned: false }] })])
    fireEvent.click(await screen.findByRole("button", { name: "Add a vault" }))
    fireEvent.change(screen.getByRole("textbox", { name: "Vault" }), { target: { value: "Work" } })
    fireEvent.change(screen.getByRole("textbox", { name: "Profile" }), { target: { value: "api-key" } })
    fireEvent.click(screen.getByRole("button", { name: "Save" }))
    await waitFor(() => expect(mockSave).toHaveBeenCalledWith("http://legato", "credential_sources", "Work", { profile: "api-key" }))
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
