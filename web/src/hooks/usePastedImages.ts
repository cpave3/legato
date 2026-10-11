import { useCallback, useEffect, useRef, useState, type ClipboardEvent } from "react"
import { useServer } from "./useServer"
import { uploadAgentImage } from "../lib/agentImage"

// An image the person pasted, once Legato has stored it.
export interface PastedImage {
  // Where the file is on the machine the agent runs on.
  path: string
  // A local address for the same image, to draw its thumbnail.
  address: string
}

export interface PastedImages {
  // For the message box's onPaste.
  onPaste: (event: ClipboardEvent<HTMLTextAreaElement>) => void
  // The pasted images whose path is still in the message.
  attached: PastedImage[]
  // How many images are on their way to Legato.
  uploading: number
  error: string | null
  dismissError: () => void
}

// usePastedImages lets the person paste an image into an agent's message
// box. An agent tool takes a message as text, so the image is stored by
// Legato as a file and its path is put in the message: every agent can read
// a file, whichever tool it runs under.
//
// text is the message as it is now; insert puts a path into it.
export function usePastedImages(agentId: string, text: string, insert: (path: string) => void): PastedImages {
  const { baseUrl } = useServer()
  const [images, setImages] = useState<PastedImage[]>([])
  const [uploading, setUploading] = useState(0)
  const [error, setError] = useState<string | null>(null)
  const insertRef = useRef(insert)
  useEffect(() => { insertRef.current = insert })

  const onPaste = useCallback((event: ClipboardEvent<HTMLTextAreaElement>) => {
    const clipboard = event.clipboardData
    const files = Array.from(clipboard?.files ?? []).filter((file) => file.type.startsWith("image/"))
    // A copy from a spreadsheet or a document carries its text and a picture
    // of it. The person wants the text there.
    if (files.length === 0 || clipboard.getData("text/plain") !== "") return
    event.preventDefault()
    setError(null)
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
    attached: images.filter((image) => text.includes(image.path)),
    uploading,
    error,
    dismissError: useCallback(() => setError(null), []),
  }
}
