import { useState } from "react"
import { Plus, Trash2 } from "lucide-react"
import { inputsOf, setField, setTrigger, triggersOf, type WorkflowEdit, type WorkflowValues, type YamlPath } from "../../lib/workflowDocument"
import { references } from "../../lib/workflowExpressions"
import { pathText } from "../../lib/workflowSchema"
import type { SchedulePreview, WorkflowDiagnostic } from "../../lib/workflowFiles"
import { Button } from "../ui/Button"
import { CommitField, Problems, ReferencePicker } from "./builderFields"
import { fieldClass, labelClass, problemsAt, valueText } from "./builderStyles"
import type { BuilderOptions } from "./stepForms"
import { conditionFields, isChatKind, KIND_TITLES, newTrigger, textExclusionOf, textMatchOf, withTextTest, OPERATORS, operandKind, operandOf, operatorOf, scalarText, triggerKinds, withOperator, type Condition, type TextMatch, type OperandKind, type Operator } from "./triggerForms"

type Props = {
  text: string
  values: WorkflowValues
  diagnostics: WorkflowDiagnostic[]
  options: BuilderOptions
  apply: (make: () => WorkflowEdit) => void
  schedule?: SchedulePreview | null
}


// TriggersEditor is the Triggers section of the builder: running by hand,
// which every workflow can, then a card for each trigger under on:.
export function TriggersEditor({ text, values, diagnostics, options, apply, schedule }: Props) {
  const triggers = triggersOf(values)
  const present = Object.keys(triggers)
  const missing = triggerKinds.filter(t => !present.includes(t.kind))
  const [adding, setAdding] = useState(false)
  const add = (kind: string) => {
    apply(() => setTrigger(text, kind, newTrigger(kind, options)))
    setAdding(false)
  }
  return <section aria-label="Triggers" className="flex flex-col gap-3 rounded-xl border border-lw-line bg-lw-surface p-4">
    <h2 className="text-xs font-semibold uppercase tracking-wide text-lw-text-2">Triggers</h2>
    <p className="rounded-lg border border-lw-line bg-lw-ground p-3 text-lw-text-2"><span className="text-lw-text">Run by hand</span>: from the Workflows tab, a card or the API. Every workflow can.</p>
    {present.map(kind => <TriggerCard key={kind} kind={kind} trigger={triggers[kind] ?? {}} text={text} values={values} diagnostics={diagnostics} options={options} apply={apply} schedule={kind === "schedule" ? schedule : null} />)}
    {missing.length > 0 && (adding
      ? <div role="group" aria-label="Trigger kinds" className="flex flex-col gap-2">
        {missing.map(t => <button key={t.kind} type="button" className="flex flex-col items-start rounded-lg border border-lw-line p-2 text-left hover:bg-lw-raised" onClick={() => add(t.kind)}>
          <span className="text-sm font-medium">{KIND_TITLES[t.kind] ?? t.kind}</span>
          <span className="text-xs text-lw-text-2">{t.description}</span>
        </button>)}
        <Button type="button" size="sm" variant="ghost" className="self-start" onClick={() => setAdding(false)}>Cancel</Button>
      </div>
      : <Button type="button" size="sm" variant="outline" className="self-start" onClick={() => setAdding(true)}><Plus size={13} />Add trigger</Button>)}
  </section>
}

