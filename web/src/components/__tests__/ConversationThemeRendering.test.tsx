import { cleanup, fireEvent, render, screen } from "@testing-library/react"
import { afterEach, expect, it, vi } from "vitest"
import { MarkdownBody } from "../MarkdownBody"
import { NativeTerminalPrompt } from "../NativeTerminalPrompt"
import { NativeQuestionCard } from "../NativeQuestionCard"
import { NativeAgentPanel } from "../NativeAgentPanel"
import { ReadImageRow } from "../ReadImageRow"

// Check the rendered elements, not the palette constants: these are the
// colors that must remain runtime variables even in memoized Markdown.
vi.mock("../../hooks/useServer", () => ({ useServer: () => ({ baseUrl: "" }) }))
vi.mock("../../hooks/useWebSocket", () => ({
  useWebSocket: () => ({ connected: true, send: vi.fn(), subscribe: () => () => {} }),
}))

afterEach(() => { cleanup(); vi.restoreAllMocks() })

it("binds memoized Markdown code surfaces to runtime theme roles", () => {
  const { container } = render(<MarkdownBody body={'`inline`\n\n```ts\nconst count = 1\n```'} />)
  expect(screen.getByText("inline").className).toContain("bg-[var(--theme-surface,")
  expect(container.querySelector("pre")?.className).toContain("bg-[var(--theme-canvas,")
})

it("renders readable native choices and previews instead of fixed white on accents", () => {
  render(<>
    <NativeTerminalPrompt askedBy="Codex" prompt={{ type: "menu", context: "Choose a mode", actions: [{ label: "Continue", keys: "Enter" }] }} onChoose={vi.fn()} />
    <NativeQuestionCard askedBy="Claude Code" question={{ requestId: "q", status: "pending", questions: [{ id: "one", question: "Which mode?", multiSelect: false, options: [{ label: "Safe", preview: "Safe preview" }] }] }} onAnswer={vi.fn()} onDismiss={vi.fn()} />
  </>)
  expect(screen.getByRole("button", { name: "Continue" }).className).toContain("text-[var(--theme-text,")
  expect(screen.getByRole("button", { name: "Continue" }).className).toContain("bg-[var(--theme-selection,")
  fireEvent.click(screen.getByRole("radio", { name: "Safe" }))
  expect(screen.getByText("Safe preview").className).toContain("bg-[var(--theme-canvas,")
  expect(screen.getByRole("button", { name: "Send answer" }).className).toContain("text-[var(--theme-text,")
})

it("renders native replies and tool badges with runtime theme colors", async () => {
  vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => String(input).includes("/snapshot")
    ? new Response(JSON.stringify({ id: "session", status: "idle", messages: [
      { role: "user", content: "system" },
      { role: "assistant", content: [{ type: "tool-call" }] },
      { role: "assistant", content: "Finished the change." },
    ], toolCalls: [{ callId: "edit", name: "edit", args: { path: "a.ts", old_string: "old", new_string: "new" } }] }), { status: 200 })
    : new Response("[]", { status: 200 }))
  render(<NativeAgentPanel agentId="theme-test" />)
  expect((await screen.findByText("edit")).className).toContain("text-[var(--theme-text,")
  const reply = await screen.findByText("Finished the change.")
  expect(reply.closest("[class*='--theme-text']")?.className).toContain("text-[var(--theme-text,")
})

it("adapts the image-read badge without fetching or recoloring user media", () => {
  const fetch = vi.spyOn(globalThis, "fetch")
  render(<ReadImageRow agentId="theme-test" path="/tmp/example.png" grouped={false} />)
  expect(screen.getByText("read").className).toContain("text-[var(--theme-text,")
  expect(screen.queryByRole("img")).toBeNull()
  expect(fetch).not.toHaveBeenCalled()
})
