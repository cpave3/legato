import { describe, expect, it } from "vitest"
import { followIfNearBottom, scrollAnchorOf } from "../useConversationScroll"

function viewport(scrollTop: number) {
  const el = document.createElement("div")
  Object.defineProperties(el, {
    scrollHeight: { value: 1000, configurable: true },
    clientHeight: { value: 300, configurable: true },
    scrollTop: { value: scrollTop, writable: true, configurable: true },
  })
  return el
}

describe("conversation scrolling", () => {
  it("follows new content only from near the end", () => {
    const near = viewport(650)
    followIfNearBottom(near)
    expect(near.scrollTop).toBe(1000)

    const away = viewport(600)
    followIfNearBottom(away)
    expect(away.scrollTop).toBe(600)

    expect(() => followIfNearBottom(null)).not.toThrow()
  })

  it("remembers where the reader was and whether that was the end", () => {
    expect(scrollAnchorOf(viewport(670))).toEqual({ atBottom: true, top: 670 })
    expect(scrollAnchorOf(viewport(600))).toEqual({ atBottom: false, top: 600 })
    expect(scrollAnchorOf(null)).toBeNull()
  })
})
