import { act, cleanup, renderHook, waitFor } from "@testing-library/react"
import type { ClipboardEvent, DragEvent } from "react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { usePastedImages } from "../usePastedImages"

vi.mock("../useServer", () => ({
  useServer: () => ({ baseUrl: "" }),
}))

const stored = (name: string) => `/tmp/legato-1000/pasted-images/task-1/${name}`
const png = (name = "image.png") => new File([new Uint8Array([0x89, 0x50, 0x4e, 0x47])], name, { type: "image/png" })

// Each image given to Legato answers with the next stored path, in turn.
function mockUploads(...paths: string[]) {
  const answers = [...paths]
  return vi.spyOn(globalThis, "fetch").mockImplementation(async (input, init) => {
    if (init?.method === "POST" && String(input) === "/api/agents/task-1/image") {
      const path = answers.shift()
      if (path) return new Response(JSON.stringify({ path }), { status: 201 })
      return new Response(JSON.stringify({ error: "only PNG, JPEG, GIF, WebP and BMP images can be pasted" }), { status: 415 })
    }
    return new Response("{}", { status: 404 })
  })
}
const uploads = (fetchMock: ReturnType<typeof mockUploads>) => fetchMock.mock.calls.filter(([, init]) => init?.method === "POST").map(([, init]) => init!.body)

// A paste event as a browser builds it: items (Chrome, Firefox, Safari),
// files (older browsers list the image there too) and the text.
interface Clip { items?: { kind: string; type: string; file?: File }[]; files?: File[]; text?: string }
function paste({ items = [], files = [], text = "" }: Clip) {
  const event = {
    clipboardData: {
      items: items.map((item) => ({ kind: item.kind, type: item.type, getAsFile: () => item.file ?? null })),
      files,
      types: [...new Set(items.map((item) => (item.kind === "file" ? "Files" : item.type)))],
      getData: (kind: string) => (kind === "text/plain" ? text : ""),
    },
    preventDefault: vi.fn(),
  }
  return event
}
const imageItem = (file: File) => ({ kind: "file", type: file.type, file })
const textItem = { kind: "string", type: "text/plain" }

function drop(files: File[], types = files.length > 0 ? ["Files"] : ["text/plain"]) {
  return { dataTransfer: { files, types, dropEffect: "none" }, preventDefault: vi.fn() }
}

function render() {
  let text = ""
  const insert = vi.fn((path: string) => { text = `${text}${path} ` })
  const hook = renderHook(({ current }) => usePastedImages("task-1", current, insert), { initialProps: { current: text } })
  return { hook, insert, rerender: () => hook.rerender({ current: text }) }
}

let clipboardRead: ReturnType<typeof vi.fn> | undefined

beforeEach(() => {
  URL.createObjectURL = vi.fn(() => "blob:image")
  URL.revokeObjectURL = vi.fn()
  clipboardRead = undefined
})

afterEach(() => {
  cleanup()
  vi.restoreAllMocks()
  Object.defineProperty(navigator, "clipboard", { value: undefined, configurable: true })
})

// The async Clipboard API, as WebKit answers it inside a paste event.
function clipboardWith(...blobs: Blob[]) {
  clipboardRead = vi.fn(async () => blobs.map((blob) => ({ types: [blob.type], getType: async () => blob })))
  Object.defineProperty(navigator, "clipboard", { value: { read: clipboardRead }, configurable: true })
}

