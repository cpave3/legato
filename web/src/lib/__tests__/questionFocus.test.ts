import { describe, expect, it } from "vitest"
import type { NativeQuestion } from "../nativeAgentSync"
import {
  answersOf, backStep, chooseOption, chosenPreview, dialogKeyAction, draftIn, missingMessage, movedIndex, nextLabel, nextStep, ownKeyAction, withDraft,
  type FocusKey,
} from "../questionFocus"

const single: NativeQuestion = { id: "s", question: "One?", options: [{ label: "A" }, { label: "B", preview: "b!" }, { label: "C" }] }
const several: NativeQuestion = { ...single, id: "m", multiSelect: true }

describe("questionFocus drafts", () => {
  it("keeps each approval's drafts apart", () => {
    const drafts = withDraft({}, "ap-1", "s", (d) => ({ ...d, other: "typed" }))
    expect(draftIn(drafts, "ap-1", "s")).toEqual({ chosen: [], other: "typed" })
    expect(draftIn(drafts, "ap-2", "s")).toEqual({ chosen: [], other: "" })
    const more = withDraft(drafts, "ap-1", "m", (d) => ({ ...d, chosen: ["A"] }))
    expect(answersOf([single, several], more, "ap-1")).toEqual({ s: ["typed"], m: ["A"] })
  })

  it("replaces a single choice and toggles one of several", () => {
    expect(chooseOption(single, { chosen: ["A"], other: "x" }, "B")).toEqual({ chosen: ["B"], other: "" })
    expect(chooseOption(several, { chosen: ["A"], other: "x" }, "B")).toEqual({ chosen: ["A", "B"], other: "x" })
    expect(chooseOption(several, { chosen: ["A", "B"], other: "" }, "A")).toEqual({ chosen: ["B"], other: "" })
  })

  it("moves round the options, starting from an end", () => {
    expect(movedIndex(single, { chosen: [], other: "" }, 1)).toBe(0)
    expect(movedIndex(single, { chosen: [], other: "" }, -1)).toBe(2)
    expect(movedIndex(single, { chosen: ["C"], other: "" }, 1)).toBe(0)
    expect(movedIndex(single, { chosen: ["A"], other: "" }, -1)).toBe(2)
  })

  it("shows the preview of a chosen option", () => {
    expect(chosenPreview(single, { chosen: ["B"], other: "" })).toBe("b!")
    expect(chosenPreview(single, { chosen: ["A"], other: "" })).toBeUndefined()
  })
})

describe("questionFocus steps", () => {
  it("goes on through the questions, to review, then sends", () => {
    expect(nextStep({ screen: "question", q: 0 }, 3)).toEqual({ screen: "question", q: 1 })
    expect(nextStep({ screen: "question", q: 2 }, 3)).toEqual({ screen: "review", q: 2 })
    expect(nextStep({ screen: "review", q: 2 }, 3)).toBe("send")
    expect(nextStep({ screen: "question", q: 0 }, 1)).toBe("send")
  })

  it("goes back from review to the last question, and stops at the first", () => {
    expect(backStep({ screen: "review", q: 0 }, 3)).toEqual({ screen: "question", q: 2 })
    expect(backStep({ screen: "question", q: 2 }, 3)).toEqual({ screen: "question", q: 1 })
    expect(backStep({ screen: "question", q: 0 }, 3)).toBeNull()
  })

  it("names what Next does", () => {
    expect(nextLabel({ screen: "review", q: 0 }, 2, "Codex")).toBe("Send answers to Codex")
    expect(nextLabel({ screen: "question", q: 0 }, 2, "Codex")).toBe("Next question")
    expect(nextLabel({ screen: "question", q: 1 }, 2, "Codex")).toBe("Review answers")
    expect(nextLabel({ screen: "question", q: 0 }, 1, "Codex")).toBe("Send answer")
    expect(missingMessage(1)).toBe("Choose an answer first.")
    expect(missingMessage(2)).toBe("This one still needs an answer.")
  })
})

describe("questionFocus keys", () => {
  const key = (k: string, extra: Partial<FocusKey> = {}): FocusKey => ({ key: k, mod: false, onButton: false, screen: "question", multiSelect: false, canType: true, ...extra })

  it("reads the box's keys", () => {
    expect(ownKeyAction("Enter")).toEqual({ do: "next" })
    expect(ownKeyAction("Escape")).toEqual({ do: "release" })
    expect(ownKeyAction("1")).toBeNull()
  })

  it("reads the dialog's keys", () => {
    expect(dialogKeyAction(key("Escape"))).toEqual({ do: "leave" })
    expect(dialogKeyAction(key("Enter", { mod: true, onButton: true }))).toEqual({ do: "send" })
    expect(dialogKeyAction(key("3"))).toEqual({ do: "choose", arg: 2 })
    expect(dialogKeyAction(key("0"))).toBeNull()
    expect(dialogKeyAction(key("ArrowUp"))).toEqual({ do: "move", arg: -1 })
    expect(dialogKeyAction(key("ArrowDown"))).toEqual({ do: "move", arg: 1 })
    expect(dialogKeyAction(key("ArrowDown", { multiSelect: true }))).toBeNull()
    expect(dialogKeyAction(key("/"))).toEqual({ do: "type" })
    expect(dialogKeyAction(key("/", { canType: false }))).toBeNull()
    expect(dialogKeyAction(key("Enter"))).toEqual({ do: "next" })
    expect(dialogKeyAction(key("Enter", { onButton: true }))).toBeNull()
    expect(dialogKeyAction(key("ArrowLeft"))).toEqual({ do: "back" })
  })

  it("answers no question on the review", () => {
    expect(dialogKeyAction(key("1", { screen: "review" }))).toBeNull()
    expect(dialogKeyAction(key("/", { screen: "review" }))).toBeNull()
    expect(dialogKeyAction(key("Enter", { screen: "review" }))).toEqual({ do: "next" })
  })
})
