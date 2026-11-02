import type { SpawnAgentRequest } from "../components/SpawnAgentDialog"

// The fields of a spawn request that say where the agent starts: a folder,
// a fresh clone of a repository, or a job clone that exists.
export function spawnPlace(request: SpawnAgentRequest): Record<string, string | number> {
  if (request.joinClone) {
    return { clone_id: request.joinClone.id, ...(request.joinClone.branch ? { branch: request.joinClone.branch } : {}) }
  }
  if (request.clone) {
    return {
      repo: request.clone.repo,
      ...(request.clone.baseBranch ? { ref: request.clone.baseBranch } : {}),
      ...(request.clone.branch ? { branch: request.clone.branch } : {}),
    }
  }
  return { working_dir: request.workingDir }
}
