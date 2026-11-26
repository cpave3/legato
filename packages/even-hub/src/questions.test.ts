import { describe, expect, it } from "vitest"
import { getTextWidth, measureTextWrap } from "@evenrealities/pretext"
import { BORDER, CANVAS_WIDTH, FRAME_WIDTH, inner } from "./layout"
import type { Approval } from "./legato"
import { images, twoParts } from "./questions.fixtures"
import {
  ASK_LINES,
  QUESTION_LINES,
  answerOf,
  askedQuestions,
  cardsOf,
  gonePage,
  noQuestionsPage,
  optionPage,
  ownAnswerPage,
  questionPage,
  readingPage,
  sendPage,
  type Asked,
} from "./questions"

const permission: Approval = { id: "ap-p", source: "agent_permission", task_id: "LEG-150", title: "Bash: git push", options: [{ id: "once", label: "Allow once", kind: "allow" }] }

const who = "LEG-157 Plans: show image files beside the Markdown"
const [image] = askedQuestions([images])
const [poll, bg] = askedQuestions([twoParts])

describe("askedQuestions", () => {
  it("lists every question of the agents' question requests in order, one each, with its part", () => {
    const asked = askedQuestions([images, permission, twoParts])
    expect(asked.map((a) => [a.approval.id, a.question.id, a.part, a.parts])).toEqual([
      ["ap-q1", "q1", 0, 1],
      ["ap-q2", "poll", 0, 2],
      ["ap-q2", "bg", 1, 2],
    ])
  })

  it("leaves out questions it could not answer: no ID, no text, or options that are not labels", () => {
    const odd: Approval = {
      ...images,
      details: {
        questions: [
          { question: "No ID?", options: [] },
          { id: "x", options: [] },
          { id: "y", question: "Bad options", options: [{ description: "no label" }] },
          "not a question",
        ],
      },
    }
    expect(askedQuestions([odd, { ...images, id: "none", details: {} }])).toEqual([])
  })
})

describe("answerOf", () => {
  it("gives the option chosen, or the operator's own words in its place", () => {
    expect(answerOf(image.question, { chosen: ["In a shared cache"], other: "" })).toEqual(["In a shared cache"])
    expect(answerOf(image.question, { chosen: ["In a shared cache"], other: " In the data folder " })).toEqual(["In the data folder"])
    expect(answerOf(image.question, undefined)).toEqual([])
  })

  it("adds the operator's own words to the options ticked when any may be chosen", () => {
    expect(answerOf(bg.question, { chosen: ["Who needs you", "Time"], other: "The weather" })).toEqual(["Who needs you", "Time", "The weather"])
  })
})

describe("cardsOf", () => {
  it("is the options, then the operator's own answer, then Done when any may be chosen", () => {
    expect(cardsOf(image.question).map((c) => c.kind)).toEqual(["option", "option", "option", "other"])
    expect(cardsOf(bg.question).map((c) => c.kind)).toEqual(["option", "option", "option", "option", "other", "done"])
  })

  it("leaves out the operator's own answer when the agent does not allow one", () => {
    expect(cardsOf({ ...image.question, allowOther: false }).map((c) => c.kind)).toEqual(["option", "option", "option"])
  })
})

describe("questionPage", () => {
  it("shows the question framed, where it is among those waiting, and the facts dim under it", () => {
    const page = questionPage(image, who, 0, 4)
    expect(page.badge).toBe("● question")
    expect(page.title.startsWith("LEG-157 Plans")).toBe(true)
    expect(page.count.trim()).toBe("1 of 4")
    expect(page.question).toBe("Where should Legato keep its copies of a plan's image files?")
    expect(page.facts).toBe("Image files · 3 options or your own")
    expect(page.hint.trim()).toBe("Swipe: questions · Tap: answer · Double tap: back")
  })

  it("says nothing of swiping when only one question waits", () => {
    expect(questionPage(image, who, 0, 1).hint.trim()).toBe("Tap: answer · Double tap: back")
  })

  it("says which part of a request it is, and that any may be chosen", () => {
    expect(questionPage(poll, who, 1, 4).facts).toBe("Polling · part 1 of 2 · 3 options or your own")
    expect(questionPage(bg, who, 2, 4).facts).toBe("Background · part 2 of 2 · 4 options or your own · choose any")
    const closed: Asked = { ...image, question: { ...image.question, allowOther: false, header: undefined } }
    expect(questionPage(closed, who, 0, 1).facts).toBe("3 options")
  })

  it("shows the answer already chosen for it in place of the options", () => {
    expect(questionPage(poll, who, 1, 4, { chosen: ["Every 5 seconds"], other: "" }).facts).toBe("Polling · part 1 of 2 · your answer: Every 5 seconds")
  })

  it("cuts a long question to the lines its frame holds, so the frame never scrolls and every swipe moves on", () => {
    const long: Asked = { ...image, question: { ...image.question, question: "Should the check fail the pull request? ".repeat(20).trim() } }
    const { question } = questionPage(long, who, 0, 1)
    expect(question.endsWith("…")).toBe(true)
    expect(measureTextWrap(question, inner(FRAME_WIDTH, BORDER)).lineCount).toBe(QUESTION_LINES)
  })
})

