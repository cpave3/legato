import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react"
import { Link, MemoryRouter, Route, Routes } from "react-router-dom"
import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { afterEach, beforeEach, expect, it, vi } from "vitest"
import { WorkflowEditPage } from "../WorkflowEditPage"

const { apiFetch } = vi.hoisted(() => ({ apiFetch: vi.fn() }))
vi.mock("../../lib/api", () => ({ apiFetch }))
vi.mock("../../hooks/useServer", () => ({ useServer: () => ({ baseUrl: "" }) }))
vi.mock("../../hooks/useWorkflow", () => ({
  useWorkflows: () => ({ data: { definitions: [], errors: null, folders: [{ id: "global", label: "Your workflows", path: "/g" }, { id: "project", label: "This project", path: "/p" }] }, refresh: vi.fn() }),
}))
// The editor is CodeMirror in its own chunk; here it is a text box.
vi.mock("../../components/workflow-editor/LazyWorkflowYamlEditor", () => ({
  // Like CodeMirror's keymap, Ctrl+Z and Ctrl+Shift+Z go to the page.
  LazyWorkflowYamlEditor: ({ value, onChange, readOnly, onUndo, onRedo }: { value: string; onChange: (v: string) => void; readOnly?: boolean; onUndo?: () => void; onRedo?: () => void }) =>
    <textarea aria-label="Workflow YAML" value={value} readOnly={readOnly} onChange={event => onChange(event.target.value)}
      onKeyDown={event => { if (event.ctrlKey && event.key.toLowerCase() === "z") (event.shiftKey ? onRedo : onUndo)?.() }} />,
}))
vi.mock("../../components/workflow-graph/LazyWorkflowGraph", () => ({ LazyWorkflowGraph: () => <div>graph</div> }))

const json = (body: unknown, status = 200) => Promise.resolve(new Response(JSON.stringify(body), { status }))
const fileText = "# kept\nschema_version: 1\nname: review\ntitle: Review\nsteps:\n  - {id: a, title: A, type: agent.run, with: {prompt: hi}}\n"
const badType = { severity: "error", message: "step \"a\" has unsupported type \"agent.nope\"", path: "steps[0].type", line: 6, column: 30, end_line: 6, end_column: 40 }

let puts: { yaml: string; digest: string; confirm_triggers: boolean }[]
let creates: { yaml: string; folder: string }[]
let putReply: (body: { confirm_triggers: boolean }) => Promise<Response>

let fileReply: () => Promise<Response>

beforeEach(() => {
  localStorage.clear()
  fileReply = () => json({ name: "review", yaml: fileText, path: "/g/review.yaml", folder: "global", digest: "sha256:old", editable: true })
  localStorage.setItem("legato:workflow-editor-mode", "yaml")
  puts = []
  creates = []
  putReply = () => json({ name: "review", path: "/g/review.yaml", folder: "global", digest: "sha256:new", diagnostics: [] })
  apiFetch.mockReset()
  apiFetch.mockImplementation((_: string, path: string, init?: RequestInit) => {
    const body = init?.body ? JSON.parse(String(init.body)) : {}
    if (path === "/api/workflow-files/check") {
      const bad = String(body.yaml).includes("agent.nope")
      return json({ definition: bad ? null : { name: "review" }, graph: bad ? null : { nodes: [], edges: [] }, diagnostics: bad ? [badType] : [] })
    }
    if (path === "/api/workflows/review/file" && !init?.method) return fileReply()
    if (path === "/api/workflows/review/file" && init?.method === "PUT") { puts.push(body); return putReply(body) }
    if (path === "/api/workflow-files" && init?.method === "POST") { creates.push(body); return json({ name: "my-workflow", path: "/p/my-workflow.workflow.yaml", folder: body.folder, digest: "sha256:x", diagnostics: [] }, 201) }
    return json({}, 404)
  })
})
afterEach(cleanup)

function renderAt(path: string) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(<QueryClientProvider client={client}><MemoryRouter initialEntries={[path]}>
    <Routes>
      <Route path="/workflows/new" element={<WorkflowEditPage mode="new" />} />
      <Route path="/workflows/:name/edit" element={<WorkflowEditPage mode="edit" />} />
      <Route path="/workflows/:name" element={<p>workflow page</p>} />
      <Route path="/board" element={<p>board page</p>} />
    </Routes>
    <Link to="/board">Board</Link>
  </MemoryRouter></QueryClientProvider>)
}

const editor = () => screen.getByRole("textbox", { name: "Workflow YAML" }) as HTMLTextAreaElement
const type = (text: string) => fireEvent.change(editor(), { target: { value: text } })

it("creates a new workflow from the starter, in the folder chosen", async () => {
  renderAt("/workflows/new")
  expect(editor().value).toContain("schema_version: 1")
  const create = screen.getByRole("button", { name: "Create" })
  expect(create).toHaveProperty("disabled", true)
  await waitFor(() => expect(create).toHaveProperty("disabled", false))
  fireEvent.change(screen.getByRole("combobox", { name: "Folder" }), { target: { value: "project" } })
  fireEvent.click(create)
  expect(await screen.findByText("workflow page")).toBeTruthy()
  expect(creates).toEqual([{ yaml: expect.stringContaining("name: my-workflow"), folder: "project", confirm_triggers: false }])
})

it("keeps Save off while the server finds an error, and lists it by line", async () => {
  renderAt("/workflows/review/edit")
  await waitFor(() => expect(editor().value).toBe(fileText))
  type(fileText.replace("agent.run", "agent.nope"))
  const problems = await screen.findByRole("list", { name: "Problems" })
  expect(within(problems).getByText(/unsupported type/).textContent).toContain("6:30")
  expect(screen.getByRole("button", { name: "Save" })).toHaveProperty("disabled", true)
  type(fileText.replace("Review", "Review it"))
  await waitFor(() => expect(screen.getByRole("button", { name: "Save" })).toHaveProperty("disabled", false))
})

