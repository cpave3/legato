#!/usr/bin/env python3
"""Capture the production Legato mod in a private Chimera/tmux, using a local model."""
import argparse
import http.server
import json
import os
import pathlib
import shlex
import shutil
import signal
import subprocess
import tempfile
import threading
import time
import urllib.request
parser = argparse.ArgumentParser()
parser.add_argument('--chimera', required=True)
parser.add_argument('--mod', required=True)
parser.add_argument('--out', required=True)
parser.add_argument('--case', default='basic', choices=['basic', 'permission', 'question', 'background', 'subagent', 'error', 'max-steps', 'output-limit', 'interrupt', 'switch', 'ctrl-c', 'signal', 'hup', 'resume', 'waiting-input'])
a = parser.parse_args()
out = pathlib.Path(a.out).resolve()
out.mkdir(parents=True, exist_ok=True)
root = pathlib.Path(tempfile.mkdtemp(prefix='leg180-capture-'))
home = root / 'home'
work = root / 'work'
home.mkdir()
work.mkdir()
(home / '.chimera').mkdir()
record = out / (a.case + '.ndjson')
record.write_text('')
pathlib.Path(str(record) + '.helper-lifecycle').write_text('')
requests = []
counts = {}
main_id = 'fixture-' + a.case

def text_of(value):
    if isinstance(value, str):
        return value
    if isinstance(value, list):
        return ' '.join((text_of(x) for x in value))
    if isinstance(value, dict):
        return str(value.get('text', ''))
    return ''

class Provider(http.server.BaseHTTPRequestHandler):

    def log_message(self, *_):
        pass

    def do_GET(self):
        body = json.dumps({'object': 'list', 'data': [{'id': 'fixture-model', 'object': 'model', 'owned_by': 'fixture'}]}).encode()
        self.send_response(200)
        self.send_header('Content-Type', 'application/json')
        self.end_headers()
        self.wfile.write(body)

    def do_POST(self):
        body = json.loads(self.rfile.read(int(self.headers['Content-Length'])))
        requests.append(body)
        sid = self.headers.get('X-Fixture-Session', 'main')
        n = counts.get(sid, 0)
        counts[sid] = n + 1
        last_user = next((text_of(m.get('content')) for m in reversed(body.get('messages', [])) if m['role'] == 'user'), '')
        tool = None
        if a.case == 'error':
            self.send_response(400)
            self.send_header('Content-Type', 'application/json')
            self.end_headers()
            self.wfile.write(b'{"error":{"message":"fixture model failure","type":"fixture_error"}}')
            return
        if a.case == 'interrupt':
            time.sleep(4)
        if n == 0:
            if a.case in ('permission', 'max-steps'):
                tool = ('bash', {'command': 'printf permission-fixture'})
            elif a.case == 'background':
                tool = ('bash', {'command': 'sleep 2; printf background-fixture', 'run_in_background': True})
            elif a.case == 'waiting-input':
                tool = ('request_user_input', {'request': 'Fixture input required', 'kind': 'information'})
            elif a.case == 'question':
                tool = ('ask_question', {'questions': [{'question': 'Fixture choice?', 'header': 'Fixture', 'options': [{'label': 'First'}, {'label': 'Second'}]}]})
            elif a.case == 'subagent' and sid == main_id:
                tool = ('spawn_agent', {'prompt': 'fixture-child', 'purpose': 'fixture-child', 'background': True, 'model': 'fixture/fixture-model'})
        if a.case == 'max-steps':
            tool = ('bash', {'command': 'printf capped-fixture'})
        if a.case == 'subagent' and sid != main_id:
            time.sleep(1)
        delta = {'role': 'assistant', 'content': 'fixture response'}
        reason = 'length' if a.case == 'output-limit' else 'stop'
        if tool:
            delta = {'role': 'assistant', 'tool_calls': [{'index': 0, 'id': 'fixture-call', 'type': 'function', 'function': {'name': tool[0], 'arguments': json.dumps(tool[1])}}]}
            reason = 'tool_calls'
        chunk = {'id': 'fixture-completion', 'object': 'chat.completion.chunk', 'created': 1, 'model': 'fixture-model', 'choices': [{'index': 0, 'delta': delta, 'finish_reason': None}]}
        final = {**chunk, 'choices': [{'index': 0, 'delta': {}, 'finish_reason': reason}], 'usage': {'prompt_tokens': 10, 'completion_tokens': 2, 'total_tokens': 12}}
        self.send_response(200)
        self.send_header('Content-Type', 'text/event-stream')
        self.end_headers()
        try:
            for obj in (chunk, final):
                self.wfile.write(('data: ' + json.dumps(obj) + '\n\n').encode())
                self.wfile.flush()
            self.wfile.write(b'data: [DONE]\n\n')
            self.wfile.flush()
        except (BrokenPipeError, ConnectionResetError):
            pass
