// THROWAWAY LEG-665: constrained scene graph, not an arbitrary HTML editor.
export type Style = {x: number; y: number; width: number; height: number; color: string; background: string; fontSize: number};
export type SceneNode = {id: string; kind: 'frame' | 'group' | 'text' | 'button'; parent: string | null; children: string[]; text: string; style: Style};
export type Document = {id: string; root: string; nodes: Record<string, SceneNode>};
export type Field = 'text' | keyof Style;
export type Operation = {type: 'set'; id: string; field: Field; value: string | number}
  | {type: 'insert'; node: SceneNode}
  | {type: 'reparent'; id: string; parent: string}
  | {type: 'remove'; id: string};
export type Patch = {baseRevision: string; actor: 'human-ui' | 'agent-fixture'; label: string; ops: Operation[]};
type Transaction = {before: Document; after: Document; patch: Patch};
type Pin = {id: string; document: string; revision: string; frame: string; node: string; quote: string; comment: string};
export class EditError extends Error {
  constructor(public code: string, message: string) { super(message); }
}
const clone = <T>(value: T): T => structuredClone(value);
const equal = (a: unknown, b: unknown) => JSON.stringify(a) === JSON.stringify(b);
function fail(code: string, message: string): never { throw new EditError(code, message); }
const numeric = ['x', 'y', 'width', 'height', 'fontSize'];
const fields = ['text', ...numeric, 'color', 'background'];
function node(document: Document, id: string): SceneNode {
  if (!Object.hasOwn(document.nodes, id)) fail('invalid', `Unknown node ${id}`);
  return document.nodes[id];
}
function value(n: SceneNode, field: Field): string | number { return field === 'text' ? n.text : n.style[field]; }
function validValue(field: string, input: unknown): void {
  if (field === 'text') {
    if (typeof input !== 'string' || input.length > 10000) fail('invalid', 'Text must be at most 10000 characters');
  } else if (numeric.includes(field)) {
    if (typeof input !== 'number' || !Number.isFinite(input) || Math.abs(input) > 10000 || (['width', 'height', 'fontSize'].includes(field) && input <= 0)) fail('invalid', `Invalid ${field}`);
  } else if (field === 'color' || field === 'background') {
    if (typeof input !== 'string' || (!/^#[0-9a-f]{6}$/i.test(input) && !(field === 'background' && input === 'transparent'))) fail('invalid', `Invalid ${field}: use #rrggbb`);
  } else fail('invalid', `Unsupported field ${field}`);
}
export function validateDocument(document: Document): void {
  if (!document || typeof document.id !== 'string' || !document.nodes || !document.root) fail('invalid', 'Invalid document');
  const visited = new Set<string>();
  function visit(id: string, parent: string | null): void {
    const n = node(document, id);
    if (!/^[a-z][a-z0-9-]{0,79}$/.test(id) || n.id !== id || n.parent !== parent || visited.has(id)) fail('invalid', `Invalid topology at ${id}`);
    if (!['frame', 'group', 'text', 'button'].includes(n.kind) || !Array.isArray(n.children) || !n.style) fail('invalid', `Invalid node ${id}`);
    if (n.children.length && n.kind !== 'frame' && n.kind !== 'group') fail('invalid', 'Only containers hold children');
    for (const key of fields) validValue(key, value(n, key as Field));
    visited.add(id);
    n.children.forEach(child => visit(child, id));
  }
  visit(document.root, null);
  if (node(document, document.root).kind !== 'frame' || visited.size !== Object.keys(document.nodes).length) fail('invalid', 'Unreachable nodes or invalid frame');
}
export function worldPosition(document: Document, id: string): {x: number; y: number} {
  const n = node(document, id);
  const parent = n.parent ? worldPosition(document, n.parent) : {x: 0, y: 0};
  return {x: n.style.x + parent.x, y: n.style.y + parent.y};
}
function conflict(path: string, base: unknown, current: unknown, proposed: unknown): void {
  if (!equal(base, current)) fail('conflict', `Conflict ${path}: base=${JSON.stringify(base)}, current=${JSON.stringify(current)}, proposed=${JSON.stringify(proposed)}`);
}
function move(document: Document, id: string, destination: string): void {
  const n = node(document, id), parent = node(document, destination);
  if (!n.parent || !['group', 'frame'].includes(parent.kind)) fail('invalid', 'Cannot move root or move into a leaf');
  for (let ancestor: SceneNode | undefined = parent; ancestor; ancestor = ancestor.parent ? node(document, ancestor.parent) : undefined) {
    if (ancestor.id === id) fail('invalid', 'Reparent would create a cycle');
  }
  const world = worldPosition(document, id), origin = worldPosition(document, destination);
  const oldParent = node(document, n.parent);
  oldParent.children = oldParent.children.filter(child => child !== id);
  parent.children.push(id); n.parent = destination;
  n.style.x = world.x - origin.x; n.style.y = world.y - origin.y;
}
function subtree(document: Document, id: string): SceneNode[] {
  const n = node(document, id);
  return [n, ...n.children.flatMap(child => subtree(document, child))];
}
function remove(document: Document, id: string): void {
  const n = node(document, id);
  if (!n.parent) fail('invalid', 'Cannot remove root');
  const parent = node(document, n.parent);
  parent.children = parent.children.filter(child => child !== id);
  for (const child of subtree(document, id)) delete document.nodes[child.id];
}

export class Editor {
  private revision = 'r0';
  private serial = 0;
  private snapshots: Record<string, Document>;
  private audit: unknown[] = [];
  private history: Transaction[] = [];
  private cursor = 0;
  private pins: Pin[] = [];
  constructor(document: Document) {
    validateDocument(document);
    this.snapshots = {r0: clone(document)};
  }
  snapshot(revision = this.revision): {revision: string; document: Document} {
    if (!Object.hasOwn(this.snapshots, revision)) fail('invalid', `Unknown revision ${revision}`);
    return {revision, document: clone(this.snapshots[revision])};
  }
  private record(document: Document): string {
    this.revision = `r${++this.serial}`;
    this.snapshots[this.revision] = clone(document);
    return this.revision;
  }
  serialize(): string {
    return JSON.stringify({schema: 1, revision: this.revision, serial: this.serial, snapshots: this.snapshots, history: this.history, cursor: this.cursor, pins: this.pins, audit: this.audit});
  }
  static deserialize(text: string): Editor {
    const data = JSON.parse(text);
    if (data.schema !== 1 || !Number.isInteger(data.serial) || data.serial < 0 || !data.snapshots || !Object.hasOwn(data.snapshots, data.revision) || !Array.isArray(data.history) || !Array.isArray(data.pins) || !Array.isArray(data.audit) || !Number.isInteger(data.cursor) || data.cursor < 0 || data.cursor > data.history.length) fail('invalid', 'Invalid saved session');
    const current = data.snapshots[data.revision] as Document;
    const editor = new Editor(current);
    for (const [revision, document] of Object.entries(data.snapshots)) {
      if (!/^r\d+$/.test(revision) || Number(revision.slice(1)) > data.serial) fail('invalid', 'Invalid saved revision');
      validateDocument(document as Document);
      if ((document as Document).id !== current.id) fail('invalid', 'Mixed document identities');
    }
    for (const entry of data.history as Transaction[]) {
      validateDocument(entry.before); validateDocument(entry.after);
      if (entry.before.id !== current.id || entry.after.id !== current.id) fail('invalid', 'Mixed history identities');
    }
    if (data.history.length) {
      const expected = data.cursor ? data.history[data.cursor - 1].after : data.history[0].before;
      if (!equal(expected, current)) fail('invalid', 'History cursor does not match saved document');
    }
    editor.revision = data.revision; editor.serial = data.serial;
    editor.snapshots = data.snapshots; editor.history = data.history; editor.cursor = data.cursor; editor.audit = data.audit;
    for (const pin of data.pins as Pin[]) {
      const original = editor.snapshot(pin.revision).document;
      if (pin.document !== current.id || pin.frame !== original.root || typeof pin.comment !== 'string' || typeof pin.quote !== 'string') fail('invalid', 'Invalid saved pin');
      node(original, pin.node);
    }
    editor.pins = data.pins;
    return editor;
  }
  pin(id: string, comment: string, revision: string): void {
    if (revision !== this.revision) fail('stale', 'Stale pin: reload head before commenting');
    if (typeof comment !== 'string' || !comment.trim() || comment.length > 4000) fail('invalid', 'Comment needs 1–4000 characters');
    const document = this.snapshot().document;
    this.pins.push({id: `pin-${this.pins.length + 1}`, document: document.id, revision, frame: document.root, node: id, quote: node(document, id).text, comment});
  }
  feedback(): (Pin & {status: string})[] {
    const document = this.snapshot().document;
    return this.pins.map(pin => ({...clone(pin), status: !Object.hasOwn(document.nodes, pin.node) ? 'orphaned' : pin.revision === this.revision ? 'current-revision' : 'historical-target-present'}));
  }
  undo(): void {
    if (!this.cursor) fail('empty', 'Nothing to undo');
    const entry = this.history[--this.cursor];
    this.record(entry.before);
    this.audit.push({outcome: 'undo', revision: this.revision, label: entry.patch.label, actor: entry.patch.actor});
  }
  redo(): void {
    if (this.cursor === this.history.length) fail('empty', 'Nothing to redo');
    const entry = this.history[this.cursor++];
    this.record(entry.after);
    this.audit.push({outcome: 'redo', revision: this.revision, label: entry.patch.label, actor: entry.patch.actor});
  }
  apply(patch: Patch, policy: 'reject' | 'rebase' = 'reject'): string {
    try {
      if (!patch || !['human-ui', 'agent-fixture'].includes(patch.actor) || typeof patch.label !== 'string' || !Array.isArray(patch.ops) || !patch.ops.length) fail('invalid', 'Invalid patch');
      const base = this.snapshot(patch.baseRevision).document;
      const head = this.snapshot().document;
      if (patch.baseRevision !== this.revision && policy !== 'rebase') fail('stale', `Stale revision ${patch.baseRevision}; head is ${this.revision}. Explicit rebase required.`);
      const used = new Set(Object.values(this.snapshots).flatMap(d => Object.keys(d.nodes)));
      for (const op of patch.ops) {
        if (op.type === 'set') {
          validValue(op.field, op.value);
          const old = node(base, op.id), current = node(head, op.id);
          conflict(`${op.id}.${op.field}`, value(old, op.field), value(current, op.field), op.value);
          for (const target of [old, current]) {
            if (op.field === 'text') target.text = op.value as string;
            else (target.style as unknown as Record<string, string | number>)[op.field] = op.value;
          }
        } else if (op.type === 'insert') {
          if (!op.node || used.has(op.node.id) || Object.hasOwn(head.nodes, op.node.id) || !op.node.parent || op.node.children.length) fail('invalid', 'Insert needs a fresh ID, container parent and no children');
          const oldParent = node(base, op.node.parent), currentParent = node(head, op.node.parent);
          conflict(`${op.node.parent}.children`, oldParent.children, currentParent.children, op.node.id);
          for (const d of [base, head]) {
            d.nodes[op.node.id] = clone(op.node);
            node(d, op.node.parent).children.push(op.node.id);
          }
        } else if (op.type === 'reparent') {
          const old = node(base, op.id), current = node(head, op.id);
          conflict(`${op.id}.parent`, old.parent, current.parent, op.parent);
          if (!old.parent || !current.parent) fail('invalid', 'Cannot move root');
          for (const parent of [old.parent, op.parent]) conflict(`${parent}.children`, node(base, parent).children, node(head, parent).children, op.id);
          // Semantic reparent preserves the LIVE world position, including human drags.
          move(base, op.id, op.parent); move(head, op.id, op.parent);
        } else if (op.type === 'remove') {
          const old = node(base, op.id), current = node(head, op.id);
          conflict(`${op.id}.subtree`, subtree(base, op.id), subtree(head, op.id), 'remove');
          if (!old.parent || !current.parent) fail('invalid', 'Cannot remove root');
          conflict(`${old.parent}.children`, node(base, old.parent).children, node(head, current.parent).children, 'remove');
          remove(base, op.id); remove(head, op.id);
        } else fail('invalid', 'Unsupported operation');
      }
      validateDocument(head);
      const previous = this.revision;
      this.history.splice(this.cursor);
      this.history.push({before: this.snapshot().document, after: clone(head), patch: clone(patch)});
      this.cursor = this.history.length;
      this.record(head);
      this.audit.push({outcome: 'applied', previous, revision: this.revision, rebased: patch.baseRevision !== previous, ...clone(patch)});
      return this.revision;
    } catch (error) {
      this.audit.push({outcome: 'rejected', revision: this.revision, message: String(error), patch: clone(patch)});
      throw error;
    }
  }
}

export function fixture(): Document {
  const style = (x: number, y: number, width: number, height: number, fontSize = 16): Style => ({x, y, width, height, fontSize, color: '#17212b', background: 'transparent'});
  return {id: 'workspace-study', root: 'board', nodes: {
    board: {id: 'board', kind: 'frame', parent: null, children: ['title', 'body', 'cta'], text: '', style: {...style(0, 0, 760, 420), background: '#f6f8fa'}},
    title: {id: 'title', kind: 'text', parent: 'board', children: [], text: 'Your next workspace', style: style(40, 40, 640, 70, 30)},
    body: {id: 'body', kind: 'text', parent: 'board', children: [], text: 'Keep a project and its conversations together.', style: style(40, 140, 640, 90)},
    cta: {id: 'cta', kind: 'button', parent: 'board', children: [], text: 'Continue', style: {...style(40, 280, 220, 50), color: '#ffffff', background: '#176350'}},
  }};
}
