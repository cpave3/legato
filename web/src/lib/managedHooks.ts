import { apiFetch, ok } from "./api"
import { setFolderItemEnabled } from "./folderItems"

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
  // The agent tools that can run managed hooks (Claude Code in Phase 1);
  // empty from a server older than the spawn dialog's checkboxes.
  supportedTools: string[]
}

const path = "/api/settings/managed-hooks"

const strings = (value: unknown): string[] => Array.isArray(value) ? value.filter((item) => typeof item === "string") : []

export async function listManagedHooks(baseUrl: string): Promise<ManagedHookList> {
  const body = await (await ok(await apiFetch(baseUrl, path))).json()
  return {
    dir: typeof body?.dir === "string" ? body.dir : "",
    hooks: Array.isArray(body?.hooks) ? body.hooks : [],
    supportedTools: strings(body?.supported_tools),
  }
}

// The hooks an agent started with tool would be given, as far as the spawn
// dialog can tell: usable, on for every agent, for that tool, the tool one
// that runs managed hooks, and, for a read-only agent, one that opts in.
// The server decides again at the spawn, with the layers the dialog cannot
// see (the Reeve, a workflow, a handoff).
export function managedHooksFor(list: ManagedHookList, tool: string, readOnly = false): ManagedHook[] {
  if (!list.supportedTools.includes(tool)) return []
  return list.hooks.filter((hook) => !hook.error && hook.enabled &&
    (hook.agent_tools.length === 0 || hook.agent_tools.includes(tool)) && (!readOnly || hook.read_only))
}

export function setManagedHookEnabled(baseUrl: string, name: string, enabled: boolean): Promise<ManagedHook> {
  return setFolderItemEnabled<ManagedHook>(baseUrl, path, name, enabled)
}
