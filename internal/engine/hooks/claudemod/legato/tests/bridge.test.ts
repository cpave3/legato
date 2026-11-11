import { expect, mock, test } from 'claude-code/testing'
import type { On } from 'claude-code'

type Posted = { url: string; body: { events?: { type: string; [field: string]: unknown }[]; [field: string]: unknown } }

const LEGATO_SESSION = {
  LEGATO_TASK_ID: 'task-1',
  LEGATO_EXECUTION_ID: 'ex-1',
  LEGATO_BIN: '/opt/legato',
}

const INTERACTIVE = { cwd: '/work/repo', surface: 'terminal', isInteractive: true } as const

// Everything beneath the mod: the bridge child it spawns (scripted by the
// lines it prints), the bridge's HTTP endpoint, and the session it reads.
type Transcript = { role: 'user' | 'assistant'; text: string; toolUses: [] }[]

// A piece of a model's response as it streams.
type Piece = { kind: 'text' | 'thinking'; index: number; text: string }

const world = (
  on: On,
  script: {
    lines?: string[]
    permission?: object
    // Answers to successive permission calls, before permission.
    permissions?: object[]
    startsAfter?: Promise<void>
    reply?: Piece[]
    // What the mod's own store holds when the session starts.
    store?: Record<string, unknown>
    // The process's variables, for a test that changes them as it goes; the
    // others give theirs to `mock.env`.
    env?: Record<string, string>
    // The statuses the bridge answers successive event batches with, before
    // it answers 204 to every one.
    eventStatuses?: number[]
    // Why the session drops every prompt submitted, when it does.
    drop?: string
  } = {},
) => {
  // The session the mod reads; a test swaps it to stand for a resume.
  const session: { id: string; cwd: string; messages: Transcript; tokens: number } = {
    id: 'claude-session-1',
    cwd: '/work/repo',
    messages: [],
    tokens: 10,
  }
  const spawned: string[][] = []
  // The variables each bridge child was started with, over the process's own.
  const spawnedWith: (Record<string, string> | undefined)[] = []
  // Every variable the mod set for its process; undefined is one it unset.
  const envSet: Record<string, string | undefined> = {}
  // What each model request reached the engine with, after the mod.
  const requested: { model: string; effort: unknown }[] = []
  const posted: Posted[] = []
  const logs: string[] = []
  const submitted: string[] = []
  // Commands the bridge prints later in the test, when it says them.
  const said: string[] = []
  let heard: (() => void) | undefined
  let isExited = false
  const say = (command: object): void => {
    said.push(JSON.stringify(command))
    heard?.()
  }
  const exit = (): void => {
    isExited = true
    heard?.()
  }

  on('process.spawn', async function* ($, e) {
    spawned.push([...e.argv])
    spawnedWith.push(e.env)
    await script.startsAfter

    for (const line of script.lines ?? []) {
      yield { stream: 'stdout', text: `${line}\n` }
    }

    while (!isExited) {
      const line = said.shift()

      if (line !== undefined) {
        yield { stream: 'stdout', text: `${line}\n` }
        continue
      }

      await new Promise<void>(resolve => {
        heard = resolve
      })
      heard = undefined
    }

    return { value: { code: 0, signal: null } }
  })
  on('http.fetch', ($, e) => {
    posted.push({ url: e.url, body: JSON.parse(e.init?.body ?? '{}') })

    const text = e.url.endsWith('/mod/permissions')
      ? JSON.stringify(script.permissions?.shift() ?? script.permission ?? { decision: 'ask' })
      : ''
    const status = (e.url.endsWith('/mod/events') ? script.eventStatuses?.shift() : undefined) ?? 200

    return { value: { status, ok: status < 300, headers: {}, text } }
  })
  // The mod's own store, which every process running the mod shares.
  const store = new Map<string, unknown>(Object.entries(script.store ?? {}))
  on('store.get', ($, e) => ({ value: store.get(e.key) }))
  on('store.set', ($, e) => {
    store.set(e.key, JSON.parse(JSON.stringify(e.value)))

    return { value: undefined }
  })
  on('store.delete', ($, e) => {
    store.delete(e.key)

    return { value: undefined }
  })
  on('store.keys', () => ({ value: [...store.keys()] }))

  const env = script.env

  if (env !== undefined) {
    on('env.get', ($, e) => ({ value: env[e.name] }))
  }
  on('env.set', ($, e) => {
    envSet[e.name] = e.value

    return { value: undefined }
  })
  on('prompt.submit', ($, e) => {
    if (script.drop !== undefined) {
      return { drop: script.drop }
    }

    submitted.push(e.text)

    return { text: e.text }
  })
  on('session.start', ($, e) => ({ cwd: e.cwd }))
  on('session.end', ($, e) => ({ sessionId: e.sessionId }))
  on('session.id', () => ({ value: session.id }))
  on('session.cwd', () => ({ value: session.cwd }))
  on('session.model', () => ({ value: 'claude-opus-5-5' }))
  on('session.messages', () => ({ value: session.messages }))
  on('session.usage', () => ({
    value: { startedAt: 0, context: { tokens: session.tokens, window: 200_000 }, rateLimits: [] },
  }))
  on('turn.step', async function* ($, e) {
    requested.push({ model: e.model, effort: e.effort })

    for (const piece of script.reply ?? [{ kind: 'text', index: 0, text: 'ok' }]) {
      yield piece
    }

    return {
      turnId: e.turnId,
      index: e.index,
      answer: 'ok',
      toolUses: [],
      stopReason: 'end_turn',
      usage: null,
    }
  })
  on('command.list', () => ({ value: [] }))
  on('ui.log', ($, e) => {
    logs.push(JSON.stringify(e))

    return { value: undefined }
  })
  on('turn.start', ($, e) => ({ turnId: e.turnId }))
  on('turn.complete', ($, e) => ({ text: e.answer }))
  on('classic.PermissionRequest', () => ({}))

  const events = () => posted.flatMap(post => post.body.events ?? [])

  return { session, spawned, spawnedWith, envSet, store, requested, posted, logs, submitted, events, say, exit }
}

// The rows the mod appends to the conversation, as their text. An append made
// while the test holds them waits until it lets them go, as a slow engine
// would keep one waiting.
const transcript = (on: On) => {
  const rows: string[] = []
  let held: Promise<void> | undefined

  on('session.append', async ($, e, next) => {
    await held

    rows.push(e.message.content.map(block => (block.type === 'text' ? String(block.text) : '')).join(''))

    return next(e)
  })

  const hold = (): (() => void) => {
    let release = (): void => {}
    held = new Promise<void>(resolve => {
      release = resolve
    })

    return () => {
      held = undefined
      release()
    }
  }

  return { rows, hold }
}

// How many times the model is given a text: as a row of the conversation or
// as a prompt of its own. A message is given exactly once.
const deliveries = (text: string, rows: readonly string[], submitted: readonly string[]): number =>
  [...rows, ...submitted].filter(given => given.includes(text)).length

