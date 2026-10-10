// The Legato usage mod for Claude Code.
//
// A Claude Code session talks to its model provider directly, so a Manticore
// instance never sees its requests. In a session Legato launched with a
// Manticore instance configured, this module tells that instance what each
// model request cost: the token counts the API reported and the model that
// answered. It reports figures only, never a prompt or a reply, and it never
// touches a request: every chunk and response passes through as it came.
// With no instance configured the module does nothing.
import type { EngineInterface, Register, TurnUsage } from 'claude-code'

// One request, in the words of Manticore's POST /v1/usage.
type UsageRecord = {
  // The request's own id: Manticore stores a record once however often its
  // report is sent.
  id: string
  model: string
  upstream: string
  prompt_tokens: number
  completion_tokens: number
  cache_read_tokens: number
  cache_creation_tokens: number
  latency_ms: number
  time_to_first_token_ms?: number
  finish_reason?: string
  started_at: number
}

// What the module measured around one request.
type Timing = { startedAt: number; firstTokenAt: number | undefined; endedAt: number }

// Where reports go. `taskId` is the Legato task the session runs under: the
// session Manticore groups its requests by, whatever Claude session
// (`/clear`, `/resume`) made them.
type Target = { url: string; key: string; taskId: string | undefined }

type Pending = { sessionId: string; record: UsageRecord }

// The provider a Claude Code session's requests go to, as Manticore's
// upstream breakdown will name it.
const UPSTREAM = 'anthropic'

// How many requests one report carries at most. Manticore takes up to 500
// and turns a larger report down whole.
const BATCH_LIMIT = 200

// How many unreported requests are held for a Manticore that is not taking
// reports. Past it the oldest are let go: a session never grows without
// bound over its own accounting.
const QUEUE_LIMIT = 2000

// The module's state, the session's for as long as this copy is loaded.
let target: Promise<Target | undefined> | undefined
// Requests not yet reported, oldest first. A report Manticore could not take
// stays here and goes out again with the next request's.
let queue: Pending[] = []
// The one run of reports under way, if any: reports go out one at a time.
let sending: Promise<void> | undefined

// Where reports go, read once from the session's environment: Legato sets
// these on every Claude Code session it launches when a Manticore instance
// is configured.
const readTarget = async ($: EngineInterface): Promise<Target | undefined> => {
  const url = (await $.env.get('LEGATO_MANTICORE_URL'))?.trim().replace(/\/+$/, '')
  const key = (await $.env.get('LEGATO_MANTICORE_API_KEY'))?.trim()

  if (!url || !key) {
    return undefined
  }

  return { url, key, taskId: (await $.env.get('LEGATO_TASK_ID'))?.trim() || undefined }
}

const targetOf = ($: EngineInterface): Promise<Target | undefined> => {
  target ??= readTarget($).catch(() => undefined)

  return target
}

const recordOf = (id: string, usage: TurnUsage, stopReason: string | null, timing: Timing): UsageRecord => ({
  id,
  model: usage.model,
  upstream: UPSTREAM,
  // Manticore counts the whole prompt; the API reports the part served from
  // the cache and the part written to it apart from the rest.
  prompt_tokens: usage.input_tokens + usage.cache_read_input_tokens + usage.cache_creation_input_tokens,
  completion_tokens: usage.output_tokens,
  cache_read_tokens: usage.cache_read_input_tokens,
  cache_creation_tokens: usage.cache_creation_input_tokens,
  latency_ms: Math.max(0, Math.round(timing.endedAt - timing.startedAt)),
  ...(timing.firstTokenAt === undefined
    ? {}
    : { time_to_first_token_ms: Math.max(0, Math.round(timing.firstTokenAt - timing.startedAt)) }),
  ...(stopReason === null ? {} : { finish_reason: stopReason }),
  started_at: Math.round(timing.startedAt),
})

const isRejection = (status: number): boolean =>
  status >= 400 && status < 500 && status !== 408 && status !== 429

// Posts the oldest requests that share a session (one report names one) and
// says whether Manticore is taking reports: false leaves them queued.
const sendOldest = async ($: EngineInterface, to: Target): Promise<boolean> => {
  const sessionId = queue[0]?.sessionId
  const end = queue.findIndex(pending => pending.sessionId !== sessionId)
  const batch = queue.slice(0, Math.min(end === -1 ? queue.length : end, BATCH_LIMIT))

  try {
    const response = await $.http.fetch(`${to.url}/v1/usage`, {
      method: 'POST',
      headers: {
        'content-type': 'application/json',
        authorization: `Bearer ${to.key}`,
        ...(sessionId === undefined ? {} : { 'x-session-id': sessionId }),
      },
      body: JSON.stringify({ records: batch.map(pending => pending.record) }),
    })

    if (!response.ok) {
      $.ui.log(`legato-manticore: ${batch.length} requests not reported (${response.status})`, { to: 'debug' })

      // A report Manticore turned down (invalid, a key it does not know)
      // gets the same answer however often it is sent: it is let go so it
      // does not hold up what follows. Anything else is worth another try.
      if (!isRejection(response.status)) {
        return false
      }
    }
  } catch (error) {
    $.ui.log(`legato-manticore: Manticore is unreachable: ${String(error)}`, { to: 'debug' })

    return false
  }

  queue = queue.filter(pending => !batch.includes(pending))

  return true
}

// Reports what is queued, in order, for as long as Manticore takes it. No
// hook waits on this: a request is never held up by its own accounting.
const drain = ($: EngineInterface, to: Target): Promise<void> => {
  sending ??= (async () => {
    try {
      while (queue.length > 0 && (await sendOldest($, to))) {
        // The next report, if more was queued meanwhile.
      }
    } finally {
      sending = undefined
    }
  })()

  return sending
}

export const register: Register = on => {
  // One model request of a turn, a subagent's included.
  on('turn.step', async function* ($, e, next) {
    const to = await targetOf($)

    if (to === undefined) {
      return yield* next(e)
    }

    const startedAt = await $.clock.now()
    const stream = next(e)
    let firstTokenAt: number | undefined

    for await (const chunk of stream) {
      firstTokenAt ??= await $.clock.now()

      yield chunk
    }

    const response = await stream.result

    if (response.usage !== null) {
      const claudeSessionId = await $.session.id()

      queue.push({
        sessionId: to.taskId ?? claudeSessionId,
        record: recordOf(`${claudeSessionId}:${e.turnId}:${e.index}`, response.usage, response.stopReason, {
          startedAt,
          firstTokenAt,
          endedAt: await $.clock.now(),
        }),
      })
      queue.splice(0, Math.max(0, queue.length - QUEUE_LIMIT))
      void drain($, to)
    }

    return response
  })

  // The last chance for what is still queued: a run that ends right after
  // its last request (`claude -p`) would otherwise leave before the report.
  on('session.end', async ($, e, next) => {
    const to = await targetOf($)

    if (to !== undefined) {
      // A report already on its way first, then one more try for the rest.
      await sending
      await drain($, to)
    }

    return next(e)
  })
}
