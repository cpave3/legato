import { cleanup, fireEvent, render, screen } from "@testing-library/react"
import { afterEach, describe, expect, it, vi } from "vitest"
import { WithField } from "../StepsEditor"
import type { BuilderOptions, FieldSpec } from "../stepForms"

// WithField on its own: each kind's control, the frame round it, and what
// a commit writes.

afterEach(cleanup)

const options: BuilderOptions = { macros: ["Review"], agents: ["claude-code", "codex"], tiers: { codex: ["fast"], claude: ["deep"] }, columns: ["Doing"], repositories: ["a/b"], bots: ["slack"] }
const spec = (over: Partial<FieldSpec>): FieldSpec => ({ name: "f", kind: "string", description: "What f is.", required: false, expressions: false, requiredAtRun: false, label: "F", essential: false, order: 0, ...over })
const commit = (element: HTMLElement, value: string) => { fireEvent.change(element, { target: { value } }); fireEvent.blur(element) }

function draw(over: Partial<FieldSpec>, value: unknown, extra: Partial<Parameters<typeof WithField>[0]> = {}) {
  const onChange = vi.fn()
  const { container } = render(<WithField spec={spec(over)} stepId="s" value={value} onChange={onChange} refs={["workflow.task"]} taskRefs={["${{ workflow.task }}", "${{ workflow.inputs.target }}"]} options={options} agent="" problems={[]} {...extra} />)
  return { onChange, container, field: () => screen.getByLabelText(`${over.name ?? "f"} of step s`) }
}

describe("the frame", () => {
  it("says the label, whether it is required, the name when it differs, and the description", () => {
    const { container } = draw({ label: "The F", required: true, essential: true }, "x")
    expect(container.firstElementChild!.getAttribute("data-essential")).toBe("true")
    expect(container.textContent).toContain("The F (required)f")
    expect(screen.getByText("What f is.")).toBeTruthy()
    cleanup()
    const plain = draw({ requiredAtRun: true }, "x")
    expect(plain.container.firstElementChild!.hasAttribute("data-essential")).toBe(false)
    expect(plain.container.textContent).toContain("F (required)")
  })

  it("offers a reference picker for a field that takes expressions, but not for buttons", () => {
    draw({ expressions: true }, "a")
    fireEvent.click(screen.getByRole("button", { name: "Insert a reference into f of step s" }))
    cleanup()
    draw({ expressions: true, kind: "chat_buttons" }, [])
    expect(screen.queryByRole("button", { name: "Insert a reference into f of step s" })).toBeNull()
  })
})

