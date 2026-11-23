// The trigger kinds in plain words: a module of its own, so the page can
// name a trigger without loading the workflow schema.

/** KIND_TITLES are the trigger kinds in plain words. */
export const KIND_TITLES: Record<string, string> = { "task.entered_column": "When a card enters a column", schedule: "On a schedule", "pr.changes_requested": "When changes are requested on a pull request" }
