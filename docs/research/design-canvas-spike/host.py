"""THROWAWAY loopback host. No Legato imports, config, tokens, or database.
Only synthetic fixture input; not a multi-user service or an HTML upload API.
"""
import argparse
import hashlib
import json
import os
from http.server import BaseHTTPRequestHandler, HTTPServer
from pathlib import Path
from urllib.parse import parse_qs, urlsplit

ROOT = Path(__file__).parent


def fixture(version=1):
    action = 'Open workspace' if version > 1 else 'Continue'
    css = '<style>body{margin:0;padding:32px;font:16px system-ui;background:#f6f8fa;color:#17212b}h1{font-size:30px}button{padding:12px 20px;background:#176350;color:white;border:0;border-radius:6px}section{padding:24px 0;border-top:1px solid #cad1d8}p{line-height:1.6}output{display:block;font-size:13px;margin-top:24px}</style>'
    script = '''<script>
    document.querySelector('button').onclick = e => e.target.textContent = 'Workspace opened';
    try { parent.document.querySelector('h1').textContent = 'BREACHED'; document.body.dataset.isolation='failed'; }
    catch { document.body.dataset.isolation='blocked'; }
    fetch('/api/feedback').then(() => document.body.dataset.network='failed').catch(() => document.body.dataset.network='blocked');
    </script>'''
    return {'id': 'workspace-study', 'title': 'Workspace study', 'version': version, 'frames': [
        {'id': 'overview', 'title': 'Overview', 'width': 760, 'height': 500, 'html': f'<!doctype html><html><head>{css}</head><body><p>Synthetic design fixture</p><h1>Your next workspace</h1><p>Keep a project and its conversations together.</p><section><h2>Website refresh</h2><p>Three conversations · Updated today</p><button data-design-id="primary-action">{action}</button></section><output>Interactive fixture · Select Pin mode to comment.</output>{script}</body></html>'},
        {'id': 'details', 'title': 'Details', 'width': 390, 'height': 560, 'html': f'<!doctype html><html><head>{css}</head><body><p>Synthetic design fixture</p><h1>Website refresh</h1><section><h2>Conversations</h2><p>Layout exploration</p><p>Content structure</p><button data-design-id="primary-action">{action}</button></section>{script}</body></html>'}
    ]}


def revision(document):
    return hashlib.sha256(json.dumps(document, sort_keys=True, separators=(',', ':')).encode()).hexdigest()


def create_server(state_path, port):
    if state_path.exists():
        state = json.loads(state_path.read_text())
    else:
        document = fixture()
        key = revision(document)
        state = {'current': key, 'revisions': {key: document}, 'feedback': []}

    def save():
        state_path.parent.mkdir(parents=True, exist_ok=True)
        temporary = state_path.with_suffix('.tmp')
        temporary.write_text(json.dumps(state, indent=2))
        os.replace(temporary, state_path)

    save()

    class Handler(BaseHTTPRequestHandler):
        def log_message(self, *_):
            pass

        def respond(self, status, value):
            data = json.dumps(value).encode()
            self.send_response(status)
            self.send_header('Content-Type', 'application/json')
            self.send_header('Content-Length', str(len(data)))
            self.send_header('Cache-Control', 'no-store')
            self.send_header('X-Content-Type-Options', 'nosniff')
            self.end_headers()
            self.wfile.write(data)

        def do_GET(self):
            url = urlsplit(self.path)
            if url.path == '/api/document':
                key = parse_qs(url.query).get('revision', [state['current']])[0]
                if key not in state['revisions']:
                    return self.respond(404, {'error': 'Unknown revision'})
                return self.respond(200, {**state['revisions'][key], 'revision': key})
            if url.path == '/api/feedback':
                return self.respond(200, state['feedback'])
            files = {'/': 'standalone.html', '/standalone.html': 'standalone.html', '/embedded.html': 'embedded.html', '/core.js': 'core.js', '/style.css': 'style.css'}
            if url.path not in files:
                return self.respond(404, {'error': 'Not found'})
            file = ROOT / files[url.path]
            data = file.read_bytes()
            self.send_response(200)
            self.send_header('Content-Type', {'.html': 'text/html', '.js': 'text/javascript', '.css': 'text/css'}[file.suffix])
            self.send_header('Content-Length', str(len(data)))
            self.send_header('Cache-Control', 'no-store')
            self.send_header('X-Content-Type-Options', 'nosniff')
            self.send_header('Content-Security-Policy', "default-src 'none'; script-src 'self' 'unsafe-inline'; style-src 'self' 'unsafe-inline'; connect-src 'self'; frame-src 'self' about:; base-uri 'none'; form-action 'none'; frame-ancestors 'self'")
            self.end_headers()
            self.wfile.write(data)

        def do_POST(self):
            if self.headers.get('Origin') not in (None, f'http://127.0.0.1:{self.server.server_port}'):
                return self.respond(403, {'error': 'Foreign or opaque origin refused'})
            if self.headers.get('Content-Type') != 'application/json':
                return self.respond(415, {'error': 'Expected JSON'})
            length = int(self.headers.get('Content-Length', 0))
            if not 0 < length <= 16000:
                return self.respond(413, {'error': 'Expected at most 16000 bytes'})
            try:
                body = json.loads(self.rfile.read(length))
            except (ValueError, UnicodeError):
                return self.respond(400, {'error': 'Invalid JSON'})
            if not isinstance(body, dict):
                return self.respond(400, {'error': 'Expected object'})
            if self.path == '/api/advance':
                if body.get('expected_revision') != state['current']:
                    return self.respond(409, {'error': 'Revision changed; reload first'})
                document = fixture(state['revisions'][state['current']]['version'] + 1)
                key = revision(document)
                state['revisions'][key] = document
                state['current'] = key
                save()
                return self.respond(200, {**document, 'revision': key})
            if self.path == '/api/feedback':
                if body.get('revision') != state['current']:
                    return self.respond(409, {'error': 'Stale revision; reload before pinning. Original feedback remains on its revision.'})
                doc = state['revisions'][state['current']]
                if body.get('document') != doc['id'] or body.get('frame') not in [f['id'] for f in doc['frames']] or body.get('anchor') != 'primary-action':
                    return self.respond(400, {'error': 'Unknown document, frame or anchor'})
                comment = body.get('comment')
                if not isinstance(comment, str) or not 0 < len(comment.strip()) <= 4000:
                    return self.respond(400, {'error': 'Comment must contain 1–4000 characters'})
                pin = {k: body[k] for k in ('document', 'revision', 'frame', 'anchor', 'comment')}
                pin['id'] = f'pin-{len(state["feedback"]) + 1}'
                state['feedback'].append(pin)
                save()
                return self.respond(201, pin)
            self.respond(404, {'error': 'Not found'})

    return HTTPServer(('127.0.0.1', port), Handler)


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--state', type=Path, required=True, help='Scratch PROTOTYPE-state.json path')
    parser.add_argument('--port', type=int, default=0)
    args = parser.parse_args()
    server = create_server(args.state, args.port)
    print(f'http://127.0.0.1:{server.server_port}', flush=True)
    try:
        server.serve_forever()
    except KeyboardInterrupt:
        pass
    finally:
        server.server_close()
