// The Legato mod for Claude Code.
//
// A Claude Code session cannot listen for connections, so Legato cannot call
// it the way it calls a Chimera session. Inside a session Legato launched,
// this module spawns `legato agent bridge` for the session's life: it reads
// commands from the bridge's stdout (a prompt to submit, a turn to interrupt)
// and posts the engine's events to it over loopback HTTP. The bridge turns
// those into the native agent protocol Legato's panel and message paths
// already speak. Outside a Legato session the module does nothing.
import type { EngineInterface, PermissionRequestDecision, Register } from 'claude-code'

type BridgeEvent = { type: string; [field: string]: unknown }

type Command =
  | { type: 'ready'; url: string }
  | { type: 'submit'; text: string }
  | { type: 'queue'; text: string }
  | { type: 'append'; text: string }
  | { type: 'interrupt' }
  | { type: 'viewers'; count: number }
  | { type: 'model'; model: string | null }
  | { type: 'effort'; effort: Effort | null }

type Effort = 'low' | 'medium' | 'high' | 'xhigh' | 'max'

type PermissionAnswer = {
  // wait: still open; the bridge answers before the host ends the call,
  // and the mod asks again with the same request ID.
  decision: 'allow' | 'deny' | 'ask' | 'wait'
  feedback?: string
  // What the model reads for a deny, when the bridge worded it.
  message?: string
  // A question's answers, by the question's own words.
  answers?: Record<string, string>
  // The allow hands the tool's input back: the engine accepts a plan no
  // other way.
  returnInput?: boolean
  // Permission changes the allow applies, in the engine's own form.
  updatedPermissions?: Allow['updatedPermissions']
}

type Allow = Extract<PermissionRequestDecision, { behavior: 'allow' }>

type Block = { type: string; [field: string]: unknown }

const JSON_HEADERS = { 'content-type': 'application/json' }

// What one text reaches the bridge with at most. The model read the whole
// thing; the panel only has to show enough to follow along.
const TEXT_LIMIT = 100_000
const ARGUMENT_LIMIT = 20_000
const RESULT_LIMIT = 64_000

// What the description of a session carries of its transcript so far: the
// newest messages that fit, each cut shorter than a live one. A long session
// described whole would be larger than the bridge accepts in one request.
const SEED_MESSAGES = 1_000
const SEED_BUDGET = 2_000_000
const SEED_TEXT_LIMIT = 20_000
const SEED_DETAIL_LIMIT = 4_000

// How a message folded into a running turn opens. The engine stores such a
// row as one the person did not type, so the frame says where it came from.
const NOTE_FRAME = 'Message received through Legato while you were working:'

const cap = (text: string, limit: number): string =>
  text.length <= limit ? text : `${text.slice(0, limit)}\n… (truncated)`

const capArguments = (input: unknown, limit = ARGUMENT_LIMIT): unknown => {
  if (input === null || typeof input !== 'object' || Array.isArray(input)) {
    return input
  }

  return Object.fromEntries(
    Object.entries(input).map(([key, value]) => [
      key,
      typeof value === 'string' ? cap(value, limit) : value,
    ]),
  )
}

// A tool result's content is a string or a list of blocks; the bridge reads
// its text alone, so media never crosses.
const resultText = (content: unknown): string => {
  if (typeof content === 'string') {
    return content
  }

  if (!Array.isArray(content)) {
    return ''
  }

  return content
    .map(part => (part?.type === 'text' && typeof part.text === 'string' ? part.text : ''))
    .join('')
}

const slimBlock = (block: Block): Block | undefined => {
  if (block.type === 'text' && typeof block.text === 'string') {
    return { type: 'text', text: cap(block.text, TEXT_LIMIT) }
  }

  // What the model lets be read of its thinking, which is often nothing; the
  // block's signature is the model's and stays in the session. An empty block
  // still goes: it is what settles a thought that streamed.
  if (block.type === 'thinking') {
    return { type: 'thinking', thinking: typeof block.thinking === 'string' ? cap(block.thinking, TEXT_LIMIT) : '' }
  }

  if (block.type === 'tool_use') {
    return { type: 'tool_use', id: block.id, name: block.name, input: capArguments(block.input) }
  }

  if (block.type === 'tool_result') {
    return {
      type: 'tool_result',
      tool_use_id: block.tool_use_id,
      is_error: block.is_error === true,
      content: cap(resultText(block.content), RESULT_LIMIT),
    }
  }

  return undefined
}

