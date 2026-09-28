#!/bin/sh
# Deterministic parser fixtures. No account, model calls or arrival claims.
set -eu
ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
python3 - "$ROOT" <<'PY'
import json, os, pathlib, subprocess, sys, tempfile
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
    ('nonfatal warning before turn', events(report)[:1] + [{'type':'item.completed','item':{'type':'error','message':'fixture warning'}}] + events(report)[1:], 0),
    ('extra skill', events(dict(report, skills=skills+['imprint:unexpected'])), 1),
    ('fatal top-level error', events(report)[:-1]+[{'type':'error','message':'fatal'}], 2),
    ('MCP retrieval', events(report)[:2]+[{'type':'item.completed','item':{'type':'mcp_tool_call'}}]+events(report)[2:], 2),
    ('collaboration retrieval', events(report)[:2]+[{'type':'item.completed','item':{'type':'collab_tool_call'}}]+events(report)[2:], 2),
    ('reasoning then report', events(report)[:2]+[{'type':'item.completed','item':{'type':'reasoning','text':'thinking'}}]+events(report)[2:], 0),
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
        if name == 'no arrival' and 'Card: absent' not in result.stdout:
            sys.exit('missing card must be distinguished from stale card')
        if name == 'stale card' and 'Card: differs' not in result.stdout:
            sys.exit('stale card must be classified as differing')
        print('PASS:', name)
    # Complete live branch with stub executables: no account or paid call.
    bindir = pathlib.Path(tmp) / 'bin'
    bindir.mkdir()
    fixture.write_text('\n'.join(map(json.dumps, events(report)[:1] + [
        {'type':'item.completed','item':{'type':'error','message':'PRIVATE_WARNING_TOKEN'}}
    ] + events(report)[1:])) + '\n')
    capture = pathlib.Path(tmp) / 'args.json'
    stub = bindir / 'codex'
    stub.write_text('#!' + sys.executable + '\n' + '''import json, os, pathlib, sys
if sys.argv[1:] == ['--version']:
    print('codex-cli fixture'); sys.exit(0)
pathlib.Path(os.environ['CAPTURE']).write_text(json.dumps(sys.argv[1:]))
print('PRIVATE_STDERR_TOKEN', file=sys.stderr)
print(pathlib.Path(os.environ['FIXTURE']).read_text(), end='')
sys.exit(int(os.environ.get('STUB_EXIT', '0')))
''')
    stub.chmod(0o755)
    timer = bindir / 'timeout'
    timer.write_text('#!/bin/sh\n[ "${STUB_TIMEOUT:-0}" = 0 ] || exit 124\nshift\nexec "$@"\n')
    timer.chmod(0o755)
    env = dict(os.environ, PATH=str(bindir)+os.pathsep+os.environ['PATH'],
               CAPTURE=str(capture), FIXTURE=str(fixture))
    script = str(root / 'tools/arrival-test-codex.sh')
    def run_live(*args, **overrides):
        return subprocess.run(['sh', script, *args], env=dict(env, **overrides),
                              capture_output=True, text=True)
    live = run_live()
    assert live.returncode == 0, live.stderr
    assert 'PASS (model report)' in live.stdout
    assert 'PRIVATE_' not in live.stdout + live.stderr
    args = json.loads(capture.read_text())
    assert args[0] == 'exec' and args[args.index('--sandbox')+1] == 'read-only'
    assert '--skip-git-repo-check' in args and '--ephemeral' in args and '--json' in args
    assert not any('dangerously' in a for a in args)
    assert card not in args[-1] and not any(skill in args[-1] for skill in skills)
    cwd = pathlib.Path(args[args.index('--cd')+1])
    assert cwd != root and not cwd.exists(), 'neutral temporary cwd must be cleaned'
    print('PASS: live stub arguments, no expectation leakage, warning, cleanup')
    retained = pathlib.Path(tmp) / 'diagnostics'
    failed = run_live('--diagnostics-dir', str(retained), STUB_EXIT='7')
    assert failed.returncode == 2 and 'CLI exit 7; cause not established' in failed.stderr
    assert 'PRIVATE_' not in failed.stdout + failed.stderr
    assert retained.stat().st_mode & 0o777 == 0o700
    assert (retained/'stderr').read_text().strip() == 'PRIVATE_STDERR_TOKEN'
    assert (retained/'events.jsonl').read_text() == fixture.read_text()
    assert (retained/'stderr').stat().st_mode & 0o777 == 0o600
    assert run_live('--diagnostics-dir', str(retained)).returncode == 2
    print('PASS: CLI failure preserves private evidence only on explicit request')
    timed = run_live(STUB_TIMEOUT='1')
    assert timed.returncode == 2 and 'timeout after 180 seconds' in timed.stderr
    print('PASS: timeout classification')
    help_result = run_live('--help')
    assert help_result.returncode == 0 and 'set -eu' not in help_result.stdout
    assert run_live('--events').returncode == 2
    assert run_live('--events', str(fixture), '--diagnostics-dir', str(retained)).returncode == 2
    print('PASS: help and invalid option handling')
PY
