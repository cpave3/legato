import { dump, load } from "js-yaml"

// The forms that edit the entries of Settings' lists, in place of JSON.
// Each field writes one key of the entry; keys a form does not know are
// kept, and "Edit as YAML" edits the whole entry in config.yaml's shape.

export interface FormField {
  key: string
  label: string
  help?: string
  optional?: boolean
  kind: "text" | "lines" | "bool" | "check" | "choice" | "swatches" | "textarea" | "tiers" | "pairs" | "number"
  choices?: [string, string][]
  optionsFrom?: "profiles" | "tiers" | "usage_meters" | "claude_accounts"
  agentKey?: string
  placeholder?: string
  // bool: what unset means.
  unsetLabel?: string
  // Only for the entries of these names (a field one agent alone reads).
  only?: string[]
  // Only while another field of the entry has this value; dropped from the
  // entry when that field changes to another.
  when?: { key: string; is: string }
  // pairs: what the key and the value of a pair are called.
  pairLabels?: [string, string]
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
  // Each entry drawn as a card of its own, with a summary.
  cards?: boolean
  summary?: (name: string, value: Record<string, unknown>) => string
}

const agentLabels: Record<string, string> = { "claude-code": "Claude Code", codex: "Codex", chimera: "Chimera" }
export const agentLabel = (name: string) => agentLabels[name] ?? name

function adapterSummary(name: string, value: Record<string, unknown>): string {
  const parts: string[] = []
  const tiers = Object.keys((value.tiers as Record<string, unknown> | undefined) ?? {}).length
  const models = Array.isArray(value.models) ? value.models.length : 0
  const args = Array.isArray(value.launch_args) ? value.launch_args.length : 0
  if (tiers) parts.push(`${tiers} ${tiers === 1 ? "tier" : "tiers"}`)
  if (models) parts.push(`${models} ${models === 1 ? "model" : "models"}`)
  if (args) parts.push(`launch: ${(value.launch_args as string[]).join(" ")}`)
  if (name !== "chimera") parts.push(value.native_bridge === false ? "a terminal only" : "connected to Legato")
  return parts.join(" · ") || "Default"
}

export const workspaceColors: [string, string][] = [
  ["#4A9EEF", "Blue"], ["#2DD4BF", "Teal"], ["#3ECF8E", "Green"], ["#F0B44C", "Amber"],
  ["#EF6461", "Red"], ["#E879B9", "Pink"], ["#A99CFF", "Purple"], ["#9A99A5", "Grey"],
]

