import { apiFetch, ok } from "./api"

// The settings routes of things Legato finds in a folder on disk (skills,
// managed hooks): GET <path> answers {dir, <key>: [...]}, and
// PUT <path>/<name> {enabled} answers the item.

export async function listFolderItems<T>(baseUrl: string, path: string, key: string): Promise<{ dir: string; items: T[] }> {
  const body = await (await ok(await apiFetch(baseUrl, path))).json()
  return { dir: typeof body?.dir === "string" ? body.dir : "", items: Array.isArray(body?.[key]) ? body[key] : [] }
}

export async function setFolderItemEnabled<T>(baseUrl: string, path: string, name: string, enabled: boolean): Promise<T> {
  const response = await ok(await apiFetch(baseUrl, `${path}/${encodeURIComponent(name)}`, {
    method: "PUT",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ enabled }),
  }))
  return response.json()
}
