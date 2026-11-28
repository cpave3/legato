import type { UsagePin, UsagePolicy } from "./usage"

// The pin form of the usage card, apart from its markup: where it starts,
// and what it sends. See components/UsageStrip.tsx.

export type PinExpiry = "indefinite" | "time" | "reset"

export interface PinDraft {
  profile: string
  expiry: PinExpiry
  // A datetime-local value, in the browser's zone.
  until: string
  // A qualified meter, for a pin kept until that limit resets.
  meter: string
}

// pinDraftStart is the form as it opens: the profile the address asks for
// when the policy has it, else the pinned one, else the active one; and the
// pin's own expiry, or until the operator unpins it.
export function pinDraftStart(policy: UsagePolicy, requestedPin: string | null, meters: string[]): PinDraft {
  const pin = policy.active.pin
  return {
    profile: startProfile(policy, requestedPin),
    expiry: pinExpiry(pin),
    until: pin?.until ? localDateTime(pin.until) : "",
    meter: pin?.until_reset ?? meters[0] ?? "",
  }
}

function startProfile(policy: UsagePolicy, requestedPin: string | null): string {
  if (requestedPin && policy.profiles.some((p) => p.name === requestedPin)) return requestedPin
  return policy.active.pin?.profile ?? policy.active.profile.name
}

function pinExpiry(pin: UsagePin | undefined): PinExpiry {
  if (pin?.until) return "time"
  return pin?.until_reset ? "reset" : "indefinite"
}

// localDateTime writes an instant as a datetime-local value in the
// browser's zone.
function localDateTime(iso: string): string {
  const date = new Date(iso)
  return new Date(date.getTime() - date.getTimezoneOffset() * 60_000).toISOString().slice(0, 16)
}

// pinRequest is the pin the form sends: the profile, and the time or the
// limit it lasts until.
export function pinRequest(draft: PinDraft): UsagePin {
  const next: UsagePin = { profile: draft.profile }
  if (draft.expiry === "time") next.until = new Date(draft.until).toISOString()
  if (draft.expiry === "reset") next.until_reset = draft.meter
  return next
}
