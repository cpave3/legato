import { useState } from "react"
import type { PromptAction, PromptState } from "../lib/agentWire"

interface NativeTerminalPromptProps {
  prompt: PromptState
  // Whose terminal is asking, as the heading names it: "Codex".
  askedBy: string
  // Answers with the keys an option stands for, sent to the tool's terminal,
  // and for a row that takes text, the text typed on it.
  onChoose: (keys: string, text?: string) => void
  // An answer is on its way: the card waits to see what the screen does.
  pending?: boolean
  // Why the last answer did not land.
  notice?: string
  // Takes the person to the terminal itself, for a prompt this cannot show.
  onOpenTerminal?: () => void
}

// Leaving a prompt is set apart from choosing one of its options.
const isWayOut = (keys: string) => keys === "Escape"

const choiceClass = "max-w-full break-words rounded bg-[var(--theme-selection,color-mix(in_oklab,var(--color-amber-700)_50%,transparent))] px-2 py-2 text-left text-xs text-[var(--theme-text,white)] hover:bg-[var(--theme-selection,color-mix(in_oklab,var(--color-amber-600)_50%,transparent))] disabled:opacity-50 sm:py-1"
const wayOutClass = "rounded px-2 py-2 text-left text-xs text-zinc-300 hover:bg-zinc-800 disabled:opacity-50 sm:py-1"

// A prompt a tool is showing in its terminal that never reaches Legato as a
// request of its own: an update notice, a trust question, a picker, a
// question with a row to type on. It is read off the screen, so it is shown
// as the terminal words it, and answered by pressing the keys the option
// takes. It knows nothing of the tool beyond its name.
export function NativeTerminalPrompt({ prompt, askedBy, onChoose, pending = false, notice, onOpenTerminal }: NativeTerminalPromptProps) {
  const actions = prompt.actions ?? []
  return (
    <section aria-label={`${askedBy} is asking in its terminal`} className="mx-4 my-2 min-w-0 rounded-lg border border-amber-600/40 bg-amber-900/10 p-3">
      <div className="flex min-w-0 flex-wrap items-center gap-2">
        <div className="text-sm font-semibold text-amber-200">{askedBy} is asking in its terminal</div>
        {pending && <div className="text-xs text-zinc-400">Answering…</div>}
        {onOpenTerminal && (
          <button className="ml-auto rounded px-2 py-1 text-xs text-zinc-300 hover:bg-zinc-800" onClick={onOpenTerminal}>
            Open terminal
          </button>
        )}
      </div>
      {prompt.context && (
        <pre className="mt-2 max-h-48 overflow-auto whitespace-pre-wrap break-words rounded bg-[var(--theme-canvas,rgba(0,0,0,0.3))] p-2 font-mono text-xs text-zinc-100">{prompt.context}</pre>
      )}
      {notice && <div role="status" className="mt-2 text-xs text-amber-300">{notice}</div>}
      <div className="mt-3 flex flex-col items-start gap-1.5">
        {actions.map((action, index) => action.takes_text
          ? <TextAnswer key={`${index}-${action.keys}`} action={action} disabled={pending} onAnswer={(text) => onChoose(action.keys, text)} />
          : (
            <button
              key={`${index}-${action.keys}`}
              className={isWayOut(action.keys) ? wayOutClass : choiceClass}
              disabled={pending}
              onClick={() => onChoose(action.keys)}
            >
              {action.label}
            </button>
          ))}
      </div>
    </section>
  )
}

// A row the operator answers by typing: Claude Code's "Type something.".
function TextAnswer({ action, disabled, onAnswer }: { action: PromptAction; disabled: boolean; onAnswer: (text: string) => void }) {
  const [text, setText] = useState("")
  const answer = text.trim()
  return (
    <form
      className="flex w-full min-w-0 gap-1.5"
      onSubmit={(event) => {
        event.preventDefault()
        if (answer && !disabled) onAnswer(answer)
      }}
    >
      <input
        aria-label={action.label}
        className="min-w-0 flex-1 rounded border border-zinc-700 bg-[var(--theme-canvas,rgba(0,0,0,0.3))] px-2 py-2 text-xs text-[var(--theme-text,white)] sm:py-1"
        placeholder={action.label}
        value={text}
        disabled={disabled}
        onChange={(event) => setText(event.target.value.replace(/[\r\n]/g, " "))}
      />
      <button type="submit" aria-label="Send answer" className={choiceClass} disabled={disabled || !answer}>
        Send
      </button>
    </form>
  )
}
