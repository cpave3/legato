import test from 'node:test';
import assert from 'node:assert/strict';
import { Editor, fixture, worldPosition } from './.build/core.js';

const human = (editor, ops) => editor.apply({baseRevision: editor.snapshot().revision, actor: 'human-ui', label: 'Direct edit', ops});
const wrapper = () => ({id: 'details-group', kind: 'group', parent: 'board', children: [], text: '', style: {x: 16, y: 16, width: 720, height: 380, color: '#17212b', background: 'transparent', fontSize: 16}});
const structure = baseRevision => ({baseRevision, actor: 'agent-fixture', label: 'Wrap details (authored patch)', ops: [
  {type: 'insert', node: wrapper()},
  {type: 'reparent', id: 'body', parent: 'details-group'},
  {type: 'reparent', id: 'cta', parent: 'details-group'},
]});

test('direct edits survive an explicitly rebased structural patch with identity and world layout intact', () => {
  const editor = new Editor(fixture());
  const initial = editor.snapshot();
  const proposal = structure(initial.revision);
  human(editor, [
    {type: 'set', id: 'title', field: 'text', value: 'Workspace for the launch team'},
    {type: 'set', id: 'cta', field: 'background', value: '#245e9b'},
    {type: 'set', id: 'cta', field: 'x', value: 130},
    {type: 'set', id: 'cta', field: 'y', value: 315},
  ]);
  const edited = editor.snapshot();
  assert.throws(() => editor.apply(proposal), /Stale revision/);
  assert.deepEqual(editor.snapshot(), edited);
  editor.apply(proposal, 'rebase');
  const result = editor.snapshot();
  assert.deepEqual(result.document.nodes.title, edited.document.nodes.title);
  for (const id of ['body', 'cta']) {
    assert.equal(result.document.nodes[id].id, id);
    assert.equal(result.document.nodes[id].text, edited.document.nodes[id].text);
    for (const field of ['width', 'height', 'background', 'color', 'fontSize']) {
      assert.equal(result.document.nodes[id].style[field], edited.document.nodes[id].style[field]);
    }
    assert.deepEqual(worldPosition(result.document, id), worldPosition(edited.document, id));
    assert.equal(result.document.nodes[id].parent, 'details-group');
  }
  assert.deepEqual(result.document.nodes['details-group'].children, ['body', 'cta']);
});

test('same-property conflicts reject the entire patch; no inserted node or human intent is lost', () => {
  const editor = new Editor(fixture());
  const baseRevision = editor.snapshot().revision;
  human(editor, [{type: 'set', id: 'cta', field: 'text', value: 'Open launch workspace'}]);
  const before = editor.snapshot();
  assert.throws(() => editor.apply({baseRevision, actor: 'agent-fixture', label: 'Conflicting compound patch', ops: [
    {type: 'insert', node: wrapper()},
    {type: 'set', id: 'cta', field: 'text', value: 'Start now'},
  ]}, 'rebase'), /Conflict cta.text.*Open launch workspace.*Start now/);
  assert.deepEqual(editor.snapshot(), before);
  assert.throws(() => editor.apply({baseRevision: 'not-a-revision', actor: 'agent-fixture', label: 'Unknown base', ops: [{type: 'set', id: 'cta', field: 'text', value: 'x'}]}, 'rebase'), /Unknown revision/);
  assert.deepEqual(editor.snapshot(), before);
});

test('global undo and redo survive serialization, preserving human edits and node identity', () => {
  const editor = new Editor(fixture());
  human(editor, [{type: 'set', id: 'title', field: 'text', value: 'Human title'}]);
  const humanState = editor.snapshot();
  editor.apply(structure(humanState.revision));
  const grouped = editor.snapshot();
  const restored = Editor.deserialize(editor.serialize());
  restored.undo();
  assert.deepEqual(restored.snapshot().document, humanState.document);
  assert.notEqual(restored.snapshot().revision, humanState.revision);
  restored.redo();
  assert.deepEqual(restored.snapshot().document, grouped.document);
  restored.undo();
  human(restored, [{type: 'set', id: 'title', field: 'color', value: '#123456'}]);
  assert.throws(() => restored.redo(), /Nothing to redo/);
  assert.equal(restored.snapshot().document.nodes.title.text, 'Human title');
});

test('pins retain original revision/identity; deletion orphans them and IDs cannot be recycled', () => {
  let editor = new Editor(fixture());
  editor.pin('cta', 'Make this action clearer', 'r0');
  human(editor, [{type: 'set', id: 'cta', field: 'text', value: 'Open workspace'}]);
  editor.apply(structure('r0'), 'rebase');
  let [pin] = editor.feedback();
  assert.equal(pin.revision, 'r0');
  assert.equal(pin.document, 'workspace-study');
  assert.equal(pin.frame, 'board');
  assert.equal(pin.node, 'cta');
  assert.equal(pin.quote, 'Continue');
  assert.equal(pin.status, 'historical-target-present');
  assert.throws(() => editor.pin('cta', 'Stale comment', 'r0'), /Stale pin/);
  const action = editor.snapshot().document.nodes.cta;
  human(editor, [{type: 'remove', id: 'cta'}]);
  editor = Editor.deserialize(editor.serialize());
  assert.equal(editor.feedback()[0].status, 'orphaned');
  assert.throws(() => human(editor, [{type: 'insert', node: action}]), /fresh ID/);
  editor.undo();
  assert.equal(editor.feedback()[0].status, 'historical-target-present');
  assert.deepEqual(editor.snapshot().document.nodes.cta, action);
});

test('deleting a concurrently edited subtree conflicts instead of erasing the human edit', () => {
  const editor = new Editor(fixture());
  editor.apply(structure('r0'));
  const baseRevision = editor.snapshot().revision;
  human(editor, [{type: 'set', id: 'cta', field: 'background', value: '#245e9b'}]);
  const before = editor.snapshot();
  assert.throws(() => editor.apply({baseRevision, actor: 'agent-fixture', label: 'Remove details', ops: [{type:'remove', id:'details-group'}]}, 'rebase'), /Conflict.*subtree/);
  assert.deepEqual(editor.snapshot(), before);
});