// One model request of the main conversation, read to its end.
const step = async ($: Parameters<Parameters<typeof test>[1]>[0], index: number): Promise<void> => {
  for await (const chunk of $.turn.step({ turnId: 'turn-1', index, model: 'claude-opus-5-5', messageCount: 1 })) {
    void chunk
  }
}

const COMPLETE = { reason: 'answer', answer: 'Done.', durationMs: 10, isAborted: false, turnId: 'turn-1' } as const

const READY = JSON.stringify({ type: 'ready', url: 'http://bridge.test/secret' })

test('outside a Legato session no bridge is started', async ($, on) => {
  const clock = mock.clock(on)
  mock.env(on, {})
  const { spawned } = world(on)

  await $.session.start(INTERACTIVE)
  await clock.settle()

  expect(spawned).toEqual([])
})

test('a headless run inside a Legato session does not take the session over', async ($, on) => {
  const clock = mock.clock(on)
  mock.env(on, LEGATO_SESSION)
  const { spawned } = world(on)

  await $.session.start({ cwd: '/work/repo', surface: null, isInteractive: false })
  await clock.settle()

  expect(spawned).toEqual([])
})

test('inside a Legato session the bridge is started and told what the session is', async ($, on) => {
  const clock = mock.clock(on)
  mock.env(on, LEGATO_SESSION)
  const { spawned, posted, events, exit } = world(on, { lines: [READY] })

  await $.session.start(INTERACTIVE)
  await clock.settle()

  expect(spawned).toEqual([['/opt/legato', 'agent', 'bridge', 'task-1', '--execution-id', 'ex-1']])
  expect(posted[0]?.url).toBe('http://bridge.test/secret/mod/events')
  expect(events()[0]).toEqual(
    expect.objectContaining({
      type: 'hello',
      sessionId: 'claude-session-1',
      cwd: '/work/repo',
      model: 'claude-opus-5-5',
      busy: false,
    }),
  )

  exit()
  await clock.settle()
})

test('a message from Legato is submitted as the person\'s own prompt', async ($, on) => {
  const clock = mock.clock(on)
  mock.env(on, LEGATO_SESSION)
  const { submitted, exit } = world(on, {
    lines: [READY, JSON.stringify({ type: 'submit', text: 'run the linter' })],
  })

  await $.session.start(INTERACTIVE)
  await clock.settle()

  expect(submitted).toEqual(['run the linter'])

  exit()
  await clock.settle()
})

test('turn events reach the bridge in the order they happened', async ($, on) => {
  const clock = mock.clock(on)
  mock.env(on, LEGATO_SESSION)
  const { events, exit } = world(on, { lines: [READY] })

  await $.session.start(INTERACTIVE)
  await clock.settle()
  await $.turn.start({ text: 'hi', turnId: 'turn-1' })
  await clock.settle()

  expect(events().map(event => event.type)).toEqual(['hello', 'turn_start'])
  expect(events()[1]).toEqual(expect.objectContaining({ turnId: 'turn-1' }))

  exit()
  await clock.settle()
})

test('a permission prompt is answered by the decision Legato returns', async ($, on) => {
  const clock = mock.clock(on)
  mock.env(on, LEGATO_SESSION)
  const { posted, exit } = world(on, { lines: [READY], permission: { decision: 'allow' } })

  await $.session.start(INTERACTIVE)
  await clock.settle()
  const answered = await $.classic.PermissionRequest({
    tool_name: 'Bash',
    tool_input: { command: 'git push' },
  })

  expect(answered.decision).toEqual({ behavior: 'allow' })
  const asked = posted.find(post => post.url.endsWith('/mod/permissions'))
  expect(asked?.body).toEqual(
    expect.objectContaining({ tool: 'Bash', input: { command: 'git push' } }),
  )

  exit()
  await clock.settle()
})

test('a held permission answered wait is asked again with the same request', async ($, on) => {
  const clock = mock.clock(on)
  mock.env(on, LEGATO_SESSION)
  const { posted, exit } = world(on, {
    lines: [READY],
    permissions: [{ decision: 'wait' }, { decision: 'wait' }, { decision: 'allow' }],
  })

  await $.session.start(INTERACTIVE)
  await clock.settle()
  const answered = await $.classic.PermissionRequest({
    tool_name: 'Bash',
    tool_input: { command: 'git push' },
  })

  expect(answered.decision).toEqual({ behavior: 'allow' })
  const asked = posted.filter(post => post.url.endsWith('/mod/permissions'))
  expect(asked).toHaveLength(3)
  expect(new Set(asked.map(post => (post.body as { requestId: string }).requestId)).size).toBe(1)

  exit()
  await clock.settle()
})

test('a denial carries the feedback written in Legato', async ($, on) => {
  const clock = mock.clock(on)
  mock.env(on, LEGATO_SESSION)
  const { exit } = world(on, {
    lines: [READY],
    permission: { decision: 'deny', feedback: 'never force push' },
  })

  await $.session.start(INTERACTIVE)
  await clock.settle()
  const answered = await $.classic.PermissionRequest({
    tool_name: 'Bash',
    tool_input: { command: 'git push --force' },
  })

  expect(answered.decision).toEqual({ behavior: 'deny', message: 'never force push' })

  exit()
  await clock.settle()
})

test('with nobody watching in Legato the dialog opens as usual', async ($, on) => {
  const clock = mock.clock(on)
  mock.env(on, LEGATO_SESSION)
  const { exit } = world(on, { lines: [READY], permission: { decision: 'ask' } })

  await $.session.start(INTERACTIVE)
  await clock.settle()
  const answered = await $.classic.PermissionRequest({
    tool_name: 'Bash',
    tool_input: { command: 'ls' },
  })

  expect(answered.decision).toBeUndefined()

  exit()
  await clock.settle()
})

test('what happens while the bridge is still starting reaches it once it is ready', async ($, on) => {
  const clock = mock.clock(on)
  mock.env(on, LEGATO_SESSION)
  let start = (): void => {}
  const startsAfter = new Promise<void>(resolve => {
    start = resolve
  })
  const { posted, events, exit } = world(on, { lines: [READY], startsAfter })

  // An auto-launched session gets its first prompt within moments, before
  // the bridge process has come up.
  await $.session.start(INTERACTIVE)
  await $.turn.start({ text: 'read your brief', turnId: 'turn-1' })
  await clock.settle()
  expect(posted).toEqual([])

  start()
  await clock.settle()

  expect(events().map(event => event.type)).toEqual(['hello', 'turn_start'])

  exit()
  await clock.settle()
})

