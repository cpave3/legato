#!/usr/bin/env python3
"""Verify only a scratch Legato server and candidate Chimera with a local scripted model."""
import argparse
import http.server
import json
import os
import pathlib
import re
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
parser.add_argument('--scenario', choices=('smoke', 'full'), default='smoke')
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
resource = root / 'resource space'
resource.mkdir()
(resource / 'canary.txt').write_text('RESOURCE-READ-CANARY')
(root / 'bin').mkdir()
(root / 'bin/chimera').symlink_to(CHIMERA)
(root / 'r').mkdir(mode=448)
(root / 'tm').mkdir()
for name in ('.profile', '.bash_profile', '.bashrc', '.zshrc', '.zprofile'):
    (home / name).write_text('export PATH=' + str(root / 'bin') + ':/usr/local/bin:/usr/bin:/bin\n')
requests = []
counts = {}
control = {}
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
        if not body.get('stream', False):
            failed = control.get('compaction_fail', False)
            self.send_response(503 if failed else 200)
            self.send_header('Content-Type', 'application/json')
            self.end_headers()
            response = {'error': {'message': 'summary-unavailable-canary'}} if failed else {
                'id': 'scratch-summary', 'object': 'chat.completion', 'created': 1, 'model': 'fixture-model',
                'choices': [{'index': 0, 'message': {'role': 'assistant', 'content': 'Scratch integration summary; retain resource grants and completed child state.'}, 'finish_reason': 'stop'}],
                'usage': {'prompt_tokens': 1000, 'completion_tokens': 20, 'total_tokens': 1020},
            }
            self.wfile.write(json.dumps(response).encode())
            return
        user = next((m.get('content') for m in reversed(body['messages']) if m['role'] == 'user'), '')
        tool_results = [m for m in body['messages'] if m['role'] == 'tool']
        delta = {'role': 'assistant', 'content': 'scratch integration complete'}
        reason = 'stop'
        key = json.dumps(user)
        count = counts.get(key, 0)
        counts[key] = count + 1
        tool = None
        if user == 'CHILD-FAIL-CANARY':
            self.send_response(400)
            self.send_header('Content-Type', 'application/json')
            self.end_headers()
            self.wfile.write(b'{"error":{"message":"CHILD-FAIL-CANARY","type":"invalid_request_error"}}')
            return
        if user == 'CHILD-LONG-CANARY':
            time.sleep(3)
            delta = {'role': 'assistant', 'content': 'LONG-CHILD-RESULT'}
        elif user == 'LEAF-CANARY':
            delta = {'role': 'assistant', 'content': 'LEAF-RESULT-CANARY'}
        elif user == 'CHILD-NESTED-CANARY':
            if count == 0:
                tool = ('spawn_agent', {'prompt': 'LEAF-CANARY', 'purpose': 'scratch nested leaf'})
            else:
                delta = {'role': 'assistant', 'content': 'NESTED-RESULT-CANARY'}
        elif user == 'CHILD-CANARY':
            time.sleep(0.5)
            delta = {'role': 'assistant', 'content': 'CHILD-RESULT-CANARY'}
        elif 'SCENARIO-BACKGROUND' in key and count == 0:
            tool = ('spawn_agent', {'prompt': 'CHILD-CANARY', 'purpose': 'scratch lifecycle', 'background': True})
        elif 'SCENARIO-FOREGROUND' in key and count == 0:
            tool = ('spawn_agent', {'prompt': 'CHILD-CANARY', 'purpose': 'scratch foreground'})
        elif 'SCENARIO-FAIL' in key and count == 0:
            tool = ('spawn_agent', {'prompt': 'CHILD-FAIL-CANARY', 'purpose': 'scratch failed child', 'background': True})
        elif 'SCENARIO-NESTED' in key and count == 0:
            tool = ('spawn_agent', {'prompt': 'CHILD-NESTED-CANARY', 'purpose': 'scratch nested child', 'background': True})
        elif 'SCENARIO-STOP-CHILD' in key and count == 0:
            tool = ('agent_stop', {'id': control['stop_id']})
        elif 'SCENARIO-STOP' in key and count == 0:
            tool = ('spawn_agent', {'prompt': 'CHILD-LONG-CANARY', 'purpose': 'scratch stopped child', 'background': True})
        elif 'SCENARIO-TIMEOUT' in key and count == 0:
            tool = ('spawn_agent', {'prompt': 'CHILD-LONG-CANARY', 'purpose': 'scratch timed child', 'background': True, 'timeout_ms': 200})
        elif 'SCENARIO-RESOURCE-READ' in key and count == 0:
            tool = ('read', {'path': str(resource / 'canary.txt')})
        elif 'SCENARIO-RESOURCE-WRITE' in key and count == 0:
            tool = ('write', {'path': str(resource / 'written.txt'), 'content': 'RESOURCE-WRITE-CANARY'})
        elif 'SCENARIO-QUESTION' in key and count == 0:
            tool = ('ask_question', {'questions': [
                {'question': 'Which runner?', 'header': 'Runner', 'options': [{'label': 'vitest'}, {'label': 'node'}]},
                {'question': 'Which mode?', 'header': 'Mode', 'options': [{'label': 'fast'}, {'label': 'full'}]},
            ]})
        elif 'SCENARIO-PERMISSION' in key and count == 0:
            tool = ('bash', {'command': 'touch ' + control.get('permission_file', 'operator-approved-canary')})
        elif 'SCENARIO-SLOW' in key:
            time.sleep(0.5)
        elif not tool_results or ('SCENARIO-MCP' in key and count == 0):
            names = [t['function']['name'] for t in body.get('tools', [])]
            matches = [n for n in names if 'task' in n and 'context' in n]
            if not matches:
                failures.append({'missing_task_context': names})
            else:
                models.append(matches[0])
                delta = {'role': 'assistant', 'tool_calls': [{'index': 0, 'id': 'scratch-mcp', 'type': 'function', 'function': {'name': matches[0], 'arguments': '{}'}}]}
                reason = 'tool_calls'
        if tool:
            delta = {'role': 'assistant', 'tool_calls': [{'index': 0, 'id': 'scenario-' + str(len(requests)), 'type': 'function', 'function': {'name': tool[0], 'arguments': json.dumps(tool[1])}}]}
            reason = 'tool_calls'
        chunk = {'id': 'scratch-response', 'object': 'chat.completion.chunk', 'created': 1, 'model': 'fixture-model', 'choices': [{'index': 0, 'delta': delta, 'finish_reason': None}]}
        final = {**chunk, 'choices': [{'index': 0, 'delta': {}, 'finish_reason': reason}], 'usage': {'prompt_tokens': 10, 'completion_tokens': 2, 'total_tokens': 12}}
        self.send_response(200)
        self.send_header('Content-Type', 'text/event-stream')
        self.end_headers()
        try:
            for value in (chunk, final):
                self.wfile.write(('data: ' + json.dumps(value) + '\n\n').encode())
                self.wfile.flush()
            self.wfile.write(b'data: [DONE]\n\n')
            self.wfile.flush()
        except (BrokenPipeError, ConnectionResetError):
            # Interrupt/stop scenarios deliberately disconnect the scripted model.
            return
