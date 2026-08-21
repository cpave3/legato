export function resolveTaskWorkingDirectory(worktreePath: string, rememberedWorkingDir: string, defaultWorkingDir: string) {
  return worktreePath || rememberedWorkingDir || defaultWorkingDir
}