// The module's state. It is the session's for as long as this copy of the
// module is loaded; a reload starts a new bridge and so starts it over.
//
// `isCollecting` is true from the moment the session starts until its bridge
// is gone: events are queued from then on, so nothing that happens while the
// bridge is still starting is lost. `endpoint` is the bridge's URL once it has
// announced itself, which is when the queue starts to drain.
let isCollecting = false
let isBridgeRunning = false
// Counts the bridges this module has had. A bridge that was told to go is no
// longer the module's: what it still prints, and its exit, change nothing.
let bridgeRun = 0
let transitionRun = 0
let endpoint: string | undefined
let queue: BridgeEvent[] = []
let wake: (() => void) | undefined
let viewers = 0
let turnId: string | undefined
let permissionCount = 0
let commandChain: Promise<void> = Promise.resolve()
// Messages folded into the running turn that no model request has carried
// yet. A turn that ends first never read them.
let unreadNotes: string[] = []
// The model and effort chosen in Legato for this session. They are applied
// to each request as it leaves; the session's own settings, and the person's
// saved defaults, are never written.
let modelOverride: string | undefined
let effortOverride: Effort | undefined

// What Legato put in the session's environment to say which task it serves.
type Identity = Record<string, string>

// What a session leaves when it ends, and what a background session keeps
// once it has taken one over. `count` and `tail` say which conversation the
// note belongs to: how many messages it had, and the last of them.
type Note = { at: number; cwd?: string; count?: number; tail?: string[]; env: Identity }

// The task this process serves, once a session of it started inside Legato.
let identity: Identity | undefined
let isBackground = false
// The session whose note is in the store under its own name.
let notedSessionId: string | undefined
let notedAt = 0

// A note left by a session that ended is for a session starting right then.
const HANDOVER_WINDOW = 120_000
// A background session's own note is good for this long after it was last
// written, and is written again as the session works.
const NOTE_LIFETIME = 7 * 24 * 3_600_000
const NOTE_REFRESH = 3_600_000
const TAIL_LENGTH = 5

// What is held for a bridge that has not announced itself yet. One that is
// slower than this many events is not coming.
const PENDING_LIMIT = 5_000

const post = (event: BridgeEvent): void => {
  const isIdentity = event.type === 'hello' || event.type === 'reset' || event.type === 'identity'

  if (!isCollecting || (endpoint === undefined && queue.length >= PENDING_LIMIT && !isIdentity)) {
    return
  }

  const last = queue.at(-1)
  // Pieces of one block that are still waiting to be sent go as one.
  const isSameBlock =
    (event.type === 'text_delta' || event.type === 'thinking_delta') &&
    last?.type === event.type &&
    last.turnId === event.turnId &&
    last.step === event.step &&
    last.index === event.index &&
    last.agentId === event.agentId

  if (isSameBlock && last !== undefined) {
    last.text = `${String(last.text)}${String(event.text)}`
  } else {
    queue.push(event)
  }

  wake?.()
}

// One pump per bridge sends the queue in order, a batch at a time. Hooks
// only queue: no hook waits on the bridge to forward what it saw.
const pump = async ($: EngineInterface, url: string): Promise<void> => {
  while (endpoint === url) {
    if (queue.length === 0) {
      await new Promise<void>(resolve => {
        wake = resolve
      })
      wake = undefined
      continue
    }

    const events = queue
    queue = []

    try {
      const response = await $.http.fetch(`${url}/mod/events`, {
        method: 'POST',
        headers: JSON_HEADERS,
        body: JSON.stringify({ events }),
      })

      if (!response.ok) {
        $.ui.log(`legato: the bridge refused ${events.length} events (${response.status})`, {
          to: 'debug',
        })
      }
    } catch (error) {
      $.ui.log(`legato: the bridge is unreachable: ${String(error)}`, { to: 'debug' })

      if (endpoint === url) {
        isCollecting = false
        endpoint = undefined
        queue = []
      }
    }
  }
}

const contextOf = async ($: EngineInterface): Promise<{ tokens?: number; window: number } | undefined> => {
  try {
    const { context } = await $.session.usage()

    return { tokens: context.tokens, window: context.window }
  } catch {
    return undefined
  }
}

