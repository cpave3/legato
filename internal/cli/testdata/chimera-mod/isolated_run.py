#!/usr/bin/env python3
"""Run a test command and its children with no launching agent identity/config.

Acquire the shared heavy-build lock BEFORE this runner changes HOME:
  flock "$HOME/.nuget/packages/.legato-heavy.lock" python3 isolated_run.py go test ...
Only ordinary toolchain/cache locations and explicit nonsecret test gates survive.
"""
import json
import os
import pathlib
import shutil
import subprocess
import sys
import tempfile

if len(sys.argv) < 2:
    raise SystemExit('usage: isolated_run.py <command> [args...]')
command = sys.argv[1:]
tools = {}
tools['python3'] = sys.executable
for name in ('go', 'node', 'pnpm', 'bun'):
    found = shutil.which(name)
    if not found:
        continue
    if pathlib.Path(found).parent.name == 'shims' and shutil.which('mise'):
        resolved = subprocess.run(['mise', 'which', name], capture_output=True, text=True, timeout=10)
        if resolved.returncode:
            raise SystemExit('cannot resolve configured test toolchain: ' + name)
        found = resolved.stdout.strip()
    tools[name] = found
if command[0] in tools:
    command[0] = tools[command[0]]
allowed = ('PATH', 'LANG', 'LC_ALL', 'TERM', 'TZ', 'CI', 'TMPDIR',
           'CHIMERA_TEST_DOCKER', 'CHIMERA_TEST_TMUX', 'CHIMERA_TEST_E2E',
           'CHIMERA_TEST_MOD_BINARY', 'CHIMERA_TEST_RELEASE_BIN', 'CHIMERA_TEST_SANDBOX_IMAGE')
env = {key: os.environ[key] for key in allowed if key in os.environ}
env['PATH'] = os.pathsep.join(dict.fromkeys(str(pathlib.Path(path).parent) for path in tools.values())) + os.pathsep + os.environ.get('PATH', '')
if 'go' in tools:
    cache = subprocess.run([tools['go'], 'env', '-json', 'GOCACHE', 'GOPATH', 'GOMODCACHE', 'GOROOT'],
                           capture_output=True, text=True, check=True, timeout=10)
    env.update(json.loads(cache.stdout))
with tempfile.TemporaryDirectory(prefix='integration-test-env-') as directory:
    for key, name in {'HOME': 'home', 'XDG_CONFIG_HOME': 'config', 'XDG_DATA_HOME': 'data',
                      'XDG_CACHE_HOME': 'cache', 'XDG_RUNTIME_DIR': 'run',
                      'TMUX_TMPDIR': 'tmux', 'LEGATO_HOME': 'legato'}.items():
        folder = pathlib.Path(directory) / name
        folder.mkdir(mode=0o700, exist_ok=True)
        env[key] = str(folder)
    (pathlib.Path(env['HOME']) / '.zshrc').write_text('')
    result = subprocess.run(command, env=env)
    raise SystemExit(result.returncode)
