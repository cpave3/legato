import { MAX_FRAMES, MAX_URL_LENGTH, type RawFrame } from "./types"

// The frames of an Error's stack, innermost first: Chrome's
// "    at name (url:line:column)" and "    at url:line:column", and
// Firefox's "name@url:line:column". Only http and https URLs are kept.
const FRAME = /(https?:\/\/[^\s()]+):(\d+):(\d+)\)?\s*$/

export function parseStack(stack: string, limit: number = MAX_FRAMES): RawFrame[] {
  const frames: RawFrame[] = []
  for (const line of stack.split("\n")) {
    if (frames.length >= limit) break
    const match = FRAME.exec(line)
    if (!match || match[1].length > MAX_URL_LENGTH) continue
    frames.push({ url: match[1], line: Number(match[2]), column: Number(match[3]), original: false })
  }
  return frames
}