export function TriggerCard({ kind, trigger, text, values, diagnostics, options, apply, schedule }: Omit<Props, "schedule"> & { kind: string; trigger: Record<string, unknown>; schedule?: SchedulePreview | null }) {
  const path: YamlPath = ["on", kind]
  const set = (sub: YamlPath, value: unknown) => apply(() => setField(text, [...path, ...sub], value === "" ? undefined : value))
  const at = (sub: YamlPath) => problemsAt(diagnostics, pathText([...path, ...sub]))
  const invalid = (sub: YamlPath) => at(sub).length > 0 ? { "aria-invalid": true as const } : {}
  const title = KIND_TITLES[kind] ?? kind
  const enabled = trigger.enabled !== false
  const inputs = Object.keys(inputsOf(values))
  const mapping = (trigger.inputs ?? {}) as Record<string, unknown>
  const refs = references(values, { kind: "trigger", trigger: kind }).map(r => r.expression)
  return <article aria-label={title} className="flex flex-col gap-3 rounded-lg border border-lw-line bg-lw-ground p-3">
    <header className="flex items-center justify-between gap-2">
      <h3 className="text-sm font-medium">{title} <span className="font-lw-mono text-xs text-lw-faint">{kind}</span></h3>
      <Button type="button" variant="ghost" size="icon-sm" aria-label={`Remove the ${kind} trigger`} onClick={() => apply(() => setTrigger(text, kind, null))}><Trash2 size={13} /></Button>
    </header>

    {kind === "task.entered_column" && <div className="grid gap-2 sm:grid-cols-2">
      <label className={labelClass}>Column
        <CommitField aria-label={`Column of the ${kind} trigger`} list="trigger-columns" value={valueText(trigger.column)} onCommit={value => set(["column"], value)} {...invalid(["column"])} />
        <datalist id="trigger-columns">{options.columns.map(column => <option key={column} value={column} />)}</datalist>
        <Problems diagnostics={at(["column"])} />
      </label>
      <label className="flex items-center gap-1.5 self-end text-xs text-lw-text-2"><input type="checkbox" aria-label="Also on cards handed off to this Legato" checked={trigger.handed_off === true} onChange={event => set(["handed_off"], event.target.checked || undefined)} />Also on cards handed off to this Legato</label>
    </div>}

    {isChatKind(kind) && <ChatFields kind={kind} trigger={trigger} bots={options.bots ?? []} set={set} at={at} />}

    {kind === "schedule" && <ScheduleFields trigger={trigger} set={set} replace={next => apply(() => setTrigger(text, kind, next))} at={at} preview={schedule} />}

    <div className="flex flex-wrap gap-4 text-xs text-lw-text-2">
      <label className="flex items-center gap-1.5"><input type="checkbox" aria-label={`The ${kind} trigger starts runs by itself`} checked={enabled} onChange={event => set(["enabled"], event.target.checked ? undefined : false)} />Starts runs by itself (in the file)</label>
      <label className="flex items-center gap-1.5"><input type="checkbox" aria-label={`The ${kind} trigger asks first`} checked={trigger.confirm === true} onChange={event => set(["confirm"], event.target.checked || undefined)} />Asks you before each run</label>
      <label className="flex items-center gap-1.5">Runs a day at most
        <span className="w-16"><CommitField aria-label={`Most runs a day of the ${kind} trigger`} inputMode="numeric" placeholder="3" value={scalarText(trigger.max_runs_per_day)}
          onCommit={raw => { const value = raw.trim(); set(["max_runs_per_day"], value === "" ? undefined : Number.isInteger(Number(value)) ? Number(value) : value) }} /></span>
      </label>
    </div>
    <Problems diagnostics={at(["max_runs_per_day"])} />

    {inputs.length > 0 && <fieldset className="flex flex-col gap-2">
      <legend className="text-xs text-lw-text-2">The run's inputs</legend>
      {inputs.map(input => <div key={input} className="grid items-center gap-2 sm:grid-cols-[8rem_1fr_auto]">
        <span className="font-lw-mono text-xs">{input}</span>
        <CommitField aria-label={`Input ${input} from the ${kind} trigger`} value={scalarText(mapping[input])} placeholder="Given when it starts" onCommit={value => set(["inputs", input], value)} />
        <ReferencePicker name={`input ${input} from the ${kind} trigger`} options={refs} onPick={expression => set(["inputs", input], `\${{ ${expression} }}`)} />
      </div>)}
      <Problems diagnostics={at(["inputs"])} />
    </fieldset>}

    {kind === "task.entered_column" && <fieldset className="flex flex-col gap-2">
      <legend className="text-xs text-lw-text-2">Only when</legend>
      {trigger.conditions
        ? <>
          <ConditionNode condition={trigger.conditions as Condition} label="Conditions" onChange={next => set(["conditions"], next)} onRemove={() => set(["conditions"], undefined)} />
          <Problems diagnostics={at(["conditions"])} />
        </>
        : <Button type="button" size="sm" variant="outline" className="self-start" onClick={() => set(["conditions"], { all: [{ field: "task.priority", equals: "" }] })}><Plus size={13} />Add a condition</Button>}
    </fieldset>}
  </article>
}

