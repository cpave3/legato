import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { CredentialsSettings } from "../CredentialsSettings"

function json(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } })
}

let grants: Array<Record<string, unknown>>
let deleted: string[]

beforeEach(() => {
  grants = [{ id: "cg-1", credential: "elevenlabs", task_id: "t1", execution_id: "ex-1", operations: ["speak"], resources: [], mode: "session", created_at: "2026-10-29T10:00:00Z" }]
  deleted = []
  vi.stubGlobal("fetch", vi.fn(async (url: string, init?: RequestInit) => {
    const method = init?.method ?? "GET"
    if (method === "DELETE") {
      deleted.push(url)
      grants = []
      return new Response(null, { status: 204 })
    }
    if (url.includes("/api/credentials/grants")) return json({ grants })
    if (url.includes("/api/credentials/audit")) return json({ entries: [{ seq: 4, at: "2026-10-29T10:01:00.000Z", event: "use", credential: "elevenlabs", operation: "speak", task_id: "t1", prev_hash: "", hash: "h" }] })
    return json({ credentials: [{ name: "elevenlabs", profile: "secret", store: "legato", secret: "legato://eleven-key", rules: [{ operation: ["speak"], approve: "auto" }] }] })
  }))
})

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

describe("CredentialsSettings", () => {
  it("lists the credentials, the live grants and what was done with them", async () => {
    render(<CredentialsSettings baseUrl="http://legato" />)

    const section = await screen.findByRole("region", { name: "Credentials" })
    await waitFor(() => expect(within(section).getByText("legato://eleven-key")).toBeTruthy())
    expect(within(section).getByText("this session")).toBeTruthy()
    expect(within(section).getByText("use")).toBeTruthy()
  })

  it("ends a grant early", async () => {
    render(<CredentialsSettings baseUrl="http://legato" />)

    fireEvent.click(await screen.findByRole("button", { name: "End grant of elevenlabs for t1" }))

    await waitFor(() => expect(deleted).toEqual(["http://legato/api/credentials/grants/cg-1"]))
    await waitFor(() => expect(screen.queryByRole("button", { name: /End grant/ })).toBeNull())
  })
})
