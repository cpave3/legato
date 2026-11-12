import { useRef } from "react"
import { ImagePlus } from "lucide-react"
import { composerToolButton } from "../lib/composer"

// AttachImageButton opens the system's image picker for an agent's message:
// on a phone that offers the photo library (and, where the browser or app
// allows, the camera); on a computer, a file dialog. What is chosen goes
// the way of a pasted image.
export function AttachImageButton({ onFiles, disabled }: { onFiles: (files: File[]) => void; disabled?: boolean }) {
  const input = useRef<HTMLInputElement>(null)
  return (
    <>
      <button
        type="button"
        onClick={() => input.current?.click()}
        disabled={disabled}
        className={composerToolButton()}
        title="Attach image"
        aria-label="Attach image"
      >
        <ImagePlus size={16} aria-hidden="true" />
      </button>
      <input
        ref={input}
        type="file"
        accept="image/*"
        multiple
        hidden
        tabIndex={-1}
        aria-label="Images to attach"
        onChange={(event) => {
          const files = Array.from(event.target.files ?? [])
          // The same image can be chosen again next time.
          event.target.value = ""
          if (files.length > 0) onFiles(files)
        }}
      />
    </>
  )
}
