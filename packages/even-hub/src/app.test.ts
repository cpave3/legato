import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { OsEventTypeList, StartUpPageCreateResult, type EvenHubEvent } from "@evenrealities/even_hub_sdk"
import { BACKGROUND_POLL_MS, startGlance, type GlassesBridge, type LegatoClient, POLL_MS } from "./app"
import type { Agent, AgentsResult, Approval, Conversation, Instance, InstancesResult } from "./legato"

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
    upgraded: [] as { name: string; content: string }[],
    exits: [] as number[],
    mic: [] as boolean[],
    createResult: StartUpPageCreateResult.success as StartUpPageCreateResult,
    async createStartUpPageContainer(page: Page) {
      if (bridge.createResult !== StartUpPageCreateResult.success) return bridge.createResult
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
      bridge.upgraded.push({ name: u.containerName!, content: u.content ?? "" })
      shown.set(u.containerName!, u.content ?? "")
      return true
    },
    micWorks: true,
    async audioControl(open: boolean) {
      bridge.mic.push(open)
      return open ? bridge.micWorks : true
    },
    device: { model: "g2", sn: "G2-1", status: { sn: "G2-1", batteryLevel: 82 } } as unknown,
    statusListener: undefined as ((s: unknown) => void) | undefined,
    async getDeviceInfo() {
      return bridge.device
    },
    onDeviceStatusChanged(cb: (s: unknown) => void) {
      bridge.statusListener = cb
      return () => (bridge.statusListener = undefined)
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
    legatos() {
      return shown.get("legatos") as string[] | undefined
    },
  }
  return bridge
}

const only: Instance = { id: "only", name: "Desk" }

