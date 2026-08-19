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

  it("starts the selected workflow with the authenticated API contract", async () => {
    const fetcher = vi.fn<typeof fetch>().mockResolvedValue(new Response(JSON.stringify({ run: { id: "run-1" } }), { status: 201 }))
    const client = new LegatoClient("http://localhost:3080", "top-secret", fetcher)

    await expect(client.startWorkflow("review pr", { repo: "cpave3/legato", pr_number: "42" })).resolves.toEqual({ run: { id: "run-1" } })
    expect(fetcher).toHaveBeenCalledWith("http://localhost:3080/api/workflows/review%20pr/runs", {
      method: "POST",
      headers: { authorization: "Bearer top-secret", "content-type": "application/json" },
      body: JSON.stringify({ inputs: { repo: "cpave3/legato", pr_number: "42" } }),
    })
  })

  it("preserves a user-fixable server error without including credentials", async () => {
    const fetcher = vi.fn<typeof fetch>().mockResolvedValue(new Response("workflow not found\n", { status: 404 }))
    const client = new LegatoClient("http://localhost:3080", "top-secret", fetcher)

    await expect(client.discover()).rejects.toThrow("workflow not found")
    await expect(client.discover()).rejects.not.toThrow("top-secret")
  })
})
