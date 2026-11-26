// THROWAWAY reusable renderer: both harnesses import this exact module.
// Content is data in an opaque-origin iframe, never HTML in the host DOM.
export async function mountDesign(root, {base = ''} = {}) {
  const node = (tag, text, parent = root) => {
    const el = document.createElement(tag);
    if (text) el.textContent = text;
    parent.append(el);
    return el;
  };
  async function request(path, body) {
    const response = await fetch(base + path, body ? {method: 'POST', headers: {'Content-Type': 'application/json'}, body: JSON.stringify(body)} : {});
    const result = await response.json();
    if (!response.ok) throw new Error(result.error);
    return result;
  }
  const title = node('h1');
  const state = node('p');
  const tools = node('div'); tools.className = 'tools';
  const frameLabel = node('label', 'Frame ', tools);
  const frameSelect = node('select', '', frameLabel);
  const widthLabel = node('label', 'Viewport ', tools);
  const widthSelect = node('select', '', widthLabel);
  for (const [value, label] of [['760', 'Desktop · 760px'], ['390', 'Phone · 390px']]) {
    const option = node('option', label, widthSelect); option.value = value;
  }
  const pinLabel = node('label', '', tools);
  const pinMode = node('input', '', pinLabel); pinMode.type = 'checkbox';
  pinLabel.append(document.createTextNode('Pin mode'));
  const reload = node('button', 'Reload current', tools);
  const advance = node('button', 'Simulate agent revision', tools);
  const grid = node('div'); grid.className = 'design-grid';
  const stage = node('section', '', grid); stage.className = 'stage';
  const iframe = node('iframe', '', stage); iframe.title = 'Design preview'; iframe.setAttribute('sandbox', 'allow-scripts'); iframe.referrerPolicy = 'no-referrer';
  const feedback = node('aside', '', grid);
  node('h2', 'Feedback', feedback);
  const anchorLabel = node('p', 'Turn on Pin mode, then select the design button.', feedback);
  const form = node('form', '', feedback);
  const commentLabel = node('label', 'Comment', form);
  const comment = node('textarea', '', commentLabel); comment.required = true; comment.maxLength = 4000;
  const submit = node('button', 'Save comment', form); submit.disabled = true;
  const status = node('p', '', feedback); status.setAttribute('role', 'status');
  const list = node('div', '', feedback);
  const details = node('details'); node('summary', 'Document and feedback state', details);
  const debug = node('pre', '', details);
  let documentState, pins = [], anchor = null, renderToken;

  function sendMode() { iframe.contentWindow.postMessage({kind: 'mode', enabled: pinMode.checked}, '*'); }
  function renderFrame() {
    anchor = null; submit.disabled = true;
    renderToken = crypto.randomUUID();
    const frame = documentState.frames.find(f => f.id === frameSelect.value);
    iframe.style.width = widthSelect.value + 'px';
    iframe.style.height = frame.height + 'px';
    // The bridge is authored for this spike, not a Claude runtime or format port.
    const bridge = `<script>
      let pinMode = false;
      addEventListener('message', event => { if(event.source === parent && event.data.kind === 'mode') pinMode = event.data.enabled; });
      addEventListener('click', event => {
        if(!pinMode) return;
        event.preventDefault(); event.stopImmediatePropagation();
        const target = event.target.closest('[data-design-id]');
        if(target) parent.postMessage({kind:'pin', token:'${renderToken}', anchor:target.dataset.designId}, '*');
      }, true);
      addEventListener('DOMContentLoaded', () => parent.postMessage({kind:'ready', token:'${renderToken}'}, '*'));
    <\/script>`;
    // The policy is placed BEFORE all untrusted markup. No same-origin permission,
    // networking, forms, popups, child frames, external scripts or top navigation.
    const policy = "default-src 'none'; script-src 'unsafe-inline'; style-src 'unsafe-inline'; img-src data:; connect-src 'none'; frame-src 'none'; form-action 'none'; base-uri 'none'; object-src 'none'";
    iframe.srcdoc = `<!doctype html><meta http-equiv="Content-Security-Policy" content="${policy}"><meta name="referrer" content="no-referrer">${bridge}${frame.html}`;
  }
  async function renderFeedback() {
    pins = await request('/api/feedback');
    list.replaceChildren();
    for (const pin of pins) {
      const entry = node('article', '', list);
      node('p', pin.comment, entry);
      node('small', `${pin.frame} / ${pin.anchor}`, entry);
      node('p', pin.revision === documentState.revision ? 'This revision' : 'Earlier revision', entry);
      const view = node('button', 'View pinned revision', entry);
      view.onclick = () => load(pin.revision);
    }
    debug.textContent = JSON.stringify({document: documentState, feedback: pins}, null, 2);
  }
  async function load(revision) {
    documentState = await request('/api/document' + (revision ? '?revision=' + revision : ''));
    title.textContent = documentState.title;
    state.textContent = `Version ${documentState.version}`;
    frameSelect.replaceChildren();
    for (const frame of documentState.frames) {
      const option = node('option', frame.title, frameSelect); option.value = frame.id;
    }
    renderFrame();
    await renderFeedback();
  }
  // Opaque-origin messages have origin "null": source is essential, not origin.
  // Document messages are hints, not privileged instructions or agent messages.
  window.addEventListener('message', event => {
    if (event.source !== iframe.contentWindow || event.data?.token !== renderToken) return;
    if (event.data.kind === 'ready') sendMode();
    if (event.data.kind !== 'pin' || !pinMode.checked || event.data.anchor !== 'primary-action') return;
    anchor = {document: documentState.id, revision: documentState.revision, frame: frameSelect.value, anchor: event.data.anchor};
    anchorLabel.textContent = `Selected: ${anchor.frame} / ${anchor.anchor}`;
    submit.disabled = false;
  });
  pinMode.onchange = sendMode;
  frameSelect.onchange = renderFrame;
  widthSelect.onchange = renderFrame;
  reload.onclick = () => load().catch(error => status.textContent = error.message);
  advance.onclick = async () => {
    try {
      await request('/api/advance', {expected_revision: documentState.revision});
      await load();
      status.textContent = 'Fixture revision created. No model was called.';
    } catch (error) { status.textContent = error.message; }
  };
  form.onsubmit = async event => {
    event.preventDefault();
    try {
      await request('/api/feedback', {...anchor, comment: comment.value});
      status.textContent = 'Comment saved'; comment.value = '';
      await renderFeedback();
    } catch (error) { status.textContent = error.message; }
  };
  await load();
}

mountDesign(document.querySelector('#design')).catch(error => {
  document.querySelector('#design').textContent = error.message;
});
