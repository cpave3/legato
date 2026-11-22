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
import { agentPage, dictationPage, entryPage, type Dictation, type FeedItem, type FeedPage, type TextPage } from "./agent"
import { glance, type Screen, type Tab } from "./glance"
import {
  BADGE_WIDTH,
  BORDER,
  CANVAS_WIDTH,
  FRAME_HEIGHT,
  FRAME_HEIGHT_WITH_HINT,
  FRAME_WIDTH,
  FRAME_X,
  FRAME_Y,
  GAP,
  HEADER_HEIGHT,
  HEADER_Y,
  HINT_HEIGHT,
  HINT_Y,
  MARGIN,
  PADDING,
  TITLE_WIDTH,
  TITLE_X,
} from "./layout"
import type { Agent, AgentsResult, ConversationResult, Instance, InstancesResult, SendResult, TranscriptResult } from "./legato"
import { picker, type PickerScreen } from "./picker"

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

// What the app asks of the Legato app: the Legatos saved there, then each
// call to the one chosen.
export type LegatoClient = {
  instances(): Promise<InstancesResult>
  agents(instance: string): Promise<AgentsResult>
  conversation(instance: string, taskId: string): Promise<ConversationResult>
  transcribe(instance: string, pcm: Uint8Array): Promise<TranscriptResult>
  send(instance: string, taskId: string, text: string): Promise<SendResult>
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

// Every page uses the spacing of layout.ts: the header row at the top, a
// bordered frame under it, and on an agent's pages the touchpad's keys
// centred at the foot.
const header = { y: HEADER_Y, height: HEADER_HEIGHT, padding: PADDING }
const TAB_WIDTH = 104
const CLOCK_WIDTH = 64

// The list page: the two tabs, a problem, the time, and the list.
const listBoxes = {
  "tab-active": { id: 1, name: "tab-active", x: MARGIN, width: TAB_WIDTH, brightness: 4, ...header },
  "tab-idle": { id: 2, name: "tab-idle", x: MARGIN + TAB_WIDTH + GAP, width: TAB_WIDTH, brightness: 4, ...header },
  status: {
    id: 3,
    name: "status",
    x: MARGIN + 2 * (TAB_WIDTH + GAP),
    width: CANVAS_WIDTH - MARGIN - CLOCK_WIDTH - GAP - (MARGIN + 2 * (TAB_WIDTH + GAP)),
    brightness: 3,
    ...header,
  },
  clock: { id: 4, name: "clock", x: CANVAS_WIDTH - MARGIN - CLOCK_WIDTH, width: CLOCK_WIDTH, brightness: 2, ...header },
} satisfies Record<string, TextBox>
const LIST = { id: 5, name: "agents" }

// The picker: a title where the tabs are, a problem, the time, and the list
// of Legatos, in the same places as the list page's.
const pickerBoxes = {
  title: { ...listBoxes["tab-active"], name: "title", border: true },
  status: listBoxes.status,
  clock: listBoxes.clock,
} satisfies Record<string, TextBox>
const PICKER_LIST = { id: 5, name: "legatos" }

// An agent's pages: the state in a bordered badge, bright, and the task and
// title beside it, dim; then the feed (a list) or one entry or a message
// (text) in the frame; then the touchpad's keys.
const agentBoxes = {
  badge: { id: 1, name: "badge", x: MARGIN, width: BADGE_WIDTH, brightness: 4, border: true, ...header },
  title: { id: 2, name: "title", x: TITLE_X, width: TITLE_WIDTH, brightness: 2, ...header },
  body: {
    id: 3,
    name: "body",
    x: FRAME_X,
    y: FRAME_Y,
    width: FRAME_WIDTH,
    height: FRAME_HEIGHT_WITH_HINT,
    padding: PADDING,
    brightness: 4,
    border: true,
    capture: true,
  },
  hint: { id: 4, name: "hint", x: 0, y: HINT_Y, width: CANVAS_WIDTH, height: HINT_HEIGHT, padding: 0, brightness: 2 },
} satisfies Record<string, TextBox>
const FEED = { id: 3, name: "feed" }

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
    borderWidth: box.border ? BORDER : 0,
    borderColor: box.border ? 15 : 0,
    borderRadius: box.border ? 6 : 0,
    content: content.length > DRAW_LIMIT ? "" : content,
    textColor: box.brightness,
    isEventCapture: box.capture ? 1 : 0,
  })
}

