import { apiFetch } from "./api"

// A skill in Legato's skills folder: a folder with a SKILL.md. Those on and
// without an error are given to every agent Legato starts from then on.
export interface Skill {
  // The folder's name, which agents call it by.
  name: string
  description: string
  path: string
  // Why the folder is not a usable skill; absent when it is.
  error?: string
  enabled: boolean
}

export interface SkillList {
  dir: string
  skills: Skill[]
}

const path = "/api/settings/skills"

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

export async function listSkills(baseUrl: string): Promise<SkillList> {
  const body = await (await ok(await apiFetch(baseUrl, path))).json()
  return { dir: typeof body?.dir === "string" ? body.dir : "", skills: Array.isArray(body?.skills) ? body.skills : [] }
}

export async function setSkillEnabled(baseUrl: string, name: string, enabled: boolean): Promise<Skill> {
  const response = await ok(await apiFetch(baseUrl, `${path}/${encodeURIComponent(name)}`, {
    method: "PUT",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ enabled }),
  }))
  return response.json()
}
