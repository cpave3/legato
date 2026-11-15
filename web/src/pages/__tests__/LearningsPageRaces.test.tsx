import "@testing-library/jest-dom/vitest"
import { afterEach, beforeEach, expect, it, vi } from "vitest"
import { act, fireEvent, render, screen } from "@testing-library/react"
import { MemoryRouter } from "react-router-dom"
import { LearningsPage } from "../LearningsPage"

const server = vi.hoisted(() => ({ baseUrl: "" }))
vi.mock("../../hooks/useServer", () => ({ useServer: () => server }))

function response(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } })
}
function deferred() {
  let resolve!: (value: Response) => void
  const promise = new Promise<Response>(done => { resolve = done })
  return { promise, resolve }
}
const page = () => <MemoryRouter><LearningsPage /></MemoryRouter>
beforeEach(() => { server.baseUrl = "" })
afterEach(() => vi.unstubAllGlobals())

it("keeps the current filter error when an older list completes, then retries", async () => {
  const old = deferred()
  const fetch = vi.fn().mockReturnValueOnce(old.promise)
    .mockResolvedValueOnce(response({ error: "Filter unavailable" }, 503))
    .mockResolvedValueOnce(response([]))
  vi.stubGlobal("fetch", fetch)
  render(page())
  fireEvent.change(screen.getByLabelText("Status"), { target: { value: "validated" } })
  expect(await screen.findByRole("alert")).toHaveTextContent("Filter unavailable")
  await act(async () => old.resolve(response([])))
  expect(screen.queryByText("No learnings found.")).not.toBeInTheDocument()
  fireEvent.click(screen.getByRole("button", { name: "Retry learnings" }))
  expect(await screen.findByText("No learnings found.")).toBeInTheDocument()
  expect(fetch.mock.calls[2][0]).toContain("status=validated")
})

it("ignores an old filter failure while the new filter is still loading", async () => {
  const old = deferred(), current = deferred()
  vi.stubGlobal("fetch", vi.fn().mockReturnValueOnce(old.promise).mockReturnValueOnce(current.promise))
  render(page())
  fireEvent.change(screen.getByLabelText("Scope"), { target: { value: "project" } })
  await act(async () => old.resolve(response({ error: "Old failure" }, 503)))
  expect(screen.queryByRole("alert")).not.toBeInTheDocument()
  expect(screen.getByRole("status")).toHaveAttribute("aria-label", "Loading learnings…")
  await act(async () => current.resolve(response([])))
  expect(screen.getByText("No learnings found.")).toBeInTheDocument()
})

it.each([200, 503])("keeps the new preview pending when an old task settles (%s), with errors separate from the list", async (status) => {
  const old = deferred(), current = deferred()
  vi.stubGlobal("fetch", vi.fn().mockResolvedValueOnce(response([])).mockReturnValueOnce(old.promise)
    .mockReturnValueOnce(current.promise))
  render(page())
  await screen.findByText("No learnings found.")
  fireEvent.change(screen.getByLabelText("Prompt preview task"), { target: { value: "old-task" } })
  fireEvent.click(screen.getByRole("button", { name: "Preview prompt" }))
  fireEvent.change(screen.getByLabelText("Prompt preview task"), { target: { value: "new-task" } })
  fireEvent.click(screen.getByRole("button", { name: "Preview prompt" }))
  await act(async () => old.resolve(response(status === 200 ? { rendered: "Old prompt", included: [], excluded: [] } : { error: "Old preview error" }, status)))
  expect(screen.queryByText("Old prompt")).not.toBeInTheDocument()
  expect(screen.queryByRole("alert")).not.toBeInTheDocument()
  expect(screen.getByRole("button", { name: "Loading preview…" })).toBeDisabled()
  await act(async () => current.resolve(response({ error: "Preview unavailable" }, 503)))
  expect(await screen.findByRole("alert")).toHaveTextContent("Preview unavailable")
  expect(screen.getByText("No learnings found.")).toBeInTheDocument()
  expect(screen.queryByRole("button", { name: "Retry learnings" })).not.toBeInTheDocument()
})

it("clears the old server preview immediately and ignores its pending list failure", async () => {
  const old = deferred()
  vi.stubGlobal("fetch", vi.fn().mockReturnValueOnce(old.promise)
    .mockResolvedValueOnce(response({ rendered: "Server A prompt", included: [], excluded: [] }))
    .mockResolvedValueOnce(response([])))
  const { rerender } = render(page())
  fireEvent.change(screen.getByLabelText("Prompt preview task"), { target: { value: "task" } })
  fireEvent.click(screen.getByRole("button", { name: "Preview prompt" }))
  await screen.findByText("Server A prompt")
  server.baseUrl = "http://server-b"
  rerender(page())
  expect(screen.queryByText("Server A prompt")).not.toBeInTheDocument()
  await screen.findByText("No learnings found.")
  await act(async () => old.resolve(response({ error: "Server A offline" }, 503)))
  expect(screen.queryByRole("alert")).not.toBeInTheDocument()
  expect(screen.getByText("No learnings found.")).toBeInTheDocument()
})
