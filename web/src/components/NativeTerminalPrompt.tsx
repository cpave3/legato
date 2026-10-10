import type { PromptState } from "../hooks/useWebSocket"

interface NativeTerminalPromptProps {
  prompt: PromptState
  // Whose terminal is asking, as the heading names it: "Codex".
  askedBy: string
  // Answers with the keys an option stands for, sent to the tool's terminal.
  onChoose: (keys: string) => void
  // Takes the person to the terminal itself, for a prompt this cannot show.
  onOpenTerminal?: () => void
}

// Leaving a prompt is set apart from choosing one of its options.
const isWayOut = (keys: string) => keys === "Escape"

// A prompt a tool is showing in its terminal that never reaches Legato as a
// request of its own: an update notice, a trust question, a picker. It is
// read off the screen, so it is shown as the terminal words it, and answered
// by pressing the keys the option takes. It knows nothing of the tool beyond
// its name.
export function NativeTerminalPrompt({ prompt, askedBy, onChoose, onOpenTerminal }: NativeTerminalPromptProps) {
  const actions = prompt.actions ?? []
  return (
    <section aria-label={`${askedBy} is asking in its terminal`} className="mx-4 my-2 min-w-0 rounded-lg border border-amber-600/40 bg-amber-900/10 p-3">
      <div className="flex min-w-0 flex-wrap items-center gap-2">
        <div className="text-sm font-semibold text-amber-200">{askedBy} is asking in its terminal</div>
        {onOpenTerminal && (
          <button className="ml-auto rounded px-2 py-1 text-xs text-zinc-300 hover:bg-zinc-800" onClick={onOpenTerminal}>
            Open terminal
          </button>
        )}
      </div>
      {prompt.context && (
        <pre className="mt-2 max-h-48 overflow-auto whitespace-pre-wrap break-words rounded bg-black/30 p-2 font-mono text-xs text-zinc-100">{prompt.context}</pre>
      )}
      <div className="mt-3 flex flex-col items-start gap-1.5">
        {actions.map((action, index) => (
          <button
            key={`${index}-${action.keys}`}
            className={isWayOut(action.keys)
              ? "rounded px-2 py-1 text-left text-xs text-zinc-300 hover:bg-zinc-800"
              : "max-w-full break-words rounded bg-amber-700/50 px-2 py-1 text-left text-xs text-white hover:bg-amber-600/50"}
            onClick={() => onChoose(action.keys)}
          >
            {action.label}
          </button>
        ))}
      </div>
    </section>
  )
}