test('a message queued during a turn that ends before reading it becomes the next prompt', async ($, on) => {
  const clock = mock.clock(on)
  mock.env(on, LEGATO_SESSION)
  const rows = mock.session(on)
  let deliver = (): void => {}
  const startsAfter = new Promise<void>(resolve => {
    deliver = resolve
  })
  const { submitted, exit } = world(on, {
    lines: [JSON.stringify({ type: 'queue', text: 'also update the docs' })],
    startsAfter,
  })

  await $.session.start(INTERACTIVE)
  await $.turn.start({ text: 'fix the bug', turnId: 'turn-1' })
  deliver()
  await clock.settle()

  // Waiting for the running turn, not submitted as a prompt of its own; no
  // tool call has ended to fold it in.
  expect(submitted).toEqual([])
  expect(JSON.stringify(rows.appended())).not.toContain('also update the docs')

  await $.turn.complete({
    reason: 'answer',
    answer: 'Fixed.',
    durationMs: 10,
    isAborted: false,
    turnId: 'turn-1',
  })
  await clock.settle()

  expect(submitted).toEqual(['also update the docs'])

  exit()
  await clock.settle()
})

// Seen live (task 2ud1k1f0): two notes queued during a turn whose last request
// called no tool. The second was given to the model in that turn and again as
// a prompt after it.
test('messages queued during a turn\'s last request reach the model once', { timeoutMs: 15_000 }, async ($, on) => {
  const clock = mock.clock(on)
  mock.env(on, LEGATO_SESSION)
  const { rows, hold } = transcript(on)
  const { submitted, events, say, exit } = world(on, { lines: [READY] })

  await $.session.start(INTERACTIVE)
  await clock.settle()
  await $.turn.start({ text: 'fix the bug', turnId: 'turn-1' })
  say({ type: 'queue', text: 'cs-169118da99 is ready' })
  await clock.settle()

  // The engine is slow to keep the second one.
  const release = hold()
  say({ type: 'queue', text: 'cs-b24b4944b0 is ready' })
  await clock.settle()

  await step($, 0)
  release()
  await clock.settle()
  await $.turn.complete(COMPLETE)
  await clock.settle()

  for (const text of ['cs-169118da99 is ready', 'cs-b24b4944b0 is ready']) {
    expect({ text, given: deliveries(text, rows, submitted) }).toEqual({ text, given: 1 })
  }

  // Shown once in Legato: a note shown during the turn is not also a prompt.
  const shown = events().filter(event => event.type === 'user_note').map(event => String(event.text))
  for (const text of shown) {
    expect(submitted.some(prompt => prompt.includes(text))).toBe(false)
  }

  exit()
  await clock.settle()
})

// A tool the test lets finish: the moment between two requests of a turn.
const tool = (on: On) => {
  let finish = (): void => {}
  let finished = Promise.resolve()

  on('tool.call', async () => {
    await finished

    return { result: 'ok' }
  })

  return {
    runs: (): void => {
      finished = new Promise<void>(resolve => {
        finish = resolve
      })
    },
    finish: (): void => finish(),
  }
}

test('a message sent while a tool runs joins the turn at the request after it, shown once', async ($, on) => {
  const clock = mock.clock(on)
  mock.env(on, LEGATO_SESSION)
  const { rows, hold } = transcript(on)
  const bash = tool(on)
  const { submitted, events, say, exit } = world(on, { lines: [READY] })

  await $.session.start(INTERACTIVE)
  await clock.settle()
  await $.turn.start({ text: 'fix the bug', turnId: 'turn-1' })
  await step($, 0)

  bash.runs()
  const called = $.tool.call({ tool: 'Bash', tool_use_id: 'tu-1', command: 'make' })
  say({ type: 'queue', text: 'also update the docs' })
  await clock.settle()
  expect(rows).toEqual([])

  // The engine is slow to keep the row: the tool call is not over until it
  // has, so the request after it is built with the row in it.
  const release = hold()
  bash.finish()
  let isOver = false
  void called.then(() => {
    isOver = true
  })
  await clock.settle()
  expect(isOver).toBe(false)

  release()
  await called
  expect(rows).toEqual([expect.stringContaining('also update the docs')])

  await step($, 1)
  await $.turn.complete(COMPLETE)
  await clock.settle()

  expect(deliveries('also update the docs', rows, submitted)).toBe(1)
  expect(events().filter(event => event.type === 'user_note')).toEqual([
    expect.objectContaining({ text: 'also update the docs' }),
  ])

  exit()
  await clock.settle()
})

test('messages join the turn in the order they were sent, repeats included', async ($, on) => {
  const clock = mock.clock(on)
  mock.env(on, LEGATO_SESSION)
  const { rows } = transcript(on)
  const bash = tool(on)
  const { events, say, exit } = world(on, { lines: [READY] })

  await $.session.start(INTERACTIVE)
  await clock.settle()
  await $.turn.start({ text: 'fix the bug', turnId: 'turn-1' })
  bash.runs()
  const called = $.tool.call({ tool: 'Bash', tool_use_id: 'tu-1', command: 'make' })
  say({ type: 'queue', text: 'yes' })
  say({ type: 'queue', text: 'yes' })
  say({ type: 'queue', text: 'no' })
  await clock.settle()
  bash.finish()
  await called
  await step($, 1)
  await clock.settle()

  expect(rows.map(row => row.split('\n\n').at(-1))).toEqual(['yes', 'yes', 'no'])
  expect(events().filter(event => event.type === 'user_note').map(event => event.text)).toEqual(['yes', 'yes', 'no'])

  exit()
  await clock.settle()
})

test('a message Legato sent as a prompt while the session was busy waits to join the turn', async ($, on) => {
  const clock = mock.clock(on)
  mock.env(on, LEGATO_SESSION)
  const { rows } = transcript(on)
  const bash = tool(on)
  const { submitted, say, exit } = world(on, { lines: [READY] })

  await $.session.start(INTERACTIVE)
  await clock.settle()
  // The turn started before the bridge heard of it.
  await $.turn.start({ text: 'fix the bug', turnId: 'turn-1' })
  bash.runs()
  const called = $.tool.call({ tool: 'Bash', tool_use_id: 'tu-1', command: 'make' })
  say({ type: 'submit', text: 'also update the docs' })
  await clock.settle()
  bash.finish()
  await called
  await $.turn.complete(COMPLETE)
  await clock.settle()

  expect(submitted).toEqual([])
  expect(deliveries('also update the docs', rows, submitted)).toBe(1)

  exit()
  await clock.settle()
})

test('a subagent\'s tool calls leave the main conversation\'s messages waiting', async ($, on) => {
  const clock = mock.clock(on)
  mock.env(on, LEGATO_SESSION)
  const { rows } = transcript(on)
  tool(on)
  const { submitted, events, say, exit } = world(on, { lines: [READY] })

  await $.session.start(INTERACTIVE)
  await clock.settle()
  await $.turn.start({ text: 'fix the bug', turnId: 'turn-1' })
  say({ type: 'queue', text: 'also update the docs' })
  await clock.settle()

  await $.tool.call({ tool: 'Bash', tool_use_id: 'tu-9', command: 'ls', agentId: 'agent-7' })
  for await (const chunk of $.turn.step({ ...STEP, agentId: 'agent-7' })) {
    void chunk
  }
  await $.turn.complete({ ...COMPLETE, agentId: 'agent-7' })
  await clock.settle()

  expect(rows).toEqual([])
  expect(submitted).toEqual([])
  expect(events().some(event => event.type === 'user_note')).toBe(false)

  await $.turn.complete(COMPLETE)
  await clock.settle()
  expect(submitted).toEqual(['also update the docs'])

  exit()
  await clock.settle()
})