function fakeLegato(...answers: AgentsResult[]) {
  const calls = {
    instances: 0,
    agents: 0,
    at: [] as string[],
    conversation: [] as string[],
    transcribe: [] as number[][],
    send: [] as [string, string][],
    reeve: [] as string[],
    limits: [] as (number | undefined)[],
    decide: [] as [string, string, string | undefined][],
  }
  let pending: Approval[] = []
  let listed: InstancesResult = { ok: true, instances: [only] }
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
    instances: async () => {
      calls.instances++
      return listed
    },
    agents: async (instance) => {
      calls.at.push(instance)
      return answers[Math.min(calls.agents++, answers.length - 1)]
    },
    conversation: async (_instance, taskId, limit) => {
      calls.conversation.push(taskId)
      calls.limits.push(limit)
      return { ok: true, conversation: taskId === "rv1" ? reeveConversation : conversation }
    },
    transcribe: async (_instance, pcm) => {
      calls.transcribe.push([...pcm])
      return { ok: true, text: "Run the tests again." }
    },
    send: async (_instance, taskId, text) => {
      calls.send.push([taskId, text])
      return { ok: true }
    },
    reeve: async (_instance, text) => {
      calls.reeve.push(text)
      return { ok: true }
    },
    approvals: async () => ({ ok: true, approvals: pending }),
    decide: async (_instance, id, decision, option) => {
      calls.decide.push([id, decision, option])
      pending = pending.filter((a) => a.id !== id)
      return { ok: true }
    },
  }
  const reeveConversation: Conversation = { ...conversation, task_id: "rv1", entries: [] }
  return {
    client,
    calls,
    conversation,
    reeveConversation,
    list(next: InstancesResult) {
      listed = next
    },
    waiting(next: Approval[]) {
      pending = next
    },
  }
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
    expect(bridge.text("clock")).toMatch(/^ +09:05$/)
    expect(bridge.items()).toEqual(["◆ Reeve  asleep", "● needs you  ASK-1 Approve", "▶ running  RUN-1 Build", "Idle agents (1) →"])
  })

  it("goes to the idle tab from its item, back on a double tap, and asks to exit from the active tab", async () => {
    const bridge = fakeBridge()
    await startGlance(bridge as unknown as GlassesBridge, fakeLegato(someAgents).client)

    bridge.send(select(3, "Idle agents (1) →"))
    await settle()
    expect(bridge.items()).toEqual(["○ IDLE-1 Rest", "← Active agents (2)"])

    bridge.send(doubleClick)
    await settle()
    expect(bridge.items()?.[1]).toBe("● needs you  ASK-1 Approve")
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
    expect(bridge.text("clock")?.trim()).toBe("09:06")

    await vi.advanceTimersByTimeAsync(POLL_MS)
    expect(bridge.rebuilt).toBe(rebuilt + 1)
    expect(bridge.items()).toEqual(["◆ Reeve  asleep", "● needs you  RUN-1 Build"])
  })

  it("shows beside the clock how old the list is once reads have failed for a minute", async () => {
    const bridge = fakeBridge()
    await startGlance(bridge as unknown as GlassesBridge, fakeLegato(someAgents, { ok: false, problem: "Can't reach Legato" }).client)

    await vi.advanceTimersByTimeAsync(55_000)
    expect(bridge.text("clock")?.trim()).toBe("09:05")
    await vi.advanceTimersByTimeAsync(5_000)
    expect(bridge.text("clock")).toMatch(/^ +1m · 09:05$/)
  })

  it("reads at once when switching to background mode and back", async () => {
    const changed: AgentsResult = { ok: true, agents: [agent("RUN-1", "working")] }
    const bridge = fakeBridge()
    const legato = fakeLegato(someAgents, changed, someAgents)
    await startGlance(bridge as unknown as GlassesBridge, legato.client)

    bridge.send(hold)
    await settle()
    expect(legato.calls.agents).toBe(2)
    expect(bridge.text("alert")?.trim()).toBe("")

    bridge.send(hold)
    await settle()
    expect(legato.calls.agents).toBe(3)
    expect(bridge.items()?.[1]).toBe("● needs you  ASK-1 Approve")
  })

  it("keeps the last list and says why when a read fails", async () => {
    const bridge = fakeBridge()
    await startGlance(bridge as unknown as GlassesBridge, fakeLegato(someAgents, { ok: false, problem: "Can't reach Legato" }).client)

    await vi.advanceTimersByTimeAsync(POLL_MS)

    expect(bridge.text("status")).toBe("Can't reach Legato")
    expect(bridge.items()?.[1]).toBe("● needs you  ASK-1 Approve")
  })

  it("opens an agent on a tap: its state in a badge, its feed kept fresh, until a double tap goes back", async () => {
    const bridge = fakeBridge()
    const legato = fakeLegato(someAgents)
    await startGlance(bridge as unknown as GlassesBridge, legato.client)

    bridge.send(select(1, "● needs you  ASK-1 Approve"))
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
    expect(bridge.items()?.[1]).toBe("● needs you  ASK-1 Approve")
    expect(bridge.exits).toEqual([])
  })

  it("opens one entry of the feed in full on a tap, and goes back to the feed on a double tap", async () => {
    const bridge = fakeBridge()
    const legato = fakeLegato(someAgents)
    legato.conversation.entries.push({ kind: "tool", text: "Run the tests", tool: "Bash", command: "pnpm test", state: "failed" })
    await startGlance(bridge as unknown as GlassesBridge, legato.client)
    bridge.send(select(2, "▶ running  RUN-1 Build"))
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
    bridge.send(select(2, "▶ running  RUN-1 Build"))
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
    bridge.send(select(2, "▶ running  RUN-1 Build"))
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
    legato.client.send = async (_instance, taskId, text) => {
      legato.calls.send.push([taskId, text])
      return answers.shift()!
    }
    await startGlance(bridge as unknown as GlassesBridge, legato.client)
    bridge.send(select(2, "▶ running  RUN-1 Build"))
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
    bridge.send(select(2, "▶ running  RUN-1 Build"))
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
    bridge.send(select(2, "▶ running  RUN-1 Build"))
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
    bridge.send(select(2, "▶ running  RUN-1 Build"))
    await settle()
    bridge.send(hold)
    await settle()
    bridge.send(audio(9))

    await vi.advanceTimersByTimeAsync(60_000)

    expect(bridge.mic).toEqual([true, false])
    expect(legato.calls.transcribe).toEqual([[9]])
  })

  it("keeps no more than a minute of sound, however fast it comes", async () => {
    const bridge = fakeBridge()
    const legato = fakeLegato(someAgents)
    await startGlance(bridge as unknown as GlassesBridge, legato.client)
    bridge.send(select(2, "▶ running  RUN-1 Build"))
    await settle()
    bridge.send(hold)
    await settle()
    const chunk = new Uint8Array(3200).fill(1)
    for (let i = 0; i < 700; i++) bridge.send({ audioEvent: { audioPcm: chunk } } as EvenHubEvent)
    bridge.send(release)
    await settle()

    expect(legato.calls.transcribe[0]).toHaveLength(60 * 16_000 * 2)
  })

  it("reads less often while the app is out of the foreground (the screen off), and at once when it is back", async () => {
    const changed: AgentsResult = { ok: true, agents: [agent("RUN-1", "idle")] }
    const bridge = fakeBridge()
    const legato = fakeLegato(someAgents, someAgents, changed)
    await startGlance(bridge as unknown as GlassesBridge, legato.client)

    bridge.send(system(OsEventTypeList.FOREGROUND_EXIT_EVENT))
    await vi.advanceTimersByTimeAsync(BACKGROUND_POLL_MS - 1)
    expect(legato.calls.agents).toBe(1)
    await vi.advanceTimersByTimeAsync(1)
    expect(legato.calls.agents).toBe(2)

    bridge.send(system(OsEventTypeList.FOREGROUND_ENTER_EVENT))
    await settle()
    expect(legato.calls.agents).toBe(3)
    expect(bridge.items()).toEqual(["◆ Reeve  asleep", "Nothing running.", "Idle agents (1) →"])
    await vi.advanceTimersByTimeAsync(POLL_MS)
    expect(legato.calls.agents).toBe(4)
  })

  it("closes the microphone and drops the message when the app goes to the background while listening", async () => {
    const bridge = fakeBridge()
    const legato = fakeLegato(someAgents)
    await startGlance(bridge as unknown as GlassesBridge, legato.client)
    bridge.send(select(2, "▶ running  RUN-1 Build"))
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
    bridge.send(select(2, "▶ running  RUN-1 Build"))
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
    bridge.send(select(2, "▶ running  RUN-1 Build"))
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
    bridge.send(select(2, "▶ running  RUN-1 Build"))
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

    bridge.send(select(2, ""))
    await settle()

    expect(legato.calls.conversation).toEqual(["RUN-1"])
  })

  it("does not send a transcript the operator has not seen: a tap while transcribing is dropped", async () => {
    const bridge = fakeBridge()
    const legato = fakeLegato(someAgents)
    let finish: (r: { ok: true; text: string }) => void = () => {}
    legato.client.transcribe = () => new Promise((done) => (finish = done))
    await startGlance(bridge as unknown as GlassesBridge, legato.client)
    bridge.send(select(2, "▶ running  RUN-1 Build"))
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
    bridge.send(select(2, "▶ running  B Beta"))
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
    expect(bridge.items()).toEqual(["◆ Reeve  asleep", "● needs you  RUN-1 Build"])
  })

  it("starts on a redraw when the glasses refuse a first page, as after the app's page reloads", async () => {
    const bridge = fakeBridge()
    bridge.createResult = StartUpPageCreateResult.invalid
    await startGlance(bridge as unknown as GlassesBridge, fakeLegato(someAgents).client)

    expect(bridge.items()?.[1]).toBe("● needs you  ASK-1 Approve")
    bridge.send(hold)
    await settle()
    expect(bridge.text("alert")).toBe("● 1")
  })

  it("draws a page again when the glasses refused it", async () => {
    const changed: AgentsResult = { ok: true, agents: [agent("RUN-1", "waiting", "Build")] }
    const bridge = fakeBridge()
    await startGlance(bridge as unknown as GlassesBridge, fakeLegato(someAgents, changed).client)

    bridge.refuseRebuild = true
    await vi.advanceTimersByTimeAsync(POLL_MS)
    bridge.refuseRebuild = false
    await vi.advanceTimersByTimeAsync(POLL_MS)

    expect(bridge.items()).toEqual(["◆ Reeve  asleep", "● needs you  RUN-1 Build"])
  })
})

