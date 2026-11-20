// Operator items, the second kind of item in the Legato widget's feed
// (Feed.tsx): what agents posted for the person with for_operator, from
// each server's needs-you rail (GET /api/attention?view=rail, so the rail's
// settings apply), as Rust lists them (voice_items, the voice-items event;
// src-tauri/src/widgetitems.rs).

export type ItemAction = { id: string; label: string; kind: string; requires_text?: boolean; text?: string; href?: string }

// An Attention item, as much of it as the widget uses.
export type Item = {
  id: string
  producer: string
  signal_kind: string
  source_revision: string
  task_id?: string
  title: string
  reason?: string
  navigation: { kind: string; target: string }
  updated_at?: string
  actions?: ItemAction[]
}

// An item and the server it waits on.
export type WidgetItem = { server: string; server_name: string; tint?: string | null; item: Item }

// The word for each kind, as the web UI's rail says it.
export const itemKinds: Record<string, string> = {
  operator_action: "Action",
  operator_question: "Question",
  operator_done: "Done",
  operator_other: "Note",
}

export function itemKind(item: Item): string {
  return itemKinds[item.signal_kind] ?? "Note"
}

// What the widget can do about an item: a question's answer needs the
// keyboard, which the widget never takes, so it is given in the main window;
// anything else can be marked done here when the item has Done.
export function canMarkDone(item: Item): boolean {
  return item.signal_kind !== "operator_question" && (item.actions ?? []).some((a) => a.id === "done" && a.kind === "execute")
}