test('a message the engine refuses to fold in is not lost', async ($, on) => {
  const clock = mock.clock(on)
  mock.env(on, LEGATO_SESSION)
  on('session.append', () => ({ deny: 'read-only session' }))
  const bash = tool(on)
  const { submitted, events, say, exit } = world(on, { lines: [READY] })

  await $.session.start(INTERACTIVE)
  await clock.settle()
  await $.turn.start({ text: 'fix the bug', turnId: 'turn-1' })
  bash.runs()
  const called = $.tool.call({ tool: 'Bash', tool_use_id: 'tu-1', command: 'make' })
  say({ type: 'queue', text: 'also update the docs' })
  await clock.settle()
  bash.finish()
  await called
  await step($, 1)
  await $.turn.complete(COMPLETE)
  await clock.settle()

  expect(events().some(event => event.type === 'user_note')).toBe(false)
  expect(submitted).toEqual(['also update the docs'])

  exit()
  await clock.settle()
})

test('events the bridge refused are sent again, numbered so it applies each once', async ($, on) => {
  const clock = mock.clock(on)
  mock.env(on, LEGATO_SESSION)
  const { posted, exit } = world(on, { lines: [READY], eventStatuses: [500] })

  await $.session.start(INTERACTIVE)
  await clock.settle()
  await $.turn.start({ text: 'fix the bug', turnId: 'turn-1' })
  await clock.advance(5_000)

  const batches = posted
    .filter(post => post.url.endsWith('/mod/events'))
    .map(post => (post.body.events ?? []).map(event => [event.type, event.seq]))
  const refused = batches[0] ?? []
  expect(refused.length).toBeGreaterThan(0)
  // The refused batch again, whole and as numbered, and then what followed.
  expect(batches.flat().slice(0, refused.length * 2)).toEqual([...refused, ...refused])
  const sent = batches.flat().slice(refused.length)
  expect(sent.map(([type]) => type)).toEqual(['hello', 'turn_start'])
  expect(new Set(sent.map(([, seq]) => seq)).size).toBe(sent.length)

  exit()
  await clock.settle()
})

// A hook can drop a prompt, or the engine refuse it. The person is told, by
// the words they sent, so they can send them again.
test('a message the session would not take is reported to Legato as not sent', async ($, on) => {
  const clock = mock.clock(on)
  mock.env(on, LEGATO_SESSION)
  const { events, say, exit } = world(on, { lines: [READY], drop: 'blocked by policy' })

  await $.session.start(INTERACTIVE)
  await clock.settle()
  say({ type: 'submit', text: 'run the linter' })
  await clock.settle()

  await $.turn.start({ text: 'fix the bug', turnId: 'turn-1' })
  say({ type: 'queue', text: 'also update the docs' })
  await clock.settle()
  await $.turn.complete(COMPLETE)
  await clock.settle()

  expect(events().filter(event => event.type === 'message_failed')).toEqual([
    expect.objectContaining({ text: 'run the linter', error: expect.stringContaining('blocked by policy') }),
    expect.objectContaining({ text: 'also update the docs', error: expect.stringContaining('blocked by policy') }),
  ])

  exit()
  await clock.settle()
})

test('a message whose turn ends while it is being queued becomes the next prompt', async ($, on) => {
  const clock = mock.clock(on)
  mock.env(on, LEGATO_SESSION)
  const { rows, hold } = transcript(on)
  const { submitted, say, exit } = world(on, { lines: [READY] })

  await $.session.start(INTERACTIVE)
  await clock.settle()
  await $.turn.start({ text: 'fix the bug', turnId: 'turn-1' })

  const release = hold()
  say({ type: 'queue', text: 'also update the docs' })
  await clock.settle()
  await $.turn.complete(COMPLETE)
  await clock.settle()
  release()
  await clock.settle()

  // Nothing else will start a turn: the model is asked to answer it now.
  expect(submitted).toEqual(['also update the docs'])
  expect(deliveries('also update the docs', rows, submitted)).toBe(1)

  exit()
  await clock.settle()
})

test('resuming another session inside the process describes that session to the bridge', async ($, on) => {
  const clock = mock.clock(on)
  mock.env(on, LEGATO_SESSION)
  const { session, events, exit } = world(on, { lines: [READY] })

  await $.session.start(INTERACTIVE)
  await clock.settle()

  // `/resume`: the process goes on under another session, whose transcript
  // was loaded rather than appended, so no row announces it.
  await $.session.end({
    reason: 'resume',
    sessionId: 'claude-session-1',
    resume: { id: 'claude-session-1' },
  })
  await clock.settle()
  expect(events().filter(event => event.type === 'hello')).toHaveLength(1)

  session.id = 'claude-session-2'
  session.cwd = '/work/resumed'
  session.messages = [
    { role: 'user', text: 'earlier question', toolUses: [] },
    { role: 'assistant', text: 'earlier answer', toolUses: [] },
  ]
  session.tokens = 88_000
  await clock.advance(1_000)

  const described = events().filter(event => event.type === 'hello')
  expect(described).toHaveLength(2)
  expect(described[1]).toEqual(
    expect.objectContaining({
      sessionId: 'claude-session-2',
      cwd: '/work/resumed',
      context: { tokens: 88_000, window: 200_000 },
      messages: [
        expect.objectContaining({ role: 'user', text: 'earlier question' }),
        expect.objectContaining({ role: 'assistant', text: 'earlier answer' }),
      ],
    }),
  )

  exit()
  await clock.settle()
})

test('a long transcript is described within a bounded size, newest messages first to be kept', async ($, on) => {
  const clock = mock.clock(on)
  mock.env(on, LEGATO_SESSION)
  const { session, posted, events, exit } = world(on, { lines: [READY] })
  session.messages = Array.from({ length: 4_000 }, (unused, index) => ({
    role: index % 2 === 0 ? ('user' as const) : ('assistant' as const),
    text: `message ${index} ${'x'.repeat(4_000)}`,
    toolUses: [] as [],
  }))

  await $.session.start(INTERACTIVE)
  await clock.settle()

  // The whole transcript would be 16 MB, more than the bridge accepts in one
  // request; a description that large is dropped and the panel stays empty.
  const body = JSON.stringify(posted[0]?.body ?? {})
  expect(body.length).toBeLessThan(3_000_000)
  const seeded = (events()[0]?.messages ?? []) as { text: string }[]
  expect(seeded.length).toBeGreaterThan(100)
  expect(seeded.at(-1)?.text.startsWith('message 3999 ')).toBe(true)

  exit()
  await clock.settle()
})

