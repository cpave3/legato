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
  script: { lines?: string[]; permission?: object; startsAfter?: Promise<void>; reply?: Piece[] } = {},
) => {
  // The session the mod reads; a test swaps it to stand for a resume.
  const session: { id: string; messages: Transcript; tokens: number } = {
    id: 'claude-session-1',
    messages: [],
    tokens: 10,
  }
  const spawned: string[][] = []
  // What each model request reached the engine with, after the mod.
  const requested: { model: string; effort: unknown }[] = []
  const posted: Posted[] = []
  const submitted: string[] = []
  let exit = (): void => {}
  const exited = new Promise<void>(resolve => {
    exit = resolve
  })

  on('process.spawn', async function* ($, e) {
    spawned.push([...e.argv])
    await script.startsAfter

    for (const line of script.lines ?? []) {
      yield { stream: 'stdout', text: `${line}\n` }
    }

    await exited

    return { value: { code: 0, signal: null } }
  })
  on('http.fetch', ($, e) => {
    posted.push({ url: e.url, body: JSON.parse(e.init?.body ?? '{}') })

    const text = e.url.endsWith('/mod/permissions')
      ? JSON.stringify(script.permission ?? { decision: 'ask' })
      : ''

    return { value: { status: 200, ok: true, headers: {}, text } }
  })
  on('prompt.submit', ($, e) => {
    submitted.push(e.text)

    return { text: e.text }
  })
  on('session.start', ($, e) => ({ cwd: e.cwd }))
  on('session.end', ($, e) => ({ sessionId: e.sessionId }))
  on('session.id', () => ({ value: session.id }))
  on('session.cwd', () => ({ value: '/work/repo' }))
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
  on('ui.log', () => ({ value: undefined }))
  on('turn.start', ($, e) => ({ turnId: e.turnId }))
  on('turn.complete', ($, e) => ({ text: e.answer }))
  on('classic.PermissionRequest', () => ({}))

  const events = () => posted.flatMap(post => post.body.events ?? [])

  return { session, spawned, requested, posted, submitted, events, exit }
}

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

  // Folded into the running turn, not submitted as a prompt of its own.
  expect(submitted).toEqual([])
  expect(JSON.stringify(rows.appended())).toContain('also update the docs')

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

test('resuming another session inside the process describes that session to the bridge', async ($, on) => {
  const clock = mock.clock(on)
  mock.env(on, LEGATO_SESSION)
  const { session, events, exit } = world(on, { lines: [READY] })

  await $.session.start(INTERACTIVE)
  await clock.settle()

  // `/resume`: the process goes on under another session, whose transcript
  // was loaded rather than appended, so no row announces it.
  session.id = 'claude-session-2'
  session.messages = [
    { role: 'user', text: 'earlier question', toolUses: [] },
    { role: 'assistant', text: 'earlier answer', toolUses: [] },
  ]
  session.tokens = 88_000
  await $.session.end({
    reason: 'resume',
    sessionId: 'claude-session-1',
    resume: { id: 'claude-session-1' },
  })
  await clock.advance(1_000)

  const described = events().filter(event => event.type === 'hello')
  expect(described).toHaveLength(2)
  expect(described[1]).toEqual(
    expect.objectContaining({
      sessionId: 'claude-session-2',
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
