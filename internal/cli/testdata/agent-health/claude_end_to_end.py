#!/usr/bin/env python3
"""Real Claude CLI/mod/bridge against disposable Legato and loopback model input."""
import argparse
import concurrent.futures
import http.server
import json
import os
import pathlib
import re
import shlex
import shutil
import socket
import subprocess
import tempfile
import threading
import time
import urllib.error
import urllib.request

parser = argparse.ArgumentParser()
parser.add_argument('--legato', required=True)
parser.add_argument('--artifacts', required=True)
args = parser.parse_args()
legato = str(pathlib.Path(args.legato).resolve())
claude = shutil.which('claude')
assert claude, 'Claude CLI unavailable'
artifacts = pathlib.Path(args.artifacts).resolve()
artifacts.mkdir(parents=True, exist_ok=True)
root = pathlib.Path(tempfile.mkdtemp(prefix='lc-health-'))
home, work = root / 'h', root / 'work'
for folder in [home, work, root / 'bin', root / 'r', root / 'tm', home / '.claude']:
    folder.mkdir(mode=0o700, parents=True, exist_ok=True)
(root / 'resource').mkdir()
requests, outcomes = [], []
control = {'scenario': 'plain', 'delay': 0}

def response_content(body):
    messages = body.get('messages', [])
    latest = messages[-1].get('content', []) if messages else []
    if isinstance(latest, str):
        latest = [{'type': 'text', 'text': latest}]
    results = [item for item in latest if item.get('type') == 'tool_result']
    scenario = control['scenario']
    if scenario.startswith('child-'):
        if any(message.get('role') == 'user' and 'CLAUDE-CHILD-PROVIDER-' in json.dumps(message.get('content')) for message in messages):
            time.sleep(control['child_delay'])
            return [{'type': 'text', 'text': 'CLAUDE-CHILD-RESULT-CANARY'}]
        if control.get('stop_id') and ('CLAUDE-STOP-CHILD' in json.dumps(latest) or control.get('stop_phase') in [1, 2]):
            phase = control.get('stop_phase', 0)
            control['stop_phase'] = phase + 1
            if phase == 0:
                return [{'type': 'tool_use', 'id': 'tool-find-stop', 'name': 'ToolSearch', 'input': {'query': 'select:TaskStop'}}]
            if phase == 1:
                return [{'type': 'tool_use', 'id': 'tool-stop-child', 'name': 'TaskStop', 'input': {'task_id': control['stop_id']}}]
        if not results and not control.get('child_started') and body.get('tools'):
            control['child_started'] = True
            return [{'type': 'tool_use', 'id': 'tool-child-' + str(len(requests)), 'name': 'Agent', 'input': {'description': 'Health ' + scenario, 'subagent_type': 'general-purpose', 'prompt': 'CLAUDE-CHILD-PROVIDER-' + scenario}}]
    if scenario == 'mcp':
        names = [tool.get('name', '') for tool in body.get('tools', [])]
        def references(value):
            if isinstance(value, dict):
                if value.get('type') == 'tool_reference':
                    names.append(value.get('tool_name', ''))
                for item in value.values():
                    references(item)
            elif isinstance(value, list):
                for item in value:
                    references(item)
        references(latest)
        if results and task_id in json.dumps(results):
            return [{'type': 'text', 'text': 'CLAUDE-MCP-CANARY'}]
        name = next((name for name in names if name.endswith('task_context')), None)
        return [{'type': 'tool_use', 'id': 'tool-mcp-' + str(len(requests)), 'name': name or 'ToolSearch', 'input': {} if name else {'query': '+legato task_context', 'max_results': 1}}]
    if scenario.startswith('permission') and not results:
        return [{'type': 'tool_use', 'id': 'tool-permission-' + str(len(requests)), 'name': 'Bash', 'input': {'command': 'printf CLAUDE-PERMISSION-CANARY > ' + shlex.quote(str(root / 'resource' / (scenario + '.txt'))), 'description': 'Write the isolated permission canary'}}]
    if scenario.startswith('question') and not results:
        return [{'type': 'tool_use', 'id': 'tool-question-' + str(len(requests)), 'name': 'AskUserQuestion', 'input': {'questions': [{'question': 'Which health runner?', 'header': 'Runner', 'options': [{'label': 'First', 'description': 'First runner'}, {'label': 'Second', 'description': 'Second runner'}], 'multiSelect': False}, {'question': 'What note?', 'header': 'Note', 'options': [{'label': 'Known', 'description': 'Known answer'}, {'label': 'Other known', 'description': 'Other answer'}], 'multiSelect': False}]}}]
    return [{'type': 'text', 'text': 'CLAUDE-INTEGRATION-CANARY'}]