const theReeve = (activity: string): Agent => ({ ...agent("rv1", activity, "The Reeve"), reeve: true })
const withReeve = (activity: string): AgentsResult => ({ ok: true, agents: [...(someAgents.ok ? someAgents.agents : []), theReeve(activity)] })

describe("the Reeve on the glasses", () => {
  beforeEach(() => {
    vi.useFakeTimers()
    vi.setSystemTime(new Date(2026, 10, 13, 9, 5))
  })
  afterEach(() => vi.useRealTimers())

  it("pins the Reeve first on the active tab, and opens its chat on a tap", async () => {
    const bridge = fakeBridge()
    const legato = fakeLegato(withReeve(""))
    legato.reeveConversation.entries.push(
      { kind: "user", text: "[legato glasses] how many are working?" },
      { kind: "assistant", text: "One: RUN-1." },
    )
    await startGlance(bridge as unknown as GlassesBridge, legato.client)
    expect(bridge.items()?.[0]).toBe("◆ Reeve  ready")

    bridge.send(select(0, "◆ Reeve  ready"))
    await settle()

    expect(bridge.text("badge")).toBe("○ ready")
    expect(bridge.text("title")).toBe("Reeve · Desk")
    expect(bridge.text("asked")).toBe("You: how many are working?")
    expect(bridge.text("reply")).toBe("One: RUN-1.")
    expect(bridge.text("hint")?.trim()).toBe("Hold: talk · Tap: history · Double tap: back")
    expect(legato.calls.conversation).toEqual(["rv1"])
    expect(legato.calls.limits).toEqual([50])

    legato.reeveConversation.entries.push({ kind: "assistant", text: "Two now." })
    await vi.advanceTimersByTimeAsync(POLL_MS)
    expect(bridge.text("reply")).toBe("One: RUN-1.\n\nTwo now.")

    bridge.send(doubleClick)
    await settle()
    expect(bridge.items()?.[0]).toBe("◆ Reeve  ready")
  })

  it("sends a dictated message to the Reeve, and shows it thinking until it replies", async () => {
    const bridge = fakeBridge()
    const legato = fakeLegato(withReeve(""), withReeve("working"))
    await startGlance(bridge as unknown as GlassesBridge, legato.client)
    bridge.send(select(0, "◆ Reeve  ready"))
    await settle()

    bridge.send(hold)
    await settle()
    expect(bridge.text("badge")).toBe("● Listening")
    expect(bridge.text("body")).toContain("Speak to the Reeve.")
    bridge.send(audio(1, 2))
    bridge.send(release)
    await settle()
    expect(bridge.text("body")).toBe("“Run the tests again.”")

    bridge.send(click)
    await settle()
    expect(legato.calls.reeve).toEqual(["Run the tests again."])
    expect(legato.calls.send).toEqual([])
    expect(bridge.text("body")).toBe("Sent to the Reeve.")

    await vi.advanceTimersByTimeAsync(1500)
    expect(bridge.text("asked")).toBe("You: Run the tests again.")
    expect(bridge.text("reply")).toBe("Thinking…")

    legato.reeveConversation.entries.push(
      { kind: "user", text: "[legato glasses] Run the tests again." },
      { kind: "assistant", text: "Started them on RUN-1." },
    )
    await vi.advanceTimersByTimeAsync(POLL_MS)
    expect(bridge.text("reply")).toBe("Started them on RUN-1.")
  })

  it("talks to a Reeve that is asleep, which the message wakes", async () => {
    const bridge = fakeBridge()
    const legato = fakeLegato(someAgents)
    await startGlance(bridge as unknown as GlassesBridge, legato.client)
    bridge.send(select(0, "◆ Reeve  asleep"))
    await settle()
    expect(bridge.text("badge")).toBe("○ asleep")
    expect(bridge.text("reply")).toBe("The Reeve is asleep. Hold to talk and it wakes.")
    expect(legato.calls.conversation).toEqual([])

    bridge.send(hold)
    await settle()
    bridge.send(audio(1))
    bridge.send(release)
    await settle()
    bridge.send(click)
    await settle()

    expect(legato.calls.reeve).toEqual(["Run the tests again."])
  })

  it("opens the Reeve's whole conversation on a tap, and comes back to the chat on a double tap", async () => {
    const bridge = fakeBridge()
    const legato = fakeLegato(withReeve(""))
    legato.reeveConversation.entries.push({ kind: "assistant", text: "Ready." })
    await startGlance(bridge as unknown as GlassesBridge, legato.client)
    bridge.send(select(0, "◆ Reeve  ready"))
    await settle()

    bridge.send(click)
    await settle()
    expect(bridge.feed()).toEqual(["Agent: Ready."])

    bridge.send(doubleClick)
    await settle()
    expect(bridge.text("reply")).toBe("Ready.")
  })

  it("keeps a message the Reeve could not take, and sends it again on a tap", async () => {
    const bridge = fakeBridge()
    const legato = fakeLegato(withReeve(""))
    const answers = [{ ok: false as const, problem: "This Legato has no Reeve." }, { ok: true as const }]
    legato.client.reeve = async (_instance, text) => {
      legato.calls.reeve.push(text)
      return answers.shift()!
    }
    await startGlance(bridge as unknown as GlassesBridge, legato.client)
    bridge.send(select(0, "◆ Reeve  ready"))
    await settle()
    bridge.send(hold)
    await settle()
    bridge.send(audio(1))
    bridge.send(release)
    await settle()
    bridge.send(click)
    await settle()
    expect(bridge.text("badge")).toBe("▲ Not sent")

    bridge.send(click)
    await settle()
    expect(legato.calls.reeve).toEqual(["Run the tests again.", "Run the tests again."])
  })
})