it("saves the file's text over the version it opened, and offers a reload when it changed on disk", async () => {
  putReply = () => json({ error: "the file changed", code: "stale", yaml: "# changed on disk\n" + fileText, digest: "sha256:disk" }, 409)
  renderAt("/workflows/review/edit")
  await waitFor(() => expect(editor().value).toBe(fileText))
  const edited = fileText.replace("title: Review", "title: Review it")
  type(edited)
  const save = screen.getByRole("button", { name: "Save" })
  await waitFor(() => expect(save).toHaveProperty("disabled", false))
  fireEvent.click(save)
  const conflict = await screen.findByRole("alertdialog", { name: "The file changed" })
  expect(puts[0]).toEqual({ yaml: edited, digest: "sha256:old", confirm_triggers: false })
  fireEvent.click(within(conflict).getByRole("button", { name: "Reload" }))
  expect(editor().value).toBe("# changed on disk\n" + fileText)
  expect(screen.queryByRole("alertdialog")).toBeNull()
})

it("asks before a save that switches a trigger on", async () => {
  putReply = body => body.confirm_triggers
    ? json({ name: "review", path: "/g/review.yaml", folder: "global", digest: "sha256:new", diagnostics: [] })
    : json({ error: "this save switches on schedule", code: "confirm_triggers", triggers: ["schedule"] }, 409)
  renderAt("/workflows/review/edit")
  await waitFor(() => expect(editor().value).toBe(fileText))
  type(fileText + "on:\n  schedule: {cron: \"0 9 * * *\"}\n")
  const save = screen.getByRole("button", { name: "Save" })
  await waitFor(() => expect(save).toHaveProperty("disabled", false))
  fireEvent.click(save)
  const ask = await screen.findByRole("alertdialog", { name: "Switch on triggers" })
  expect(ask.textContent).toContain("schedule")
  fireEvent.click(within(ask).getByRole("button", { name: "Save and switch on" }))
  await waitFor(() => expect(puts.map(p => p.confirm_triggers)).toEqual([false, true]))
  await waitFor(() => expect(screen.queryByRole("alertdialog")).toBeNull())
})

it("keeps unsaved text as a draft when the page is left, and offers it back", async () => {
  const first = renderAt("/workflows/review/edit")
  await waitFor(() => expect(editor().value).toBe(fileText))
  type(fileText.replace("title: Review", "title: Kept"))
  first.unmount()
  renderAt("/workflows/review/edit")
  const offer = await screen.findByRole("alertdialog", { name: "Unsaved changes" })
  fireEvent.click(within(offer).getByRole("button", { name: "Restore them" }))
  expect(editor().value).toContain("title: Kept")
})

it("asks before a link elsewhere in the app takes the page away", async () => {
  const confirm = vi.spyOn(window, "confirm").mockReturnValueOnce(false).mockReturnValueOnce(true)
  renderAt("/workflows/review/edit")
  await waitFor(() => expect(editor().value).toBe(fileText))
  type(fileText.replace("title: Review", "title: Kept"))
  fireEvent.click(screen.getByRole("link", { name: "Board" }))
  expect(screen.queryByText("board page")).toBeNull()
  fireEvent.click(screen.getByRole("link", { name: "Board" }))
  expect(await screen.findByText("board page")).toBeTruthy()
  expect(confirm).toHaveBeenCalledTimes(2)
  confirm.mockRestore()
})

it("retries a file that could not be read", async () => {
  fileReply = () => json({ error: "the server is restarting" }, 503)
  renderAt("/workflows/review/edit")
  const alert = await screen.findByRole("alert")
  fileReply = () => json({ name: "review", yaml: fileText, path: "/g/review.yaml", folder: "global", digest: "sha256:old", editable: true })
  fireEvent.click(within(alert).getByRole("button", { name: "Retry" }))
  await waitFor(() => expect(editor().value).toBe(fileText))
})

it("keeps a new workflow's draft while offering it, until a choice is made", async () => {
  const first = renderAt("/workflows/new")
  type(editor().value.replace("title: My workflow", "title: Kept"))
  first.unmount()
  const second = renderAt("/workflows/new")
  expect(await screen.findByRole("alertdialog", { name: "Unsaved changes" })).toBeTruthy()
  second.unmount()
  renderAt("/workflows/new")
  const offer = await screen.findByRole("alertdialog", { name: "Unsaved changes" })
  fireEvent.click(within(offer).getByRole("button", { name: "Restore them" }))
  expect(editor().value).toContain("title: Kept")
})

it("undoes typing with the keyboard in the one history, a pause making a new step", async () => {
  let now = 1_000_000
  const clock = vi.spyOn(Date, "now").mockImplementation(() => now)
  try {
    renderAt("/workflows/review/edit")
    await waitFor(() => expect(editor().value).toBe(fileText))
    const one = fileText.replace("title: Review", "title: R")
    const two = fileText.replace("title: Review", "title: Re")
    type(one)
    now += 100
    type(two)
    now += 5_000
    const three = two.replace("prompt: hi", "prompt: hello")
    type(three)
    fireEvent.keyDown(editor(), { key: "z", ctrlKey: true })
    expect(editor().value).toBe(two)
    fireEvent.keyDown(editor(), { key: "z", ctrlKey: true })
    expect(editor().value).toBe(fileText)
    fireEvent.keyDown(editor(), { key: "Z", ctrlKey: true, shiftKey: true })
    expect(editor().value).toBe(two)
  } finally {
    clock.mockRestore()
  }
})
