import {
  AlertTriangle, Bell, CheckCircle, CircleHelp, DoorOpen, GitMerge, GitPullRequest,
  Hourglass, ListChecks, MessageSquare, ShieldCheck, SquareTerminal, XOctagon, type LucideIcon,
} from "lucide-react"
import type { NeedsYouKind, NeedsYouTone } from "./needsYou"

// Each kind of needs-you item has its own icon, so colour is never the only
// cue, and each tone its colours.
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

