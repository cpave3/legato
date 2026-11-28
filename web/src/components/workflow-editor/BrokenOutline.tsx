import type { ReactNode } from "react"
import { Button } from "../ui/Button"

const NOT_PARSED = "the YAML does not parse"

// fileProblem says why a file can't be drawn as forms.
function fileProblem(file: string): string {
  if (file.startsWith(NOT_PARSED)) return `The YAML doesn't parse, so it can't be shown here: ${file.replace(/^the YAML does not parse: /, "")}`
  return `This file can't be shown as forms: ${file}.`
}

type Props = { file: string; canvas: boolean; notices?: ReactNode; cards?: ReactNode; onEditYaml: () => void }

/** BrokenOutline is the outline of a file the forms can't show at all: why,
 * and its YAML. On the canvas the page's notices and cards come through
 * here: they stay, with the alert, when there is no outline to draw. */
export function BrokenOutline({ file, canvas, notices, cards, onEditYaml }: Props) {
  const alert = <div role="alert" className="flex flex-col items-start gap-2 rounded-xl border border-lw-danger-line bg-lw-error-soft p-4 text-sm text-lw-danger">
    <p>{fileProblem(file)}</p>
    <Button size="sm" variant="outline" onClick={() => onEditYaml()}>Edit as YAML</Button>
  </div>
  if (!canvas) return alert
  return <div className="flex min-h-0 flex-1 flex-col gap-3 overflow-y-auto p-4">
    {notices && <div className="flex max-w-xl flex-col gap-2">{notices}</div>}
    {alert}
    {cards && <div className="max-w-sm">{cards}</div>}
  </div>
}