model = http.server.ThreadingHTTPServer(('127.0.0.1', 0), Model)
thread = threading.Thread(target=model.serve_forever, daemon=True)
thread.start()
(home / '.chimera/config.json').write_text(json.dumps({'defaultModel': 'fixture/fixture-model', 'models': {'fixture/fixture-model': {'contextWindow': 200000}}, 'compaction': {'keepRecentTokens': 500, 'reserveTokens': 1000}, 'providers': {'fixture': {'shape': 'openai', 'adapter': 'compatible', 'baseUrl': f'http://127.0.0.1:{model.server_port}/v1', 'apiKey': 'fixture-not-a-secret'}}}))
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
stream_probes = []

def diagnostic(text):
    if token:
        text = text.replace(token, '[scratch-auth-token]')
    return re.sub(r'https?://[^\s<>"\']+', '[scratch-endpoint]', text)

def close_probe(probe, strict=True):
    process, reader = probe['process'], probe['reader']
    if process.poll() is None:
        process.stdin.write('{"close":true}\n')
        process.stdin.flush()
        try:
            process.wait(timeout=5)
        except subprocess.TimeoutExpired:
            process.terminate()
            process.wait(timeout=5)
    reader.join(2)
    if strict and (process.returncode != 0 or reader.is_alive()):
        raise RuntimeError('scratch stream did not close cleanly')

