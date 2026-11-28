import type { SpawnAgentRequest } from "./spawnRequest"

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

// The fields of a spawn request that say what the agent starts with, each
// absent when it was not chosen: resources, managed hooks left out, the
// sandbox, the host network, the first message and the tier.
export function spawnStartFields(request: SpawnAgentRequest): Record<string, string | boolean | string[]> {
  return {
    ...(request.resources ? { resources: request.resources } : {}),
    ...(request.hooksOff ? { hooks_off: request.hooksOff } : {}),
    ...(request.sandbox === false ? { sandbox: false } : {}),
    ...(request.dockerHostNetwork ? { docker_host_network: true } : {}),
    ...(request.prompt ? { prompt: request.prompt } : {}),
    ...(request.tier ? { tier: request.tier } : {}),
  }
}
