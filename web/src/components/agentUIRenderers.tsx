import type { ReactNode } from "react"
import type { UIComponent } from "../lib/agentUi"
import { MarkdownBody } from "./MarkdownBody"
import { Picture, Stat, type CardContext } from "./AgentUIParts"
import { HEADING, TONE_BADGE, TONE_TEXT, tone } from "../lib/agentUiStyle"

export type { CardContext }

type Renderer = (component: UIComponent, at: number, card: CardContext) => ReactNode

function heading(component: UIComponent) {
  const { Tag, className } = HEADING[component.level ?? 2] ?? HEADING[2]
  return <Tag className={className}>{component.text}</Tag>
}

function text(component: UIComponent) {
  const colour = TONE_TEXT[tone(component.tone)]
  return (
    <div className={colour === TONE_TEXT.neutral ? "text-zinc-300" : colour}>
      <MarkdownBody body={component.text ?? ""} disableImages skipHtml />
    </div>
  )
}

function stats(component: UIComponent) {
  return (
    <div className="grid gap-2" style={{ gridTemplateColumns: "repeat(auto-fit, minmax(7.5rem, 1fr))" }}>
      {(component.items ?? []).map((item, index) => <Stat key={index} item={item} />)}
    </div>
  )
}

function keyValue(component: UIComponent) {
  return (
    <dl className="grid grid-cols-[minmax(0,auto)_minmax(0,1fr)] gap-x-4 gap-y-1 text-sm">
      {(component.items ?? []).map((item, index) => (
        <div key={index} className="contents">
          <dt className="text-zinc-500">{item.label}</dt>
          <dd className="break-words text-zinc-200">{item.value}</dd>
        </div>
      ))}
    </dl>
  )
}

function table(component: UIComponent) {
  return (
    <div className="overflow-x-auto">
      <table className="w-full border-collapse text-left text-xs">
        <thead>
          <tr>{(component.columns ?? []).map((column, index) => <th key={index} scope="col" className="border-b border-zinc-700 px-2 py-1.5 font-medium text-zinc-400">{column}</th>)}</tr>
        </thead>
        <tbody>
          {(component.rows ?? []).map((row, rowIndex) => (
            <tr key={rowIndex} className="border-b border-zinc-800 last:border-0">
              {row.map((cell, cellIndex) => <td key={cellIndex} className="px-2 py-1.5 align-top text-zinc-200">{cell}</td>)}
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  )
}

function list(component: UIComponent) {
  const List = component.ordered ? "ol" : "ul"
  return (
    <List className={`space-y-1 pl-5 text-sm text-zinc-300 ${component.ordered ? "list-decimal" : "list-disc"}`}>
      {(component.items ?? []).map((item, index) => <li key={index}>{item.text}</li>)}
    </List>
  )
}

function badges(component: UIComponent) {
  return (
    <div className="flex flex-wrap gap-1.5">
      {(component.items ?? []).map((item, index) => <span key={index} className={`rounded-full border px-2 py-0.5 text-xs ${TONE_BADGE[tone(item.tone)]}`}>{item.text}</span>)}
    </div>
  )
}

function progress(component: UIComponent) {
  const percent = Math.round(Math.min(1, Math.max(0, component.fraction ?? 0)) * 100)
  return (
    <div>
      <div className="mb-1 flex items-baseline justify-between gap-2 text-xs">
        <span className="text-zinc-300">{component.label}</span>
        <span className="text-zinc-500">{percent}%</span>
      </div>
      <div role="progressbar" aria-label={component.label || "Progress"} aria-valuemin={0} aria-valuemax={100} aria-valuenow={percent} className="h-2 overflow-hidden rounded-full bg-zinc-800">
        <div className="h-full rounded-full bg-indigo-500" style={{ width: `${percent}%` }} />
      </div>
      {component.detail && <div className="mt-1 text-xs text-zinc-500">{component.detail}</div>}
    </div>
  )
}

// A bar's width, against the largest bar of its chart.
function barWidth(number: number | undefined, largest: number): number {
  return largest > 0 ? Math.round(((number ?? 0) / largest) * 100) : 0
}

function barChart(component: UIComponent) {
  const items = component.items ?? []
  const largest = Math.max(0, ...items.map((item) => item.number ?? 0))
  return (
    <div>
      {component.label && <div className="mb-1.5 text-xs text-zinc-400">{component.label}</div>}
      <ul aria-label={component.label || "Bar chart"} className="space-y-1">
        {items.map((item, index) => (
          <li key={index} className="grid grid-cols-[minmax(3rem,25%)_minmax(0,1fr)_auto] items-center gap-2 text-xs">
            <span className="truncate text-zinc-400">{item.label}</span>
            <span aria-hidden="true" className="h-2.5 overflow-hidden rounded-sm bg-zinc-800">
              <span data-bar className="block h-full rounded-sm bg-indigo-500" style={{ width: `${barWidth(item.number, largest)}%` }} />
            </span>
            <span className="tabular-nums text-zinc-200">{item.number ?? 0}</span>
          </li>
        ))}
      </ul>
    </div>
  )
}

function code(component: UIComponent) {
  return (
    <pre className="max-w-full overflow-x-auto rounded border border-zinc-700 bg-[var(--theme-canvas,rgba(0,0,0,0.4))] p-2 font-mono text-xs leading-5 text-zinc-200">
      <code>{component.text}</code>
    </pre>
  )
}

function image(component: UIComponent, at: number, card: CardContext) {
  return <Picture entry={component} at={card.picture(`${at}`)} card={card} thumbnail={false} />
}

function imageGrid(component: UIComponent, at: number, card: CardContext) {
  return (
    <ul aria-label="Images" className="grid list-none gap-2 p-0" style={{ gridTemplateColumns: "repeat(auto-fill, minmax(7rem, 1fr))" }}>
      {(component.items ?? []).map((item, index) => (
        <li key={index} className="min-w-0">
          <Picture entry={item} at={card.picture(`${at}:${index}`)} card={card} thumbnail />
        </li>
      ))}
    </ul>
  )
}

function actions(component: UIComponent, _at: number, { onProposeMessage }: CardContext) {
  if (!onProposeMessage) return null
  return (
    <div className="flex flex-wrap gap-2">
      {(component.items ?? []).map((item, index) => (
        <button
          key={index}
          type="button"
          title="Puts this in your message box"
          onClick={() => item.message && onProposeMessage(item.message)}
          className="min-h-9 rounded border border-zinc-600 bg-zinc-800 px-3 py-1.5 text-xs font-medium text-zinc-200 transition-colors hover:border-zinc-500 hover:bg-zinc-700"
        >
          {item.label}
        </button>
      ))}
    </div>
  )
}

// One renderer per component type of Legato's fixed list.
const RENDERERS: Record<string, Renderer> = {
  heading,
  text,
  stat: (component) => <Stat item={component} />,
  stats,
  key_value: keyValue,
  table,
  list,
  badges,
  progress,
  bar_chart: barChart,
  code,
  image,
  image_grid: imageGrid,
  divider: () => <hr className="border-zinc-800" />,
  actions,
}

// renderComponent draws one component of a card, or null for one it leaves
// out. A newer server may know components this build does not.
export function renderComponent(component: UIComponent, at: number, card: CardContext): ReactNode {
  const render = Object.hasOwn(RENDERERS, component.type) ? RENDERERS[component.type] : undefined
  return render ? render(component, at, card) : null
}
