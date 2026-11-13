import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { OsEventTypeList, StartUpPageCreateResult, type EvenHubEvent } from "@evenrealities/even_hub_sdk"
import { startGlance, type GlassesBridge, type LegatoClient, POLL_MS } from "./app"
import type { Agent, AgentsResult, Conversation } from "./legato"

const agent = (task_id: string, activity: string, title = ""): Agent => ({
  task_id,
  title,
  activity,
  status: "running",
  sandboxed: false,
  started_at: "2026-11-13T07:00:00Z",
})

type Page = {
  textObject?: { containerName?: string; content?: string }[]
  listObject?: { containerName?: string; itemContainer?: { itemName?: string[] } }[]
}

// A bridge that keeps what the app drew, as the glasses would show it.
function fakeBridge() {
  let shown = new Map<string, string | string[]>()
  let listener: ((e: EvenHubEvent) => void) | undefined
  const draw = (page: Page) => {
    shown = new Map()
    for (const t of page.textObject ?? []) shown.set(t.containerName!, t.content ?? "")
    for (const l of page.listObject ?? []) shown.set(l.containerName!, l.itemContainer?.itemName ?? [])
  }
  const bridge = {
    created: 0,
    rebuilt: 0,
    upgrades: 0,
    exits: [] as number[],
    mic: [] as boolean[],
    async createStartUpPageContainer(page: Page) {
      bridge.created++
      draw(page)
      return StartUpPageCreateResult.success
    },
    refuseRebuild: false,
    failUpgrades: 0,
    async rebuildPageContainer(page: Page) {
      if (bridge.refuseRebuild) return false
      bridge.rebuilt++
      draw(page)
      return true
    },
    async textContainerUpgrade(u: { containerName?: string; content?: string }) {
      if (bridge.failUpgrades > 0) {
        bridge.failUpgrades--
        throw new Error("bluetooth dropped")
      }
      bridge.upgrades++
      shown.set(u.containerName!, u.content ?? "")
      return true
    },
    micWorks: true,
    async audioControl(open: boolean) {
      bridge.mic.push(open)
      return open ? bridge.micWorks : true
    },
    onEvenHubEvent(cb: (e: EvenHubEvent) => void) {
      listener = cb
      return () => (listener = undefined)
    },
    async shutDownPageContainer(mode?: number) {
      bridge.exits.push(mode ?? 0)
      return true
    },
    send(e: EvenHubEvent) {
      listener?.(e)
    },
    text(name: string) {
      return shown.get(name) as string | undefined
    },
    items() {
      return shown.get("agents") as string[] | undefined
    },
    feed() {
      return shown.get("feed") as string[] | undefined
    },
  }
  return bridge
}

function fakeLegato(...answers: AgentsResult[]) {
  const calls = { agents: 0, conversation: [] as string[], transcribe: [] as number[][], send: [] as [string, string][] }
  const conversation: Conversation = {
    task_id: "",
    status: "running",
    entries: [{ kind: "assistant", text: "Working on it." }],
    permission: null,
    question: null,
    context_used: 0,
    context_window: 0,
  }
  const client: LegatoClient = {
    agents: async () => answers[Math.min(calls.agents++, answers.length - 1)],
    conversation: async (taskId) => {
      calls.conversation.push(taskId)
      return { ok: true, conversation }
    },
    transcribe: async (pcm) => {
      calls.transcribe.push([...pcm])
      return { ok: true, text: "Run the tests again." }
    },
    send: async (taskId, text) => {
      calls.send.push([taskId, text])
      return { ok: true }
    },
  }
  return { client, calls, conversation }
}

const click = { sysEvent: {} } as EvenHubEvent
const doubleClick = { sysEvent: { eventType: OsEventTypeList.DOUBLE_CLICK_EVENT } } as EvenHubEvent
const hold = { sysEvent: { eventType: OsEventTypeList.LONG_PRESS_EVENT } } as EvenHubEvent
const release = { sysEvent: { eventType: OsEventTypeList.LONG_PRESS_RELEASE_EVENT } } as EvenHubEvent
const pick = (index: number, name: string): EvenHubEvent => ({
  listEvent: { containerID: 3, containerName: "feed", currentSelectItemIndex: index, currentSelectItemName: name },
} as EvenHubEvent)
const select = (index: number, name: string): EvenHubEvent => ({
  listEvent: { containerID: 5, containerName: "agents", currentSelectItemIndex: index, currentSelectItemName: name },
} as EvenHubEvent)
const system = (eventType: OsEventTypeList): EvenHubEvent => ({ sysEvent: { eventType } }) as EvenHubEvent
const audio = (...bytes: number[]): EvenHubEvent => ({ audioEvent: { audioPcm: new Uint8Array(bytes) } }) as EvenHubEvent