describe("usePastedImages and a paste", () => {
  it("takes an image the clipboard lists as an item, and puts its path in the message", async () => {
    const fetchMock = mockUploads(stored("pasted-1.png"))
    const { hook, insert } = render()
    const image = png()
    const event = paste({ items: [imageItem(image)] })

    act(() => hook.result.current.onPaste(event as unknown as ClipboardEvent<HTMLTextAreaElement>))

    await waitFor(() => expect(insert).toHaveBeenCalledWith(stored("pasted-1.png")))
    expect(uploads(fetchMock)).toEqual([image])
    expect(event.preventDefault).toHaveBeenCalled()
  })

  it("takes an image that comes with text too, and lets the text be pasted as well", async () => {
    const fetchMock = mockUploads(stored("pasted-1.png"))
    const { hook, insert } = render()
    const image = png()
    const event = paste({ items: [textItem, imageItem(image)], text: "Screenshot.png" })

    act(() => hook.result.current.onPaste(event as unknown as ClipboardEvent<HTMLTextAreaElement>))

    await waitFor(() => expect(insert).toHaveBeenCalledWith(stored("pasted-1.png")))
    expect(uploads(fetchMock)).toEqual([image])
    expect(event.preventDefault).not.toHaveBeenCalled()
  })

  it("takes an image a browser lists only among the files", async () => {
    const fetchMock = mockUploads(stored("pasted-1.png"))
    const { hook, insert } = render()
    const image = png()

    act(() => hook.result.current.onPaste(paste({ files: [image] }) as unknown as ClipboardEvent<HTMLTextAreaElement>))

    await waitFor(() => expect(insert).toHaveBeenCalledWith(stored("pasted-1.png")))
    expect(uploads(fetchMock)).toEqual([image])
  })

  it("gives an image the clipboard lists as both an item and a file to Legato once", async () => {
    const fetchMock = mockUploads(stored("pasted-1.png"), stored("pasted-2.png"))
    const { hook, insert } = render()
    const image = png()

    act(() => hook.result.current.onPaste(paste({ items: [imageItem(image)], files: [image] }) as unknown as ClipboardEvent<HTMLTextAreaElement>))

    await waitFor(() => expect(insert).toHaveBeenCalledTimes(1))
    expect(uploads(fetchMock)).toHaveLength(1)
  })

  // WebKitGTK (the desktop app on Linux) fires the paste event with nothing
  // in it when the clipboard holds an image; the Clipboard API has it.
  it("asks the clipboard for an image when the paste event carries nothing", async () => {
    const fetchMock = mockUploads(stored("pasted-1.png"))
    const image = new Blob([new Uint8Array([0x89, 0x50, 0x4e, 0x47])], { type: "image/png" })
    clipboardWith(image)
    const { hook, insert } = render()

    act(() => hook.result.current.onPaste(paste({}) as unknown as ClipboardEvent<HTMLTextAreaElement>))

    await waitFor(() => expect(insert).toHaveBeenCalledWith(stored("pasted-1.png")))
    expect(uploads(fetchMock)).toHaveLength(1)
    expect((uploads(fetchMock)[0] as Blob).type).toBe("image/png")
  })

  it("leaves a paste of text alone, and does not ask the clipboard", async () => {
    const fetchMock = mockUploads()
    clipboardWith(png())
    const { hook } = render()
    const event = paste({ items: [textItem], text: "plain words" })

    act(() => hook.result.current.onPaste(event as unknown as ClipboardEvent<HTMLTextAreaElement>))

    expect(event.preventDefault).not.toHaveBeenCalled()
    expect(clipboardRead).not.toHaveBeenCalled()
    expect(uploads(fetchMock)).toEqual([])
  })

  it("says nothing when the paste carries nothing and the clipboard has no image", async () => {
    const fetchMock = mockUploads()
    clipboardWith(new Blob(["words"], { type: "text/plain" }))
    const { hook } = render()

    act(() => hook.result.current.onPaste(paste({}) as unknown as ClipboardEvent<HTMLTextAreaElement>))

    await waitFor(() => expect(clipboardRead).toHaveBeenCalled())
    expect(uploads(fetchMock)).toEqual([])
    expect(hook.result.current.error).toBeNull()
  })
})

describe("usePastedImages and images attached or dropped", () => {
  it("gives each image chosen in the picker to Legato and puts each path in the message", async () => {
    const fetchMock = mockUploads(stored("pasted-1.png"), stored("pasted-2.png"))
    const { hook, insert } = render()
    const [first, second] = [png("a.png"), png("b.png")]

    act(() => hook.result.current.attach([first, second]))

    await waitFor(() => expect(insert).toHaveBeenCalledTimes(2))
    expect(uploads(fetchMock)).toEqual([first, second])
  })

  it("refuses a file that is not an image, by its name", async () => {
    const fetchMock = mockUploads()
    const { hook } = render()

    act(() => hook.result.current.attach([new File(["x"], "notes.txt", { type: "text/plain" })]))

    expect(hook.result.current.error).toBe("notes.txt is not an image")
    expect(uploads(fetchMock)).toEqual([])
  })

  it("takes images dropped on the message box", async () => {
    const fetchMock = mockUploads(stored("pasted-1.png"))
    const { hook, insert } = render()
    const image = png()
    const over = drop([])
    over.dataTransfer.types = ["Files"]

    act(() => hook.result.current.dropTarget.onDragOver(over as unknown as DragEvent<HTMLElement>))
    expect(over.preventDefault).toHaveBeenCalled()
    expect(hook.result.current.dragging).toBe(true)

    const dropped = drop([image])
    act(() => hook.result.current.dropTarget.onDrop(dropped as unknown as DragEvent<HTMLElement>))

    expect(dropped.preventDefault).toHaveBeenCalled()
    expect(hook.result.current.dragging).toBe(false)
    await waitFor(() => expect(insert).toHaveBeenCalledWith(stored("pasted-1.png")))
    expect(uploads(fetchMock)).toEqual([image])
  })

  it("leaves a drag of text to the message box", () => {
    const { hook } = render()
    const over = drop([], ["text/plain"])

    act(() => hook.result.current.dropTarget.onDragOver(over as unknown as DragEvent<HTMLElement>))

    expect(over.preventDefault).not.toHaveBeenCalled()
    expect(hook.result.current.dragging).toBe(false)
  })
})
