import { Component, useCallback, useMemo, useRef, useState, type ReactNode } from "react"
import { inputsOf, parseWorkflow, shapeProblem, WorkflowDocumentError, type WorkflowEdit } from "../../lib/workflowDocument"
import type { SchedulePreview, WorkflowDiagnostic } from "../../lib/workflowFiles"
import { Button } from "../ui/Button"
import { PendingDrafts } from "./pendingDrafts"
import { DetailsForm, InputCard, NewInputForm } from "./itemForms"
import { StepsEditor } from "./StepsEditor"
import { TriggersEditor } from "./TriggersEditor"
import { noOptions, type BuilderOptions } from "./stepForms"

export type WorkflowBuilderProps = {
  text: string
  /** onEdit is told each change the builder makes, as one undo step. */
  onEdit: (edit: WorkflowEdit) => void
  diagnostics?: WorkflowDiagnostic[]
  /** nameFixed: an existing workflow's name can't change (LEG-460). */
  nameFixed?: boolean
  onEditYaml: () => void
  /** onPending is told whether a field holds text not yet written. */
  onPending?: (pending: boolean) => void
  /** options are what the pickers offer. */
  options?: BuilderOptions
  /** schedule is the server's preview of the schedule trigger. */
  schedule?: SchedulePreview | null
}

// WorkflowBuilder is the Visual side of the editor: the workflow's details
// and inputs as forms, over the same text the YAML side edits
// (lib/workflowDocument). Steps and triggers are edited in YAML until
// their sections land.
function Builder({ text, onEdit, diagnostics = [], nameFixed, onEditYaml, options = noOptions, schedule }: WorkflowBuilderProps) {
  const parsed = useMemo(() => parseWorkflow(text), [text])
  const [error, setError] = useState("")
  if (parsed.errors.length > 0) {
    return <div role="alert" className="flex flex-col items-start gap-2 rounded-xl border border-lw-danger-line bg-lw-error-soft p-4 text-sm text-lw-danger">
      <p>The YAML doesn't parse, so it can't be shown here: {parsed.errors[0]}</p>
      <Button size="sm" variant="outline" onClick={onEditYaml}>Edit as YAML</Button>
    </div>
  }
  const shape = shapeProblem(parsed.values)
  if (shape) {
    return <div role="alert" className="flex flex-col items-start gap-2 rounded-xl border border-lw-line bg-lw-surface p-4 text-sm">
      <p>This file can't be shown as forms yet: {shape}.</p>
      <Button size="sm" variant="outline" onClick={onEditYaml}>Edit as YAML</Button>
    </div>
  }
  if (parsed.readOnly) {
    return <div role="alert" className="flex flex-col items-start gap-2 rounded-xl border border-lw-line bg-lw-surface p-4 text-sm">
      <p>This file can't be edited here: {parsed.readOnly}.</p>
      <Button size="sm" variant="outline" onClick={onEditYaml}>Edit as YAML</Button>
    </div>
  }
  const values = parsed.values
  const apply = (make: () => WorkflowEdit) => {
    try {
      setError("")
      const result = make()
      if (result.text !== text) onEdit(result)
    } catch (reason) {
      setError(reason instanceof WorkflowDocumentError ? reason.message : String(reason))
    }
  }
  const inputs = inputsOf(values)

  return <div className="flex flex-col gap-5 text-sm">
    {error && <p role="alert" className="text-sm text-lw-danger">{error}</p>}
    <section aria-label="Details" className="flex flex-col gap-3 rounded-xl border border-lw-line bg-lw-surface p-4">
      <h2 className="text-xs font-semibold uppercase tracking-wide text-lw-text-2">Details</h2>
      <DetailsForm text={text} values={values} diagnostics={diagnostics} apply={apply} nameFixed={nameFixed} />
    </section>

    <section aria-label="Inputs" className="flex flex-col gap-3 rounded-xl border border-lw-line bg-lw-surface p-4">
      <h2 className="text-xs font-semibold uppercase tracking-wide text-lw-text-2">Inputs</h2>
      {Object.keys(inputs).length === 0 && <p className="text-lw-text-2">This workflow takes no inputs.</p>}
      <ul className="flex flex-col gap-3">
        {Object.entries(inputs).map(([name, input]) => <li key={name} aria-label={`Input ${name}`} className="flex flex-col gap-2 rounded-lg border border-lw-line p-3">
          <InputCard text={text} values={values} diagnostics={diagnostics} apply={apply} name={name} input={input} />
        </li>)}
      </ul>
      <NewInputForm text={text} apply={apply} />
    </section>

    <StepsEditor text={text} values={values} diagnostics={diagnostics} options={options} apply={apply} />

    <TriggersEditor text={text} values={values} diagnostics={diagnostics} options={options} apply={apply} schedule={schedule} />
  </div>
}

type BoundaryProps = { text: string; onEditYaml: () => void; children: ReactNode }

// Boundary keeps a form that fails to draw from taking the editor down: it
// says so and offers YAML, and tries again when the text changes.
class Boundary extends Component<BoundaryProps, { failed: string | null; text: string }> {
  state = { failed: null as string | null, text: this.props.text }
  static getDerivedStateFromError(error: unknown) { return { failed: error instanceof Error ? error.message : String(error) } }
  static getDerivedStateFromProps(props: BoundaryProps, state: { failed: string | null; text: string }) {
    return props.text !== state.text ? { failed: null, text: props.text } : null
  }
  render() {
    if (!this.state.failed) return this.props.children
    return <div role="alert" className="flex flex-col items-start gap-2 rounded-xl border border-lw-danger-line bg-lw-error-soft p-4 text-sm text-lw-danger">
      <p>The forms can't show this file: {this.state.failed}</p>
      <Button size="sm" variant="outline" onClick={this.props.onEditYaml}>Edit as YAML</Button>
    </div>
  }
}

export default function WorkflowBuilder(props: WorkflowBuilderProps) {
  const { onPending } = props
  const pending = useRef(new Set<string>())
  const report = useCallback((id: string, isPending: boolean) => {
    const had = pending.current.size > 0
    if (isPending) pending.current.add(id)
    else pending.current.delete(id)
    if (had !== pending.current.size > 0) onPending?.(pending.current.size > 0)
  }, [onPending])
  return <PendingDrafts.Provider value={report}>
    <Boundary text={props.text} onEditYaml={props.onEditYaml}><Builder {...props} /></Boundary>
  </PendingDrafts.Provider>
}
