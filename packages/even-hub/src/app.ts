import {
  AudioInputSource,
  CreateStartUpPageContainer,
  DeviceModel,
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
import { APPROVAL_BODY_LINES, approvalPage, approvalsPage, decisionPage, type ApprovalPage, type Decision } from "./approvals"
import { ENDED, agentPage, dictationPage, entryPage, entryShift, followEntry, sameEntry, type Dictation, type FeedItem, type FeedPage, type TextPage } from "./agent"
import { ALERT_WIDTH, COUNTS_WIDTH, background, type BackgroundPage } from "./background"
import { glance, type Screen, type Tab } from "./glance"
import {
  BADGE_WIDTH,
  BORDER,
  CANVAS_WIDTH,
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
  boxHeight,
  center,
  inner,
  right,
} from "./layout"
import { REEVE_ENTRIES, type Agent, type AgentsResult, type Approval, type ApprovalsResult, type ConversationResult, type Instance, type InstancesResult, type SendResult, type TranscriptResult } from "./legato"
import { picker, type PickerScreen } from "./picker"
import { heard, reeveChat, reeveDictation, type ChatPage } from "./reeve"

// How often the app reads Legato while it is in front, and while it is not.
// On real glasses the screen going off sends FOREGROUND_EXIT, but waking it
// sends nothing (opening the G2 menu sends FOREGROUND_ENTER), so the app
// cannot wait for the wake: it reads on, as often as the phone page says
// (settings.ts; every POLL_MS unless the operator saves the phone reads), to
// keep what the glasses show on waking fresh, and to retry a page they
// refused to draw. BACKGROUND_POLL_MS is the pace when startGlance is told none.
export const POLL_MS = 5000
export const BACKGROUND_POLL_MS = 30_000
// How often the page in front looks at how old what it shows is: the list
// beside its clock, an agent's pages and the Reeve's chat at the end of their
// title, once that is a minute or more. The glasses are sent it only when it
// changes, so not at all while reads succeed.
const AGE_TICK_MS = 30_000
// The longest message it records, and how long "Sent" stays up. The sound
// kept is cut at a minute's worth (16 kHz mono s16le) however fast it comes
// (the simulator's null microphone sends it as fast as it can), so a message
// always fits what the Legato app takes.
const MAX_RECORDING_S = 60
const MAX_RECORDING_BYTES = MAX_RECORDING_S * 16_000 * 2
const SENT_MS = 1500
// How long the Even app may take to say which glasses these are.
const DEVICE_TIMEOUT_MS = 2000

// The parts of the SDK's bridge the app uses.
export type GlassesBridge = Pick<
  EvenAppBridge,
  | "createStartUpPageContainer"
  | "rebuildPageContainer"
  | "textContainerUpgrade"
  | "onEvenHubEvent"
  | "shutDownPageContainer"
  | "audioControl"
  | "getDeviceInfo"
  | "onDeviceStatusChanged"
>

// What the app asks of the Legato app: the Legatos saved there, then each
// call to the one chosen.
export type LegatoClient = {
  instances(): Promise<InstancesResult>
  agents(instance: string): Promise<AgentsResult>
  conversation(instance: string, taskId: string, limit?: number): Promise<ConversationResult>
  transcribe(instance: string, pcm: Uint8Array): Promise<TranscriptResult>
  send(instance: string, taskId: string, text: string): Promise<SendResult>
  // A message to the instance's Reeve, dictated on the glasses.
  reeve(instance: string, text: string): Promise<SendResult>
  // The approvals waiting, and a decision on one (never one that must be signed).
  approvals(instance: string): Promise<ApprovalsResult>
  decide(instance: string, id: string, decision: "allow" | "deny", option?: string): Promise<SendResult>
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
// Wide enough for how old the list is and the time: "59m · 23:59".
const CLOCK_WIDTH = 120

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

// The list page and the picker say what the touchpad does on the same line
// at the foot as an agent's pages.
const footHint = agentBoxes.hint

// The Reeve's chat: the agents' header, what the operator said last on a dim
// line at the top of the frame's place, the reply framed under it (it takes
// the touchpad, and scrolls when long), and the keys.
const ASKED_HEIGHT = boxHeight(PADDING)
const REPLY_Y = FRAME_Y + ASKED_HEIGHT + GAP
const chatBoxes = {
  badge: agentBoxes.badge,
  title: agentBoxes.title,
  asked: { id: 3, name: "asked", x: FRAME_X, y: FRAME_Y, width: FRAME_WIDTH, height: ASKED_HEIGHT, padding: PADDING, brightness: 2 },
  reply: { ...agentBoxes.body, id: 4, name: "reply", y: REPLY_Y, height: HINT_Y - GAP - REPLY_Y },
  hint: { ...agentBoxes.hint, id: 5 },
} satisfies Record<string, TextBox>

// Background mode: one row across the top, who needs the operator bright on
// the left, the counts and battery dim on the right; the counts take the
// touchpad.
const backgroundBoxes = {
  alert: { id: 1, name: "alert", x: MARGIN, y: HEADER_Y, width: ALERT_WIDTH, height: boxHeight(PADDING), padding: PADDING, brightness: 4 },
  counts: {
    id: 2,
    name: "counts",
    x: MARGIN + ALERT_WIDTH + GAP,
    y: HEADER_Y,
    width: COUNTS_WIDTH,
    height: boxHeight(PADDING),
    padding: PADDING,
    brightness: 2,
    capture: true,
  },
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
function frameList(id: number, name: string, height: number, items: string[], y = FRAME_Y): ListContainerProperty {
  return new ListContainerProperty({
    containerID: id,
    containerName: name,
    xPosition: FRAME_X,
    yPosition: y,
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

function listLayout(screen: Screen, hint: string): Layout {
  const selected = (tab: Tab) => ({ border: screen.tab === tab, brightness: screen.tab === tab ? 4 : 2 })
  return {
    textObject: [
      textBox({ ...listBoxes["tab-active"], ...selected("active") }, screen.tabs.active),
      textBox({ ...listBoxes["tab-idle"], ...selected("idle") }, screen.tabs.idle),
      textBox(listBoxes.status, screen.status),
      textBox(listBoxes.clock, screen.clock),
      textBox({ ...footHint, id: 6 }, hint),
    ],
    listObject: [frameList(LIST.id, LIST.name, FRAME_HEIGHT_WITH_HINT, screen.items.map((i) => i.text))],
  }
}

function pickerLayout(page: PickerScreen, hint: string): Layout {
  return {
    textObject: [
      textBox(pickerBoxes.title, page.title),
      textBox(pickerBoxes.status, page.status),
      textBox(pickerBoxes.clock, page.clock),
      textBox({ ...footHint, id: 6 }, hint),
    ],
    listObject: [frameList(PICKER_LIST.id, PICKER_LIST.name, FRAME_HEIGHT_WITH_HINT, page.items.map((i) => i.text))],
  }
}

function chatLayout(page: ChatPage): Layout {
  return {
    textObject: [
      textBox(chatBoxes.badge, page.badge),
      textBox(chatBoxes.title, page.title),
      textBox(chatBoxes.asked, page.asked),
      textBox(chatBoxes.reply, page.reply),
      textBox(chatBoxes.hint, page.hint),
    ],
    listObject: [],
  }
}

// An approval the glasses may decide: what it is, in brief, at the top of
// the frame's place, bright, and its options framed under it.
const APPROVAL_BODY_HEIGHT = APPROVAL_BODY_LINES * 27 + 2 * PADDING + 1
const OPTIONS_Y = FRAME_Y + APPROVAL_BODY_HEIGHT + GAP
const approvalBoxes = {
  badge: agentBoxes.badge,
  title: agentBoxes.title,
  body: { id: 3, name: "body", x: FRAME_X, y: FRAME_Y, width: FRAME_WIDTH, height: APPROVAL_BODY_HEIGHT, padding: PADDING, brightness: 4 },
  hint: { ...agentBoxes.hint, id: 5 },
} satisfies Record<string, TextBox>

function approvalLayout(page: ApprovalPage): Layout {
  return {
    textObject: [
      textBox(approvalBoxes.badge, page.badge),
      textBox(approvalBoxes.title, page.title),
      textBox(approvalBoxes.body, page.body),
      textBox(approvalBoxes.hint, page.hint),
    ],
    listObject: [frameList(4, "options", HINT_Y - GAP - OPTIONS_Y, page.options, OPTIONS_Y)],
  }
}

function backgroundLayout(page: BackgroundPage): Layout {
  return { textObject: [textBox(backgroundBoxes.alert, page.alert), textBox(backgroundBoxes.counts, page.counts)], listObject: [] }
}

const keys = (text: string) => center(text, CANVAS_WIDTH)

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

// The picker of Legatos; a Legato's list of agents, or its counts folded
// into a row across the top (background mode); the Reeve's chat, with what was just sent
// to it (`pending`) until its conversation has it; or an agent's view, which
// shows its feed, or one entry of it in full, or a message being dictated.
// An agent's view opened from the Reeve's chat (its history) goes back there.
// A conversation as a view keeps it: the last answer, when it was read, and
// why the latest read failed when one before it worked (that one stays up).
type Reading = { read?: ConversationResult; readAt?: Date; problem?: string }
type ReeveView = Reading & { kind: "reeve"; dictation?: Dictation; pending?: string }
type AgentView = Reading & {
  kind: "agent"
  agent: Agent
  entry?: FeedItem
  dictation?: Dictation
  back?: ReeveView
}
// The approvals waiting; one of them, opened from that list or from an
// agent's feed (`back`), with a decision on it under way.
type ApprovalsView = { kind: "approvals" }
type ApprovalView = { kind: "approval"; approval: Approval; decision?: Decision; back: ApprovalsView | AgentView }
type View = { kind: "picker" } | { kind: "list" } | { kind: "background" } | ReeveView | AgentView | ApprovalsView | ApprovalView
// The views a message is dictated from.
type Talking = ReeveView | AgentView
const talking = (v: View): v is Talking => v.kind === "agent" || v.kind === "reeve"
const listening = (v: View): v is Talking => talking(v) && v.dictation?.step === "listening"

// Keeps an answer in a view, holding on to the last one that worked when
// this one failed.
function took(v: Reading, read: ConversationResult | undefined): void {
  if (read?.ok) {
    v.read = read
    v.readAt = new Date()
    v.problem = undefined
  } else if (read && v.read?.ok) {
    v.problem = read.problem
  } else {
    v.read = read
    v.readAt = undefined
    v.problem = undefined
  }
}
const freshness = (v: Reading) => ({ readAt: v.readAt, problem: v.problem, now: new Date() })

// Keeps an answer in an agent's view, and has an entry opened from its feed
// follow what it shows (a reply growing, a call ending), so an open entry is
// as live as the feed.
function tookFeed(v: AgentView, read: ConversationResult | undefined): void {
  const before = v.read
  took(v, read)
  if (!v.entry || !read?.ok) return
  const shift = before?.ok ? entryShift(before.conversation, read.conversation) : undefined
  v.entry = followEntry(v.entry, agentPage(v.agent, v.read), shift)
}

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
export type GlanceOptions = {
  // How often to read while the glasses' screen is off (out of the
  // foreground), asked each time; undefined reads nothing then. The phone
  // page sets it (settings.ts). BACKGROUND_POLL_MS when not given.
  screenOffReadMs?: () => number | undefined
}

export async function startGlance(bridge: GlassesBridge, legato: LegatoClient, options: GlanceOptions = {}): Promise<void> {
  const screenOffReadMs = options.screenOffReadMs ?? (() => BACKGROUND_POLL_MS)
  // The next read: soon while in front, as the phone page says while not.
  const scheduleRead = () => {
    const after = inFront ? POLL_MS : screenOffReadMs()
    if (after !== undefined) timer = setTimeout(() => void enqueue(refresh), after)
  }
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
  let approvals: Approval[] = []
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
        // The glasses take a first page once a session: after the app's page
        // reloads (the dev server, or the WebView) they refuse another, and
        // the page still up is rebuilt instead.
        if (result !== StartUpPageCreateResult.success && !(await bridge.rebuildPageContainer(new RebuildPageContainer(page)))) {
          throw new Error(`the glasses refused the page (result ${result})`)
        }
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
      // The glasses ignore an update to empty text and keep the old one, so
      // a box is cleared with a space.
      await bridge.textContainerUpgrade(
        new TextContainerUpgrade({ containerID: t.containerID, containerName: name, content: content || " " }),
      )
      drawnText.set(name, content)
    }
  }

  // The Reeve's running agent, if any.
  const reeveAgent = () => agents.find((a) => a.reeve && a.status === "running")

  async function show(first = false): Promise<void> {
    if (view.kind === "picker") {
      pickerScreen = picker(instances, { problem: pickerProblem ?? leftBecause, read: instancesRead, updatedAt: instancesAt })
      const hint = keys("Tap: open · Double tap: exit")
      // The time at the right of its box, where it was before the box grew.
      pickerScreen = { ...pickerScreen, clock: right(pickerScreen.clock, inner(CLOCK_WIDTH)) }
      const texts = { title: pickerScreen.title, status: pickerScreen.status, clock: pickerScreen.clock, hint }
      await draw(`picker ${JSON.stringify(pickerScreen.items)}`, pickerLayout(pickerScreen, hint), texts, first)
    } else if (view.kind === "list") {
      if (!first) screen = glance(agents, { tab, updatedAt, now: new Date(), problem, approvals: approvals.length })
      screen = { ...screen, clock: right(screen.clock, inner(CLOCK_WIDTH)) }
      const hint = keys(`Tap: open · Hold: background · Double tap: ${tab === "idle" || picked ? "back" : "exit"}`)
      const texts = { "tab-active": screen.tabs.active, "tab-idle": screen.tabs.idle, status: screen.status, clock: screen.clock, hint }
      await draw(`list ${JSON.stringify([screen.tab, screen.items])}`, listLayout(screen, hint), texts, first)
    } else if (view.kind === "approvals") {
      const page = approvalsPage(approvals, instance?.name ?? "")
      feed = page.items
      await draw(`approvals ${JSON.stringify(feed)}`, feedLayout(page), page)
    } else if (view.kind === "approval") {
      const { approval, decision } = view
      if (decision) {
        const page = decisionPage(approval, decision)
        await draw(`text approval ${approval.id}`, textLayout(page), page)
      } else {
        const page = approvalPage(approval)
        if (page.options.length) await draw(`approval ${approval.id}`, approvalLayout(page), page)
        else await draw(`text approval ${approval.id}`, textLayout(page), page)
      }
    } else if (view.kind === "background") {
      const page = background(agents, { problem, battery: glasses.battery, charging: glasses.charging })
      await draw("background", backgroundLayout(page), page)
    } else if (view.kind === "reeve") {
      const name = instance?.name ?? ""
      if (view.dictation) {
        const page = reeveDictation(name, view.dictation)
        await draw("text reeve", textLayout(page), page)
      } else {
        const page = reeveChat(reeveAgent(), view.read, { instance: name, pending: view.pending, ...freshness(view) })
        await draw("reeve", chatLayout(page), page)
      }
    } else if (view.dictation || view.entry) {
      const page = view.dictation ? dictationPage(view.agent, view.dictation) : entryPage(view.agent, view.entry!, freshness(view))
      await draw(`text ${view.agent.task_id}`, textLayout(page), page)
    } else {
      const page = agentPage(view.agent, view.read, freshness(view))
      feed = page.items
      await draw(`feed ${view.agent.task_id} ${JSON.stringify(feed)}`, feedLayout(page), page)
    }
  }

  // The glasses' serial number and battery, from the Even app: background
  // mode shows the battery. A status of another device (the ring) is not theirs.
  const glasses: { sn?: string; battery?: number; charging?: boolean } = {}
  async function readDevice(): Promise<void> {
    // Within a limit: every press waits behind it in the queue.
    const late = new Promise<null>((done) => setTimeout(() => done(null), DEVICE_TIMEOUT_MS))
    const device = await Promise.race([bridge.getDeviceInfo().catch(() => null), late])
    if (!device || !DeviceModel.isGlasses(device.model)) return
    glasses.sn = device.sn
    glasses.battery = device.status?.batteryLevel
    glasses.charging = device.status?.isCharging
  }

  // Reading Legato: the list always, and the open agent's conversation.
  let timer: ReturnType<typeof setTimeout> | undefined
  let inFront = true
  // When the last read started, to tell whether what is shown may be stale.
  let readAt = 0

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
    if (listening(view)) await stopMicrophone()
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
    // The agents, the approvals and the open conversation are read at once,
    // so one slow call holds back none of the others. The Reeve's chat reads
    // the Reeve the last list had, and reads again should this list name
    // another.
    const feed = view.kind === "agent" && !view.dictation ? view : undefined
    const chat = view.kind === "reeve" && !view.dictation ? view : undefined
    const rv = chat ? reeveAgent() : undefined
    const [answer, waiting, read, chatRead] = await Promise.all([
      legato.agents(at.id),
      legato.approvals(at.id),
      feed ? legato.conversation(at.id, feed.agent.task_id) : undefined,
      rv ? legato.conversation(at.id, rv.task_id, REEVE_ENTRIES) : undefined,
    ])
    if (instance !== at) return
    if (!answer.ok && answer.gone) return toPicker(answer.problem)
    if (answer.ok) {
      agents = answer.agents
      updatedAt = new Date()
      problem = undefined
    } else {
      problem = answer.problem
    }
    // The approvals waiting; the last list stays when a read fails.
    if (waiting.ok) approvals = waiting.approvals
    // An approval decided somewhere else meanwhile is left, unless the
    // glasses' own decision is under way or done.
    const step = view.kind === "approval" ? view.decision?.step : undefined
    if (view.kind === "approval" && step !== "deciding" && step !== "done" && !approvals.some((a) => a.id === (view as ApprovalView).approval.id)) {
      view = view.back
    }
    // The open agent as listed now; one Legato no longer lists as running
    // has ended, and says so rather than the state it had.
    if (view.kind === "agent" && answer.ok) {
      const open = view
      open.agent = agents.find((a) => a.task_id === open.agent.task_id) ?? { ...open.agent, status: ENDED }
    }
    if (feed && read && view === feed) {
      if (!read.ok && read.gone) return toPicker(read.problem)
      tookFeed(feed, read)
    }
    if (chat && view === chat) {
      const answer = reeveAgent()?.task_id === rv?.task_id ? chatRead : await reeveRead()
      if (answer && !answer.ok && answer.gone) return toPicker(answer.problem)
      settleReeve(chat, answer)
    }
  }

  // The Reeve's conversation, when it is running; the message just sent is
  // let go once the conversation has it.
  async function readReeve(open: ReeveView): Promise<void> {
    settleReeve(open, await reeveRead())
  }

  async function reeveRead(): Promise<ConversationResult | undefined> {
    const rv = reeveAgent()
    return rv ? await legato.conversation(instance?.id ?? "", rv.task_id, REEVE_ENTRIES) : undefined
  }

  function settleReeve(open: ReeveView, read: ConversationResult | undefined): void {
    took(open, read)
    if (open.pending && heard(open.read, open.pending)) open.pending = undefined
  }

  async function refresh(): Promise<void> {
    clearTimeout(timer)
    readAt = Date.now()
    try {
      if (view.kind === "picker") await readInstances()
      else await readInstance()
      await show()
    } finally {
      // Polling goes on whatever went wrong this time.
      scheduleRead()
    }
  }

  // Dictation: the microphone's sound while listening, and the clock of it.
  let recording: Uint8Array[] = []
  let recorded = 0
  let ticker: ReturnType<typeof setInterval> | undefined

  async function stopMicrophone(): Promise<void> {
    clearInterval(ticker)
    ticker = undefined
    await bridge.audioControl(false)
  }

  async function listen(open: Talking): Promise<void> {
    recording = []
    recorded = 0
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

  async function finishListening(open: Talking): Promise<void> {
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
    const asked: Dictation = { step: "transcribing" }
    open.dictation = asked
    await show()
    // Outside the queue, so a double tap drops the message at once and reads
    // go on; the words come back through it, and only to the same page.
    const failed: TranscriptResult = { ok: false, problem: "Transcription failed." }
    void legato.transcribe(instance?.id ?? "", pcm).catch(() => failed).then((heard) =>
      enqueue(async () => {
        if (view !== open || open.dictation !== asked) return
        open.dictation = heard.ok ? { step: "review", text: heard.text } : { step: "problem", problem: heard.problem }
        await show()
      }),
    )
  }

  async function sendDictated(open: Talking, text: string): Promise<void> {
    open.dictation = { step: "sending", text }
    await show()
    const at = instance?.id ?? ""
    const sent = open.kind === "reeve" ? await legato.reeve(at, text) : await legato.send(at, open.agent.task_id, text)
    if (view !== open) return
    if (!sent.ok) {
      open.dictation = { step: "problem", problem: sent.problem, text }
      return show()
    }
    open.dictation = { step: "sent" }
    if (open.kind === "reeve") open.pending = text
    await show()
    setTimeout(
      () =>
        void enqueue(async () => {
          if (view !== open || open.dictation?.step !== "sent") return
          open.dictation = undefined
          if (open.kind === "reeve") {
            // A Reeve the message woke is running now.
            await readInstance()
          } else {
            open.entry = undefined
            took(open, await legato.conversation(at, open.agent.task_id))
          }
          if (view === open) await show()
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
    step?: Dictation["step"] | Decision["step"]
    entry?: FeedItem
    items: Screen["items"]
    feed: FeedItem[]
    legatos: PickerScreen["items"]
  }
  const seen = (): Seen => ({
    view,
    step: talking(view) ? view.dictation?.step : view.kind === "approval" ? view.decision?.step : undefined,
    entry: view.kind === "agent" ? view.entry : undefined,
    items: screen.items,
    feed,
    legatos: pickerScreen.items,
  })

  async function onPress(kind: "click" | "double" | "hold", event: EvenHubEvent, at: Seen): Promise<void> {
    if (view !== at.view) return
    // An entry a read followed since is still the entry the press was made on.
    if (view.kind === "agent" && (view.dictation?.step !== at.step || !sameEntry(view.entry, at.entry))) return
    if (view.kind === "reeve" && view.dictation?.step !== at.step) return
    if (view.kind === "approval" && view.decision?.step !== at.step) return
    if (view.kind === "approvals") return onApprovalsPress(kind, event, at)
    if (view.kind === "approval") return onApprovalPress(view, kind, event)
    if (view.kind === "background") {
      // Only a hold unfolds it: a brush of the temple leaves it be.
      if (kind !== "hold") return
      view = { kind: "list" }
      await show()
      // Fresh as soon as it unfolds.
      return refresh()
    }
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
      if (kind === "hold") {
        view = { kind: "background" }
        await show()
        return refresh()
      }
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
      if (item?.approvals) {
        view = { kind: "approvals" }
        return show()
      }
      if (item?.reeve) {
        const open: ReeveView = { kind: "reeve" }
        view = open
        await show()
        await readReeve(open)
        if (view === open) await show()
        return
      }
      const agent = item?.open ? agents.find((a) => a.task_id === item.open) : undefined
      if (!agent) return
      const open: View = { kind: "agent", agent }
      view = open
      await show()
      took(open, await legato.conversation(instance?.id ?? "", agent.task_id))
      if (view === open) await show()
      return
    }

    const open = view
    const step = open.dictation?.step
    // Double tap goes back one step: a message is dropped, an entry closed,
    // the feed left for the list (or for the Reeve's chat it was opened
    // from), the chat left for the list.
    if (kind === "double") {
      if (step === "listening") await stopMicrophone()
      if (open.dictation) {
        open.dictation = undefined
        if (open.kind === "reeve") await readReeve(open)
        else tookFeed(open, await legato.conversation(instance?.id ?? "", open.agent.task_id))
      } else if (open.kind === "agent" && open.entry) {
        // Back on the feed at once, then as it is now.
        open.entry = undefined
        await show()
        took(open, await legato.conversation(instance?.id ?? "", open.agent.task_id))
        if (view !== open) return
      } else if (open.kind === "agent" && open.back) {
        view = open.back
        await readReeve(open.back)
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
        if (open.kind === "reeve") {
          // A tap on the chat opens the Reeve's whole conversation.
          const rv = reeveAgent()
          if (!rv) return
          const history: AgentView = { kind: "agent", agent: rv, back: open }
          view = history
          await show()
          took(history, await legato.conversation(instance?.id ?? "", rv.task_id))
          if (view === history) await show()
          return
        }
        // A tap on a line of the feed opens it, by its name as shown.
        const chosen = event.listEvent
        if (open.entry || !chosen) return
        const entry = chosen.currentSelectItemName
          ? at.feed.find((i) => i.text === chosen.currentSelectItemName)
          : at.feed[chosen.currentSelectItemIndex ?? 0]
        if (!entry?.detail) return
        // What the agent asks opens its approval, when Legato has one.
        const asked = entry.asks
          ? approvals.find((a) => a.task_id === open.agent.task_id && a.source.startsWith("agent_"))
          : undefined
        if (asked) {
          view = { kind: "approval", approval: asked, back: open }
          return show()
        }
        open.entry = entry
        return show()
      }
    }
  }

  // The approvals list: a tap opens one, by its line as shown; double tap
  // goes back to the agents.
  async function onApprovalsPress(kind: "click" | "double" | "hold", event: EvenHubEvent, at: Seen): Promise<void> {
    if (kind === "double") {
      view = { kind: "list" }
      return show()
    }
    const chosen = event.listEvent
    if (kind !== "click" || !chosen) return
    const item = chosen.currentSelectItemName
      ? at.feed.find((i) => i.text === chosen.currentSelectItemName)
      : at.feed[chosen.currentSelectItemIndex ?? 0]
    const approval = approvals.find((a) => a.id === item?.detail)
    if (!approval) return
    view = { kind: "approval", approval, back: view as ApprovalsView }
    return show()
  }

  // An approval: a tap on an option asks to confirm it, a second tap
  // decides; double tap goes back a step. Nothing is decided on one tap.
  async function onApprovalPress(open: ApprovalView, kind: "click" | "double" | "hold", event: EvenHubEvent): Promise<void> {
    const step = open.decision?.step
    if (kind === "hold" || step === "deciding" || step === "done") return
    if (kind === "double") {
      if (open.decision) open.decision = undefined
      else view = open.back
      return show()
    }
    if (step === "confirm" || step === "problem") return decide(open, open.decision!.option)
    const page = approvalPage(open.approval)
    const chosen = event.listEvent
    if (!page.options.length || !chosen) return
    const index = chosen.currentSelectItemName ? page.options.indexOf(chosen.currentSelectItemName) : (chosen.currentSelectItemIndex ?? 0)
    const option = open.approval.options[index]
    if (!option) return
    open.decision = { step: "confirm", option }
    return show()
  }

  async function decide(open: ApprovalView, option: Approval["options"][number]): Promise<void> {
    open.decision = { step: "deciding", option }
    await show()
    const decision = option.kind === "deny" ? "deny" : "allow"
    const done = await legato.decide(instance?.id ?? "", open.approval.id, decision, option.id)
    if (view !== open) return
    open.decision = done.ok ? { step: "done", option } : { step: "problem", option, problem: done.problem }
    await show()
    if (!done.ok) return
    setTimeout(
      () =>
        void enqueue(async () => {
          if (view !== open) return
          approvals = approvals.filter((a) => a.id !== open.approval.id)
          view = open.back
          await readInstance()
          await show()
        }),
      SENT_MS,
    )
  }

  await show(true)

  bridge.onDeviceStatusChanged((status) => {
    if (!glasses.sn || status.sn !== glasses.sn) return
    void enqueue(async () => {
      glasses.battery = status.batteryLevel ?? glasses.battery
      glasses.charging = status.isCharging ?? glasses.charging
      if (view.kind === "background") await show()
    })
  })

  bridge.onEvenHubEvent((event) => {
    // Sound is kept as it comes, outside the queue.
    if (event.audioEvent) {
      if (listening(view) && recorded < MAX_RECORDING_BYTES) {
        const chunk = new Uint8Array(event.audioEvent.audioPcm).subarray(0, MAX_RECORDING_BYTES - recorded)
        recording.push(chunk)
        recorded += chunk.length
      }
      return
    }
    switch (event.sysEvent?.eventType) {
      case OsEventTypeList.FOREGROUND_EXIT_EVENT:
        inFront = false
        clearTimeout(timer)
        scheduleRead()
        void enqueue(async () => {
          if (listening(view)) {
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
    // Waking the screen sends nothing, so anything else the glasses send (a
    // press, a scroll) says the operator is looking: reading goes back to
    // every POLL_MS, at once when the last read is older than that.
    if (!inFront) {
      inFront = true
      clearTimeout(timer)
      if (Date.now() - readAt >= POLL_MS) void enqueue(refresh)
      else scheduleRead()
    }
    const kind = press(event)
    if (!kind) return
    // A release ends whatever recording is going when its turn comes: the
    // hold that started it is ahead of it in the queue.
    if (kind === "release") {
      void enqueue(async () => {
        if (listening(view)) await finishListening(view)
      })
      return
    }
    const at = seen()
    void enqueue(() => onPress(kind, event, at))
  })

  // Every page that says how old it is says it again; only what changed is sent.
  setInterval(() => void enqueue(() => show()), AGE_TICK_MS)
  await enqueue(refresh)
  // After the first read, so the agents are not kept waiting for it.
  void enqueue(readDevice)
}
