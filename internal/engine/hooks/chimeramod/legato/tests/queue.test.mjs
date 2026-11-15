import assert from 'node:assert/strict';

export async function test(h) {
  h.$.env.set('LEGATO_TASK_ID', 'fixture-task');
  h.$.env.set('LEGATO_BIN', '/fixture/legato');
  let release;
  const blocked = new Promise(resolve => { release = resolve; });
  h.setResult('process[0].write', blocked);
  await h.emit({ type: 'session.start', sessionId: 'main', name: 'fixture-task', reason: 'new' });
  for (let i = 0; i < 1100; i++) {
    await h.emit({ type: 'message.append', sessionId: 'main', messageId: i, text: 'observer' });
  }
  await h.emit({ type: 'turn.start', sessionId: 'main', turnId: 't', source: 'user' });
  await h.emit({ type: 'turn.complete', sessionId: 'main', turnId: 't', reason: 'stop' });
  const ending = h.emit({ type: 'session.end', sessionId: 'main', reason: 'exit' });
  await new Promise(resolve => setTimeout(resolve, 0));
  assert.equal(h.calls.filter(c => c.path === 'process[0].write').length, 1);
  h.setResult('process[0].write', undefined);
  release();
  await ending;
  const records = h.calls.filter(c => c.path === 'process[0].write').map(c => JSON.parse(c.args[0]));
  assert.ok(records.length <= 1000);
  assert.deepEqual(records.filter(r => r.type !== 'message.append').map(r => r.type), [
    'session.start', 'turn.start', 'turn.complete', 'session.end',
  ]);
  const messages = records.filter(r => r.type === 'message.append');
  assert.ok(messages[0].messageId > 0);
  assert.equal(messages.at(-1).messageId, 1099);
  assert.equal(h.processes.length, 1);
}
