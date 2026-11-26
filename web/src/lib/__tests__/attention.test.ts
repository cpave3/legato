import { afterEach, describe, expect, it, vi } from "vitest"

vi.mock("../api", () => ({ apiFetch: vi.fn() }))

import { apiFetch } from "../api"
import { AttentionElsewhereError, executeAttentionAction } from "../attention"

const mocked = vi.mocked(apiFetch)
const json = (body: unknown, status: number) => new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } })

afterEach(() => mocked.mockReset())

describe("an attention item's action", () => {
  it("says where a review loop already runs when Legato refuses to start another", async () => {
    mocked.mockResolvedValue(json({ error: "review_loop_active: held", href: "/workflows/runs/wr-1" }, 409))
    const refused = await executeAttentionAction("", "att-1", "start_review", "1").catch((error: unknown) => error)
    expect(refused).toBeInstanceOf(AttentionElsewhereError)
    expect(refused).toMatchObject({ message: "review_loop_active: held", href: "/workflows/runs/wr-1" })
  })

  it("throws any other refusal as it came", async () => {
    mocked.mockResolvedValue(json({ error: "attention item changed; refresh and try again" }, 409))
    const refused = await executeAttentionAction("", "att-1", "start_review", "1").catch((error: unknown) => error)
    expect(refused).not.toBeInstanceOf(AttentionElsewhereError)
    expect((refused as Error).message).toContain("attention item changed")
  })
})
