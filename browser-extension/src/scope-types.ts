// Legato Scope: pins a person places on a page of their own site, sent to
// Legato as one batch for an agent to act on. These are the shapes the
// extension sends and reads (POST /api/scope/batches, GET /api/scope/pins).

export type Box = { x: number; y: number; width: number; height: number }

// The component that drew the pinned element, when a later ticket can tell.
export type PinSource = {
  name?: string
  file?: string
  line?: number
  column?: number
  owners?: string[]
  confidence: "exact" | "file-only" | "name-only" | "none"
}

export type PinPage = {
  url: string
  title: string
  viewport: { width: number; height: number }
  scroll: { x: number; y: number }
  dpr: number
}

export type PinTarget = {
  // Stable first; each matched exactly one element when the pin was placed.
  // A candidate is a CSS selector, or role=<role>[name="<accessible name>"].
  selectors: string[]
  text: string
  // In page coordinates (CSS pixels from the document's top left).
  box: Box
}

export type PinStyles = {
  // Computed values that differ from the browser's default for the tag.
  computed: Record<string, string>
  classes: string[]
  inline: string
  // The parent's layout properties.
  parent: Record<string, string>
}

// One pin as POST /api/scope/batches takes it.
export type ScopePin = {
  comment: string
  page: PinPage
  target: PinTarget
  styles: PinStyles
  source?: PinSource
  // data:image/jpeg;base64,…, at most 500 KB decoded.
  screenshot?: string
}

export type ScopeBatch = { origin: string; pins: ScopePin[] }
export type ScopeBatchResult = { batch_id: string; pin_ids: string[]; subscribed: boolean; delivered: boolean }

// A pin not sent yet, as the extension keeps it in chrome.storage.local. Its
// screenshot is kept apart, under a key of its own.
export type ScopeDraft = {
  id: string
  createdAt: string
  pin: Omit<ScopePin, "screenshot">
  hasScreenshot: boolean
}

export type SentPinState = "sent" | "delivered" | "fixed" | "wont_fix"

// A sent pin, as GET /api/scope/pins answers.
export type SentPin = {
  id: string
  batch_id: string
  origin: string
  url: string
  path: string
  comment: string
  state: SentPinState
  note?: string
  selector?: string
  selectors: string[]
  text?: string
  box: Box
  has_screenshot: boolean
  created_at: string
  updated_at: string
}

export const MAX_PINS_PER_BATCH = 20
export const MAX_SCREENSHOT_BYTES = 500 * 1024
