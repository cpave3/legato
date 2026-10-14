import { afterEach, describe, expect, it, vi } from "vitest"
import {
  NOTIFY, STUDIO_API, StudioError, addScene, agreePlan, chapterHeadings, lengthOf, moveScene, planLength, plannedMs, renameChapter, rewriteScene, writeBrief, clock, decideOffPlan, intoTake, openOffPlan, pointsHit, reviewRequest, timeline, transcriptRuns, fetchStudioProject, focusedScene, leftTheScript, lineUpRequest, planChanges, questionAboutScene, recordedCount, sceneSignature,
  studioAddress, talkingPoints, uploadTake, type StudioProject, type StudioScene,
} from "../agentStudio"

function scene(overrides: Partial<StudioScene> = {}): StudioScene {
  return { id: "intro", kind: "card", status: "waiting", still: "/api/scenes/intro/still.png?v=abc", takes: [], ...overrides }
}

function project(scenes: StudioScene[], overrides: Partial<StudioProject> = {}): StudioProject {
  return {
    api: 5, title: "Restack demo", storyboard: "/talks/restack/storyboard.json", face: "corner",
    problems: [], warnings: [], scenes, render: { running: false, log: [] }, ...overrides,
  }
}

function answer(status: number, body: unknown): Response {
  return new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } })
}

afterEach(() => vi.restoreAllMocks())

describe("the studio's address", () => {
  it("is under the item it belongs to, on the server the page talks to", () => {
    expect(studioAddress("ui-0123456789", "/api/project")).toBe("/api/ui-items/ui-0123456789/studio/api/project")
    // A still's address comes from the studio with its own version in it.
    expect(studioAddress("ui-0123456789", "/api/scenes/intro/still.png?v=abc")).toBe("/api/ui-items/ui-0123456789/studio/api/scenes/intro/still.png?v=abc")
  })
})

describe("fetchStudioProject", () => {
  it("reads the storyboard as the studio describes it", async () => {
    const fetched = vi.spyOn(globalThis, "fetch").mockResolvedValue(answer(200, project([scene()])))
    const got = await fetchStudioProject("http://legato.test:3080", "ui-0123456789")
    expect(got.scenes[0].id).toBe("intro")
    expect(fetched.mock.calls[0][0]).toBe("http://legato.test:3080/api/ui-items/ui-0123456789/studio/api/project")
  })

  it("says why there is no studio, in a way the panel can act on", async () => {
    vi.spyOn(globalThis, "fetch").mockResolvedValue(answer(503, { error: "libretto is not installed on this machine, so the studio cannot open.", code: "libretto_missing" }))
    const failure = await fetchStudioProject("", "ui-0123456789").catch((err) => err)
    expect(failure).toBeInstanceOf(StudioError)
    expect(failure.code).toBe("libretto_missing")
    expect(failure.message).toContain("libretto is not installed")
  })

  it("turns down a studio that speaks a newer language than this page", async () => {
    vi.spyOn(globalThis, "fetch").mockResolvedValue(answer(200, project([], { api: STUDIO_API + 1 })))
    const failure = await fetchStudioProject("", "ui-0123456789").catch((err) => err)
    expect(failure.code).toBe("legato_old")
    expect(failure.message).toContain("update Legato")
  })

  it("reports an answer that is not the studio's at all", async () => {
    vi.spyOn(globalThis, "fetch").mockResolvedValue(new Response("<html>bad gateway</html>", { status: 502 }))
    const failure = await fetchStudioProject("", "ui-0123456789").catch((err) => err)
    expect(failure.code).toBe("studio_failed")
  })
})

