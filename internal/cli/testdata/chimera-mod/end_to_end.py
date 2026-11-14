#!/usr/bin/env python3
"""Verify only a scratch Legato server and candidate Chimera with a local scripted model."""
import argparse
import http.server
import json
import os
import pathlib
import shutil
import signal
import socket
import sqlite3
import subprocess
import tempfile
import threading
import time
import urllib.request
parser = argparse.ArgumentParser()
parser.add_argument('--legato', required=True)
parser.add_argument('--chimera', required=True)
parser.add_argument('--artifacts', required=True)
args = parser.parse_args()
LEGATO = str(pathlib.Path(args.legato).resolve())
CHIMERA = str(pathlib.Path(args.chimera).resolve())
artifacts = pathlib.Path(args.artifacts).resolve()
artifacts.mkdir(parents=True, exist_ok=True)
root = pathlib.Path(tempfile.mkdtemp(prefix='l180-'))
home = root / 'h'
home.mkdir()
(home / '.chimera').mkdir()
work = root / 'work'
work.mkdir()
(root / 'bin').mkdir()
(root / 'bin/chimera').symlink_to(CHIMERA)
(root / 'r').mkdir(mode=448)
(root / 'tm').mkdir()
for name in ('.profile', '.bash_profile', '.bashrc', '.zshrc', '.zprofile'):
    (home / name).write_text('export PATH=' + str(root / 'bin') + ':/usr/local/bin:/usr/bin:/bin\n')
requests = []
models = []
failures = []

class Model(http.server.BaseHTTPRequestHandler):

    def log_message(self, *_):
        pass

    def do_GET(self):
        self.send_response(200)
        self.send_header('Content-Type', 'application/json')
        self.end_headers()
        self.wfile.write(b'{"object":"list","data":[]}')

    def do_POST(self):
        body = json.loads(self.rfile.read(int(self.headers['Content-Length'])))
        requests.append(body)
        user = next((m.get('content') for m in reversed(body['messages']) if m['role'] == 'user'), '')
        tool_results = [m for m in body['messages'] if m['role'] == 'tool']
        delta = {'role': 'assistant', 'content': 'scratch integration complete'}
        reason = 'stop'
        if not tool_results:
            names = [t['function']['name'] for t in body.get('tools', [])]
            matches = [n for n in names if 'task' in n and 'context' in n]
            if not matches:
                failures.append({'missing_task_context': names})
            else:
                models.append(matches[0])
                delta = {'role': 'assistant', 'tool_calls': [{'index': 0, 'id': 'scratch-mcp', 'type': 'function', 'function': {'name': matches[0], 'arguments': '{}'}}]}
                reason = 'tool_calls'
        chunk = {'id': 'scratch-response', 'object': 'chat.completion.chunk', 'created': 1, 'model': 'fixture-model', 'choices': [{'index': 0, 'delta': delta, 'finish_reason': None}]}
        final = {**chunk, 'choices': [{'index': 0, 'delta': {}, 'finish_reason': reason}], 'usage': {'prompt_tokens': 10, 'completion_tokens': 2, 'total_tokens': 12}}
        self.send_response(200)
        self.send_header('Content-Type', 'text/event-stream')
        self.end_headers()
        for value in (chunk, final):
            self.wfile.write(('data: ' + json.dumps(value) + '\n\n').encode())
            self.wfile.flush()
        self.wfile.write(b'data: [DONE]\n\n')
        self.wfile.flush()
model = http.server.ThreadingHTTPServer(('127.0.0.1', 0), Model)
thread = threading.Thread(target=model.serve_forever, daemon=True)
thread.start()
(home / '.chimera/config.json').write_text(json.dumps({'defaultModel': 'fixture/fixture-model', 'providers': {'fixture': {'shape': 'openai', 'adapter': 'compatible', 'baseUrl': f'http://127.0.0.1:{model.server_port}/v1', 'apiKey': 'fixture-not-a-secret'}}}))
with socket.socket() as sock:
    sock.bind(('127.0.0.1', 0))
    port = sock.getsockname()[1]