describe("background mode", () => {
  beforeEach(() => {
    vi.useFakeTimers()
    vi.setSystemTime(new Date(2026, 10, 13, 9, 5))
  })
  afterEach(() => vi.useRealTimers())

  it("folds the list into a row of counts across the top on a hold, kept fresh, and unfolds it on another", async () => {
    const changed: AgentsResult = { ok: true, agents: [agent("RUN-1", "waiting"), agent("ASK-1", "waiting")] }
    const bridge = fakeBridge()
    const legato = fakeLegato(someAgents, someAgents, changed)
    await startGlance(bridge as unknown as GlassesBridge, legato.client)
    expect(bridge.text("hint")?.trim()).toBe("Tap: open · Hold: background · Double tap: exit")

    bridge.send(hold)
    await settle()
    expect(bridge.text("alert")).toBe("● 1")
    expect(bridge.text("counts")?.trim()).toBe("▶ 1  ○ 1  ·  G2 82%")
    expect(bridge.items()).toBeUndefined()

    await vi.advanceTimersByTimeAsync(POLL_MS)
    expect(bridge.text("alert")).toBe("● 2")
    expect(bridge.text("counts")?.trim()).toBe("G2 82%")

    bridge.send(click)
    bridge.send(doubleClick)
    await settle()
    expect(bridge.text("alert")).toBe("● 2")
    expect(bridge.exits).toEqual([])

    bridge.send(hold)
    await settle()
    expect(bridge.items()).toEqual(["◆ Reeve  asleep", "● needs you  RUN-1", "● needs you  ASK-1"])
  })
})

