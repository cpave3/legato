import { describe, expect, it } from "vitest"
import type { Heard } from "../takeSound"
import { keptPicture, takeRefusal, takeWarning } from "../studioTake"

const heard = (extra: Partial<Heard> = {}): Heard => ({ seconds: 5, loud: true, silentMs: 0, silentTotalMs: 0, silence: null, ...extra })

describe("studioTake", () => {
  it("refuses a take thrown away, too short, or without sound, in that order", () => {
    expect(takeRefusal({ cancelled: true, lastedMs: 10, bytes: 0 })).toBe("That take was thrown away.")
    expect(takeRefusal({ cancelled: false, lastedMs: 999, bytes: 0 })).toMatch(/too short/)
    expect(takeRefusal({ cancelled: false, lastedMs: 5000, bytes: 1999 })).toMatch(/^Nothing was recorded/)
    expect(takeRefusal({ cancelled: false, lastedMs: 5000, bytes: 5000, heard: heard({ loud: false }) })).toMatch(/^Nothing was recorded/)
    expect(takeRefusal({ cancelled: false, lastedMs: 5000, bytes: 5000, heard: heard() })).toBe("")
    expect(takeRefusal({ cancelled: false, lastedMs: 5000, bytes: 5000 })).toBe("")
  })

  it("warns of a recorder that may lose the end, of missing sound, and of a quiet stretch", () => {
    expect(takeWarning(undefined, 5000)).toMatch(/its own recorder was used/)
    expect(takeWarning(heard({ seconds: 4 }), 5000)).toBe("Only 4.0 s of sound came from a take of 5.0 s, so some of what you said is missing. Listen to it, and record it again if it is.")
    expect(takeWarning(heard({ seconds: 4.6 }), 5000)).toBe("")
    expect(takeWarning(heard({ silentTotalMs: 3000 }), 5000)).toBe("The microphone gave nothing at all for 3.0 s of this 5.0 s take, in short stretches. If you were speaking then, another page or program may have had the microphone: close it, and record the take again.")
  })

  it("keeps a picture only when the camera gave one", () => {
    expect(keptPicture(null)).toBeUndefined()
    expect(keptPicture(new Blob(["x"]))).toBeUndefined()
    const film = new Blob(["x".repeat(2000)])
    expect(keptPicture(film)).toBe(film)
  })
})
