import { beforeEach, describe, expect, it, vi } from "vitest"
import { fireEvent, render, screen } from "@testing-library/react"
import { MemoryRouter } from "react-router-dom"
import { AgentSelectionProvider } from "../../hooks/useAgentSelection"
import { BoardPage } from "../Board"

const mockFetch = vi.fn()

vi.stubGlobal("fetch", mockFetch)
vi.mock("../../hooks/useServer", () => ({ useServer: () => ({ baseUrl: "http://legato.test" }) }))
vi.mock("../../hooks/useVoiceEnabled", () => ({ useVoiceEnabled: () => true }))
vi.mock("../../hooks/useBoard", () => ({
  useBoard: () => ({
    columns: [{ name: "Backlog", cards: [] }],
    workspaces: [],
    loading: false,
    error: null,
    refresh: vi.fn(),
  }),
}))
vi.mock("../../hooks/useToast", () => ({ useToast: () => ({ addToast: vi.fn() }) }))
vi.mock("../../hooks/useWorkflow", () => ({ useWorkflows: () => ({ data: null, loading: false, error: null }) }))
vi.mock("../../components/VoiceRecorder", () => ({
  VoiceRecorder: ({ baseUrl, transcriptionOnly }: { baseUrl: string; transcriptionOnly?: boolean }) => (
    <button type="button" aria-label="Board description dictation" data-base-url={baseUrl} data-transcription-only={transcriptionOnly}>Dictate</button>
  ),
}))

describe("Board voice dictation", () => {
  beforeEach(() => {
    mockFetch.mockReset()
    mockFetch.mockResolvedValue({ ok: true, json: async () => ({ repositories: [] }) })
  })

  it("offers configured transcription-only dictation in the New Task modal", () => {
    render(<MemoryRouter><AgentSelectionProvider><BoardPage /></AgentSelectionProvider></MemoryRouter>)

    fireEvent.keyDown(window, { key: "n" })

    const dictate = screen.getByRole("button", { name: "Board description dictation" })
    expect(dictate.getAttribute("data-base-url")).toBe("http://legato.test")
    expect(dictate.getAttribute("data-transcription-only")).toBe("true")
  })
})