describe("clearing a box on the glasses", () => {
  beforeEach(() => vi.useFakeTimers())
  afterEach(() => vi.useRealTimers())

  it("sends a space, not an empty text, which the glasses ignore", async () => {
    const answered: AgentsResult = { ok: true, agents: [agent("RUN-1", "working"), agent("ASK-1", "working")] }
    const bridge = fakeBridge()
    await startGlance(bridge as unknown as GlassesBridge, fakeLegato(someAgents, someAgents, answered).client)
    bridge.send(hold)
    await settle()
    expect(bridge.text("alert")).toBe("● 1")

    await vi.advanceTimersByTimeAsync(POLL_MS)

    expect(bridge.upgraded.filter((u) => u.name === "alert").at(-1)).toEqual({ name: "alert", content: " " })
    expect(bridge.upgraded.some((u) => u.content === "")).toBe(false)
  })
})

describe("the glasses' battery in background mode", () => {
  beforeEach(() => vi.useFakeTimers())
  afterEach(() => vi.useRealTimers())

  it("shows the glasses' battery, and follows its changes", async () => {
    const bridge = fakeBridge()
    await startGlance(bridge as unknown as GlassesBridge, fakeLegato(someAgents).client)
    bridge.send(hold)
    await settle()
    expect(bridge.text("counts")?.trim()).toBe("▶ 1  ○ 1  ·  G2 82%")

    bridge.statusListener?.({ sn: "G2-1", batteryLevel: 15, isCharging: false })
    await settle()
    expect(bridge.text("alert")).toBe("● 1  ▲ G2 15%")
  })

  it("ignores another device's status, such as the ring's", async () => {
    const bridge = fakeBridge()
    await startGlance(bridge as unknown as GlassesBridge, fakeLegato(someAgents).client)
    bridge.send(hold)
    await settle()

    bridge.statusListener?.({ sn: "RING-1", batteryLevel: 5 })
    await settle()
    expect(bridge.text("counts")?.trim()).toBe("▶ 1  ○ 1  ·  G2 82%")
  })

  it("goes on without the battery when the Even app never answers for the device", async () => {
    const bridge = fakeBridge()
    bridge.getDeviceInfo = () => new Promise(() => {})
    await startGlance(bridge as unknown as GlassesBridge, fakeLegato(someAgents).client)
    await vi.advanceTimersByTimeAsync(5000)

    bridge.send(hold)
    await settle()
    expect(bridge.text("counts")?.trim()).toBe("▶ 1  ○ 1")
  })

  it("shows no battery when the Even app gives no glasses", async () => {
    const bridge = fakeBridge()
    bridge.device = null
    await startGlance(bridge as unknown as GlassesBridge, fakeLegato(someAgents).client)
    bridge.send(hold)
    await settle()
    expect(bridge.text("counts")?.trim()).toBe("▶ 1  ○ 1")
  })
})