type Layout = { textObject: TextContainerProperty[]; listObject: ListContainerProperty[] }

// A bordered list in the frame, which takes the touchpad.
function frameList(id: number, name: string, height: number, items: string[]): ListContainerProperty {
  return new ListContainerProperty({
    containerID: id,
    containerName: name,
    xPosition: FRAME_X,
    yPosition: FRAME_Y,
    width: FRAME_WIDTH,
    height,
    borderWidth: BORDER,
    borderColor: 15,
    borderRadius: 6,
    paddingLength: PADDING,
    isEventCapture: 1,
    itemContainer: new ListItemContainerProperty({ itemCount: items.length, isItemSelectBorderEn: 1, itemName: items }),
  })
}

function listLayout(screen: Screen): Layout {
  const selected = (tab: Tab) => ({ border: screen.tab === tab, brightness: screen.tab === tab ? 4 : 2 })
  return {
    textObject: [
      textBox({ ...listBoxes["tab-active"], ...selected("active") }, screen.tabs.active),
      textBox({ ...listBoxes["tab-idle"], ...selected("idle") }, screen.tabs.idle),
      textBox(listBoxes.status, screen.status),
      textBox(listBoxes.clock, screen.clock),
    ],
    listObject: [frameList(LIST.id, LIST.name, FRAME_HEIGHT, screen.items.map((i) => i.text))],
  }
}

function pickerLayout(page: PickerScreen): Layout {
  return {
    textObject: [textBox(pickerBoxes.title, page.title), textBox(pickerBoxes.status, page.status), textBox(pickerBoxes.clock, page.clock)],
    listObject: [frameList(PICKER_LIST.id, PICKER_LIST.name, FRAME_HEIGHT, page.items.map((i) => i.text))],
  }
}

function feedLayout(page: FeedPage): Layout {
  return {
    textObject: [textBox(agentBoxes.badge, page.badge), textBox(agentBoxes.title, page.title), textBox(agentBoxes.hint, page.hint)],
    listObject: [frameList(FEED.id, FEED.name, FRAME_HEIGHT_WITH_HINT, page.items.map((i) => i.text))],
  }
}

function textLayout(page: TextPage): Layout {
  return {
    textObject: [
      textBox(agentBoxes.badge, page.badge),
      textBox(agentBoxes.title, page.title),
      textBox(agentBoxes.body, page.body),
      textBox(agentBoxes.hint, page.hint),
    ],
    listObject: [],
  }
}

type Click = "click" | "double" | "hold" | "release" | undefined

// A touchpad press, wherever the SDK put it: a list's or a text container's
// event, or a system event. A click is type 0, which the SDK may leave out;
// simulator 0.9.5 sends a tap on a text container as {"sysEvent":{"eventSource":1}}.
function press(event: EvenHubEvent): Click {
  const type = event.listEvent ? event.listEvent.eventType : event.textEvent ? event.textEvent.eventType : event.sysEvent?.eventType
  if (!event.listEvent && !event.textEvent && !event.sysEvent) return undefined
  if (type === undefined || type === OsEventTypeList.CLICK_EVENT) return "click"
  if (type === OsEventTypeList.DOUBLE_CLICK_EVENT) return "double"
  if (type === OsEventTypeList.LONG_PRESS_EVENT) return "hold"
  if (type === OsEventTypeList.LONG_PRESS_RELEASE_EVENT) return "release"
  return undefined
}

// The picker of Legatos; a Legato's list of agents; or an agent's view, which
// shows its feed, or one entry of it in full, or a message being dictated.
type View =
  | { kind: "picker" }
  | { kind: "list" }
  | { kind: "agent"; agent: Agent; read?: ConversationResult; entry?: FeedItem; dictation?: Dictation }

// The item a list press chose among the items shown: the one at its index
// when that is the name pressed (two Legatos may share a name), else the
// first of that name, else by index when the SDK gave no name.
function chosenItem<T extends { text: string }>(items: T[], index: number | undefined, name: string | undefined): T | undefined {
  const at = items[index ?? 0]
  if (!name) return at
  return at?.text === name ? at : items.find((i) => i.text === name)
}

