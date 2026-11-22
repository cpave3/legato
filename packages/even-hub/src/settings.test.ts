import { describe, expect, it } from "vitest"
import { loadCode, saveCode } from "./settings"

function memoryStorage(initial: Record<string, string> = {}) {
  const data = new Map(Object.entries(initial))
  return {
    data,
    getLocalStorage: async (key: string) => data.get(key) ?? "",
    setLocalStorage: async (key: string, value: string) => (data.set(key, value), true),
  }
}

const CODE = "ABCDEFGHIJKLMNOPQRSTUVWX"

describe("loadCode", () => {
  it("reads the pairing code saved on the phone, or none", async () => {
    expect(await loadCode(memoryStorage({ "legato.phone.code": CODE }))).toBe(CODE)
    expect(await loadCode(memoryStorage())).toBe("")
  })
})

describe("saveCode", () => {
  it("keeps the code as the Legato app shows it, without the spaces and dashes it was copied with", async () => {
    const storage = memoryStorage()

    expect(await saveCode(storage, " abcd-efgh ijkl-mnop qrst-uvwx ")).toBe(CODE)

    expect(storage.data.get("legato.phone.code")).toBe(CODE)
  })

  it("forgets the code when given none", async () => {
    const storage = memoryStorage({ "legato.phone.code": CODE })

    expect(await saveCode(storage, "  ")).toBe("")

    expect(await loadCode(storage)).toBe("")
  })

  it("refuses what is not a code, keeping the one saved", async () => {
    const storage = memoryStorage({ "legato.phone.code": CODE })

    await expect(saveCode(storage, "ABCDEFGHIJKLMNO")).rejects.toThrow(/16 to 64/)
    await expect(saveCode(storage, "A".repeat(65))).rejects.toThrow(/16 to 64/)
    await expect(saveCode(storage, "ABCDEFGHIJKLMNOP1")).rejects.toThrow(/letters and the digits 2 to 7/)

    expect(await loadCode(storage)).toBe(CODE)
  })

  it("takes a code of 16 or 64 characters", async () => {
    const storage = memoryStorage()

    expect(await saveCode(storage, "A".repeat(16))).toBe("A".repeat(16))
    expect(await saveCode(storage, "2".repeat(64))).toBe("2".repeat(64))
  })
})
