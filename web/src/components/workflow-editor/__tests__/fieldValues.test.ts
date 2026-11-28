import { describe, expect, it } from "vitest"
import { isExpressionValue, jsonText, jsonValue, notANumber, showsPicker, suggestionsFor, takesExpressionText } from "../fieldValues"
import type { FieldSpec } from "../stepForms"

const spec = (over: Partial<FieldSpec>): FieldSpec => ({ name: "f", kind: "string", description: "", required: false, expressions: true, requiredAtRun: false, label: "F", essential: false, order: 0, ...over })

describe("a field's value", () => {
  it("is an expression when it holds ${{", () => {
    expect(isExpressionValue("a ${{ x }}")).toBe(true)
    expect(isExpressionValue("a")).toBe(false)
    expect(isExpressionValue(3)).toBe(false)
  })

  it("is not a number when an integer field holds other text", () => {
    expect(notANumber("integer", "abc")).toBe(true)
    expect(notANumber("integer", " 12 ")).toBe(false)
    expect(notANumber("integer", "${{ x }}")).toBe(false)
    expect(notANumber("integer", "")).toBe(false)
    expect(notANumber("integer", 4)).toBe(false)
    expect(notANumber("string", "abc")).toBe(false)
  })

  it("reads and writes JSON", () => {
    expect(jsonText(undefined)).toBe("")
    expect(jsonText("raw")).toBe("raw")
    expect(jsonText([1])).toBe("[\n  1\n]")
    expect(jsonValue(" ")).toBeUndefined()
    expect(jsonValue("[1]")).toEqual([1])
    expect(jsonValue("nope")).toBe("nope")
  })
})

describe("suggestionsFor", () => {
  const options = { macros: ["M"], agents: ["A"], tiers: { a: ["t1"], b: ["t2"] }, columns: ["C"] }
  it("offers each kind this Legato's values, a tier its agent's or every agent's", () => {
    expect(suggestionsFor("macro", options, "")).toEqual(["M"])
    expect(suggestionsFor("agent", options, "")).toEqual(["A"])
    expect(suggestionsFor("tier", options, "b")).toEqual(["t2"])
    expect(suggestionsFor("tier", options, "")).toEqual(["t1", "t2"])
    expect(suggestionsFor("column", options, "")).toEqual(["C"])
    expect(suggestionsFor("repository", options, "")).toEqual([])
    expect(suggestionsFor("chat_bot", options, "")).toEqual([])
    expect(suggestionsFor("path", options, "")).toEqual([])
  })
})

describe("which control a field gets", () => {
  it("takes values at the cursor in the inspector for a text kind that reads values", () => {
    expect(takesExpressionText(spec({}), true, true, false)).toBe(true)
    expect(takesExpressionText(spec({}), false, true, false)).toBe(false)
    expect(takesExpressionText(spec({}), true, false, false)).toBe(false)
    expect(takesExpressionText(spec({ kind: "integer" }), true, true, false)).toBe(false)
    expect(takesExpressionText(spec({ enum: ["a"] }), true, true, false)).toBe(false)
    expect(takesExpressionText(spec({ enum: ["a"] }), true, true, true)).toBe(true)
  })

  it("offers the reference picker outside the inspector, but not for buttons", () => {
    expect(showsPicker(spec({}), false)).toBe(true)
    expect(showsPicker(spec({}), true)).toBe(false)
    expect(showsPicker(spec({ expressions: false }), false)).toBe(false)
    expect(showsPicker(spec({ kind: "chat_buttons" }), false)).toBe(false)
  })
})
