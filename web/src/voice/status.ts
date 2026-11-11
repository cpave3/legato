import { apiFetch } from "../lib/api"

// The fragment that opens the voice sheet: the ring button sets it, and the
// Android app loads the main's page with it.
export const VOICE_HASH = "#reeve-voice"

// GET /api/reeve/voice, the parts the page reads.
export interface VoiceStatus {
  instance: { id: string; name: string; role: string }
  ready: boolean
  reason: string
  setting: string
}

// fetchVoiceStatus asks the server whether a conversation will work, giving
// up after 2 seconds.
export async function fetchVoiceStatus(baseUrl: string): Promise<VoiceStatus> {
  const abort = new AbortController()
  const timer = setTimeout(() => abort.abort(), 2000)
  try {
    const res = await apiFetch(baseUrl, "/api/reeve/voice", { signal: abort.signal })
    if (!res.ok) throw new Error(String(res.status))
    return (await res.json()) as VoiceStatus
  } finally {
    clearTimeout(timer)
  }
}

// openVoice opens the voice sheet.
export function openVoice() {
  window.location.hash = VOICE_HASH
}