class Model(http.server.BaseHTTPRequestHandler):
    def log_message(self, *unused):
        pass

    def do_GET(self):
        self.send_error(404)

    def do_POST(self):
        body = json.loads(self.rfile.read(int(self.headers.get('Content-Length', '0'))) or '{}')
        if 'count_tokens' in self.path or '/messages' not in self.path:
            data = json.dumps({'input_tokens': 40} if 'count_tokens' in self.path else {}).encode()
            self.send_response(200)
            self.send_header('Content-Type', 'application/json')
            self.end_headers()
            self.wfile.write(data)
            return
        requests.append(body)
        if control['scenario'] == 'api-error':
            self.send_response(400)
            self.send_header('Content-Type', 'application/json')
            self.end_headers()
            self.wfile.write(json.dumps({'type': 'error', 'error': {'type': 'invalid_request_error', 'message': 'CLAUDE-NONSECRET-API-ERROR-CANARY'}}).encode())
            return
        time.sleep(control['delay'])
        content = response_content(body)
        stop_reason = 'tool_use' if any(item['type'] == 'tool_use' for item in content) else 'end_turn'
        message = {'id': 'msg-fixture-' + str(len(requests)), 'type': 'message', 'role': 'assistant', 'model': body.get('model'), 'content': content, 'stop_reason': stop_reason, 'stop_sequence': None, 'usage': {'input_tokens': 40, 'output_tokens': 8}}
        try:
            self.send_response(200)
            self.send_header('Content-Type', 'text/event-stream' if body.get('stream') else 'application/json')
            self.end_headers()
            if not body.get('stream'):
                self.wfile.write(json.dumps(message).encode())
                return
            events = [('message_start', {'message': {**message, 'content': [], 'stop_reason': None}})]
            for index, item in enumerate(content):
                start = {'type': 'text', 'text': ''} if item['type'] == 'text' else {**item, 'input': {}}
                delta = {'type': 'text_delta', 'text': item['text']} if item['type'] == 'text' else {'type': 'input_json_delta', 'partial_json': json.dumps(item['input'])}
                events.extend([('content_block_start', {'index': index, 'content_block': start}), ('content_block_delta', {'index': index, 'delta': delta}), ('content_block_stop', {'index': index})])
            events.extend([('message_delta', {'delta': {'stop_reason': stop_reason, 'stop_sequence': None}, 'usage': {'output_tokens': 8}}), ('message_stop', {})])
            for name, data in events:
                self.wfile.write(('event: ' + name + '\ndata: ' + json.dumps({'type': name, **data}) + '\n\n').encode())
                self.wfile.flush()
        except (BrokenPipeError, ConnectionResetError):
            pass

model = http.server.ThreadingHTTPServer(('127.0.0.1', 0), Model)
threading.Thread(target=model.serve_forever, daemon=True).start()
wrapper = root / 'bin/claude'
wrapper.write_text('#!/bin/sh\nprintf called > ' + shlex.quote(str(root / 'wrapper-called')) + '\nprintf "%s\\n" "$$" >> ' + shlex.quote(str(root / 'claude-pids')) + '\nexport ANTHROPIC_BASE_URL=' + shlex.quote(f'http://127.0.0.1:{model.server_port}') + '\nexport ANTHROPIC_API_KEY=fixture-not-a-secret\nexport CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC=1\nexport DISABLE_AUTOUPDATER=1\nexec ' + shlex.quote(claude) + ' --debug-file ' + shlex.quote(str(root / 'claude-debug.log')) + ' "$@"\n')
wrapper.chmod(0o700)
profile = {'hasCompletedOnboarding': True, 'lastOnboardingVersion': '2.1.322', 'theme': 'dark', 'customApiKeyResponses': {'approved': ['fixture-not-a-secret'], 'rejected': []}, 'projects': {str(work): {'hasTrustDialogAccepted': True, 'hasCompletedProjectOnboarding': True}}}
for file in [home / '.claude.json', home / '.claude/.claude.json']:
    file.write_text(json.dumps(profile))
