import { useEffect, useRef, useState, type KeyboardEvent, type ReactNode } from "react"
import ReactMarkdown from "react-markdown"
import remarkGfm from "remark-gfm"
import { ArrowLeft, MessageSquarePlus, Send, X } from "lucide-react"
import { Link, useNavigate, useParams } from "react-router-dom"
import { usePlan } from "../hooks/usePlan"
import { useServer } from "../hooks/useServer"
import { addPlanComment, askPlanQuestion, planAction, respondToPlanQuestion, type PlanComment, type PlanOption, type PlanQuestion } from "../lib/plan"
import { ReviewMessageBody } from "../components/ReviewMessageBody"

type SourcePosition = { start?: { offset?: number }; end?: { offset?: number } }
type SelectedBlock = { start: number; end: number; text: string; label: string }

type BlockProps = {
  node?: { position?: SourcePosition }
  children?: ReactNode
}

function keyboardSubmit(event: KeyboardEvent<HTMLTextAreaElement>, submit: () => void, disabled: boolean) {
  if (event.key !== "Enter" || (!event.metaKey && !event.ctrlKey) || disabled) return
  event.preventDefault()
  submit()
}

function byteOffset(source: string, characterOffset: number) {
  return new TextEncoder().encode(source.slice(0, characterOffset)).length
}

