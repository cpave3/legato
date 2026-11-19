import { describe, expect, it } from "vitest"
import { createClearedStore, createDraftStore, createLock, draftsKey, memoryArea } from "./scope-drafts"
import type { ScopeDraft } from "./scope-types"

const origin = "http://localhost:5173"

function draft(id: string, comment = "Too much padding", url = `${origin}/settings`): ScopeDraft {
  return {
    id,
    createdAt: "2026-11-19T10:00:00Z",
    hasScreenshot: false,
    pin: {
      comment,
      page: { url, title: "Settings", viewport: { width: 1280, height: 800 }, scroll: { x: 0, y: 0 }, dpr: 2 },
      target: { selectors: ["#save"], text: "Save", box: { x: 10, y: 20, width: 80, height: 30 } },
      styles: { computed: {}, classes: [], inline: "", parent: {} },
    },
  }
}

describe("the drafts of an origin", () => {
  // Each tab's content script has its own store over the same storage; the
  // service worker makes their changes one at a time with one lock.
  it("loses no change when two stores share a lock", async () => {
    const area = memoryArea()
    const lock = createLock()
    const one = createDraftStore(area, origin, lock)
    const two = createDraftStore(area, origin, lock)
    await Promise.all([one.add(draft("a")), two.add(draft("b"))])
    expect((await one.list()).map((d) => d.id).sort()).toEqual(["a", "b"])
    await Promise.all([one.remove("a"), two.update("b", "Edited")])
    expect(await one.list()).toEqual([{ ...draft("b", "Edited") }])
    const cleared = [createClearedStore(area, origin, lock), createClearedStore(area, origin, lock)]
    await Promise.all([cleared[0].add(["sp-1"]), cleared[1].add(["sp-2"])])
    expect((await cleared[0].list()).sort()).toEqual(["sp-1", "sp-2"])
  })

  it("goes on after a change that failed", async () => {
    const lock = createLock()
    await expect(lock("k", async () => { throw new Error("full") })).rejects.toThrow("full")
    expect(await lock("k", async () => 1)).toBe(1)
  })

  it("keeps drafts across pages of the origin, in the order they were placed", async () => {
    const area = memoryArea()
    const store = createDraftStore(area, origin)
    await store.add(draft("a"))
    await store.add(draft("b", "Wrong colour", `${origin}/billing`))
    expect((await createDraftStore(area, origin).list()).map((d) => d.id)).toEqual(["a", "b"])
  })

  it("keeps each origin's drafts apart", async () => {
    const area = memoryArea()
    await createDraftStore(area, origin).add(draft("a"))
    expect(await createDraftStore(area, "https://other.test").list()).toEqual([])
    expect(draftsKey(origin)).toBe("scopeDrafts:http://localhost:5173")
  })

  it("edits a draft's comment and deletes drafts", async () => {
    const store = createDraftStore(memoryArea(), origin)
    await store.add(draft("a"))
    await store.add(draft("b"))
    await store.add(draft("c"))
    await store.update("b", "Make it red")
    await store.remove("a")
    expect((await store.list()).map((d) => [d.id, d.pin.comment])).toEqual([["b", "Make it red"], ["c", "Too much padding"]])
    await store.removeMany(["b", "c"])
    expect(await store.list()).toEqual([])
  })

  it("keeps a screenshot under its own key, and deletes it with its draft", async () => {
    const area = memoryArea()
    const store = createDraftStore(area, origin)
    await store.add(draft("a"), "data:image/jpeg;base64,AAAA")
    const [saved] = await store.list()
    expect(saved.hasScreenshot).toBe(true)
    expect(JSON.stringify(area.data[draftsKey(origin)])).not.toContain("base64")
    expect(await store.screenshot("a")).toBe("data:image/jpeg;base64,AAAA")
    await store.remove("a")
    expect(await store.screenshot("a")).toBeUndefined()
    expect(Object.keys(area.data)).toEqual([draftsKey(origin)])
  })

  it("ignores a stored value that is not a list of drafts", async () => {
    const area = memoryArea({ [draftsKey(origin)]: "nonsense" })
    expect(await createDraftStore(area, origin).list()).toEqual([])
  })
})