describe("optionPage", () => {
  const none = { chosen: [], other: "" }

  it("shows one option: the question dim above, the label framed with its place, its description under it", () => {
    const page = optionPage(image, who, 0, none)
    expect(page.badge).toBe("● answer?")
    expect(page.ask).toBe("Where should Legato keep its copies of a plan's image files?")
    expect(page.option.startsWith("★ Beside the plan ")).toBe(true)
    expect(page.option.endsWith("1/4")).toBe(true)
    expect(page.description).toBe("In the plan's own folder, so the files move and are removed with it.")
    expect(page.hint.trim()).toBe("Swipe: options · Tap: choose · Double tap: back")

    const third = optionPage(image, who, 2, none)
    expect(third.option.startsWith("Leave them where they are ")).toBe(true)
    expect(third.description).toBe("")
  })

  it("keeps the label on one line, so its frame never scrolls", () => {
    const wordy: Asked = { ...image, question: { ...image.question, options: [{ label: "A very long option label that goes on and on past the edge of the glasses" }] } }
    const { option } = optionPage(wordy, who, 0, none)
    expect(getTextWidth(option)).toBeLessThanOrEqual(inner(FRAME_WIDTH, BORDER))
    expect(option).toContain("…")
    expect(option.endsWith("1/2")).toBe(true)
  })

  it("cuts the question above to its lines", () => {
    const long: Asked = { ...image, question: { ...image.question, question: "Which one? ".repeat(40).trim() } }
    expect(measureTextWrap(optionPage(long, who, 0, none).ask, inner(FRAME_WIDTH)).lineCount).toBe(ASK_LINES)
  })

  it("marks the option chosen before, when the operator comes back to it", () => {
    expect(optionPage(image, who, 1, { chosen: ["In a shared cache"], other: "" }).option.startsWith("● In a shared cache")).toBe(true)
  })

  it("ticks options when any may be chosen, and says so", () => {
    const ticked = optionPage(bg, who, 1, { chosen: ["Agent counts"], other: "" })
    expect(ticked.badge).toBe("● choose any")
    expect(ticked.option.startsWith("■ Agent counts")).toBe(true)
    expect(ticked.option.endsWith("2/6")).toBe(true)
    expect(ticked.hint.trim()).toBe("Swipe: options · Tap: tick · Double tap: back")
    expect(optionPage(bg, who, 0, { chosen: ["Agent counts"], other: "" }).option.startsWith("□ Who needs you")).toBe(true)
  })

  it("offers the operator's own answer last, by holding", () => {
    const page = optionPage(image, who, 3, none)
    expect(page.option.startsWith("Your own answer…")).toBe(true)
    expect(page.description).toBe("Hold the touchpad and say it. You see the words before anything is sent.")
    expect(page.hint.trim()).toBe("Swipe: options · Hold: say it · Double tap: back")
    const own = optionPage(image, who, 3, { chosen: [], other: "In the data folder" })
    expect(own.description).toBe("Yours: “In the data folder”. Tap to drop it, or hold to say it again.")
    expect(own.hint.trim()).toBe("Tap: drop it · Hold: say again · Double tap: back")
  })

  it("keeps every hint on the line at the foot", () => {
    const drafts = [none, { chosen: ["Who needs you"], other: "The weather" }]
    for (const draft of drafts) {
      for (let card = 0; card < 6; card++) expect(getTextWidth(optionPage(bg, who, card, draft).hint)).toBeLessThanOrEqual(CANVAS_WIDTH)
    }
  })

  it("ends a choose-any question with Done, which says what is ticked", () => {
    const done = optionPage(bg, who, 5, { chosen: ["Who needs you", "Agent counts"], other: "" })
    expect(done.option.startsWith("Done · 2 ticked")).toBe(true)
    expect(done.description).toBe("Who needs you, Agent counts")
    expect(done.hint.trim()).toBe("Swipe: options · Tap: done · Double tap: back")

    const empty = optionPage(bg, who, 5, none)
    expect(empty.option.startsWith("Done · none ticked")).toBe(true)
    expect(empty.description).toBe("Tick one first, or say your own.")
    expect(empty.hint.trim()).toBe("Swipe: options · Double tap: back")
  })
})