describe("uploadTake", () => {
  it("sends a take of sound alone as it is, with its own kind", async () => {
    const fetched = vi.spyOn(globalThis, "fetch").mockResolvedValue(answer(200, project([scene({ status: "recorded" })])))
    const sound = new Blob(["opus"], { type: "audio/webm;codecs=opus" })
    const after = await uploadTake("", "ui-0123456789", "intro", sound)
    expect(after.scenes[0].status).toBe("recorded")
    const [address, init] = fetched.mock.calls[0]
    expect(address).toBe("/api/ui-items/ui-0123456789/studio/api/scenes/intro/takes")
    expect(init?.method).toBe("POST")
    expect(init?.body).toBe(sound)
    expect((init?.headers as Record<string, string>)["Content-Type"]).toBe("audio/webm;codecs=opus")
  })

  it("sends a take with the camera as two recordings: the picture, and the sound by itself", async () => {
    const fetched = vi.spyOn(globalThis, "fetch").mockResolvedValue(answer(200, project([scene({ status: "recorded" })])))
    const sound = new Blob(["opus"], { type: "audio/webm;codecs=opus" })
    const picture = new Blob(["vp9 and opus"], { type: "video/webm;codecs=vp9,opus" })
    await uploadTake("", "ui-0123456789", "intro", sound, picture)
    const init = fetched.mock.calls[0][1]
    const form = init?.body as FormData
    expect(form).toBeInstanceOf(FormData)
    expect((form.get("take") as Blob).size).toBe(picture.size)
    expect((form.get("sound") as Blob).size).toBe(sound.size)
    // The browser names the form's kind itself, with the boundary in it.
    expect((init?.headers as Record<string, string>)["Content-Type"]).toBeUndefined()
  })
})

describe("what the person said against what was planned", () => {
  it("counts the scenes that are theirs to record", () => {
    const p = project([scene({ status: "recorded" }), scene({ id: "steps" }), scene({ id: "outro", status: "voice" }), scene({ id: "pause", status: "silent" })])
    expect(recordedCount(p)).toEqual({ recorded: 1, toRecord: 2 })
  })

  it("sees a take that left its script, and only then", () => {
    expect(leftTheScript(scene({ status: "recorded", match: { found: 3, total: 40 } }))).toBe(true)
    expect(leftTheScript(scene({ status: "recorded", match: { found: 38, total: 40 } }))).toBe(false)
    // No script, nothing to leave.
    expect(leftTheScript(scene({ status: "recorded" }))).toBe(false)
    expect(leftTheScript(scene({ match: { found: 0, total: 0 } }))).toBe(false)
  })
})

describe("questionAboutScene", () => {
  it("gives the agent the scene the person is looking at, and leaves room for what they think", () => {
    const text = questionAboutScene("Restack demo", project([]), scene({ id: "steps", kind: "terminal", chapter: "How it goes", notes: "Run restack and show the log." }))
    expect(text).toContain(`About the scene "steps" in the studio "Restack demo"`)
    expect(text).toContain("/talks/restack/storyboard.json")
    expect(text).toContain("a terminal")
    expect(text).toContain("Talking points: Run restack and show the log.")
    expect(text.endsWith("What I think: ")).toBe(true)
  })

  it("says what the scene says once it has words", () => {
    const text = questionAboutScene("Demo", project([]), scene({ status: "recorded", says: "So here we restack.", heard: "So here we restack." }))
    expect(text).toContain("Recorded. I said: So here we restack.")
  })
})

describe("lineUpRequest", () => {
  it("asks the agent to follow what was said, and names what was recorded", () => {
    const text = lineUpRequest("Restack demo", project([
      scene({ id: "intro", status: "recorded", take: { src: "takes/intro-01.webm", url: "/x" } }),
      scene({ id: "steps", status: "recorded", take: { src: "takes/steps-02.webm", url: "/y" }, match: { found: 4, total: 30 } }),
      scene({ id: "outro" }),
    ]))
    expect(text).toContain(`I have recorded "Restack demo"`)
    expect(text).toContain("/talks/restack/storyboard.json")
    expect(text).toContain("intro (takes/intro-01.webm)")
    expect(text).toContain("steps (takes/steps-02.webm)")
    expect(text).toContain("Not recorded yet: outro")
    // Leaving the script is direction, and the request says so.
    expect(text).toContain("I went my own way in: steps")
    expect(text).toContain("follow what I said")
    expect(text).toContain("libretto transcribe")
  })

  it("leaves out what does not apply", () => {
    const text = lineUpRequest("Demo", project([scene({ status: "recorded", take: { src: "takes/intro-01.webm", url: "/x" } })]))
    expect(text).not.toContain("Not recorded yet")
    expect(text).not.toContain("went my own way")
  })
})