// What the session is, and its transcript so far: the first event a bridge
// receives, and what it is sent again when the process goes on under another
// session. At launch it is read inside `session.start`, which the engine
// awaits before the first prompt, so every row that follows is one the
// `session.append` hook sees and the two never overlap.
const describe = async ($: EngineInterface): Promise<BridgeEvent> => {
  const [sessionId, cwd, model, messages, context, commands] = await Promise.all([
    $.session.id(),
    $.session.cwd(),
    $.session.model(),
    $.session.messages(),
    contextOf($),
    $.command.list(),
  ])

  // Newest first, until the budget is spent; then back in reading order.
  const seed: unknown[] = []
  let room = SEED_BUDGET

  for (let at = messages.length - 1; at >= 0 && seed.length < SEED_MESSAGES; at -= 1) {
    const message = messages[at]

    if (message === undefined) {
      continue
    }

    const slim = {
      role: message.role,
      text: cap(message.text, SEED_TEXT_LIMIT),
      toolUses: message.toolUses.map(use => ({
        tool_use_id: use.tool_use_id,
        tool: use.tool,
        input: capArguments(use.input, SEED_DETAIL_LIMIT),
        text: use.text === undefined ? undefined : cap(use.text, SEED_DETAIL_LIMIT),
        isError: use.isError === true,
      })),
    }
    const size = JSON.stringify(slim).length

    if (size > room) {
      break
    }

    room -= size
    seed.push(slim)
  }

  return {
    type: 'hello',
    sessionId,
    cwd,
    model,
    busy: turnId !== undefined,
    context,
    commands: commands.map(command => ({
      name: command.name,
      description: command.description,
      source: command.source,
    })),
    messages: seed.reverse(),
  }
}

// The engine switches identity after `session.end` returns. Clear identifies
// it without erasing rows already posted after the reset; resume seeds history.
const reportTransition = async (
  $: EngineInterface,
  endedSessionId: string,
  reason: 'clear' | 'resume',
  transition: number,
): Promise<void> => {
  const run = bridgeRun

  for (let attempt = 0; attempt < 50; attempt += 1) {
    if (!isCollecting || run !== bridgeRun || transition !== transitionRun) {
      return
    }

    const sessionId = await $.session.id()

    if (sessionId !== endedSessionId) {
      const event = reason === 'clear'
        ? { type: 'identity', sessionId, cwd: await $.session.cwd() }
        : await describe($)

      if (transition === transitionRun) {
        post(event)
        await keepNote($)
      }

      return
    }

    await $.clock.sleep(100)
  }

  throw new Error(`/${reason} did not expose a replacement session ID within 5 seconds`)
}

// The session ended in this process for good. `/background` is one way: the
// conversation goes on in another Claude process, whose bridge can only bind
// the task once this one has let go of it. The bridge is told directly, since
// the engine gives an ending session little time and the pump may not get
// another turn.
const retire = async ($: EngineInterface): Promise<void> => {
  const url = endpoint

  if (url === undefined) {
    return
  }

  bridgeRun += 1
  isBridgeRunning = false
  isCollecting = false
  endpoint = undefined
  queue = []
  viewers = 0
  wake?.()

  try {
    await $.http.fetch(`${url}/mod/events`, {
      method: 'POST',
      headers: JSON_HEADERS,
      body: JSON.stringify({ events: [{ type: 'ended' }] }),
    })
  } catch (error) {
    $.ui.log(`legato: the bridge was not told the session ended: ${String(error)}`, { to: 'debug' })
  }
}

