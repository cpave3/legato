import "@testing-library/jest-dom/vitest"
import { afterEach, beforeEach, expect, it, vi } from "vitest"
import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react"
import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { ListSettings } from "../settings/ListSettings"
import { queryKeys } from "../../lib/query"
import type { ConfigList } from "../../lib/configSettings"

const { fetchLists, save, remove } = vi.hoisted(() => ({ fetchLists: vi.fn(), save: vi.fn(), remove: vi.fn() }))
vi.mock("../../lib/configSettings", () => ({
  fetchConfigLists: fetchLists, saveConfigListEntry: save, removeConfigListEntry: remove,
}))
const workspace: ConfigList = { key: "workspaces", title: "Workspaces", class: "change", mode: "restart", name_field: "name", entries: [] }
const tools: ConfigList = { ...workspace, key: "adapters", title: "Agent tools" }
function show(lists?: string[], client = new QueryClient({ defaultOptions: { queries: { retry: false } } })) {
  return { client, ...render(<QueryClientProvider client={client}><ListSettings baseUrl="http://test" lists={lists} /></QueryClientProvider>) }
}
beforeEach(() => { fetchLists.mockReset().mockResolvedValue([workspace, tools]); save.mockReset().mockResolvedValue(undefined); remove.mockReset().mockResolvedValue(undefined) })
afterEach(cleanup)

it("filters by list key, defaults to all, and renders nothing for an empty owner", async () => {
  const view = show(["adapters"])
  await screen.findByRole("button", { name: "Add to agent tools" })
  expect(screen.queryByRole("button", { name: "Add to workspaces" })).toBeNull()
  view.unmount()
  const empty = show([])
  expect(empty.container).toBeEmptyDOMElement()
  empty.unmount()
  show()
  await screen.findByRole("button", { name: "Add to workspaces" })
  expect(screen.getByRole("button", { name: "Add to agent tools" })).toBeInTheDocument()
})

it("shows loading, retries errors, and keeps cached lists visible after a failed refresh", async () => {
  fetchLists.mockReturnValueOnce(new Promise(() => {}))
  const loading = show(["workspaces"])
  expect(screen.getByRole("status")).toHaveTextContent("Loading")
  loading.unmount()
  fetchLists.mockRejectedValueOnce(new Error("Network unavailable"))
  const view = show(["workspaces"])
  expect(await screen.findByRole("alert")).toHaveTextContent("Network unavailable")
  fireEvent.click(screen.getByRole("button", { name: "Retry" }))
  await screen.findByRole("button", { name: "Add to workspaces" })
  fetchLists.mockRejectedValueOnce(new Error("Refresh failed"))
  await view.client.invalidateQueries({ queryKey: queryKeys.configLists("http://test") })
  expect(await screen.findByRole("alert")).toHaveTextContent("Showing previously loaded lists")
  expect(screen.getByRole("button", { name: "Add to workspaces" })).toBeInTheDocument()
})

it("edits workspace fields and advanced JSON without losing unknown fields, and cancels drafts", async () => {
  fetchLists.mockResolvedValue([{ ...workspace, entries: [{ name: "Home", source: "database", pinned: false, value: { color: "red", path: "/old", future: { enabled: true } } }] }])
  show(["workspaces"])
  fireEvent.click(await screen.findByRole("button", { name: "Edit" }))
  fireEvent.change(screen.getByRole("textbox", { name: "Color" }), { target: { value: "blue" } })
  const json = screen.getByRole("textbox", { name: "Workspaces fields" })
  expect(JSON.parse((json as HTMLTextAreaElement).value)).toEqual({ color: "blue", path: "/old", future: { enabled: true } })
  fireEvent.change(json, { target: { value: '{"color":"green","path":"/new","future":{"enabled":false}}' } })
  expect(screen.getByRole("textbox", { name: "Path" })).toHaveValue("/new")
  fireEvent.click(screen.getByRole("button", { name: "Save" }))
  await waitFor(() => expect(save).toHaveBeenCalledWith("http://test", "workspaces", "Home", { color: "green", path: "/new", future: { enabled: false } }))
  fireEvent.click(await screen.findByRole("button", { name: "Edit" }))
  fireEvent.change(screen.getByRole("textbox", { name: "Color" }), { target: { value: "discard" } })
  fireEvent.click(screen.getByRole("button", { name: "Cancel" }))
  fireEvent.click(screen.getByRole("button", { name: "Edit" }))
  expect(screen.getByRole("textbox", { name: "Color" })).toHaveValue("red")
})

it.each([
  { key: "adapters", title: "Agent tools", field: "Launch arguments (one per line)", before: { launch_args: ["--old"], tiers: { fast: { launch_args: ["--fast"] } } }, text: "--model\nmodel with spaces", expected: { launch_args: ["--model", "model with spaces"], tiers: { fast: { launch_args: ["--fast"] } } } },
  { key: "credentials", title: "Credentials", field: "Secret reference", before: { secret: "op://vault/item/password", policy: [{ operation: "read" }], settings: { upstream: "https://example.com" } }, text: "legato://mqtt", expected: { secret: "legato://mqtt", policy: [{ operation: "read" }], settings: { upstream: "https://example.com" } } },
  { key: "credential_sources", title: "Credential sources", field: "Profile", before: { profile: "ssh", policy: [] }, text: "secret", expected: { profile: "secret", policy: [] } },
  { key: "agents.tmux_options", title: "tmux options", field: "Value", before: "on", text: "off", expected: "off" },
])("saves $key using its real JSON shape", async ({ key, title, field, before, text, expected }) => {
  fetchLists.mockResolvedValue([{ ...workspace, key, title, entries: [{ name: "example", value: before, pinned: false, source: "database" }] }])
  show([key])
  fireEvent.click(await screen.findByRole("button", { name: "Edit" }))
  fireEvent.change(screen.getByRole("textbox", { name: field }), { target: { value: text } })
  fireEvent.click(screen.getByRole("button", { name: "Save" }))
  await waitFor(() => expect(save).toHaveBeenCalledWith("http://test", key, "example", expected))
})

