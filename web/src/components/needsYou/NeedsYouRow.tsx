import { useState } from "react"
import { Link } from "react-router-dom"
import {
  AlertTriangle, Bell, CheckCircle, CircleHelp, Copy, DoorOpen, ExternalLink, GitMerge, GitPullRequest,
  Hourglass, ListChecks, MessageSquare, ShieldCheck, SquareTerminal, XOctagon, type LucideIcon,
} from "lucide-react"
import type { AttentionAction, AttentionItem } from "../../lib/attention"
import { externalHref, legatoHref } from "../../lib/navigation"
import { itemHow, needsYouKind, needsYouKinds, shortAge, type NeedsYouKind, type NeedsYouTone } from "../../lib/needsYou"

export const kindIcons: Record<NeedsYouKind, LucideIcon> = {
  approval: ShieldCheck,
  action: SquareTerminal,
  question: CircleHelp,
  done: CheckCircle,
  other: MessageSquare,
  waiting: Hourglass,
  error: XOctagon,
  failed: AlertTriangle,
  gate: DoorOpen,
  plan: ListChecks,
  review: GitPullRequest,
  merge: GitMerge,
  legato: Bell,
}

export const toneClass: Record<NeedsYouTone, string> = {
  needs: "bg-lw-needs-soft text-lw-needs",
  question: "bg-lw-accent-soft text-lw-accent-text",
  error: "bg-lw-error-soft text-lw-error",
  done: "bg-lw-done-soft text-lw-done",
  plain: "bg-lw-idle-soft text-lw-text-2",
}

// The kind of an item: its word and its own icon, on its tone.
export function KindTag({ item }: { item: AttentionItem }) {
  const kind = needsYouKind(item)
  const Icon = kindIcons[kind]
  const { word, tone } = needsYouKinds[kind]
  return (
    <span className={`inline-flex items-center gap-1 rounded px-1.5 py-0.5 text-[11px] font-semibold ${toneClass[tone]}`}>
      <Icon size={12} aria-hidden="true" />{word}
    </span>
  )
}

const button = "inline-flex min-h-7 items-center gap-1.5 rounded-md border border-lw-line-strong/40 bg-lw-raised px-2.5 text-xs font-medium text-lw-text hover:bg-lw-selected disabled:opacity-50"
const quiet = "inline-flex min-h-7 items-center gap-1.5 rounded-md px-1.5 text-xs text-lw-text-2 hover:text-lw-text disabled:opacity-50"

function hrefOf(action: AttentionAction, item: AttentionItem): { path: string } | { external: string } | null {
  if (action.href) {
    if (action.href.startsWith("/")) return { path: action.href }
    const external = externalHref({ kind: "external", target: action.href })
    return external ? { external } : null
  }
  const external = externalHref(item.navigation)
  if (external) return { external }
  const path = legatoHref(item.navigation)
  return path ? { path } : null
}

export interface NeedsYouRowProps {
  item: AttentionItem
  // Runs an execute action, with the operator's text for one that needs it.
  onAction: (item: AttentionItem, actionId: string, text?: string) => void
  onDismiss: (item: AttentionItem) => void
  // The item is the agent's in view: outlined and labelled "this agent".
  here?: boolean
  pending?: boolean
}

