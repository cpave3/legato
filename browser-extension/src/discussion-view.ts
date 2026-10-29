import type { NativeConversationItem, NativePermissionItem } from "../../web/src/lib/nativeAgentSync"
import { linkFileReferences, type FileReference } from "./file-references"
import { renderSafeMarkdown } from "./markdown"

type Handlers = {
  onPermission: (requestId: string, decision: "allow" | "deny") => void
  onOpenFile?: (reference: FileReference) => void
}

export type DiscussionTone = "live" | "needs" | "idle"

/** What the agent is doing, in the words of the panel's header. */
export function discussionStatus(state: { connected: boolean; loading: boolean; busy: boolean; items: NativeConversationItem[] }): { label: string; tone: DiscussionTone } {
  if (!state.connected) return { label: "Reconnecting…", tone: "idle" }
  if (state.loading) return { label: "Loading…", tone: "idle" }
  const waiting = state.items.some((item) => item.permission?.status === "pending" || item.question?.status === "pending")
  if (waiting) return { label: "Needs you", tone: "needs" }
  return state.busy ? { label: "Working", tone: "live" } : { label: "Waiting for your question", tone: "idle" }
}

// Draws the conversation: what the person sent, the agent's replies as
// sanitized Markdown with the files it cites as buttons, a line for each
// tool call, and open requests. Thoughts
// are left out; the full detail is in Legato.
export function renderDiscussionFeed(feed: HTMLElement, items: NativeConversationItem[], handlers: Handlers): void {
  const doc = feed.ownerDocument
  const rows: HTMLElement[] = []
  for (const item of items) {
    if (item.permission) {
      rows.push(permissionCard(doc, item.permission, handlers))
      continue
    }
    if (item.question) {
      const row = element(doc, "div", "request", "question")
      row.append(
        element(doc, "div", "request-title", undefined, "Asks you"),
        element(doc, "div", "request-body", undefined, item.question.questions.map((question) => question.question).join("\n")),
        element(doc, "div", "request-note", undefined, item.question.status === "pending" ? "Answer it in Legato." : "Answered."),
      )
      rows.push(row)
      continue
    }
    if (item.tool || item.bang) {
      const row = element(doc, "div", "tool", "tool")
      const name = item.tool?.toolName ?? "shell"
      const what = item.tool ? toolSummary(item.tool.summary, item.tool.args) : `!${item.bang!.command}`
      row.append(element(doc, "span", "tool-name", undefined, name), element(doc, "span", "tool-what", undefined, what))
      rows.push(row)
      continue
    }
    if (!item.text.trim()) continue
    if (item.role === "user") {
      rows.push(element(doc, "div", "message user", "user", item.text))
    } else if (item.role === "assistant") {
      const row = element(doc, "div", "message assistant", "assistant")
      renderSafeMarkdown(row, item.text)
      if (handlers.onOpenFile) linkFileReferences(row, handlers.onOpenFile)
      rows.push(row)
    }
  }
  feed.replaceChildren(...rows)
}

function permissionCard(doc: Document, permission: NativePermissionItem, handlers: Handlers): HTMLElement {
  const card = element(doc, "div", "request", "permission")
  card.append(
    element(doc, "div", "request-title", undefined, permission.plan ? "Asks you to approve a plan" : `Wants to run ${permission.tool}`),
    element(doc, "pre", "request-body", undefined, permission.plan ?? permission.command),
  )
  if (permission.status === "pending") {
    const actions = element(doc, "div", "request-actions")
    for (const [label, decision] of [["Allow", "allow"], ["Deny", "deny"]] as const) {
      const button = element(doc, "button", decision === "allow" ? "accent" : "quiet", undefined, label) as HTMLButtonElement
      button.type = "button"
      button.addEventListener("click", () => handlers.onPermission(permission.requestId, decision))
      actions.append(button)
    }
    card.append(actions)
  } else {
    const settled: Record<string, string> = { submitting: "Sending…", allowed: "Allowed", denied: "Denied", expired: "Answered elsewhere" }
    card.append(element(doc, "div", "request-note", undefined, settled[permission.status] ?? permission.status))
  }
  return card
}

function toolSummary(summary: string | undefined, args: Record<string, unknown>): string {
  if (summary) return summary
  for (const key of ["command", "path", "file_path", "pattern", "query", "url"]) {
    if (typeof args[key] === "string") return args[key] as string
  }
  return ""
}

function element(doc: Document, tag: string, className?: string, role?: string, text?: string): HTMLElement {
  const node = doc.createElement(tag)
  if (className) node.className = className
  if (role) node.dataset.role = role
  if (text !== undefined) node.textContent = text
  return node
}
