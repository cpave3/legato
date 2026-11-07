import { beforeEach, describe, expect, it } from "vitest"
import { terminalUrl, touchWheel, wheelSequence } from "../terminalSocket"
import { setToken } from "../auth"

describe("terminalUrl", () => {
  beforeEach(() => localStorage.clear())

  it("uses the page's origin and its token", () => {
    setToken("abc d")
    const u = new URL(terminalUrl("", "t 1", 120, 40))
    expect(u.protocol).toBe("ws:")
    expect(u.host).toBe(window.location.host)
    expect(u.pathname).toBe("/api/agents/t%201/terminal")
    expect(u.searchParams.get("cols")).toBe("120")
    expect(u.searchParams.get("rows")).toBe("40")
    expect(u.searchParams.get("token")).toBe("abc d")
  })

  it("uses another server's address and token, wss for https", () => {
    setToken("remote", "https://box:8443")
    const u = new URL(terminalUrl("https://box:8443", "x", 80, 24))
    expect(u.protocol).toBe("wss:")
    expect(u.host).toBe("box:8443")
    expect(u.searchParams.get("token")).toBe("remote")
  })

  it("sends no token when there is none", () => {
    expect(new URL(terminalUrl("", "x", 80, 24)).searchParams.has("token")).toBe(false)
  })
})

describe("wheel", () => {
  it("makes SGR wheel reports", () => {
    expect(wheelSequence(true)).toBe("\x1b[<64;1;1M")
    expect(wheelSequence(false)).toBe("\x1b[<65;1;1M")
  })
  it("splits a drag into notches and a remainder", () => {
    expect(touchWheel(40, 17)).toEqual({ notches: 2, rest: 6 })
    expect(touchWheel(-40, 17)).toEqual({ notches: -2, rest: -6 })
    expect(touchWheel(10, 17)).toEqual({ notches: 0, rest: 10 })
  })
})
