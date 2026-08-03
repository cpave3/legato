export function workflowArtifactFilename(originalPath: string | undefined, outputKey: string): string {
  const basename = originalPath?.split(/[\\/]/).filter(Boolean).at(-1)
  return basename || outputKey
}
