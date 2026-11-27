import { afterEach, describe, expect, it, vi } from "vitest"
import { listJobClones, removeJobClone } from "../jobClones"
import {
  addMCPConnection, beginWebAuthorization, cancelAuthorization, changeMCPConnection, confirmAuthorization,
  disconnectMCPConnection, getAuthorization, getMCPConnection, listMCPConnections, removeMCPConnection, testMCPConnection,
} from "../mcpConnections"
import { addRepository, listRepositories, removeRepository } from "../repositories"
import { listRepositoryGuidance, removeRepositoryGuidance, saveRepositoryGuidance } from "../repositoryGuidance"
import { attachedResources, attachResources, deleteResource, listResources, saveResource } from "../resources"
import { listSkills, setSkillEnabled } from "../skills"
import { deleteMacro, fetchMacroList, fetchMacros, renameMacro, resetMacro, saveMacro, setMacroFavorite, startMacro } from "../macros"

// Every API client below reads a refused request the same way: the body's
// error when it is a string (even an empty one), else the status text, else
// "request failed (<status>)". These tests pin that through each exported
// call, so the readers can share one implementation without any caller
// seeing a different message.

afterEach(() => vi.unstubAllGlobals())

const calls: Record<string, () => Promise<unknown>> = {
  listJobClones: () => listJobClones(""),
  removeJobClone: () => removeJobClone("", 1),
  listMCPConnections: () => listMCPConnections(""),
  getMCPConnection: () => getMCPConnection("", "docs"),
  addMCPConnection: () => addMCPConnection("", { name: "docs" } as Parameters<typeof addMCPConnection>[1]),
  changeMCPConnection: () => changeMCPConnection("", "docs", {} as Parameters<typeof changeMCPConnection>[2]),
  testMCPConnection: () => testMCPConnection("", "docs"),
  removeMCPConnection: () => removeMCPConnection("", "docs"),
  beginWebAuthorization: () => beginWebAuthorization("", "docs"),
  getAuthorization: () => getAuthorization("", "a1"),
  confirmAuthorization: () => confirmAuthorization("", "a1"),
  cancelAuthorization: () => cancelAuthorization("", "a1"),
  disconnectMCPConnection: () => disconnectMCPConnection("", "docs"),
  listRepositories: () => listRepositories(""),
  addRepository: () => addRepository("", { name: "app", repo: "acme/app" } as Parameters<typeof addRepository>[1]),
  removeRepository: () => removeRepository("", 1),
  listRepositoryGuidance: () => listRepositoryGuidance(""),
  saveRepositoryGuidance: () => saveRepositoryGuidance("", "acme/app", "Be kind."),
  removeRepositoryGuidance: () => removeRepositoryGuidance("", "acme/app"),
  listResources: () => listResources(""),
  saveResource: () => saveResource("", { id: 0, name: "docs", paths: [] }),
  deleteResource: () => deleteResource("", 1),
  attachedResources: () => attachedResources("", "T-1"),
  attachResources: () => attachResources("", "T-1", ["docs"]),
  listSkills: () => listSkills(""),
  setSkillEnabled: () => setSkillEnabled("", "tdd", true),
  fetchMacroList: () => fetchMacroList(""),
  fetchMacros: () => fetchMacros(""),
  startMacro: () => startMacro("", "T-1", "go"),
  saveMacro: () => saveMacro("", "go", { message: "Go." }),
  setMacroFavorite: () => setMacroFavorite("", "go", true),
  deleteMacro: () => deleteMacro("", "go"),
  resetMacro: () => resetMacro("", "go"),
  renameMacro: () => renameMacro("", "go", "went"),
}

// Calls whose success resolves to nothing: the body of a successful
// response is none of their business, so they must leave it unread.
const voidCalls = [
  "removeJobClone", "removeMCPConnection", "cancelAuthorization", "removeRepository", "removeRepositoryGuidance",
  "deleteResource", "saveMacro", "setMacroFavorite", "deleteMacro", "resetMacro", "renameMacro",
]

interface Refusal { name: string; body: string | null; status: number; statusText: string; message: string }

const refusals: Refusal[] = [
  { name: "a string error", body: '{"error":"no such thing"}', status: 404, statusText: "Not Found", message: "no such thing" },
  { name: "a non-string error", body: '{"error":{"code":"x"}}', status: 409, statusText: "Conflict", message: "Conflict" },
  { name: "a numeric error", body: '{"error":7}', status: 409, statusText: "Conflict", message: "Conflict" },
  { name: "an empty string error, over the status text", body: '{"error":""}', status: 400, statusText: "Bad Request", message: "request failed (400)" },
  { name: "no error field", body: "{}", status: 500, statusText: "Internal Server Error", message: "Internal Server Error" },
  { name: "invalid JSON", body: "<html>gateway</html>", status: 502, statusText: "Bad Gateway", message: "Bad Gateway" },
  { name: "null JSON", body: "null", status: 500, statusText: "Server Error", message: "Server Error" },
  { name: "a JSON string", body: '"broken"', status: 500, statusText: "Server Error", message: "Server Error" },
  { name: "a JSON number", body: "42", status: 500, statusText: "Server Error", message: "Server Error" },
  { name: "no body", body: null, status: 503, statusText: "Unavailable", message: "Unavailable" },
  { name: "invalid JSON and no status text", body: "oops", status: 418, statusText: "", message: "request failed (418)" },
  { name: "a non-string error and no status text", body: '{"error":false}', status: 422, statusText: "", message: "request failed (422)" },
]

describe.each(Object.keys(calls))("%s", (name) => {
  const call = calls[name]

  it.each(refusals)("rejects with an Error on $name", async ({ body, status, statusText, message }) => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response(body, { status, statusText })))

    const failure = await call().then(() => undefined, (error: unknown) => error)
    expect(failure).toBeInstanceOf(Error)
    expect((failure as Error).message).toBe(message)
  })

  it("propagates a network failure unchanged", async () => {
    const network = new TypeError("Failed to fetch")
    vi.stubGlobal("fetch", vi.fn().mockRejectedValue(network))

    await expect(call()).rejects.toBe(network)
  })

  if (voidCalls.includes(name)) {
    it("leaves a successful response's body unread", async () => {
      const response = new Response('{"ignored":true}', { status: 200 })
      vi.stubGlobal("fetch", vi.fn().mockResolvedValue(response))

      await expect(call()).resolves.toBeUndefined()
      expect(response.bodyUsed).toBe(false)
    })
  }
})