test('context usage is reported after each model request of a turn', async ($, on) => {
  const clock = mock.clock(on)
  mock.env(on, LEGATO_SESSION)
  const { session, events, exit } = world(on, { lines: [READY] })

  await $.session.start(INTERACTIVE)
  await $.turn.start({ text: 'hi', turnId: 'turn-1' })
  session.tokens = 61_000

  for await (const chunk of $.turn.step({
    turnId: 'turn-1',
    index: 0,
    model: 'claude-opus-5-5',
    messageCount: 1,
  })) {
    expect(chunk.kind).toBe('text')
  }

  await clock.settle()

  expect(events().find(event => event.type === 'usage')).toEqual(
    expect.objectContaining({ context: { tokens: 61_000, window: 200_000 } }),
  )

  exit()
  await clock.settle()
})

const STEP = { turnId: 'turn-1', index: 0, model: 'claude-opus-5-5', effort: 'medium', messageCount: 1 } as const

test('a model and effort chosen in Legato are what each request of the session uses', async ($, on) => {
  const clock = mock.clock(on)
  mock.env(on, LEGATO_SESSION)
  const { requested, events, exit } = world(on, {
    lines: [
      READY,
      JSON.stringify({ type: 'model', model: 'claude-sonnet-5-5' }),
      JSON.stringify({ type: 'effort', effort: 'xhigh' }),
    ],
  })

  await $.session.start(INTERACTIVE)
  await clock.settle()

  for await (const chunk of $.turn.step(STEP)) {
    expect(chunk.kind).toBe('text')
  }

  await clock.settle()

  expect(requested).toEqual([{ model: 'claude-sonnet-5-5', effort: 'xhigh' }])
  // The bridge is told what the session itself would have used, which is
  // what the panel shows as the default beside the choice.
  expect(events().find(event => event.type === 'settings')).toEqual(
    expect.objectContaining({ effort: 'medium' }),
  )

  exit()
  await clock.settle()
})

test('clearing the choice returns the session to its own model and effort', async ($, on) => {
  const clock = mock.clock(on)
  mock.env(on, LEGATO_SESSION)
  const { requested, exit } = world(on, {
    lines: [
      READY,
      JSON.stringify({ type: 'model', model: 'claude-sonnet-5-5' }),
      JSON.stringify({ type: 'effort', effort: 'xhigh' }),
      JSON.stringify({ type: 'model', model: null }),
      JSON.stringify({ type: 'effort', effort: null }),
    ],
  })

  await $.session.start(INTERACTIVE)
  await clock.settle()

  for await (const chunk of $.turn.step(STEP)) {
    expect(chunk.kind).toBe('text')
  }

  expect(requested).toEqual([{ model: 'claude-opus-5-5', effort: 'medium' }])

  exit()
  await clock.settle()
})

test('a subagent keeps its own model whatever was chosen for the session', async ($, on) => {
  const clock = mock.clock(on)
  mock.env(on, LEGATO_SESSION)
  const { requested, exit } = world(on, {
    lines: [READY, JSON.stringify({ type: 'model', model: 'claude-sonnet-5-5' })],
  })

  await $.session.start(INTERACTIVE)
  await clock.settle()

  for await (const chunk of $.turn.step({ ...STEP, model: 'claude-haiku-5-5', agentId: 'agent-7' })) {
    expect(chunk.kind).toBe('text')
  }

  expect(requested).toEqual([{ model: 'claude-haiku-5-5', effort: 'medium' }])

  exit()
  await clock.settle()
})

const COLOR_QUESTION = [
  {
    question: 'Which color?',
    header: 'Color',
    multiSelect: false,
    options: [
      { label: 'Red', description: 'Warm' },
      { label: 'Blue', description: 'Cool' },
    ],
  },
]

test('a question is answered with what the person chose in Legato', async ($, on) => {
  const clock = mock.clock(on)
  mock.env(on, LEGATO_SESSION)
  const { posted, exit } = world(on, {
    lines: [READY],
    permission: { decision: 'allow', answers: { 'Which color?': 'Blue' } },
  })

  await $.session.start(INTERACTIVE)
  await clock.settle()
  const answered = await $.classic.PermissionRequest({
    tool_name: 'AskUserQuestion',
    tool_input: { questions: COLOR_QUESTION },
  })

  // The answers travel inside the tool's own input, as its dialog's would.
  expect(answered.decision).toEqual({
    behavior: 'allow',
    updatedInput: { questions: COLOR_QUESTION, answers: { 'Which color?': 'Blue' } },
  })
  const asked = posted.find(post => post.url.endsWith('/mod/permissions'))
  expect(asked?.body).toEqual(
    expect.objectContaining({ tool: 'AskUserQuestion', input: { questions: COLOR_QUESTION } }),
  )

  exit()
  await clock.settle()
})

test('a denial reads to the model as the bridge worded it', async ($, on) => {
  const clock = mock.clock(on)
  mock.env(on, LEGATO_SESSION)
  const { exit } = world(on, {
    lines: [READY],
    permission: { decision: 'deny', feedback: 'later', message: 'The user dismissed these questions.' },
  })

  await $.session.start(INTERACTIVE)
  await clock.settle()
  const answered = await $.classic.PermissionRequest({
    tool_name: 'AskUserQuestion',
    tool_input: { questions: COLOR_QUESTION },
  })

  expect(answered.decision).toEqual({ behavior: 'deny', message: 'The user dismissed these questions.' })

  exit()
  await clock.settle()
})

test('a plan is approved by handing its input back with the edit mode chosen in Legato', async ($, on) => {
  const clock = mock.clock(on)
  mock.env(on, LEGATO_SESSION)
  const acceptEdits = [{ type: 'setMode', mode: 'acceptEdits', destination: 'session' }]
  const { exit } = world(on, {
    lines: [READY],
    permission: { decision: 'allow', returnInput: true, updatedPermissions: acceptEdits },
  })
  const plan = { plan: '# Add a comment\n\n- Edit README.md', planFilePath: '/home/me/.claude/plans/p.md' }

  await $.session.start(INTERACTIVE)
  await clock.settle()
  const answered = await $.classic.PermissionRequest({ tool_name: 'ExitPlanMode', tool_input: plan })

  expect(answered.decision).toEqual({
    behavior: 'allow',
    updatedInput: plan,
    updatedPermissions: acceptEdits,
  })

  exit()
  await clock.settle()
})

