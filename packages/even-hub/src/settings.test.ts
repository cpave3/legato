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

  it("reads the Legato a packed build was made for when nothing is saved", async () => {
    expect(await loadSource(memoryStorage(), { url: "https://erebus.example.ts.net" })).toEqual({
      url: "https://erebus.example.ts.net",
    })
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

describe("saveSource and a token already saved", () => {
  it("keeps the saved token when only the address's path changes", async () => {
    const storage = memoryStorage({ "legato.url": "https://erebus.example.ts.net", "legato.token": "t0k" })

    await saveSource(storage, { url: "https://erebus.example.ts.net/" })

    expect(await loadSource(storage)).toEqual({ url: "https://erebus.example.ts.net/", token: "t0k" })
  })

  it("drops the saved token when the address moves to another server", async () => {
    const storage = memoryStorage({ "legato.url": "https://erebus.example.ts.net", "legato.token": "t0k" })

    await saveSource(storage, { url: "https://other.example.ts.net" })

    expect(await loadSource(storage)).toEqual({ url: "https://other.example.ts.net" })
  })
})

describe("saveSource refusing an address", () => {
  it("refuses plain HTTP except on this device, so the token never travels in the clear", async () => {
    const storage = memoryStorage()

    await expect(saveSource(storage, { url: "http://erebus.example.ts.net", token: "t" })).rejects.toThrow(/HTTPS/)
    await expect(saveSource(storage, { url: "not an address", token: "t" })).rejects.toThrow(/address/)
    expect(storage.data.size).toBe(0)

    await saveSource(storage, { url: "http://127.0.0.1:3080", token: "t" })
    expect(await loadSource(storage)).toEqual({ url: "http://127.0.0.1:3080", token: "t" })
  })
})