describe("talkingPoints", () => {
  it("reads notes as the points a person speaks to, not as text to read out", () => {
    expect(talkingPoints("- Restack rebases every branch above\n- One command, no conflicts by hand\n\n* It is undoable")).toEqual([
      "Restack rebases every branch above", "One command, no conflicts by hand", "It is undoable",
    ])
    expect(talkingPoints("1. First\n2) Second\n• Third")).toEqual(["First", "Second", "Third"])
  })

  it("keeps notes written as a sentence as one point", () => {
    expect(talkingPoints("Say why it matters.")).toEqual(["Say why it matters."])
    expect(talkingPoints(undefined)).toEqual([])
    expect(talkingPoints("  \n")).toEqual([])
  })
})

describe("planChanges", () => {
  const before = (scenes: StudioScene[]) => new Map(scenes.map((s) => [s.id, sceneSignature(s)]))

  it("says what the agent added, removed and changed since the person last looked", () => {
    const was = [scene({ id: "intro" }), scene({ id: "steps", notes: "- Old point" }), scene({ id: "bar" })]
    const now = [scene({ id: "intro" }), scene({ id: "steps", notes: "- New point" }), scene({ id: "sync" }), scene({ id: "outro" })]
    const diff = planChanges(before(was), now)
    expect(diff.marks).toEqual({ steps: "changed", sync: "new", outro: "new" })
    expect(diff.removed).toEqual(["bar"])
    expect(diff.summary).toBe("added sync and outro; removed bar; changed steps.")
  })

  it("does not call a take a change of plan", () => {
    const was = [scene({ id: "intro", notes: "- Say hello" })]
    const now = [scene({ id: "intro", notes: "- Say hello", status: "recorded", heard: "Hello.", says: "Hello.", take: { src: "takes/intro-01.webm", url: "/x" } })]
    expect(planChanges(before(was), now)).toEqual({ marks: {}, removed: [], summary: "" })
  })

  it("sees a new picture as a change", () => {
    const diff = planChanges(before([scene({ id: "intro", still: "/api/scenes/intro/still.png?v=1" })]), [scene({ id: "intro", still: "/api/scenes/intro/still.png?v=2" })])
    expect(diff.marks).toEqual({ intro: "changed" })
    expect(diff.summary).toBe("changed intro.")
  })
})

describe("focusedScene", () => {
  it("reads the scene the agent put in front of the person", () => {
    expect(focusedScene(JSON.stringify({ storyboard: "/talks/storyboard.json", scene: "sync" }))).toBe("sync")
    expect(focusedScene(JSON.stringify({ storyboard: "/talks/storyboard.json" }))).toBeNull()
    expect(focusedScene("not json")).toBeNull()
    expect(focusedScene(undefined)).toBeNull()
  })
})

describe("chapterHeadings", () => {
  it("heads a scene with its chapter only where the chapter starts", () => {
    expect(chapterHeadings([
      scene({ id: "a", chapter: "Start" }), scene({ id: "b" }), scene({ id: "c", chapter: "Start" }), scene({ id: "d", chapter: "Restack" }), scene({ id: "e" }),
    ])).toEqual(["Start", "", "", "Restack", ""])
  })
})

