import { useCallback, useEffect, useRef, useState, type ClipboardEvent, type DragEvent } from "react"
import { useServer } from "./useServer"
import { uploadAgentImage } from "../lib/agentImage"

// An image the person pasted, attached or dropped, once Legato has stored it.
export interface PastedImage {
  // Where the file is on the machine the agent runs on.
  path: string
  // A local address for the same image, to draw its thumbnail.
  address: string
}

export interface PastedImages {
  // For the message box's onPaste.
  onPaste: (event: ClipboardEvent<HTMLTextAreaElement>) => void
  // Images the person chose with the attach button.
  attach: (files: Iterable<File>) => void
  // For the element images can be dropped on: the message box and what is
  // around it.
  dropTarget: {
    onDragOver: (event: DragEvent<HTMLElement>) => void
    onDragLeave: (event: DragEvent<HTMLElement>) => void
    onDrop: (event: DragEvent<HTMLElement>) => void
  }
  // Whether files are being dragged over the drop target now.
  dragging: boolean
  // The images whose path is still in the message.
  attached: PastedImage[]
  // How many images are on their way to Legato.
  uploading: number
  error: string | null
  dismissError: () => void
}

const isImage = (blob: Blob) => blob.type.startsWith("image/")

// imagesInPaste are the images a paste event carries. A browser lists an
// image among the items, and some also among the files, the same file
// twice; older ones only among the files.
function imagesInPaste(clipboard: DataTransfer): File[] {
  const fromItems = Array.from(clipboard.items ?? [])
    .filter((item) => item.kind === "file" && item.type.startsWith("image/"))
    .map((item) => item.getAsFile())
    .filter((file): file is File => file !== null)
  if (fromItems.length > 0) return fromItems
  return Array.from(clipboard.files ?? []).filter(isImage)
}

// imageOnClipboard asks the system clipboard for an image. WebKitGTK, the
// desktop app's engine on Linux, fires a paste of an image with nothing in
// the event, but answers the Clipboard API inside it. It must be asked
// while the paste is being handled, and only a secure page can.
async function imageOnClipboard(): Promise<File[]> {
  if (typeof navigator === "undefined" || !navigator.clipboard?.read) return []
  const images: File[] = []
  for (const item of await navigator.clipboard.read()) {
    const type = item.types.find((kind) => kind.startsWith("image/"))
    if (!type) continue
    const blob = await item.getType(type)
    images.push(new File([blob], `pasted.${type.slice("image/".length)}`, { type }))
  }
  return images
}

const carriesFiles = (event: DragEvent<HTMLElement>) => Array.from(event.dataTransfer?.types ?? []).includes("Files")

// usePastedImages lets the person put an image in an agent's message: paste
// it, choose it with the attach button, or drop it on the message box. An
// agent tool takes a message as text, so the image is stored by Legato as a
// file and its path is put in the message: every agent can read a file,
// whichever tool it runs under.
//
// text is the message as it is now; insert puts a path into it.
export function usePastedImages(agentId: string, text: string, insert: (path: string) => void): PastedImages {
  const { baseUrl } = useServer()
  const [images, setImages] = useState<PastedImage[]>([])
  const [uploading, setUploading] = useState(0)
  const [error, setError] = useState<string | null>(null)
  const [dragging, setDragging] = useState(false)
  const insertRef = useRef(insert)
  useEffect(() => { insertRef.current = insert })

  const upload = useCallback((files: File[]) => {
    for (const file of files) {
      setUploading((count) => count + 1)
      uploadAgentImage(baseUrl, agentId, file).then(
        (path) => {
          setImages((current) => [...current, { path, address: URL.createObjectURL(file) }])
          insertRef.current(path)
        },
        (reason: unknown) => setError(reason instanceof Error ? reason.message : String(reason)),
      ).finally(() => setUploading((count) => count - 1))
    }
  }, [agentId, baseUrl])

  const attach = useCallback((chosen: Iterable<File>) => {
    const files = Array.from(chosen)
    const other = files.find((file) => !isImage(file))
    if (other) {
      setError(`${other.name} is not an image`)
      return
    }
    setError(null)
    upload(files)
  }, [upload])

  const onPaste = useCallback((event: ClipboardEvent<HTMLTextAreaElement>) => {
    const clipboard = event.clipboardData
    if (!clipboard) return
    const found = imagesInPaste(clipboard)
    const text = clipboard.getData("text/plain")
    if (found.length > 0) {
      // Text that comes with an image (its file name, a copy of cells and a
      // picture of them) is still pasted; the image is added beside it.
      if (text === "") event.preventDefault()
      setError(null)
      upload(found)
      return
    }
    // Text alone is the message box's. A paste with nothing in it may be an
    // image that WebKitGTK did not put in the event.
    if (text !== "" || clipboard.types?.length > 0) return
    imageOnClipboard().then((onClipboard) => {
      if (onClipboard.length === 0) return
      setError(null)
      upload(onClipboard)
    }, () => {})
  }, [upload])

  const dropTarget = {
    onDragOver: useCallback((event: DragEvent<HTMLElement>) => {
      if (!carriesFiles(event)) return
      event.preventDefault()
      if (event.dataTransfer) event.dataTransfer.dropEffect = "copy"
      setDragging(true)
    }, []),
    onDragLeave: useCallback((event: DragEvent<HTMLElement>) => {
      // Leaving for a child of the target is not leaving it.
      const to = event.relatedTarget as Node | null
      if (to && event.currentTarget.contains?.(to)) return
      setDragging(false)
    }, []),
    onDrop: useCallback((event: DragEvent<HTMLElement>) => {
      setDragging(false)
      const files = Array.from(event.dataTransfer?.files ?? [])
      if (files.length === 0) return
      // A browser opens a file dropped anywhere it is not taken.
      event.preventDefault()
      attach(files)
    }, [attach]),
  }

  // A thumbnail belongs to a path in the message. When the path goes (the
  // message was sent, or the person deleted it) so does the thumbnail.
  useEffect(() => {
    const gone = images.filter((image) => !text.includes(image.path))
    if (gone.length === 0) return
    for (const image of gone) URL.revokeObjectURL(image.address)
    setImages((current) => current.filter((image) => !gone.includes(image)))
  }, [images, text])

  // Another agent's message box starts with nothing pasted.
  const imagesRef = useRef(images)
  useEffect(() => { imagesRef.current = images })
  useEffect(() => () => {
    for (const image of imagesRef.current) URL.revokeObjectURL(image.address)
    setImages([])
    setError(null)
  }, [agentId])

  return {
    onPaste,
    attach,
    dropTarget,
    dragging,
    attached: images.filter((image) => text.includes(image.path)),
    uploading,
    error,
    dismissError: useCallback(() => setError(null), []),
  }
}
