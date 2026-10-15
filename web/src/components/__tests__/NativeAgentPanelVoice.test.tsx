import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { NativeAgentPanel } from "../NativeAgentPanel"

vi.mock("../../hooks/useWebSocket", () => ({
  useWebSocket: () => ({ connected: true, send: vi.fn(), subscribe: () => () => {} }),
}))

vi.mock("../../hooks/useServer", () => ({
  useServer: () => ({ baseUrl: "" }),
}))

describe("NativeAgentPanel dictation", () => {
  beforeEach(() => {
    vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
      const url = String(input)
      if (url.includes("/snapshot")) return new Response(JSON.stringify({ id: "session-1", status: "idle", messages: [], toolCalls: [] }), { status: 200 })
      if (url.endsWith("/api/voice/transcribe")) return new Response(JSON.stringify({ text: "which tickets have designs" }), { status: 200 })
      return new Response("[]", { status: 200 })
    })
    vi.stubGlobal("AudioContext", class {
      destination = {}
      createMediaStreamSource() { return { connect: vi.fn(), disconnect: vi.fn() } }
      createScriptProcessor() { return { connect: vi.fn(), disconnect: vi.fn(), onaudioprocess: null } }
      close() { return Promise.resolve() }
    })
    Object.defineProperty(navigator, "mediaDevices", {
      configurable: true,
      value: { getUserMedia: vi.fn().mockResolvedValue({ getTracks: () => [{ stop: vi.fn() }] }) },
    })
  })

  afterEach(() => {
    cleanup()
    vi.restoreAllMocks()
    vi.unstubAllGlobals()
  })

  it("sets the foot of the composer aside while it records, and puts what was said in the message", async () => {
    render(<NativeAgentPanel agentId="task-1" voiceEnabled />)
    const box = await screen.findByRole("textbox", { name: "Message" }) as HTMLTextAreaElement
    fireEvent.change(box, { target: { value: "can you look" } })

    fireEvent.click(screen.getByRole("button", { name: "Voice dictation" }))
    const recording = await screen.findByRole("group", { name: "Dictating" })
    expect(within(recording).getByText("0:00")).not.toBeNull()
    expect(screen.queryByTestId("composer-actions")).toBeNull()

    fireEvent.click(within(recording).getByRole("button", { name: "Stop" }))
    await waitFor(() => expect(box.value).toBe("can you look which tickets have designs"))
    expect(screen.getByTestId("composer-actions")).not.toBeNull()
  })

  it("throws a recording away with Cancel", async () => {
    render(<NativeAgentPanel agentId="task-1" voiceEnabled />)
    await screen.findByRole("textbox", { name: "Message" })

    fireEvent.click(screen.getByRole("button", { name: "Voice dictation" }))
    fireEvent.click(within(await screen.findByRole("group", { name: "Dictating" })).getByRole("button", { name: "Cancel" }))

    expect(screen.queryByRole("group", { name: "Dictating" })).toBeNull()
    expect(vi.mocked(fetch).mock.calls.some(([url]) => String(url).endsWith("/api/voice/transcribe"))).toBe(false)
  })
})
