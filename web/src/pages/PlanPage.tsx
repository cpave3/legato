import { memo, useCallback, useEffect, useRef, useState, type KeyboardEvent, type MouseEvent, type ReactNode } from "react"
import ReactMarkdown from "react-markdown"
import remarkGfm from "remark-gfm"
import { ArrowLeft, MessageSquarePlus, PanelRight, Send, X } from "lucide-react"
import { Link, useNavigate, useParams } from "react-router-dom"
import { usePlan } from "../hooks/usePlan"
import { useServer } from "../hooks/useServer"
import { addPlanComment, askPlanQuestion, planAction, respondToPlanQuestion, updatePlanComment, type PlanComment, type PlanOption, type PlanQuestion } from "../lib/plan"
import { ReviewMessageBody } from "../components/ReviewMessageBody"
import { GoToAgentButton } from "../components/GoToAgentButton"
import { Button } from "../components/ui/Button"
import { InlineNotice } from "../components/ui/InlineNotice"
import { VisualMarkdownPre } from "../components/WiremarkDiagram"

type SourcePosition = { start?: { offset?: number }; end?: { offset?: number } }
type SelectedBlock = { start: number; end: number; characterStart: number; characterEnd: number; text: string; label: string }

type MarkdownNode = { position?: SourcePosition; properties?: Record<string, unknown> }
type MdastNode = { data?: { hProperties?: Record<string, unknown> } }
type Root = { children?: MdastNode[] }

function remarkOutermostBlocks() {
  return (tree: Root) => {
    for (const node of tree.children ?? []) {
      node.data ??= {}
      node.data.hProperties ??= {}
      node.data.hProperties["data-plan-outer"] = "true"
    }
  }
}

function keyboardSubmit(event: KeyboardEvent<HTMLTextAreaElement>, submit: () => void, disabled: boolean) {
  if (event.key !== "Enter" || (!event.metaKey && !event.ctrlKey) || disabled) return
  event.preventDefault()
  submit()
}

function byteOffset(source: string, characterOffset: number) {
  return new TextEncoder().encode(source.slice(0, characterOffset)).length
}