def cli(*args):
    p = subprocess.run([LEGATO, *args], env=env, cwd=work, capture_output=True, text=True, timeout=30)
    if p.returncode:
        raise RuntimeError(diagnostic(p.stderr + p.stdout))
    return p.stdout.strip()

def api(path, body=None):
    req = urllib.request.Request(f'http://127.0.0.1:{port}' + path, data=None if body is None else json.dumps(body).encode(), headers={'Authorization': 'Bearer ' + token, 'Content-Type': 'application/json'})
    try:
        with urllib.request.urlopen(req, timeout=35) as r:
            payload = r.read()
            return json.loads(payload) if payload else None
    except urllib.error.HTTPError as e:
        raise RuntimeError(diagnostic(e.read().decode())) from None

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
    raise RuntimeError(label + ' timed out\n' + diagnostic((root / 'serve.log').read_text()[-12000:]))

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
    cli('resource', 'add', 'Launch canary', str(resource), '--write')
    made = json.loads(cli('task', 'create', 'Scratch Chimera integration', '--json'))
    data = made['data']
    task_id = data.get('id') or data.get('task', {}).get('id')
    if not task_id:
        raise RuntimeError('create task response missing ID: ' + json.dumps(made))
    start()
    result = api('/api/tasks/' + task_id + '/agent', {'agent_kind': 'chimera', 'working_dir': str(work), 'resources': ['Launch canary'], 'prompt': 'Call task_context, then finish.'})
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
    if args.scenario == 'full':
        outcomes = []

        def native(path='', body=None):
            request = urllib.request.Request(endpoint + '/v1/sessions/' + first['chimera_session_id'] + path,
                data=None if body is None else json.dumps(body).encode(), headers={'Content-Type': 'application/json'})
            with urllib.request.urlopen(request, timeout=8) as response:
                raw = response.read()
                return json.loads(raw) if raw else None

        def compact():
            return api('/api/agents?view=compact&task=' + task_id)[0]

        def settled():
            return native()['status'] == 'idle' and compact()['activity'] == ''

        def pending(source):
            return next((approval for approval in api('/api/approvals?task=' + task_id)['approvals'] if approval['source'] == source), None)

        def message(text):
            api('/api/tasks/' + task_id + '/agent', {'prompt': text})

        def ws_send(payload):
            script = r'''
              let input = ''; process.stdin.on('data', chunk => input += chunk);
              process.stdin.on('end', () => {
                const {url, payload} = JSON.parse(input);
                const socket = new WebSocket(url);
                const errors = [];
                const deadline = setTimeout(() => { console.error('scratch WebSocket deadline'); process.exit(1); }, 4000);
                socket.onopen = () => { socket.send(JSON.stringify(payload)); setTimeout(() => socket.close(), 300); };
                socket.onmessage = event => { const message = JSON.parse(event.data); if (message.type === 'error') errors.push(message.error); };
                socket.onerror = () => errors.push('scratch WebSocket transport failed');
                socket.onclose = () => { clearTimeout(deadline); process.stdout.write(JSON.stringify(errors)); };
              });
            '''
            result = subprocess.run([shutil.which('node'), '-e', script], input=json.dumps({'url': f'ws://127.0.0.1:{port}/ws?token=' + token, 'payload': payload}),
                text=True, capture_output=True, env=env, timeout=6, check=True)
            return json.loads(result.stdout)

        def start_probe(cursor=''):
            process = subprocess.Popen([shutil.which('node'), str(pathlib.Path(__file__).with_name('stream_probe.mjs'))],
                stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True, env=env)
            events = []
            reader = threading.Thread(target=lambda: events.extend(json.loads(line) for line in process.stdout), daemon=True)
            reader.start()
            probe = {'process': process, 'reader': reader, 'events': events}
            stream_probes.append(probe)
            process.stdin.write(json.dumps({'url': f'ws://127.0.0.1:{port}/ws?token=' + token, 'task': task_id, 'cursor': cursor}) + '\n')
            process.stdin.flush()
            wait(lambda: events and events[0]['type'] == 'open', 'scratch stream open')
            return probe

        stream = start_probe()
        for attempt in range(10):
            native('/messages?append=true', {'content': 'stream-ready-canary'})
            time.sleep(0.1)
            if any(event.get('barrier') for event in stream['events']):
                break
        assert any(event.get('barrier') and event.get('append') for event in stream['events']), 'idle append not identified as context-only on the live stream'
        assert settled(), 'idle append started a turn'
        outcomes.append('idle-append-through-real-native-stream')
        assert str(resource) in native()['additionalWritePaths'], 'launch resource grant missing'
        for action in ('READ', 'WRITE'):
            before = len(requests)
            message('SCENARIO-RESOURCE-' + action)
            expected = 'RESOURCE-READ-CANARY' if action == 'READ' else 'RESOURCE-WRITE-CANARY'
            wait(lambda: any(expected in json.dumps(request['messages']) for request in requests[before:]) and settled(), 'resource tool result')
        assert (resource / 'written.txt').read_text() == 'RESOURCE-WRITE-CANARY'
        outcomes.append('resource-with-space-granted-and-used')

        for mode in ('BACKGROUND', 'FOREGROUND'):
            before = len(requests)
            message('SCENARIO-' + mode)
            if mode == 'BACKGROUND':
                wait(lambda: compact().get('background_jobs'), 'active background child')
            else:
                running = wait(lambda: next((child for child in native().get('subagentStates', []) if child.get('status') == 'running'), None), 'in-flight child snapshot')
                assert running.get('parentCallId') and running.get('childSessionId'), 'exact foreground association missing'
            wait(lambda: any('CHILD-RESULT-CANARY' in json.dumps(request['messages']) for request in requests[before:]) and settled(), mode + ' child result consumed and parent idle')
            children = native().get('subagentStates', [])
            assert children and children[-1]['status'] == 'finished', 'canonical completion missing'
            assert not compact().get('background_jobs'), 'completed child remains active'
            proxy = api('/api/agents/' + task_id + '/chimera/snapshot')
            assert proxy['subagentStates'] == children, 'proxy replaced canonical lifecycle'
            conversation = api('/api/agents/' + task_id + '/conversation?limit=100')
            assert conversation['status'] == 'idle'
            assert 'CHILD-RESULT-CANARY' in json.dumps(native()), 'child result not delivered'
            outcomes.append(mode.lower() + '-snapshot-list-result')

        for mode, expected in (('FAIL', 'error'), ('TIMEOUT', 'error'), ('NESTED', 'finished')):
            previous = {child['subagentId'] for child in native().get('subagentStates', [])}
            before = len(requests)
            message('SCENARIO-' + mode)
            child = wait(lambda: next((child for child in native().get('subagentStates', []) if child['subagentId'] not in previous and child['status'] == expected), None), mode + ' child terminal state')
            wait(lambda: any(child['subagentId'] in json.dumps(request['messages'][-1]) for request in requests[before:]) and settled(), mode + ' result delivered and settled')
            assert not compact().get('background_jobs'), mode + ' left an active job'
            if mode == 'NESTED':
                assert any('LEAF-RESULT-CANARY' in json.dumps(request['messages']) for request in requests[before:]), 'nested tool result missing'
                assert row()['chimera_session_id'] == first['chimera_session_id'], 'nested child stole main binding'
            outcomes.append(mode.lower() + '-child-result')

        message('SCENARIO-STOP')
        child = wait(lambda: next((child for child in native().get('subagentStates', []) if child.get('purpose') == 'scratch stopped child'), None), 'stoppable child')
        control['stop_id'] = child['subagentId']
        wait(lambda: compact()['activity'] == 'background', 'parent background before stop')
        message('SCENARIO-STOP-CHILD')
        wait(lambda: any(child['subagentId'] == control['stop_id'] and child['status'] == 'interrupted' for child in native().get('subagentStates', [])) and settled(), 'stopped child and parent settled')
        assert not compact().get('background_jobs'), 'stopped child remains an active job'
        outcomes.append('stop-child-without-auto-result')

        for index, decision in enumerate(('answer', 'dismiss', 'ws-answer', 'ws-dismiss')):
            message('SCENARIO-QUESTION-' + str(index))
            approval = wait(lambda: pending('agent_question'), 'question approval')
            snapshot = api('/api/agents/' + task_id + '/chimera/snapshot')
            questions = snapshot['pendingQuestions'][0]['questions']
            assert [question['id'] for question in questions] == ['q1', 'q2']
            wait(lambda: compact()['activity'] == 'waiting', 'question waiting state')
            if decision.startswith('ws-'):
                request_id = snapshot['pendingQuestions'][0]['requestId']
                payload = {'type': 'agent_question', 'agent_id': task_id, 'request_id': request_id}
                if decision == 'ws-answer':
                    errors = ws_send({**payload, 'answers': {'q1': ['vitest']}})
                    assert errors and 'requires one answer' in errors[0], 'invalid answer not surfaced'
                    payload['answers'] = {'q1': ['vitest'], 'q2': ['custom-canary']}
                else:
                    payload['dismissed'] = True
                assert not ws_send(payload), 'WebSocket answer failed'
            elif decision == 'answer':
                api('/api/approvals/' + approval['id'] + '/decision', {'decision': 'answer', 'answers': {'q1': ['vitest'], 'q2': ['custom-canary']}})
            else:
                api('/api/approvals/' + approval['id'] + '/dismiss', {})
            wait(settled, 'question resolved')
            result = native()['toolCalls'][-1]['result']
            if decision.endswith('answer'):
                assert result['status'] == 'answered' and result['answers'][1]['answer'] == 'custom-canary', result
                assert result['answers'][1]['custom'] is True, result
            else:
                assert result['status'] == 'cancelled', result
            outcomes.append('question-' + decision + '-through-ui-payload')
            wait(lambda: pending('agent_question') is None, 'answered approval withdrawn')

        for decision in ('allow', 'deny', 'dismiss'):
            control['permission_file'] = 'permission-' + decision + '-canary'
            message('SCENARIO-PERMISSION-' + decision)
            approval = wait(lambda: pending('agent_permission'), 'permission approval')
            wait(lambda: compact()['activity'] == 'waiting', 'permission waiting state')
            if decision == 'dismiss':
                api('/api/approvals/' + approval['id'] + '/dismiss', {})
            else:
                api('/api/approvals/' + approval['id'] + '/decision', {'decision': decision})
            wait(settled, 'permission resolved')
            assert (work / control['permission_file']).exists() == (decision == 'allow'), 'permission outcome did not control execution'
            outcomes.append('permission-' + decision + '-through-ui-payload')

        for cycle in range(5):
            turn = 'SCENARIO-SLOW-' + str(cycle)
            message(turn)
            wait(lambda: any(turn in json.dumps(request['messages'][-1]) for request in requests), 'slow model turn started')
            markers = ['append-canary-' + str(cycle) + '-' + str(index) for index in range(4)]
            errors = []
            def append(marker):
                try:
                    native('/messages?append=true', {'content': marker})
                except Exception as error:
                    errors.append(type(error).__name__)
            writers = [threading.Thread(target=append, args=(marker,)) for marker in markers]
            for writer in writers:
                writer.start()
            for writer in writers:
                writer.join(10)
                assert not writer.is_alive(), 'append did not finish within budget'
            assert not errors, errors
            wait(lambda: all(marker in json.dumps(native()['messages']) for marker in markers) and settled(), 'accepted concurrent appends persisted and turn settled')
            messages = native()['messages']
            for marker in markers:
                assert sum(marker in json.dumps(item) for item in messages if item['role'] == 'user') == 1, 'lost or duplicated append: ' + marker
        outcomes.append('five-cycles-four-concurrent-appends')
        message('SCENARIO-SLOW-WS')
        wait(lambda: any('SCENARIO-SLOW-WS' in json.dumps(request['messages'][-1]) for request in requests), 'WebSocket steering turn started')
        assert not ws_send({'type': 'agent_message', 'agent_id': task_id, 'request_id': 'queue-canary', 'content': 'queued-message-canary', 'queue': True})
        wait(lambda: 'queued-message-canary' in json.dumps(native()['messages']) and settled(), 'queued UI message consumed')
        outcomes.append('queued-message-through-ui-websocket')
        close_probe(stream)
        observed = [event for event in stream['events'] if event['type'] == 'event']
        ids = [event['eventId'] for event in observed]
        assert len(ids) > 30 and len(ids) == len(set(ids)), 'duplicate or missing live event IDs'
        assert any(event['eventType'] == 'subagent_finished' for event in observed), 'late child completion absent from SSE fanout'
        cursor_index = len(ids) // 2
        for cycle in range(5):
            replay = start_probe(ids[cursor_index])
            expected = ids[cursor_index + 1:]
            wait(lambda: len([event for event in replay['events'] if event['type'] == 'event']) >= len(expected), 'cursor replay')
            close_probe(replay)
            assert [event['eventId'] for event in replay['events'] if event['type'] == 'event'] == expected, 'replay lost, duplicated or reordered events'
        outcomes.append('five-live-reconnect-replays-no-loss-duplicates-reordering')
        compactions = start_probe(ids[-1])
        preserved = {child['subagentId']: child['status'] for child in native().get('subagentStates', [])}
        for failed in (False, True):
            control['compaction_fail'] = failed
            native('/messages?append=true', {'content': 'compaction-input-canary ' * 500})
            count = native().get('compactionCount', 0)
            native('/compact', {})
            wait(lambda: native().get('compactionCount', 0) > count and not native()['compactionActive'], 'manual compaction checkpoint')
            if failed:
                wait(lambda: any(event.get('recoveryWarning') for event in compactions['events']), 'visible summary-unavailable recovery warning')
            assert {child['subagentId']: child['status'] for child in native().get('subagentStates', [])} == preserved, 'compaction lost child lifecycle'
        control['compaction_fail'] = False
        close_probe(compactions)
        outcomes.append('manual-compaction-and-summary-unavailable-recovery')

        reload_stream = start_probe('unknown-pre-reload-cursor')
        old_agent = row()['id']
        api('/api/agents/' + task_id + '/restart', {})
        resumed = wait(lambda: (current := row()) and current['id'] != old_agent and current['chimera_endpoint'] and current, 'scratch agent reload and binding')
        endpoint = resumed['chimera_endpoint']
        assert resumed['chimera_session_id'] == first['chimera_session_id'], 'reload changed conversation identity'
        wait(settled, 'resumed agent idle')
        assert str(resource) in native()['additionalWritePaths'], 'reload lost resource grants'
        assert {child['subagentId']: child['status'] for child in native().get('subagentStates', [])} == preserved, 'resume lost child outcomes'
        before, tool_count = len(requests), len(models)
        message('SCENARIO-MCP-RELOAD')
        wait(lambda: len(models) > tool_count and any(item['role'] == 'tool' and task_id in json.dumps(item['content']) for request in requests[before:] for item in request['messages']) and settled(), 'MCP tool available and executable after reload')
        wait(lambda: any(event.get('reloadMessage') for event in reload_stream['events']), 'existing panel follows replacement execution stream', timeout=6)
        close_probe(reload_stream)
        context = native('/context')
        conversation = api('/api/agents/' + task_id + '/conversation')
        assert context['contextWindow'] == conversation['context_window'] == 200000, 'context window disagreement'
        assert context['lastPromptTokens'] == conversation['context_used'], 'context occupancy disagreement'
        outcomes.append('reload-resume-mcp-resource-context-agreement')
        print(json.dumps({'scenario': 'full', 'passed': outcomes}))
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
except Exception as error:
    (artifacts / 'scratch-native-error.log').write_text(diagnostic((root / 'serve.log').read_text()))
    current = row() if task_id else None
    if current:
        pane = subprocess.run(['/usr/bin/tmux', 'capture-pane', '-p', '-t', current['tmux_session']], env=env, capture_output=True, text=True).stdout
        (artifacts / 'scratch-native-pane.txt').write_text(diagnostic(pane))
        (artifacts / 'scratch-native-row.json').write_text(json.dumps({k: current.get(k) for k in ('status', 'agent_kind', 'command', 'hook_source', 'activity')}, indent=2))
    (artifacts / 'scratch-native-requests.json').write_text(json.dumps(requests, indent=2))
    raise RuntimeError(diagnostic(str(error))) from None
finally:
    for probe in stream_probes:
        close_probe(probe, strict=False)
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
