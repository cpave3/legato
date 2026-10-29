import type { NavigationSummary } from "../hooks/useNavigationSummary"

// The tone of a count: agents at work, something that waits on the person,
// something that failed, or a plain number.
export type NavTone = "live" | "needs" | "error" | "neutral"

export interface NavSection {
  key: string
  label: string
  to: string
  // The icon's SVG path, drawn on a 24 by 24 grid as the design draws it.
  icon: string
  count?: number
  tone?: NavTone
  // What the count is of, for the link's name: "Plans: 8 to decide".
  noun?: string
  // A rule above this section in the rail.
  separated?: boolean
}

export const navIcons = {
  attention: "M6 8a6 6 0 0 1 12 0c0 7 3 9 3 9H3s3-2 3-9M10.3 21a1.94 1.94 0 0 0 3.4 0",
  ensemble: "M3 3h7v7H3zM14 3h7v7h-7zM3 14h7v7H3zM14 14h7v7h-7z",
  agents: "M21 15a2 2 0 0 1-2 2H7l-4 4V5a2 2 0 0 1 2-2h14a2 2 0 0 1 2 2z",
  board: "M4 4h4v16H4zM10 4h4v10h-4zM16 4h4v7h-4z",
  review: "M9 11l3 3L22 4M21 12v7a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h11",
  plans: "M9 2h6v4H9zM16 4h2a2 2 0 0 1 2 2v14a2 2 0 0 1-2 2H6a2 2 0 0 1-2-2V6a2 2 0 0 1 2-2h2M9 12h6M9 16h4",
  workflows: "M6 3a3 3 0 1 0 0 6 3 3 0 0 0 0-6zM18 15a3 3 0 1 0 0 6 3 3 0 0 0 0-6zM6 9v3a3 3 0 0 0 3 3h6",
  learning: "M2 3h6a4 4 0 0 1 4 4v14a3 3 0 0 0-3-3H2zM22 3h-6a4 4 0 0 0-4 4v14a3 3 0 0 1 3-3h7z",
  reports: "M3 3v18h18M7 15l4-4 4 3 5-6",
  audit: "M12 3l8 3v6c0 5-3.5 8-8 9-4.5-1-8-4-8-9V6zM9 12l2 2 4-4",
  settings: "M4 21v-7M4 10V3M12 21v-9M12 8V3M20 21v-5M20 12V3M1 14h6M9 8h6M17 16h6",
  more: "M5 12h.01M12 12h.01M19 12h.01",
}

export const ENSEMBLE_PATH = "/agents?view=ensemble"

// navSections is every section of the app, in the rail's order and groups.
export function navSections(summary: Partial<NavigationSummary> = {}): Record<string, NavSection> {
  return {
    attention: { key: "attention", label: "Attention", to: "/attention", icon: navIcons.attention, count: summary.attention, tone: "needs", noun: "items" },
    ensemble: { key: "ensemble", label: "Ensemble", to: ENSEMBLE_PATH, icon: navIcons.ensemble, count: summary.agents, tone: "live", noun: "non-idle" },
    agents: { key: "agents", label: "Agents", to: "/agents", icon: navIcons.agents, separated: true },
    board: { key: "board", label: "Board", to: "/board", icon: navIcons.board },
    review: { key: "review", label: "Review", to: "/review", icon: navIcons.review, count: summary.review, tone: "needs", noun: "to review", separated: true },
    plans: { key: "plans", label: "Plans", to: "/plans", icon: navIcons.plans, count: summary.plans, tone: "needs", noun: "to decide" },
    workflows: { key: "workflows", label: "Flows", to: "/workflows", icon: navIcons.workflows, count: summary.workflow_attention, tone: "error", noun: "need attention", separated: true },
    learning: { key: "learning", label: "Learning", to: "/learning", icon: navIcons.learning, count: summary.learning, tone: "neutral", noun: "to decide" },
    reports: { key: "reports", label: "Reports", to: "/reports", icon: navIcons.reports },
    audit: { key: "audit", label: "Audit", to: "/audit", icon: navIcons.audit },
    settings: { key: "settings", label: "Settings", to: "/settings", icon: navIcons.settings },
  }
}

// The rail's sections, top to bottom; Settings stands apart at the foot.
export const railOrder = ["attention", "agents", "ensemble", "board", "review", "plans", "workflows", "learning", "reports", "audit"]

// sectionName is a section's accessible name, with its count when it has one.
export function sectionName(section: Pick<NavSection, "label" | "count" | "noun">): string {
  return section.count ? `${section.label}: ${section.count} ${section.noun}` : section.label
}

export const badgeTone: Record<NavTone, string> = {
  live: "bg-lw-live-soft text-lw-live",
  needs: "bg-lw-needs-soft text-lw-needs",
  error: "bg-lw-error-soft text-lw-error",
  neutral: "bg-lw-line text-[#b4b3bd]",
}

export function badgeText(count: number): string {
  return count > 99 ? "99+" : String(count)
}

// Whether a section is the page in view. The ensemble and the agent list
// share a path and differ by the view.
export function sectionActive(section: NavSection, pathname: string, search: string): boolean {
  const ensemble = pathname.startsWith("/agents") && new URLSearchParams(search).get("view") === "ensemble"
  if (section.key === "ensemble") return ensemble
  if (section.key === "agents") return pathname.startsWith("/agents") && !ensemble
  return pathname === section.to || pathname.startsWith(`${section.to}/`)
}