describe("a take as the agent reviewed it", () => {
  const words = (...list: [string, number, number][]) => list.map(([text, startMs, endMs]) => ({ text, startMs, endMs }))
  const said = (overrides: Partial<StudioScene> = {}): StudioScene => scene({
    id: "align", status: "recorded", notes: "- Speak freely\n- What you said is the truth\n- The agent fixes it",
    take: { src: "takes/align-02.webm", url: "/t", startMs: 1000, endMs: 20000, current: true },
    transcript: words(["You", 1200, 1400], ["can", 1400, 1600], ["speak", 1600, 2000], ["freely.", 2000, 2500],
      ["Oh,", 4000, 4300], ["and", 4300, 4500], ["sync.", 4500, 5000], ["Then", 5100, 5400], ["it", 5400, 5500], ["is", 5500, 5600], ["fixed.", 5600, 6000]),
    ...overrides,
  })
  const reviewed = { points: [{ point: 1, from: 1600, to: 2500 }, { point: 3, from: 5100, to: 6000 }, { point: 9, from: 0, to: 10 }], offPlan: [{ from: 4000, to: 5000, about: "sync", note: "You went into sync." }] }

  it("draws the transcript as runs, with what covers a talking point and what left the plan", () => {
    const runs = transcriptRuns(said({ review: reviewed }))
    expect(runs.map((run) => [run.text, run.point, run.offPlan])).toEqual([
      ["You can", undefined, undefined],
      ["speak freely.", 1, undefined],
      ["Oh, and sync.", undefined, 1],
      ["Then it is fixed.", 3, undefined],
    ])
    // Each sentence says when it is said, from the start of the scene's part.
    expect(runs.map((run) => run.at)).toEqual([200, undefined, 3000, 4100])
  })

  it("is plain words until the agent has looked", () => {
    expect(transcriptRuns(said()).map((run) => run.text)).toEqual(["You can speak freely.", "Oh, and sync.", "Then it is fixed."])
    expect(transcriptRuns(scene())).toEqual([])
  })

  it("stops marking a stretch once the person has decided about it", () => {
    const decided = { ...reviewed, offPlan: [{ ...reviewed.offPlan[0], decision: "keep" as const }] }
    expect(openOffPlan(said({ review: reviewed }))).toEqual([{ place: 1, part: reviewed.offPlan[0] }])
    expect(openOffPlan(said({ review: decided }))).toEqual([])
    expect(transcriptRuns(said({ review: decided })).some((run) => run.offPlan)).toBe(false)
  })

  it("counts the talking points the take covers, and no point the scene does not have", () => {
    expect(pointsHit(said({ review: reviewed }))).toEqual([1, 3])
    expect(pointsHit(said())).toEqual([])
  })

  it("times a word by the scene's own sound, which leaves out what was cut", () => {
    const take = { src: "t", url: "/t", startMs: 1000, endMs: 20000, skip: [[2000, 3500]] }
    expect(intoTake(take, 1500)).toBe(500)
    expect(intoTake(take, 5000)).toBe(2500)
    expect(intoTake(undefined, 700)).toBe(700)
  })

  it("lays the scenes out as wide as they are long", () => {
    const blocks = timeline(project([
      scene({ id: "a", status: "recorded", durationMs: 18000 }),
      said({ durationMs: 41000, review: reviewed }),
      scene({ id: "c", status: "waiting" }),
    ]))
    expect(blocks.map((block) => [block.id, block.weight, block.decide])).toEqual([["a", 18000, false], ["align", 41000, true], ["c", 14000, false]])
    expect(clock(278000)).toBe("4:38")
    expect(clock(41400)).toBe("0:41")
  })

  it("asks Legato to tell the agent, when the person allows it", async () => {
    const fetched = vi.spyOn(globalThis, "fetch").mockImplementation(async () => answer(200, project([])))
    const header = (n: number) => new Headers(fetched.mock.calls[n][1]?.headers).get(NOTIFY)
    await uploadTake("", "ui-0123456789", "align", new Blob(["x"], { type: "audio/wav" }), undefined, true)
    await uploadTake("", "ui-0123456789", "align", new Blob(["x"], { type: "audio/wav" }))
    // Only a new scene is the agent's to make; a cut or a keep needs nobody.
    await decideOffPlan("", "ui-0123456789", "align", 2, "add", true)
    await decideOffPlan("", "ui-0123456789", "align", 2, "cut", true)
    expect([header(0), header(1), header(2), header(3)]).toEqual(["1", null, "1", null])
    expect(String(fetched.mock.calls[2][0])).toContain("/api/ui-items/ui-0123456789/studio/api/scenes/align/offplan/2")
    expect(JSON.parse(String(fetched.mock.calls[2][1]?.body))).toEqual({ decision: "add" })
  })

  it("words a request for a review, for when the agent was not told", () => {
    const text = reviewRequest("Restack demo", project([]), said())
    expect(text).toContain('the scene "align"')
    expect(text).toContain("takes/align-02.webm")
    expect(text).toContain("libretto review")
  })
})