const push: Approval = {
  id: "ap-1",
  source: "agent_permission",
  task_id: "ASK-1",
  title: "Bash: git push",
  summary: "git push origin main",
  options: [
    { id: "once", label: "Allow once", kind: "allow" },
    { id: "deny", label: "Deny", kind: "deny" },
  ],
}
const signedToken: Approval = { ...push, id: "ap-2", task_id: "RUN-1", source: "credential", title: "Use the GitHub token", signed: true }
const options = (bridge: ReturnType<typeof fakeBridge>) => bridge.text("options") as unknown as string[] | undefined
const choose = (index: number, name: string): EvenHubEvent =>
  ({ listEvent: { containerID: 4, containerName: "options", currentSelectItemIndex: index, currentSelectItemName: name } }) as EvenHubEvent

describe("approvals on the glasses", () => {
  beforeEach(() => {
    vi.useFakeTimers()
    vi.setSystemTime(new Date(2026, 10, 13, 9, 5))
  })
  afterEach(() => vi.useRealTimers())

  async function openApprovals(waiting: Approval[]) {
    const bridge = fakeBridge()
    const legato = fakeLegato(someAgents)
    legato.waiting(waiting)
    await startGlance(bridge as unknown as GlassesBridge, legato.client)
    expect(bridge.items()?.[1]).toBe(`● To approve (${waiting.length}) →`)
    bridge.send(select(1, `● To approve (${waiting.length}) →`))
    await settle()
    return { bridge, legato }
  }

  it("lists the approvals waiting, and allows one after a second tap confirms it", async () => {
    const { bridge, legato } = await openApprovals([push, signedToken])
    expect(bridge.text("badge")).toBe("● to approve")
    expect(bridge.feed()).toEqual(["● ASK-1 Bash: git push", "◇ RUN-1 Use the GitHub token"])

    bridge.send(pick(0, "● ASK-1 Bash: git push"))
    await settle()
    expect(bridge.text("badge")).toBe("● approve?")
    expect(bridge.text("body")).toBe("Bash: git push")
    expect(options(bridge)).toEqual(["Allow once", "Deny"])

    bridge.send(choose(0, "Allow once"))
    await settle()
    expect(bridge.text("badge")).toBe("Allow?")
    expect(legato.calls.decide).toEqual([])

    bridge.send(click)
    await settle()
    expect(legato.calls.decide).toEqual([["ap-1", "allow", "once"]])
    expect(bridge.text("badge")).toBe("Allowed")

    await vi.advanceTimersByTimeAsync(1500)
    expect(bridge.feed()).toEqual(["◇ RUN-1 Use the GitHub token"])
  })

  it("denies, and drops the choice on a double tap before it is confirmed", async () => {
    const { bridge, legato } = await openApprovals([push])
    bridge.send(pick(0, "● ASK-1 Bash: git push"))
    await settle()
    bridge.send(choose(1, "Deny"))
    await settle()
    expect(bridge.text("badge")).toBe("Deny?")

    bridge.send(doubleClick)
    await settle()
    expect(bridge.text("badge")).toBe("● approve?")
    expect(legato.calls.decide).toEqual([])

    bridge.send(choose(1, "Deny"))
    await settle()
    bridge.send(click)
    await settle()
    expect(legato.calls.decide).toEqual([["ap-1", "deny", "deny"]])
  })

  it("offers nothing to decide for one that must be signed", async () => {
    const { bridge, legato } = await openApprovals([signedToken])
    bridge.send(pick(0, "◇ RUN-1 Use the GitHub token"))
    await settle()

    expect(bridge.text("badge")).toBe("◇ signed")
    expect(options(bridge)).toBeUndefined()
    bridge.send(click)
    await settle()
    expect(legato.calls.decide).toEqual([])
  })

  it("does not take a tap made on the choice for a confirmation", async () => {
    const { bridge, legato } = await openApprovals([push])
    bridge.send(pick(0, "● ASK-1 Bash: git push"))
    await settle()

    bridge.send(choose(0, "Allow once"))
    bridge.send(click)
    await settle()

    expect(bridge.text("badge")).toBe("Allow?")
    expect(legato.calls.decide).toEqual([])
  })

  it("opens an agent's approval from the line of what it asks", async () => {
    const bridge = fakeBridge()
    const legato = fakeLegato(someAgents)
    legato.waiting([push])
    legato.conversation.status = "waiting_for_permission"
    legato.conversation.permission = { tool: "Bash", command: "git push" }
    await startGlance(bridge as unknown as GlassesBridge, legato.client)
    bridge.send(select(2, "● needs you  ASK-1 Approve"))
    await settle()

    bridge.send(pick(0, "● Asks: Bash git push"))
    await settle()
    expect(bridge.text("badge")).toBe("● approve?")

    bridge.send(doubleClick)
    await settle()
    expect(bridge.feed()?.[0]).toBe("● Asks: Bash git push")
  })

  it("goes back when the approval is decided somewhere else meanwhile", async () => {
    const { bridge, legato } = await openApprovals([push])
    bridge.send(pick(0, "● ASK-1 Bash: git push"))
    await settle()

    legato.waiting([])
    await vi.advanceTimersByTimeAsync(POLL_MS)

    expect(bridge.feed()).toEqual(["Nothing waits for you."])
  })
})