// ChatFields are a chat trigger's: the bot (one offered, or a typed name),
// where and from whom, how its text matches, what the loop stop counts by
// and whether other bots' messages count. A button trigger matches the
// clicked button's value instead, by who clicked, and counts runs by the
// message, so it has neither of the last two.
function ChatFields({ kind, trigger, bots, set, at }: { kind: string; trigger: Record<string, unknown>; bots: string[]; set: (sub: YamlPath, value: unknown) => void; at: (sub: YamlPath) => WorkflowDiagnostic[] }) {
  const invalid = (sub: YamlPath) => at(sub).length > 0 ? { "aria-invalid": true as const } : {}
  const button = kind === "chat.button"
  const matchKey = button ? "value" : "text"
  const matchLabel = button ? "Value" : "Text"
  const match = textMatchOf(trigger[matchKey])
  const exclusion = textExclusionOf(trigger.text)
  // The builder shows one exclusion; with both, the other is kept as it is.
  const bothExclusions = exclusion.mode === "not_regex" && "not_contains" in ((trigger.text ?? {}) as TextMatch)
  const listId = `trigger-bots-${kind.replace(/\W/g, "-")}`
  return <div className="flex flex-col gap-2">
    <div className="grid gap-2 sm:grid-cols-3">
      <label className={labelClass}>Bot
        <CommitField aria-label={`Bot of the ${kind} trigger`} list={listId} placeholder="slack" value={valueText(trigger.bot)} onCommit={value => set(["bot"], value)} {...invalid(["bot"])} />
        <datalist id={listId}>{bots.map(bot => <option key={bot} value={bot} />)}</datalist>
        <Problems diagnostics={at(["bot"])} />
      </label>
      <label className={labelClass}>Channel
        <CommitField aria-label={`Channel of the ${kind} trigger`} placeholder="Any channel" value={valueText(trigger.channel)} onCommit={value => set(["channel"], value)} {...invalid(["channel"])} />
        <Problems diagnostics={at(["channel"])} />
      </label>
      <label className={labelClass}>{button ? "Who may click" : "From user"}
        <CommitField aria-label={`User of the ${kind} trigger`} placeholder={button ? "Anyone in the channel" : "Anyone"} value={valueText(trigger.user)} onCommit={value => set(["user"], value)} {...invalid(["user"])} />
        <Problems diagnostics={at(["user"])} />
      </label>
    </div>
    <div className="grid gap-2 sm:grid-cols-[10rem_1fr]">
      <label className={labelClass}>{matchLabel}
        <select aria-label={`${matchLabel} match of the ${kind} trigger`} className={fieldClass} value={match.mode} onChange={event => {
          set([matchKey], withTextTest(trigger[matchKey], match.mode, event.target.value, match.pattern))
        }}>
          <option value="">{button ? "Any button" : "Any text"}</option><option value="contains">Contains</option><option value="regex">Matches the regex</option>
        </select>
      </label>
      {match.mode && <label className={labelClass}>{match.mode === "regex" ? (button ? "Regex; named groups are event.match.<name>" : "Regex") : "Words (any case)"}
        <CommitField aria-label={`${matchLabel} of the ${kind} trigger`} value={match.pattern} onCommit={value => set([matchKey], withTextTest(trigger[matchKey], match.mode, match.mode, value))} {...invalid([matchKey])} />
      </label>}
    </div>
    {/* A button's value takes a match alone; exclusions are for message text. */}
    {!button && <>
      <div className="grid gap-2 sm:grid-cols-[10rem_1fr]">
        <label className={labelClass}>Except
          <select aria-label={`Text exclusion of the ${kind} trigger`} className={fieldClass} value={exclusion.mode} disabled={bothExclusions} onChange={event => {
            set(["text"], withTextTest(trigger.text, exclusion.mode, event.target.value, exclusion.pattern))
          }}>
            <option value="">Nothing</option><option value="not_contains">Containing</option><option value="not_regex">Matching the regex</option>
          </select>
        </label>
        {exclusion.mode && <label className={labelClass}>{exclusion.mode === "not_regex" ? "Regex" : "Words (any case)"}
          <CommitField aria-label={`Excluded text of the ${kind} trigger`} value={exclusion.pattern} onCommit={value => set(["text"], withTextTest(trigger.text, exclusion.mode, exclusion.mode, value))} {...invalid(["text"])} />
        </label>}
      </div>
      {bothExclusions && <p className="text-xs text-lw-text-2">Also excludes text containing {scalarText((trigger.text as TextMatch).not_contains)}: edit the YAML to change both.</p>}
    </>}
    <Problems diagnostics={at([matchKey])} />
    {!button && <div className="flex flex-wrap items-center gap-4 text-xs text-lw-text-2">
      <label className="flex items-center gap-1.5">The loop stop counts runs by
        <select aria-label={`Loop stop of the ${kind} trigger counts runs by`} className={`${fieldClass} w-auto`} value={valueText(trigger.loop_item)} onChange={event => set(["loop_item"], event.target.value)}>
          <option value="">Channel (the default)</option><option value="channel">Channel</option><option value="thread">Thread</option>
        </select>
      </label>
      <label className="flex items-center gap-1.5"><input type="checkbox" aria-label={`The ${kind} trigger also runs on other bots' messages`} checked={trigger.include_bots === true} onChange={event => set(["include_bots"], event.target.checked || undefined)} />Also on other bots' messages</label>
      <label className="flex items-center gap-1.5"><input type="checkbox" aria-label={`The ${kind} trigger fires only when no other chat trigger on the bot matched`} checked={trigger.fallback === true} onChange={event => set(["fallback"], event.target.checked || undefined)} />Only when no other trigger matched (fallback)</label>
    </div>}
    <Problems diagnostics={at(["fallback"])} />
  </div>
}

