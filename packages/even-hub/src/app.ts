import {
  AudioInputSource,
  CreateStartUpPageContainer,
  ListContainerProperty,
  ListItemContainerProperty,
  OsEventTypeList,
  RebuildPageContainer,
  StartUpPageCreateResult,
  TextContainerProperty,
  TextContainerUpgrade,
  type EvenAppBridge,
  type EvenHubEvent,
} from "@evenrealities/even_hub_sdk"
import { agentPage, dictationPage, type Dictation, type Page } from "./agent"
import { CANVAS_HEIGHT, CANVAS_WIDTH, glance, type Screen, type Tab } from "./glance"
import type { Agent, AgentsResult, ConversationResult, SendResult, TranscriptResult } from "./legato"

// How often the app reads Legato while it is in front.
export const POLL_MS = 5000
// The longest message it records, and how long "Sent" stays up.
const MAX_RECORDING_S = 60
const SENT_MS = 1500

// The parts of the SDK's bridge the app uses.
export type GlassesBridge = Pick<
  EvenAppBridge,
  | "createStartUpPageContainer"
  | "rebuildPageContainer"
  | "textContainerUpgrade"
  | "onEvenHubEvent"
  | "shutDownPageContainer"
  | "audioControl"
>

// What the app asks of Legato.
export type LegatoClient = {
  agents(): Promise<AgentsResult>
  conversation(taskId: string): Promise<ConversationResult>
  transcribe(pcm: Uint8Array): Promise<TranscriptResult>
  send(taskId: string, text: string): Promise<SendResult>
}

// A text container on a page.
type TextBox = {
  id: number
  name: string
  x: number
  y: number
  width: number
  height: number
  padding: number
  brightness: number
  border?: boolean
  capture?: boolean
}

const HEADER = 36

// The list page: the two tabs, a problem, the time, and the list.
const listBoxes = {
  "tab-active": { id: 1, name: "tab-active", x: 0, y: 0, width: 112, height: HEADER, padding: 2, brightness: 4 },
  "tab-idle": { id: 2, name: "tab-idle", x: 116, y: 0, width: 100, height: HEADER, padding: 2, brightness: 4 },
  status: { id: 3, name: "status", x: 220, y: 0, width: 276, height: HEADER, padding: 4, brightness: 3 },
  clock: { id: 4, name: "clock", x: CANVAS_WIDTH - 80, y: 0, width: 80, height: HEADER, padding: 4, brightness: 2 },
} satisfies Record<string, TextBox>
const LIST = { id: 5, name: "agents", y: HEADER + 4 }

// The agent page: its title, what it said (which scrolls), and what the
// touchpad does.
const agentBoxes = {
  title: { id: 1, name: "title", x: 0, y: 0, width: CANVAS_WIDTH, height: HEADER, padding: 4, brightness: 4 },
  body: { id: 2, name: "body", x: 0, y: HEADER, width: CANVAS_WIDTH, height: CANVAS_HEIGHT - 2 * HEADER, padding: 8, brightness: 4, capture: true },
  hint: { id: 3, name: "hint", x: 0, y: CANVAS_HEIGHT - HEADER, width: CANVAS_WIDTH, height: HEADER, padding: 4, brightness: 2 },
} satisfies Record<string, TextBox>

// The most characters a text container takes when a page is drawn; more
// goes in with an update after.
const DRAW_LIMIT = 1000

function textBox(box: TextBox, content: string): TextContainerProperty {
  return new TextContainerProperty({
    containerID: box.id,
    containerName: box.name,
    xPosition: box.x,
    yPosition: box.y,
    width: box.width,
    height: box.height,
    paddingLength: box.padding,
    borderWidth: box.border ? 1 : 0,
    borderColor: box.border ? 15 : 0,
    borderRadius: box.border ? 6 : 0,
    content: content.length > DRAW_LIMIT ? "" : content,
    textColor: box.brightness,
    isEventCapture: box.capture ? 1 : 0,
  })
}

