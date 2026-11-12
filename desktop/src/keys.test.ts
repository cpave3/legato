import { describe, expect, it } from "vitest"
import { recordedKey } from "./keys"

const press = (code: string, mods: { ctrl?: boolean; alt?: boolean; shift?: boolean; meta?: boolean } = {}) => ({
  code, ctrlKey: !!mods.ctrl, altKey: !!mods.alt, shiftKey: !!mods.shift, metaKey: !!mods.meta,
})

describe("recordedKey", () => {
  it("writes the modifiers held and the key's code, as the app reads them", () => {
    expect(recordedKey(press("KeyV", { ctrl: true, alt: true }))).toBe("Ctrl+Alt+KeyV")
    expect(recordedKey(press("Space", { ctrl: true, alt: true, shift: true }))).toBe("Ctrl+Alt+Shift+Space")
    expect(recordedKey(press("Digit1", { meta: true }))).toBe("Super+Digit1")
  })

  it("waits while only modifiers are down", () => {
    for (const code of ["ControlLeft", "AltRight", "ShiftLeft", "MetaLeft", "OSRight"]) {
      expect(recordedKey(press(code, { ctrl: true }))).toBeNull()
    }
  })

  it("leaves a key with no modifiers to the app, which says why it is refused", () => {
    expect(recordedKey(press("KeyA"))).toBe("KeyA")
  })
})