function ScheduleFields({ trigger, set, replace, at, preview }: { trigger: Record<string, unknown>; set: (sub: YamlPath, value: unknown) => void; replace: (next: Record<string, unknown>) => void; at: (sub: YamlPath) => WorkflowDiagnostic[]; preview?: SchedulePreview | null }) {
  const cron = typeof trigger.cron === "string"
  const invalid = (sub: YamlPath) => at(sub).length > 0 ? { "aria-invalid": true as const } : {}
  return <div className="flex flex-col gap-2">
    <div role="radiogroup" aria-label="Schedule" className="inline-flex self-start rounded-md border border-lw-line p-0.5">
      {(["every", "cron"] as const).map(option => <button key={option} type="button" role="radio" aria-checked={(option === "cron") === cron} className={`min-h-7 rounded px-2.5 text-xs ${(option === "cron") === cron ? "bg-lw-raised text-lw-text" : "text-lw-text-2"}`}
        onClick={() => {
          if ((option === "cron") === cron) return
          // One edit: the schedule's way of saying when changes whole.
          const rest = Object.fromEntries(Object.entries(trigger).filter(([key]) => !["every", "at", "on", "cron"].includes(key)))
          replace(option === "cron" ? { cron: "0 9 * * 1-5", ...rest } : { every: "weekdays", at: "09:00", ...rest })
        }}>{option === "every" ? "Every" : "Cron"}</button>)}
    </div>
    {cron
      ? <label className={labelClass}>Cron
        <CommitField aria-label="Cron" value={valueText(trigger.cron)} onCommit={value => set(["cron"], value)} {...invalid(["cron"])} />
        <Problems diagnostics={at(["cron"])} />
      </label>
      : <div className="grid gap-2 sm:grid-cols-3">
        <label className={labelClass}>Every
          <select aria-label="Every" className={fieldClass} value={valueText(trigger.every)} onChange={event => {
            // on is for weekly alone: leaving weekly takes it out in the same edit.
            const next: Record<string, unknown> = { ...trigger, every: event.target.value }
            if (event.target.value !== "weekly") delete next.on
            replace(next)
          }}>
            <option value="daily">Day</option><option value="weekdays">Weekday</option><option value="weekly">Week</option>
          </select>
          <Problems diagnostics={at(["every"])} />
        </label>
        <label className={labelClass}>At
          <CommitField aria-label="At" placeholder="09:00" value={valueText(trigger.at)} onCommit={value => set(["at"], value)} {...invalid(["at"])} />
        </label>
        {trigger.every === "weekly" && <label className={labelClass}>On
          <select aria-label="On" className={fieldClass} value={valueText(trigger.on)} onChange={event => set(["on"], event.target.value)}>
            <option value="">Choose a day</option>
            {["monday", "tuesday", "wednesday", "thursday", "friday", "saturday", "sunday"].map(day => <option key={day} value={day}>{day}</option>)}
          </select>
        </label>}
      </div>}
    <div className="grid gap-2 sm:grid-cols-2">
      <label className={labelClass}>Time zone
        <CommitField aria-label="Time zone" placeholder="This machine's" value={valueText(trigger.timezone)} onCommit={value => set(["timezone"], value)} {...invalid(["timezone"])} />
        <Problems diagnostics={at(["timezone"])} />
      </label>
      <label className={labelClass}>Missed runs
        <select aria-label="Missed runs" className={fieldClass} value={valueText(trigger.catch_up)} onChange={event => set(["catch_up"], event.target.value)}>
          <option value="">Run once, late (the default)</option><option value="once">Run once, late</option><option value="skip">Skip them</option>
        </select>
      </label>
    </div>
    {preview && preview.next.length > 0 && <p aria-label="Next runs" className="text-xs text-lw-text-2">{preview.description}. Next: {preview.next.map(time => new Date(time).toLocaleString()).join(" · ")}</p>}
  </div>
}

