import { parseUIComponents, type AgentUIItem } from "../lib/agentUi"
import { AgentUIComponents } from "./AgentUIComponents"
import { MarkdownBody } from "./MarkdownBody"
import { SandboxedHtml } from "./SandboxedHtml"

interface AgentUIItemViewProps {
  item: AgentUIItem
  // fill: take the container's height (the side panel). Otherwise the view
  // is as tall as its content, up to a limit (a card in the conversation).
  fill?: boolean
  onOverflow?: (overflowing: boolean) => void
  onProposeMessage?: (text: string) => void
}

// AgentUIItemView draws one item an agent showed, by its format: HTML in a
// sandboxed frame, Markdown and component cards with Legato's own elements.
export function AgentUIItemView({ item, fill = false, onOverflow, onProposeMessage }: AgentUIItemViewProps) {
  const title = item.title || (item.placement === "panel" ? "Artifact from the agent" : "Card from the agent")
  if (item.format === "html") {
    return (
      <SandboxedHtml
        html={item.content ?? ""}
        title={title}
        resourceDomains={item.resource_domains ?? []}
        fill={fill}
        onOverflow={onOverflow}
        onProposeMessage={onProposeMessage}
      />
    )
  }
  const body = item.format === "markdown"
    ? <MarkdownBody body={item.content ?? ""} renderMermaid />
    : <AgentUIComponents components={parseUIComponents(item.content)} onProposeMessage={onProposeMessage} />
  return fill
    ? <div className="min-h-0 flex-1 overflow-y-auto p-4 text-zinc-200">{body}</div>
    : <div className="p-3 text-zinc-200">{body}</div>
}
