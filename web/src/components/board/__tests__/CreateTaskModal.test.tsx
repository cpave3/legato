import { afterEach, describe, expect, it, vi } from "vitest"
import { act, cleanup, fireEvent, render, screen } from "@testing-library/react"
import { CreateTaskModal } from "../CreateTaskModal"

vi.mock("../../VoiceRecorder", () => ({
  VoiceRecorder: ({ baseUrl, transcriptionOnly, onTranscription }: { baseUrl: string; transcriptionOnly?: boolean; onTranscription?: (text: string) => void }) => (
    <button type="button" aria-label="Dictate description" data-base-url={baseUrl} data-transcription-only={transcriptionOnly} onClick={() => onTranscription?.("dictated text")}>Dictate</button>
  ),
}))

afterEach(() => {
  cleanup()
  vi.useRealTimers()
})

describe("CreateTaskModal", () => {
  it("inserts dictation into the selected description and creates only on submit", () => {
    const onSubmit = vi.fn()
    render(
      <CreateTaskModal
        open
        columns={["Backlog"]}
        currentColumn="Backlog"
        workspaces={[]}
        baseUrl="http://legato.test"
        voiceEnabled
        onClose={() => {}}
        onSubmit={onSubmit}
      />
    )

    fireEvent.change(screen.getByLabelText(/Title/i), { target: { value: "Dictated task" } })
    const description = screen.getByLabelText("Description") as HTMLTextAreaElement
    fireEvent.change(description, { target: { value: "Before replace after" } })
    description.focus()
    description.setSelectionRange(7, 14)

    const dictate = screen.getByRole("button", { name: "Dictate description" })
    expect(dictate.getAttribute("data-base-url")).toBe("http://legato.test")
    expect(dictate.getAttribute("data-transcription-only")).toBe("true")
    fireEvent.click(dictate)

    expect(description.value).toBe("Before dictated text after")
    expect(description).toBe(document.activeElement)
    expect(description.selectionStart).toBe(20)
    expect(description.selectionEnd).toBe(20)
    expect(onSubmit).not.toHaveBeenCalled()

    fireEvent.click(screen.getByRole("button", { name: "Create" }))
    expect(onSubmit).toHaveBeenCalledWith(expect.objectContaining({ description: "Before dictated text after" }))
  })

  it("hides dictation when voice is disabled", () => {
    render(
      <CreateTaskModal
        open
        columns={["Backlog"]}
        currentColumn="Backlog"
        workspaces={[]}
        baseUrl="http://legato.test"
        voiceEnabled={false}
        onClose={() => {}}
        onSubmit={() => {}}
      />
    )

    expect(screen.queryByRole("button", { name: "Dictate description" })).toBeNull()
  })

  it("appends dictation on a new line when no textarea selection is available", () => {
    render(
      <CreateTaskModal
        open
        columns={["Backlog"]}
        currentColumn="Backlog"
        workspaces={[]}
        baseUrl="http://legato.test"
        voiceEnabled
        onClose={() => {}}
        onSubmit={() => {}}
      />
    )

    const description = screen.getByLabelText("Description") as HTMLTextAreaElement
    fireEvent.change(description, { target: { value: "Existing paragraph" } })

    fireEvent.click(screen.getByRole("button", { name: "Dictate description" }))

    expect(description.value).toBe("Existing paragraph\ndictated text")
    expect(description.selectionStart).toBe(32)
  })

  it("submits with correct payload", () => {
    const onSubmit = vi.fn()
    render(
      <CreateTaskModal
        open={true}
        columns={["Backlog", "Doing"]}
        currentColumn="Doing"
        workspaces={[{ id: 1, name: "W1", color: "#fff" }]}
        baseUrl=""
        voiceEnabled={false}
        onClose={() => {}}
        onSubmit={onSubmit}
      />
    )

    expect(screen.getByRole("dialog", { name: "New Task" })).toBeTruthy()
    expect(screen.getByRole("button", { name: "Close dialog" })).toBeTruthy()
    const dialog = screen.getByRole("dialog", { name: "New Task" })
    expect(dialog.className).toContain("max-w-2xl")
    expect(screen.getByRole("group", { name: "Task details" })).toBeTruthy()
    expect(screen.getByRole("group", { name: "Placement" })).toBeTruthy()
    expect(dialog.querySelector("[data-create-task-layout]")?.className).toContain("md:grid-cols-[minmax(0,1fr)_15rem]")
    expect((screen.getByLabelText("Description") as HTMLTextAreaElement).rows).toBe(10)
    expect(screen.getByText("Markdown supported")).toBeTruthy()

    const titleInput = screen.getByLabelText(/Title/i)
    fireEvent.change(titleInput, { target: { value: "New task title" } })

    const descInput = screen.getByLabelText(/Description/i)
    fireEvent.change(descInput, { target: { value: "Description text" } })
    expect((screen.getByRole("combobox", { name: "Column" }) as HTMLSelectElement).value).toBe("Doing")
    expect((screen.getByRole("combobox", { name: "Priority" }) as HTMLSelectElement).value).toBe("")
    expect((screen.getByRole("combobox", { name: "Workspace" }) as HTMLSelectElement).value).toBe("")

    const createBtn = screen.getByRole("button", { name: /Create/i })
    fireEvent.click(createBtn)

    expect(onSubmit).toHaveBeenCalledWith({
      title: "New task title",
      description: "Description text",
      column: "Doing",
      priority: "",
      workspace_id: null,
    })
  })

  it("keeps the title focused and editable across open rerenders", () => {
    vi.useFakeTimers()
    const onSubmit = vi.fn()
    const firstOnClose = vi.fn()
    const latestOnClose = vi.fn()
    const { rerender } = render(
      <CreateTaskModal
        open={true}
        columns={["Backlog", "Doing"]}
        currentColumn="Backlog"
        workspaces={[]}
        baseUrl=""
        voiceEnabled={false}
        onClose={firstOnClose}
        onSubmit={onSubmit}
      />
    )

    act(() => vi.advanceTimersByTime(50))
    const titleInput = screen.getByLabelText(/Title/i)
    expect(titleInput).toBe(document.activeElement)
    fireEvent.change(titleInput, { target: { value: "Keep" } })

    rerender(
      <CreateTaskModal
        open={true}
        columns={["Backlog", "Doing"]}
        currentColumn="Backlog"
        workspaces={[]}
        baseUrl=""
        voiceEnabled={false}
        onClose={latestOnClose}
        onSubmit={onSubmit}
        loading={true}
      />
    )

    expect(titleInput).toBe(document.activeElement)
    fireEvent.change(titleInput, { target: { value: "Keep typing" } })
    expect((titleInput as HTMLInputElement).value).toBe("Keep typing")
  })

  it("preserves form data while an open request is loading", () => {
    const onSubmit = vi.fn()
    const { rerender } = render(
      <CreateTaskModal
        open={true}
        columns={["Backlog", "Doing"]}
        currentColumn="Backlog"
        workspaces={[]}
        baseUrl=""
        voiceEnabled={false}
        onClose={() => {}}
        onSubmit={onSubmit}
      />
    )

    fireEvent.change(screen.getByLabelText(/Title/i), { target: { value: "Keep this title" } })
    fireEvent.change(screen.getByLabelText(/Description/i), { target: { value: "Keep this description" } })

    rerender(
      <CreateTaskModal
        open={true}
        columns={[...["Backlog", "Doing"]]}
        currentColumn="Backlog"
        workspaces={[]}
        baseUrl=""
        voiceEnabled={false}
        onClose={() => {}}
        onSubmit={onSubmit}
        loading={true}
      />
    )

    expect((screen.getByLabelText(/Title/i) as HTMLInputElement).value).toBe("Keep this title")
    expect((screen.getByLabelText(/Description/i) as HTMLTextAreaElement).value).toBe("Keep this description")
    expect((screen.getByRole("button", { name: "Creating…" }) as HTMLButtonElement).disabled).toBe(true)
  })
})
