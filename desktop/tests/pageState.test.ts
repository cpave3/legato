import assert from "node:assert/strict"
import { test } from "node:test"
import { failedAtStart, recovered, screenFor } from "../src/pageState.ts"

const up = (id: string) => ({ id, online: true, approvals: [] })
const down = (id: string) => ({ id, online: false, error: "Connection refused", approvals: [] })

test("a server that does not answer gets the app's offline screen, not its page", () => {
  assert.equal(screenFor(down("a"), false), "offline")
  assert.equal(screenFor(up("a"), false), "page")
})

test("a server whose state is not known yet shows its page", () => {
  assert.equal(screenFor(undefined, false), "page")
})

test("a page that did not load is not shown, whatever the server's state", () => {
  assert.equal(screenFor(undefined, true), "offline")
  assert.equal(screenFor(up("a"), true), "offline")
})

test("a page made before the server's state was known failed when the server turns out to be down", () => {
  const made = new Set(["a", "b"])
  assert.deepEqual(failedAtStart(made, {}, { a: down("a"), b: up("b"), c: down("c") }), ["a"])
})

test("a loaded page whose server goes down later is kept as it is", () => {
  assert.deepEqual(failedAtStart(new Set(["a"]), { a: up("a") }, { a: down("a") }), [])
  assert.deepEqual(failedAtStart(new Set(["a"]), { a: down("a") }, { a: down("a") }), [])
})

test("a failed page loads again once its server answers", () => {
  const failed = { a: "http://a.test/board", b: "", c: "" }
  assert.deepEqual(recovered(failed, { a: up("a"), b: down("b") }), ["a"])
})
