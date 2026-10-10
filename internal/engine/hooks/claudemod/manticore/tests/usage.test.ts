import { expect, mock, test } from 'claude-code/testing'
import type { MockClock } from 'claude-code/testing'
import type { Chunk, On, TurnStepResult } from 'claude-code'

type Posted = { url: string; headers: Record<string, string>; body: { records: Record<string, unknown>[] } }

const MANTICORE = {
  LEGATO_TASK_ID: 'task-1',
  LEGATO_MANTICORE_URL: 'http://manticore.test:3456',
  LEGATO_MANTICORE_API_KEY: 'mc_secret',
}

const STEP = { turnId: 'turn-1', index: 0, model: 'claude-opus-5-5', messageCount: 1 } as const

const USAGE = {
  model: 'claude-opus-5-5',
  input_tokens: 50,
  output_tokens: 300,
  cache_read_input_tokens: 1000,
  cache_creation_input_tokens: 150,
}

// Everything beneath the mod: the model's answer to each request and the
// Manticore instance its reports are posted to.
const world = (
  on: On,
  script: {
    usage?: typeof USAGE | null
    // A model that takes its time: how long to the first chunk and from
    // there to the end, on the test's clock.
    slow?: { clock: MockClock; firstTokenAfterMs: number; restAfterMs: number }
    // How Manticore answers each report in turn: a status, or `down` for an
    // instance that cannot be reached. Reports past the list are accepted.
    manticore?: (number | 'down')[]
    // A Manticore that takes the connection and says nothing until the test
    // lets it.
    silentUntil?: Promise<void>
  } = {},
) => {
  const posted: Posted[] = []

  on('http.fetch', async ($, e) => {
    posted.push({
      url: e.url,
      headers: e.init?.headers ?? {},
      body: JSON.parse(e.init?.body ?? '{}'),
    })

    await script.silentUntil

    const answer = script.manticore?.[posted.length - 1] ?? 200

    if (answer === 'down') {
      throw new Error('connect ECONNREFUSED')
    }

    return { value: { status: answer, ok: answer < 300, headers: {}, text: '{}' } }
  })
  on('turn.step', async function* ($, e) {
    await script.slow?.clock.sleep(script.slow.firstTokenAfterMs)

    yield { kind: 'text', index: 0, text: 'ok' }

    await script.slow?.clock.sleep(script.slow.restAfterMs)

    return {
      turnId: e.turnId,
      index: e.index,
      answer: 'ok',
      toolUses: [],
      stopReason: 'end_turn',
      usage: script.usage === undefined ? USAGE : script.usage,
    }
  })
  on('session.id', () => ({ value: 'claude-session-1' }))
  on('session.end', ($, e) => ({ sessionId: e.sessionId }))
  on('ui.log', () => ({ value: undefined }))

  const records = () => posted.flatMap(post => post.body.records)

  return { posted, records }
}

// Runs one model request the way the engine does: every chunk read, then
// the response the stream returns.
const request = async (
  stream: AsyncGenerator<Chunk<'turn.step'>, TurnStepResult>,
): Promise<{ chunks: Chunk<'turn.step'>[]; response: TurnStepResult }> => {
  const chunks: Chunk<'turn.step'>[] = []

  for (;;) {
    const step = await stream.next()

    if (step.done) {
      return { chunks, response: step.value }
    }

    chunks.push(step.value)
  }
}

test('each model request is reported to Manticore with what it cost', async ($, on) => {
  const clock = mock.clock(on)
  mock.env(on, MANTICORE)
  const { posted, records } = world(on)

  await request($.turn.step(STEP))
  await clock.settle()

  expect(posted).toHaveLength(1)
  expect(posted[0]?.url).toBe('http://manticore.test:3456/v1/usage')
  // The Client's key authenticates the report; the Legato task groups it.
  expect(posted[0]?.headers).toEqual(
    expect.objectContaining({ authorization: 'Bearer mc_secret', 'x-session-id': 'task-1' }),
  )
  expect(records()).toEqual([
    expect.objectContaining({
      model: 'claude-opus-5-5',
      upstream: 'anthropic',
      // The whole prompt: what was read from and written to the cache counts.
      prompt_tokens: 1200,
      completion_tokens: 300,
      cache_read_tokens: 1000,
      cache_creation_tokens: 150,
      finish_reason: 'end_turn',
    }),
  ])
})

