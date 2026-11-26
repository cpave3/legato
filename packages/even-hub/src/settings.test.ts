import { describe, expect, it } from "vitest"
import { POLL_MS } from "./app"
import { loadCode, loadScreenOff, saveCode, saveScreenOff, screenOffMs } from "./settings"

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

describe("reading while the glasses' screen is off", () => {
  it("is every 5 seconds, as while it is on, unless the operator chose 30, 60 or off", async () => {
    const storage = memoryStorage()
    expect(await loadScreenOff(storage)).toBe("5")

    await saveScreenOff(storage, "off")
    expect(storage.data.get("legato.screen-off")).toBe("off")
    expect(await loadScreenOff(storage)).toBe("off")

    // A choice saved before 5 seconds was one stays as it was.
    expect(await loadScreenOff(memoryStorage({ "legato.screen-off": "30" }))).toBe("30")
    expect(await loadScreenOff(memoryStorage({ "legato.screen-off": "10" }))).toBe("5")
    await saveScreenOff(storage, "5")
    expect(storage.data.get("legato.screen-off")).toBe("5")
    await expect(saveScreenOff(storage, "10")).rejects.toThrow()
  })

  it("comes to milliseconds, or none when off", () => {
    expect(screenOffMs("5")).toBe(POLL_MS)
    expect(screenOffMs("30")).toBe(30_000)
    expect(screenOffMs("60")).toBe(60_000)
    expect(screenOffMs("off")).toBeUndefined()
  })
})
