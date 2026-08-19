import { describe, expect, it, vi } from "vitest"
import { LegatoClient } from "./legato"

const discovery = { definitions: [], errors: [], repositories: [] }

describe("LegatoClient", () => {
  it("discovers workflows with worker-owned bearer authentication", async () => {
    const fetcher = vi.fn<typeof fetch>().mockResolvedValue(new Response(JSON.stringify(discovery), { status: 200 }))
    const client = new LegatoClient("http://localhost:3080", "top-secret", fetcher)

    await expect(client.discover()).resolves.toEqual(discovery)
    expect(fetcher).toHaveBeenCalledWith("http://localhost:3080/api/workflows", {
      headers: { authorization: "Bearer top-secret" },
    })
  })

  it("preserves a user-fixable server error without including credentials", async () => {
    const fetcher = vi.fn<typeof fetch>().mockResolvedValue(new Response("workflow not found\n", { status: 404 }))
    const client = new LegatoClient("http://localhost:3080", "top-secret", fetcher)

    await expect(client.discover()).rejects.toThrow("workflow not found")
    await expect(client.discover()).rejects.not.toThrow("top-secret")
  })
})
