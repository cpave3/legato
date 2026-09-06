import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react"
import "@testing-library/jest-dom/vitest"
import { afterEach, expect, it, vi } from "vitest"
import { NativeModelSettings } from "../NativeModelSettings"
import type { ModelSettingsState } from "../../lib/modelSettings"

const state: ModelSettingsState = {
  model: "manticore/sol", overrides: {}, defaults: { reasoningEffort: "medium" }, effective: { reasoningEffort: "medium" },
  capabilities: { schemaVersion: 1, modelId: "gpt-5.6-sol", profile: "codex", source: "test", fingerprint: "one", settings: {
    reasoningEffort: { status: "supported", type: "enum", values: ["low", "medium", "high", "max"] },
    parallelToolCalls: { status: "supported", type: "boolean" },
    thinking: { status: "unsupported", type: "enum" },
  } },
}
afterEach(() => { cleanup(); vi.restoreAllMocks() })

it("does not apply a stale response after the panel is unmounted", async () => {
  let finish!: (response: Response) => void
  const saved = vi.fn()
  vi.spyOn(globalThis, "fetch").mockImplementation(async (_url, init) => init?.method === "POST" ? new Promise<Response>((resolve) => { finish = resolve }) : new Response(JSON.stringify(state)))
  const view = render(<NativeModelSettings agentId="one" baseUrl="" model={state.model} busy={false} onSaved={saved} />)
  fireEvent.click(screen.getByRole("button", { name: "Model settings" }))
  await screen.findByLabelText("Reasoning effort")
  fireEvent.click(screen.getByRole("button", { name: "Apply" }))
  view.unmount()
  finish(new Response(JSON.stringify({ settingsState: state })))
  await new Promise((resolve) => setTimeout(resolve, 0))
  expect(saved).not.toHaveBeenCalled()
})

it("offers supported controls and applies max and false together", async () => {
  const saved = vi.fn()
  const fetchMock = vi.spyOn(globalThis, "fetch").mockImplementation(async (_url, init) => {
    if (init?.method === "POST") return new Response(JSON.stringify({ settingsState: { ...state, effective: { reasoningEffort: "max", parallelToolCalls: false } } }))
    return new Response(JSON.stringify(state))
  })
  render(<NativeModelSettings agentId="one" baseUrl="" model={state.model} busy={false} onSaved={saved} />)
  fireEvent.click(screen.getByRole("button", { name: "Model settings" }))
  fireEvent.change(await screen.findByLabelText("Reasoning effort"), { target: { value: "max" } })
  expect(screen.queryByLabelText("Thinking")).toBeNull()
  fireEvent.change(screen.getByLabelText("Parallel tool calls"), { target: { value: "false" } })
  fireEvent.click(screen.getByRole("button", { name: "Apply" }))
  await waitFor(() => expect(saved).toHaveBeenCalledOnce())
  const post = fetchMock.mock.calls.find(([, init]) => init?.method === "POST")
  expect(JSON.parse(String(post?.[1]?.body))).toEqual({ expectedModel: state.model, settings: { reasoningEffort: "max", parallelToolCalls: false } })
  expect(document.activeElement).toBe(screen.getByRole("button", { name: "Model settings" }))
})

it("keeps a failed draft and disables applying during a run", async () => {
  vi.spyOn(globalThis, "fetch").mockImplementation(async (_url, init) => init?.method === "POST"
    ? new Response(JSON.stringify({ error: "agent is running" }), { status: 409 }) : new Response(JSON.stringify(state)))
  const view = render(<NativeModelSettings agentId="one" baseUrl="" model={state.model} busy={false} onSaved={vi.fn()} />)
  fireEvent.click(screen.getByRole("button", { name: "Model settings" }))
  fireEvent.change(await screen.findByLabelText("Reasoning effort"), { target: { value: "max" } })
  fireEvent.click(screen.getByRole("button", { name: "Apply" }))
  expect(await screen.findByRole("alert")).toHaveTextContent("agent is running")
  expect(screen.getByLabelText("Reasoning effort")).toHaveValue("max")
  view.rerender(<NativeModelSettings agentId="one" baseUrl="" model={state.model} busy={true} onSaved={vi.fn()} />)
  expect(screen.getByRole("button", { name: "Apply" })).toBeDisabled()
})

it("resets current-model overrides only after Apply and supports Escape", async () => {
  const fetchMock = vi.spyOn(globalThis, "fetch").mockImplementation(async (_url, init) => new Response(JSON.stringify(init?.method === "POST" ? { settingsState: state } : { ...state, overrides: { reasoningEffort: "high" } })))
  render(<NativeModelSettings agentId="one" baseUrl="" model={state.model} busy={false} onSaved={vi.fn()} />)
  fireEvent.click(screen.getByRole("button", { name: "Model settings" }))
  await screen.findByLabelText("Reasoning effort")
  fireEvent.click(screen.getByText("Reset to defaults"))
  expect(fetchMock).toHaveBeenCalledTimes(1)
  expect(screen.getByLabelText("Reasoning effort")).toHaveValue("")
  fireEvent.click(screen.getByRole("button", { name: "Apply" }))
  await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull())
  expect(JSON.parse(String(fetchMock.mock.calls[1][1]?.body))).toMatchObject({ resetSettings: true })
  fireEvent.click(screen.getByRole("button", { name: "Model settings" }))
  fireEvent.keyDown(await screen.findByRole("dialog"), { key: "Escape" })
  expect(screen.queryByRole("dialog")).toBeNull()
})