test('a request passes through exactly as it came, reported or not', async ($, on) => {
  const clock = mock.clock(on)
  mock.env(on, MANTICORE)
  world(on)

  const { chunks, response } = await request($.turn.step(STEP))
  await clock.settle()

  expect(chunks).toEqual([{ kind: 'text', index: 0, text: 'ok' }])
  expect(response).toEqual(
    expect.objectContaining({ turnId: 'turn-1', index: 0, answer: 'ok', stopReason: 'end_turn', usage: USAGE }),
  )
})

test('with no Manticore instance configured nothing is sent', async ($, on) => {
  const clock = mock.clock(on)
  mock.env(on, { LEGATO_TASK_ID: 'task-1' })
  const { posted } = world(on)

  const { chunks, response } = await request($.turn.step(STEP))
  await clock.settle()

  expect(posted).toEqual([])
  expect(chunks).toHaveLength(1)
  expect(response.usage).toEqual(USAGE)
})

test('a request that got no response is not reported', async ($, on) => {
  const clock = mock.clock(on)
  mock.env(on, MANTICORE)
  const { posted } = world(on, { usage: null })

  await request($.turn.step(STEP))
  await clock.settle()

  expect(posted).toEqual([])
})

test('outside a Legato task the Claude session groups the requests', async ($, on) => {
  const clock = mock.clock(on)
  mock.env(on, { LEGATO_MANTICORE_URL: 'http://manticore.test:3456/', LEGATO_MANTICORE_API_KEY: 'mc_secret' })
  const { posted } = world(on)

  await request($.turn.step(STEP))
  await clock.settle()

  expect(posted[0]?.url).toBe('http://manticore.test:3456/v1/usage')
  expect(posted[0]?.headers['x-session-id']).toBe('claude-session-1')
})

test('a report says when the request began and how long the model took', async ($, on) => {
  const clock = mock.clock(on, { now: 1_790_000_000_000 })
  mock.env(on, MANTICORE)
  const { records } = world(on, { slow: { clock, firstTokenAfterMs: 800, restAfterMs: 3400 } })

  const running = request($.turn.step(STEP))
  await clock.settle()
  await clock.advance(800)
  await clock.advance(3400)
  await running
  await clock.settle()

  expect(records()).toEqual([
    expect.objectContaining({ started_at: 1_790_000_000_000, time_to_first_token_ms: 800, latency_ms: 4200 }),
  ])
})

test('each request has an id of its own, so a report sent twice is counted once', async ($, on) => {
  const clock = mock.clock(on)
  mock.env(on, MANTICORE)
  const { records } = world(on)

  await request($.turn.step(STEP))
  await request($.turn.step({ ...STEP, index: 1 }))
  // A subagent's request in the same turn position is another request.
  await request($.turn.step({ ...STEP, turnId: 'turn-2', index: 0, agentId: 'agent-7' }))
  await clock.settle()

  expect(records().map(record => record.id)).toEqual([
    'claude-session-1:turn-1:0',
    'claude-session-1:turn-1:1',
    'claude-session-1:turn-2:0',
  ])
})

for (const [state, answer] of [
  ['cannot be reached', 'down'],
  ['is failing', 503],
  ['is too busy', 429],
] as const) {
  test(`a report sent while Manticore ${state} goes out again with the next`, async ($, on) => {
    const clock = mock.clock(on)
    mock.env(on, MANTICORE)
    const { posted } = world(on, { manticore: [answer] })

    await request($.turn.step(STEP))
    await clock.settle()
    await request($.turn.step({ ...STEP, index: 1 }))
    await clock.settle()

    expect(posted).toHaveLength(2)
    expect(posted[1]?.body.records.map(record => record.id)).toEqual([
      'claude-session-1:turn-1:0',
      'claude-session-1:turn-1:1',
    ])
  })
}