test('the rules the engine suggests for not asking again reach the bridge', async ($, on) => {
  const clock = mock.clock(on)
  mock.env(on, LEGATO_SESSION)
  const suggestions = [
    {
      type: 'addRules' as const,
      rules: [{ toolName: 'Bash', ruleContent: 'npm run:*' }],
      behavior: 'allow' as const,
      destination: 'localSettings' as const,
    },
  ]
  const { posted, exit } = world(on, {
    lines: [READY],
    permission: { decision: 'allow', updatedPermissions: suggestions },
  })

  await $.session.start(INTERACTIVE)
  await clock.settle()
  const answered = await $.classic.PermissionRequest({
    tool_name: 'Bash',
    tool_input: { command: 'npm run build' },
    permission_suggestions: suggestions,
  })

  const asked = posted.find(post => post.url.endsWith('/mod/permissions'))
  expect(asked?.body.suggestions).toEqual(suggestions)
  // Allowed as asked for: the command itself is not rewritten.
  expect(answered.decision).toEqual({ behavior: 'allow', updatedPermissions: suggestions })

  exit()
  await clock.settle()
})

test('the model\'s thinking reaches the bridge as it streams, apart from its reply', async ($, on) => {
  const clock = mock.clock(on)
  mock.env(on, LEGATO_SESSION)
  const { events, exit } = world(on, {
    lines: [READY],
    reply: [
      { kind: 'thinking', index: 0, text: 'The flag belongs ' },
      { kind: 'thinking', index: 0, text: 'in list.ts.' },
      { kind: 'text', index: 1, text: 'Adding it.' },
    ],
  })

  await $.session.start(INTERACTIVE)
  await $.turn.start({ text: 'add a flag', turnId: 'turn-1' })

  const passed: string[] = []

  for await (const chunk of $.turn.step(STEP)) {
    passed.push(chunk.kind)
  }

  await clock.settle()

  // Every piece went on to the engine as it came.
  expect(passed).toEqual(['thinking', 'thinking', 'text'])

  const streamed = events()
    .filter(event => event.type === 'thinking_delta' || event.type === 'text_delta')
    .map(event => ({ type: event.type, index: event.index, text: event.text }))

  expect(streamed.map(event => event.type)).toEqual(expect.arrayContaining(['thinking_delta', 'text_delta']))
  expect(streamed.filter(event => event.type === 'thinking_delta').map(event => event.text).join('')).toBe(
    'The flag belongs in list.ts.',
  )
  expect(streamed.filter(event => event.type === 'text_delta').map(event => event.text).join('')).toBe('Adding it.')
  expect(streamed.find(event => event.type === 'thinking_delta')).toEqual(
    expect.objectContaining({ index: 0 }),
  )

  exit()
  await clock.settle()
})

test('a stored thinking block reaches the bridge with what can be read of it and no more', async ($, on) => {
  const clock = mock.clock(on)
  mock.env(on, LEGATO_SESSION)
  mock.session(on)
  const { events, exit } = world(on, { lines: [READY] })

  await $.session.start(INTERACTIVE)
  await $.session.append({
    uuid: 'row-1',
    door: 'response',
    origin: { kind: 'model', model: 'claude-opus-5-5' },
    message: {
      type: 'assistant',
      role: 'assistant',
      content: [{ type: 'thinking', thinking: 'Read the file first.', signature: 'signed-by-the-model' }],
    },
  } as never)
  // Most models keep their thinking to themselves: the block has no text.
  await $.session.append({
    uuid: 'row-2',
    door: 'response',
    origin: { kind: 'model', model: 'claude-opus-5-5' },
    message: {
      type: 'assistant',
      role: 'assistant',
      content: [{ type: 'thinking', thinking: '', signature: 'signed-by-the-model' }],
    },
  } as never)
  await clock.settle()

  const rows = events()
    .filter(event => event.type === 'row')
    .map(event => (event.message as { content: unknown }).content)

  // The empty block still goes: it is what settles a thought that streamed.
  expect(rows).toEqual([
    [{ type: 'thinking', thinking: 'Read the file first.' }],
    [{ type: 'thinking', thinking: '' }],
  ])
  expect(JSON.stringify(events())).not.toContain('signed-by-the-model')

  exit()
  await clock.settle()
})

test('a session that ends for good tells its bridge to go', async ($, on) => {
  const clock = mock.clock(on)
  mock.env(on, LEGATO_SESSION)
  const { posted, events, exit } = world(on, { lines: [READY] })

  await $.session.start(INTERACTIVE)
  await clock.settle()

  // `/background`: the conversation goes on in another Claude process, which
  // starts a bridge of its own. This one must let go of the task first.
  await $.session.end({
    reason: 'prompt_input_exit',
    sessionId: 'claude-session-1',
    resume: { id: 'claude-session-1' },
  })

  expect(events().at(-1)).toEqual({ type: 'ended' })
  expect(posted.at(-1)?.url).toBe('http://bridge.test/secret/mod/events')

  exit()
  await clock.settle()
})

test('a session that started again after ending gets a bridge of its own', async ($, on) => {
  const clock = mock.clock(on)
  mock.env(on, LEGATO_SESSION)
  const { spawned, exit } = world(on, { lines: [READY] })

  await $.session.start(INTERACTIVE)
  await clock.settle()
  await $.session.end({
    reason: 'other',
    sessionId: 'claude-session-1',
    resume: { id: 'claude-session-1' },
  })

  // The bridge that was told to go may not have exited yet.
  await $.session.start(INTERACTIVE)
  await clock.settle()

  expect(spawned).toHaveLength(2)

  exit()
  await clock.settle()
})

test('a /clear keeps the bridge it has', async ($, on) => {
  const clock = mock.clock(on)
  mock.env(on, LEGATO_SESSION)
  const { events, exit } = world(on, { lines: [READY] })

  await $.session.start(INTERACTIVE)
  await clock.settle()
  await $.session.end({
    reason: 'clear',
    sessionId: 'claude-session-1',
    resume: { id: 'claude-session-1' },
  })
  await clock.settle()

  expect(events().map(event => event.type)).not.toContain('ended')

  exit()
  await clock.settle()
})

test('a /clear reports the replacement identity once the engine switches sessions', async ($, on) => {
  const clock = mock.clock(on)
  mock.env(on, LEGATO_SESSION)
  const { session, spawned, events, exit } = world(on, { lines: [READY] })

  await $.session.start(INTERACTIVE)
  await clock.settle()
  await $.session.end({
    reason: 'clear',
    sessionId: session.id,
    resume: { id: session.id },
  })
  await clock.settle()
  expect(events().filter(event => event.type === 'reset')).toEqual([expect.objectContaining({ type: 'reset' })])

  session.id = 'claude-cleared-session'
  session.cwd = '/work/cleared'
  await clock.advance(100)

  expect(events().filter(event => event.type === 'identity')).toEqual([
    expect.objectContaining({ type: 'identity', sessionId: 'claude-cleared-session', cwd: '/work/cleared' }),
  ])
  expect(spawned).toHaveLength(1)

  exit()
  await clock.settle()
})