type Layout = { textObject: TextContainerProperty[]; listObject: ListContainerProperty[] }

function listLayout(screen: Screen): Layout {
  const selected = (tab: Tab) => ({ border: screen.tab === tab, brightness: screen.tab === tab ? 4 : 2 })
  return {
    textObject: [
      textBox({ ...listBoxes["tab-active"], ...selected("active") }, screen.tabs.active),
      textBox({ ...listBoxes["tab-idle"], ...selected("idle") }, screen.tabs.idle),
      textBox(listBoxes.status, screen.status),
      textBox(listBoxes.clock, screen.clock),
    ],
    listObject: [
      new ListContainerProperty({
        containerID: LIST.id,
        containerName: LIST.name,
        xPosition: 0,
        yPosition: LIST.y,
        width: CANVAS_WIDTH,
        height: CANVAS_HEIGHT - LIST.y,
        borderWidth: 0,
        paddingLength: 4,
        isEventCapture: 1,
        itemContainer: new ListItemContainerProperty({
          itemCount: screen.items.length,
          isItemSelectBorderEn: 1,
          itemName: screen.items.map((i) => i.text),
        }),
      }),
    ],
  }
}

function agentLayout(page: Page): Layout {
  return {
    textObject: [textBox(agentBoxes.title, page.title), textBox(agentBoxes.body, page.body), textBox(agentBoxes.hint, page.hint)],
    listObject: [],
  }
}

type Click = "click" | "double" | undefined

// A touchpad press, wherever the SDK put it: a list's or a text container's
// event, or a system event. A click is type 0, which the SDK may leave out;
// simulator 0.9.5 sends a tap on a text container as {"sysEvent":{"eventSource":1}}.
function press(event: EvenHubEvent): Click {
  const type = event.listEvent ? event.listEvent.eventType : event.textEvent ? event.textEvent.eventType : event.sysEvent?.eventType
  if (!event.listEvent && !event.textEvent && !event.sysEvent) return undefined
  if (type === undefined || type === OsEventTypeList.CLICK_EVENT) return "click"
  if (type === OsEventTypeList.DOUBLE_CLICK_EVENT) return "double"
  return undefined
}

type View = { kind: "list" } | { kind: "agent"; agent: Agent; read?: ConversationResult; dictation?: Dictation }