// ConditionNode edits one condition: a group (all or any of its
// conditions), a not, or a field compared with a value.
function ConditionNode({ condition, label, onChange, onRemove }: { condition: Condition; label: string; onChange: (next: Condition) => void; onRemove?: () => void }) {
  if (condition.all || condition.any) {
    const mode = condition.all ? "all" : "any"
    const children = (condition.all ?? condition.any)!
    const setChildren = (next: Condition[]) => onChange({ [mode]: next })
    return <div role="group" aria-label={label} className="flex flex-col gap-2 rounded border border-lw-line p-2">
      <div className="flex items-center gap-2 text-xs">
        <select aria-label={`${label}: all or any`} className={`${fieldClass} w-auto`} value={mode} onChange={event => onChange({ [event.target.value]: children })}>
          <option value="all">All of these</option><option value="any">Any of these</option>
        </select>
        {onRemove && <Button type="button" variant="ghost" size="icon-sm" aria-label={`Remove ${label}`} onClick={onRemove}><Trash2 size={13} /></Button>}
      </div>
      {children.map((child, i) => <ConditionNode key={i} condition={child} label={`${label}, ${i + 1}`} onChange={next => setChildren(children.map((c, j) => j === i ? next : c))} onRemove={children.length > 1 ? () => setChildren(children.filter((_, j) => j !== i)) : undefined} />)}
      <div className="flex gap-2">
        <Button type="button" size="sm" variant="ghost" onClick={() => setChildren([...children, { field: "task.priority", equals: "" }])}><Plus size={13} />Condition</Button>
        <Button type="button" size="sm" variant="ghost" onClick={() => setChildren([...children, { any: [{ field: "task.priority", equals: "" }] }])}><Plus size={13} />Group</Button>
        <Button type="button" size="sm" variant="ghost" onClick={() => setChildren([...children, { not: { field: "task.priority", equals: "" } }])}><Plus size={13} />Not</Button>
      </div>
    </div>
  }
  if (condition.not) {
    return <div role="group" aria-label={label} className="flex flex-col gap-1 rounded border border-lw-line p-2">
      <div className="flex items-center gap-2 text-xs text-lw-text-2">Not
        {onRemove && <Button type="button" variant="ghost" size="icon-sm" aria-label={`Remove ${label}`} onClick={onRemove}><Trash2 size={13} /></Button>}
      </div>
      <ConditionNode condition={condition.not} label={`${label}, not`} onChange={next => onChange({ not: next })} />
    </div>
  }
  return <ConditionLeaf condition={condition} label={label} onChange={onChange} onRemove={onRemove} />
}

