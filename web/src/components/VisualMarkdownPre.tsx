import { isValidElement, type ReactNode } from "react"
import { MermaidMarkdownPre } from "./MermaidDiagram"
import { WiremarkDiagram } from "./WiremarkDiagram"

function wiremarkCodeSource(children: ReactNode) {
  if (!isValidElement<{ className?: string; children?: ReactNode }>(children)) return null
  if (!/^language-wireframe(?:\s|$)/i.test(children.props.className ?? "")) return null
  const source = children.props.children
  return typeof source === "string" ? source : null
}

export function VisualMarkdownPre({ children, fallbackClassName }: { children?: ReactNode; fallbackClassName?: string }) {
  const source = wiremarkCodeSource(children)
  return source === null
    ? <MermaidMarkdownPre fallbackClassName={fallbackClassName}>{children}</MermaidMarkdownPre>
    : <WiremarkDiagram source={source}/>
}