// The variables Legato gives a session, as this process holds them.
const readIdentity = async ($: EngineInterface): Promise<Identity> => {
  const held: [string, string | undefined][] = [
    ['LEGATO_TASK_ID', await $.env.get('LEGATO_TASK_ID')],
    ['LEGATO_EXECUTION_ID', await $.env.get('LEGATO_EXECUTION_ID')],
    ['LEGATO_BIN', await $.env.get('LEGATO_BIN')],
    ['LEGATO_SOCKET', await $.env.get('LEGATO_SOCKET')],
    ['LEGATO_CONFIG', await $.env.get('LEGATO_CONFIG')],
    ['LEGATO_HOME', await $.env.get('LEGATO_HOME')],
    ['LEGATO_TASK_EPHEMERAL', await $.env.get('LEGATO_TASK_EPHEMERAL')],
    ['LEGATO_AGENT_ROLE', await $.env.get('LEGATO_AGENT_ROLE')],
    ['LEGATO_PARENT_TASK_ID', await $.env.get('LEGATO_PARENT_TASK_ID')],
    ['LEGATO_SUBTASK_ID', await $.env.get('LEGATO_SUBTASK_ID')],
    ['LEGATO_ROLE_PROMPT_FILE', await $.env.get('LEGATO_ROLE_PROMPT_FILE')],
    ['LEGATO_BRIEF_FILE', await $.env.get('LEGATO_BRIEF_FILE')],
    ['LEGATO_WORKFLOW_RUN_ID', await $.env.get('LEGATO_WORKFLOW_RUN_ID')],
    ['LEGATO_WORKFLOW_STEP_ID', await $.env.get('LEGATO_WORKFLOW_STEP_ID')],
    ['LEGATO_WORKFLOW_ATTEMPT_ID', await $.env.get('LEGATO_WORKFLOW_ATTEMPT_ID')],
    ['XDG_RUNTIME_DIR', await $.env.get('XDG_RUNTIME_DIR')],
  ]

  return Object.fromEntries(
    held.filter((pair): pair is [string, string] => pair[1] !== undefined && pair[1] !== ''),
  )
}

// Makes this process the session of `env`'s task: the hooks and tools it
// starts from here on read these. A variable the task does not have is
// unset, so nothing of another task's is left behind.
const writeIdentity = async ($: EngineInterface, env: Identity): Promise<void> => {
  await $.env.set('LEGATO_TASK_ID', env.LEGATO_TASK_ID)
  await $.env.set('LEGATO_EXECUTION_ID', env.LEGATO_EXECUTION_ID)
  await $.env.set('LEGATO_SOCKET', env.LEGATO_SOCKET)
  await $.env.set('LEGATO_TASK_EPHEMERAL', env.LEGATO_TASK_EPHEMERAL)
  await $.env.set('LEGATO_AGENT_ROLE', env.LEGATO_AGENT_ROLE)
  await $.env.set('LEGATO_PARENT_TASK_ID', env.LEGATO_PARENT_TASK_ID)
  await $.env.set('LEGATO_SUBTASK_ID', env.LEGATO_SUBTASK_ID)
  await $.env.set('LEGATO_ROLE_PROMPT_FILE', env.LEGATO_ROLE_PROMPT_FILE)
  await $.env.set('LEGATO_BRIEF_FILE', env.LEGATO_BRIEF_FILE)
  await $.env.set('LEGATO_WORKFLOW_RUN_ID', env.LEGATO_WORKFLOW_RUN_ID)
  await $.env.set('LEGATO_WORKFLOW_STEP_ID', env.LEGATO_WORKFLOW_STEP_ID)
  await $.env.set('LEGATO_WORKFLOW_ATTEMPT_ID', env.LEGATO_WORKFLOW_ATTEMPT_ID)

  // Which Legato it is: kept as inherited when the task's note has none.
  if (env.LEGATO_BIN !== undefined) {
    await $.env.set('LEGATO_BIN', env.LEGATO_BIN)
  }

  if (env.LEGATO_CONFIG !== undefined) {
    await $.env.set('LEGATO_CONFIG', env.LEGATO_CONFIG)
  }

  if (env.LEGATO_HOME !== undefined) {
    await $.env.set('LEGATO_HOME', env.LEGATO_HOME)
  }

  if (env.XDG_RUNTIME_DIR !== undefined) {
    await $.env.set('XDG_RUNTIME_DIR', env.XDG_RUNTIME_DIR)
  }
}

const mark = (text: string): string => {
  let hash = 5381

  for (let at = 0; at < text.length; at += 1) {
    hash = ((hash * 33) ^ text.charCodeAt(at)) >>> 0
  }

  return hash.toString(36)
}

// The last messages of a conversation's first `count`, each as its role and
// a mark of its text.
const tailOf = (messages: readonly { role: string; text: string }[], count: number): string[] =>
  messages
    .slice(Math.max(0, count - TAIL_LENGTH), count)
    .map(message => `${message.role}:${mark(message.text)}`)

const isNote = (value: unknown): value is Note =>
  typeof value === 'object' &&
  value !== null &&
  typeof (value as Note).at === 'number' &&
  typeof (value as Note).env === 'object' &&
  (value as Note).env !== null

