import type { VoiceView } from "./session"

// idleLine is why a session is idle, said for a person; reeve is the
// Reeve's name at the start and in the middle of a sentence (lib/reeveName).
export function idleLine(view: VoiceView, reeve: { start: string; mid: string } = { start: "The Reeve", mid: "the Reeve" }): string {
  if (view.ended) return view.ended === "bye" || view.ended === "closed" ? "" : `Ended: ${view.ended}`
  if (view.written) return "Answered in writing"
  switch (view.reason) {
    case "no_speech": return "I didn't hear anything"
    case "nothing_heard": return "I couldn't make out any words"
    case "no_answer": return `${reeve.start} didn't answer`
    case "failed": return `That didn't reach ${reeve.mid}`
    case "mic_lost": return "The microphone stopped"
  }
  return ""
}

