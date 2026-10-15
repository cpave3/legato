import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react"
import type { ReactNode } from "react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { NativeAgentPanel } from "../NativeAgentPanel"

const send = vi.hoisted(() => vi.fn())

vi.mock("../../hooks/useWebSocket", () => ({
  useWebSocket: () => ({ connected: true, send, subscribe: () => () => {} }),
}))

vi.mock("../../hooks/useServer", () => ({
  useServer: () => ({ baseUrl: "" }),
}))

// The test page cannot make an address for a blob, so a stand-in counts them.
let made: string[] = []
let revoked: string[] = []

beforeEach(() => {
  made = []
  revoked = []
  send.mockReset()
  URL.createObjectURL = vi.fn(() => {
    const address = `blob:image-${made.length + 1}`
    made.push(address)
    return address
  })
  URL.revokeObjectURL = vi.fn((address: string) => { revoked.push(address) })
})

afterEach(() => {
  cleanup()
  vi.restoreAllMocks()
  localStorage.clear()
})

function wrapper({ children }: { children: ReactNode }) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false, staleTime: Infinity } } })
  return <QueryClientProvider client={client}>{children}</QueryClientProvider>
}

// A session whose agent made these tool calls. images answers the image API
// by path: a blob, or a status with the server's reason. stored answers each
// pasted image in turn: the path Legato kept it under, or its refusal.
function mockSession(toolCalls: unknown[], images: Record<string, Blob | { status: number; error: string }> = {}, stored: (string | { status: number; error: string })[] = []) {
  const answers = [...stored]
  return vi.spyOn(globalThis, "fetch").mockImplementation(async (input, init) => {
    const url = String(input)
    if (init?.method === "POST" && url === "/api/agents/task-1/image") {
      const answer = answers.shift() ?? { status: 500, error: "unexpected paste" }
      if (typeof answer === "string") return new Response(JSON.stringify({ path: answer }), { status: 201 })
      return new Response(JSON.stringify({ error: answer.error }), { status: answer.status })
    }
    if (url.includes("/snapshot")) {
      return new Response(JSON.stringify({
        id: "task-1", status: "idle", pendingPermissions: [],
        messages: [
          { role: "user", content: "You are an agent." },
          { role: "user", content: "Check the panel." },
          { role: "assistant", content: toolCalls.map(() => ({ type: "tool-call" })) },
        ],
        toolCalls,
      }), { status: 200 })
    }
    const image = /\/api\/agents\/task-1\/image\?path=(.+)$/.exec(url)
    if (image) {
      const found = images[decodeURIComponent(image[1])]
      if (found instanceof Blob) return new Response(found, { status: 200 })
      return new Response(JSON.stringify({ error: found?.error ?? "no such file" }), { status: found?.status ?? 404 })
    }
    return new Response("[]", { status: 200 })
  })
}

const png = () => new Blob([new Uint8Array([0x89, 0x50, 0x4e, 0x47])], { type: "image/png" })
const read = (callId: string, path: string) => ({ callId, name: "read", args: { path }, result: {} })

describe("NativeAgentPanel and images an agent read", () => {
  it("shows an image the agent read when its row is clicked", async () => {
    const fetchMock = mockSession([read("c1", "/tmp/shots/panel wide.png")], { "/tmp/shots/panel wide.png": png() })
    render(<NativeAgentPanel agentId="task-1" />, { wrapper })

    const row = await screen.findByRole("button", { name: /panel wide\.png/ })
    // Nothing is fetched until the person asks to see it.
    expect(fetchMock.mock.calls.some(([input]) => String(input).includes("/image"))).toBe(false)
    fireEvent.click(row)

    const image = await screen.findByRole("img", { name: "panel wide.png" })
    expect(image.getAttribute("src")).toBe("blob:image-1")
    expect(fetchMock.mock.calls.some(([input]) => String(input) === "/api/agents/task-1/image?path=%2Ftmp%2Fshots%2Fpanel%20wide.png")).toBe(true)
  })

  it("leaves a read of any other file as the plain line it was", async () => {
    const fetchMock = mockSession([read("c1", "/repo/internal/server/server.go")])
    render(<NativeAgentPanel agentId="task-1" />, { wrapper })

    expect(await screen.findByText("server.go")).toBeTruthy()
    expect(screen.queryByRole("button", { name: /server\.go/ })).toBeNull()
    expect(fetchMock.mock.calls.some(([input]) => String(input).includes("/image"))).toBe(false)
  })

  it("says why, in the server's words, when the image cannot be shown", async () => {
    mockSession([read("c1", "/tmp/shots/gone.png")], { "/tmp/shots/gone.png": { status: 404, error: "no such file" } })
    render(<NativeAgentPanel agentId="task-1" />, { wrapper })

    fireEvent.click(await screen.findByRole("button", { name: /gone\.png/ }))

    expect((await screen.findByRole("alert")).textContent).toBe("Cannot show this image: no such file")
    expect(screen.queryByRole("img")).toBeNull()
  })

  it("asks for a relative path as the agent wrote it: the server knows the session's directory", async () => {
    const fetchMock = mockSession([read("c1", "docs/shot.png")], { "docs/shot.png": png() })
    render(<NativeAgentPanel agentId="task-1" />, { wrapper })

    fireEvent.click(await screen.findByRole("button", { name: /shot\.png/ }))

    await screen.findByRole("img", { name: "shot.png" })
    expect(fetchMock.mock.calls.some(([input]) => String(input) === "/api/agents/task-1/image?path=docs%2Fshot.png")).toBe(true)
  })

  it("shows the image full size on a click, until Escape", async () => {
    mockSession([read("c1", "/tmp/shots/panel.png")], { "/tmp/shots/panel.png": png() })
    render(<NativeAgentPanel agentId="task-1" />, { wrapper })
    fireEvent.click(await screen.findByRole("button", { name: /^read.*panel\.png/ }))
    await screen.findByRole("img", { name: "panel.png" })

    fireEvent.click(screen.getByRole("button", { name: "Show panel.png full size" }))
    const dialog = screen.getByRole("dialog", { name: "panel.png, full size" })
    expect(dialog.querySelector("img")!.getAttribute("src")).toBe("blob:image-1")

    fireEvent.keyDown(window, { key: "Escape" })
    expect(screen.queryByRole("dialog", { name: "panel.png, full size" })).toBeNull()
    // The row stays open.
    expect(screen.getByRole("img", { name: "panel.png" })).toBeTruthy()
  })

  it("reads the file again each time the row is opened, and lets the old copy go", async () => {
    const fetchMock = mockSession([read("c1", "/tmp/shots/panel.png")], { "/tmp/shots/panel.png": png() })
    render(<NativeAgentPanel agentId="task-1" />, { wrapper })
    const row = await screen.findByRole("button", { name: /^read.*panel\.png/ })
    const asked = () => fetchMock.mock.calls.filter(([input]) => String(input).includes("/image")).length

    fireEvent.click(row)
    await screen.findByRole("img", { name: "panel.png" })
    fireEvent.click(row)
    await waitFor(() => expect(screen.queryByRole("img")).toBeNull())
    expect(revoked).toEqual(["blob:image-1"])

    fireEvent.click(row)
    await waitFor(() => expect(screen.getByRole("img", { name: "panel.png" }).getAttribute("src")).toBe("blob:image-2"))
    expect(asked()).toBe(2)
  })
})

