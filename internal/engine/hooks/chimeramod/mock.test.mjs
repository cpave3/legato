import { test } from 'node:test';
import assert from 'node:assert/strict';
import { register } from './legato/index.mjs';

const tick = () => new Promise(resolve => setImmediate(resolve));

async function host({ task = 'task-1', subagent = false, write, spawn, output } = {}) {
  const hooks = new Map();
  const records = [], spawns = [], logs = [];
  let closed = false;
  const child = {
    async write(text) { if (write) await write(text); records.push(JSON.parse(text)); },
    close() { closed = true; },
    kill() { throw new Error('must not kill bridge'); },
    output: output ?? (async function* () {})(),
    exitPromise: Promise.resolve({ code: 0, signal: null }),
  };
  const $ = {
    env: { get: name => ({ LEGATO_TASK_ID: task, LEGATO_BIN: '/bin/legato' })[name] },
    session: { isSubagent: subagent, cwd: '/work' },
    server: { url: () => 'http://127.0.0.1:7777/secret-token' },
    chimera: { version: '1.2.3' }, api: { version: 1 },
    ui: { log: (text, options) => { logs.push({ text, ...options }); } },
    process: { spawn: options => { spawns.push(options); if (spawn) spawn(); return child; } },
  };
  await register((type, hook) => hooks.set(type, hook));
  return {
    records, spawns, logs, context: $, get closed() { return closed; },
    async emit(type, fields = {}, context = $) {
      const hook = hooks.get(type);
      assert.ok(hook, `registered ${type}`);
      let nextCalls = 0;
      const result = await hook(context, { type, sessionId: 'session-1', ...fields }, async () => { nextCalls++; return 'continued'; });
      assert.equal(nextCalls, 1);
      assert.equal(result, 'continued');
    },
  };
}

 test('start binds once, forwards ordered events, switch rebinds and final end drains before close', async () => {
  const h = await host();
  await h.emit('session.start', { reason: 'new', name: 'first' });
  await h.emit('turn.start', { turnId: 't1', source: 'user' });
  await h.emit('permission.request', { requestId: 'p1', tool: 'bash' });
  await h.emit('permission.resolved', { requestId: 'p1', decision: 'allow' });
  await h.emit('question.request', { requestId: 'q1' });
  await h.emit('question.resolved', { requestId: 'q1' });
  await h.emit('background.start', { jobId: 'j1', command: 'sleep 1' });
  await h.emit('background.exit', { jobId: 'j1', exitCode: 0 });
  await h.emit('agent.spawn', { agentId: 'a1', description: 'research', background: true });
  await h.emit('agent.finish', { agentId: 'a1', description: 'research', background: true });
  await h.emit('turn.complete', { turnId: 't1', reason: 'error', error: 'failure' });
  await h.emit('session.end', { reason: 'switch' });
  assert.equal(h.closed, false);
  await h.emit('session.start', { reason: 'switch', sessionId: 'session-2', name: 'second', parentSessionId: 'session-1' });
  await h.emit('session.end', { reason: 'exit', sessionId: 'session-2' });
  assert.deepEqual(h.spawns, [{ argv: ['/bin/legato', 'agent', 'link', 'chimera'] }]);
  assert.equal(h.closed, true);
  assert.equal(h.records.length, 14);
  assert.deepEqual(h.records.map(r => r.seq), Array.from({ length: 14 }, (_, i) => i + 1));
  assert.deepEqual(h.records[0], { type: 'session.start', sessionId: 'session-1', reason: 'new', name: 'first', serverUrl: 'http://127.0.0.1:7777/secret-token', version: '1.2.3', apiVersion: 1, cwd: '/work', seq: 1 });
  assert.equal(h.records[10].reason, 'error');
  assert.equal(h.records[12].parentSessionId, 'session-1');
});

test('outside Legato and subagents never start or forward', async () => {
  for (const options of [{ task: '' }, { subagent: true }]) {
    const h = await host(options);
    await h.emit('session.start', { reason: 'new', name: 'ignored' });
    await h.emit('turn.start', { turnId: 't1', source: 'user' });
    await h.emit('session.end', { reason: 'exit' });
    await tick();
    assert.deepEqual(h.spawns, []);
    assert.deepEqual(h.records, []);
  }
});

test('hooks only enqueue, one pump retries the failed record before later records, and end awaits drain', async () => {
  let release;
  const blocked = new Promise(resolve => { release = resolve; });
  let attempts = 0, active = 0, maximum = 0;
  const h = await host({ write: async () => {
    active++;
    maximum = Math.max(maximum, active);
    try {
      if (++attempts === 1) { await blocked; throw new Error('temporary write failure'); }
    } finally { active--; }
  } });
  await h.emit('session.start', { reason: 'new', name: 'test' });
  await h.emit('turn.start', { turnId: 't1', source: 'user' });
  assert.equal(h.records.length, 0);
  let ended = false;
  const end = h.emit('session.end', { reason: 'exit' }).then(() => { ended = true; });
  await tick();
  assert.equal(ended, false);
  assert.equal(h.closed, false);
  release();
  await end;
  assert.equal(maximum, 1);
  assert.equal(attempts, 4);
  assert.deepEqual(h.records.map(r => r.seq), [1, 2, 3]);
  assert.equal(h.closed, true);
});

