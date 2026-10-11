import { apiFetch } from "./api"

// The kinds of image file the server gives out (agentImageTypes in
// internal/server/agent_image.go). The server decides by a file's content;
// the name only says which rows are worth offering.
const IMAGE_PATH = /\.(png|jpe?g|gif|webp|bmp)$/i

// isImagePath says whether a file an agent read is an image Legato can show.
export function isImagePath(path: string): boolean {
  return IMAGE_PATH.test(path)
}

// fetchAgentImage reads an image file from the machine the agent runs on,
// through Legato's server. A relative path is taken from the session's
// working directory. It fails with the server's own reason.
export async function fetchAgentImage(baseUrl: string, taskId: string, path: string): Promise<Blob> {
  const response = await apiFetch(baseUrl, `/api/agents/${encodeURIComponent(taskId)}/image?path=${encodeURIComponent(path)}`)
  if (!response.ok) {
    const body = await response.json().catch(() => null) as { error?: string } | null
    throw new Error(body?.error ?? `request failed (${response.status})`)
  }
  return response.blob()
}

// uploadAgentImage gives Legato an image the person pasted for an agent.
// Legato keeps it as a file on the machine the agent runs on and answers
// with the file's path, which is what the message to the agent names.
export async function uploadAgentImage(baseUrl: string, taskId: string, image: Blob): Promise<string> {
  const response = await apiFetch(baseUrl, `/api/agents/${encodeURIComponent(taskId)}/image`, {
    method: "POST",
    headers: { "Content-Type": image.type || "application/octet-stream" },
    body: image,
  })
  const body = await response.json().catch(() => null) as { path?: string; error?: string } | null
  if (!response.ok || !body?.path) throw new Error(body?.error ?? `request failed (${response.status})`)
  return body.path
}

// insertAtCursor puts text into a message box's text where the cursor is
// (at the end when the box is not known), set off from its neighbours by a
// space, and never replaces what the person typed.
export function insertAtCursor(text: string, addition: string, cursor?: number | null): string {
  const at = typeof cursor === "number" && cursor >= 0 && cursor <= text.length ? cursor : text.length
  const [before, after] = [text.slice(0, at), text.slice(at)]
  const lead = before === "" || /\s$/.test(before) ? "" : " "
  const trail = /^\s/.test(after) ? "" : " "
  return `${before}${lead}${addition}${trail}${after}`
}
