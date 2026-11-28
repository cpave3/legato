import { describe, expect, it } from "vitest"
import { catalogError, catalogItems, catalogStatus, listOrEmpty, slashItemsOf, startCatalog, type Catalog } from "../nativeCatalog"

const loaded: Catalog = {
  key: "a",
  commands: [{ name: "review", body: "Please review", path: "/c", source: "user" }],
  skills: [{ name: "tdd", description: "Tests first", path: "/s", source: "project" }],
  loading: false,
  errors: ["Skills unavailable: x"],
}

describe("nativeCatalog", () => {
  it("keeps what a session's catalog held while it loads again, and drops another session's", () => {
    expect(startCatalog(loaded, "a")).toEqual({ ...loaded, loading: true, errors: [] })
    expect(startCatalog(loaded, "b")).toEqual({ key: "b", commands: [], skills: [], loading: true, errors: [] })
  })

  it("says which list is unavailable and why", () => {
    expect(catalogError("commands", new Error("HTTP 500"))).toBe("Commands unavailable: HTTP 500")
    expect(catalogError("skills", "offline")).toBe("Skills unavailable: offline")
  })

  it("takes a list from an endpoint and refuses anything else", async () => {
    await expect(catalogItems(new Response("[1]", { status: 200 }))).resolves.toEqual([1])
    await expect(catalogItems(new Response("{}", { status: 200 }))).rejects.toThrow("Invalid catalog response")
    await expect(catalogItems(new Response("", { status: 503 }))).rejects.toThrow("HTTP 503")
  })

  it("offers the session's commands then its skills in the slash menu", () => {
    expect(slashItemsOf(loaded, "a")).toEqual([
      { type: "command", name: "review", description: "", body: "Please review", source: "user" },
      { type: "skill", name: "tdd", description: "Tests first", source: "project" },
    ])
    expect(slashItemsOf(loaded, "b")).toEqual([])
  })

  it("counts another session's catalog as still loading, with no errors of its own", () => {
    expect(catalogStatus(loaded, "a")).toEqual({ loading: false, errors: ["Skills unavailable: x"] })
    expect(catalogStatus(loaded, "b")).toEqual({ loading: true, errors: [] })
  })

  it("reads anything that is not a list as an empty one", () => {
    expect(listOrEmpty([1, 2])).toEqual([1, 2])
    expect(listOrEmpty({ macros: [] })).toEqual([])
    expect(listOrEmpty(undefined)).toEqual([])
  })
})
