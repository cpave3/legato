import { cleanup, fireEvent, render, screen, within } from "@testing-library/react"
import { afterEach, describe, expect, it, vi } from "vitest"
import { NativeQuestionCard } from "../NativeQuestionCard"
import type { NativeQuestionItem } from "../../lib/nativeAgentSync"

const request = (overrides: Partial<NativeQuestionItem> = {}): NativeQuestionItem => ({
  requestId: "req-1",
  status: "pending",
  questions: [
    {
      id: "q1",
      header: "Color",
      question: "Which color?",
      options: [{ label: "Red", description: "Warm" }, { label: "Blue", description: "Cool", preview: "preview of blue" }],
      multiSelect: false,
      allowOther: true,
    },
    {
      id: "q2",
      header: "Sizes",
      question: "Which sizes?",
      options: [{ label: "Small" }, { label: "Medium" }, { label: "Large" }],
      multiSelect: true,
      allowOther: true,
    },
  ],
  ...overrides,
})

const renderCard = (question = request()) => {
  const onAnswer = vi.fn()
  const onDismiss = vi.fn()
  render(<NativeQuestionCard question={question} askedBy="Claude Code" onAnswer={onAnswer} onDismiss={onDismiss} />)
  const card = screen.getByRole("region", { name: "Question from Claude Code" })
  const group = (name: string) => within(card).getByRole("group", { name })
  return { card, group, onAnswer, onDismiss }
}

describe("NativeQuestionCard", () => {
  afterEach(cleanup)

  it("sends the options chosen once every question has an answer", () => {
    const { card, group, onAnswer } = renderCard()
    const send = within(card).getByRole("button", { name: "Send answers" }) as HTMLButtonElement

    expect(within(group("Which color?")).getByText("Warm")).not.toBeNull()
    expect(send.disabled).toBe(true)

    fireEvent.click(within(group("Which color?")).getByRole("radio", { name: /Blue/ }))
    expect(send.disabled).toBe(true)
    fireEvent.click(within(group("Which sizes?")).getByRole("checkbox", { name: /Small/ }))
    fireEvent.click(within(group("Which sizes?")).getByRole("checkbox", { name: /Large/ }))
    fireEvent.click(send)

    expect(onAnswer).toHaveBeenCalledWith("req-1", { q1: ["Blue"], q2: ["Small", "Large"] })
  })

  it("takes an answer the person types in place of the options", () => {
    const { card, group, onAnswer } = renderCard()

    fireEvent.click(within(group("Which color?")).getByRole("radio", { name: /Red/ }))
    fireEvent.change(within(group("Which color?")).getByLabelText("Your own answer"), { target: { value: "  Teal  " } })
    fireEvent.click(within(group("Which sizes?")).getByRole("checkbox", { name: /Medium/ }))
    fireEvent.change(within(group("Which sizes?")).getByLabelText("Your own answer"), { target: { value: "Extra large" } })
    fireEvent.click(within(card).getByRole("button", { name: "Send answers" }))

    // One choice: the typed answer replaces the option. Several: it joins them.
    expect(onAnswer).toHaveBeenCalledWith("req-1", { q1: ["Teal"], q2: ["Medium", "Extra large"] })
  })

  it("shows what choosing an option would look like", () => {
    const { group } = renderCard()
    expect(screen.queryByText("preview of blue")).toBeNull()

    fireEvent.click(within(group("Which color?")).getByRole("radio", { name: /Blue/ }))

    expect(within(group("Which color?")).getByText("preview of blue")).not.toBeNull()
  })

  it("lets the person close the question without answering", () => {
    const { card, onAnswer, onDismiss } = renderCard()

    fireEvent.click(within(card).getByRole("button", { name: "Dismiss" }))

    expect(onDismiss).toHaveBeenCalledWith("req-1")
    expect(onAnswer).not.toHaveBeenCalled()
  })

  it("offers no typed answer where the agent does not take one", () => {
    const question = request()
    question.questions = [{ ...question.questions[0], allowOther: false }]
    const { card } = renderCard(question)

    expect(within(card).queryByLabelText("Your own answer")).toBeNull()
    expect(within(card).getByRole("button", { name: "Send answer" })).not.toBeNull()
  })

  it("holds its controls while an answer is on its way and reports a failure", () => {
    const { card } = renderCard(request({ status: "submitting" }))
    expect((within(card).getByRole("radio", { name: /Blue/ }) as HTMLInputElement).disabled).toBe(true)
    expect((within(card).getByRole("button", { name: "Dismiss" }) as HTMLButtonElement).disabled).toBe(true)
    cleanup()

    renderCard(request({ error: "answer question failed: 400 Bad Request" }))
    expect(screen.getByRole("alert").textContent).toContain("400 Bad Request")
  })

  it("keeps the answers in view once the question is settled", () => {
    const { card } = renderCard(request({ status: "answered", answers: { q1: ["Blue"], q2: ["Small", "Extra large"] } }))

    expect(within(card).getByText("Answered")).not.toBeNull()
    expect(within(card).getByText("Blue")).not.toBeNull()
    expect(within(card).getByText("Small, Extra large")).not.toBeNull()
    expect(within(card).queryByRole("radio")).toBeNull()
    expect(within(card).queryByRole("button", { name: /Send answer/ })).toBeNull()
  })

  it("says when a question was closed without an answer from here", () => {
    const dismissed = renderCard(request({ status: "dismissed" }))
    expect(within(dismissed.card).getByText("Dismissed")).not.toBeNull()
    cleanup()

    const elsewhere = renderCard(request({ status: "expired" }))
    expect(within(elsewhere.card).getByText("Closed elsewhere")).not.toBeNull()
    expect(within(elsewhere.card).queryByRole("button")).toBeNull()
  })
})
