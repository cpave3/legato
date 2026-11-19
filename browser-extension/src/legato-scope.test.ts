import { describe, expect, it, vi } from "vitest"
import { LegatoClient, LegatoRequestError } from "./legato"

const json = (body: unknown, status = 200) => new Response(JSON.stringify(body), { status })

describe("LegatoClient's Scope calls", () => {
  it("posts a batch of pins", async () => {
    const answer = { batch_id: "sb-1", pin_ids: ["sp-1"], subscribed: false, delivered: false }
    const fetcher = vi.fn<typeof fetch>().mockResolvedValue(json(answer, 201))
    const batch = { origin: "http://localhost:5173", pins: [] }
    await expect(new LegatoClient("http://localhost:3080", "t", fetcher).sendScopeBatch(batch)).resolves.toEqual(answer)
    expect(fetcher).toHaveBeenCalledWith("http://localhost:3080/api/scope/batches", {
      method: "POST",
      headers: { authorization: "Bearer t", "content-type": "application/json" },
      body: JSON.stringify(batch),
    })
  })

  it("lists an origin's pins", async () => {
    const fetcher = vi.fn<typeof fetch>().mockResolvedValue(json({ pins: null }))
    await expect(new LegatoClient("http://localhost:3080", "t", fetcher).listScopePins("http://localhost:5173")).resolves.toEqual([])
    expect(fetcher.mock.calls[0][0]).toBe("http://localhost:3080/api/scope/pins?origin=http%3A%2F%2Flocalhost%3A5173")
  })

  it("reopens a pin with a new comment, and withdraws one", async () => {
    const fetcher = vi.fn<typeof fetch>()
      .mockResolvedValueOnce(json({ id: "sp-1", state: "sent" }))
      .mockResolvedValueOnce(new Response(null, { status: 204 }))
    const client = new LegatoClient("http://localhost:3080", "t", fetcher)
    await expect(client.reopenScopePin("sp/1", "Still wrong")).resolves.toEqual({ id: "sp-1", state: "sent" })
    expect(fetcher.mock.calls[0][0]).toBe("http://localhost:3080/api/scope/pins/sp%2F1/reopen")
    expect(fetcher.mock.calls[0][1]?.body).toBe(JSON.stringify({ comment: "Still wrong" }))
    await expect(client.withdrawScopePin("sp-2")).resolves.toBeUndefined()
    expect(fetcher.mock.calls[1][0]).toBe("http://localhost:3080/api/scope/pins/sp-2/withdraw")
    expect(fetcher.mock.calls[1][1]?.method).toBe("POST")
  })

  it("says which status Legato refused with", async () => {
    const fetcher = vi.fn<typeof fetch>().mockResolvedValue(json({ error: "too many pins" }, 413))
    const failure = await new LegatoClient("http://localhost:3080", "t", fetcher).listScopePins("http://a.test").catch((error: unknown) => error)
    expect(failure).toBeInstanceOf(LegatoRequestError)
    expect(failure).toMatchObject({ message: "too many pins", status: 413 })
  })
})
