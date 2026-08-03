import { useState } from "react"
import { useNavigate } from "react-router-dom"
import { useServer } from "../hooks/useServer"
import { startWorkflow, type WorkflowDefinition, type WorkflowRepositoryPreset } from "../lib/workflow"
import { WorkflowTaskInput } from "./WorkflowTaskInput"

export function RunWorkflowDialog({ definition, repositories, initialValues = {}, readOnlyInputs = [], onClose }: {
  definition: WorkflowDefinition
  repositories: WorkflowRepositoryPreset[]
  initialValues?: Record<string, string>
  readOnlyInputs?: string[]
  onClose: () => void
}) {
  const { baseUrl } = useServer()
  const navigate = useNavigate()
  const inputs = definition.inputs ?? {}
  const repositoryKeys = ["repo", "primary_dir", "base_branch"].filter(key => key in inputs)
  const [values, setValues] = useState<Record<string, string>>(() => Object.fromEntries(Object.entries(inputs).flatMap(([key, input]) => {
    const value = initialValues[key] ?? input.default ?? (input.choices?.[0]) ?? (input.type === "boolean" ? false : undefined)
    return value == null ? [] : [[key, String(value)]]
  })))
  const [error, setError] = useState("")
  const [busy, setBusy] = useState(false)
  const readOnly = new Set(readOnlyInputs)

  async function submit(event: React.FormEvent) {
    event.preventDefault()
    const missing = Object.entries(inputs).find(([key, input]) => input.required && !values[key] && input.default == null)
    if (missing) { setError(`${missing[0]} is required`); return }
    setBusy(true)
    const declaredValues = Object.fromEntries(Object.entries(values).filter(([key]) => key in inputs))
    try {
      const view = await startWorkflow(baseUrl, definition.name, declaredValues)
      navigate(`/workflows/runs/${view.run.id}`)
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : String(cause))
      setBusy(false)
    }
  }

  return <div role="dialog" aria-modal="true" aria-label={`Run ${definition.title}`} className="fixed inset-0 z-50 flex items-end bg-black/70 p-2 sm:items-center sm:justify-center">
    <form onSubmit={submit} className="max-h-[calc(100dvh-1rem)] w-full overflow-y-auto rounded-xl border border-zinc-700 bg-zinc-900 p-4 sm:max-w-lg">
      <h2 className="text-lg font-semibold">Run {definition.title}</h2>
      {repositories.length > 0 && repositoryKeys.length > 0 && <label className="mt-4 block text-sm"><span>Repository preset</span><select defaultValue="" onChange={event => { const preset = repositories.find(repository => repository.name === event.target.value); if (preset) setValues(current => ({ ...current, ...Object.fromEntries(repositoryKeys.map(key => [key, key === "repo" ? preset.repo : key === "primary_dir" ? preset.primary_dir : preset.base_branch ?? ""])) })) }} className="mt-1 min-h-11 w-full rounded-lg border border-zinc-700 bg-zinc-950 px-3"><option value="">Custom</option>{repositories.map(repository => <option key={repository.name} value={repository.name}>{repository.name}</option>)}</select></label>}
      <div className="mt-4 space-y-4">{Object.entries(inputs).map(([key, input]) => <div key={key} className="block text-sm"><span>{key}{input.required && " *"}</span>{input.description && <span className="ml-2 text-xs text-zinc-500">{input.description}</span>}{readOnly.has(key) ? <input aria-label={key} readOnly value={values[key] ?? String(input.default ?? "")} className="mt-1 min-h-11 w-full rounded-lg border border-zinc-700 bg-zinc-800 px-3 text-zinc-400" /> : input.type === "task" ? <WorkflowTaskInput label={key} required={input.required} value={values[key] ?? String(input.default ?? "")} onChange={value => setValues(current => ({ ...current, [key]: value }))} /> : input.type === "repository" ? <select aria-label={key} required={input.required} value={values[key] ?? ""} onChange={event => setValues(current => ({ ...current, [key]: event.target.value }))} className="mt-1 min-h-11 w-full rounded-lg border border-zinc-700 bg-zinc-950 px-3"><option value="">Select repository</option>{repositories.map(repository => <option key={repository.name} value={repository.name}>{repository.name}</option>)}</select> : input.choices?.length ? <select aria-label={key} required={input.required} value={values[key] ?? String(input.default ?? input.choices[0] ?? "")} onChange={event => setValues(current => ({ ...current, [key]: event.target.value }))} className="mt-1 min-h-11 w-full rounded-lg border border-zinc-700 bg-zinc-950 px-3">{input.choices.map(choice => <option key={String(choice)} value={String(choice)}>{String(choice)}</option>)}</select> : <input aria-label={key} required={input.required} type={input.secret ? "password" : input.type === "integer" ? "number" : "text"} value={values[key] ?? String(input.default ?? "")} onChange={event => setValues(current => ({ ...current, [key]: event.target.value }))} className="mt-1 min-h-11 w-full rounded-lg border border-zinc-700 bg-zinc-950 px-3" />}</div>)}</div>
      {error && <p className="mt-3 text-sm text-red-400">{error}</p>}
      <div className="mt-5 flex gap-2"><button type="button" onClick={onClose} className="min-h-11 flex-1 rounded-lg border border-zinc-700">Cancel</button><button disabled={busy} className="min-h-11 flex-1 rounded-lg bg-indigo-500 font-medium">{busy ? "Starting…" : "Start"}</button></div>
    </form>
  </div>
}