for name in ['.profile', '.bash_profile', '.bashrc', '.zshrc', '.zprofile']:
    (home / name).write_text('export PATH=' + shlex.quote(str(root / 'bin') + ':' + os.environ['PATH']) + '\n')
with socket.socket() as sock:
    sock.bind(('127.0.0.1', 0))
    port = sock.getsockname()[1]
config = root / 'config.yaml'
config.write_text(f'db:\n  path: {root}/legato.db\ndata_dir: {root}/data\nweb:\n  discovery: false\nupdates:\n  mode: off\nagents:\n  peer_awareness: false\n  sandbox:\n    enabled: false\nadapters:\n  claude-code:\n    native_bridge: true\n    launch_args: [--model, claude-sonnet-5-5, --permission-mode, default]\nboard:\n  columns:\n    - name: Backlog\n    - name: Doing\n    - name: Review\n    - name: Done\n')
env = {'PATH': str(root / 'bin') + ':' + os.environ['PATH'], 'HOME': str(home), 'CLAUDE_CONFIG_DIR': str(home / '.claude'), 'LEGATO_HOME': str(home / '.legato'), 'LEGATO_CONFIG': str(config), 'XDG_RUNTIME_DIR': str(root / 'r'), 'XDG_CONFIG_HOME': str(home / '.config'), 'XDG_DATA_HOME': str(home / '.local/share'), 'TMUX_TMPDIR': str(root / 'tm'), 'TERM': 'xterm-256color', 'LANG': 'C.UTF-8'}
log = open(root / 'serve.log', 'w')
process, token, task_id = None, None, None
streams = []


def diagnostic(text):
    if token:
        text = text.replace(token, '[scratch-auth-token]')
    return re.sub(r'https?://[^\s<>"\']+', '[scratch-endpoint]', text)


def cli(*arguments):
    result = subprocess.run([legato, *arguments], env=env, cwd=work, capture_output=True, text=True, timeout=20)
    if result.returncode:
        raise RuntimeError(diagnostic(result.stderr + result.stdout))
    return result.stdout.strip()


def api(path, body=None):
    request = urllib.request.Request(f'http://127.0.0.1:{port}' + path, data=None if body is None else json.dumps(body).encode(), headers={'Authorization': 'Bearer ' + token, 'Content-Type': 'application/json'})
    try:
        with urllib.request.urlopen(request, timeout=8) as response:
            data = response.read()
            return json.loads(data) if data else None
    except urllib.error.HTTPError as error:
        raise RuntimeError(diagnostic(error.read().decode())) from None


def row():
    rows = api('/api/agents?task=' + task_id)
    return rows[0] if rows else None


def wait(check, label, timeout=35):
    deadline = time.monotonic() + timeout
    while time.monotonic() < deadline:
        try:
            value = check()
            if value:
                return value
        except (OSError, urllib.error.URLError):
            pass
        time.sleep(0.05)
    raise RuntimeError(label + ' timed out')

def start_stream(cursor=''):
    child = subprocess.Popen([shutil.which('node'), str(pathlib.Path(__file__).with_name('ws_probe.mjs'))], env=env, stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True)
    events = []
    reader = threading.Thread(target=lambda: events.extend(json.loads(line) for line in child.stdout), daemon=True)
    reader.start()
    stream = {'process': child, 'events': events, 'reader': reader}
    streams.append(stream)
    child.stdin.write(json.dumps({'url': f'ws://127.0.0.1:{port}/ws?token=' + token, 'task': task_id, 'cursor': cursor}) + '\n')
    child.stdin.flush()
    wait(lambda: events and events[0]['type'] == 'open', 'native WebSocket open')
    return stream

def ws_send(stream, payload):
    stream['process'].stdin.write(json.dumps({'send': payload}) + '\n')
    stream['process'].stdin.flush()

def close_stream(stream):
    child = stream['process']
    if child.poll() is None:
        child.stdin.write('{"close":true}\n')
        child.stdin.flush()
        try:
            child.wait(timeout=5)
        except subprocess.TimeoutExpired:
            child.terminate()
            child.wait(timeout=5)
    stream['reader'].join(2)

