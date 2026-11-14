const stateEvents = [
  'session.start', 'session.end', 'turn.start', 'turn.complete',
  'permission.request', 'permission.resolved', 'question.request', 'question.resolved',
  'background.start', 'background.exit', 'agent.spawn', 'agent.finish',
];
const observerEvents = new Set(['message.append']);
const stateFields = [
  'sessionId', 'reason', 'name', 'parentSessionId', 'turnId', 'source',
  'requestId', 'toolName', 'decision', 'messageId', 'jobId', 'exitCode', 'agentId', 'background',
];
const hookTimeout = 10_000;

function beforeDeadline(promise, deadline) {
  let timer;
  return Promise.race([
    promise,
    new Promise((_, reject) => {
      timer = setTimeout(() => reject(new Error('Legato link exceeded 10s deadline')), Math.max(0, deadline - Date.now()));
    }),
  ]).finally(() => clearTimeout(timer));
}

export async function register(on) {
  const queue = [];
  let seq = 0;
  let child;
  let output;
  let pump;
  let failure;
  let inFlight;
  let activeContext;

  async function drain($) {
    if (!child) {
      child = $.process.spawn({ argv: [$.env.get('LEGATO_BIN'), 'agent', 'link', 'chimera'] });
      output = (async () => {
        for await (const chunk of child.output) {
          if (chunk.stream === 'stderr') await activeContext.ui.log(chunk.text.trimEnd(), { to: 'debug' });
        }
      })().catch(error => { failure = error; });
      // Observe rejection immediately, even if the session is still open.
      child.exitPromise.catch(error => { failure = error; });
    }
    while (queue.length) {
      if (failure) throw failure;
      const record = queue[0];
      inFlight = record;
      const deadline = Date.now() + hookTimeout;
      // Retry the same seq; the link deduplicates a write accepted before a failure.
      await beforeDeadline((async () => {
        for (let attempt = 0; ; attempt++) {
          if (failure) throw failure;
          if (Date.now() >= deadline) throw new Error('Legato link exceeded 10s deadline');
          try {
            await child.write(JSON.stringify(record) + '\n');
            return;
          } catch (error) {
            if (attempt === 2 || Date.now() >= deadline) throw error;
            await new Promise(resolve => setTimeout(resolve, 25 * (attempt + 1)));
          }
        }
      })(), deadline);
      queue.shift();
      inFlight = undefined;
    }
  }

  function startPump($) {
    if (!pump && !failure) {
      pump = Promise.resolve().then(() => drain($)).catch(error => { failure = error; }).finally(() => {
        pump = undefined;
        if (queue.length && !failure) startPump($);
      });
    }
    return pump;
  }

  for (const type of [...stateEvents, ...observerEvents]) {
    on(type, async ($, event, next) => {
      if (!$.env.get('LEGATO_TASK_ID') || $.session.isSubagent) return next();
      activeContext = $;
      if (failure) throw failure;
      const deadline = Date.now() + hookTimeout;
      // Message bodies, permission args and error text can exceed the pipe's
      // line limit; the link needs only lifecycle and activity metadata.
      const record = { type, cwd: $.session.cwd, seq: ++seq };
      for (const field of stateFields) {
        if (event[field] !== undefined) record[field] = event[field];
      }
      for (const field of ['command', 'description']) {
        if (event[field] !== undefined) record[field] = event[field].slice(0, 4096);
      }
      if (type === 'session.start') {
        record.serverUrl = await beforeDeadline($.server.url(), deadline);
        record.version = $.chimera.version;
        record.apiVersion = $.api.version;
      }
      // A hard bound trades hook latency for lossless critical records. When
      // only critical/in-flight records remain, backpressure rather than grow
      // memory. A wedged link fails visibly after 10s instead of dropping state.
      while (queue.length >= 1000) {
        const oldest = queue.findIndex(item => item !== inFlight && observerEvents.has(item.type));
        if (oldest !== -1) {
          queue.splice(oldest, 1);
          break;
        }
        if (observerEvents.has(type)) return next();
        await beforeDeadline(startPump($), deadline);
        if (failure) throw failure;
      }
      queue.push(record);
      startPump($);
      if (type === 'session.end' && event.reason !== 'switch') {
        do {
          await beforeDeadline(startPump($), deadline);
          if (failure) throw failure;
        } while (queue.length);
        await beforeDeadline(Promise.resolve(child.close()), deadline);
        const result = await beforeDeadline(child.exitPromise, deadline);
        await beforeDeadline(output, deadline);
        if (failure) throw failure;
        if (result.code !== 0) throw new Error(`Legato link exited: ${result.code ?? result.signal}`);
      }
      return next();
    });
  }
}
