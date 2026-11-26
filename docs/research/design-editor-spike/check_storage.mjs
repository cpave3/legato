import test from 'node:test';
import assert from 'node:assert/strict';
import { mkdtemp, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { once } from 'node:events';
import { createHost } from './serve.mjs';
import { Editor, fixture } from './.build/core.js';

test('HTTP CAS save survives host restart with editable state, pins and undo/redo history', async () => {
  const folder = await mkdtemp(join(tmpdir(), 'PROTOTYPE-editor-test-'));
  let host;
  async function start() {
    host = await createHost(join(folder, 'PROTOTYPE-state.json'));
    host.listen(0, '127.0.0.1'); await once(host, 'listening');
    return `http://127.0.0.1:${host.address().port}`;
  }
  async function stop() { await new Promise((resolve, reject) => host.close(error => error ? reject(error) : resolve())); }
  try {
    let base = await start();
    const editor = new Editor(fixture());
    editor.pin('cta', 'Keep the action clear', 'r0');
    editor.apply({baseRevision:'r0', actor:'human-ui', label:'Typed title', ops:[{type:'set', id:'title', field:'text', value:'Launch workspace'}]});
    const session = editor.serialize();
    const save = () => fetch(base + '/state', {method:'POST', headers:{'Content-Type':'application/json'}, body:JSON.stringify({expectedVersion:0, session})});
    const results = await Promise.all([save(), save()]);
    assert.deepEqual(results.map(r => r.status).sort(), [200,409]);
    await stop();
    base = await start();
    const stored = await (await fetch(base + '/state')).json();
    assert.equal(stored.version, 1);
    assert.equal(stored.session, session);
    const loaded = Editor.deserialize(stored.session);
    assert.equal(loaded.snapshot().document.nodes.title.text, 'Launch workspace');
    assert.equal(loaded.feedback()[0].node, 'cta');
    loaded.undo();
    assert.equal(loaded.snapshot().document.nodes.title.text, 'Your next workspace');
    loaded.redo();
    assert.equal(loaded.snapshot().document.nodes.title.text, 'Launch workspace');
  } finally { if(host?.listening) await stop(); await rm(folder, {recursive:true}); }
});
