#!/bin/sh
# Deterministic parser fixtures. No account, model calls or arrival claims.
set -eu
ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
python3 - "$ROOT" <<'PY'
import json, pathlib, subprocess, sys, tempfile
root = pathlib.Path(sys.argv[1])
card = (root / 'hooks/kernkarte.md').read_text().rstrip('\n')
skills = ['imprint:' + p.name for p in sorted((root / 'skills').iterdir()) if (p / 'SKILL.md').is_file()]
report = {'card': card, 'skills': skills}
def events(value):
    return [{'type': 'thread.started', 'thread_id': 'fixture'},
            {'type': 'turn.started'},
            {'type': 'item.completed', 'item': {'type': 'agent_message', 'text': json.dumps(value)}},
            {'type': 'turn.completed', 'usage': {}}]
cases = [
    ('matching report', events(report), 0),
    ('stale card', events(dict(report, card=card.replace('Every rule', 'Each rule'))), 1),
    ('missing skill', events(dict(report, skills=skills[:-1])), 1),
    ('no arrival', events({'card': '', 'skills': []}), 1),
    ('wrong result shape', events({'card': card, 'skills': 'imprint:knowledge-keeping'}), 2),
    ('unknown keys', events(dict(report, claimed_pass=True)), 2),
    ('incomplete stream', events(report)[:-1], 2),
    ('failed turn', events(report)[:-1] + [{'type':'turn.failed','error':{'message':'fixture'}}], 2),
    ('tool retrieval', events(report)[:2] + [{'type':'item.completed','item':{'type':'command_execution','command':'cat card'}}] + events(report)[2:], 2),
    ('two reports', events(report)[:-1] + events(report)[2:], 2),
    ('non-json response', events(report)[:2] + [{'type':'item.completed','item':{'type':'agent_message','text':'PASS'}}] + events(report)[-1:], 2),
    ('malformed stream', '{bad json\n', 2),
    ('non-object event', '[]\n', 2),
    ('duplicate event key', '{"type":"thread.started","type":"turn.started"}\n', 2),
    ('duplicate skills', events(dict(report, skills=skills + skills[:1])), 2),
    ('runtime initialization error', events(report)[:1] + [{'type':'item.completed','item':{'type':'error','message':'fixture runtime failure'}}] + events(report)[1:], 2),
    ('empty stream', '', 2),
]
with tempfile.TemporaryDirectory() as tmp:
    fixture = pathlib.Path(tmp) / 'events.jsonl'
    for name, data, expected in cases:
        fixture.write_text(data if isinstance(data, str) else '\n'.join(map(json.dumps, data)) + '\n')
        result = subprocess.run(['sh', str(root / 'tools/arrival-test-codex.sh'), '--events', str(fixture)], capture_output=True, text=True)
        if result.returncode != expected:
            sys.exit(f'{name}: expected {expected}, got {result.returncode}\n{result.stdout}{result.stderr}')
        if expected == 0 and ('fixture' not in result.stdout.lower() or 'not checked' not in result.stdout.lower()):
            sys.exit('fixture success must not claim live arrival')
        print('PASS:', name)
PY