function sourceBlock(markdown: string, node?: MarkdownNode, kind = "block"): SelectedBlock | null {
  const start = node?.position?.start?.offset
  const end = node?.position?.end?.offset
  if (start === undefined || end === undefined || end <= start) return null
  const text = markdown.slice(start, end)
  const outermost = node?.properties?.dataPlanOuter === "true" || node?.properties?.["data-plan-outer"] === "true"
  const fencedCode = kind === "code block" && /^(?:```|~~~)/.test(text)
  if (!outermost && !fencedCode) return null
  const excerpt = text.replace(/^#{1,6}\s+/, "").replace(/^[-*>+\d.)\s]+/, "").replace(/\s+/g, " ").trim().slice(0, 72)
  return { start: byteOffset(markdown, start), end: byteOffset(markdown, end), characterStart: start, characterEnd: end, text, label: `Comment on ${kind} beginning ${excerpt}` }
}

function commentState(comments: PlanComment[]) {
  if (comments.some(comment => comment.submitted_at)) return "submitted"
  if (comments.length > 0) return "draft"
  return undefined
}

function EditableComment({ comment, onSave }: { comment: PlanComment; onSave: (body: string) => Promise<void> }) {
  const [editing, setEditing] = useState(false)
  const [body, setBody] = useState(comment.body)
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState<string | null>(null)

  async function save() {
    if (!body.trim() || saving) return
    setSaving(true)
    setError(null)
    try {
      await onSave(body.trim())
      setEditing(false)
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : String(cause))
    } finally {
      setSaving(false)
    }
  }

  if (editing) return <div className="rounded bg-indigo-950/35 p-2 text-sm text-zinc-200">
    <textarea aria-label="Edit comment" value={body} onChange={event => setBody(event.target.value)} onKeyDown={event => keyboardSubmit(event, () => void save(), saving || !body.trim())} className="w-full rounded border border-zinc-700 bg-zinc-900 p-2"/>
    {error && <p role="alert" className="mt-1 text-xs text-red-300">{error}</p>}
    <div className="mt-2 flex gap-2"><button disabled={saving || !body.trim()} onClick={() => void save()} className="text-xs text-indigo-300">{saving ? "Saving…" : "Save"}</button><button disabled={saving} onClick={() => { setBody(comment.body); setError(null); setEditing(false) }} className="text-xs text-zinc-400">Cancel</button></div>
  </div>
  return <div className="rounded bg-indigo-950/35 p-2 text-sm text-zinc-200">
    <p>{comment.body}</p>
    <div className="mt-1 flex items-center justify-between"><span className="text-[10px] font-semibold uppercase tracking-wide text-indigo-300">{comment.submitted_at ? "Submitted" : "Draft"}</span><button aria-label="Edit comment" onClick={() => setEditing(true)} className="text-xs text-zinc-400 hover:text-indigo-300">Edit</button></div>
  </div>
}

function BlockFrame({ block, selected, comments, coveredComments, onUpdateComment, onSelect, onDragStart, onDragEnter, onDragEnd, children }: {
  block: SelectedBlock | null
  selected: boolean
  comments: PlanComment[]
  coveredComments: PlanComment[]
  onUpdateComment: (comment: PlanComment, body: string) => Promise<void>
  onSelect: (block: SelectedBlock) => void
  onDragStart: (block: SelectedBlock, event: MouseEvent) => void
  onDragEnter: (block: SelectedBlock) => void
  onDragEnd: () => void
  children: ReactNode
}) {
  if (!block) return <>{children}</>
  const state = commentState(coveredComments)
  return <div id={`plan-block-${block.start}`} className={`group relative my-1 rounded border-l-2 pl-7 pr-2 transition-colors ${comments.length > 0 ? "lg:grid lg:grid-cols-[minmax(0,1fr)_18rem] lg:gap-4" : ""} ${selected ? "border-indigo-400 bg-indigo-950/35" : state ? "border-amber-500/70 bg-amber-950/15" : "border-transparent hover:border-zinc-700 hover:bg-zinc-900/40"}`}>
    <button
      type="button"
      aria-label={block.label}
      aria-pressed={selected}
      data-commented={state}
      onClick={() => onSelect(block)}
      onMouseDown={event => onDragStart(block, event)}
      onMouseEnter={() => onDragEnter(block)}
      onMouseUp={onDragEnd}
      className="absolute left-1 top-2 rounded p-1 text-zinc-600 opacity-60 hover:bg-zinc-800 hover:text-indigo-300 focus:opacity-100 group-hover:opacity-100"
    ><MessageSquarePlus size={14}/></button>
    <div>{children}</div>
    {comments.length > 0 && <div className="mb-3 space-y-2 border-l border-indigo-700/60 pl-3 lg:my-2" aria-label="Block comment thread">
      {comments.map(comment => <EditableComment key={comment.id} comment={comment} onSave={body => onUpdateComment(comment, body)}/>)}
    </div>}
  </div>
}

const PlanDocument = memo(function PlanDocument({ markdown, revisionID, comments, selectedBlock, onUpdateComment, onSelect, onDragStart, onDragEnter, onDragEnd }: {
  markdown: string
  revisionID: string
  comments: PlanComment[]
  selectedBlock: SelectedBlock | null
  onUpdateComment: (comment: PlanComment, body: string) => Promise<void>
  onSelect: (block: SelectedBlock) => void
  onDragStart: (block: SelectedBlock, event: MouseEvent) => void
  onDragEnter: (block: SelectedBlock) => void
  onDragEnd: () => void
}) {
  const currentComments = comments.filter(item => item.revision_id === revisionID)
  const anchoredComments = currentComments.filter(item => item.selection_start !== undefined && item.selection_end !== undefined)
  const generalComments = currentComments.filter(item => !item.selected_text)
  const blockComments = (block: SelectedBlock | null) => block ? anchoredComments.filter(item => item.selection_start === block.start) : []
  const coveringComments = (block: SelectedBlock | null) => block ? anchoredComments.filter(item => item.selection_start !== undefined && item.selection_end !== undefined && item.selection_start <= block.start && item.selection_end >= block.end) : []
  const block = (props: { node?: unknown }, kind: string, content: ReactNode) => {
    const anchor = sourceBlock(markdown, props.node as MarkdownNode | undefined, kind)
    const selected = !!anchor && !!selectedBlock && anchor.start >= selectedBlock.start && anchor.end <= selectedBlock.end
    return <BlockFrame block={anchor} selected={selected} comments={blockComments(anchor)} coveredComments={coveringComments(anchor)} onUpdateComment={onUpdateComment} onSelect={onSelect} onDragStart={onDragStart} onDragEnter={onDragEnter} onDragEnd={onDragEnd}>{content}</BlockFrame>
  }

  return <main data-plan-document className="overflow-y-auto p-3 md:p-6"><div className="mx-auto max-w-4xl">
    <article className="rounded border border-zinc-800 bg-zinc-950 p-5 leading-7">
      <ReactMarkdown remarkPlugins={[remarkGfm, remarkOutermostBlocks]} components={{
        h1: props => block(props, "heading", <h1 className="mb-5 mt-1 text-2xl font-bold">{props.children}</h1>),
        h2: props => block(props, "heading", <h2 className="mb-3 mt-6 text-xl font-semibold">{props.children}</h2>),
        h3: props => block(props, "heading", <h3 className="mb-2 mt-5 text-lg font-semibold">{props.children}</h3>),
        h4: props => block(props, "heading", <h4 className="mb-2 mt-4 font-semibold">{props.children}</h4>),
        h5: props => block(props, "heading", <h5 className="mb-2 mt-4 text-sm font-semibold">{props.children}</h5>),
        h6: props => block(props, "heading", <h6 className="mb-2 mt-4 text-xs font-semibold uppercase">{props.children}</h6>),
        p: props => block(props, "paragraph", <p className="my-3 text-zinc-300">{props.children}</p>),
        li: ({ children }) => <li className="ml-5 list-item">{children}</li>,
        blockquote: props => block(props, "blockquote", <blockquote className="my-3 border-l-2 border-zinc-700 pl-3 text-zinc-400">{props.children}</blockquote>),
        pre: props => block(props, "code block", <VisualMarkdownPre>{props.children}</VisualMarkdownPre>),
        tr: ({ children }) => <div role="row" className="grid grid-flow-col border-b border-zinc-800">{children}</div>,
        ul: props => block(props, "list", <ul className="my-3 list-disc">{props.children}</ul>),
        ol: props => block(props, "list", <ol className="my-3 list-decimal">{props.children}</ol>),
        table: props => block(props, "table", <div role="table" className="my-4 overflow-x-auto">{props.children}</div>),
        thead: ({ children }) => <div role="rowgroup" className="font-semibold">{children}</div>,
        tbody: ({ children }) => <div role="rowgroup">{children}</div>,
        th: ({ children }) => <div role="columnheader" className="p-2 text-left">{children}</div>,
        td: ({ children }) => <div role="cell" className="p-2">{children}</div>,
        code: ({ children }) => <code className="rounded bg-black/40 px-1 font-mono text-sm">{children}</code>,
      }}>{markdown}</ReactMarkdown>
    </article>
    {generalComments.length > 0 && <section role="region" aria-label="General feedback" className="mt-6 rounded border border-zinc-800 bg-zinc-950 p-5"><h2 className="mb-3 text-sm font-semibold uppercase tracking-wide text-zinc-400">General feedback</h2><div className="space-y-2">{generalComments.map(item => <EditableComment key={item.id} comment={item} onSave={body => onUpdateComment(item, body)}/>)}</div></section>}
  </div></main>
})

export function PlanPage() {
  const { planId = "" } = useParams()
  const id = decodeURIComponent(planId)
  const { baseUrl } = useServer()
  const { data, loading, error, refresh } = usePlan(id)
  const navigate = useNavigate()
  const commentInput = useRef<HTMLTextAreaElement>(null)
  const dragAnchor = useRef<SelectedBlock | null>(null)
  const suppressClick = useRef(false)
  const [selectedBlock, setSelectedBlock] = useState<SelectedBlock | null>(null)
  const [localComments, setLocalComments] = useState<PlanComment[]>([])
  const [comment, setComment] = useState("")
  const [feedbackOpen, setFeedbackOpen] = useState(false)
  const [question, setQuestion] = useState("")
  const [cleanupAfterImplementation, setCleanupAfterImplementation] = useState(false)
  const [busy, setBusy] = useState(false)
  const [actionError, setActionError] = useState<string | null>(null)
  const [notice, setNotice] = useState<{ message: string; variant: "success" | "warning" } | null>(null)

  useEffect(() => {
    if (data) setLocalComments(data.comments)
  }, [data])

  async function run(action: () => Promise<void>) {
    setBusy(true)
    setActionError(null)
    try {
      await action()
      await refresh()
    } catch (cause) {
      setActionError(cause instanceof Error ? cause.message : String(cause))
    } finally {
      setBusy(false)
    }
  }

  const selectBlock = useCallback((block: SelectedBlock) => {
    if (suppressClick.current) {
      suppressClick.current = false
      return
    }
    setSelectedBlock(block)
    requestAnimationFrame(() => commentInput.current?.focus())
  }, [])

  const selectRange = useCallback((first: SelectedBlock, last: SelectedBlock) => {
    const start = Math.min(first.start, last.start)
    const end = Math.max(first.end, last.end)
    const characterStart = Math.min(first.characterStart, last.characterStart)
    const characterEnd = Math.max(first.characterEnd, last.characterEnd)
    setSelectedBlock({ start, end, characterStart, characterEnd, text: data?.revision.markdown.slice(characterStart, characterEnd) ?? "", label: "Comment on selected blocks" })
  }, [data?.revision.markdown])

  const startDrag = useCallback((block: SelectedBlock, event: MouseEvent) => {
    if (event.button !== 0) return
    event.preventDefault()
    dragAnchor.current = block
    selectRange(block, block)
  }, [selectRange])

  const extendDrag = useCallback((block: SelectedBlock) => {
    if (!dragAnchor.current) return
    if (block.start !== dragAnchor.current.start) suppressClick.current = true
    selectRange(dragAnchor.current, block)
  }, [selectRange])

  const endDrag = useCallback(() => {
    if (!dragAnchor.current) return
    dragAnchor.current = null
    requestAnimationFrame(() => commentInput.current?.focus())
  }, [])

  async function submitComment() {
    if (!comment.trim()) return
    setBusy(true)
    setActionError(null)
    try {
      const created = await addPlanComment(baseUrl, id, {
        body: comment.trim(),
        ...(selectedBlock ? {
          selection_start: selectedBlock.start,
          selection_end: selectedBlock.end,
          selected_text: selectedBlock.text,
        } : {}),
      })
      setLocalComments(current => [...current, created])
      setComment("")
      setSelectedBlock(null)
      await refresh()
    } catch (cause) {
      setActionError(cause instanceof Error ? cause.message : String(cause))
    } finally {
      setBusy(false)
    }
  }

  const editComment = useCallback(async (existing: PlanComment, body: string) => {
    const updated = await updatePlanComment(baseUrl, id, existing.id, body)
    setLocalComments(current => current.map(item => item.id === updated.id ? updated : item))
    await refresh()
  }, [baseUrl, id, refresh])

  async function verdict(action: "request-changes" | "approve" | "reject" | "reopen") {
    await run(async () => {
      await planAction(baseUrl, id, action, action === "approve" ? { cleanup_after_implementation: cleanupAfterImplementation } : undefined)
      if (action !== "reopen") navigate("/plans")
    })
  }

  async function ask() {
    if (!question.trim()) return
    await run(async () => {
      const result = await askPlanQuestion(baseUrl, id, question.trim())
      setQuestion("")
      setNotice(result.warning
        ? { message: result.warning, variant: "warning" }
        : { message: "Question sent", variant: "success" })
    })
  }

  if (loading && !data) return <div className="p-6 text-zinc-500">Loading plan…</div>
  if (error && !data) return <div className="p-6 text-red-300">{error}</div>
  if (!data) return null

  const unanswered = data.questions.filter(item => item.required && !data.responses.some(response => response.question_id === item.id)).length
  const priorRevisionComments = localComments.filter(item => item.revision_id !== data.revision.id && item.selected_text)

  return <div className="flex h-full min-h-0 flex-col bg-[#0a0a0f] text-zinc-200">
    <header data-plan-header className="flex flex-wrap items-center justify-between gap-2 border-b border-zinc-800 px-3 py-2 md:px-5 md:py-3">
      <div><Link to="/plans" className="flex items-center gap-1 text-xs text-zinc-500"><ArrowLeft size={13}/> Plan queue</Link><h1 className="mt-1 text-lg font-semibold">{data.plan.title}</h1><div className="text-xs text-zinc-500">revision {data.plan.latest_revision} · {data.plan.status.replace("_", " ")}</div></div>
      <div data-plan-actions className="flex w-full flex-wrap items-center gap-2 md:w-auto md:flex-nowrap"><Button variant="outline" size="sm" onClick={() => setFeedbackOpen(true)} className="min-h-11 lg:hidden"><PanelRight size={15}/> Feedback</Button><GoToAgentButton taskId={data.plan.task_id} />{data.plan.status === "proposed" && <><label className="mr-2 hidden max-w-52 items-center gap-2 text-xs text-zinc-400 md:flex" title="Remove the editable plan bundle after successful implementation; collaboration history is retained"><input type="checkbox" checked={cleanupAfterImplementation} disabled={busy} onChange={event => setCleanupAfterImplementation(event.target.checked)} className="accent-indigo-500"/>Clean up plan files after implementation</label><Button variant="destructive" size="sm" disabled={busy} onClick={() => void verdict("reject")} className="hidden sm:inline-flex">Reject</Button><Button variant="warning" size="sm" disabled={busy} onClick={() => void verdict("request-changes")} className="min-h-11 md:min-h-9">Request changes</Button><Button variant="success" size="sm" title={unanswered ? "Answer required choices first" : "Approve plan"} disabled={busy || unanswered > 0} onClick={() => void verdict("approve")} className="min-h-11 md:min-h-9">Approve</Button></>}{data.plan.status === "rejected" && <Button size="sm" onClick={() => void verdict("reopen")}>Reopen</Button>}</div>
    </header>
    {actionError && <InlineNotice variant="error" className="rounded-none border-x-0 border-t-0 px-5 py-2 text-xs">{actionError}</InlineNotice>}
    {notice && <InlineNotice variant={notice.variant} className="rounded-none border-x-0 border-t-0 px-5 py-2 text-xs">{notice.message}</InlineNotice>}
    <div className="grid min-h-0 flex-1 grid-cols-1 lg:grid-cols-[minmax(0,1fr)_25rem]">
      <PlanDocument markdown={data.revision.markdown} revisionID={data.revision.id} comments={localComments} selectedBlock={selectedBlock} onUpdateComment={editComment} onSelect={selectBlock} onDragStart={startDrag} onDragEnter={extendDrag} onDragEnd={endDrag}/>
      <aside className={`${feedbackOpen ? "fixed inset-2 z-50 flex shadow-2xl" : "hidden"} min-h-0 flex-col overflow-y-auto rounded-lg border border-zinc-800 bg-zinc-950 p-4 lg:static lg:flex lg:rounded-none lg:border-y-0 lg:border-r-0`}>
        <div className="-mx-4 -mt-4 mb-4 flex min-h-14 shrink-0 items-center justify-between border-b border-zinc-800 px-4 lg:hidden">
          <h2 className="text-sm font-semibold text-zinc-200">Plan feedback</h2>
          <button aria-label="Close feedback" onClick={() => setFeedbackOpen(false)} className="flex h-11 w-11 items-center justify-center rounded text-zinc-400 hover:bg-zinc-900 hover:text-zinc-200"><X size={18}/></button>
        </div>
        {data.questions.length > 0 && <><h2 className="text-xs font-semibold uppercase text-zinc-500">Choices</h2><div className="mt-3 space-y-4">{data.questions.map(item => <PlanQuestionControl key={item.id} question={item} response={data.responses.find(response => response.question_id === item.id)} disabled={busy} onRespond={body => run(() => respondToPlanQuestion(baseUrl, id, item.key, body))}/>)}</div></>}
        <div className="mt-4 border-t border-zinc-800 pt-4"><h2 className="text-xs font-semibold uppercase text-zinc-500">{selectedBlock ? (selectedBlock.label === "Comment on selected blocks" ? "Comment on selected blocks" : "Comment on selected block") : "General comment"}</h2>
          {selectedBlock && <div className="mt-2 rounded bg-indigo-950/40 p-2 text-xs text-indigo-200"><span className="line-clamp-3">{selectedBlock.text}</span><button aria-label="Clear selected block" onClick={() => setSelectedBlock(null)} className="mt-1 flex items-center gap-1 text-zinc-400"><X size={12}/> Clear</button></div>}
          <textarea ref={commentInput} aria-label={selectedBlock ? (selectedBlock.label === "Comment on selected blocks" ? "Comment on selected blocks" : "Comment on selected block") : "General plan comment"} value={comment} onChange={event => setComment(event.target.value)} onKeyDown={event => keyboardSubmit(event, () => void submitComment(), busy || !comment.trim())} placeholder={selectedBlock ? "Comment on this block…" : "Leave general feedback…"} className="mt-2 w-full rounded border border-zinc-700 bg-zinc-900 p-2 text-sm"/>
          <Button size="sm" disabled={busy || !comment.trim()} onClick={() => void submitComment()} className="mt-2">{busy ? "Saving…" : "Add draft comment"}</Button>
          {priorRevisionComments.length > 0 && <details className="mt-4 text-sm text-zinc-400"><summary className="cursor-pointer text-xs font-semibold uppercase text-zinc-500">Prior revision feedback ({priorRevisionComments.length})</summary><div className="mt-2 space-y-2">{priorRevisionComments.map(item => <div key={item.id} className="rounded border border-zinc-800 p-2"><blockquote className="line-clamp-2 border-l border-zinc-700 pl-2 text-xs text-zinc-500">{item.selected_text}</blockquote><p className="mt-1">{item.body}</p></div>)}</div></details>}
        </div>
        <div className="mt-6 border-t border-zinc-800 pt-4"><h2 className="text-xs font-semibold uppercase text-zinc-500">Questions & answers</h2><div className="my-3 space-y-2">{data.messages.map(message => <div key={message.id} className={`rounded p-2 ${message.author === "user" ? "bg-indigo-950/40" : "bg-zinc-900"}`}><ReviewMessageBody body={message.body}/></div>)}</div><div className="flex gap-2"><textarea aria-label="Question for plan agent" value={question} onChange={event => setQuestion(event.target.value)} onKeyDown={event => keyboardSubmit(event, () => void ask(), busy || !question.trim())} className="min-w-0 flex-1 rounded border border-zinc-700 bg-zinc-900 p-2 text-sm" placeholder="Ask the agent…"/><Button variant="secondary" size="icon" aria-label="Ask plan agent" disabled={busy || !question.trim()} onClick={() => void ask()}><Send size={15}/></Button></div></div>
      </aside>
    </div>
  </div>
}

function PlanQuestionControl({ question, response, disabled, onRespond }: { question: PlanQuestion; response?: { values_json: string; text: string }; disabled: boolean; onRespond: (body: { values?: string[]; text?: string }) => Promise<void> }) {
  const options = JSON.parse(question.options_json || "[]") as PlanOption[]
  const selected = response ? JSON.parse(response.values_json || "[]") as string[] : []
  const [text, setText] = useState(response?.text ?? "")
  if (question.kind === "free_text") return <div><label className="text-sm text-zinc-200">{question.prompt}{question.required && <span className="text-amber-400"> *</span>}<textarea value={text} onChange={event => setText(event.target.value)} onKeyDown={event => keyboardSubmit(event, () => void onRespond({ text }), disabled || !text.trim())} className="mt-2 w-full rounded border border-zinc-700 bg-zinc-900 p-2 text-sm"/></label><button disabled={disabled || !text.trim()} onClick={() => void onRespond({ text })} className="mt-1 text-xs text-indigo-300">Save answer</button></div>
  return <fieldset><legend className="text-sm">{question.prompt}{question.required && <span className="text-amber-400"> *</span>}</legend><div className="mt-2 space-y-1">{options.map(option => <label key={option.id} className="flex gap-2 text-sm text-zinc-400"><input type={question.kind === "single_choice" ? "radio" : "checkbox"} name={question.id} checked={selected.includes(option.id)} disabled={disabled} onChange={() => void onRespond({ values: question.kind === "single_choice" ? [option.id] : selected.includes(option.id) ? selected.filter(value => value !== option.id) : [...selected, option.id] })}/><span>{option.label}{option.description && <small className="block text-zinc-600">{option.description}</small>}</span></label>)}</div></fieldset>
}