// Leaves a note of the task this session served, for the session that goes
// on with its conversation. `/background` starts that session in a process
// of Claude Code's daemon, which has the environment of whichever session
// first started the daemon, so it cannot know its task any other way.
const leaveNote = async ($: EngineInterface, endedSessionId: string): Promise<void> => {
  if (identity?.LEGATO_TASK_ID === undefined) {
    return
  }

  const [at, cwd, messages] = await Promise.all([
    $.clock.now(),
    $.session.cwd(),
    $.session.messages(),
  ])
  const note: Note = {
    at,
    cwd,
    count: messages.length,
    tail: tailOf(messages, messages.length),
    env: identity,
  }

  await $.store.set(`handover:${endedSessionId}`, note)

  // A background session that ends no longer holds the task.
  if (isBackground && notedSessionId !== undefined) {
    await $.store.delete(`session:${notedSessionId}`)
    notedSessionId = undefined
  }
}

// Whether a background session holds this launch of the task. The process
// it was sent from can stay on as a viewer of it, with the task's own
// variables; it must not take the task back when its session starts again.
const isHandedOver = async ($: EngineInterface, held: Identity): Promise<boolean> => {
  const now = await $.clock.now()

  for (const key of await $.store.keys()) {
    if (!key.startsWith('session:')) {
      continue
    }

    const note = await $.store.get(key)

    if (
      isNote(note) &&
      now - note.at <= NOTE_LIFETIME &&
      note.env.LEGATO_TASK_ID === held.LEGATO_TASK_ID &&
      note.env.LEGATO_EXECUTION_ID === held.LEGATO_EXECUTION_ID
    ) {
      return true
    }
  }

  return false
}

// The note of the conversation this session goes on with: left moments ago,
// in this directory, by a session whose messages are this one's first.
const claimNote = async ($: EngineInterface, sessionId: string): Promise<Note | undefined> => {
  const [now, cwd, messages, keys] = await Promise.all([
    $.clock.now(),
    $.session.cwd(),
    $.session.messages(),
    $.store.keys(),
  ])
  let found: { key: string; note: Note } | undefined

  for (const key of keys) {
    const isHandover = key.startsWith('handover:')

    if (!isHandover && !key.startsWith('session:')) {
      continue
    }

    const note = await $.store.get(key)

    // Notes nobody will read any more go as they are passed.
    if (!isNote(note) || now - note.at > (isHandover ? HANDOVER_WINDOW : NOTE_LIFETIME)) {
      await $.store.delete(key)
      continue
    }

    const count = note.count ?? 0
    const isThisConversation =
      isHandover &&
      note.cwd === cwd &&
      count > 0 &&
      messages.length >= count &&
      tailOf(messages, count).join('|') === (note.tail ?? []).join('|')

    if (isThisConversation && (found === undefined || note.at > found.note.at)) {
      found = { key, note }
    }
  }

  if (found === undefined) {
    return undefined
  }

  await $.store.set(`session:${sessionId}`, { at: now, env: found.note.env })
  await $.store.delete(found.key)

  return found.note
}

// Which task a background session serves. Never the one in its environment,
// which is another session's: the note it took over, or none.
const adopt = async ($: EngineInterface): Promise<Identity | undefined> => {
  const sessionId = await $.session.id()
  const own = await $.store.get(`session:${sessionId}`)
  let note = isNote(own) ? own : undefined

  if (
    note === undefined && isBridgeRunning && identity !== undefined && notedSessionId !== undefined
  ) {
    // This process kept its bridge across /clear or /resume; its task did not change.
    await keepNote($)
    note = { at: notedAt, env: identity }
  }

  // The session that ended writes its note as this one starts.
  for (let attempt = 0; note === undefined && attempt < 10; attempt += 1) {
    if (attempt > 0) {
      await $.clock.sleep(200)
    }

    note = await claimNote($, sessionId)
  }

  await writeIdentity($, note?.env ?? {})

  if (note !== undefined) {
    notedSessionId = sessionId
    notedAt = await $.clock.now()
  }

  return note?.env
}

