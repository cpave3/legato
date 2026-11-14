import assert from 'node:assert/strict';

export async function test(h) {
  h.$.env.set('LEGATO_TASK_ID', 'fixture-task');
  h.$.env.set('LEGATO_BIN', '/fixture/legato');
  const failed = Promise.reject(new Error('temporary pipe failure'));
  failed.catch(() => {});
  h.setResult('process[0].write', failed);
  await h.emit({ type: 'session.start', sessionId: 'main', name: 'fixture-task', reason: 'new' });
  await new Promise(resolve => setTimeout(resolve, 0));
  h.setResult('process[0].write', undefined);
  await h.emit({ type: 'session.end', sessionId: 'main', reason: 'exit' });
  const records = h.calls.filter(c => c.path === 'process[0].write').map(c => JSON.parse(c.args[0]));
  assert.deepEqual(records.map(r => r.seq), [1, 1, 2]);
  assert.deepEqual(records[0], records[1]);
  assert.equal(h.processes.length, 1);
}
