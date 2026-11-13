import { describe, expect, it } from "vitest"
import { loadSource, saveSource } from "./settings"

function memoryStorage(initial: Record<string, string> = {}) {
  const data = new Map(Object.entries(initial))
  return {
    data,
    getLocalStorage: async (key: string) => data.get(key) ?? "",
    setLocalStorage: async (key: string, value: string) => (data.set(key, value), true),
  }
}

describe("loadSource", () => {
  it("reads Legato's address and token saved on the phone", async () => {
    const storage = memoryStorage({ "legato.url": "https://erebus.example.ts.net", "legato.token": "t0k" })

    expect(await loadSource(storage)).toEqual({ url: "https://erebus.example.ts.net", token: "t0k" })
  })

  it("reads through the dev server when nothing is saved", async () => {
    expect(await loadSource(memoryStorage())).toEqual({ url: "/legato" })
  })
})

describe("saveSource", () => {
  it("keeps the address and token, trimmed, and clears both when the address is empty", async () => {
    const storage = memoryStorage()

    await saveSource(storage, { url: " https://erebus.example.ts.net/ ", token: " t0k " })
    expect(await loadSource(storage)).toEqual({ url: "https://erebus.example.ts.net/", token: "t0k" })

    await saveSource(storage, { url: "", token: "left over" })
    expect(await loadSource(storage)).toEqual({ url: "/legato" })
    expect(storage.data.get("legato.token")).toBe("")
  })
})