// NeedsYouRow draws one item that needs the operator: what kind it is, who
// posted it and when, what is needed, what it blocks, how to do it, and its
// actions. The rail, the phone sheet and a run's Needs you list share it.
export function NeedsYouRow({ item, onAction, onDismiss, here, pending }: NeedsYouRowProps) {
  const [replying, setReplying] = useState(false)
  const [text, setText] = useState("")
  const [copied, setCopied] = useState(false)
  const { copy, link } = itemHow(item)
  const actions = item.actions ?? []
  const executes = actions.filter((action) => action.kind === "execute" && !action.requires_text)
  const reply = actions.find((action) => action.kind === "execute" && action.requires_text)
  const navigates = actions.filter((action) => action.kind === "navigate" && action !== link)
  const send = () => {
    if (!reply || !text.trim()) return
    onAction(item, reply.id, text.trim())
    setText("")
    setReplying(false)
  }
  return (
    <article aria-label={item.title} data-here={here ? "true" : undefined}
      className={`flex flex-col gap-2 rounded-[10px] border p-3 ${here ? "border-lw-accent-ring bg-lw-accent-soft/40" : "border-lw-line bg-lw-surface"}`}>
      <div className="flex min-w-0 flex-wrap items-center gap-1.5 text-[11px] text-lw-text-2">
        <KindTag item={item} />
        {item.task_id && <span className="font-lw-mono text-lw-text">{item.task_id}</span>}
        {here && <span className="text-lw-accent-text">· this agent</span>}
        <span>· {shortAge(item.updated_at)}</span>
      </div>
      <p className="m-0 text-[13px] font-semibold leading-snug text-lw-text">{item.title}</p>
      {item.reason && <p className="m-0 text-xs leading-snug text-lw-text-2">{item.reason}</p>}
      {copy?.text && (
        <div className="flex min-w-0 items-center gap-1.5 rounded-md border border-lw-line bg-lw-rail py-1 pl-2 pr-1 font-lw-mono text-xs text-lw-text">
          <span className="min-w-0 flex-1 truncate" title={copy.text}>{copy.text}</span>
          <button type="button" className={quiet} onClick={() => {
            void navigator.clipboard?.writeText(copy.text ?? "").then(() => setCopied(true), () => setCopied(false))
          }}>
            <Copy size={13} aria-hidden="true" />{copied ? "Copied" : copy.label}
          </button>
        </div>
      )}
      {link?.href && (
        <a href={link.href} target="_blank" rel="noopener noreferrer" className="flex min-w-0 items-center gap-1.5 rounded-md border border-lw-line bg-lw-rail px-2 py-1 font-lw-mono text-xs text-lw-link hover:underline">
          <span className="min-w-0 flex-1 truncate">{link.href}</span><ExternalLink size={13} aria-hidden="true" /><span className="sr-only">{link.label}</span>
        </a>
      )}
      <div className="flex flex-wrap items-center gap-1.5">
        {executes.map((action) => (
          <button key={action.id} type="button" disabled={pending}
            className={action.intent === "primary" ? `${button} border-lw-accent bg-lw-accent text-white hover:bg-lw-accent` : button}
            onClick={() => {
              if (action.confirmation && !window.confirm(action.confirmation)) return
              onAction(item, action.id)
            }}>
            {action.label}
          </button>
        ))}
        {reply && !replying && <button type="button" className={quiet} disabled={pending} onClick={() => setReplying(true)}>{reply.label}…</button>}
        {navigates.map((action) => {
          const target = hrefOf(action, item)
          if (!target) return null
          return "external" in target
            ? <a key={action.id} href={target.external} target="_blank" rel="noopener noreferrer" className={button}>{action.label} <ExternalLink size={12} aria-hidden="true" /></a>
            : <Link key={action.id} to={target.path} className={button}>{action.label}</Link>
        })}
        <span className="flex-1" />
        <button type="button" className={quiet} disabled={pending} onClick={() => onDismiss(item)}>Dismiss</button>
      </div>
      {reply && replying && (
        <form className="flex items-center gap-1.5" onSubmit={(event) => { event.preventDefault(); send() }}>
          <input aria-label={reply.label} autoFocus value={text} onChange={(event) => setText(event.target.value)} placeholder="Your answer"
            className="min-h-8 min-w-0 flex-1 rounded-md border border-lw-line-strong/40 bg-lw-rail px-2 text-xs text-lw-text" />
          <button type="submit" className={button} disabled={pending || !text.trim()}>Send</button>
        </form>
      )}
    </article>
  )
}