for (const reason of ['clear', 'resume'] as const) {
  test(`a /${reason} that never switches identity reports failure instead of the old ID`, async ($, on) => {
    const clock = mock.clock(on)
    mock.env(on, LEGATO_SESSION)
    const { session, events, logs, exit } = world(on, { lines: [READY] })

    await $.session.start(INTERACTIVE)
    await clock.settle()
    await $.session.end({ reason, sessionId: session.id, resume: { id: session.id } })
    await clock.advance(6_000)

    expect(events().filter(event => event.type === 'hello')).toHaveLength(1)
    expect(events().filter(event => event.type === 'reset' && event.sessionId !== undefined)).toEqual([])
    expect(logs.join('\n')).toContain(`/${reason} did not expose a replacement session ID`)

    exit()
    await clock.settle()
  })
}

test('identity transitions survive a full startup queue in order', async ($, on) => {
  const clock = mock.clock(on)
  mock.env(on, LEGATO_SESSION)
  let ready = (): void => {}
  const startsAfter = new Promise<void>(resolve => {
    ready = resolve
  })
  const { session, events, exit } = world(on, { lines: [READY], startsAfter })

  await $.session.start(INTERACTIVE)
  for (let index = 0; index < 5_000; index += 1) {
    await $.turn.start({ text: 'hi', turnId: `turn-${index}` })
  }
  await $.session.end({ reason: 'clear', sessionId: session.id, resume: { id: session.id } })
  session.id = 'claude-cleared-session'
  session.cwd = '/work/cleared'
  await clock.advance(100)

  await $.session.end({ reason: 'resume', sessionId: session.id, resume: { id: session.id } })
  session.id = 'claude-resumed-session'
  session.cwd = '/work/resumed'
  await clock.advance(100)
  ready()
  await clock.settle()

  expect(events().filter(event => event.type === 'hello' || event.type === 'reset' || event.type === 'identity').map(
    ({ type, sessionId, cwd }) => ({ type, sessionId, cwd }),
  )).toEqual([
    { type: 'hello', sessionId: 'claude-session-1', cwd: '/work/repo' },
    { type: 'reset', sessionId: undefined, cwd: undefined },
    { type: 'identity', sessionId: 'claude-cleared-session', cwd: '/work/cleared' },
    { type: 'reset', sessionId: undefined, cwd: undefined },
    { type: 'hello', sessionId: 'claude-resumed-session', cwd: '/work/resumed' },
  ])

  exit()
  await clock.settle()
})

// What a background worker inherits: the environment of the daemon that
// started it, which is that of some other Legato session.
const INHERITED = {
  LEGATO_TASK_ID: 'someone-else',
  LEGATO_EXECUTION_ID: 'ex-someone-else',
  LEGATO_BIN: '/opt/other/legato',
  CLAUDE_CODE_SESSION_KIND: 'bg',
}

const CONVERSATION: Transcript = [
  { role: 'user', text: 'refactor the sync service', toolUses: [] },
  { role: 'assistant', text: 'Done: three files changed.', toolUses: [] },
]

test('a session that ends leaves a note of which task it served', async ($, on) => {
  const clock = mock.clock(on, { now: 5_000 })
  mock.env(on, LEGATO_SESSION)
  const { session, store, exit } = world(on, { lines: [READY] })
  session.messages = CONVERSATION

  await $.session.start(INTERACTIVE)
  await clock.settle()
  await $.session.end({
    reason: 'prompt_input_exit',
    sessionId: 'claude-session-1',
    resume: { id: 'claude-session-1' },
  })

  expect(store.get('handover:claude-session-1')).toEqual(
    expect.objectContaining({
      at: 5_000,
      cwd: '/work/repo',
      count: 2,
      env: { LEGATO_TASK_ID: 'task-1', LEGATO_EXECUTION_ID: 'ex-1', LEGATO_BIN: '/opt/legato' },
    }),
  )

  exit()
  await clock.settle()
})

test('a session sent to the background serves the task its note names, not the one it inherited', async ($, on) => {
  const clock = mock.clock(on, { now: 5_000 })
  const env: Record<string, string> = { ...LEGATO_SESSION }
  const { session, spawned, spawnedWith, envSet, store, exit } = world(on, { lines: [READY], env })
  session.messages = CONVERSATION

  await $.session.start(INTERACTIVE)
  await clock.settle()
  await $.session.end({
    reason: 'prompt_input_exit',
    sessionId: 'claude-session-1',
    resume: { id: 'claude-session-1' },
  })

  // The worker that goes on with the conversation: a process of the daemon,
  // which has another session's environment.
  for (const name of Object.keys(env)) {
    delete env[name]
  }

  Object.assign(env, INHERITED)
  session.id = 'claude-session-2'
  session.messages = [...CONVERSATION, { role: 'user', text: 'and now the tests', toolUses: [] }]

  await $.session.start(INTERACTIVE)
  await clock.settle()

  expect(spawned.at(-1)).toEqual(['/opt/legato', 'agent', 'bridge', 'task-1', '--execution-id', 'ex-1'])
  expect(spawnedWith.at(-1)).toEqual(expect.objectContaining({ LEGATO_TASK_ID: 'task-1' }))
  // Hooks and tools the session starts from here on act for its own task.
  expect(envSet).toEqual(
    expect.objectContaining({ LEGATO_TASK_ID: 'task-1', LEGATO_EXECUTION_ID: 'ex-1', LEGATO_BIN: '/opt/legato' }),
  )
  // The note is this session's from now on: a worker started again finds it.
  expect(store.has('handover:claude-session-1')).toBe(false)
  expect(store.get('session:claude-session-2')).toEqual(
    expect.objectContaining({ env: expect.objectContaining({ LEGATO_TASK_ID: 'task-1' }) }),
  )

  exit()
  await clock.settle()
})

test('a background session whose daemon was started outside Legato still finds its task', async ($, on) => {
  const clock = mock.clock(on, { now: 5_000 })
  const env: Record<string, string> = { ...LEGATO_SESSION }
  const { session, spawned, exit } = world(on, { lines: [READY], env })
  session.messages = CONVERSATION

  await $.session.start(INTERACTIVE)
  await clock.settle()
  await $.session.end({
    reason: 'prompt_input_exit',
    sessionId: 'claude-session-1',
    resume: { id: 'claude-session-1' },
  })

  for (const name of Object.keys(env)) {
    delete env[name]
  }

  env.CLAUDE_CODE_SESSION_KIND = 'bg'
  session.id = 'claude-session-2'

  await $.session.start(INTERACTIVE)
  await clock.settle()

  expect(spawned.at(-1)).toEqual(['/opt/legato', 'agent', 'bridge', 'task-1', '--execution-id', 'ex-1'])

  exit()
  await clock.settle()
})

