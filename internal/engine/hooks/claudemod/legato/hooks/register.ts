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
  decision: 'allow' | 'deny' | 'ask'
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

// What is held for a bridge that has not announced itself yet. One that is
// slower than this many events is not coming.
const PENDING_LIMIT = 5_000

const post = (event: BridgeEvent): void => {
  if (!isCollecting || (endpoint === undefined && queue.length >= PENDING_LIMIT)) {
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

// `/resume`: the process goes on under another session, whose transcript was
// loaded rather than appended, so no row announces it. Once the switch has
// happened the session is described again.
const redescribe = async ($: EngineInterface, endedSessionId: string): Promise<void> => {
  for (let attempt = 0; attempt < 50; attempt += 1) {
    if ((await $.session.id()) !== endedSessionId) {
      break
    }

    await $.clock.sleep(100)
  }

  post(await describe($))
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
const runBridge = async ($: EngineInterface, taskId: string): Promise<void> => {
  const bin = (await $.env.get('LEGATO_BIN')) ?? 'legato'
  const executionId = await $.env.get('LEGATO_EXECUTION_ID')
  const argv = [bin, 'agent', 'bridge', taskId]

  if (executionId !== undefined && executionId !== '') {
    argv.push('--execution-id', executionId)
  }

  let buffered = ''

  try {
    for await (const piece of $.process.spawn({ argv })) {
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
    const taskId = await $.env.get('LEGATO_TASK_ID')

    if (taskId === undefined || taskId === '') {
      return started
    }

    // A headless run inside a Legato session (a script calling `claude -p`)
    // is not the session Legato launched and must not take its place.
    const isWanted =
      e.isInteractive || (await $.env.get('LEGATO_CLAUDE_BRIDGE_HEADLESS')) === '1'

    if (!isWanted) {
      return started
    }

    const described = await describe($)

    if (isBridgeRunning) {
      // The process started another session under the bridge it already has.
      post(described)
    } else {
      queue = [described]
      isCollecting = true
      isBridgeRunning = true
      void runBridge($, taskId)
    }

    return started
  })

  on('session.end', async ($, e, next) => {
    const ended = await next(e)

    if (e.reason === 'clear' || e.reason === 'resume') {
      turnId = undefined
      unreadNotes = []
      post({ type: 'reset' })
    }

    if (e.reason === 'resume' && isCollecting) {
      void redescribe($, e.sessionId).catch(report($, 'describing the resumed session'))
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

      // The wait is spent inside this call, where a hook's own time is not
      // counted, so a person may take as long as they need.
      const response = await $.http.fetch(`${url}/mod/permissions`, {
        method: 'POST',
        headers: JSON_HEADERS,
        body: JSON.stringify({
          requestId,
          tool: e.tool_name,
          input: capArguments(e.tool_input),
          // What the engine's own "don't ask again" would change.
          suggestions: e.permission_suggestions,
        }),
      })

      answer = JSON.parse(response.text) as PermissionAnswer
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