// ConditionLeaf is a field compared with a value, of a type chosen here.
function ConditionLeaf({ condition, label, onChange, onRemove }: { condition: Condition; label: string; onChange: (next: Condition) => void; onRemove?: () => void }) {
  const operator = operatorOf(condition)
  const value = condition[operator]
  const sample = operator === "in" ? undefined : value
  // The type is the value's, or the one chosen while there is no value yet.
  const [chosen, setChosen] = useState<OperandKind>(operandKind(sample))
  const kind = sample === undefined || sample === "" ? chosen : operandKind(sample)
  const typed = (raw: string, as: OperandKind) => operandOf(raw, as)
  return <div role="group" aria-label={label} className="grid items-center gap-2 sm:grid-cols-[1fr_8rem_1fr_auto]">
    <CommitField aria-label={`${label}: field`} list="condition-fields" value={condition.field ?? ""} onCommit={field => onChange({ ...condition, field })} />
    <datalist id="condition-fields">{conditionFields.map(field => <option key={field} value={field} />)}</datalist>
    <select aria-label={`${label}: compare`} className={fieldClass} value={operator} onChange={event => onChange(withOperator(condition, event.target.value as Operator, operator === "in" || event.target.value === "in" || event.target.value === "exists" ? undefined : value))}>
      {OPERATORS.map(op => <option key={op} value={op}>{op === "not_equals" ? "is not" : op === "equals" ? "is" : op === "in" ? "is one of" : "exists"}</option>)}
    </select>
    {operator === "exists"
      ? <select aria-label={`${label}: value`} className={fieldClass} value={String(value !== false)} onChange={event => onChange(withOperator(condition, "exists", event.target.value === "true"))}>
        <option value="true">yes</option><option value="false">no</option>
      </select>
      : operator === "in"
        ? <InList label={label} items={Array.isArray(value) ? value : []} chosen={chosen} onChoose={setChosen} onChange={items => onChange(withOperator(condition, "in", items))} />
        : <div className="flex gap-1">
          <CommitField aria-label={`${label}: value`} placeholder="value" value={scalarText(value)} onCommit={raw => onChange(withOperator(condition, operator, typed(raw, kind)))} />
          <KindSelect label={`${label}: value type`} kind={kind} onChange={as => { setChosen(as); onChange(withOperator(condition, operator, typed(scalarText(value), as))) }} />
        </div>}
    {onRemove ? <Button type="button" variant="ghost" size="icon-sm" aria-label={`Remove ${label}`} onClick={onRemove}><Trash2 size={13} /></Button> : <span />}
  </div>
}

function KindSelect({ label, kind, onChange }: { label: string; kind: OperandKind; onChange: (kind: OperandKind) => void }) {
  return <select aria-label={label} className={`${fieldClass} w-24`} value={kind} onChange={event => onChange(event.target.value as OperandKind)}>
    <option value="text">text</option><option value="number">number</option><option value="boolean">true/false</option>
  </select>
}

// InList is an in condition's values, one field each with its own type, so
// editing one changes no other ([foo, true] stays a text and a boolean),
// and a value with a comma in it stays one value.
function InList({ label, items, chosen, onChoose, onChange }: { label: string; items: unknown[]; chosen: OperandKind; onChoose: (kind: OperandKind) => void; onChange: (items: unknown[]) => void }) {
  const [adding, setAdding] = useState("")
  const add = () => { if (adding !== "") { onChange([...items, operandOf(adding, chosen)]); setAdding("") } }
  return <div className="flex flex-col gap-1">
    {items.map((item, i) => <div key={i} className="flex gap-1">
      <CommitField aria-label={`${label}: value ${i + 1}`} value={scalarText(item)} onCommit={raw => onChange(items.map((other, j) => j === i ? operandOf(raw, operandKind(item)) : other))} />
      <KindSelect label={`${label}: value ${i + 1} type`} kind={operandKind(item)} onChange={as => onChange(items.map((other, j) => j === i ? operandOf(scalarText(item), as) : other))} />
      <Button type="button" variant="ghost" size="icon-sm" aria-label={`Remove value ${i + 1} of ${label}`} onClick={() => onChange(items.filter((_, j) => j !== i))}><Trash2 size={13} /></Button>
    </div>)}
    <div className="flex gap-1">
      <input aria-label={`${label}: new value`} className={fieldClass} value={adding} placeholder="value" onChange={event => setAdding(event.target.value)} onKeyDown={event => { if (event.key === "Enter") { event.preventDefault(); add() } }} />
      <KindSelect label={`${label}: new value type`} kind={chosen} onChange={onChoose} />
      <Button type="button" variant="ghost" size="sm" aria-label={`Add a value to ${label}`} disabled={adding === ""} onClick={add}><Plus size={13} /></Button>
    </div>
  </div>
}
