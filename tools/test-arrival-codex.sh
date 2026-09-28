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
if sys.argv[1:] == ['login', 'status']:
    print('PRIVATE_LOGIN_TOKEN'); print('PRIVATE_LOGIN_TOKEN', file=sys.stderr)
    sys.exit(int(os.environ.get('STUB_LOGIN_EXIT', '0')))
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
    for login_exit in ['1', '7']:
        no_login = run_live(STUB_LOGIN_EXIT=login_exit)
        if no_login.returncode != 2 or 'not logged in (or login status unreadable)' not in no_login.stderr:
            raise AssertionError('failed login status must stop live execution explicitly')
        if capture.exists() or 'PRIVATE_' in no_login.stdout + no_login.stderr:
            raise AssertionError('failed login status must not execute or expose credentials')
    print('PASS: missing/unreadable login status stops execution without credential output')
    live = run_live()
    if not (live.returncode == 0):
        raise AssertionError(live.stderr)
    if not ('PASS (model report)' in live.stdout):
        raise AssertionError("test assertion failed: 'PASS (model report)' in live.stdout")
    if not ('PRIVATE_' not in live.stdout + live.stderr):
        raise AssertionError("test assertion failed: 'PRIVATE_' not in live.stdout + live.stderr")
    args = json.loads(capture.read_text())
    if not (args[0] == 'exec' and args[args.index('--sandbox') + 1] == 'read-only'):
        raise AssertionError("test assertion failed: args[0] == 'exec' and args[args.index('--sandbox') + 1] == 'read-only'")
    if not ('--skip-git-repo-check' in args and '--ephemeral' in args and ('--json' in args)):
        raise AssertionError("test assertion failed: '--skip-git-repo-check' in args and '--ephemeral' in args and ('--json' in args)")
    if not (not any(('dangerously' in a for a in args))):
        raise AssertionError("test assertion failed: not any(('dangerously' in a for a in args))")
    if not (card not in args[-1] and (not any((skill in args[-1] for skill in skills)))):
        raise AssertionError('test assertion failed: card not in args[-1] and (not any((skill in args[-1] for skill in skills)))')
    cwd = pathlib.Path(args[args.index('--cd')+1])
    if not (cwd != root and (not cwd.exists())):
        raise AssertionError('neutral temporary cwd must be cleaned')
    print('PASS: live stub arguments, no expectation leakage, warning, cleanup')
    retained = pathlib.Path(tmp) / 'diagnostics'
    failed = run_live('--diagnostics-dir', str(retained), STUB_EXIT='7')
    if not (failed.returncode == 2 and 'CLI exit 7; cause not established' in failed.stderr):
        raise AssertionError("test assertion failed: failed.returncode == 2 and 'CLI exit 7; cause not established' in failed.stderr")
    if not ('PRIVATE_' not in failed.stdout + failed.stderr):
        raise AssertionError("test assertion failed: 'PRIVATE_' not in failed.stdout + failed.stderr")
    if not (retained.stat().st_mode & 511 == 448):
        raise AssertionError('test assertion failed: retained.stat().st_mode & 511 == 448')
    if not ((retained / 'stderr').read_text().strip() == 'PRIVATE_STDERR_TOKEN'):
        raise AssertionError("test assertion failed: (retained / 'stderr').read_text().strip() == 'PRIVATE_STDERR_TOKEN'")
    if not ((retained / 'events.jsonl').read_text() == fixture.read_text()):
        raise AssertionError("test assertion failed: (retained / 'events.jsonl').read_text() == fixture.read_text()")
    if not ((retained / 'stderr').stat().st_mode & 511 == 384):
        raise AssertionError("test assertion failed: (retained / 'stderr').stat().st_mode & 511 == 384")
    if not (run_live('--diagnostics-dir', str(retained)).returncode == 2):
        raise AssertionError("test assertion failed: run_live('--diagnostics-dir', str(retained)).returncode == 2")
    print('PASS: CLI failure preserves private evidence only on explicit request')
    timed = run_live(STUB_TIMEOUT='1')
    if not (timed.returncode == 2 and 'timeout after 180 seconds' in timed.stderr):
        raise AssertionError("test assertion failed: timed.returncode == 2 and 'timeout after 180 seconds' in timed.stderr")
    print('PASS: timeout classification')
    help_result = run_live('--help')
    if not (help_result.returncode == 0 and 'set -eu' not in help_result.stdout):
        raise AssertionError("test assertion failed: help_result.returncode == 0 and 'set -eu' not in help_result.stdout")
    if not (run_live('--events').returncode == 2):
        raise AssertionError("test assertion failed: run_live('--events').returncode == 2")
    if not (run_live('--events', str(fixture), '--diagnostics-dir', str(retained)).returncode == 2):
        raise AssertionError("test assertion failed: run_live('--events', str(fixture), '--diagnostics-dir', str(retained)).returncode == 2")
    print('PASS: help and invalid option handling')
PY
