import { apiFetch } from "./api"

// A resource is a named set of files and folders an agent can be given: an
// Obsidian vault, a folder of specs. Chimera is granted each path; other
// agents are told about them.
export interface ResourcePath {
  path: string
  access: "read" | "write"
}

export interface Resource {
  // 0 or absent for a resource from the configuration.
  id?: number
  name: string
  paths: ResourcePath[]
  // A resource from the configuration cannot be changed here.
  configured: boolean
}

async function ok(response: Response): Promise<Response> {
  if (response.ok) return response
  let message = response.statusText
  try {
    const body = await response.json()
    if (body && typeof body.error === "string") message = body.error
  } catch {
    // The body was not JSON; the status says enough.
  }
  throw new Error(message || `request failed (${response.status})`)
}

export async function listResources(baseUrl: string): Promise<Resource[]> {
  const response = await ok(await apiFetch(baseUrl, "/api/resources"))
  const body = await response.json()
  return Array.isArray(body?.resources) ? body.resources : []
}

export async function saveResource(baseUrl: string, resource: Pick<Resource, "id" | "name" | "paths">): Promise<Resource> {
  const path = resource.id ? `/api/resources/${resource.id}` : "/api/resources"
  const response = await ok(await apiFetch(baseUrl, path, {
    method: resource.id ? "PUT" : "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ name: resource.name, paths: resource.paths }),
  }))
  return response.json()
}

export async function deleteResource(baseUrl: string, id: number): Promise<void> {
  await ok(await apiFetch(baseUrl, `/api/resources/${id}`, { method: "DELETE" }))
}

// The names of the resources the running agent of a task was given.
export async function attachedResources(baseUrl: string, taskId: string): Promise<string[]> {
  const response = await ok(await apiFetch(baseUrl, `/api/agents/${encodeURIComponent(taskId)}/resources`))
  const body = await response.json()
  return Array.isArray(body?.attached) ? body.attached : []
}

// Gives the running agent of a task more resources, and returns the names of
// all it now has.
export async function attachResources(baseUrl: string, taskId: string, names: string[]): Promise<string[]> {
  const response = await ok(await apiFetch(baseUrl, `/api/agents/${encodeURIComponent(taskId)}/resources`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ names }),
  }))
  const body = await response.json()
  return Array.isArray(body?.attached) ? body.attached : []
}

// What a path's access reads as.
export function accessLabel(access: ResourcePath["access"]): string {
  return access === "write" ? "read and write" : "read only"
}