export const listForms: Record<string, ListForm> = {
  claude_accounts: {
    name: { label: "Name", placeholder: "work" },
    fields: [
      { key: "label", label: "Display name", kind: "text", optional: true },
      { key: "config_dir", label: "Config directory", kind: "text", help: "Absolute account-local directory. Sign in there with Claude's CLI before launching." },
    ],
    empty: "No named Claude accounts.", add: "New account",
    summary: (name, value) => String(value.label || name),
  },
  "usage.profiles": {
    name: { label: "Name" },
    fields: [
      { key: "agent", label: "Agent", kind: "choice", choices: Object.entries(agentLabels) },
      { key: "account", label: "Account", kind: "choice", optional: true, unsetLabel: "Default account", optionsFrom: "claude_accounts", when: { key: "agent", is: "claude-code" } },
      { key: "tier", label: "Tier", kind: "choice", optional: true, unsetLabel: "Agent default", optionsFrom: "tiers", agentKey: "agent" },
      { key: "reviewer.agent", label: "Reviewer agent", kind: "choice", optional: true, unsetLabel: "Usual reviewer", choices: Object.entries(agentLabels) },
      { key: "reviewer.account", label: "Reviewer account", kind: "choice", optional: true, unsetLabel: "Default account", optionsFrom: "claude_accounts", when: { key: "reviewer.agent", is: "claude-code" } },
      { key: "reviewer.tier", label: "Reviewer tier", kind: "choice", optional: true, unsetLabel: "Reviewer default", optionsFrom: "tiers", agentKey: "reviewer.agent" },
    ],
    empty: "No usage profiles.", add: "New profile",
    summary: (_name, value) => [value.agent, value.tier].filter(Boolean).join(" · "),
  },
  "usage.rules": {
    name: { label: "Name" },
    fields: [
      { key: "order", label: "Order", kind: "number", help: "Lower numbers match first; names break ties." },
      { key: "meter", label: "Meter", kind: "choice", optionsFrom: "usage_meters" },
      { key: "at_least", label: "At least (%)", kind: "number" },
      { key: "profile", label: "Profile", kind: "choice", optionsFrom: "profiles" },
    ],
    empty: "No usage rules.", add: "New rule",
    ordered: "Lower order matches first; names break ties. The first matching rule chooses the profile. With no match, the fallback profile applies.",
  },
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
  "pull_requests.rules": {
    name: { label: "Repository", placeholder: "owner/name or owner/*", help: "A GitHub repository, or a pattern with *: owner/*, */name, or * for all. An exact name beats a pattern, then the pattern with more of the name; case is ignored." },
    fields: [
      { key: "mode", label: "Pull requests", kind: "choice", choices: [["draft", "As drafts"], ["ready", "Ready for review"], ["agent", "The agent chooses"]], help: "As drafts: Legato refuses any other. Ready for review: a handoff may open them without asking. Agents are told which." },
      { key: "ready_after_review", label: "Mark ready once reviewed", kind: "check", optional: true, when: { key: "mode", is: "draft" }, help: "A handed-off task's drafts are marked ready for review, bottom first, once review passes approve them. Legato never merges." },
    ],
    empty: "No rules: handoffs open drafts and other agents choose.", add: "Add a rule",
  },
  credential_sources: {
    name: { label: "Vault", placeholder: "op://Work", help: "A 1Password vault, as op://<vault>, whose items become credentials." },
    fields: [{ key: "profile", label: "Profile", kind: "choice", optional: true, unsetLabel: "SSH keys and secrets", choices: [["ssh", "SSH keys only"], ["secret", "Secrets only"]], help: "Which of its items it gives." }],
    empty: "No credential sources.", add: "Add a vault",
  },
  adapters: {
    name: { label: "Agent", placeholder: "claude-code", help: "claude-code, codex or chimera." },
    fields: [
      { key: "launch_args", label: "Launch arguments", kind: "lines", optional: true, placeholder: "--permission-mode auto", help: "One per line, added to every start of this agent." },
      { key: "tiers", label: "Tiers", kind: "tiers", optional: true, help: "Named launch profiles, picked per macro, per swarm part, and for the Reeve and reviewers. The conductor reads what each is for." },
      { key: "models", label: "Models in the model menu", kind: "lines", optional: true, only: ["claude-code"], placeholder: "claude-opus-5-5", help: "Full model IDs, one per line." },
      { key: "native_bridge", label: "Connect to Legato", kind: "bool", optional: true, only: ["claude-code", "codex"], unsetLabel: "On (default)", help: "Lets Legato message, interrupt and approve directly. Off, the agent is a terminal only." },
      { key: "modes", label: "Modes", kind: "pairs", optional: true, only: ["chimera"], pairLabels: ["Role", "Mode"], help: "Chimera's --mode for each role: conductor, worker, or a role of a plan." },
    ],
    empty: "No agent set up here: each runs as installed.", add: "Set up an agent", cards: true, summary: adapterSummary,
  },
  "swarm.prompts": {
    name: { label: "Role", placeholder: "backend", help: "conductor, worker, or a role a plan names." },
    fields: ["claude-code", "codex", "chimera"].map((agent) => ({
      key: agent, label: `${agentLabel(agent)}'s prompt`, kind: "textarea" as const, optional: true,
      help: `Replaces the built-in prompt for this role when the agent is ${agentLabel(agent)}.`,
    })),
    empty: "No role prompts: every role uses the built-in prompt.", add: "Add a role prompt",
    summary: (_name, value) => Object.keys(value).map(agentLabel).join(", "),
  },
  "reeve.rules": {
    name: { label: "Pattern", placeholder: "config.*", help: "The calls it matches, as config.* or task.update." },
    fields: [{ key: "level", label: "Needs", kind: "choice", choices: [["auto", "Nothing: runs at once"], ["approve", "A tap"], ["signed", "A signature"], ["verified", "A signature with a fingerprint or PIN"]] }],
    empty: "No rules: each change needs what its class needs.", add: "Add a rule",
    ordered: "Rules apply in order, and the last that matches wins. Those set here come after config.yaml's, in name order. A change waits for your signature.",
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
// fieldApplies says whether a field belongs to an entry of this name and
// value (FormField.only, FormField.when).
export function fieldApplies(field: FormField, name: string, value: Record<string, unknown>): boolean {
  if (field.only && !field.only.includes(name.trim())) return false
  return !field.when || field.when.key.split(".").reduce<unknown>((node, key) => node !== null && typeof node === "object" ? (node as Record<string, unknown>)[key] : undefined, value) === field.when.is
}

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