// Starts the app: reads the Legatos saved in the Legato app, opens the only
// one or draws the picker, reads the chosen Legato now and every POLL_MS
// while the app is in front, and answers the touchpad. Resolves once the
// first page is shown.
export async function startGlance(bridge: GlassesBridge, legato: LegatoClient): Promise<void> {
  // The Legatos the Legato app listed, the one open, and whether it was
  // picked (so a double tap on its active tab goes back to the picker) or
  // opened as the only one (so it exits).
  let instances: Instance[] | undefined
  let instancesRead = false
  // Whether a list has been read yet: the only Legato is opened straight
  // away on the first list read, not on the first read attempted (which
  // fails until the glasses are paired).
  let listed = false
  let instancesAt: Date | undefined
  // Why the list of Legatos may be stale (a failed read), and why the app
  // left the Legato it had open, which stays until another is opened.
  let pickerProblem: string | undefined
  let leftBecause: string | undefined
  let instance: Instance | undefined
  let picked = false
  let pickerScreen: PickerScreen = picker(undefined)

  let agents: Agent[] = []
  let updatedAt: Date | undefined
  let problem: string | undefined
  let tab: Tab = "active"
  let view: View = { kind: "picker" }
  let screen: Screen = { ...glance([]), items: [{ text: "Reading Legato…" }] }
  // The open agent's feed, as last drawn.
  let feed: FeedItem[] = []

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

  async function draw(key: string, layout: Layout, texts: Partial<Record<string, unknown>>, first = false): Promise<void> {
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
      const content = String(texts[name] ?? "")
      if (drawnText.get(name) === content) continue
      await bridge.textContainerUpgrade(new TextContainerUpgrade({ containerID: t.containerID, containerName: name, content }))
      drawnText.set(name, content)
    }
  }

  async function show(first = false): Promise<void> {
    if (view.kind === "picker") {
      pickerScreen = picker(instances, { problem: pickerProblem ?? leftBecause, read: instancesRead, updatedAt: instancesAt })
      const texts = { title: pickerScreen.title, status: pickerScreen.status, clock: pickerScreen.clock }
      await draw(`picker ${JSON.stringify(pickerScreen.items)}`, pickerLayout(pickerScreen), texts, first)
    } else if (view.kind === "list") {
      if (!first) screen = glance(agents, { tab, updatedAt, problem })
      const texts = { "tab-active": screen.tabs.active, "tab-idle": screen.tabs.idle, status: screen.status, clock: screen.clock }
      await draw(`list ${JSON.stringify([screen.tab, screen.items])}`, listLayout(screen), texts, first)
    } else if (view.dictation || view.entry) {
      const page = view.dictation ? dictationPage(view.agent, view.dictation) : entryPage(view.agent, view.entry!)
      await draw(`text ${view.agent.task_id}`, textLayout(page), page)
    } else {
      const page = agentPage(view.agent, view.read)
      feed = page.items
      await draw(`feed ${view.agent.task_id} ${JSON.stringify(feed)}`, feedLayout(page), page)
    }
  }

  // Reading Legato: the list always, and the open agent's conversation.
  let timer: ReturnType<typeof setTimeout> | undefined
  let inFront = true

  // Opens a Legato's list of agents, fresh.
  function openInstance(chosen: Instance, fromPicker: boolean): void {
    instance = chosen
    picked = fromPicker
    leftBecause = undefined
    agents = []
    updatedAt = undefined
    problem = undefined
    tab = "active"
    screen = { ...glance([]), items: [{ text: "Reading Legato…" }] }
    view = { kind: "list" }
  }

  // Back to the picker, saying why when the open Legato has gone, and reads
  // the list again.
  async function toPicker(why?: string): Promise<void> {
    if (view.kind === "agent" && view.dictation?.step === "listening") await stopMicrophone()
    instance = undefined
    leftBecause = why
    view = { kind: "picker" }
    await show()
    await readInstances()
  }

  async function readInstances(): Promise<void> {
    const answer = await legato.instances()
    if (view.kind !== "picker") return
    instancesRead = true
    if (!answer.ok) {
      pickerProblem = answer.problem
      return
    }
    const first = !listed
    listed = true
    pickerProblem = undefined
    instances = answer.instances
    instancesAt = new Date()
    if (first && instances.length === 1) {
      openInstance(instances[0], false)
      await readInstance()
    }
  }

  async function readInstance(): Promise<void> {
    const at = instance
    if (!at) return
    const answer = await legato.agents(at.id)
    if (instance !== at) return
    if (!answer.ok && answer.gone) return toPicker(answer.problem)
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
      if (!open.dictation) {
        open.read = await legato.conversation(at.id, open.agent.task_id)
        if (!open.read.ok && open.read.gone && view === open) return toPicker(open.read.problem)
      }
    }
  }

  async function refresh(): Promise<void> {
    clearTimeout(timer)
    try {
      if (view.kind === "picker") await readInstances()
      else await readInstance()
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
    const heard = await legato.transcribe(instance?.id ?? "", pcm)
    if (view !== open || !open.dictation) return
    open.dictation = heard.ok ? { step: "review", text: heard.text } : { step: "problem", problem: heard.problem }
    await show()
  }

  async function sendDictated(open: Extract<View, { kind: "agent" }>, text: string): Promise<void> {
    open.dictation = { step: "sending", text }
    await show()
    const sent = await legato.send(instance?.id ?? "", open.agent.task_id, text)
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
          open.entry = undefined
          open.read = await legato.conversation(instance?.id ?? "", open.agent.task_id)
          await show()
        }),
      SENT_MS,
    )
  }

  // A press is judged against what was on the glasses when it came, not when
  // its turn in the queue does: a tap made while "Transcribing…" showed must
  // not send the transcript that replaced it, nor a tap on a row open the
  // agent a refresh has since moved there.
  type Seen = {
    view: View
    step?: Dictation["step"]
    entry?: FeedItem
    items: Screen["items"]
    feed: FeedItem[]
    legatos: PickerScreen["items"]
  }
  const seen = (): Seen => ({
    view,
    step: view.kind === "agent" ? view.dictation?.step : undefined,
    entry: view.kind === "agent" ? view.entry : undefined,
    items: screen.items,
    feed,
    legatos: pickerScreen.items,
  })

  async function onPress(kind: "click" | "double" | "hold", event: EvenHubEvent, at: Seen): Promise<void> {
    if (view !== at.view) return
    if (view.kind === "agent" && (view.dictation?.step !== at.step || view.entry !== at.entry)) return
    if (view.kind === "picker") {
      if (kind === "hold") return
      if (kind === "double") {
        await bridge.shutDownPageContainer(1)
        return
      }
      const chosen = event.listEvent
      if (!chosen) return
      const item = chosenItem(at.legatos, chosen.currentSelectItemIndex, chosen.currentSelectItemName)
      const open = item?.open ? instances?.find((i) => i.id === item.open) : undefined
      if (!open) return
      openInstance(open, true)
      await show()
      await readInstance()
      return show()
    }
    if (view.kind === "list") {
      if (kind === "hold") return
      if (kind === "double") {
        if (tab === "idle") {
          tab = "active"
          return show()
        }
        if (picked) {
          await toPicker()
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
      open.read = await legato.conversation(instance?.id ?? "", agent.task_id)
      if (view === open) await show()
      return
    }

    const open = view
    const step = open.dictation?.step
    // Double tap goes back one step: a message is dropped, an entry closed,
    // the feed left for the list.
    if (kind === "double") {
      if (step === "listening") await stopMicrophone()
      if (open.dictation) {
        open.dictation = undefined
        open.read = await legato.conversation(instance?.id ?? "", open.agent.task_id)
      } else if (open.entry) {
        open.entry = undefined
      } else {
        view = { kind: "list" }
      }
      return show()
    }
    // Hold to talk: from the feed, an entry, or a problem.
    if (kind === "hold") {
      if (step === undefined || step === "problem") return listen(open)
      return
    }
    switch (step) {
      case "problem":
        if (open.dictation?.step === "problem" && open.dictation.text) return sendDictated(open, open.dictation.text)
        return
      case "listening":
        // The release stops it; a tap does too, should a release not come.
        return finishListening(open)
      case "review":
        return sendDictated(open, (open.dictation as { text: string }).text)
      case undefined: {
        // A tap on a line of the feed opens it, by its name as shown.
        const chosen = event.listEvent
        if (open.entry || !chosen) return
        const entry = chosen.currentSelectItemName
          ? at.feed.find((i) => i.text === chosen.currentSelectItemName)
          : at.feed[chosen.currentSelectItemIndex ?? 0]
        if (!entry?.detail) return
        open.entry = entry
        return show()
      }
    }
  }

  await show(true)

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
    // A release ends whatever recording is going when its turn comes: the
    // hold that started it is ahead of it in the queue.
    if (kind === "release") {
      void enqueue(async () => {
        if (view.kind === "agent" && view.dictation?.step === "listening") await finishListening(view)
      })
      return
    }
    const at = seen()
    void enqueue(() => onPress(kind, event, at))
  })

  await enqueue(refresh)
}
