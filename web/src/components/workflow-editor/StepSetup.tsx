import { setField, type WorkflowEdit, type WorkflowValues, type YamlPath } from "../../lib/workflowDocument"
import { references } from "../../lib/workflowExpressions"
import { pathText, workflowSchema } from "../../lib/workflowSchema"
import type { WorkflowDiagnostic } from "../../lib/workflowFiles"
import { Problems } from "./builderFields"
import { problemsAt } from "./builderStyles"
import { STEP_KEYS, stepForm, type BuilderOptions, type FieldSpec, type StepValue } from "./stepForms"
import { startOf } from "./stepRows"
import { OutputsEditor, StepIdField, WithField } from "./StepsEditor"

// The inspector's Setup and Outputs of a step (LEG-508): the essential
// fields first, in the catalog's order under their plain labels; the rest
// under More options, which says what of them is set; how the step starts;
// its ID. Outputs lists what later steps can read.

type Props = {
  step: StepValue
  index: number
  text: string
  values: WorkflowValues
  diagnostics: WorkflowDiagnostic[]
  options: BuilderOptions
  apply: (make: () => WorkflowEdit) => void
}

const STARTS: { value: string; label: string }[] = [
  { value: "automatic", label: "Right after" },
  { value: "manual", label: "You start it" },
  { value: "agent_ended", label: "When the agent ends" },
]

const isSet = (value: unknown) => value !== undefined && value !== null && value !== "" && value !== false

const capitalized = (name: string) => name.charAt(0).toUpperCase() + name.slice(1)

export function StepSetup({ step, index, text, values, diagnostics, options, apply }: Props) {
  const path: YamlPath = ["steps", index]
  const id = step.id ?? ""
  const form = step.type ? stepForm(step.type) : null
  const withValues = step.with ?? {}
  const at = (sub: YamlPath) => problemsAt(diagnostics, pathText([...path, ...sub]))
  const setWith = (name: string, value: unknown) => apply(() => setField(text, [...path, "with", name], value))
  const refs = references(values, { kind: "with", step: index }).map(r => r.expression).filter(e => !e.endsWith("."))
  const taskRefs = ["${{ workflow.task }}", ...Object.entries(values.inputs ?? {}).filter(([, input]) => input?.type === "task").map(([name]) => `\${{ workflow.inputs.${name} }}`)]
  const exclusive = (form?.groups ?? []).filter(g => g.kind === "exactly_one")
  const groupOf = (name: string) => exclusive.find(g => g.fields.includes(name))
  const others = [...Object.keys(withValues).filter(key => !form?.fields.some(f => f.name === key)).map(key => `with.${key}`), ...Object.keys(step).filter(key => !STEP_KEYS.includes(key) && key !== "start")]

  const field = (spec: FieldSpec) => <WithField key={spec.name} rich spec={spec} stepId={id} value={withValues[spec.name]} onChange={value => setWith(spec.name, value)} refs={refs} taskRefs={taskRefs} options={options} agent={String(withValues.agent ?? "")} problems={at(["with", spec.name])} />

  // An exactly-one group is one segmented choice where its first field
  // would be, then the field chosen.
  const drawn = new Set<string>()
  const draw = (spec: FieldSpec) => {
    const group = groupOf(spec.name)
    if (!group) return field(spec)
    if (drawn.has(group.fields.join())) return null
    drawn.add(group.fields.join())
    const chosen = group.fields.find(f => f in withValues) ?? group.fields[0]
    const chosenSpec = form!.fields.find(f => f.name === chosen)!
    return <div key={group.fields.join()} className="flex flex-col gap-1.5">
      <div role="radiogroup" aria-label={`Step ${id}: ${group.fields.join(" or ")}`} className="inline-flex self-start rounded-md border border-lw-line p-0.5">
        {group.fields.map(name => <button key={name} type="button" role="radio" aria-checked={chosen === name} className={`min-h-8 rounded px-2.5 text-xs ${chosen === name ? "bg-lw-raised text-lw-text" : "text-lw-text-2"}`}
          onClick={() => { if (chosen !== name) apply(() => setField(setField(text, [...path, "with", chosen], undefined).text, [...path, "with", name], "")) }}>{capitalized(name)}</button>)}
      </div>
      {field(chosenSpec)}
    </div>
  }

  const fields = form?.fields ?? []
  // A field of a group goes with the group, essential if any of it is.
  const essential = (spec: FieldSpec) => spec.essential || Boolean(groupOf(spec.name)?.fields.some(name => fields.find(f => f.name === name)?.essential))
  const first = fields.filter(essential)
  const rest = fields.filter(spec => !essential(spec))
  const setInRest = rest.filter(spec => isSet(withValues[spec.name])).map(spec => spec.label)
  const start = startOf(step)
  const startKey = values.schema_version === 2 ? "start" : "trigger"

  return <div className="flex flex-col gap-3">
    <Problems diagnostics={at(["type"])} />
    {first.map(draw)}
    {rest.length > 0 && <details className="group rounded-lg border border-lw-line" open={rest.some(spec => at(["with", spec.name]).length > 0) || undefined}>
      <summary className="flex min-h-9 cursor-pointer list-none items-center gap-2 px-3 text-sm text-lw-text-2">
        <span className="text-lw-text">More options ({rest.length})</span>
        {setInRest.length > 0 && <span className="min-w-0 flex-1 truncate text-xs">{setInRest.join(", ")}</span>}
      </summary>
      <div className="flex flex-col gap-3 border-t border-lw-line p-3">{rest.map(draw)}</div>
    </details>}
    {others.length > 0 && <p className="text-xs text-lw-text-2">Other fields (edit in YAML): <span className="font-lw-mono">{others.join(", ")}</span></p>}
    <fieldset className="flex flex-col gap-1.5 border-t border-lw-line pt-3">
      <legend className="text-xs text-lw-text-2">How it starts</legend>
      <div role="radiogroup" aria-label={`How step ${id} starts`} className="inline-flex flex-wrap self-start rounded-md border border-lw-line p-0.5">
        {STARTS.map(option => <button key={option.value} type="button" role="radio" aria-checked={start === option.value} className={`min-h-8 rounded px-2.5 text-xs ${start === option.value ? "bg-lw-raised text-lw-text" : "text-lw-text-2"}`}
          onClick={() => apply(() => setField(text, [...path, startKey], option.value === "automatic" ? undefined : option.value))}>{option.label}</button>)}
      </div>
      <Problems diagnostics={at([startKey])} />
    </fieldset>
    <footer className="border-t border-lw-line pt-3">
      <StepIdField step={step} index={index} text={text} values={values} apply={apply} diagnostics={diagnostics} hint="Used in references: steps.<ID>.outputs" />
    </footer>
  </div>
}

