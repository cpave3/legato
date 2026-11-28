import { listFolderItems, setFolderItemEnabled } from "./folderItems"

// A hook in Legato's managed-hooks folder: a folder with a HOOK.yaml. Those
// on and without an error are given to the agents Legato starts from then on.
export interface ManagedHook {
  // The folder's name, which the manifest's name must equal.
  name: string
  description: string
  // The events it runs at: session_start, pre_tool, post_tool, stop.
  events: string[]
  // The agent tools it is for; empty is every one.
  agent_tools: string[]
  // Whether read-only agents get it too.
  read_only: boolean
  path: string
  // Why the folder is not a usable hook; absent when it is.
  error?: string
  enabled: boolean
}

export interface ManagedHookList {
  dir: string
  hooks: ManagedHook[]
}

const path = "/api/settings/managed-hooks"

export async function listManagedHooks(baseUrl: string): Promise<ManagedHookList> {
  const { dir, items } = await listFolderItems<ManagedHook>(baseUrl, path, "hooks")
  return { dir, hooks: items }
}

export function setManagedHookEnabled(baseUrl: string, name: string, enabled: boolean): Promise<ManagedHook> {
  return setFolderItemEnabled<ManagedHook>(baseUrl, path, name, enabled)
}
