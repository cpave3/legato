import assert from 'node:assert/strict';

export async function test(h) {
  h.$.env.set('LEGATO_TASK_ID', 'parent-task');
  h.$.env.set('LEGATO_BIN', '/fixture/legato');
  h.setResult('session.isSubagent', true);
  await h.emit({ type: 'session.start', sessionId: 'child', reason: 'new', name: 'child' });
  await h.emit({ type: 'turn.start', sessionId: 'child', turnId: 't', source: 'user' });
  await h.emit({ type: 'session.end', sessionId: 'child', reason: 'exit' });
  assert.deepEqual(h.processes, []);
}
