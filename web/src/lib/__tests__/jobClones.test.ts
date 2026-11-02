import { afterEach, describe, expect, it, vi } from "vitest"
import { listJobClones } from "../jobClones"

afterEach(() => vi.unstubAllGlobals())

describe("listJobClones", () => {
  it("returns clones in server order and accepts a genuinely empty list", async () => {
    const clone = { id: 8, repo: "acme/app", path: "/jobs/app", branch: "", task_ids: ["T-1"], busy: true, last_used_at: "2026-11-02T10:00:00Z", unpushed: 0 }
    const clones = [clone, { ...clone, id: 2, branch: "older", busy: false }]
    const fetch = vi.fn()
      .mockResolvedValueOnce(new Response(JSON.stringify({ clones })))
      .mockResolvedValueOnce(new Response(JSON.stringify({ clones: [] })))
    vi.stubGlobal("fetch", fetch)

    expect(await listJobClones("http://legato")).toEqual(clones)
    expect(fetch.mock.calls[0][0]).toBe("http://legato/api/job-clones")
    expect(await listJobClones("")).toEqual([])
  })

  it("reports an older server's non-JSON error using its HTTP status", async () => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response("<html>not found</html>", { status: 404 })))

    await expect(listJobClones("")).rejects.toThrow("request failed (404)")
  })

  it("propagates network errors", async () => {
    vi.stubGlobal("fetch", vi.fn().mockRejectedValue(new Error("Failed to fetch")))

    await expect(listJobClones("")).rejects.toThrow("Failed to fetch")
  })

  it("rejects with the server's reason instead of listing no clones", async () => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response(JSON.stringify({ error: "clone store unavailable" }), { status: 503 })))

    await expect(listJobClones("http://legato")).rejects.toThrow("clone store unavailable")
  })

  it.each(["null", "{}", '{"clones":null}', '{"clones":{}}', '<html>older server</html>'])("rejects a malformed response: %s", async (body) => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response(body)))

    await expect(listJobClones("")).rejects.toThrow("Invalid job clones response")
  })

  it.each([null, {}, { id: 1, repo: "acme/app", task_ids: "T-1" }])("rejects malformed clone entries: %j", async (clone) => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response(JSON.stringify({ clones: [clone] }))))

    await expect(listJobClones("")).rejects.toThrow("Invalid job clones response")
  })
})