describe("ownAnswerPage", () => {
  it("listens with the question in view, then shows the words to use or say again", () => {
    const listening = ownAnswerPage(image, who, { step: "listening", seconds: 4 })
    expect(listening.badge).toBe("● Listening")
    expect(listening.body).toBe("Say your answer to:\nWhere should Legato keep its copies of a plan's image files?\n\n0:04")
    expect(listening.hint.trim()).toBe("Release: stop · Double tap: cancel")

    expect(ownAnswerPage(image, who, { step: "transcribing" })).toMatchObject({ badge: "Your answer", body: "Transcribing…" })
    const review = ownAnswerPage(image, who, { step: "review", text: "In the data folder." })
    expect(review).toMatchObject({ badge: "Your answer", body: "“In the data folder.”" })
    expect(review.hint.trim()).toBe("Tap: use it · Hold: say it again · Double tap: back")

    const problem = ownAnswerPage(image, who, { step: "problem", problem: "Heard nothing." })
    expect(problem).toMatchObject({ badge: "▲ Problem", body: "Heard nothing." })
    expect(problem.hint.trim()).toBe("Hold: try again · Double tap: back")
  })
})

describe("sendPage", () => {
  const chosen = new Map([["q1", { chosen: ["Beside the plan (Recommended)"], other: "" }]])

  it("asks to send one answer, the answer first, then what it answers", () => {
    const page = sendPage(images, who, chosen, { step: "confirm" })
    expect(page.badge).toBe("Send?")
    expect(page.body).toBe("Beside the plan\n\nWhere should Legato keep its copies of a plan's image files?")
    expect(page.hint.trim()).toBe("Tap: send · Double tap: back")
  })

  it("reviews every part of a request on one page, sent together", () => {
    const drafts = new Map([
      ["poll", { chosen: ["Every 5 seconds"], other: "" }],
      ["bg", { chosen: ["Who needs you", "Agent counts"], other: "" }],
    ])
    expect(sendPage(twoParts, who, drafts, { step: "confirm" }).body).toBe(
      "Polling: Every 5 seconds\nBackground: Who needs you, Agent counts\n\nBoth answers go to LEG-163 together.",
    )
  })

  it("says it is sending, then that it was sent and what is next", () => {
    expect(sendPage(images, who, chosen, { step: "sending" })).toMatchObject({ badge: "Sending", hint: "" })
    expect(sendPage(images, who, chosen, { step: "sent" }).body).toBe("Answered LEG-157.")
    expect(sendPage(images, who, chosen, { step: "sent", next: poll }).body).toBe(
      "Answered LEG-157.\n\nNext, LEG-163 asks: How often should the glasses read Legato while the screen is on?",
    )
  })

  it("keeps the answer when it was not sent, and offers to send it again", () => {
    const page = sendPage(images, who, chosen, { step: "problem", problem: "The Legato app took too long to answer" })
    expect(page.badge).toBe("▲ Not sent")
    expect(page.body).toBe("The Legato app took too long to answer\n\nYour answer is kept: Beside the plan")
    expect(page.hint.trim()).toBe("Tap: send again · Double tap: back")
  })
})

describe("the other pages", () => {
  it("says a question that left while open was not answered from here", () => {
    const page = gonePage(who, true)
    expect(page.badge).toBe("○ gone")
    expect(page.body).toBe("No longer waiting.\n\nIt was answered elsewhere, or its agent moved on. Nothing was sent.")
    expect(page.hint.trim()).toBe("Tap: next question · Double tap: back")
    expect(gonePage(who, false).hint.trim()).toBe("Double tap: back")
  })

  it("says when no agent asks anything", () => {
    const page = noQuestionsPage("Desk")
    expect(page.badge).toBe("○ questions")
    expect(page.body).toBe("No agent is asking you anything.\n\nNew questions show here and on the Active tab.")
    expect(page.hint.trim()).toBe("Double tap: back")
  })

  it("says it is reading the question", () => {
    expect(readingPage(who)).toMatchObject({ badge: "● question", body: "Reading the question…" })
    const failed = readingPage(who, "The Legato app took too long to answer")
    expect(failed).toMatchObject({ badge: "▲ Problem", body: "Can't read the question: The Legato app took too long to answer" })
    expect(failed.hint.trim()).toBe("Tap: try again · Double tap: back")
  })

  it("fits every header's title in its box", () => {
    const long = "LEG-157 Plans: show image files beside the Markdown, and much more besides that"
    expect(questionPage(image, long, 0, 1).title.endsWith("…")).toBe(true)
  })
})