config = root / 'config.yaml'
config.write_text(f'db:\n  path: {root}/legato.db\ndata_dir: {root}/data\nweb:\n  discovery: false\nupdates:\n  mode: off\nagents:\n  peer_awareness: false\n  sandbox:\n    enabled: false\nadapters:\n  chimera:\n    native_bridge: true\n    launch_args: [--no-skills, --no-modes, --no-claude-compat, --auto-approve, none]\nboard:\n  columns:\n    - name: Backlog\n    - name: Doing\n    - name: Review\n    - name: Done\n')
env = {'PATH': str(root / 'bin') + ':/usr/local/bin:/usr/bin:/bin', 'HOME': str(home), 'LEGATO_HOME': str(home / '.legato'), 'LEGATO_CONFIG': str(config), 'XDG_RUNTIME_DIR': str(root / 'r'), 'XDG_CONFIG_HOME': str(home / '.config'), 'XDG_DATA_HOME': str(home / '.local/share'), 'TMUX_TMPDIR': str(root / 'tm'), 'TERM': 'xterm-256color', 'LANG': 'C.UTF-8'}
log = open(root / 'serve.log', 'w')
process = None
token = None
task_id = None

def cli(*args):
    p = subprocess.run([LEGATO, *args], env=env, cwd=work, capture_output=True, text=True, timeout=30)
    if p.returncode:
        raise RuntimeError(p.stderr + p.stdout)
    return p.stdout.strip()

def api(path, body=None):
    req = urllib.request.Request(f'http://127.0.0.1:{port}' + path, data=None if body is None else json.dumps(body).encode(), headers={'Authorization': 'Bearer ' + token, 'Content-Type': 'application/json'})
    try:
        with urllib.request.urlopen(req, timeout=35) as r:
            payload = r.read()
            return json.loads(payload) if payload else None
    except urllib.error.HTTPError as e:
        raise RuntimeError(e.read().decode()) from e

def wait(fn, label, timeout=25):
    end = time.monotonic() + timeout
    while time.monotonic() < end:
        try:
            result = fn()
            if result:
                return result
        except (OSError, urllib.error.URLError):
            pass
        time.sleep(0.05)
    raise RuntimeError(label + ' timed out\n' + (root / 'serve.log').read_text()[-12000:])

def start():
    global process, token
    process = subprocess.Popen([LEGATO, 'serve', '--port', str(port)], env=env, cwd=work, stdout=log, stderr=log)
    wait(lambda: (root / 'data/auth-token').exists(), 'auth token')
    token = cli('auth', 'token')
    wait(lambda: api('/api/tasks'), 'server health')

def row():
    with sqlite3.connect(root / 'legato.db') as db:
        db.row_factory = sqlite3.Row
        value = db.execute('select * from agent_sessions where task_id=? order by id desc limit 1', (task_id,)).fetchone()
        return dict(value) if value else None
