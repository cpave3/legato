#!/usr/bin/env python3
"""Normalize only captured paths/URLs; expected state observations stay hand-authored."""
import argparse
import hashlib
import json
import pathlib

parser = argparse.ArgumentParser()
parser.add_argument('--raw', required=True)
parser.add_argument('--binary', required=True)
parser.add_argument('--revision', required=True)
args = parser.parse_args()
folder = pathlib.Path(__file__).parent
manifest = json.loads((folder / 'cases.json').read_text())
versions = set()
for case in manifest['cases']:
    events = [json.loads(line) for line in (pathlib.Path(args.raw) / (case['name'] + '.ndjson')).read_text().splitlines()]
    if len(events) != len(case['activities']):
        raise ValueError(case['name'] + ': event count changed; review and update expected observations first')
    for event in events:
        event['cwd'] = '/repo'
        if 'serverUrl' in event:
            event['serverUrl'] = 'http://127.0.0.1:4444/fixture-secret'
            versions.add(event['version'])
    (folder / (case['name'] + '.ndjson')).write_text(''.join(json.dumps(event, separators=(',', ':')) + '\n' for event in events))
if len(versions) != 1:
    raise ValueError('recordings have mixed runtime versions: ' + repr(versions))
manifest['binary_sha256'] = hashlib.sha256(pathlib.Path(args.binary).read_bytes()).hexdigest()
manifest['source_revision'] = args.revision
manifest['version'] = versions.pop()
(folder / 'cases.json').write_text(json.dumps(manifest, indent=2) + '\n')
