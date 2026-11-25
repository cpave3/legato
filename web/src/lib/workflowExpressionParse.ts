// The expression language of ${{ }}, read as the engine reads it
// (internal/engine/workflow/expr.go): a path, with [n] and .field reaching
// into JSON, and ?? fallbacks that may be quoted literals, null, true, false
// or whole numbers. internal/engine/workflow/testdata/expressions.json is
// checked by both.
//
//   expression := operand ( "??" operand )*
//   operand    := path | literal          (the first is always a path)
//   path       := name ( "." name | "[" integer "]" )*

export type PathPart = { name: string } | { index: number }
export type Operand = { path: PathPart[] } | { literal: unknown }
export type ParsedExpression = { operands: Operand[] }
export type ParseError = { error: string }

const notName = new Set([" ", "\t", "\n", "\r", ".", "[", "]", "{", "}", "?", "'", "\""])
const spaces = new Set([" ", "\t", "\n", "\r"])

function wordLiteral(word: string): { value: unknown } | null {
  if (word === "null") return { value: null }
  if (word === "true") return { value: true }
  if (word === "false") return { value: false }
  if (/^-?[0-9]+$/.test(word)) return { value: Number(word) }
  return null
}

class Parser {
  at = 0
  readonly text: string
  constructor(text: string) { this.text = text }
  done() { return this.at >= this.text.length }
  space() { while (!this.done() && spaces.has(this.text[this.at])) this.at++ }
  name(): string {
    const start = this.at
    while (!this.done() && !notName.has(this.text[this.at])) this.at++
    return this.text.slice(start, this.at)
  }
  operand(first: boolean): Operand | ParseError {
    if (this.done()) return { error: first ? "empty expression" : "?? needs a fallback after it" }
    const c = this.text[this.at]
    if (c === "'" || c === "\"") {
      const end = this.text.indexOf(c, this.at + 1)
      if (end < 0) return { error: `unterminated ${c} in "${this.text}"` }
      const value = this.text.slice(this.at + 1, end)
      this.at = end + 1
      if (first) return { error: `an expression starts with a path, not the literal "${value}"` }
      return { literal: value }
    }
    const word = this.name()
    if (word === "") return { error: `unexpected "${this.text.slice(this.at)}" in "${this.text}"` }
    const literal = wordLiteral(word)
    if (literal) {
      if (first) return { error: `an expression starts with a path, not the literal ${word}` }
      return { literal: literal.value }
    }
    const path: PathPart[] = [{ name: word }]
    while (!this.done()) {
      const next = this.text[this.at]
      if (next === ".") {
        this.at++
        const name = this.name()
        if (name === "") return { error: `a name must follow . in "${this.text}"` }
        path.push({ name })
      } else if (next === "[") {
        const end = this.text.indexOf("]", this.at)
        if (end < 0) return { error: `unclosed [ in "${this.text}"` }
        const inside = this.text.slice(this.at + 1, end).trim()
        if (!/^-?[0-9]+$/.test(inside)) return { error: `[${inside}] in "${this.text}": an index is a whole number, such as [0] or [-1]` }
        path.push({ index: Number(inside) })
        this.at = end + 1
      } else {
        break
      }
    }
    return { path }
  }
}

export function isParseError(value: unknown): value is ParseError {
  return typeof value === "object" && value !== null && "error" in value
}

/** parseExpression reads the inside of a ${{ }}. */
export function parseExpression(text: string): ParsedExpression | ParseError {
  const parser = new Parser(text)
  const operands: Operand[] = []
  for (;;) {
    parser.space()
    const operand = parser.operand(operands.length === 0)
    if (isParseError(operand)) return operand
    operands.push(operand)
    parser.space()
    if (parser.done()) return { operands }
    if (!text.startsWith("??", parser.at)) return { error: `unexpected "${text.slice(parser.at)}" in "${text}": join a fallback with ??` }
    parser.at += 2
  }
}

/** parsePath reads a bare path, such as a condition's field. */
export function parsePath(text: string): PathPart[] | ParseError {
  const parser = new Parser(text)
  parser.space()
  const operand = parser.operand(true)
  if (isParseError(operand)) return operand
  parser.space()
  if (!parser.done() || !("path" in operand)) return { error: `"${text}" is not a path: a field takes a path alone` }
  return operand.path
}

/** pathNames are the leading names of a path, up to its first index. */
export function pathNames(path: PathPart[]): string[] {
  const names: string[] = []
  for (const part of path) {
    if (!("name" in part)) break
    names.push(part.name)
  }
  return names
}

/** pathString writes a path as a file says it. */
export function pathString(path: PathPart[]): string {
  return path.map((part, i) => "index" in part ? `[${part.index}]` : `${i > 0 ? "." : ""}${part.name}`).join("")
}

export type ExpressionMatch = { start: number; end: number; inner: string }

/** findExpressions lists each ${{ }} in text. A quoted literal may hold }};
 * a quote with no close is not one. A ${{ with no }} after it is text. */
export function findExpressions(text: string): ExpressionMatch[] {
  const out: ExpressionMatch[] = []
  let from = 0
  for (;;) {
    const start = text.indexOf("${{", from)
    if (start < 0) return out
    let at = start + 3
    let end = -1
    while (at < text.length) {
      const c = text[at]
      if (c === "'" || c === "\"") {
        const close = text.indexOf(c, at + 1)
        if (close >= 0) { at = close + 1; continue }
      }
      if (text.startsWith("}}", at)) { end = at + 2; break }
      at++
    }
    if (end < 0) return out
    out.push({ start, end, inner: text.slice(start + 3, end - 2).trim() })
    from = end
  }
}
