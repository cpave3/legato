import type { ConfigList, ConfigListEntry } from "./configSettings"
import { fieldApplies, type FormField, type ListForm } from "./listForms"

// Reading and writing the value of a Settings list entry through its form:
// a field's key may be dotted (reviewer.tier), and an emptied object leaves
// the entry.

export type EntryValue = Record<string, unknown> | string

export function asObject(value: unknown): Record<string, unknown> {
  return value !== null && typeof value === "object" && !Array.isArray(value) ? value as Record<string, unknown> : {}
}

export function fieldValue(value: unknown, key: string): unknown {
  return key.split(".").reduce<unknown>((v, part) => asObject(v)[part], value)
}

const isEmptyObject = (value: unknown) => typeof value === "object" && value !== null && Object.keys(value).length === 0

export function writeField(object: Record<string, unknown>, key: string, value: unknown): Record<string, unknown> {
  const [head, ...tail] = key.split(".")
  const next = { ...object }
  const out = tail.length ? writeField(asObject(next[head]), tail.join("."), value) : value
  if (out === undefined || isEmptyObject(out)) delete next[head]
  else next[head] = out
  return next
}

// unusedName is base-N for the first N no row has.
export function unusedName(base: string, taken: string[]): string {
  for (let n = taken.length + 1; ; n++) if (!taken.includes(`${base}-${n}`)) return `${base}-${n}`
}

// duplicate is a name two rows share, or "".
export function duplicate(names: string[]): string {
  const seen = new Set<string>()
  for (const n of names.map((x) => x.trim()).filter(Boolean)) {
    if (seen.has(n)) return n
    seen.add(n)
  }
  return ""
}

// entryValue is an entry's value as its form edits it: a string for a
// scalar form, else an object.
export function entryValue(form: ListForm, value: unknown): EntryValue {
  if (form.scalar) return typeof value === "string" ? value : ""
  return asObject(value)
}

// parsedValue is a value read back from YAML as the form edits it.
export function parsedValue(form: ListForm, value: unknown): EntryValue {
  return form.scalar ? String(value ?? "") : asObject(value)
}

// firstError is the first refusal a field gave, or "".
export function firstError(errors: Record<string, string>): string {
  return Object.values(errors).find(Boolean) ?? ""
}

// setEntryField writes one field. A usage profile's tier means nothing to
// another agent, so changing the agent clears it; and a field that no
// longer applies (a draft rule's option on a rule that is not draft)
// leaves the entry.
export function setEntryField(listKey: string, form: ListForm, name: string, draft: EntryValue, key: string, next: unknown): Record<string, unknown> {
  let object = writeField(asObject(draft), key, next)
  if (listKey === "usage.profiles" && (key === "agent" || key === "reviewer.agent")) {
    object = writeField(object, key === "agent" ? "tier" : "reviewer.tier", undefined)
  }
  for (const field of form.fields) if (field.when && !fieldApplies(field, name, object)) object = writeField(object, field.key, undefined)
  return object
}

const listEntries = (lists: ConfigList[], key: string) => lists.find((l) => l.key === key)?.entries ?? []

// fieldChoiceNames is what a field with optionsFrom offers: the usage
// rule meters come from the server's usage catalog, not a list of our own;
// profiles and tiers come from the settings lists.
function fieldChoiceNames(field: FormField, draft: EntryValue, lists: ConfigList[], meters: string[]): string[] {
  if (field.optionsFrom === "usage_meters") return meters
  if (field.optionsFrom === "profiles") return listEntries(lists, "usage.profiles").map((e) => e.name)
  const agent = listEntries(lists, "adapters").find((e) => e.name === fieldValue(draft, field.agentKey ?? "agent"))
  return Object.keys(asObject(asObject(agent?.value).tiers))
}

// resolveField fills in the choices of a field that takes them from
// elsewhere: Claude accounts by label, else by name.
export function resolveField(field: FormField, draft: EntryValue, lists: ConfigList[], meters: string[]): FormField {
  if (!field.optionsFrom) return field
  if (field.optionsFrom === "claude_accounts") {
    return { ...field, choices: listEntries(lists, "claude_accounts").map((e) => [e.name, String(asObject(e.value).label || e.name)]) }
  }
  return { ...field, choices: fieldChoiceNames(field, draft, lists, meters).map((name) => [name, name]) }
}

// fieldSummary is one field's words in an entry's summary, or "".
function fieldSummary(field: FormField, v: unknown): string {
  if (v === undefined || v === "") return ""
  switch (field.kind) {
    case "choice": return field.choices?.find(([c]) => c === v)?.[1] ?? String(v)
    case "swatches": return ""
    case "check": return v === true ? field.label : ""
    default: return Array.isArray(v) ? v.join(" ") : String(v)
  }
}

// entrySummary says in a few words what an entry holds.
export function entrySummary(form: ListForm, entry: ConfigListEntry): string {
  if (form.summary) return form.summary(entry.name, asObject(entry.value))
  if (form.scalar) return typeof entry.value === "string" ? entry.value : ""
  const object = asObject(entry.value)
  return form.fields.map((f) => fieldSummary(f, fieldValue(object, f.key))).filter(Boolean).join(" · ")
}

// sortedEntries puts usage rules in the order they match: lower first,
// names breaking ties. Other lists keep the server's order.
export function sortedEntries(list: ConfigList): ConfigListEntry[] {
  if (list.key !== "usage.rules") return list.entries
  return [...list.entries].sort((a, b) => Number(asObject(a.value).order) - Number(asObject(b.value).order) || a.name.localeCompare(b.name))
}

// savedNotice says when a saved entry takes effect.
export function savedNotice(mode: ConfigList["mode"]): string {
  if (mode === "restart") return "Saved. It applies when Legato restarts."
  if (mode === "new_agents") return "Saved. It applies to agents started from now on."
  return "Saved."
}

export type Tier = { description?: string; launch_args?: string[] } & Record<string, unknown>
export type TierRow = { name: string; description: string; args: string; rest: Tier }

export function tierRows(value: unknown): TierRow[] {
  return (Object.entries(asObject(value)) as [string, Tier][])
    .map(([name, tier]) => ({ name, description: tier.description ?? "", args: (tier.launch_args ?? []).join("\n"), rest: tier }))
}

// tierRowsError is why the rows cannot be given back as tiers, or "".
export function tierRowsError(rows: TierRow[]): string {
  const twice = duplicate(rows.map((r) => r.name))
  if (twice) return `Two tiers are named ${twice}.`
  if (rows.some((r) => !r.name.trim())) return "Give every tier a name."
  return ""
}

// tiersOf gives rows back as tiers, keeping the fields a row does not show;
// launch arguments are one per line.
export function tiersOf(rows: TierRow[]): Record<string, Tier> | undefined {
  const tiers = rows.map((r): [string, Tier] => {
    const rest: Tier = { ...r.rest }
    delete rest.description
    delete rest.launch_args
    const args = r.args.split("\n").map((a) => a.trim()).filter(Boolean)
    return [r.name.trim(), { ...rest, ...(r.description.trim() ? { description: r.description } : {}), ...(args.length ? { launch_args: args } : {}) }]
  })
  return tiers.length ? Object.fromEntries(tiers) : undefined
}