function sourceBlock(markdown: string, position?: SourcePosition, kind = "block"): SelectedBlock | null {
  const start = position?.start?.offset
  const end = position?.end?.offset
  if (start === undefined || end === undefined || end <= start) return null
  const text = markdown.slice(start, end)
  const excerpt = text.replace(/^#{1,6}\s+/, "").replace(/^[-*>+\d.)\s]+/, "").replace(/\s+/g, " ").trim().slice(0, 72)
  return { start: byteOffset(markdown, start), end: byteOffset(markdown, end), text, label: `Comment on ${kind} beginning ${excerpt}` }
}

function commentState(comments: PlanComment[]) {
  if (comments.some(comment => comment.submitted_at)) return "submitted"
  if (comments.length > 0) return "draft"
  return undefined
}

function BlockFrame({ block, selected, comments, onSelect, children }: {
  block: SelectedBlock | null
  selected: boolean
  comments: PlanComment[]
  onSelect: (block: SelectedBlock) => void
  children: ReactNode
}) {
  if (!block) return <>{children}</>
  const state = commentState(comments)
  return <div id={`plan-block-${block.start}`} className={`group relative my-1 rounded border-l-2 pl-7 pr-2 transition-colors ${comments.length > 0 ? "lg:grid lg:grid-cols-[minmax(0,1fr)_18rem] lg:gap-4" : ""} ${selected ? "border-indigo-400 bg-indigo-950/35" : state ? "border-amber-500/70 bg-amber-950/15" : "border-transparent hover:border-zinc-700 hover:bg-zinc-900/40"}`}>
    <button
      type="button"
      aria-label={block.label}
      aria-pressed={selected}
      data-commented={state}
      onClick={() => onSelect(block)}
      className="absolute left-1 top-2 rounded p-1 text-zinc-600 opacity-60 hover:bg-zinc-800 hover:text-indigo-300 focus:opacity-100 group-hover:opacity-100"
    ><MessageSquarePlus size={14}/></button>
    <div>{children}</div>
    {comments.length > 0 && <div className="mb-3 space-y-2 border-l border-indigo-700/60 pl-3 lg:my-2" aria-label="Block comment thread">
      {comments.map(comment => <button type="button" key={comment.id} onClick={() => onSelect(block)} className="block w-full rounded bg-indigo-950/35 p-2 text-left text-sm text-zinc-200 hover:bg-indigo-950/60 focus:ring-1 focus:ring-indigo-400">
        <span className="block">{comment.body}</span>
        <span className="mt-1 block text-[10px] font-semibold uppercase tracking-wide text-indigo-300">{comment.submitted_at ? "Submitted" : "Draft"}</span>
      </button>)}
    </div>}
  </div>
}

export function PlanPage() {
  const { planId = "" } = useParams()
  const id = decodeURIComponent(planId)
  const { baseUrl } = useServer()
  const { data, loading, error, refresh } = usePlan(id)
  const navigate = useNavigate()
  const commentInput = useRef<HTMLTextAreaElement>(null)
  const [selectedBlock, setSelectedBlock] = useState<SelectedBlock | null>(null)
  const [localComments, setLocalComments] = useState<PlanComment[]>([])
  const [comment, setComment] = useState("")
  const [question, setQuestion] = useState("")
  const [busy, setBusy] = useState(false)
  const [actionError, setActionError] = useState<string | null>(null)
  const [info, setInfo] = useState<string | null>(null)

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

  function selectBlock(block: SelectedBlock) {
    setSelectedBlock(block)
    requestAnimationFrame(() => commentInput.current?.focus())
  }

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

  async function verdict(action: "request-changes" | "approve" | "reject" | "reopen") {
    await run(async () => {
      await planAction(baseUrl, id, action)
      if (action !== "reopen") navigate("/plans")
    })
  }

  async function ask() {
    if (!question.trim()) return
    await run(async () => {
      const result = await askPlanQuestion(baseUrl, id, question.trim())
      setQuestion("")
      setInfo(result.warning ?? "Question sent")
    })
  }

  if (loading && !data) return <div className="p-6 text-zinc-500">Loading plan…</div>
  if (error && !data) return <div className="p-6 text-red-300">{error}</div>
  if (!data) return null

  const unanswered = data.questions.filter(item => item.required && !data.responses.some(response => response.question_id === item.id)).length
  const currentAnchored = localComments.filter(item => item.revision_id === data.revision.id && item.selection_start !== undefined && item.selection_end !== undefined)
  const generalComments = localComments.filter(item => !item.selected_text)
  const priorRevisionComments = localComments.filter(item => item.revision_id !== data.revision.id && item.selected_text)
  const blockComments = (block: SelectedBlock | null) => block ? currentAnchored.filter(item => item.selection_start === block.start && item.selection_end === block.end) : []
  const block = (props: BlockProps, kind: string, content: ReactNode) => {
    const anchor = sourceBlock(data.revision.markdown, props.node?.position, kind)
    return <BlockFrame block={anchor} selected={!!anchor && selectedBlock?.start === anchor.start && selectedBlock?.end === anchor.end} comments={blockComments(anchor)} onSelect={selectBlock}>{content}</BlockFrame>
  }

  return <div className="flex h-full min-h-0 flex-col bg-[#0a0a0f] text-zinc-200">
    <header className="flex items-center justify-between border-b border-zinc-800 px-5 py-3">
      <div><Link to="/plans" className="flex items-center gap-1 text-xs text-zinc-500"><ArrowLeft size={13}/> Plan queue</Link><h1 className="mt-1 text-lg font-semibold">{data.plan.title}</h1><div className="text-xs text-zinc-500">revision {data.plan.latest_revision} · {data.plan.status.replace("_", " ")}</div></div>
      <div className="flex gap-2">{data.plan.status === "proposed" && <><button disabled={busy} onClick={() => void verdict("reject")} className="rounded border border-red-900 px-3 py-1.5 text-xs text-red-300">Reject</button><button disabled={busy} onClick={() => void verdict("request-changes")} className="rounded border border-amber-800 px-3 py-1.5 text-xs text-amber-300">Request changes</button><button title={unanswered ? "Answer required choices first" : "Approve plan"} disabled={busy || unanswered > 0} onClick={() => void verdict("approve")} className="rounded bg-emerald-700 px-3 py-1.5 text-xs text-white disabled:opacity-40">Approve plan</button></>}{data.plan.status === "rejected" && <button onClick={() => void verdict("reopen")} className="rounded bg-indigo-600 px-3 py-1.5 text-xs">Reopen</button>}</div>
    </header>
    {actionError && <div role="alert" className="border-b border-red-900 bg-red-950/40 px-5 py-2 text-xs text-red-300">{actionError}</div>}{info && <div className="border-b border-zinc-800 px-5 py-2 text-xs text-zinc-400">{info}</div>}
    <div className="grid min-h-0 flex-1 grid-cols-1 lg:grid-cols-[minmax(0,1fr)_25rem]">
      <main className="overflow-y-auto p-6"><div className="mx-auto max-w-4xl">
        <article className="rounded border border-zinc-800 bg-zinc-950 p-5 leading-7">
          <ReactMarkdown remarkPlugins={[remarkGfm]} components={{
            h1: props => block(props, "heading", <h1 className="mb-5 mt-1 text-2xl font-bold">{props.children}</h1>),
            h2: props => block(props, "heading", <h2 className="mb-3 mt-6 text-xl font-semibold">{props.children}</h2>),
            h3: props => block(props, "heading", <h3 className="mb-2 mt-5 text-lg font-semibold">{props.children}</h3>),
            h4: props => block(props, "heading", <h4 className="mb-2 mt-4 font-semibold">{props.children}</h4>),
            h5: props => block(props, "heading", <h5 className="mb-2 mt-4 text-sm font-semibold">{props.children}</h5>),
            h6: props => block(props, "heading", <h6 className="mb-2 mt-4 text-xs font-semibold uppercase">{props.children}</h6>),
            p: props => block(props, "paragraph", <p className="my-3 text-zinc-300">{props.children}</p>),
            li: props => block(props, "list item", <li className="ml-5 list-item">{props.children}</li>),
            blockquote: props => block(props, "blockquote", <blockquote className="my-3 border-l-2 border-zinc-700 pl-3 text-zinc-400">{props.children}</blockquote>),
            pre: props => block(props, "code block", <pre className="my-3 overflow-x-auto rounded bg-black/40 p-3">{props.children}</pre>),
            tr: props => block(props, "table row", <div role="row" className="grid grid-flow-col border-b border-zinc-800">{props.children}</div>),
            ul: ({ children }) => <ul className="my-3 list-disc">{children}</ul>,
            ol: ({ children }) => <ol className="my-3 list-decimal">{children}</ol>,
            table: ({ children }) => <div role="table" className="my-4 overflow-x-auto">{children}</div>,
            thead: ({ children }) => <div role="rowgroup" className="font-semibold">{children}</div>,
            tbody: ({ children }) => <div role="rowgroup">{children}</div>,
            th: ({ children }) => <div role="columnheader" className="p-2 text-left">{children}</div>,
            td: ({ children }) => <div role="cell" className="p-2">{children}</div>,
            code: ({ children }) => <code className="rounded bg-black/40 px-1 font-mono text-sm">{children}</code>,
          }}>{data.revision.markdown}</ReactMarkdown>
        </article>
      </div></main>
      <aside className="flex min-h-0 flex-col overflow-y-auto border-l border-zinc-800 bg-zinc-950 p-4">
        {data.questions.length > 0 && <><h2 className="text-xs font-semibold uppercase text-zinc-500">Choices</h2><div className="mt-3 space-y-4">{data.questions.map(item => <PlanQuestionControl key={item.id} question={item} response={data.responses.find(response => response.question_id === item.id)} disabled={busy} onRespond={body => run(() => respondToPlanQuestion(baseUrl, id, item.key, body))}/>)}</div></>}
        <div className="mt-4 border-t border-zinc-800 pt-4"><h2 className="text-xs font-semibold uppercase text-zinc-500">{selectedBlock ? "Comment on selected block" : "General comment"}</h2>
          {selectedBlock && <div className="mt-2 rounded bg-indigo-950/40 p-2 text-xs text-indigo-200"><span className="line-clamp-3">{selectedBlock.text}</span><button aria-label="Clear selected block" onClick={() => setSelectedBlock(null)} className="mt-1 flex items-center gap-1 text-zinc-400"><X size={12}/> Clear</button></div>}
          <textarea ref={commentInput} aria-label={selectedBlock ? "Comment on selected block" : "General plan comment"} value={comment} onChange={event => setComment(event.target.value)} onKeyDown={event => keyboardSubmit(event, () => void submitComment(), busy || !comment.trim())} placeholder={selectedBlock ? "Comment on this block…" : "Leave general feedback…"} className="mt-2 w-full rounded border border-zinc-700 bg-zinc-900 p-2 text-sm"/>
          <button disabled={busy || !comment.trim()} onClick={() => void submitComment()} className="mt-2 rounded bg-indigo-600 px-3 py-1.5 text-xs disabled:opacity-40">{busy ? "Saving…" : "Add draft comment"}</button>
          {generalComments.length > 0 && <div className="mt-3 space-y-2" aria-label="General comments">{generalComments.map(item => <div key={item.id} className="rounded border border-zinc-800 bg-zinc-900 p-2 text-sm"><p>{item.body}</p><span className="text-[10px] font-semibold uppercase text-zinc-500">{item.submitted_at ? "Submitted" : "Draft"}</span></div>)}</div>}
          {priorRevisionComments.length > 0 && <details className="mt-4 text-sm text-zinc-400"><summary className="cursor-pointer text-xs font-semibold uppercase text-zinc-500">Prior revision feedback ({priorRevisionComments.length})</summary><div className="mt-2 space-y-2">{priorRevisionComments.map(item => <div key={item.id} className="rounded border border-zinc-800 p-2"><blockquote className="line-clamp-2 border-l border-zinc-700 pl-2 text-xs text-zinc-500">{item.selected_text}</blockquote><p className="mt-1">{item.body}</p></div>)}</div></details>}
        </div>
        <div className="mt-6 border-t border-zinc-800 pt-4"><h2 className="text-xs font-semibold uppercase text-zinc-500">Questions & answers</h2><div className="my-3 space-y-2">{data.messages.map(message => <div key={message.id} className={`rounded p-2 ${message.author === "user" ? "bg-indigo-950/40" : "bg-zinc-900"}`}><ReviewMessageBody body={message.body}/></div>)}</div><div className="flex gap-2"><textarea aria-label="Question for plan agent" value={question} onChange={event => setQuestion(event.target.value)} onKeyDown={event => keyboardSubmit(event, () => void ask(), busy || !question.trim())} className="min-w-0 flex-1 rounded border border-zinc-700 bg-zinc-900 p-2 text-sm" placeholder="Ask the agent…"/><button aria-label="Ask plan agent" disabled={busy || !question.trim()} onClick={() => void ask()} className="rounded bg-zinc-800 p-2"><Send size={15}/></button></div></div>
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