type CatalogOutput = { name: string; type: string; description?: string }

/** StepOutputs is what later steps can read of a step: its type's own
 * outputs (from the catalog), then the outputs it declares. */
export function StepOutputs({ step, index, text, diagnostics, apply }: Omit<Props, "options" | "values">) {
  const id = step.id ?? ""
  const path: YamlPath = ["steps", index]
  const extension = step.type ? workflowSchema.definitions[`with.${step.type}`]?.["x-legato"] : undefined
  const own: CatalogOutput[] = extension?.outputs ?? []
  return <div className="flex flex-col gap-3">
    {own.length > 0 && <section aria-label={`What ${id} gives`} className="flex flex-col gap-1">
      <h3 className="text-xs text-lw-text-2">Later steps can read</h3>
      <ul className="flex flex-col divide-y divide-lw-line rounded-lg border border-lw-line text-xs">
        {own.map(output => <li key={output.name} className="flex flex-wrap items-baseline gap-x-2 px-2 py-1.5">
          <span className="font-lw-mono text-lw-text">{output.name}</span>
          <span className="text-lw-faint">{output.type}</span>
          {output.description && <span className="text-lw-text-2">{output.description}</span>}
          <code className="ml-auto font-lw-mono text-[11px] text-lw-faint">{`steps.${id}.outputs.${output.name}`}</code>
        </li>)}
      </ul>
    </section>}
    {extension?.declared_outputs && <p className="text-xs text-lw-text-2">The agent completes this step with the outputs declared here.</p>}
    <OutputsEditor step={step} id={id} path={path} text={text} apply={apply} problems={problemsAt(diagnostics, pathText([...path, "outputs"]))} taken={[]} />
  </div>
}
