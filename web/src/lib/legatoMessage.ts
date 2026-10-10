// A message Legato itself sends to an agent opens with a bracketed tag:
// `[legato coordination]`, `[legato review]`, `[legato plan]`, `[legato]` for a
// swarm inbox pointer, and `[swarm event]`. It reaches the agent as a user
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
}

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
  return { label, body: text.slice(tag.length) }
}
