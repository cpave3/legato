// Disposable loopback harness, NOT a production host or auth decision.
import { createServer } from 'node:http';
import { readFile, writeFile, rename, mkdir } from 'node:fs/promises';
import { dirname } from 'node:path';
import { Editor } from './.build/core.js';

export async function createHost(statePath) {
  let stored = {version: 0, session: null};
  try { stored = JSON.parse(await readFile(statePath, 'utf8')); }
  catch (error) { if(error.code !== 'ENOENT') throw error; }
  if(stored.session) Editor.deserialize(stored.session);
  let writes = Promise.resolve();
  const files = new Map([['/', 'index.html'], ['/index.html', 'index.html'], ['/embedded.html', 'embedded.html'], ['/style.css', 'style.css'], ['/ui.js', '.build/ui.js'], ['/core.js', '.build/core.js']]);
  const host = createServer(async (request, response) => {
    const json = (status, data) => {
      response.writeHead(status, {'Content-Type':'application/json', 'Cache-Control':'no-store', 'X-Content-Type-Options':'nosniff'});
      response.end(JSON.stringify(data));
    };
    try {
      if(request.url === '/state' && request.method === 'GET') return json(200, stored);
      if(request.url === '/state' && request.method === 'POST') {
        const origin = request.headers.origin;
        if(origin && origin !== `http://127.0.0.1:${host.address().port}`) return json(403, {error:'Foreign or opaque origin refused'});
        if(request.headers['content-type'] !== 'application/json') return json(415, {error:'Expected JSON'});
        let text = '';
        for await (const chunk of request) {
          text += chunk;
          if(Buffer.byteLength(text) > 1_000_000) return json(413, {error:'Scratch session exceeds 1 MB'});
        }
        const body = JSON.parse(text);
        const save = async () => {
          if(body.expectedVersion !== stored.version) return json(409, {error:'Saved state changed in another client. Load saved state before saving.'});
          const session = Editor.deserialize(body.session).serialize();
          const next = {version: stored.version + 1, session};
          await mkdir(dirname(statePath), {recursive:true});
          await writeFile(statePath + '.tmp', JSON.stringify(next));
          await rename(statePath + '.tmp', statePath);
          stored = next;
          json(200, {version: stored.version});
        };
        // Serialize CAS + atomic replace: concurrent requests cannot both win.
        const operation = writes.then(save);
        writes = operation.catch(() => {}); // individual failure is returned below
        await operation;
        return;
      }
      if(request.method !== 'GET' || !files.has(request.url)) return json(404, {error:'Not found'});
      const file = files.get(request.url);
      const data = await readFile(new URL(file, import.meta.url));
      response.writeHead(200, {
        'Content-Type': file.endsWith('.html') ? 'text/html' : file.endsWith('.css') ? 'text/css' : 'text/javascript',
        'Cache-Control':'no-store', 'X-Content-Type-Options':'nosniff',
        'Content-Security-Policy': "default-src 'none'; script-src 'self' 'unsafe-inline'; style-src 'self' 'unsafe-inline'; connect-src 'self'; frame-src 'self' about:; base-uri 'none'; form-action 'none'; frame-ancestors 'self'",
      });
      response.end(data);
    } catch(error) { json(400, {error: error.message}); }
  });
  return host;
}