describe("the plan before it is recorded", () => {
  it("guesses how long a scene will run from how much it is to say", () => {
    const three = scene({ notes: "- One\n- Two\n- Three" })
    expect(plannedMs(three)).toBe(34000)
    expect(plannedMs(scene())).toBe(14000)
    // A scene that has been said is as long as it is.
    expect(lengthOf(scene({ status: "recorded", durationMs: 41000 }))).toEqual({ ms: 41000, sure: true })
    expect(lengthOf(three)).toEqual({ ms: 34000, sure: false })
    expect(planLength(project([three, scene({ id: "b", status: "recorded", durationMs: 41000 })]))).toEqual({ ms: 75000, sure: false })
    expect(planLength(project([scene({ id: "b", status: "recorded", durationMs: 41000 })]))).toEqual({ ms: 41000, sure: true })
  })

  it("sends each change of the plan to the studio, and has the agent told only of what is the agent's to do", async () => {
    const fetched = vi.spyOn(globalThis, "fetch").mockImplementation(async () => answer(200, project([])))
    const sent = (n: number) => {
      const [address, init] = fetched.mock.calls[n]
      return { method: init?.method, path: String(address).split("/studio")[1], body: init?.body ? JSON.parse(String(init.body)) : undefined, told: new Headers(init?.headers).get(NOTIFY) }
    }
    const planned = scene({ id: "demo", kind: "planned", planned: true, shows: "A terminal" })
    const drawn = scene({ id: "sync", kind: "card", shows: "A card about sync" })

    await writeBrief("", "ui-1", "How restack works", true)
    await rewriteScene("", "ui-1", planned, { notes: "- One command", shows: "A terminal, then the log" }, true)
    await rewriteScene("", "ui-1", drawn, { shows: "A diagram" }, true)
    await rewriteScene("", "ui-1", drawn, { notes: "- Other" }, true)
    await moveScene("", "ui-1", "demo", -1)
    await renameChapter("", "ui-1", "More", "Going further")
    await addScene("", "ui-1", "What happens on a conflict", "demo", true)
    await agreePlan("", "ui-1", true)

    expect(sent(0)).toEqual({ method: "POST", path: "/api/brief", body: { text: "How restack works" }, told: "1" })
    // A planned scene is rewritten by hand; nothing is drawn yet to redo.
    expect(sent(1)).toEqual({ method: "PATCH", path: "/api/scenes/demo", body: { notes: "- One command", shows: "A terminal, then the log" }, told: null })
    // A drawn scene's picture is the agent's to change.
    expect(sent(2)).toEqual({ method: "PATCH", path: "/api/scenes/sync", body: { shows: "A diagram" }, told: "1" })
    expect(sent(3).told).toBeNull()
    expect(sent(4)).toEqual({ method: "POST", path: "/api/scenes/demo/move", body: { by: -1 }, told: null })
    expect(sent(5)).toEqual({ method: "POST", path: "/api/chapters", body: { from: "More", to: "Going further" }, told: null })
    expect(sent(6)).toEqual({ method: "POST", path: "/api/scenes", body: { shows: "What happens on a conflict", after: "demo" }, told: "1" })
    expect(sent(7)).toEqual({ method: "POST", path: "/api/agree", body: undefined, told: "1" })
  })
})
