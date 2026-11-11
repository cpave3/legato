import { reeveWash } from "./ReeveEntry"

// Whether a line said out loud was heard. Unknown until Legato reports it.
export type SaidStatus = "spoken" | "not_spoken"

export interface SaidBubbleProps {
  // The words the agent said out loud.
  text: string
  // Whether they were spoken, when Legato knows.
  status?: SaidStatus
}

// SaidBubble is a line the agent said out loud through Legato's speak tool:
// the words, in the Reeve's violet, so the agent need not write them too.
export function SaidBubble({ text, status }: SaidBubbleProps) {
  return (
    <div className="flex justify-start px-4 py-2">
      <div
        role="group"
        aria-label="Said out loud"
        className={`max-w-[75%] rounded-lg border-l-2 border-l-[#8b5cf6] px-3 py-2 text-[13px] text-lw-text whitespace-pre-wrap break-words ${reeveWash}`}
      >
        <div className="mb-1 flex items-center gap-2 text-[10px] font-semibold uppercase tracking-wide text-[#c4b5fd]">
          <span><span aria-hidden="true">◈ </span>Said</span>
          {status === "not_spoken" && <span className="text-lw-error">Not spoken</span>}
        </div>
        <div>{text}</div>
      </div>
    </div>
  )
}