for (const [reason, answer] of [
  ['finds invalid', 400],
  ['refuses the key of', 401],
] as const) {
  test(`a report Manticore ${reason} is not sent again`, async ($, on) => {
    const clock = mock.clock(on)
    mock.env(on, MANTICORE)
    const { posted } = world(on, { manticore: [answer] })

    await request($.turn.step(STEP))
    await clock.settle()
    await request($.turn.step({ ...STEP, index: 1 }))
    await clock.settle()

    // Sending it again would get the same answer, and hold up what follows.
    expect(posted).toHaveLength(2)
    expect(posted[1]?.body.records.map(record => record.id)).toEqual(['claude-session-1:turn-1:1'])
  })
}

test('what is still unreported goes out when the session ends', async ($, on) => {
  const clock = mock.clock(on)
  mock.env(on, MANTICORE)
  const { posted } = world(on, { manticore: ['down'] })

  await request($.turn.step(STEP))
  await clock.settle()
  await $.session.end({ sessionId: 'claude-session-1', reason: 'other', resume: { id: 'claude-session-1' } })

  expect(posted).toHaveLength(2)
  expect(posted[1]?.body.records.map(record => record.id)).toEqual(['claude-session-1:turn-1:0'])
})

test('a Manticore that does not answer holds no request up', async ($, on) => {
  const clock = mock.clock(on)
  mock.env(on, MANTICORE)
  let answer = (): void => {}
  const silentUntil = new Promise<void>(resolve => {
    answer = resolve
  })
  const { posted, records } = world(on, { silentUntil })

  // Both requests run to their end while the first report is still open.
  const first = await request($.turn.step(STEP))
  await clock.settle()
  const second = await request($.turn.step({ ...STEP, index: 1 }))
  await clock.settle()

  expect(first.response.answer).toBe('ok')
  expect(second.response.answer).toBe('ok')
  // One report at a time: the second request waits its turn in the queue.
  expect(posted).toHaveLength(1)

  answer()
  await clock.settle()

  expect(records().map(record => record.id)).toEqual(['claude-session-1:turn-1:0', 'claude-session-1:turn-1:1'])
})

test('after an outage the backlog goes out in reports small enough for Manticore to take', async ($, on) => {
  const clock = mock.clock(on)
  mock.env(on, MANTICORE)
  const OUTAGE = 250
  const { posted } = world(on, { manticore: Array.from({ length: OUTAGE }, () => 'down' as const) })

  for (let index = 0; index <= OUTAGE; index++) {
    await request($.turn.step({ ...STEP, index }))
    await clock.settle()
  }

  // Manticore refuses a report of more than 500 records outright.
  const afterOutage = posted.slice(OUTAGE).map(post => post.body.records.length)
  expect(afterOutage).toEqual([200, 51])
  expect(posted[OUTAGE]?.body.records[0]?.id).toBe('claude-session-1:turn-1:0')
  expect(posted.at(-1)?.body.records.at(-1)?.id).toBe(`claude-session-1:turn-1:${OUTAGE}`)
})

test('an outage that outlasts the backlog loses the oldest requests, never the session', async ($, on) => {
  const clock = mock.clock(on)
  mock.env(on, MANTICORE)
  const HELD = 2000
  const { posted } = world(on, { manticore: Array.from({ length: HELD + 1 }, () => 'down' as const) })

  for (let index = 0; index <= HELD + 1; index++) {
    await request($.turn.step({ ...STEP, index }))
    await clock.settle()
  }

  const reported = posted.slice(HELD + 1).flatMap(post => post.body.records.map(record => record.id))
  expect(reported).toHaveLength(HELD)
  // The two oldest were let go to make room.
  expect(reported[0]).toBe('claude-session-1:turn-1:2')
  expect(reported.at(-1)).toBe(`claude-session-1:turn-1:${HELD + 1}`)
})
