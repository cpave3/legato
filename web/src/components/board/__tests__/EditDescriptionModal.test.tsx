import { afterEach, describe, expect, it, vi } from "vitest"
import { cleanup, render, screen, fireEvent } from "@testing-library/react"
import { EditDescriptionModal } from "../EditDescriptionModal"

vi.mock("../../VoiceRecorder", () => ({
  VoiceRecorder: ({ onTranscription }: { onTranscription?: (text: string) => void }) => (
    <button type="button" aria-label="Dictate description" onClick={() => onTranscription?.("dictated text")}>Dictate</button>
  ),
}))

afterEach(cleanup)

describe("EditDescriptionModal", () => {
  it("replaces the selected description and saves only on submit", () => {
    const onSave = vi.fn()
    render(
      <EditDescriptionModal
        open
        currentDescription={"First line\nreplace this\nLast line"}
        baseUrl="http://legato.test"
        voiceEnabled
        onClose={() => {}}
        onSave={onSave}
      />
    )

    const description = screen.getByLabelText("Description") as HTMLTextAreaElement
    description.focus()
    description.setSelectionRange(11, 23)
    fireEvent.select(description)

    fireEvent.click(screen.getByRole("button", { name: "Dictate description" }))

    expect(description.value).toBe("First line\ndictated text\nLast line")
    expect(description).toBe(document.activeElement)
    expect(description.selectionStart).toBe(24)
    expect(description.selectionEnd).toBe(24)
    expect(onSave).not.toHaveBeenCalled()

    fireEvent.click(screen.getByRole("button", { name: "Save" }))
    expect(onSave).toHaveBeenCalledWith("First line\ndictated text\nLast line")
  })

  it("hides dictation when voice is disabled", () => {
    render(
      <EditDescriptionModal
        open
        currentDescription="Existing"
        baseUrl="http://legato.test"
        voiceEnabled={false}
        onClose={() => {}}
        onSave={() => {}}
      />
    )

    expect(screen.queryByRole("button", { name: "Dictate description" })).toBeNull()
  })

  it("appends dictation on a new line when no textarea selection is available", () => {
    render(
      <EditDescriptionModal
        open
        currentDescription="Existing"
        baseUrl="http://legato.test"
        voiceEnabled
        onClose={() => {}}
        onSave={() => {}}
      />
    )

    const description = screen.getByLabelText("Description") as HTMLTextAreaElement
    fireEvent.click(screen.getByRole("button", { name: "Dictate description" }))
    expect(description.value).toBe("Existing\ndictated text")
    expect(description.selectionStart).toBe(22)
  })

  it("submits with current content", () => {
    const onSave = vi.fn()
    render(<EditDescriptionModal open={true} currentDescription="Old desc" baseUrl="" voiceEnabled={false} onClose={() => {}} onSave={onSave} />)
    expect(screen.getByRole("dialog", { name: "Edit description" })).toBeTruthy()
    expect(screen.getByRole("button", { name: "Close dialog" })).toBeTruthy()
    const description = screen.getByLabelText("Description") as HTMLTextAreaElement
    expect(description.rows).toBe(14)
    expect(description.className).toContain("resize-y")
    expect(screen.getByText("Markdown supported")).toBeTruthy()
    const panel = screen.getByRole("dialog", { name: "Edit description" })
    expect(panel.className).toContain("max-w-2xl")
    expect(panel.className).toContain("max-h-[calc(100dvh-2rem)]")
    expect(panel.className).not.toContain("h-[min(80vh")
    const saveBtn = screen.getByRole("button", { name: /Save/i })
    fireEvent.click(saveBtn)
    expect(onSave).toHaveBeenCalledWith("Old desc")
  })

  it("explains provider ownership and edits only the supplied Legato details", () => {
    const onSave = vi.fn()
    render(<EditDescriptionModal open currentDescription="Local only" baseUrl="" voiceEnabled={false} providerBacked onClose={() => {}} onSave={onSave} />)
    expect(screen.getByRole("dialog", { name: "Edit Legato details" })).toBeTruthy()
    expect(screen.getByText(/provider description is read-only/i)).toBeTruthy()
    expect((screen.getByLabelText("Description") as HTMLTextAreaElement).value).toBe("Local only")
    fireEvent.click(screen.getByRole("button", { name: "Save" }))
    expect(onSave).toHaveBeenCalledWith("Local only")
  })
})
