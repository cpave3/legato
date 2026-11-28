import { listFolderItems, setFolderItemEnabled } from "./folderItems"

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

export async function listSkills(baseUrl: string): Promise<SkillList> {
  const { dir, items } = await listFolderItems<Skill>(baseUrl, path, "skills")
  return { dir, skills: items }
}

export function setSkillEnabled(baseUrl: string, name: string, enabled: boolean): Promise<Skill> {
  return setFolderItemEnabled<Skill>(baseUrl, path, name, enabled)
}