// A background session's note is under its session's name, and a `/clear`
// or a resume gives the session another: the note follows it, so a worker
// the daemon starts again still finds its task. A session that keeps working
// keeps its note from running out.
const keepNote = async ($: EngineInterface): Promise<void> => {
  if (!isBackground || identity === undefined) {
    return
  }

  const [sessionId, now] = await Promise.all([$.session.id(), $.clock.now()])

  if (sessionId === notedSessionId && now - notedAt < NOTE_REFRESH) {
    return
  }

  if (notedSessionId !== undefined && notedSessionId !== sessionId) {
    await $.store.delete(`session:${notedSessionId}`)
  }

  notedSessionId = sessionId
  notedAt = now
  await $.store.set(`session:${sessionId}`, { at: now, env: identity })
}

const report =
  ($: EngineInterface, what: string) =>
  (error: unknown): void => {
    $.ui.log(`legato: ${what} failed: ${String(error)}`, { to: 'debug' })
  }

// A prompt of its own, read as the person's words. `/name args` naming a
// command of the session runs that command, as typing it would.
const submit = async ($: EngineInterface, text: string): Promise<void> => {
  const slash = /^\/([\w:.-]+)(?:\s+([\s\S]*))?$/.exec(text.trim())

  if (slash !== null) {
    const [, name = '', args = ''] = slash
    const commands = await $.command.list()

    if (commands.some(command => command.name === name)) {
      void $.command.run({ command: name, args }).catch(report($, `/${name}`))

      return
    }
  }

  // Not awaited: it resolves when its turn starts, which may be a while.
  void $.prompt.submit({ text, asUser: true }).catch(report($, 'submitting a prompt'))
}

// A row the next model request reads, without starting a turn.
const appendRow = async ($: EngineInterface, text: string): Promise<void> => {
  const stored = await $.session.append({
    message: { type: 'user', content: [{ type: 'text', text }] },
  })

  if (stored.deny !== undefined) {
    throw new Error(stored.deny)
  }
}

const run = async ($: EngineInterface, command: Command): Promise<void> => {
  if (command.type === 'submit') {
    await submit($, command.text)
  } else if (command.type === 'queue') {
    // Sent while a turn ran. It joins that turn, whose next model request
    // reads it; if the turn has since ended it is a prompt of its own.
    if (turnId === undefined) {
      await submit($, command.text)
    } else {
      await appendRow($, `${NOTE_FRAME}\n\n${command.text}`)
      unreadNotes.push(command.text)
    }
  } else if (command.type === 'append') {
    await appendRow($, command.text)
  }
}

const handle = ($: EngineInterface, line: string): void => {
  let command: Command

  try {
    command = JSON.parse(line) as Command
  } catch {
    return
  }

  // The bridge is listening: what was held for it starts to drain.
  if (command.type === 'ready') {
    endpoint = command.url
    void pump($, command.url)

    return
  }

  // These two never wait behind a message that is still being delivered.
  if (command.type === 'interrupt') {
    if (turnId !== undefined) {
      void $.turn.abort({ turnId }).catch(report($, 'interrupting the turn'))
    }

    return
  }

  if (command.type === 'viewers') {
    viewers = command.count

    return
  }

  if (command.type === 'model') {
    modelOverride = command.model ?? undefined

    return
  }

  if (command.type === 'effort') {
    effortOverride = command.effort ?? undefined

    return
  }

  // The rest run in the order the bridge wrote them: an attachment appended
  // before a prompt must be in the transcript before that prompt enters.
  commandChain = commandChain.then(() => run($, command).catch(report($, command.type)))
}

// The bridge lives exactly as long as this loop: the child is killed when
// the module unloads, and the loop ends when the child exits.
const runBridge = async ($: EngineInterface, taskId: string, env: Identity): Promise<void> => {
  const bin = env.LEGATO_BIN ?? 'legato'
  const executionId = env.LEGATO_EXECUTION_ID
  const argv = [bin, 'agent', 'bridge', taskId]

  if (executionId !== undefined && executionId !== '') {
    argv.push('--execution-id', executionId)
  }

  bridgeRun += 1

  const run = bridgeRun
  let buffered = ''

  try {
    // The task's own variables go to the child by name: in a background
    // session this process started with another task's.
    for await (const piece of $.process.spawn({ argv, env })) {
      if (run !== bridgeRun) {
        continue
      }

      if (piece.stream === 'stderr') {
        $.ui.log(`legato bridge: ${piece.text.trim()}`, { to: 'debug' })
        continue
      }

      // A piece ends wherever the child's write did, not at a line.
      buffered += piece.text

      for (let end = buffered.indexOf('\n'); end >= 0; end = buffered.indexOf('\n')) {
        const line = buffered.slice(0, end).trim()
        buffered = buffered.slice(end + 1)

        if (line !== '') {
          handle($, line)
        }
      }
    }
  } catch (error) {
    $.ui.log(`legato: the bridge did not run: ${String(error)}`, { to: 'debug' })
  }

  if (run !== bridgeRun) {
    return
  }

  isBridgeRunning = false
  isCollecting = false
  endpoint = undefined
  queue = []
  viewers = 0
  wake?.()
}