describe("startGlance with several Legatos", () => {
  beforeEach(() => {
    vi.useFakeTimers()
    vi.setSystemTime(new Date(2026, 10, 13, 9, 5))
  })
  afterEach(() => vi.useRealTimers())

  const desk: Instance = { id: "desk", name: "Desk" }
  const laptop: Instance = { id: "laptop", name: "Laptop" }
  const pickFrom = (index: number, name: string): EvenHubEvent =>
    ({ listEvent: { containerID: 5, containerName: "legatos", currentSelectItemIndex: index, currentSelectItemName: name } }) as EvenHubEvent

  async function started(...instances: Instance[]) {
    const bridge = fakeBridge()
    const legato = fakeLegato(someAgents)
    legato.list({ ok: true, instances })
    await startGlance(bridge as unknown as GlassesBridge, legato.client)
    return { bridge, legato }
  }

  it("opens the only Legato straight away, and exits from its active tab", async () => {
    const { bridge, legato } = await started(desk)

    expect(bridge.items()?.[1]).toBe("● needs you  ASK-1 Approve")
    expect(legato.calls.at).toEqual(["desk"])

    bridge.send(doubleClick)
    await settle()
    expect(bridge.exits).toEqual([1])
  })

  it("lists several to pick from, reading none of their agents yet", async () => {
    const { bridge, legato } = await started(desk, laptop)

    expect(bridge.legatos()).toEqual(["Desk", "Laptop"])
    expect(bridge.text("title")).toBe("Legato")
    expect(legato.calls.at).toEqual([])
  })

  it("opens the Legato tapped, and reads that one from then on", async () => {
    const { bridge, legato } = await started(desk, laptop)

    bridge.send(pickFrom(1, "Laptop"))
    await settle()
    expect(bridge.items()?.[1]).toBe("● needs you  ASK-1 Approve")
    expect(legato.calls.at).toEqual(["laptop"])

    await vi.advanceTimersByTimeAsync(POLL_MS)
    expect(legato.calls.at).toEqual(["laptop", "laptop"])
  })

  it("tells two Legatos of the same name apart by where they were in the list", async () => {
    const { bridge, legato } = await started({ id: "a", name: "Legato" }, { id: "b", name: "Legato" })

    bridge.send(pickFrom(1, "Legato"))
    await settle()

    expect(legato.calls.at).toEqual(["b"])
  })

  it("goes back to the picker on a double tap from the active tab, and exits from the picker", async () => {
    const { bridge } = await started(desk, laptop)
    bridge.send(pickFrom(0, "Desk"))
    await settle()

    bridge.send(doubleClick)
    await settle()
    expect(bridge.legatos()).toEqual(["Desk", "Laptop"])
    expect(bridge.exits).toEqual([])

    bridge.send(doubleClick)
    await settle()
    expect(bridge.exits).toEqual([1])
  })

  it("reads the list again while the picker is in front", async () => {
    const { bridge, legato } = await started(desk, laptop)
    legato.list({ ok: true, instances: [desk, laptop, { id: "studio", name: "Studio" }] })

    await vi.advanceTimersByTimeAsync(POLL_MS)

    expect(bridge.legatos()).toEqual(["Desk", "Laptop", "Studio"])
  })

  it("says to add a Legato in the Legato app when it has none", async () => {
    const { bridge } = await started()

    expect(bridge.legatos()).toEqual(["Add a Legato in the Legato app."])
  })

  it("shows the Legato app's problem in the picker's header", async () => {
    const bridge = fakeBridge()
    const legato = fakeLegato(someAgents)
    legato.list({ ok: false, problem: "Not paired with the Legato app" })
    await startGlance(bridge as unknown as GlassesBridge, legato.client)

    expect(bridge.text("status")).toBe("Not paired with the Legato app")
    expect(bridge.legatos()).toEqual(["No Legatos to show."])
  })

  it("goes back to the picker, saying why, when the open Legato is removed from the app", async () => {
    const bridge = fakeBridge()
    const legato = fakeLegato(someAgents, { ok: false, problem: "That Legato is no longer in the app", gone: true })
    legato.list({ ok: true, instances: [desk, laptop] })
    await startGlance(bridge as unknown as GlassesBridge, legato.client)
    bridge.send(pickFrom(0, "Desk"))
    await settle()
    legato.list({ ok: true, instances: [laptop] })

    await vi.advanceTimersByTimeAsync(POLL_MS)

    expect(bridge.legatos()).toEqual(["Laptop"])
    expect(bridge.text("status")).toBe("That Legato is no longer in the app")
  })

  it("goes back to the picker when the open Legato is removed while an agent is open", async () => {
    const bridge = fakeBridge()
    const legato = fakeLegato(someAgents)
    legato.list({ ok: true, instances: [desk, laptop] })
    await startGlance(bridge as unknown as GlassesBridge, legato.client)
    bridge.send(pickFrom(0, "Desk"))
    await settle()
    bridge.send(select(2, "▶ running  RUN-1 Build"))
    await settle()
    legato.client.agents = async () => ({ ok: false, problem: "That Legato is no longer in the app", gone: true })

    await vi.advanceTimersByTimeAsync(POLL_MS)

    expect(bridge.legatos()).toEqual(["Desk", "Laptop"])
    expect(bridge.text("status")).toBe("That Legato is no longer in the app")
  })

  it("opens the Legato tapped by its name when the list has moved under it", async () => {
    const { bridge, legato } = await started(desk, laptop)
    legato.list({ ok: true, instances: [laptop, desk] })
    const tap = pickFrom(0, "Desk")

    await vi.advanceTimersByTimeAsync(POLL_MS)
    bridge.send(tap)
    await settle()

    expect(legato.calls.at).toEqual(["desk"])
  })

  it("still opens the only Legato straight away when the first read failed, as before pairing", async () => {
    const bridge = fakeBridge()
    const legato = fakeLegato(someAgents)
    legato.list({ ok: false, problem: "Not paired with the Legato app" })
    await startGlance(bridge as unknown as GlassesBridge, legato.client)
    expect(bridge.legatos()).toEqual(["No Legatos to show."])

    legato.list({ ok: true, instances: [desk] })
    await vi.advanceTimersByTimeAsync(POLL_MS)

    expect(bridge.items()?.[1]).toBe("● needs you  ASK-1 Approve")
    bridge.send(doubleClick)
    await settle()
    expect(bridge.exits).toEqual([1])
  })
})

