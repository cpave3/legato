import { describe, expect, it } from "vitest"
import { insertImagePath } from "../agentImage"

const path = "/tmp/legato-1000/pasted-images/task-1/pasted-1.png"

describe("insertImagePath", () => {
  it("puts the path where the cursor is, set off by spaces", () => {
    expect(insertImagePath("What is wrong here?", path)).toBe(`What is wrong here? ${path} `)
    expect(insertImagePath("look at  please", path, 8)).toBe(`look at ${path} please`)
  })

  // LEG-151: Claude Code will not take a message from Legato that starts with
  // "/", so a path never starts the message.
  it("names the image when the path would start the message", () => {
    expect(insertImagePath("", path)).toBe(`Image: ${path} `)
    expect(insertImagePath("  ", path)).toBe(`  Image: ${path} `)
    expect(insertImagePath("what is this?", path, 0)).toBe(`Image: ${path} what is this?`)
  })

  it("does not name a second image after the first", () => {
    const first = insertImagePath("", path)
    expect(insertImagePath(first, "/tmp/b.png")).toBe(`Image: ${path} /tmp/b.png `)
  })
})
