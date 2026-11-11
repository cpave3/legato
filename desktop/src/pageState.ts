import type { Status } from "./api"

// What the content area holds for the server in front: its page, or the
// app's own offline screen. Checked by desktop/tests/pageState.test.ts.

// Pages that did not load, by server: the address that failed, or "" when
// the app does not know it (the page then starts at the server's root).
export type Failed = Record<string, string>

// A server that does not answer, or whose page did not load, gets the
// offline screen. Until its state is known the page is shown, so a server
// that is up never waits for the first poll.
export function screenFor(status: Status | undefined, failed: boolean): "page" | "offline" {
  return failed || (status !== undefined && !status.online) ? "offline" : "page"
}

// The servers whose page was made before their state was known and that
// turn out to be down: those pages did not load. A page that loaded while
// its server was up is kept when the server goes down later; the web UI
// reconnects by itself.
export function failedAtStart(made: ReadonlySet<string>, before: Record<string, Status>, now: Record<string, Status>): string[] {
  return [...made].filter((id) => !before[id] && now[id] !== undefined && !now[id].online)
}

// The failed pages whose server answers now, to load again.
export function recovered(failed: Failed, statuses: Record<string, Status>): string[] {
  return Object.keys(failed).filter((id) => statuses[id]?.online)
}
