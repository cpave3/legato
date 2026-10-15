import type { ReactNode } from "react"
import { cn } from "../lib/utils"

export function Mark({ glyph, tone, children }: { glyph: string; tone: string; children: ReactNode }) {
  return <span className={cn("inline-flex items-center gap-1", tone)}><span aria-hidden="true">{glyph}</span><span>{children}</span></span>
}

// How a pull request's checks stand.
export function ChecksMark({ status }: { status: string }) {
  switch (status) {
    case "pass": return <Mark glyph="✓" tone="text-lw-live">CI passed</Mark>
    case "fail": return <Mark glyph="✕" tone="text-lw-error">CI failed</Mark>
    case "pending": return <Mark glyph="◌" tone="text-lw-needs">CI pending</Mark>
    default: return <span className="text-lw-faint">no checks</span>
  }
}

// How a pull request's review stands.
export function ReviewMark({ decision }: { decision: string }) {
  switch (decision) {
    case "APPROVED": return <Mark glyph="✓" tone="text-lw-live">approved</Mark>
    case "CHANGES_REQUESTED": return <Mark glyph="✕" tone="text-lw-error">changes requested</Mark>
    default: return <span className="text-lw-faint">review pending</span>
  }
}