test('critical-only saturation backpressures without dropping records', async () => {
  let release;
  const blocked = new Promise(resolve => { release = resolve; });
  const h = await host({ write: () => blocked });
  await h.emit('session.start', { reason: 'new', name: 'binding' });
  for (let i = 0; i < 999; i++) await h.emit('permission.request', { requestId: `p${i}` });
  let accepted = false;
  const overflow = h.emit('permission.resolved', { requestId: 'p0' }).then(() => { accepted = true; });
  await tick();
  assert.equal(accepted, false, 'critical event waits for bounded queue capacity');
  release();
  await overflow;
  await h.emit('session.end', { reason: 'exit' });
  assert.equal(h.records.length, 1002);
  assert.deepEqual(h.records.map(r => r.seq), Array.from({ length: 1002 }, (_, i) => i + 1));
});

test('observer overflow evicts oldest, not newest', async () => {
  let release;
  const blocked = new Promise(resolve => { release = resolve; });
  const h = await host({ write: () => blocked });
  await h.emit('session.start', { reason: 'resume', name: 'binding' });
  for (let i = 0; i < 1001; i++) await h.emit('message.append', { messageId: `m${i}` });
  release();
  await h.emit('session.end', { reason: 'delete' });
  const observers = h.records.filter(r => r.type === 'message.append');
  assert.equal(observers[0].messageId, 'm3');
  assert.equal(observers.at(-1).messageId, 'm1000');
  assert.equal(observers.length, 998);
});

test('child output is consumed so its pipe cannot block event delivery', async () => {
  let consumed = false;
  const h = await host({ output: (async function* () {
    yield { stream: 'stdout', text: 'ready' };
    yield { stream: 'stderr', text: 'diagnostic' };
    consumed = true;
  })() });
  await h.emit('session.start', { reason: 'new', name: 'test' });
  await h.emit('session.end', { reason: 'exit' });
  assert.equal(consumed, true);
  assert.deepEqual(h.logs, [{ text: 'diagnostic', to: 'debug' }]);
});

test('permanent write failure is surfaced at final drain without closing or losing the head', async () => {
  const h = await host({ write: () => { throw new Error('broken pipe'); } });
  await h.emit('session.start', { reason: 'new', name: 'test' });
  await assert.rejects(h.emit('session.end', { reason: 'exit' }), /broken pipe/);
  assert.equal(h.spawns.length, 1);
  assert.equal(h.closed, false);
  assert.deepEqual(h.records, []);
});

test('spawn failure is surfaced and never respawns the child', async () => {
  const h = await host({ spawn: () => { throw new Error('binary not found'); } });
  await h.emit('session.start', { reason: 'new', name: 'test' });
  await tick();
  await assert.rejects(h.emit('turn.start', { turnId: 't1', source: 'user' }), /binary not found/);
  await assert.rejects(h.emit('session.end', { reason: 'exit' }), /binary not found/);
  assert.equal(h.spawns.length, 1);
});

test('a wedged link fails within the hook deadline and cannot resume writes later', async t => {
  t.mock.timers.enable({ apis: ['setTimeout', 'Date'] });
  let release;
  const blocked = new Promise((_, reject) => { release = () => reject(new Error('late write failure')); });
  let attempts = 0;
  const h = await host({ write: () => { attempts++; return blocked; } });
  await h.emit('session.start', { reason: 'new' });
  const end = assert.rejects(h.emit('session.end', { reason: 'exit' }), /10s deadline/);
  await tick();
  t.mock.timers.tick(10_001);
  await end;
  release();
  await tick();
  t.mock.timers.tick(100);
  await tick();
  assert.equal(attempts, 1, 'timed-out writes must not retry in the background');
  await assert.rejects(h.emit('turn.start'), /10s deadline/);
  assert.equal(h.closed, false);
});

// Observer content is not needed for state and may exceed the link line limit.
test('message observers do not forward conversation payloads', async () => {
  const h = await host();
  await h.emit('session.start', { reason: 'new' });
  await h.emit('message.append', { messageId: 'm1', message: { text: 'x'.repeat(2 * 1024 * 1024) } });
  await h.emit('session.end', { reason: 'exit' });
  assert.equal(h.records[1].messageId, 'm1');
  assert.equal(h.records[1].message, undefined);
});


test('child diagnostics use the current session context after switching', async () => {
  let release;
  const gate = new Promise(resolve => { release = resolve; });
  const h = await host({ output: (async function* () { await gate; yield { stream: 'stderr', text: 'after switch' }; })() });
  await h.emit('session.start', { reason: 'new' });
  await h.emit('session.end', { reason: 'switch' });
  h.context.ui.log = () => { throw new Error('SessionEnded'); };
  const logs = [];
  const current = { ...h.context, ui: { log: (text, options) => logs.push({ text, ...options }) } };
  await h.emit('session.start', { reason: 'switch', sessionId: 'session-2' }, current);
  release();
  await h.emit('session.end', { reason: 'exit', sessionId: 'session-2' }, current);
  assert.deepEqual(logs, [{ text: 'after switch', to: 'debug' }]);
});


test('state reports keep large permission arguments and error text out of the pipe', async () => {
  const h = await host();
  await h.emit('session.start', { reason: 'new' });
  await h.emit('permission.request', { requestId: 'p', toolName: 'write', args: { content: 'x'.repeat(2 * 1024 * 1024) } });
  await h.emit('turn.complete', { reason: 'error', error: 'x'.repeat(2 * 1024 * 1024) });
  await h.emit('session.end', { reason: 'exit' });
  assert.equal(h.records[1].requestId, 'p');
  assert.equal(Object.hasOwn(h.records[1], 'args'), false);
  assert.equal(h.records[2].reason, 'error');
  assert.equal(Object.hasOwn(h.records[2], 'error'), false);
});
