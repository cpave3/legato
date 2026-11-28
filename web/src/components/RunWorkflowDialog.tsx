import { Dialog, DialogPanel, DialogHeader, DialogTitle, DialogBody, DialogFooter } from "./ui/Dialog"
import { useState } from "react"
import { Link, useNavigate } from "react-router-dom"
import { useServer } from "../hooks/useServer"
import { startWorkflow, type WorkflowDefinition, type WorkflowInput, type WorkflowRepositoryPreset } from "../lib/workflow"
import { afterStartPath, declaredValues, firstValues, missingInput, presetValues, refusalOf, REPOSITORY_KEYS, shownInputs, shownValue, type Inputs, type Refusal } from "../lib/workflowStart"
import { WorkflowTaskInput } from "./WorkflowTaskInput"

// raw is the input's value, if it has one; value what its field shows.
type FieldProps = { name: string; input: WorkflowInput; raw: string | undefined; value: string; repositories: WorkflowRepositoryPreset[]; onChange: (value: string) => void }

// ChoiceOrTextField is a choice from an input's choices, or text (a
// password for a secret, a number for an integer).
function ChoiceOrTextField({ name, input, raw, value, onChange }: FieldProps) {
  if (input.choices?.length) {
    return <select aria-label={name} required={input.required} value={raw ?? String(input.default ?? input.choices[0] ?? "")} onChange={event => onChange(event.target.value)} className="mt-1 min-h-11 w-full rounded-lg border border-zinc-700 bg-zinc-950 px-3">{input.choices.map(choice => <option key={String(choice)} value={String(choice)}>{String(choice)}</option>)}</select>
  }
  return <input aria-label={name} required={input.required} type={input.secret ? "password" : input.type === "integer" ? "number" : "text"} value={value} onChange={event => onChange(event.target.value)} className="mt-1 min-h-11 w-full rounded-lg border border-zinc-700 bg-zinc-950 px-3" />
}

// InputField is one input's field, by its type: read-only when the page
// fixes it.
function InputField({ readOnly, ...props }: FieldProps & { readOnly: boolean }) {
  const { name, input, raw, value, repositories, onChange } = props
  if (readOnly) return <input aria-label={name} readOnly value={value} className="mt-1 min-h-11 w-full rounded-lg border border-zinc-700 bg-zinc-800 px-3 text-zinc-400" />
  if (input.type === "task") return <WorkflowTaskInput label={name} required={input.required} value={value} onChange={onChange} />
  if (input.type === "repository") return <RepositoryWorkflowInput label={name} required={input.required} repositories={repositories} value={raw ?? ""} onChange={onChange} />
  return <ChoiceOrTextField {...props} />
}

// InputLabel is an input's name, marked when required, and what it is.
function InputLabel({ name, input }: { name: string; input: WorkflowInput }) {
  return <><span>{name}{input.required && " *"}</span>{input.description && <span className="ml-2 text-xs text-zinc-500">{input.description}</span>}</>
}

// useStartWorkflow starts the workflow from the form: a required input
// missing, a start refused (with where it points: the review loop that
// holds the pull request, say) or failed is the error; else it leads where
// the workflow says.
function useStartWorkflow(definition: WorkflowDefinition, inputs: Inputs, values: Record<string, string>, onClose: () => void) {
  const { baseUrl } = useServer()
  const navigate = useNavigate()
  const [error, setError] = useState("")
  const [errorLink, setErrorLink] = useState<Refusal["link"] | null>(null)
  const [busy, setBusy] = useState(false)
  const fail = (message: string, link: Refusal["link"] | null = null) => {
    setError(message)
    if (link) setErrorLink(link)
    setBusy(false)
  }
  async function submit(event: React.FormEvent) {
    event.preventDefault()
    const missing = missingInput(inputs, values)
    if (missing) { setError(`${missing} is required`); return }
    setBusy(true)
    const declared = declaredValues(values, inputs)
    try {
      const view = await startWorkflow(baseUrl, definition.name, declared)
      const refused = refusalOf(view)
      if (refused) { fail(refused.error, refused.link); return }
      const path = afterStartPath(definition, inputs, declared, view.run.id)
      if (path === null) onClose()
      else navigate(path)
    } catch (cause) {
      fail(cause instanceof Error ? cause.message : String(cause))
    }
  }
  return { submit, error, errorLink, busy }
}

