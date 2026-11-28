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

it("keeps Tab in the labelled model form and Escape returns to its trigger", async () => {
  const user = (await import('@testing-library/user-event')).default.setup()
  vi.spyOn(globalThis, 'fetch').mockResolvedValue(new Response(JSON.stringify(state)))
  render(<NativeModelSettings agentId="one" baseUrl="" model={state.model} busy={false} onSaved={vi.fn()} />)
  const trigger = screen.getByRole('button', { name: 'Model settings' })
  await user.click(trigger)
  await screen.findByLabelText('Reasoning effort')
  const apply = screen.getByRole('button', { name: 'Apply' })
  apply.focus()
  await user.tab()
  expect(screen.getByRole('dialog').contains(document.activeElement)).toBe(true)
  await user.keyboard('{Escape}')
  expect(screen.queryByRole('dialog')).toBeNull()
  expect(trigger).toHaveFocus()
})

const thinkingState: ModelSettingsState = {
  model: "anthropic/opus", overrides: { thinking: "on" }, defaults: { thinking: "on", maxOutputTokens: 4000, temperature: 0.5 }, effective: { thinking: "on" },
  capabilities: { schemaVersion: 1, modelId: "opus-x", profile: "claude", source: "test", fingerprint: "two", settings: {
    thinking: { status: "supported", type: "enum", values: ["on", "off"] },
    reasoningEffort: { status: "supported", type: "enum", values: ["low", "high"], default: "low" },
    thinkingBudgetTokens: { status: "supported", type: "number", min: 1, max: 100, integer: true },
    temperature: { status: "unsupported", type: "number", reason: "Fixed by the provider." },
    maxOutputTokens: { status: "unknown", type: "number" },
  } },
}

async function openWith(value: unknown, model = thinkingState.model, post?: unknown) {
  const fetchMock = vi.spyOn(globalThis, "fetch").mockImplementation(async (_url, init) =>
    new Response(JSON.stringify(init?.method === "POST" ? post : value)))
  render(<NativeModelSettings agentId="one" baseUrl="" model={model} busy={false} onSaved={vi.fn()} />)
  fireEvent.click(screen.getByRole("button", { name: "Model settings" }))
  return fetchMock
}

it("shows the resolved model, defaults and the reasons for settings it cannot take", async () => {
  await openWith(thinkingState)
  expect(await screen.findByText("Resolved model: opus-x")).toBeInTheDocument()
  expect(screen.getByLabelText("Thinking")).toHaveValue("on")
  expect(screen.getByRole("option", { name: "Default (on)" })).toBeInTheDocument()
  expect(screen.getByRole("option", { name: "Default (low)" })).toBeInTheDocument()
  const budget = screen.getByLabelText("Thinking budget")
  expect(budget).toHaveAttribute("type", "number")
  expect(budget).toHaveAttribute("placeholder", "Default")
  expect(budget).toHaveAttribute("min", "1")
  expect(budget).toHaveAttribute("max", "100")
  expect(budget).toHaveAttribute("step", "1")
  expect(screen.getByText("Temperature: Fixed by the provider.")).toBeInTheDocument()
  expect(screen.getByText("Output limit: Support is unknown for this model.")).toBeInTheDocument()
  expect(screen.getByText("Applies to this session. Default uses the configured model value.")).toBeInTheDocument()
})

it("turning thinking off clears and disables effort and budget", async () => {
  const fetchMock = await openWith(thinkingState, thinkingState.model, { settingsState: thinkingState })
  fireEvent.change(await screen.findByLabelText("Thinking budget"), { target: { value: "12" } })
  expect(screen.getByLabelText("Thinking budget")).toHaveValue(12)
  fireEvent.change(screen.getByLabelText("Thinking"), { target: { value: "off" } })
  expect(screen.getByLabelText("Reasoning effort")).toBeDisabled()
  expect(screen.getByLabelText("Thinking budget")).toBeDisabled()
  expect(screen.getByLabelText("Thinking budget")).toHaveValue(null)
  fireEvent.click(screen.getByRole("button", { name: "Apply" }))
  await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(2))
  expect(JSON.parse(String(fetchMock.mock.calls[1][1]?.body)).settings).toEqual({ thinking: "off", thinkingBudgetTokens: null, reasoningEffort: null })
})

it("a cleared number goes back to its default", async () => {
  const fetchMock = await openWith(thinkingState, thinkingState.model, { settingsState: thinkingState })
  fireEvent.change(await screen.findByLabelText("Thinking budget"), { target: { value: "12" } })
  fireEvent.change(screen.getByLabelText("Thinking budget"), { target: { value: "" } })
  fireEvent.click(screen.getByRole("button", { name: "Apply" }))
  await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(2))
  expect(JSON.parse(String(fetchMock.mock.calls[1][1]?.body)).settings).toEqual({ thinkingBudgetTokens: null })
})

it("names boolean options On and Off", async () => {
  await openWith(state, state.model)
  await screen.findByLabelText("Parallel tool calls")
  expect(screen.getByRole("option", { name: "On" })).toHaveValue("true")
  expect(screen.getByRole("option", { name: "Off" })).toHaveValue("false")
})

it("says settings are unavailable for another model or schema", async () => {
  await openWith(thinkingState, "other/model")
  expect(await screen.findByText(/Settings information is unavailable for this model/)).toBeInTheDocument()
  expect(screen.getByRole("button", { name: "Apply" })).toBeDisabled()
  expect(screen.queryByText(/Resolved model/)).toBeNull()
})

it("refuses a saved state for another model", async () => {
  await openWith(thinkingState, thinkingState.model, { settingsState: { ...thinkingState, model: "else" } })
  await screen.findByLabelText("Thinking")
  fireEvent.click(screen.getByRole("button", { name: "Apply" }))
  expect(await screen.findByRole("alert")).toHaveTextContent("The model changed. Reopen its settings.")
})

it("reports a failed read", async () => {
  vi.spyOn(globalThis, "fetch").mockResolvedValue(new Response(JSON.stringify({ error: "no session" }), { status: 404 }))
  render(<NativeModelSettings agentId="one" baseUrl="" model="m" busy={false} onSaved={vi.fn()} />)
  fireEvent.click(screen.getByRole("button", { name: "Model settings" }))
  expect(await screen.findByRole("alert")).toHaveTextContent("no session")
})

it("asks to wait while the agent runs", async () => {
  vi.spyOn(globalThis, "fetch").mockResolvedValue(new Response(JSON.stringify(thinkingState)))
  render(<NativeModelSettings agentId="one" baseUrl="" model={thinkingState.model} busy={true} onSaved={vi.fn()} />)
  fireEvent.click(screen.getByRole("button", { name: "Model settings" }))
  expect(await screen.findByText("Wait for the current run to finish before applying settings.")).toBeInTheDocument()
  expect(screen.getByLabelText("Thinking")).toBeDisabled()
  expect(screen.getByRole("button", { name: "Reset to defaults" })).toBeDisabled()
})