server = http.server.ThreadingHTTPServer(('127.0.0.1', 0), Provider)
thread = threading.Thread(target=server.serve_forever, daemon=True)
thread.start()
config = {'defaultModel': 'fixture/fixture-model', 'providers': {'fixture': {'shape': 'openai', 'adapter': 'compatible', 'baseUrl': f'http://127.0.0.1:{server.server_port}/v1', 'apiKey': 'fixture-not-a-secret', 'headers': {'X-Fixture-Session': '{{session.id}}'}}}}
(home / '.chimera/config.json').write_text(json.dumps(config))
wrapper = root / 'capture-link'
wrapper.write_text('#!/bin/sh\nif test "$1 $2 $3" != "agent link chimera"; then exit 2; fi\nexec /bin/cat >> "$LEGATO_CAPTURE_FILE"\n')
wrapper.chmod(448)
if a.case in ('signal', 'hup'):
    wrapper.write_text("#!/usr/bin/python3\nimport os, signal, sys, json, time\npath=os.environ['LEGATO_CAPTURE_FILE']\ndef note(event):\n with open(path+'.helper-lifecycle','a') as log:log.write(json.dumps({'event':event,'at':time.monotonic()})+'\\n')\ndef terminate(*args):note('SIGTERM');sys.exit(0)\nsignal.signal(signal.SIGTERM,terminate)\nnote('started')\nwith open(path,'a',buffering=1) as out:\n for line in sys.stdin:out.write(line)\nnote('EOF')\n")
    wrapper.chmod(448)
socket = str(root / 'tmux.sock')
env = {'PATH': '/usr/local/bin:/usr/bin:/bin', 'HOME': str(home), 'TERM': 'xterm-256color', 'LANG': 'C.UTF-8', 'XDG_CONFIG_HOME': str(home / '.config'), 'XDG_DATA_HOME': str(home / '.local/share'), 'XDG_RUNTIME_DIR': str(root / 'runtime'), 'LEGATO_TASK_ID': 'fixture-task', 'LEGATO_BIN': str(wrapper), 'LEGATO_CAPTURE_FILE': str(record), 'CHIMERA_MOD_DIRS': str(pathlib.Path(a.mod).resolve()), 'CHIMERA_SERVER_SECRET': 'fixture-server-secret'}
(root / 'runtime').mkdir(mode=448)

def tmux(*args, check=True):
    return subprocess.run(['/usr/bin/tmux', '-S', socket, '-f', '/dev/null', *args], env=env, text=True, capture_output=True, check=check)

def events():
    return [json.loads(x) for x in record.read_text().splitlines() if x]

def wait(predicate, label, timeout=20):
    end = time.monotonic() + timeout
    while time.monotonic() < end:
        result = predicate()
        if result:
            return result
        time.sleep(0.03)
    pane = tmux('capture-pane', '-p', '-t', 'fixture', check=False).stdout
    raise RuntimeError(label + ' timed out; events=' + json.dumps(events()) + '\npane=' + pane)

def event(kind):
    return next((x for x in reversed(events()) if x['type'] == kind), None)

def api(path, body=None):
    start = event('session.start')
    url = start['serverUrl'] + '/v1/sessions/' + start['sessionId'] + path
    req = urllib.request.Request(url, data=None if body is None else json.dumps(body).encode(), headers={'Content-Type': 'application/json'})
    try:
        with urllib.request.urlopen(req, timeout=5) as res:
            payload = res.read()
            return json.loads(payload) if payload else None
    except urllib.error.HTTPError as error:
        raise RuntimeError(error.read().decode()) from error

def send(text):
    tmux('send-keys', '-t', 'fixture', '-l', text)
    tmux('send-keys', '-t', 'fixture', 'Enter')
