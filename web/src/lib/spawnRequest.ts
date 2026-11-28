export type SessionPolicy = "resume" | "new"

// What the person asked for when they confirmed the dialog.
export interface SpawnAgentRequest {
  // Empty when nothing was typed, or when the dialog has no title field.
  title: string
  // Empty for the default adapter.
  agentKind: string
  account?: string
  // As it was typed: a path under the home directory may start with a tilde.
  workingDir: string
  // Set only when the chosen agent tool has a saved session to resume.
  sessionPolicy?: SessionPolicy
  // The names of the resources the agent is given; absent when none were picked.
  resources?: string[]
  // False starts the agent outside Legato's sandbox; absent otherwise.
  sandbox?: false
  // True lets the agent's containers use the host's network; absent otherwise.
  dockerHostNetwork?: true
  // The agent's first message; absent when nothing was written.
  prompt?: string
  // The agent tool's tier, from a macro chosen to start with; absent for none.
  tier?: string
  // Set when the agent starts in a new clone of a repository instead of a
  // folder; workingDir is then empty. Empty fields are the server's to fill:
  // the default branch as the base, legato/<task id> as the branch.
  clone?: { repo: string; baseBranch: string; branch: string }
  // Set when the agent continues in a clone Legato made before (the next
  // branch of a stack); workingDir is then empty. An empty branch stays on
  // the clone's.
  joinClone?: { id: number; branch: string }
  // The engram the agent starts as; absent for a normal session.
  engram?: string
  // Bind the engram to the place and make it the default there (or, with no
  // engram, clear the place's default); absent when not ticked.
  rememberEngram?: boolean
}
