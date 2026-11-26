import { Editor, fixture, worldPosition, type Document as SceneDocument, type SceneNode, type Patch, type Operation } from './core.js';

const escapeHTML = (text: string) => text.replace(/[&<>"']/g, ch => ({'&':'&amp;', '<':'&lt;', '>':'&gt;', '"':'&quot;', "'":'&#39;'}[ch]!));

// Rendering is a projection of owned scene data, NOT source-code/DOM surgery.
export function previewDocument(document: SceneDocument, token: string, revision: string): string {
  function render(id: string): string {
    const n = document.nodes[id], s = n.style;
    const root = id === document.root;
    const container = n.kind === 'frame' || n.kind === 'group';
    const style = `left:${s.x}px;top:${s.y}px;width:${s.width}px;height:${s.height}px;color:${s.color};background:${s.background};font-size:${s.fontSize}px;`;
    return `<div data-node="${id}" class="node ${root ? 'root' : ''} ${n.kind}" style="${style}">${root ? '' : `<button data-move="${id}" aria-label="Move ${id}" class="move">Move ${id}</button>`}${container ? n.children.map(render).join('') : `<div data-edit="${id}" contenteditable="true" spellcheck="false" role="textbox" aria-label="Edit ${id}">${escapeHTML(n.text)}</div>`}</div>`;
  }
  const bridge = `
    let pinMode = false, drag = null;
    const send = data => parent.postMessage({...data, token:${JSON.stringify(token)}, revision:${JSON.stringify(revision)}}, '*');
    addEventListener('message', e => {
      if(e.source !== parent || e.data.kind !== 'mode')return;
      pinMode=e.data.enabled;
      document.querySelectorAll('[data-edit]').forEach(el=>el.contentEditable=String(!pinMode));
      document.querySelectorAll('[data-move]').forEach(el=>el.disabled=pinMode);
      send({kind:'mode-ready',enabled:pinMode});
    });
    addEventListener('click', e => {
      if(!pinMode) return;
      e.preventDefault();e.stopImmediatePropagation();
      const n=e.target.closest('[data-node]'); if(n) send({kind:'pin',id:n.dataset.node});
    },true);
    addEventListener('focusout', e => {
      if(e.target.matches('[data-edit]') && !pinMode) send({kind:'text',id:e.target.dataset.edit,value:e.target.textContent});
    });
    addEventListener('pointerdown',e=>{
      const handle=e.target.closest('[data-move]'); if(!handle || pinMode)return;
      e.preventDefault();handle.setPointerCapture(e.pointerId);
      const n=handle.closest('[data-node]');
      drag={id:handle.dataset.move,n,x:e.clientX,y:e.clientY,left:parseFloat(n.style.left),top:parseFloat(n.style.top)};
    });
    addEventListener('pointermove',e=>{if(drag){drag.n.style.left=(drag.left+e.clientX-drag.x)+'px';drag.n.style.top=(drag.top+e.clientY-drag.y)+'px';}});
    addEventListener('pointerup',e=>{if(drag){const d=drag;drag=null;send({kind:'move',id:d.id,x:d.left+e.clientX-d.x,y:d.top+e.clientY-d.y});}});
    addEventListener('pointercancel',()=>{if(drag){drag.n.style.left=drag.left+'px';drag.n.style.top=drag.top+'px';drag=null;}});
    addEventListener('DOMContentLoaded',()=>send({kind:'ready'}));
  `;
  const policy = "default-src 'none'; script-src 'unsafe-inline'; style-src 'unsafe-inline'; connect-src 'none'; img-src data:; frame-src 'none'; form-action 'none'; base-uri 'none'; object-src 'none'";
  return `<!doctype html><html><head><meta http-equiv="Content-Security-Policy" content="${policy}"><meta name="referrer" content="no-referrer"><style>body{margin:0;font:16px/1.5 system-ui;color:#17212b}.node{position:absolute;box-sizing:border-box}.root{position:relative}.group{outline:1px dashed #75897e}.move{position:absolute;right:0;top:-18px;z-index:2;font:10px system-ui;background:white;color:#263b46;border:1px solid #8d9ba8;border-radius:3px;padding:1px 5px;cursor:move;touch-action:none}.button [data-edit]{display:flex;align-items:center;justify-content:center;height:100%;border-radius:5px}.text [data-edit]{white-space:pre-wrap;min-height:100%}[contenteditable]:focus{outline:2px solid #19745b;outline-offset:3px}::selection{background:#c1e2d5;color:#142d23}</style><script>${bridge}<\/script></head><body>${render(document.root)}</body></html>`;
}

export async function mountEditor(root: HTMLElement): Promise<void> {
  const get = <T extends HTMLElement>(id: string) => root.querySelector<T>('#' + id)!;
  const frame = get<HTMLIFrameElement>('preview');
  const selected = get<HTMLSelectElement>('selected');
  const proposal = get<HTMLTextAreaElement>('proposal');
  const scenario = get<HTMLSelectElement>('scenario');
  const mode = get<HTMLInputElement>('pin-mode');
  const status = get('status');
  let editor = new Editor(fixture()), storageVersion = 0, selectedId = 'cta';
  let token = '', baseRevision = 'r0';
  let anchor: {id: string; revision: string} | null = null;
  function message(text: string, error = false) {status.textContent = text; status.dataset.error = String(error);}
  function showState() {
    get('state').textContent = JSON.stringify({snapshot:editor.snapshot(), feedback:editor.feedback(), session:JSON.parse(editor.serialize())},null,2);
  }
  function sendMode() {
    frame.style.visibility='hidden';
    frame.contentWindow!.postMessage({kind:'mode',enabled:mode.checked},'*');
  }
  function render() {
    const snapshot = editor.snapshot(), document = snapshot.document;
    get('revision').textContent = `${document.id} / ${document.root} / ${snapshot.revision}`;
    selected.replaceChildren();
    for (const n of Object.values(document.nodes)) {
      const option = window.document.createElement('option');option.value=n.id;option.textContent=`${n.id} · ${n.kind}`;selected.append(option);
    }
    if(!Object.hasOwn(document.nodes,selectedId)) selectedId='title';
    selected.value=selectedId;
    const n=document.nodes[selectedId], world=worldPosition(document,selectedId);
    get('position').textContent=`Parent: ${n.parent ?? 'none'} · Canvas position: ${world.x}, ${world.y}`;
    get<HTMLInputElement>('fill').value=n.style.background;
    get<HTMLInputElement>('x').value=String(n.style.x);
    get<HTMLInputElement>('y').value=String(n.style.y);
    token=crypto.randomUUID();
    frame.style.visibility='hidden';
    frame.srcdoc=previewDocument(document,token,snapshot.revision);
    anchor=null;get<HTMLButtonElement>('add-pin').disabled=true;
    get('anchor').textContent='Turn on Pin mode, then select a design element.';
    const list=get('feedback');list.replaceChildren();
    for(const pin of editor.feedback()) {
      const entry=window.document.createElement('article');
      for(const text of [pin.comment,`${pin.node} / ${pin.frame} · ${pin.status === 'orphaned' ? 'Orphaned' : pin.status === 'current-revision' ? 'This revision' : 'Historical '+pin.revision+' — target present at head'}`,`Original ${pin.revision}: “${pin.quote}”`]) {
        const p=window.document.createElement('p');p.textContent=text;entry.append(p);
      }
      list.append(entry);
    }
    showState();
  }
  function applyHuman(ops: Operation[], revision = editor.snapshot().revision) {
    editor.apply({baseRevision:revision,actor:'human-ui',label:'Direct text/property/drag edit',ops});
  }
  function act(work: () => void, success: string) {
    try {work();render();message(success);}
    catch(error) {showState();message(String(error),true);}
  }
  window.addEventListener('message', event => {
    if(event.source !== frame.contentWindow || event.data?.token !== token) return;
    const data=event.data;
    if(data.kind==='ready') {sendMode();return;}
    if(data.kind==='mode-ready') {if(data.enabled===mode.checked)frame.style.visibility='visible';return;}
    if(typeof data.id !== 'string') return;
    if(data.kind==='pin' && mode.checked) {
      anchor={id:data.id,revision:data.revision};get('anchor').textContent=`Selected ${data.id} at ${data.revision}`;
      get<HTMLButtonElement>('add-pin').disabled=false;
    } else if(data.kind==='text') {
      act(()=>applyHuman([{type:'set',id:data.id,field:'text',value:data.value}],data.revision),'Inline text edit applied');
    } else if(data.kind==='move') {
      selectedId=data.id;
      act(()=>applyHuman([{type:'set',id:data.id,field:'x',value:data.x},{type:'set',id:data.id,field:'y',value:data.y}],data.revision),'Pointer drag applied');
    }
  });
  selected.onchange=()=>{selectedId=selected.value;render();};
  for(const id of ['fill','x','y'] as const) {
    get<HTMLInputElement>(id).onchange=()=>act(()=>applyHuman([{type:'set',id:selectedId,field:id==='fill'?'background':id,value:id==='fill'?get<HTMLInputElement>(id).value:Number(get<HTMLInputElement>(id).value)}]),'Property edit applied');
  }
  mode.onchange=sendMode;
  get('undo').onclick=()=>act(()=>editor.undo(),'Undid latest transaction (global history)');
  get('redo').onclick=()=>act(()=>editor.redo(),'Redid next transaction (global history)');
  get('add-pin').onclick=()=>act(()=>{editor.pin(anchor!.id,get<HTMLTextAreaElement>('comment').value,anchor!.revision);get<HTMLTextAreaElement>('comment').value='';},'Pin saved in session; Save session writes it to disk');
  function draft() {
    const group: SceneNode={id:'details-group',kind:'group',parent:'board',children:[],text:'',style:{x:16,y:16,width:720,height:380,color:'#17212b',background:'transparent',fontSize:16}};
    const ops: Operation[]=scenario.value==='rename'?[{type:'set',id:'cta',field:'text',value:'Start now'}]:scenario.value==='remove'?[{type:'remove',id:'cta'}]:[{type:'insert',node:group},{type:'reparent',id:'body',parent:'details-group'},{type:'reparent',id:'cta',parent:'details-group'}];
    proposal.value=JSON.stringify({baseRevision,actor:'agent-fixture',label:`Authored ${scenario.value} patch`,ops},null,2);
  }
  get('capture').onclick=()=>{baseRevision=editor.snapshot().revision;draft();message(`Captured ${baseRevision}; this authored patch will not track later edits`);};
  scenario.onchange=draft;
  get('apply').onclick=()=>act(()=>editor.apply(JSON.parse(proposal.value) as Patch),'Authored patch applied (no model call)');
  get('rebase').onclick=()=>act(()=>editor.apply(JSON.parse(proposal.value) as Patch,'rebase'),'Explicit semantic rebase applied (no model call)');
  async function load() {
    const response=await fetch('/state');const saved=await response.json();
    if(!response.ok)throw new Error(saved.error);
    storageVersion=saved.version;
    if(saved.session)editor=Editor.deserialize(saved.session);
    render();message(saved.session?`Loaded saved version ${storageVersion}`:'New unsaved session');
  }
  get('load').onclick=()=>load().catch(error=>message(String(error),true));
  get('save').onclick=async()=>{
    try {
      const response=await fetch('/state',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({expectedVersion:storageVersion,session:editor.serialize()})});
      const result=await response.json();if(!response.ok)throw new Error(result.error);
      storageVersion=result.version;message(`Saved version ${storageVersion}`);
    } catch(error) {message(String(error),true);}
  };
  draft();await load();
}
mountEditor(document.querySelector<HTMLElement>('#app')!).catch(error=>{
  const status=document.querySelector<HTMLElement>('#status')!;status.textContent=String(error);status.dataset.error='true';
});