export function RunWorkflowDialog({ definition, repositories, initialValues = {}, readOnlyInputs = [], hiddenInputs = [], onClose }: {
  definition: WorkflowDefinition
  repositories: WorkflowRepositoryPreset[]
  initialValues?: Record<string, string>
  readOnlyInputs?: string[]
  // Inputs left out of the form, and of the start: their defaults apply.
  hiddenInputs?: string[]
  onClose: () => void
}) {
  const inputs = shownInputs(definition, hiddenInputs)
  const repositoryKeys = REPOSITORY_KEYS.filter(key => key in inputs)
  const [values, setValues] = useState<Record<string, string>>(() => firstValues(inputs, initialValues))
  const { submit, error, errorLink, busy } = useStartWorkflow(definition, inputs, values, onClose)
  const readOnly = new Set(readOnlyInputs)
  const setValue = (key: string, value: string) => setValues(current => ({ ...current, [key]: value }))

  return <Dialog open onClose={onClose}>
    <DialogPanel className="max-w-lg">
    <form onSubmit={submit} className="flex min-h-0 flex-col">
      <DialogHeader><DialogTitle className="text-lg font-semibold">Run {definition.title}</DialogTitle></DialogHeader>
      <DialogBody>
      {/* A preset fills the repository inputs. */}
      {repositories.length > 0 && repositoryKeys.length > 0 && <label className="mt-4 block text-sm"><span>Repository preset</span><select defaultValue="" onChange={event => { const preset = repositories.find(repository => repository.name === event.target.value); if (preset) setValues(current => ({ ...current, ...presetValues(preset, repositoryKeys) })) }} className="mt-1 min-h-11 w-full rounded-lg border border-zinc-700 bg-zinc-950 px-3"><option value="">Custom</option>{repositories.map(repository => <option key={repository.name} value={repository.name}>{repository.name}</option>)}</select></label>}
      <div className="mt-4 space-y-4">{Object.entries(inputs).map(([key, input]) => <div key={key} className="block text-sm"><InputLabel name={key} input={input} /><InputField name={key} input={input} raw={values[key]} value={shownValue(values, key, input)} readOnly={readOnly.has(key)} repositories={repositories} onChange={value => setValue(key, value)} /></div>)}</div>
      {error && <p role="alert" className="mt-3 text-sm text-red-400">{error}</p>}
      {error && errorLink && <Link to={errorLink.to} onClick={onClose} className="mt-2 inline-block text-sm text-lw-link underline">{errorLink.label}</Link>}
      </DialogBody><DialogFooter className="flex gap-2"><button type="button" onClick={onClose} className="min-h-11 flex-1 rounded-lg border border-zinc-700">Cancel</button><button disabled={busy} className="min-h-11 flex-1 rounded-lg bg-indigo-500 font-medium">{busy ? "Starting…" : "Start"}</button></DialogFooter>
    </form>
    </DialogPanel>
  </Dialog>
}

function RepositoryWorkflowInput({ label, required, repositories, value, onChange }: {
  label: string
  required?: boolean
  repositories: WorkflowRepositoryPreset[]
  value: string
  onChange: (value: string) => void
}) {
  const preset = repositories.find(repository => repository.name === value)
  const custom = value !== "" && !preset
  let fields = { repo: "", primary_dir: "", base_branch: "main" }
  if (custom) {
    try { fields = { ...fields, ...JSON.parse(value) } } catch { /* keep editable defaults */ }
  }
  const update = (field: keyof typeof fields, next: string) => onChange(JSON.stringify({ ...fields, [field]: next }))
  return <div className="mt-1 space-y-2">
    <select aria-label={label} required={required} value={preset?.name ?? (custom ? "__custom" : "")} onChange={event => {
      if (event.target.value === "__custom") onChange(JSON.stringify(fields))
      else onChange(event.target.value)
    }} className="min-h-11 w-full rounded-lg border border-zinc-700 bg-zinc-950 px-3">
      <option value="">Select repository</option>
      {repositories.map(repository => <option key={repository.name} value={repository.name}>{repository.name}</option>)}
      <option value="__custom">Custom</option>
    </select>
    {custom && <div className="space-y-2 rounded-lg border border-zinc-800 p-3">
      <label className="block text-xs text-zinc-400">Repository<input aria-label={`${label} repository`} required value={fields.repo} placeholder="owner/repo" onChange={event => update("repo", event.target.value)} className="mt-1 min-h-11 w-full rounded-lg border border-zinc-700 bg-zinc-950 px-3 text-sm text-zinc-100" /></label>
      <label className="block text-xs text-zinc-400">Primary directory<input aria-label={`${label} primary directory`} required value={fields.primary_dir} onChange={event => update("primary_dir", event.target.value)} className="mt-1 min-h-11 w-full rounded-lg border border-zinc-700 bg-zinc-950 px-3 text-sm text-zinc-100" /></label>
      <label className="block text-xs text-zinc-400">Base branch<input aria-label={`${label} base branch`} value={fields.base_branch} onChange={event => update("base_branch", event.target.value)} className="mt-1 min-h-11 w-full rounded-lg border border-zinc-700 bg-zinc-950 px-3 text-sm text-zinc-100" /></label>
    </div>}
  </div>
}