try:
    cli('columns', 'sync')
    created = json.loads(cli('task', 'create', 'Claude integration scratch', '--json'))['data']
    task_id = created.get('id') or created['task']['id']
    process = subprocess.Popen([legato, 'serve', '--port', str(port)], env=env, cwd=work, stdout=log, stderr=log)
    wait(lambda: (root / 'data/auth-token').exists(), 'scratch server token')
    token = cli('auth', 'token')
    wait(lambda: api('/api/tasks'), 'scratch API')
    api('/api/tasks/' + task_id + '/agent', {'agent_kind': 'claude-code', 'working_dir': str(work), 'prompt': 'CLAUDE-SMOKE: return the integration canary.'})
    first = wait(lambda: (current := row()) and current.get('has_chimera_endpoint') and current, 'real Claude native binding')
    wait(lambda: requests and 'CLAUDE-INTEGRATION-CANARY' in json.dumps(api('/api/agents/' + task_id + '/conversation')) and row()['activity'] == '', 'native final reply and idle agreement')
    outcomes.append('real-launch-mod-state-native-final-idle')
    snapshot = lambda: api('/api/agents/' + task_id + '/chimera/snapshot')
    assert api('/api/agents/' + task_id + '/chimera/modes') == [], 'Claude advertised unsupported modes'
    try:
        api('/api/agents/' + task_id + '/chimera/modes', {'mode': 'plan'})
    except RuntimeError as error:
        assert '409 Conflict' in str(error), 'unsupported mode was not explicitly rejected'
    else:
        raise AssertionError('unsupported Claude mode silently accepted')
    outcomes.append('unsupported-mode-explicit-error')
    def settled():
        return snapshot()['status'] == 'idle' and row()['activity'] == ''
    def message(text):
        api('/api/tasks/' + task_id + '/agent', {'prompt': text})
    stream = start_stream()
    control['scenario'] = 'mcp'
    before = len(requests)
    message('CLAUDE-MCP: call the actual scratch task_context tool.')
    wait(lambda: len(requests) > before and 'CLAUDE-MCP-CANARY' in json.dumps(snapshot()) and settled(), 'actual Claude MCP round trip')
    assert any('task_context' in call['name'] and task_id in json.dumps(call.get('result')) for call in snapshot()['toolCalls']), 'MCP did not return the scratch task identity'
    outcomes.append('actual-mcp-task-context-identity')
    def pending(kind):
        return next((approval for approval in api('/api/approvals?task=' + task_id)['approvals'] if approval['source'] == kind), None)
    for decision in ['allow', 'deny']:
        control['scenario'] = 'permission-' + decision
        before = len(requests)
        message('CLAUDE-PERMISSION-' + decision)
        approval = wait(lambda: pending('agent_permission'), 'actual permission request')
        wait(lambda: row()['activity'] == 'waiting', 'permission aggregate waiting')
        api('/api/approvals/' + approval['id'] + '/decision', {'decision': decision})
        wait(lambda: len(requests) >= before + 2 and settled(), 'permission outcome settled')
        assert (root / 'resource' / (control['scenario'] + '.txt')).exists() == (decision == 'allow'), 'permission outcome did not control real execution'
        outcomes.append('real-permission-' + decision + '-ui-payload')
    for decision in ['answer', 'dismiss']:
        control['scenario'] = 'question-' + decision
        before = len(requests)
        message('CLAUDE-QUESTION-' + decision)
        approval = wait(lambda: pending('agent_question'), 'actual question request')
        questions = snapshot()['pendingQuestions'][0]
        wait(lambda: row()['activity'] == 'waiting', 'question aggregate waiting')
        if decision == 'answer':
            items = questions['questions']
            answers = {items[0]['id']: ['Second'], items[1]['id']: ['custom-health-canary']}
            ws_send(stream, {'type': 'agent_question', 'agent_id': task_id, 'request_id': questions['requestId'], 'answers': answers})
        else:
            api('/api/approvals/' + approval['id'] + '/dismiss', {})
        wait(lambda: len(requests) >= before + 2 and settled(), 'question result settled')
        assert not snapshot().get('pendingQuestions'), 'answered question still pending'
        result = json.dumps(requests[-1]['messages'][-1])
        assert ('custom-health-canary' in result) == (decision == 'answer'), 'question answer not delivered to actual model'
        outcomes.append('real-question-' + decision + '-ui-payload')
    control['scenario'] = 'plain'
    for cycle in range(3):
        control['delay'] = 0.5
        before = len(requests)
        message('CLAUDE-LONG-' + str(cycle))
        wait(lambda: len(requests) > before and snapshot()['status'] == 'running', 'active turn for steering')
        markers = ['CLAUDE-QUEUED-' + str(cycle) + '-' + str(index) for index in range(4)]
        with concurrent.futures.ThreadPoolExecutor(max_workers=4) as pool:
            list(pool.map(message, markers))
        wait(lambda: all(marker in json.dumps(requests[-1]['messages']) for marker in markers) and settled(), 'queued markers consumed before clean stop')
        history = json.dumps(snapshot()['messages'])
        assert all(history.count(marker) == 1 for marker in markers), 'native queued message duplication/loss'
        assert all(json.dumps(requests[-1]['messages']).count(marker) == 1 for marker in markers), 'model queued message duplication/loss'
    control['delay'] = 0
    outcomes.append('three-cycles-four-concurrent-clean-stop-messages')
    observed = []
    for cycle in range(3):
        ids = [event['eventId'] for event in stream['events'] if event['type'] == 'event']
        assert ids, 'native stream never received events'
        observed.extend(ids)
        close_stream(stream)
        marker = 'CLAUDE-REPLAY-' + str(cycle)
        before = len(requests)
        message(marker)
        wait(lambda: len(requests) > before and marker in json.dumps(snapshot()['messages']) and settled(), 'disconnected turn complete')
        stream = start_stream(ids[-1])
        wait(lambda: any(event.get('marker') == marker for event in stream['events']), 'replayed user turn')
    observed.extend(event['eventId'] for event in stream['events'] if event['type'] == 'event')
    assert len(observed) == len(set(observed)), 'reconnect duplicated native events'
    outcomes.append('three-live-reconnect-replays-no-duplicate-events')
    control['delay'] = 3
    before = len(requests)
    message('CLAUDE-INTERRUPT-CANARY')
    wait(lambda: len(requests) > before and snapshot()['status'] == 'running', 'interrupt active turn')
    api('/api/agents/' + task_id + '/chimera/interrupt', {})
    control['delay'] = 0
    wait(settled, 'interrupt snapshot and aggregate idle')
    outcomes.append('actual-interrupt-native-and-aggregate-idle')
    old_execution = row()['execution_id']
    before = len(requests)
    api('/api/agents/' + task_id + '/restart', {})
    wait(lambda: row()['execution_id'] != old_execution and row()['has_chimera_endpoint'], 'reload new execution native binding')
    wait(lambda: len(requests) > before and settled(), 'resumed reload notice consumed')
    history = json.dumps(snapshot()['messages'])
    assert all('CLAUDE-QUEUED-0-' + str(index) in history for index in range(4)), 'reload resumed the wrong/empty conversation'
    event_count = len(stream['events'])
    message('CLAUDE-RELOAD-CANARY')
    wait(lambda: any(event.get('marker') == 'CLAUDE-RELOAD-CANARY' for event in stream['events'][event_count:]) and settled(), 'persistent viewer follows reloaded Claude')
    control['scenario'] = 'mcp'
    before = len(requests)
    message('CLAUDE-MCP-RELOAD: call task_context again')
    wait(lambda: len(requests) > before and any('task_context' in call['name'] and task_id in json.dumps(call.get('result')) for call in snapshot()['toolCalls']) and settled(), 'resumed MCP available')
    outcomes.append('reload-resume-history-stream-mcp')
    for mode in ['background', 'stop']:
        control.update(scenario='child-' + mode, child_started=False, child_delay=8 if mode == 'stop' else 2, stop_id=None, stop_phase=0)
        before = len(requests)
        message('CLAUDE-CHILD-' + mode)
        def child_record():
            return next((call['subagent'] for call in snapshot()['toolCalls'] if call.get('subagent', {}).get('purpose') == 'Health child-' + mode), None)
        child = wait(child_record, 'real ' + mode + ' child associated with parent')
        assert child['subagentId'] != task_id, 'child took parent identity'
        wait(lambda: snapshot()['status'] == 'idle' and row()['activity'] == 'background', 'parent idle while background child runs')
        launched = next(call for call in snapshot()['toolCalls'] if call.get('subagent', {}).get('subagentId') == child['subagentId'])
        assert launched['subagent']['status'] == 'running' and launched.get('result') and launched['result'].get('reason') is None, 'async launch reported child stopped before completion'
        if mode == 'stop':
            control['stop_id'] = child['subagentId']
            message('CLAUDE-STOP-CHILD')
        wait(lambda: child_record().get('status') != 'running' and settled(), 'child completion native and aggregate idle')
        if mode == 'stop':
            assert child_record()['reason'] == 'interrupted' and child_record()['status'] == 'interrupted', 'stopped child not reported as canonical interrupted'
        else:
            assert 'CLAUDE-CHILD-RESULT-CANARY' in json.dumps(requests[before:]), 'real child result never reached parent model'
        outcomes.append('real-' + mode + '-child-lifecycle-result-identity')
    control['scenario'] = 'api-error'
    message('CLAUDE-API-ERROR-CANARY')
    wait(lambda: 'CLAUDE-NONSECRET-API-ERROR-CANARY' in json.dumps(snapshot()) and snapshot()['status'] == 'idle' and row()['activity'] == 'error', 'provider error visible and no stuck working')
    wait(lambda: any(event.get('errorCanary') for event in stream['events']), 'provider error reaches live panel')
    outcomes.append('real-provider-error-visible-and-aggregate-error')
    control['scenario'] = 'plain'
    before = len(requests)
    message('CLAUDE-AFTER-ERROR-CANARY')
    wait(lambda: len(requests) > before and settled(), 'recovered provider next turn')
    outcomes.append('next-turn-after-provider-error')
    print(json.dumps({'agent': 'claude-code', 'passed': outcomes, 'model_calls': len(requests)}))
