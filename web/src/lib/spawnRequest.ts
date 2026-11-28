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

// What the dialog holds when it is confirmed: each choice as it stands, with
// whether the dialog offered it, since a choice it did not offer is not sent.
export interface SpawnChoices {
  title: string
  agentKind: string
  // The tool that runs: the one picked, else the default.
  effectiveAdapter: string
  account: string
  // The path as typed; it is trimmed here.
  path: string
  // True when the agent starts in a clone rather than at path.
  inClone: boolean
  // The clone picked, when it is one that exists.
  jobClone?: { id: number; busy: boolean }
  // The repository a new clone is of.
  cloneRepo?: string
  branch: string
  baseBranch: string
  offersSessionChoice: boolean
  sessionPolicy: SessionPolicy
  resources: string[]
  unsandboxed: boolean
  hostNetworkOffered: boolean
  hostNetwork: boolean
  prompt: string
  tier: string
  engramShown: boolean
  engram: string
  canRemember: boolean
  rememberEngram: boolean
}

// The working directory the agent starts in: empty for a clone.
export function spawnWorkingDir(choices: Pick<SpawnChoices, "inClone" | "path">): string {
  return choices.inClone ? "" : choices.path.trim()
}

// A Claude Code account is sent only when it is not the saved session's,
// which a resume keeps; it names the tool, since the default may not be it.
function accountPart(choices: SpawnChoices): Partial<Pick<SpawnAgentRequest, "agentKind" | "account">> {
  const resuming = choices.offersSessionChoice && choices.sessionPolicy === "resume"
  if (choices.effectiveAdapter !== "claude-code" || !choices.account || resuming) return {}
  return { agentKind: "claude-code", account: choices.account }
}

// Where in a clone: one that exists, joined (on its own branch while an
// agent works in it), or a new one of the repository.
function clonePart(choices: SpawnChoices): Pick<SpawnAgentRequest, "joinClone" | "clone"> {
  if (!choices.inClone) return {}
  const { jobClone, cloneRepo } = choices
  if (jobClone) return { joinClone: { id: jobClone.id, branch: jobClone.busy ? "" : choices.branch.trim() } }
  if (!cloneRepo) return {}
  return { clone: { repo: cloneRepo, baseBranch: choices.baseBranch.trim(), branch: choices.branch.trim() } }
}

// The choices the agent starts with, each absent when left alone.
function startPart(choices: SpawnChoices): Pick<SpawnAgentRequest, "sessionPolicy" | "resources" | "sandbox" | "dockerHostNetwork"> {
  return {
    ...(choices.offersSessionChoice ? { sessionPolicy: choices.sessionPolicy } : {}),
    ...(choices.resources.length > 0 ? { resources: choices.resources } : {}),
    ...(choices.unsandboxed ? { sandbox: false as const } : {}),
    ...(choices.hostNetworkOffered && choices.hostNetwork ? { dockerHostNetwork: true as const } : {}),
  }
}

// The first message, the macro's tier and the engram, each absent when none.
function firstTurnPart(choices: SpawnChoices): Pick<SpawnAgentRequest, "prompt" | "tier" | "engram" | "rememberEngram"> {
  const prompt = choices.prompt.trim()
  return {
    ...(prompt ? { prompt } : {}),
    ...(choices.tier ? { tier: choices.tier } : {}),
    ...(choices.engramShown && choices.engram ? { engram: choices.engram } : {}),
    ...(choices.canRemember && choices.rememberEngram ? { rememberEngram: true } : {}),
  }
}

// buildSpawnRequest turns the dialog's choices into the request onSubmit
// gets.
export function buildSpawnRequest(choices: SpawnChoices): SpawnAgentRequest {
  return {
    title: choices.title.trim(),
    agentKind: choices.agentKind,
    ...accountPart(choices),
    workingDir: spawnWorkingDir(choices),
    ...clonePart(choices),
    ...startPart(choices),
    ...firstTurnPart(choices),
  }
}