const settle = () => vi.advanceTimersByTimeAsync(0)

const someAgents: AgentsResult = {
  ok: true,
  agents: [agent("RUN-1", "working", "Build"), agent("ASK-1", "waiting", "Approve"), agent("IDLE-1", "", "Rest")],
}

describe("startGlance", () => {
  beforeEach(() => {
    vi.useFakeTimers()
    vi.setSystemTime(new Date(2026, 10, 13, 9, 5))
  })
  afterEach(() => vi.useRealTimers())

  it("draws the page once, then the active agents with the way to the idle ones", async () => {
    const bridge = fakeBridge()
    await startGlance(bridge as unknown as GlassesBridge, fakeLegato(someAgents).client)

    expect(bridge.created).toBe(1)
    expect(bridge.text("tab-active")).toBe("Active 2")
    expect(bridge.text("tab-idle")).toBe("Idle 1")
    expect(bridge.text("clock")).toBe("09:05")
    expect(bridge.items()).toEqual(["● needs you  ASK-1 Approve", "▶ running  RUN-1 Build", "Idle agents (1) →"])
  })

  it("goes to the idle tab from its item, back on a double tap, and asks to exit from the active tab", async () => {
    const bridge = fakeBridge()
    await startGlance(bridge as unknown as GlassesBridge, fakeLegato(someAgents).client)

    bridge.send(select(2, "Idle agents (1) →"))
    await settle()
    expect(bridge.items()).toEqual(["○ IDLE-1 Rest", "← Active agents (2)"])

    bridge.send(doubleClick)
    await settle()
    expect(bridge.items()?.[0]).toBe("● needs you  ASK-1 Approve")
    expect(bridge.exits).toEqual([])

    bridge.send(doubleClick)
    await settle()
    expect(bridge.exits).toEqual([1])
  })

  it("redraws the list only when its items change, and updates the clock in place", async () => {
    const changed: AgentsResult = { ok: true, agents: [agent("RUN-1", "waiting", "Build")] }
    const bridge = fakeBridge()
    const legato = fakeLegato(someAgents, someAgents, changed)
    await startGlance(bridge as unknown as GlassesBridge, legato.client)
    const rebuilt = bridge.rebuilt

    vi.setSystemTime(new Date(2026, 10, 13, 9, 6))
    await vi.advanceTimersByTimeAsync(POLL_MS)
    expect(bridge.rebuilt).toBe(rebuilt)
    expect(bridge.text("clock")).toBe("09:06")

    await vi.advanceTimersByTimeAsync(POLL_MS)
    expect(bridge.rebuilt).toBe(rebuilt + 1)
    expect(bridge.items()).toEqual(["● needs you  RUN-1 Build"])
  })

  it("keeps the last list and says why when a read fails", async () => {
    const bridge = fakeBridge()
    await startGlance(bridge as unknown as GlassesBridge, fakeLegato(someAgents, { ok: false, problem: "Can't reach Legato" }).client)

    await vi.advanceTimersByTimeAsync(POLL_MS)

    expect(bridge.text("status")).toBe("Can't reach Legato")
    expect(bridge.items()?.[0]).toBe("● needs you  ASK-1 Approve")
  })

  it("opens an agent on a tap: its state in a badge, its feed kept fresh, until a double tap goes back", async () => {
    const bridge = fakeBridge()
    const legato = fakeLegato(someAgents)
    await startGlance(bridge as unknown as GlassesBridge, legato.client)

    bridge.send(select(0, "● needs you  ASK-1 Approve"))
    await settle()
    expect(bridge.text("badge")).toBe("● needs you")
    expect(bridge.text("title")).toBe("ASK-1 Approve")
    expect(bridge.feed()).toEqual(["Agent: Working on it."])
    expect(legato.calls.conversation).toEqual(["ASK-1"])

    legato.conversation.entries.push({ kind: "assistant", text: "Done." })
    await vi.advanceTimersByTimeAsync(POLL_MS)
    expect(bridge.feed()).toEqual(["Agent: Done.", "Agent: Working on it."])

    bridge.send(doubleClick)
    await settle()
    expect(bridge.items()?.[0]).toBe("● needs you  ASK-1 Approve")
    expect(bridge.exits).toEqual([])
  })

  it("opens one entry of the feed in full on a tap, and goes back to the feed on a double tap", async () => {
    const bridge = fakeBridge()
    const legato = fakeLegato(someAgents)
    legato.conversation.entries.push({ kind: "tool", text: "Run the tests", tool: "Bash", command: "pnpm test", state: "failed" })
    await startGlance(bridge as unknown as GlassesBridge, legato.client)
    bridge.send(select(1, "▶ running  RUN-1 Build"))
    await settle()

    bridge.send(pick(0, "▲ Bash: Run the tests"))
    await settle()
    expect(bridge.text("body")).toBe("Bash (failed)\n\nRun the tests\n\npnpm test")
    expect(bridge.text("badge")).toBe("▶ running")

    bridge.send(doubleClick)
    await settle()
    expect(bridge.feed()).toEqual(["▲ Bash: Run the tests", "Agent: Working on it."])
  })

  it("dictates from an opened entry too", async () => {
    const bridge = fakeBridge()
    await startGlance(bridge as unknown as GlassesBridge, fakeLegato(someAgents).client)
    bridge.send(select(1, "▶ running  RUN-1 Build"))
    await settle()
    bridge.send(pick(0, "Agent: Working on it."))
    await settle()

    bridge.send(hold)
    await settle()

    expect(bridge.mic).toEqual([true])
    expect(bridge.text("badge")).toBe("● Listening")
  })

  it("takes a dictated message: listens while held, transcribes on release, shows it, and sends it on a tap", async () => {
    const bridge = fakeBridge()
    const legato = fakeLegato(someAgents)
    await startGlance(bridge as unknown as GlassesBridge, legato.client)
    bridge.send(select(1, "▶ running  RUN-1 Build"))
    await settle()

    bridge.send(hold)
    await settle()
    expect(bridge.mic).toEqual([true])
    expect(bridge.text("badge")).toBe("● Listening")

    bridge.send(audio(1, 2))
    bridge.send(audio(3))
    await vi.advanceTimersByTimeAsync(2000)
    expect(bridge.text("body")).toContain("0:02")

    bridge.send(release)
    await settle()
    expect(bridge.mic).toEqual([true, false])
    expect(legato.calls.transcribe).toEqual([[1, 2, 3]])
    expect(bridge.text("body")).toBe("“Run the tests again.”")
    expect(legato.calls.send).toEqual([])

    bridge.send(click)
    await settle()
    expect(legato.calls.send).toEqual([["RUN-1", "Run the tests again."]])
    expect(bridge.text("body")).toBe("Sent to RUN-1.")

    await vi.advanceTimersByTimeAsync(1500)
    expect(bridge.feed()).toEqual(["Agent: Working on it."])
  })

  it("keeps a message the agent was too busy for, and sends it again on a tap", async () => {
    const bridge = fakeBridge()
    const legato = fakeLegato(someAgents)
    const answers = [{ ok: false as const, problem: "The agent is busy. Try again when it stops." }, { ok: true as const }]
    legato.client.send = async (taskId, text) => {
      legato.calls.send.push([taskId, text])
      return answers.shift()!
    }
    await startGlance(bridge as unknown as GlassesBridge, legato.client)
    bridge.send(select(1, "▶ running  RUN-1 Build"))
    await settle()
    bridge.send(hold)
    await settle()
    bridge.send(audio(1))
    bridge.send(release)
    await settle()

    bridge.send(click)
    await settle()
    expect(bridge.text("body")).toBe("The agent is busy. Try again when it stops.\n\n“Run the tests again.”")
    expect(bridge.mic).toEqual([true, false])

    bridge.send(click)
    await settle()
    expect(legato.calls.send).toEqual([
      ["RUN-1", "Run the tests again."],
      ["RUN-1", "Run the tests again."],
    ])
    expect(bridge.text("body")).toBe("Sent to RUN-1.")
  })

  it("cancels a message on a double tap, closing the microphone, and sends nothing", async () => {
    const bridge = fakeBridge()
    const legato = fakeLegato(someAgents)
    await startGlance(bridge as unknown as GlassesBridge, legato.client)
    bridge.send(select(1, "▶ running  RUN-1 Build"))
    await settle()

    bridge.send(hold)
    await settle()
    bridge.send(audio(1))
    bridge.send(doubleClick)
    await settle()

    expect(bridge.mic).toEqual([true, false])
    expect(legato.calls.transcribe).toEqual([])
    expect(bridge.feed()).toEqual(["Agent: Working on it."])
  })

  it("says it heard nothing when no sound came, without asking Legato", async () => {
    const bridge = fakeBridge()
    const legato = fakeLegato(someAgents)
    await startGlance(bridge as unknown as GlassesBridge, legato.client)
    bridge.send(select(1, "▶ running  RUN-1 Build"))
    await settle()

    bridge.send(hold)
    await settle()
    bridge.send(release)
    await settle()

    expect(legato.calls.transcribe).toEqual([])
    expect(bridge.text("body")).toBe("Heard nothing.")
  })

  it("stops listening on its own after a minute", async () => {
    const bridge = fakeBridge()
    const legato = fakeLegato(someAgents)
    await startGlance(bridge as unknown as GlassesBridge, legato.client)
    bridge.send(select(1, "▶ running  RUN-1 Build"))
    await settle()
    bridge.send(hold)
    await settle()
    bridge.send(audio(9))

    await vi.advanceTimersByTimeAsync(60_000)

    expect(bridge.mic).toEqual([true, false])
    expect(legato.calls.transcribe).toEqual([[9]])
  })

  it("stops reading, and listening, while the app is in the background", async () => {
    const bridge = fakeBridge()
    const legato = fakeLegato(someAgents)
    await startGlance(bridge as unknown as GlassesBridge, legato.client)

    bridge.send(system(OsEventTypeList.FOREGROUND_EXIT_EVENT))
    await vi.advanceTimersByTimeAsync(POLL_MS * 5)
    expect(legato.calls.agents).toBe(1)

    bridge.send(system(OsEventTypeList.FOREGROUND_ENTER_EVENT))
    await settle()
    expect(legato.calls.agents).toBe(2)
    await vi.advanceTimersByTimeAsync(POLL_MS)
    expect(legato.calls.agents).toBe(3)
  })

  it("closes the microphone and drops the message when the app goes to the background while listening", async () => {
    const bridge = fakeBridge()
    const legato = fakeLegato(someAgents)
    await startGlance(bridge as unknown as GlassesBridge, legato.client)
    bridge.send(select(1, "▶ running  RUN-1 Build"))
    await settle()
    bridge.send(hold)
    await settle()
    bridge.send(audio(1))

    bridge.send(system(OsEventTypeList.FOREGROUND_EXIT_EVENT))
    await settle()
    expect(bridge.mic).toEqual([true, false])

    bridge.send(system(OsEventTypeList.FOREGROUND_ENTER_EVENT))
    await settle()
    expect(bridge.feed()).toEqual(["Agent: Working on it."])
    expect(legato.calls.transcribe).toEqual([])
  })

  it("does not take a scroll of an agent's page for a tap", async () => {
    const bridge = fakeBridge()
    await startGlance(bridge as unknown as GlassesBridge, fakeLegato(someAgents).client)
    bridge.send(select(1, "▶ running  RUN-1 Build"))
    await settle()

    bridge.send({ listEvent: { containerID: 3, containerName: "feed", eventType: OsEventTypeList.SCROLL_BOTTOM_EVENT } } as EvenHubEvent)
    bridge.send({ listEvent: { containerID: 3, containerName: "feed", eventType: OsEventTypeList.SCROLL_TOP_EVENT } } as EvenHubEvent)
    await settle()

    expect(bridge.mic).toEqual([])
  })

  it("says so when the microphone does not start, and tries again on a hold", async () => {
    const bridge = fakeBridge()
    bridge.micWorks = false
    await startGlance(bridge as unknown as GlassesBridge, fakeLegato(someAgents).client)
    bridge.send(select(1, "▶ running  RUN-1 Build"))
    await settle()

    bridge.send(hold)
    await settle()
    expect(bridge.text("body")).toBe("The microphone didn't start.")

    bridge.micWorks = true
    bridge.send(hold)
    await settle()
    expect(bridge.text("badge")).toBe("● Listening")
  })

  it("shows why a transcription failed, and records again on a hold", async () => {
    const bridge = fakeBridge()
    const legato = fakeLegato(someAgents)
    legato.client.transcribe = async () => ({ ok: false, problem: "Transcription is off in Legato." })
    await startGlance(bridge as unknown as GlassesBridge, legato.client)
    bridge.send(select(1, "▶ running  RUN-1 Build"))
    await settle()
    bridge.send(hold)
    await settle()
    bridge.send(audio(1))
    bridge.send(release)
    await settle()

    expect(bridge.text("body")).toBe("Transcription is off in Legato.")
    bridge.send(hold)
    await settle()
    expect(bridge.mic).toEqual([true, false, true])
  })

  it("finds the chosen item by its index when the SDK gives no name", async () => {
    const bridge = fakeBridge()
    const legato = fakeLegato(someAgents)
    await startGlance(bridge as unknown as GlassesBridge, legato.client)

    bridge.send(select(1, ""))
    await settle()

    expect(legato.calls.conversation).toEqual(["RUN-1"])
  })

  it("does not send a transcript the operator has not seen: a tap while transcribing is dropped", async () => {
    const bridge = fakeBridge()
    const legato = fakeLegato(someAgents)
    let finish: (r: { ok: true; text: string }) => void = () => {}
    legato.client.transcribe = () => new Promise((done) => (finish = done))
    await startGlance(bridge as unknown as GlassesBridge, legato.client)
    bridge.send(select(1, "▶ running  RUN-1 Build"))
    await settle()
    bridge.send(hold)
    await settle()
    bridge.send(audio(1))
    bridge.send(release)
    await settle()
    expect(bridge.text("body")).toBe("Transcribing…")

    bridge.send(click)
    await settle()
    finish({ ok: true, text: "delete the branch" })
    await settle()

    expect(bridge.text("body")).toBe("“delete the branch”")
    expect(legato.calls.send).toEqual([])
  })

  it("opens the agent the operator chose even when a refresh re-sorted the list meanwhile", async () => {
    const bridge = fakeBridge()
    const before: AgentsResult = { ok: true, agents: [agent("A", "working", "Alpha"), agent("B", "working", "Beta")] }
    const after: AgentsResult = { ok: true, agents: [agent("A", "working", "Alpha"), agent("B", "waiting", "Beta")] }
    const legato = fakeLegato(before)
    let answer: (r: AgentsResult) => void = () => {}
    await startGlance(bridge as unknown as GlassesBridge, legato.client)
    legato.client.agents = () => new Promise((done) => (answer = done))

    await vi.advanceTimersByTimeAsync(POLL_MS)
    bridge.send(select(1, "▶ running  B Beta"))
    answer(after)
    await settle()

    expect(legato.calls.conversation).not.toContain("A")
  })

  it("keeps polling after a call to the glasses fails", async () => {
    const changed: AgentsResult = { ok: true, agents: [agent("RUN-1", "waiting", "Build")] }
    const bridge = fakeBridge()
    const legato = fakeLegato(someAgents, someAgents, changed)
    await startGlance(bridge as unknown as GlassesBridge, legato.client)

    vi.setSystemTime(new Date(2026, 10, 13, 9, 6))
    bridge.failUpgrades = 1
    await vi.advanceTimersByTimeAsync(POLL_MS)
    await vi.advanceTimersByTimeAsync(POLL_MS)

    expect(legato.calls.agents).toBe(3)
    expect(bridge.items()).toEqual(["● needs you  RUN-1 Build"])
  })

  it("draws a page again when the glasses refused it", async () => {
    const changed: AgentsResult = { ok: true, agents: [agent("RUN-1", "waiting", "Build")] }
    const bridge = fakeBridge()
    await startGlance(bridge as unknown as GlassesBridge, fakeLegato(someAgents, changed).client)

    bridge.refuseRebuild = true
    await vi.advanceTimersByTimeAsync(POLL_MS)
    bridge.refuseRebuild = false
    await vi.advanceTimersByTimeAsync(POLL_MS)

    expect(bridge.items()).toEqual(["● needs you  RUN-1 Build"])
  })
})