test('a background session with no note of its own serves no task', async ($, on) => {
  const clock = mock.clock(on, { now: 5_000 })
  mock.env(on, INHERITED)
  const { session, spawned, envSet, store } = world(on, {
    lines: [READY],
    store: {
      // Another conversation, ended in the same directory a moment ago.
      'handover:claude-session-9': {
        at: 4_000,
        cwd: '/work/repo',
        count: 1,
        tail: ['user:zzzz'],
        env: { LEGATO_TASK_ID: 'task-9' },
      },
    },
  })
  session.messages = CONVERSATION

  const starting = $.session.start(INTERACTIVE)
  await clock.advance(10_000)
  await starting
  await clock.settle()

  expect(spawned).toEqual([])
  expect('LEGATO_TASK_ID' in envSet && envSet.LEGATO_TASK_ID === undefined).toBe(true)
  expect('LEGATO_EXECUTION_ID' in envSet && envSet.LEGATO_EXECUTION_ID === undefined).toBe(true)
  expect(store.has('handover:claude-session-9')).toBe(true)
})

test('a background session started again finds the task it took over', async ($, on) => {
  const clock = mock.clock(on, { now: 5_000 })
  mock.env(on, INHERITED)
  const { spawned, exit } = world(on, {
    lines: [READY],
    store: {
      'session:claude-session-1': { at: 1_000, env: { LEGATO_TASK_ID: 'task-1', LEGATO_BIN: '/opt/legato' } },
    },
  })

  await $.session.start(INTERACTIVE)
  await clock.settle()

  expect(spawned).toEqual([['/opt/legato', 'agent', 'bridge', 'task-1']])

  exit()
  await clock.settle()
})

test('the process a session was sent to the background from does not take the task back', async ($, on) => {
  const clock = mock.clock(on, { now: 5_000 })
  mock.env(on, LEGATO_SESSION)
  // The process can stay on as a viewer of the background session, with the
  // task's own variables; a reload of the mod starts its session again.
  const { spawned } = world(on, {
    lines: [READY],
    store: {
      'session:claude-session-2': { at: 4_000, env: { LEGATO_TASK_ID: 'task-1', LEGATO_EXECUTION_ID: 'ex-1' } },
    },
  })

  await $.session.start(INTERACTIVE)
  await clock.settle()

  expect(spawned).toEqual([])
})

test('a later launch of the task is not held back by an earlier launch that went to the background', async ($, on) => {
  const clock = mock.clock(on, { now: 5_000 })
  mock.env(on, LEGATO_SESSION)
  const { spawned, exit } = world(on, {
    lines: [READY],
    store: {
      'session:claude-session-2': { at: 4_000, env: { LEGATO_TASK_ID: 'task-1', LEGATO_EXECUTION_ID: 'ex-0' } },
    },
  })

  await $.session.start(INTERACTIVE)
  await clock.settle()

  expect(spawned).toHaveLength(1)

  exit()
  await clock.settle()
})

test('a background session that ends hands its note on', async ($, on) => {
  const clock = mock.clock(on, { now: 5_000 })
  mock.env(on, INHERITED)
  const { session, store, exit } = world(on, {
    lines: [READY],
    store: {
      'session:claude-session-1': { at: 1_000, env: { LEGATO_TASK_ID: 'task-1', LEGATO_EXECUTION_ID: 'ex-1' } },
    },
  })
  session.messages = CONVERSATION

  await $.session.start(INTERACTIVE)
  await clock.settle()
  await $.session.end({
    reason: 'other',
    sessionId: 'claude-session-1',
    resume: { id: 'claude-session-1' },
  })

  // Nothing holds the task any more; a worker started again for the same
  // conversation takes the note as any session going on with it would.
  expect(store.has('session:claude-session-1')).toBe(false)
  expect(store.get('handover:claude-session-1')).toEqual(
    expect.objectContaining({ count: 2, env: expect.objectContaining({ LEGATO_TASK_ID: 'task-1' }) }),
  )

  exit()
  await clock.settle()
})

test('clear resets before new rows and reports its new identity non-destructively', async ($, on) => {
  const clock = mock.clock(on)
  mock.env(on, LEGATO_SESSION)
  mock.session(on)
  const { session, events, exit } = world(on, { lines: [READY] })
  await $.session.start(INTERACTIVE)
  await clock.settle()
  await $.session.end({ reason: 'clear', sessionId: session.id, resume: { id: session.id } })
  session.id = 'cleared-session'
  await $.session.append({
    uuid: 'new-row', door: 'prompt', origin: { kind: 'user' },
    message: { type: 'user', role: 'user', content: [{ type: 'text', text: 'fresh question' }] },
  } as never)
  await $.turn.start({ text: 'fresh question', turnId: 'new-turn' })
  await clock.advance(100)
  expect(events().map(event => event.type)).toEqual(['hello', 'reset', 'row', 'turn_start', 'identity'])
  expect(events().at(-1)).toEqual(expect.objectContaining({ type: 'identity', sessionId: 'cleared-session', cwd: '/work/repo' }))
  exit()
  await clock.settle()
})

test('a later transition invalidates the earlier identity poll', async ($, on) => {
  const clock = mock.clock(on)
  mock.env(on, LEGATO_SESSION)
  const { session, events, exit } = world(on, { lines: [READY] })
  await $.session.start(INTERACTIVE)
  await clock.settle()
  await $.session.end({ reason: 'clear', sessionId: session.id, resume: { id: session.id } })
  session.id = 'cleared-session'
  await $.session.end({ reason: 'resume', sessionId: session.id, resume: { id: session.id } })
  session.id = 'resumed-session'
  session.messages = [{ role: 'assistant', text: 'resumed answer', toolUses: [] }]
  await clock.advance(100)
  expect(events().map(event => event.type)).toEqual(['hello', 'reset', 'reset', 'hello'])
  expect(events().at(-1)).toEqual(expect.objectContaining({ sessionId: 'resumed-session' }))
  exit()
  await clock.settle()
})

for (const reason of ['clear', 'resume'] as const) {
  test(`a background /${reason} keeps its task when the successor starts before the poll`, async ($, on) => {
    const clock = mock.clock(on, { now: 5_000 })
    mock.env(on, INHERITED)
    const { session, spawned, envSet, store, events, exit } = world(on, {
      lines: [READY],
      store: { 'session:claude-session-1': { at: 1_000, env: LEGATO_SESSION } },
    })
    await $.session.start(INTERACTIVE)
    await clock.settle()
    await $.session.end({ reason, sessionId: session.id, resume: { id: session.id } })
    session.id = 'continued-background-session'
    await $.session.start(INTERACTIVE)
    await clock.advance(100)
    expect(spawned).toHaveLength(1)
    expect(envSet.LEGATO_TASK_ID).toBe('task-1')
    expect(store.has('session:claude-session-1')).toBe(false)
    expect(store.get('session:continued-background-session')).toEqual(expect.objectContaining({ env: LEGATO_SESSION }))
    expect(events().at(-1)).toEqual(expect.objectContaining({ type: 'hello', sessionId: session.id }))
    exit()
    await clock.settle()
  })
}