// Starts the app: draws the list, reads Legato now and every POLL_MS while
// the app is in front, and answers the touchpad. Resolves once the first
// list is shown.
export async function startGlance(bridge: GlassesBridge, legato: LegatoClient): Promise<void> {
  let agents: Agent[] = []
  let updatedAt: Date | undefined
  let problem: string | undefined
  let tab: Tab = "active"
  let view: View = { kind: "list" }
  let screen: Screen = { ...glance([]), items: [{ text: "Reading Legato…" }] }

  // What is on the glasses: the page's layout key and each text's content.
  let drawnKey = ""
  let drawnText = new Map<string, string>()

  // Everything that changes the state runs in turn, so a tap during a read
  // waits for it rather than racing it.
  let queue = Promise.resolve()
  const enqueue = (work: () => Promise<void>) => {
    queue = queue.then(work).catch((err: unknown) => console.error("[legato-even-hub]", err))
    return queue
  }

  async function draw(key: string, layout: Layout, texts: Record<string, string>, first = false): Promise<void> {
    if (key !== drawnKey) {
      const page = { containerTotalNum: layout.textObject.length + layout.listObject.length, ...layout }
      if (first) {
        const result = await bridge.createStartUpPageContainer(new CreateStartUpPageContainer(page))
        if (result !== StartUpPageCreateResult.success) throw new Error(`the glasses refused the page (result ${result})`)
      } else if (!(await bridge.rebuildPageContainer(new RebuildPageContainer(page)))) {
        // Left undrawn, so the next refresh draws it again.
        throw new Error("the glasses refused the page")
      }
      drawnKey = key
      drawnText = new Map(layout.textObject.map((t) => [t.containerName!, t.content ?? ""]))
    }
    for (const t of layout.textObject) {
      const name = t.containerName!
      if (drawnText.get(name) === texts[name]) continue
      await bridge.textContainerUpgrade(
        new TextContainerUpgrade({ containerID: t.containerID, containerName: name, content: texts[name] }),
      )
      drawnText.set(name, texts[name])
    }
  }

  async function show(first = false): Promise<void> {
    if (view.kind === "list") {
      if (!first) screen = glance(agents, { tab, updatedAt, problem })
      const texts = { "tab-active": screen.tabs.active, "tab-idle": screen.tabs.idle, status: screen.status, clock: screen.clock }
      await draw(`list ${JSON.stringify([screen.tab, screen.items])}`, listLayout(screen), texts, first)
    } else {
      const page = view.dictation ? dictationPage(view.agent, view.dictation) : agentPage(view.agent, view.read)
      await draw(`agent ${view.agent.task_id}`, agentLayout(page), { title: page.title, body: page.body, hint: page.hint })
    }
  }

  // Reading Legato: the list always, and the open agent's conversation.
  let timer: ReturnType<typeof setTimeout> | undefined
  let inFront = true

  async function refresh(): Promise<void> {
    clearTimeout(timer)
    try {
      const answer = await legato.agents()
      if (answer.ok) {
        agents = answer.agents
        updatedAt = new Date()
        problem = undefined
      } else {
        problem = answer.problem
      }
      if (view.kind === "agent") {
        const open = view
        open.agent = agents.find((a) => a.task_id === open.agent.task_id) ?? open.agent
        if (!open.dictation) open.read = await legato.conversation(open.agent.task_id)
      }
      await show()
    } finally {
      // Polling goes on whatever went wrong this time.
      if (inFront) timer = setTimeout(() => void enqueue(refresh), POLL_MS)
    }
  }

  // Dictation: the microphone's sound while listening, and the clock of it.
  let recording: Uint8Array[] = []
  let ticker: ReturnType<typeof setInterval> | undefined

  async function stopMicrophone(): Promise<void> {
    clearInterval(ticker)
    ticker = undefined
    await bridge.audioControl(false)
  }

  async function listen(open: Extract<View, { kind: "agent" }>): Promise<void> {
    recording = []
    open.dictation = { step: "listening", seconds: 0 }
    await show()
    if (!(await bridge.audioControl(true, AudioInputSource.Glasses))) {
      open.dictation = { step: "problem", problem: "The microphone didn't start." }
      return show()
    }
    const started = Date.now()
    ticker = setInterval(
      () =>
        void enqueue(async () => {
          if (view !== open || open.dictation?.step !== "listening") return
          const seconds = Math.floor((Date.now() - started) / 1000)
          if (seconds >= MAX_RECORDING_S) return finishListening(open)
          open.dictation = { step: "listening", seconds }
          await show()
        }),
      1000,
    )
  }

  async function finishListening(open: Extract<View, { kind: "agent" }>): Promise<void> {
    await stopMicrophone()
    const length = recording.reduce((n, chunk) => n + chunk.length, 0)
    if (!length) {
      open.dictation = { step: "problem", problem: "Heard nothing." }
      return show()
    }
    const pcm = new Uint8Array(length)
    let at = 0
    for (const chunk of recording) {
      pcm.set(chunk, at)
      at += chunk.length
    }
    recording = []
    open.dictation = { step: "transcribing" }
    await show()
    const heard = await legato.transcribe(pcm)
    if (view !== open || !open.dictation) return
    open.dictation = heard.ok ? { step: "review", text: heard.text } : { step: "problem", problem: heard.problem }
    await show()
  }

  async function sendDictated(open: Extract<View, { kind: "agent" }>, text: string): Promise<void> {
    open.dictation = { step: "sending", text }
    await show()
    const sent = await legato.send(open.agent.task_id, text)
    if (view !== open) return
    if (!sent.ok) {
      open.dictation = { step: "problem", problem: sent.problem, text }
      return show()
    }
    open.dictation = { step: "sent" }
    await show()
    setTimeout(
      () =>
        void enqueue(async () => {
          if (view !== open || open.dictation?.step !== "sent") return
          open.dictation = undefined
          open.read = await legato.conversation(open.agent.task_id)
          await show()
        }),
      SENT_MS,
    )
  }

  // A press is judged against what was on the glasses when it came, not when
  // its turn in the queue does: a tap made while "Transcribing…" showed must
  // not send the transcript that replaced it, nor a tap on a row open the
  // agent a refresh has since moved there.
  type Seen = { view: View; step?: Dictation["step"]; items: Screen["items"] }
  const seen = (): Seen => ({
    view,
    step: view.kind === "agent" ? view.dictation?.step : undefined,
    items: screen.items,
  })

  async function onPress(kind: "click" | "double", event: EvenHubEvent, at: Seen): Promise<void> {
    if (view !== at.view || (view.kind === "agent" && view.dictation?.step !== at.step)) return
    if (view.kind === "list") {
      if (kind === "double") {
        if (tab === "idle") {
          tab = "active"
          return show()
        }
        await bridge.shutDownPageContainer(1)
        return
      }
      const chosen = event.listEvent
      if (!chosen) return
      // By name, as the glasses showed it; by index only when the SDK gave no name.
      const item = chosen.currentSelectItemName
        ? at.items.find((i) => i.text === chosen.currentSelectItemName)
        : at.items[chosen.currentSelectItemIndex ?? 0]
      if (item?.go) {
        tab = item.go
        return show()
      }
      const agent = item?.open ? agents.find((a) => a.task_id === item.open) : undefined
      if (!agent) return
      const open: View = { kind: "agent", agent }
      view = open
      await show()
      open.read = await legato.conversation(agent.task_id)
      if (view === open) await show()
      return
    }

    const open = view
    const step = open.dictation?.step
    if (kind === "double") {
      if (step === "listening") await stopMicrophone()
      if (open.dictation) {
        open.dictation = undefined
        open.read = await legato.conversation(open.agent.task_id)
      } else {
        view = { kind: "list" }
      }
      return show()
    }
    switch (step) {
      case "problem":
        if (open.dictation?.step === "problem" && open.dictation.text) return sendDictated(open, open.dictation.text)
        return listen(open)
      case undefined:
        return listen(open)
      case "listening":
        return finishListening(open)
      case "review":
        return sendDictated(open, (open.dictation as { text: string }).text)
    }
  }

  await draw(`list ${JSON.stringify([screen.tab, screen.items])}`, listLayout(screen), {
    "tab-active": screen.tabs.active,
    "tab-idle": screen.tabs.idle,
    status: screen.status,
    clock: screen.clock,
  }, true)

  bridge.onEvenHubEvent((event) => {
    // Sound is kept as it comes, outside the queue.
    if (event.audioEvent) {
      if (view.kind === "agent" && view.dictation?.step === "listening") recording.push(new Uint8Array(event.audioEvent.audioPcm))
      return
    }
    switch (event.sysEvent?.eventType) {
      case OsEventTypeList.FOREGROUND_EXIT_EVENT:
        inFront = false
        clearTimeout(timer)
        void enqueue(async () => {
          if (view.kind === "agent" && view.dictation?.step === "listening") {
            await stopMicrophone()
            view.dictation = undefined
          }
        })
        return
      case OsEventTypeList.FOREGROUND_ENTER_EVENT:
        inFront = true
        void enqueue(refresh)
        return
    }
    const kind = press(event)
    if (!kind) return
    const at = seen()
    void enqueue(() => onPress(kind, event, at))
  })

  await enqueue(refresh)
}
