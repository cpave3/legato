import { beforeEach, describe, expect, it, vi } from "vitest"
import { fetchPlanQueue, respondToPlanQuestion, planAction } from "../plan"

const mockFetch = vi.fn()
vi.stubGlobal("fetch", mockFetch)
vi.mock("../auth", () => ({ getToken: () => "token" }))

describe("plan API", () => {
  beforeEach(() => mockFetch.mockReset())
  it("loads the plan queue from the active server", async () => {
    mockFetch.mockResolvedValue({ ok:true, json:async()=>[{plan_id:"pl-1",title:"Search"}] })
    expect(await fetchPlanQueue("https://legato.test")).toEqual([{plan_id:"pl-1",title:"Search"}])
    expect(mockFetch).toHaveBeenCalledWith("https://legato.test/api/plans/queue", expect.objectContaining({headers:{Authorization:"Bearer token"}}))
  })
  it("answers a required choice and approves through lifecycle endpoints", async () => {
    mockFetch.mockResolvedValue({ok:true})
    await respondToPlanQuestion("", "pl-1", "backend", {values:["sqlite"]})
    await planAction("", "pl-1", "approve")
    expect(mockFetch).toHaveBeenNthCalledWith(1,"/api/plans/pl-1/responses/backend",expect.objectContaining({method:"PUT",body:'{"values":["sqlite"]}'}))
    expect(mockFetch).toHaveBeenNthCalledWith(2,"/api/plans/pl-1/approve",expect.objectContaining({method:"POST"}))
  })
})