except Exception as error:
    (artifacts / 'provider-shape.json').write_text(diagnostic(json.dumps([{'model': request.get('model'), 'latest': json.dumps(request.get('messages', [])[-1:])[:1200], 'agent_properties': list(next((tool for tool in request.get('tools', []) if tool.get('name') == 'Agent'), {}).get('input_schema', {}).get('properties', {}))} for request in requests[-5:]])))
    (artifacts / 'scratch-native-error.log').write_text(diagnostic((root / 'serve.log').read_text()))
    (artifacts / 'launch-probe.json').write_text(json.dumps({'wrapper_called': (root / 'wrapper-called').exists(), 'model_calls': len(requests)}))
    if (root / 'claude-debug.log').exists():
        lines = (root / 'claude-debug.log').read_text().splitlines()
        (artifacts / 'plugin-diagnostics.log').write_text(diagnostic('\n'.join(line for line in lines if any(word in line.lower() for word in ['legato', 'session.start', 'plugin.register', 'process.spawn']))))
    if task_id and token:
        (artifacts / 'conversation-diagnostic.json').write_text(diagnostic(json.dumps(api('/api/agents/' + task_id + '/conversation'))))
        (artifacts / 'snapshot-diagnostic.json').write_text(diagnostic(json.dumps(snapshot())))
        (artifacts / 'aggregate-diagnostic.json').write_text(diagnostic(json.dumps(row())))
    sessions = subprocess.run(['/usr/bin/tmux', 'list-panes', '-a', '-F', '#{pane_id}'], env=env, capture_output=True, text=True)
    panes = []
    for pane in sessions.stdout.splitlines():
        result = subprocess.run(['/usr/bin/tmux', 'capture-pane', '-p', '-t', pane], env=env, capture_output=True, text=True)
        panes.append(diagnostic(result.stdout))
    (artifacts / 'scratch-native-pane.txt').write_text('\n'.join(panes))
    raise RuntimeError(diagnostic(str(error))) from None
finally:
    for stream in streams:
        close_stream(stream)
    if process and process.poll() is None:
        process.terminate()
        try:
            process.wait(timeout=10)
        except subprocess.TimeoutExpired:
            process.kill()
            process.wait(timeout=5)
    subprocess.run(['/usr/bin/tmux', 'kill-server'], env=env, capture_output=True, timeout=5)
    if (root / 'claude-pids').exists():
        for pid in (root / 'claude-pids').read_text().splitlines():
            deadline = time.monotonic() + 8
            while time.monotonic() < deadline:
                try:
                    state = pathlib.Path('/proc', pid, 'stat').read_text().split(') ', 1)[1].split()[0]
                except FileNotFoundError:
                    break
                if state == 'Z':
                    break
                time.sleep(0.05)
            else:
                raise RuntimeError('scratch Claude did not exit; preserving its private root')
    time.sleep(0.2)
    model.shutdown()
    model.server_close()
    log.close()
    shutil.rmtree(root)
