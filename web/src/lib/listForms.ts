import { dump, load } from "js-yaml"

// The forms that edit the entries of Settings' lists, in place of JSON.
// Each field writes one key of the entry; keys a form does not know are
// kept, and "Edit as YAML" edits the whole entry in config.yaml's shape.

export interface FormField {
  key: string
  label: string
  help?: string
  optional?: boolean
  kind: "text" | "lines" | "bool" | "choice" | "swatches" | "textarea"
  choices?: [string, string][]
  placeholder?: string
  // bool: what unset means.
  unsetLabel?: string
}

export interface ListForm {
  // What an entry's name is called, and what it is for.
  name: { label: string; help?: string; placeholder?: string }
  // The fields of an entry that is an object; or `scalar`, the one value of
  // an entry that is a string.
  fields: FormField[]
  scalar?: FormField
  // The words of the empty list, and of the button that adds the first.
  empty: string
  add: string
  // Whether the list keeps its entries' order (the Reeve's rules).
  ordered?: string
}

export const workspaceColors: [string, string][] = [
  ["#4A9EEF", "Blue"], ["#2DD4BF", "Teal"], ["#3ECF8E", "Green"], ["#F0B44C", "Amber"],
  ["#EF6461", "Red"], ["#E879B9", "Pink"], ["#A99CFF", "Purple"], ["#9A99A5", "Grey"],
]

export const listForms: Record<string, ListForm> = {
  workspaces: {
    name: { label: "Name", placeholder: "infra" },
    fields: [
      { key: "color", label: "Colour", kind: "swatches", choices: workspaceColors, help: "Shown on its cards and in the workspace menu." },
      { key: "path", label: "Folder", kind: "text", optional: true, placeholder: "~/src/infra", help: "Where a swarm started from one of its cards works." },
    ],
    empty: "No workspaces.", add: "New workspace",
  },
  "agents.tmux_options": {
    name: { label: "Option", placeholder: "mouse", help: "A tmux option, set on each agent's session." },
    fields: [],
    scalar: { key: "", label: "Value", kind: "text", placeholder: "on" },
    empty: "No tmux options.", add: "New option",
  },
  "agents.sandbox.mounts": {
    name: { label: "Folder", placeholder: "~/notes", help: "A folder every sandboxed agent sees." },
    fields: [{ key: "access", label: "Access", kind: "choice", choices: [["read", "Read only"], ["write", "Read and write"]], optional: true, help: "Read only unless chosen." }],
    empty: "No extra folders.", add: "Add a folder",
  },
  credential_sources: {
    name: { label: "Vault", placeholder: "Work", help: "A 1Password vault whose items become credentials." },
    fields: [{ key: "profile", label: "Profile", kind: "text", placeholder: "api-key", help: "The profile its items take, such as api-key or ssh." }],
    empty: "No credential sources.", add: "Add a vault",
  },
  credentials: {
    name: { label: "Name", placeholder: "github" },
    fields: [
      { key: "secret", label: "Secret", kind: "text", placeholder: "legato://github-token", help: "A secret's name, legato://name or op://vault/item/field, never its value." },
      { key: "profile", label: "Profile", kind: "text", placeholder: "github" },
      { key: "type", label: "Type", kind: "text", optional: true, placeholder: "value" },
    ],
    empty: "No credentials.", add: "Add a credential",
  },
}

// A form for a list it does not know: its name, and YAML.
export function formFor(listKey: string, nameField?: string): ListForm {
  return listForms[listKey] ?? {
    name: { label: nameField === "pattern" ? "Pattern" : nameField === "vault" ? "Vault" : nameField === "path" ? "Path" : "Name" },
    fields: [], empty: "None yet.", add: "Add",
  }
}

// The entry in config.yaml's shape: a list of objects shows its name
// field among the others; a map shows the name as the key.
export function entryYaml(name: string, value: unknown, nameField?: string): string {
  const shaped = nameField && value !== null && typeof value === "object" && !Array.isArray(value)
    ? { [nameField]: name, ...(value as Record<string, unknown>) }
    : { [name || "name"]: value }
  return dump(shaped, { lineWidth: 100, noRefs: true })
}

// parseEntryYaml reads an entry back from its YAML: its name and value.
export function parseEntryYaml(text: string, nameField?: string): { name: string; value: unknown } {
  const parsed = load(text)
  if (parsed === null || typeof parsed !== "object" || Array.isArray(parsed)) throw new Error("Write the entry as YAML fields, as in config.yaml.")
  const object = parsed as Record<string, unknown>
  if (nameField) {
    const { [nameField]: name, ...rest } = object
    if (typeof name !== "string" || !name.trim()) throw new Error(`Give the entry's ${nameField}.`)
    return { name, value: rest }
  }
  const keys = Object.keys(object)
  if (keys.length !== 1) throw new Error("Write one entry: its name, then its fields under it.")
  return { name: keys[0], value: object[keys[0]] }
}
