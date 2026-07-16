import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { VoiceRecorder } from "../VoiceRecorder"

const mockFetch = vi.fn()
vi.stubGlobal("fetch", mockFetch)

afterEach(cleanup)

beforeEach(() => {
  mockFetch.mockReset()
  mockFetch.mockResolvedValue({ ok: true, status: 200, json: async () => ({ text: "dictated question" }) })
  const track = { stop: vi.fn() }
  vi.stubGlobal("AudioContext", class {
    destination = {}
    createMediaStreamSource() { return { connect: vi.fn(), disconnect: vi.fn() } }
    createScriptProcessor() { return { connect: vi.fn(), disconnect: vi.fn(), onaudioprocess: null } }
    close() { return Promise.resolve() }
  })
  Object.defineProperty(navigator, "mediaDevices", {
    configurable: true,
    value: { getUserMedia: vi.fn().mockResolvedValue({ getTracks: () => [track] }) },
  })
})

describe("VoiceRecorder", () => {
  it("returns transcription without requesting agent delivery", async () => {
    const onTranscription = vi.fn()
    render(<VoiceRecorder baseUrl="" transcriptionOnly onTranscription={onTranscription} />)

    fireEvent.click(screen.getByTitle("Voice dictation (V)"))
    await screen.findByTitle("Click to add transcription")
    fireEvent.click(screen.getByTitle("Click to add transcription"))

    await waitFor(() => expect(onTranscription).toHaveBeenCalledWith("dictated question"))
    expect(mockFetch).toHaveBeenCalledWith("/api/voice/transcribe", expect.objectContaining({
      body: expect.stringContaining('"transcription_only":true'),
    }))
  })
})