const screenshot = () => new File([new Uint8Array([0x89, 0x50, 0x4e, 0x47])], "image.png", { type: "image/png" })
const pasteOf = (files: File[], text = "") => ({ clipboardData: { files, getData: (kind: string) => (kind === "text/plain" ? text : "") } })
const stored = (name: string) => `/tmp/legato-1000/pasted-images/task-1/${name}`

async function messageBox(): Promise<HTMLTextAreaElement> {
  return await screen.findByPlaceholderText("Message task-1") as HTMLTextAreaElement
}

describe("NativeAgentPanel and an image the person pastes", () => {
  it("gives the image to Legato and puts the path of the stored file in the message", async () => {
    const fetchMock = mockSession([], {}, [stored("pasted-1.png")])
    render(<NativeAgentPanel agentId="task-1" />, { wrapper })
    const box = await messageBox()
    fireEvent.change(box, { target: { value: "What is wrong here?" } })
    const image = screenshot()

    fireEvent.paste(box, pasteOf([image]))

    await waitFor(() => expect(box.value).toBe(`What is wrong here? ${stored("pasted-1.png")} `))
    const upload = fetchMock.mock.calls.find(([, init]) => init?.method === "POST")!
    expect(upload[1]!.body).toBe(image)
    // The person sees which image the path stands for.
    expect(screen.getByRole("img", { name: "Pasted image pasted-1.png" }).getAttribute("src")).toBe("blob:image-1")
  })

  it("sends the path as part of the message, and clears the thumbnail with it", async () => {
    mockSession([], {}, [stored("pasted-1.png")])
    render(<NativeAgentPanel agentId="task-1" />, { wrapper })
    const box = await messageBox()
    fireEvent.paste(box, pasteOf([screenshot()]))
    await screen.findByRole("img", { name: "Pasted image pasted-1.png" })

    fireEvent.click(screen.getByRole("button", { name: "Send" }))

    expect(send).toHaveBeenCalledWith(expect.objectContaining({ type: "agent_message", agent_id: "task-1", content: stored("pasted-1.png") }))
    await waitFor(() => expect(screen.queryByRole("img", { name: /Pasted image/ })).toBeNull())
    expect(revoked).toEqual(["blob:image-1"])
  })

  it("drops the thumbnail when the person deletes the path from the message", async () => {
    mockSession([], {}, [stored("pasted-1.png")])
    render(<NativeAgentPanel agentId="task-1" />, { wrapper })
    const box = await messageBox()
    fireEvent.paste(box, pasteOf([screenshot()]))
    await screen.findByRole("img", { name: "Pasted image pasted-1.png" })

    fireEvent.change(box, { target: { value: "never mind" } })

    await waitFor(() => expect(screen.queryByRole("img", { name: /Pasted image/ })).toBeNull())
  })

  it("says why, in the server's words, when an image is not taken, and leaves the message alone", async () => {
    mockSession([], {}, [{ status: 413, error: "the image is larger than 10 MB" }])
    render(<NativeAgentPanel agentId="task-1" />, { wrapper })
    const box = await messageBox()
    fireEvent.change(box, { target: { value: "Look" } })

    fireEvent.paste(box, pasteOf([screenshot()]))

    expect((await screen.findByRole("alert")).textContent).toContain("Image not added: the image is larger than 10 MB")
    expect(box.value).toBe("Look")
  })

  it("leaves a paste of text to the message box, with or without a picture of that text", async () => {
    const fetchMock = mockSession([])
    render(<NativeAgentPanel agentId="task-1" />, { wrapper })
    const box = await messageBox()

    // Text alone, and a copy from a spreadsheet: its text and a picture of it.
    expect(fireEvent.paste(box, pasteOf([], "plain words"))).toBe(true)
    expect(fireEvent.paste(box, pasteOf([screenshot()], "a\tb"))).toBe(true)

    expect(fetchMock.mock.calls.some(([, init]) => init?.method === "POST")).toBe(false)
  })
})
