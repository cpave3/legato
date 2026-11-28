import { useState } from "react"
import { useToast } from "../hooks/useToast"
import { useConfirm } from "./ui/useConfirm"
import { buildStandaloneDocument, deleteAgentUIItem, inlineImages, itemImages, type AgentUIItem } from "../lib/agentUi"
import { artifactFileName, artifactLabel, artifactMediaType } from "../lib/artifactPanel"

function save(item: AgentUIItem, saved: string) {
  const url = URL.createObjectURL(new Blob([saved], { type: artifactMediaType(item) }))
  const link = document.createElement("a")
  link.href = url
  link.download = artifactFileName(item)
  link.click()
  URL.revokeObjectURL(url)
}

type Toast = ReturnType<typeof useToast>

async function copySource(item: AgentUIItem | null, toast: Toast) {
  if (!item?.content) return
  try {
    await navigator.clipboard.writeText(item.content)
    toast?.addToast("Source copied", "success")
  } catch {
    toast?.addToast("The browser did not allow copying", "error")
  }
}

function download(baseUrl: string, item: AgentUIItem | null, setBusy: (busy: boolean) => void) {
  if (!item?.content) return
  // Markdown and component cards are saved as the agent wrote them.
  if (item.format !== "html") {
    save(item, item.content)
    return
  }
  // HTML is saved as a whole page with Legato's theme in it: the agent's
  // styles depend on that theme, and outside Legato nothing supplies it.
  const page = (images: ReturnType<typeof itemImages>) => save(item, buildStandaloneDocument({
    html: item.content ?? "", title: artifactLabel(item), resourceDomains: item.resource_domains ?? [], images,
  }))
  const images = itemImages(baseUrl, item)
  if (images.length === 0) {
    page(images)
    return
  }
  // Its image files go into the page too, so it opens where Legato is not.
  setBusy(true)
  void inlineImages(images).then(page).finally(() => setBusy(false))
}

async function removeItem(baseUrl: string, item: AgentUIItem, toast: Toast) {
  try {
    await deleteAgentUIItem(baseUrl, item.id)
    toast?.addToast("Removed", "success")
  } catch (cause) {
    toast?.addToast(cause instanceof Error ? cause.message : "Could not remove it", "error")
  }
}

// useArtifactActions is what the artifact panel can do with the item it
// shows: copy its source, download it, and remove it. busy covers a download
// that is gathering image files and a removal in flight.
export function useArtifactActions(baseUrl: string, item: AgentUIItem | null, selected: AgentUIItem | undefined) {
  const toast = useToast()
  const { confirm, dialog: confirmDialog } = useConfirm()
  const [busy, setBusy] = useState(false)

  async function remove() {
    if (!selected || busy) return
    if (!await confirm({ title: "Remove artifact", message: `Remove "${artifactLabel(selected)}" and all of its revisions? The agent can show it again.`, confirmLabel: "Remove artifact", destructive: true })) return
    setBusy(true)
    await removeItem(baseUrl, selected, toast)
    setBusy(false)
  }

  return {
    busy,
    copySource: () => copySource(item, toast),
    download: () => download(baseUrl, item, setBusy),
    remove,
    confirmDialog,
  }
}
