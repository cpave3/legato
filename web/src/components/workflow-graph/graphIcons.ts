import { ArrowRightLeft, AtSign, BookOpen, Bot, CircleStop, Clock, Columns3, FileText, FileX, FolderSearch, FolderX, GitBranch, GitPullRequest, Hand, ListFilter, MessageCircle, MessageSquarePlus, MessageSquareQuote, MessageSquareShare, MessageSquareText, MessageSquareWarning, MessagesSquare, MousePointerClick, Repeat, Send, Shrink, Smile, SmilePlus, StickyNote, Tag, TicketCheck, TicketPlus, Tickets, Trash2, UserCheck, Workflow, type LucideIcon } from "lucide-react"

// The icon of each step type and trigger kind, as the graph's nodes draw
// them; one it does not know gets Workflow's.

export const stepIcons: Record<string, LucideIcon> = {
  "agent.run": Bot,
  "agent.dispatch": Send,
  "agent.message": MessageSquareText,
  "agent.interview": MessagesSquare,
  "agent.stop": CircleStop,
  "agent.compact": Shrink,
  "task.note": StickyNote,
  "task.move": ArrowRightLeft,
  "task.context": FileText,
  "task.metadata.set": Tag,
  "task.working_dir.resolve": FolderSearch,
  "ticket.import": TicketCheck,
  "tickets.list": Tickets,
  "tasks.list": ListFilter,
  "github.pr.prepare": GitPullRequest,
  "github.pr.review_passes": Repeat,
  "repo.ensure": GitBranch,
  "worktree.remove": FolderX,
  "file.remove": FileX,
  "chimera.session.remove": Trash2,
  "learning.packet.prepare": BookOpen,
  "learning.proposals.submit": BookOpen,
  "chat.message.send": MessageSquareShare,
  "chat.reaction.add": SmilePlus,
  "chat.reaction.remove": Smile,
  "chat.thread.read": MessageSquareQuote,
  "ticket.create": TicketPlus,
  "ticket.comment": MessageSquarePlus,
  "ticket.update": UserCheck,
}

export const triggerIcons: Record<string, LucideIcon> = {
  manual: Hand,
  "task.entered_column": Columns3,
  schedule: Clock,
  "pr.changes_requested": MessageSquareWarning,
  "chat.message": MessageCircle,
  "chat.mention": AtSign,
  "chat.button": MousePointerClick,
}

export function stepIcon(type: string): LucideIcon {
  return stepIcons[type] ?? Workflow
}

export function triggerIcon(kind: string): LucideIcon {
  return triggerIcons[kind] ?? Workflow
}
