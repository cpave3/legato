import assert from 'node:assert/strict';

export async function test(h) {
  h.$.env.set('LEGATO_TASK_ID', '');
  await h.emit({ type: 'session.start', sessionId: 'outside', reason: 'new', name: 'outside' });
  await h.emit({ type: 'turn.start', sessionId: 'outside', turnId: 't', source: 'user' });
  await h.emit({ type: 'session.end', sessionId: 'outside', reason: 'exit' });
  assert.deepEqual(h.processes, []);
}