try:
    cli('columns', 'sync')
    made = json.loads(cli('task', 'create', 'Scratch Chimera integration', '--json'))
    data = made['data']
    task_id = data.get('id') or data.get('task', {}).get('id')
    if not task_id:
        raise RuntimeError('create task response missing ID: ' + json.dumps(made))
    start()
    result = api('/api/tasks/' + task_id + '/agent', {'agent_kind': 'chimera', 'working_dir': str(work), 'prompt': 'Call task_context, then finish.'})
    first = wait(lambda: (x := row()) and x['chimera_endpoint'] and (x['hook_source'] == 'mod') and x, 'mod binding')
    wait(lambda: len(requests) >= 2 and any((m['role'] == 'tool' for m in requests[-1]['messages'])), 'MCP tool round trip')
    assert not failures, failures
    tool_result = [m['content'] for m in requests[-1]['messages'] if m['role'] == 'tool'][-1]
    assert task_id in json.dumps(tool_result), (task_id, tool_result)
    wait(lambda: row()['activity'] == '', 'idle state')
    with sqlite3.connect(root / 'legato.db') as db:
        saved = db.execute("select provider_session_id from task_native_sessions where task_id=? and agent_kind='chimera'", (task_id,)).fetchone()
    assert saved and saved[0] == first['chimera_session_id']
    endpoint = first['chimera_endpoint']
    parsed = urllib.parse.urlsplit(endpoint)
    anonymous = urllib.parse.urlunsplit((parsed.scheme, parsed.netloc, '/v1/instance', '', ''))
    try:
        urllib.request.urlopen(anonymous, timeout=3)
        raise AssertionError('anonymous server accepted request')
    except urllib.error.HTTPError as e:
        assert e.code == 404, e.code
    with urllib.request.urlopen(endpoint + '/v1/instance', timeout=3) as r:
        assert r.status == 200
        instance = json.load(r)
    argv = (pathlib.Path('/proc') / str(instance['pid']) / 'cmdline').read_bytes().split(b'\x00')
    assert b'--mcp-config' not in argv
    assert not any((parsed.path.strip('/').encode() in arg for arg in argv))
    tmux_secret = subprocess.run(['/usr/bin/tmux', 'show-environment', '-t', first['tmux_session'], 'CHIMERA_SERVER_SECRET'], env=env, capture_output=True, text=True)
    assert tmux_secret.returncode != 0
    bootstrap = subprocess.run(['/usr/bin/tmux', 'show-environment', '-t', first['tmux_session'], 'LEGATO_CHIMERA_SECRET_FILE'], env=env, capture_output=True, text=True, check=True).stdout.strip().split('=', 1)[1]
    assert not pathlib.Path(bootstrap).exists()
    process.terminate()
    process.wait(timeout=15)
    process = None
    start()
    after = wait(lambda: (x := row()) and x['chimera_endpoint'] == endpoint and x, 'restart binding')
    api('/api/tasks/' + task_id + '/agent', {'prompt': 'After the scratch server restart, finish.'})
    wait(lambda: any(('After the scratch server restart' in json.dumps(r['messages']) for r in requests)), 'post-restart message')
    wait(lambda: row()['activity'] == '', 'post-restart idle')
    subprocess.run(['/usr/bin/tmux', 'send-keys', '-t', first['tmux_session'], '-l', '/exit'], env=env, check=True)
    subprocess.run(['/usr/bin/tmux', 'send-keys', '-t', first['tmux_session'], 'Enter'], env=env, check=True)
    wait(lambda: (x := row()) is None or not x['chimera_endpoint'], 'exit unbind')
    print(json.dumps({'spawn': True, 'mod_binding': True, 'saved_identity': True, 'mcp_tool': models[0], 'mcp_round_trip': True, 'mod_mcp_no_flag': True, 'secret_not_in_argv': True, 'secret_not_in_tmux': True, 'bootstrap_consumed': True, 'anonymous_http': 404, 'authenticated_http': 200, 'restart_recovery': True, 'message_after_restart': True, 'exit_unbind': True}))
except Exception:
    shutil.copyfile(root / 'serve.log', artifacts / 'scratch-native-error.log')
    current = row() if task_id else None
    if current:
        pane = subprocess.run(['/usr/bin/tmux', 'capture-pane', '-p', '-t', current['tmux_session']], env=env, capture_output=True, text=True).stdout
        (artifacts / 'scratch-native-pane.txt').write_text(pane)
        (artifacts / 'scratch-native-row.json').write_text(json.dumps({k: current.get(k) for k in ('status', 'agent_kind', 'command', 'hook_source', 'activity')}, indent=2))
    (artifacts / 'scratch-native-requests.json').write_text(json.dumps(requests, indent=2))
    raise
finally:
    if process is not None:
        process.terminate()
        try:
            process.wait(timeout=15)
        except subprocess.TimeoutExpired:
            process.kill()
            process.wait()
    subprocess.run(['/usr/bin/tmux', 'kill-server'], env=env, capture_output=True)
    log.close()
    model.shutdown()
    model.server_close()
    thread.join(2)
    shutil.rmtree(root)
