// A message Legato itself sends to an agent opens with a bracketed tag:
// `[legato coordination]`, `[legato review]`, `[legato plan]`, `[legato review
// pass]` and, after approval, `[legato steward]` (a full bubble labelled
// steward), `[legato]` for a swarm inbox pointer, and `[swarm event]`. It reaches the agent as a user
// turn, so the conversation needs the tag to tell it from what a person typed.
//
// `[legato peer <task-id>]` opens words another agent wrote and Legato only
// relayed (`legato agent message`, `legato agent broadcast`). They end with a
// footer telling the recipient how to reply, which is for the agent to read.
export interface LegatoMessage {
  // What kind of message it is, for the label: "coordination", "review", ...
  label: string
  // The message without its tag.
  body: string
  // Set when another agent wrote the message: that agent's task ID, or "" for
  // a sender that did not name itself.
  peer?: string
  // Set for a message that tells of something that happened rather than
  // asking anything: a change set's result, a restart, an agent update. The conversation
  // draws it as one line, like a tool call, which opens to the whole text.
  event?: true
}

// The kinds of Legato message that are events, not words to the agent.
const eventLabels = new Set(["change set", "restart", "agent"])

const tagPattern = /^\[(legato|swarm event)([^\]]*)\]\s*/
const peerPattern = /^ peer(?: (\S+))?$/
const peerFooterPattern = /\n+---\nAgent-to-agent message[^\n]*$/

export function parseLegatoMessage(text: string): LegatoMessage | null {
  const match = tagPattern.exec(text)
  if (!match) return null
  const [tag, family, rest] = match
  // An encoded swarm event carries its payload inside the tag; there is no
  // body to show apart from it.
  if (rest.includes(":")) return { label: family, body: text }
  const peer = family === "legato" ? peerPattern.exec(rest) : null
  if (peer) {
    return { label: "peer", peer: peer[1] ?? "", body: text.slice(tag.length).replace(peerFooterPattern, "") }
  }
  const label = family === "legato" ? rest.trim() || "notice" : family
  const body = text.slice(tag.length)
  return family === "legato" && eventLabels.has(label) ? { label, body, event: true } : { label, body }
}

// A change set's ID, shortened as a summary shows it: cs-816fab12cd is
// cs-816f….
function shortSet(id: string): string {
  return id.length > 7 ? `${id.slice(0, 7)}…` : id
}

const changeSetReport = /^Change set (cs-[0-9a-z]+) \(.*\): (\d+ of \d+ ran(?:, \d+ failed)?(?:, \d+ skipped)?)\.?$/
const changeSetDenied = /^The operator denied change set (cs-[0-9a-z]+)\b/

// The one line an event's row shows: for a change set, its short ID and how
// its calls went (✓ when all ran), or that it was denied; for a restart,
// whether Legato is back or the restart was rolled back. Agent updates name
// the task or agent count and event count. Other kinds show their first line.
export function legatoEventSummary(label: string, body: string): string {
  const lines = body.split("\n")
  const first = lines[0]?.trim() ?? ""
  if (label === "agent") {
    // WatchMessage keeps each event on one top-level bullet; quoted details
    // cannot introduce another line. Titles may contain parentheses.
    const events = [...body.matchAll(/^- ([^\s:]+)(?::| )/gm)]
    if (!events.length) return "Agent update"
    const agents = new Set(events.map(event => event[1]))
    const source = agents.size === 1 ? events[0][1] : `${agents.size} agents`
    return `${source} · ${events.length} ${events.length === 1 ? "event" : "events"}`
  }
  if (label === "change set") {
    const report = changeSetReport.exec(first)
    if (report) {
      const [, id, counts] = report
      const allRan = !counts.includes("failed") && !counts.includes("skipped")
      let summary = `${shortSet(id)} · ${counts}${allRan ? " ✓" : ""}`
      if (body.includes("Legato restarts now")) summary += " · restarting"
      return summary
    }
    const denied = changeSetDenied.exec(first)
    if (denied) return `${shortSet(denied[1])} · denied`
  }
  if (label === "restart" && first.startsWith("Legato is back")) {
    return body.includes("rolled back") ? "restart: rolled back" : "restart: back"
  }
  return first
}
