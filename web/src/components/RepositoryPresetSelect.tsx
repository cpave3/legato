export interface RepositoryPreset {
  name: string
  repo: string
  primary_dir: string
  base_branch?: string
}

export function RepositoryPresetSelect({
  repositories,
  value,
  onChange,
}: {
  repositories: RepositoryPreset[]
  value: string
  onChange: (name: string, repository?: RepositoryPreset) => void
}) {
  if (repositories.length === 0) return null

  return (
    <label className="block text-xs text-zinc-400">
      Repository preset
      <select
        aria-label="Repository preset"
        value={value}
        onChange={(event) => {
          const name = event.target.value
          onChange(name, repositories.find((repository) => repository.name === name))
        }}
        className="mt-1 min-h-11 w-full rounded border border-zinc-700 bg-zinc-950 px-3"
      >
        <option value="">Custom</option>
        {repositories.map((repository) => (
          <option key={repository.name} value={repository.name}>{repository.name}</option>
        ))}
      </select>
    </label>
  )
}
