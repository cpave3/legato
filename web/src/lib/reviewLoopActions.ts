import type { ReviewLoopAction } from "./workflow"

// What a review loop's actions are called, and what End asks first, on the
// run page and the Shipping page alike (in the words Attention's End uses).

export const reviewLoopActionLabels: Record<ReviewLoopAction, string> = {
  another_pass: "Run another pass",
  message: "Message the author",
  stop_watching: "Stop watching",
  watch_again: "Watch again",
  end: "End",
}

// endConfirmation is End's question: a steward's, or a stuck loop's.
export function endConfirmation(steward: boolean): { title: string; message: string; confirmLabel: string; destructive: boolean } {
  return steward
    ? { title: "End the steward?", message: "The review loop ends with outcome ended: nothing watches the pull requests after, legato-reviews' approval stays on GitHub as it is, and the run goes on to its next step.", confirmLabel: "End", destructive: true }
    : { title: "End the review loop as stuck?", message: "The run goes on to its next step.", confirmLabel: "End", destructive: true }
}