pid = None
try:
    cmd = [str(pathlib.Path(a.chimera).resolve()), '--cwd', str(work), '--session-id', main_id, '--session-name', 'fixture-task', '--no-skills', '--no-modes', '--no-claude-compat', '--auto-approve', 'none' if a.case == 'permission' else 'all', '--prompt', 'fixture-' + a.case]
    if a.case == 'max-steps':
        cmd += ['--max-steps', '1']
    tmux('new-session', '-d', '-s', 'fixture', '-x', '120', '-y', '35', '-c', str(work), 'exec ' + shlex.join(cmd))
    pid = int(tmux('display-message', '-p', '-t', 'fixture', '#{pane_pid}').stdout.strip())
    wait(lambda: event('session.start'), 'session.start')
    if a.case == 'permission':
        wait(lambda: event('permission.request'), 'permission.request')
        snapshot = api('')
        (out / 'permission-snapshot.json').write_text(json.dumps(snapshot, indent=2))
        req = snapshot['pendingPermissions'][0]
        api('/permissions/' + req['requestId'], {'decision': 'allow'})
    if a.case == 'question':
        q = wait(lambda: event('question.request'), 'question.request')
        snapshot = api('')
        (out / 'question-snapshot.json').write_text(json.dumps(snapshot, indent=2))
        api('/questions/' + q['requestId'], {'status': 'answered', 'answers': [{'question': 'Fixture choice?', 'answer': 'First', 'custom': False}]})
    if a.case == 'interrupt':
        wait(lambda: event('turn.start'), 'turn.start')
        wait(lambda: len(requests) > 0, 'provider request')
        time.sleep(0.1)
        api('/interrupt', {})
    wait(lambda: event('turn.complete'), 'turn.complete', 30)
    if a.case == 'background':
        wait(lambda: event('background.exit'), 'background.exit')
    if a.case == 'subagent':
        wait(lambda: event('agent.finish'), 'agent.finish')
    if a.case == 'basic':
        before = len(events())
        send('!printf bang-fixture')
        wait(lambda: len(events()) > before and event('message.append'), 'bang append')
    if a.case == 'switch':
        send('/new')
        wait(lambda: len([e for e in events() if e['type'] == 'session.start']) >= 2, 'new session')
        send('/fork fixture-fork')
        wait(lambda: len([e for e in events() if e['type'] == 'session.start']) >= 3, 'fork session')
        send('/sessions')
        time.sleep(0.2)
        tmux('send-keys', '-t', 'fixture', 'Down', 'Enter')
        wait(lambda: len([e for e in events() if e['type'] == 'session.start']) >= 4, 'session picker switch')
    if a.case == 'ctrl-c':
        tmux('send-keys', '-t', 'fixture', 'C-c')
        time.sleep(0.2)
        tmux('send-keys', '-t', 'fixture', 'C-c')
    elif a.case in ('signal', 'hup'):
        (out / (a.case + '-process.json')).write_text(json.dumps({'pid': pid, 'exe': os.readlink('/proc/' + str(pid) + '/exe'), 'target': 'process PID, not process group'}))
        os.kill(pid, signal.SIGTERM if a.case == 'signal' else signal.SIGHUP)
    else:
        send('/exit')
    wait(lambda: event('session.end') and event('session.end').get('reason') != 'switch', 'session.end')
    if a.case == 'resume':
        (out / 'resume-initial.ndjson').write_text(record.read_text())
        wait(lambda: tmux('has-session', '-t', 'fixture', check=False).returncode != 0, 'first process exit')
        record.write_text('')
        resumed = [str(pathlib.Path(a.chimera).resolve()), '--cwd', str(work), '--session', main_id, '--no-skills', '--no-modes', '--no-claude-compat', '--prompt', 'fixture-resume-again']
        tmux('new-session', '-d', '-s', 'fixture', '-x', '120', '-y', '35', '-c', str(work), 'exec ' + shlex.join(resumed))
        pid = int(tmux('display-message', '-p', '-t', 'fixture', '#{pane_pid}').stdout.strip())
        wait(lambda: event('turn.complete'), 'resumed turn')
        send('/exit')
        wait(lambda: event('session.end'), 'resumed exit')
    wait(lambda: tmux('has-session', '-t', 'fixture', check=False).returncode != 0, 'process exit')
    print(json.dumps({'case': a.case, 'events': [x['type'] for x in events()], 'reason': event('turn.complete').get('reason'), 'record': str(record)}))
finally:
    tmux('kill-server', check=False)
    if pid:
        for _ in range(50):
            try:
                os.kill(pid, 0)
            except ProcessLookupError:
                break
            time.sleep(0.1)
        else:
            os.kill(pid, signal.SIGKILL)
    (out / (a.case + '-requests.json')).write_text(json.dumps(requests, indent=2))
    server.shutdown()
    server.server_close()
    thread.join(2)
    shutil.rmtree(root)