describe("the controls by kind", () => {
  it("writes text, and drops an empty optional value", () => {
    const { onChange, field } = draw({}, "a")
    commit(field(), "")
    expect(onChange).toHaveBeenLastCalledWith(undefined)
    commit(field(), "b")
    expect(onChange).toHaveBeenLastCalledWith("b")
  })

  it("keeps an empty required value", () => {
    const { onChange, field } = draw({ required: true }, "a")
    commit(field(), "")
    expect(onChange).toHaveBeenLastCalledWith("")
  })

  it("writes long text in a multi-line field", () => {
    const { field } = draw({ kind: "text" }, "a\nb")
    expect(field().tagName).toBe("TEXTAREA")
  })

  it("turns a boolean on and off, and shows an expression as text", () => {
    const { onChange, field } = draw({ kind: "boolean" }, true)
    fireEvent.click(field())
    expect(onChange).toHaveBeenLastCalledWith(undefined)
    cleanup()
    const required = draw({ kind: "boolean", required: true }, true)
    fireEvent.click(required.field())
    expect(required.onChange).toHaveBeenLastCalledWith(false)
    cleanup()
    const expression = draw({ kind: "boolean" }, "${{ x }}")
    expect((expression.field() as HTMLInputElement).value).toBe("${{ x }}")
  })

  it("writes a whole number as one and anything else as text, marking it", () => {
    const { onChange, field } = draw({ kind: "integer" }, 3)
    commit(field(), "12")
    expect(onChange).toHaveBeenLastCalledWith(12)
    cleanup()
    const wrong = draw({ kind: "integer", name: "n" }, "abc")
    expect(wrong.field().getAttribute("aria-invalid")).toBe("true")
    expect(screen.getByText("n must be a whole number or a ${{ }} expression")).toBeTruthy()
    cleanup()
    const expression = draw({ kind: "integer" }, "${{ steps.a.outputs.n }}")
    expect(expression.field().hasAttribute("aria-invalid")).toBe(false)
  })

  it("writes JSON as its value, text that isn't JSON as text, and nothing as nothing", () => {
    const { onChange, field } = draw({ kind: "json" }, { a: 1 })
    expect((field() as HTMLTextAreaElement).value).toBe('{\n  "a": 1\n}')
    commit(field(), '{"b": 2}')
    expect(onChange).toHaveBeenLastCalledWith({ b: 2 })
    commit(field(), "not json")
    expect(onChange).toHaveBeenLastCalledWith("not json")
    commit(field(), "  ")
    expect(onChange).toHaveBeenLastCalledWith(undefined)
    cleanup()
    expect((draw({ kind: "json" }, undefined).field() as HTMLTextAreaElement).value).toBe("")
    cleanup()
    expect((draw({ kind: "json" }, "raw").field() as HTMLTextAreaElement).value).toBe("raw")
  })

  it("shows buttons given by an expression as text", () => {
    expect((draw({ kind: "chat_buttons" }, "${{ x }}").field() as HTMLInputElement).value).toBe("${{ x }}")
  })

  it("chooses a task reference, keeping one it doesn't list", () => {
    const { onChange, field } = draw({ kind: "task" }, "")
    expect([...(field() as HTMLSelectElement).options].map(o => o.text)).toEqual(["Choose a task", "The workflow's task", "Input target"])
    fireEvent.change(field(), { target: { value: "${{ workflow.inputs.target }}" } })
    expect(onChange).toHaveBeenLastCalledWith("${{ workflow.inputs.target }}")
    cleanup()
    expect([...(draw({ kind: "task" }, "${{ workflow.task }}").field() as HTMLSelectElement).options].map(o => o.text)).toEqual(["The workflow's task", "Input target"])
  })

  it("chooses from an enum, or types an expression in its place", () => {
    const { onChange, field } = draw({ enum: ["a", "b"] }, "")
    expect([...(field() as HTMLSelectElement).options].map(o => o.text)).toEqual(["The default", "a", "b"])
    fireEvent.change(field(), { target: { value: "b" } })
    expect(onChange).toHaveBeenLastCalledWith("b")
    cleanup()
    expect([...(draw({ enum: ["a"], required: true }, "").field() as HTMLSelectElement).options][0].text).toBe("Choose")
    cleanup()
    expect(draw({ enum: ["a"] }, "${{ x }}").field().tagName).toBe("INPUT")
  })

  it("suggests this Legato's values for each kind that has them", () => {
    const listed = (kind: string, agent = "") => {
      const { container } = draw({ kind }, "", { agent })
      const values = [...container.querySelectorAll("datalist option")].map(o => o.getAttribute("value"))
      cleanup()
      return values
    }
    expect(listed("macro")).toEqual(["Review"])
    expect(listed("agent")).toEqual(["claude-code", "codex"])
    expect(listed("tier", "codex")).toEqual(["fast"])
    expect(listed("tier")).toEqual(["fast", "deep"])
    expect(listed("column")).toEqual(["Doing"])
    expect(listed("repository")).toEqual(["a/b"])
    expect(listed("chat_bot")).toEqual(["slack"])
    expect(listed("path")).toEqual([])
  })
})

describe("in the inspector", () => {
  const reads = { values: { schema_version: 2, steps: [{ id: "s", type: "agent.run" }] }, location: { kind: "with" as const, step: 0 } }

  it("draws a boolean as a switch and an agent as a choice", () => {
    const { onChange } = draw({ kind: "boolean" }, false, { rich: true })
    fireEvent.click(screen.getByRole("switch", { name: "f of step s" }))
    expect(onChange).toHaveBeenLastCalledWith(true)
    cleanup()
    const agent = draw({ kind: "agent" }, "", { rich: true })
    fireEvent.click(screen.getByRole("radio", { name: "codex" }))
    expect(agent.onChange).toHaveBeenLastCalledWith("codex")
  })

  it("takes a value from elsewhere into a text field that takes expressions", () => {
    draw({ expressions: true }, "a", { rich: true, reads: reads as never })
    expect(screen.queryByRole("button", { name: "Insert a reference into f of step s" })).toBeNull()
    expect(screen.getByLabelText("f of step s")).toBeTruthy()
  })
})
