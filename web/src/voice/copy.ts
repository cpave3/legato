import type { VoiceView } from "./session"

// idleLine is why a session is idle, said for a person.
export function idleLine(view: VoiceView): string {
  if (view.ended) return view.ended === "bye" || view.ended === "closed" ? "" : `Ended: ${view.ended}`
  if (view.written) return "Answered in writing"
  switch (view.reason) {
    case "no_speech": return "I didn't hear anything"
    case "nothing_heard": return "I couldn't make out any words"
    case "no_answer": return "The Reeve didn't answer"
    case "failed": return "That didn't reach the Reeve"
    case "mic_lost": return "The microphone stopped"
  }
  return ""
}

