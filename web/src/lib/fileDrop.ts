// keepFileDropsOnThePage stops a file dropped outside a drop target from
// replacing the page. A browser opens such a file in the tab; the desktop
// app, which hands drops to the page (so the message box can take images),
// would show the file in place of Legato. A drop target (the message box)
// handles its own drops first; this only claims those nothing took, and
// shows them as not droppable. It returns a function that removes it.
export function keepFileDropsOnThePage(target: Window = window): () => void {
  const carriesFiles = (event: DragEvent) => Array.from(event.dataTransfer?.types ?? []).includes("Files")
  const onDragOver = (event: DragEvent) => {
    if (!carriesFiles(event) || event.defaultPrevented) return
    event.preventDefault()
    if (event.dataTransfer) event.dataTransfer.dropEffect = "none"
  }
  const onDrop = (event: DragEvent) => {
    if (carriesFiles(event)) event.preventDefault()
  }
  target.addEventListener("dragover", onDragOver)
  target.addEventListener("drop", onDrop)
  return () => {
    target.removeEventListener("dragover", onDragOver)
    target.removeEventListener("drop", onDrop)
  }
}
