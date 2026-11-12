// Recording a key for the voice key (Servers & keys): the modifiers held and
// the DOM's code for the key, which src-tauri/src/voicekey.rs (accelerator)
// writes as the global-shortcut plugin reads it, or refuses with a reason.

export type KeyPress = Pick<KeyboardEvent, "code" | "ctrlKey" | "altKey" | "shiftKey" | "metaKey">

const MODIFIER = /^(Control|Alt|Shift|Meta|OS)(Left|Right)?$/

// The key pressed, or null while only modifiers are down.
export function recordedKey(e: KeyPress): string | null {
  if (MODIFIER.test(e.code)) return null
  const mods: string[] = []
  if (e.ctrlKey) mods.push("Ctrl")
  if (e.altKey) mods.push("Alt")
  if (e.shiftKey) mods.push("Shift")
  if (e.metaKey) mods.push("Super")
  return [...mods, e.code].join("+")
}
