import { afterEach, describe, expect, it, vi } from "vitest"
import { archiveTask, deleteTask } from "../board"

afterEach(() => vi.unstubAllGlobals())

// A worker refuses to archive or delete a card a main handed to it; the
// person reads why, in the server's words.
describe("a refused delete or archive", () => {
  const refusal = "this task belongs to workstation; return or recall it from there"

  it("throws the server's words", async () => {
    vi.stubGlobal("fetch", vi.fn(async () => new Response(JSON.stringify({ error: refusal }), { status: 409 })))

    await expect(deleteTask("", "w1")).rejects.toThrow(refusal)
    await expect(archiveTask("", "w1")).rejects.toThrow(refusal)
  })

  it("says the status when the server gives no words", async () => {
    vi.stubGlobal("fetch", vi.fn(async () => new Response("<html>oops</html>", { status: 500 })))

    await expect(deleteTask("", "w1")).rejects.toThrow("deleteTask failed: 500")
    await expect(archiveTask("", "w1")).rejects.toThrow("archiveTask failed: 500")
  })
})