export const register: Register = on => {
  on('session.start', async ($, e, next) => {
    const started = await next(e)

    isBackground = (await $.env.get('CLAUDE_CODE_SESSION_KIND')) === 'bg'

    const held = await readIdentity($)

    // Outside Legato. A background session is asked for its note whatever
    // it holds: its variables are the daemon's, not its own.
    if (!isBackground && held.LEGATO_TASK_ID === undefined) {
      return started
    }

    if (!isBackground && (await isHandedOver($, held).catch(() => false))) {
      return started
    }

    const env = isBackground ? await adopt($).catch(() => undefined) : held
    const taskId = env?.LEGATO_TASK_ID

    if (env === undefined || taskId === undefined) {
      return started
    }

    identity = env

    // A headless run inside a Legato session (a script calling `claude -p`)
    // is not the session Legato launched and must not take its place.
    const isWanted =
      e.isInteractive || (await $.env.get('LEGATO_CLAUDE_BRIDGE_HEADLESS')) === '1'

    if (!isWanted) {
      return started
    }

    const described = await describe($)
    transitionRun += 1

    if (isBridgeRunning) {
      // The process started another session under the bridge it already has.
      post(described)
    } else {
      queue = [described]
      isCollecting = true
      isBridgeRunning = true
      void runBridge($, taskId, env)
    }

    return started
  })

  on('session.end', async ($, e, next) => {
    // Every other reason ends the session in this process.
    if (e.reason !== 'clear' && e.reason !== 'resume') {
      await leaveNote($, e.sessionId).catch(report($, 'leaving a hand-over note'))
      await retire($)
    } else {
      transitionRun += 1
      turnId = undefined
      unreadNotes = []
      post({ type: 'reset' })
    }

    const transition = transitionRun
    const ended = await next(e)

    if (e.reason === 'clear' || e.reason === 'resume') {
      if (isCollecting) {
        void reportTransition($, e.sessionId, e.reason, transition).catch(report($, `reporting the /${e.reason} identity`))
      }
    }

    return ended
  })

  // The conversation's own rows, as stored. The hook only relays. What the
  // engine injects beside the conversation (reminders, listings, hook output)
  // is no part of what the panel shows and never leaves the session.
  on('session.append', async ($, e, next) => {
    const stored = await next(e)
    const message = stored.message
    const isConversation = message?.type === 'user' || message?.type === 'assistant'

    if (isCollecting && message !== undefined && isConversation && message.role !== undefined) {
      post({
        type: 'row',
        uuid: stored.uuid,
        door: e.door,
        origin: e.origin,
        agentId: e.agentId,
        message: {
          type: message.type,
          role: message.role,
          isMeta: message.isMeta === true,
          name: message.name,
          content: (message.content as unknown as Block[])
            .map(slimBlock)
            .filter(block => block !== undefined),
        },
      })
    }

    return stored
  })

  on('turn.start', ($, e, next) => {
    turnId = e.turnId
    post({ type: 'turn_start', turnId: e.turnId })

    return next(e)
  })

  on('turn.complete', async ($, e, next) => {
    const completed = await next(e)

    if (e.agentId !== undefined) {
      post({ type: 'turn_complete', turnId: e.turnId, reason: e.reason, agentId: e.agentId })

      return completed
    }

    turnId = undefined

    // The turn ended before any request carried these: the model never read
    // them. They become a prompt of their own, as a message typed during a
    // turn does.
    const unread = unreadNotes
    unreadNotes = []

    if (unread.length > 0) {
      void $.prompt
        .submit({ text: unread.join('\n\n'), asUser: true })
        .catch(report($, 'submitting a queued message'))
    }

    void keepNote($).catch(report($, 'keeping the session\'s note'))

    if (isCollecting) {
      const [model, context] = await Promise.all([
        $.session.model().catch(() => undefined),
        contextOf($),
      ])

      post({ type: 'model', model })
      post({ type: 'turn_complete', turnId: e.turnId, reason: e.reason, context })
    }

    return completed
  })

  // One model request of a turn. The session's requests go out with the
  // model and effort chosen in Legato, when one was; the reply and the
  // thinking before it are forwarded as they stream, every chunk passed on
  // exactly as it came.
  on('turn.step', async function* ($, e, next) {
    if (e.agentId !== undefined) {
      return yield* next(e)
    }

    // What the session would use on its own: the default the panel shows.
    post({ type: 'settings', effort: e.effort === undefined ? '' : String(e.effort) })

    const stream = next({
      ...e,
      model: modelOverride ?? e.model,
      effort: effortOverride ?? e.effort,
    })

    // This request carries every row appended so far, so the model now reads
    // the messages folded into the turn; the panel shows them from here.
    for (const text of unreadNotes) {
      post({ type: 'user_note', text })
    }

    unreadNotes = []

    for await (const chunk of stream) {
      if ((chunk.kind === 'text' || chunk.kind === 'thinking') && chunk.text !== '') {
        post({
          type: chunk.kind === 'text' ? 'text_delta' : 'thinking_delta',
          turnId: e.turnId,
          step: e.index,
          index: chunk.index,
          text: chunk.text,
        })
      }

      yield chunk
    }

    const response = await stream.result

    // Each request moves the context figure; a long turn makes many.
    if (isCollecting) {
      post({ type: 'usage', context: await contextOf($) })
    }

    return response
  })

  on('agent.spawn', async ($, e, next) => {
    const spawned = await next(e)

    if (spawned.agentId !== undefined && e.parentAgentId === undefined) {
      post({
        type: 'agent_spawn',
        toolUseId: e.tool_use_id,
        agentId: spawned.agentId,
        description: e.description,
      })
    }

    return spawned
  })

  // A permission dialog is opening. While someone follows the session in
  // Legato the bridge also puts the question to them and holds this request
  // until one side answers; the terminal's dialog stays up throughout. With
  // nobody watching, or when the bridge cannot say, the dialog alone decides.
  //
  // A question the model asks (AskUserQuestion) opens the same way: its
  // dialog is its permission prompt, and the answers are its decision.
  on('classic.PermissionRequest', async ($, e, next) => {
    const beneath = await next(e)
    const url = endpoint

    if (beneath.decision !== undefined || url === undefined) {
      return beneath
    }

    let answer: PermissionAnswer

    try {
      permissionCount += 1

      const requestId = `permission-${permissionCount}-${await $.clock.now()}`

      const body = JSON.stringify({
        requestId,
        tool: e.tool_name,
        input: capArguments(e.tool_input),
        // What the engine's own "don't ask again" would change.
        suggestions: e.permission_suggestions,
      })

      // The wait is spent inside these calls, where a hook's own time is not
      // counted, so a person may take as long as they need. The host ends a
      // call after 30 seconds, so the bridge answers "wait" before then and
      // the request is asked again under the same ID.
      do {
        const response = await $.http.fetch(`${url}/mod/permissions`, {
          method: 'POST',
          headers: JSON_HEADERS,
          body,
        })

        answer = JSON.parse(response.text) as PermissionAnswer
      } while (answer.decision === 'wait')
    } catch {
      return beneath
    }

    if (answer.decision === 'allow') {
      const input = e.tool_input as Record<string, unknown>
      const decision: Allow = { behavior: 'allow' }

      if (answer.answers !== undefined) {
        // The engine reads a question's answers from the tool's own input,
        // where its dialog would have put them.
        decision.updatedInput = { ...input, answers: answer.answers }
      } else if (answer.returnInput === true) {
        decision.updatedInput = input
      }

      if (answer.updatedPermissions !== undefined) {
        decision.updatedPermissions = answer.updatedPermissions
      }

      return { ...beneath, decision }
    }

    if (answer.decision === 'deny') {
      return {
        ...beneath,
        decision: {
          behavior: 'deny',
          message: answer.message ?? answer.feedback ?? 'Denied in Legato.',
        },
      }
    }

    return beneath
  })
}
