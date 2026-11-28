import type { Approval } from "./approvals"

// What an approval card shows beside its options, by the kind of approval.
// A credential, a Reeve change set and a Reeve secret draw their own body,
// which already says what they would run and why.

const OWN_BODY = new Set(["credential", "reeve_change_set", "reeve_secret"])

// The command the approval would run, or its summary: nothing for a kind
// with a body of its own, nor for a plan, which is folded away instead.
export function approvalRunText(approval: Approval): string | undefined {
  if (OWN_BODY.has(approval.source) || approval.details.plan) return undefined
  return approval.details.command || approval.summary || undefined
}

// The agent's reason, unless the credential or change set body shows it.
// A secret's form does not, so the card does.
export function approvalReason(approval: Approval): string | undefined {
  if (approval.source === "credential" || approval.source === "reeve_change_set") return undefined
  return approval.details.reason || undefined
}