it("keeps adapter native bridge unset, false, and true distinct", async () => {
  fetchLists.mockResolvedValue([{ ...workspace, key: "adapters", title: "Agent tools", entries: [] }])
  show(["adapters"])
  fireEvent.click(await screen.findByRole("button", { name: "Add to agent tools" }))
  fireEvent.change(screen.getByRole("textbox", { name: "Agent tools name" }), { target: { value: "codex" } })
  const bridge = screen.getByRole("combobox", { name: "Native bridge" })
  expect(bridge).toHaveValue("")
  fireEvent.change(bridge, { target: { value: "false" } })
  expect(JSON.parse((screen.getByRole("textbox", { name: "Agent tools fields" }) as HTMLTextAreaElement).value)).toEqual({ native_bridge: false })
  fireEvent.change(bridge, { target: { value: "true" } })
  fireEvent.change(bridge, { target: { value: "" } })
  fireEvent.click(screen.getByRole("button", { name: "Save" }))
  await waitFor(() => expect(save).toHaveBeenCalledWith("http://test", "adapters", "codex", {}))
})

it("adds a sandbox mount by path with access fields", async () => {
  fetchLists.mockResolvedValue([{ ...workspace, key: "agents.sandbox.mounts", title: "Sandbox mounts", name_field: "path", entries: [] }])
  show(["agents.sandbox.mounts"])
  fireEvent.click(await screen.findByRole("button", { name: "Add to sandbox mounts" }))
  expect(screen.getByText("Path")).toBeInTheDocument()
  fireEvent.change(screen.getByRole("textbox", { name: "Sandbox mounts name" }), { target: { value: "/srv/data" } })
  fireEvent.change(screen.getByRole("textbox", { name: "Access" }), { target: { value: "read" } })
  fireEvent.click(screen.getByRole("button", { name: "Save" }))
  await waitFor(() => expect(save).toHaveBeenCalledWith("http://test", "agents.sandbox.mounts", "/srv/data", { access: "read" }))
})

it("blocks invalid JSON without losing the draft and prevents duplicate saves while pending", async () => {
  save.mockImplementation(() => new Promise(() => {}))
  show(["workspaces"])
  fireEvent.click(await screen.findByRole("button", { name: "Add to workspaces" }))
  fireEvent.change(screen.getByRole("textbox", { name: "Workspaces name" }), { target: { value: "Home" } })
  const json = screen.getByRole("textbox", { name: "Workspaces fields" })
  fireEvent.change(json, { target: { value: '{"future":' } })
  fireEvent.click(screen.getByRole("button", { name: "Save" }))
  expect(screen.getByRole("alert")).toHaveTextContent("not valid JSON")
  expect(json).toHaveValue('{"future":')
  expect(save).not.toHaveBeenCalled()
  expect(screen.getByRole("textbox", { name: "Color" })).toBeDisabled()
  fireEvent.change(json, { target: { value: '{"future":42}' } })
  fireEvent.click(screen.getByRole("button", { name: "Save" }))
  expect(await screen.findByRole("button", { name: "Saving…" })).toBeDisabled()
  expect(save).toHaveBeenCalledTimes(1)
})

it("round-trips unknown list shapes through advanced JSON and keeps rejected drafts", async () => {
  fetchLists.mockResolvedValue([{ ...workspace, key: "future.list", title: "Future list", entries: [{ name: "example", value: ["a", "b"], source: "database", pinned: false }] }])
  save.mockRejectedValueOnce(new Error("This change requires approval"))
  show(["future.list"])
  fireEvent.click(await screen.findByRole("button", { name: "Edit" }))
  expect(screen.getByRole("textbox", { name: "Future list fields" })).toHaveValue('[\n  "a",\n  "b"\n]')
  fireEvent.click(screen.getByRole("button", { name: "Save" }))
  expect(await screen.findByRole("alert")).toHaveTextContent("requires approval")
  expect(save).toHaveBeenCalledWith("http://test", "future.list", "example", ["a", "b"])
  fireEvent.click(screen.getByRole("button", { name: "Save" }))
  await waitFor(() => expect(screen.queryByRole("textbox", { name: "Future list fields" })).toBeNull())
})


it("keeps an entry draft when its server value changes and asks which version to use", async () => {
  const entry = (path: string) => ({ ...workspace, entries: [{ name: "Home", source: "database", pinned: false, value: { path } }] })
  fetchLists.mockResolvedValue([entry("/old")])
  const { client } = show(["workspaces"])
  fireEvent.click(await screen.findByRole("button", { name: "Edit" }))
  fireEvent.change(screen.getByRole("textbox", { name: "Path" }), { target: { value: "/mine" } })
  await act(async () => { client.setQueryData(queryKeys.configLists("http://test"), [entry("/elsewhere")]) })
  expect(await screen.findByText("This entry changed elsewhere.")).toBeVisible()
  expect(screen.getByRole("textbox", { name: "Path" })).toHaveValue("/mine")
  expect(screen.getByRole("button", { name: "Save" })).toBeDisabled()
  fireEvent.click(screen.getByRole("button", { name: "Keep my edit" }))
  fireEvent.click(screen.getByRole("button", { name: "Save" }))
  await waitFor(() => expect(save).toHaveBeenCalledWith("http://test", "workspaces", "Home", { path: "/mine" }))
})

