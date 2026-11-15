import assert from 'node:assert/strict';

// Run by `chimera mod test`: the host registers a fresh mod for this file.
export async function test(h) {
  h.$.env.set('LEGATO_TASK_ID', 'fixture-task');
  h.$.env.set('LEGATO_BIN', '/fixture/legato');
  h.setResult('server.url', 'http://127.0.0.1:7777/fixture-secret');
  const events = [
    { type: 'session.start', sessionId: 'first', reason: 'new', name: 'fixture-task' },
    { type: 'turn.start', sessionId: 'first', turnId: 'turn', source: 'user' },
    { type: 'permission.request', sessionId: 'first', requestId: 'p', toolName: 'bash', args: {} },
    { type: 'permission.resolved', sessionId: 'first', requestId: 'p', decision: 'allow' },
    { type: 'question.request', sessionId: 'first', requestId: 'q' },
    { type: 'question.resolved', sessionId: 'first', requestId: 'q' },
    { type: 'background.start', sessionId: 'first', jobId: 'shell', command: 'sleep 1' },
    { type: 'agent.spawn', sessionId: 'first', agentId: 'child', description: 'research', background: true },
    { type: 'turn.complete', sessionId: 'first', turnId: 'turn', reason: 'stop' },
    { type: 'background.exit', sessionId: 'first', jobId: 'shell', command: 'sleep 1', exitCode: 0 },
    { type: 'agent.finish', sessionId: 'first', agentId: 'child', description: 'research', background: true },
    { type: 'session.end', sessionId: 'first', reason: 'switch' },
    { type: 'session.start', sessionId: 'second', reason: 'switch', name: 'fixture-task' },
    { type: 'session.end', sessionId: 'second', reason: 'exit' },
  ];
  for (const event of events) await h.emit(event);
  assert.deepEqual(h.processes, [{ argv: ['/fixture/legato', 'agent', 'link', 'chimera'] }]);
  const records = h.calls.filter(c => c.path === 'process[0].write').map(c => JSON.parse(c.args[0]));
  assert.equal(records.length, events.length);
  assert.deepEqual(records.map(r => r.seq), events.map((_, i) => i + 1));
  assert.deepEqual(records.map(r => r.type), events.map(e => e.type));
  assert.equal(records[0].serverUrl, 'http://127.0.0.1:7777/fixture-secret');
  assert.equal(records[0].apiVersion, 1);
  assert.equal(records.at(-2).sessionId, 'second');
  assert.ok(h.calls.some(c => c.path === 'process[0].close'));
}
