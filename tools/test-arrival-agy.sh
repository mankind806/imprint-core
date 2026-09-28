#!/bin/sh
# Deterministic parser fixtures for Antigravity arrival probe.
# No account, model calls or arrival claims.
set -eu
ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
python3 - "$ROOT" <<'PY'
import json, os, pathlib, subprocess, sys, tempfile
root = pathlib.Path(sys.argv[1])
card = (root / 'hooks/kernkarte.md').read_text().rstrip('\n')
skills = [p.name for p in sorted((root / 'skills').iterdir()) if (p / 'SKILL.md').is_file()]
report = {'card': card, 'skills': skills}
def events(value, status='SUCCESS'):
    return json.dumps({
        'conversation_id': 'fixture-conv',
        'status': status,
        'response': json.dumps(value) if isinstance(value, dict) else str(value),
        'duration_seconds': 1.0,
        'num_turns': 1
    })

cases = [
    ('matching report', events(report), 0),
    ('matching report with imprint prefixes', events({'card': card, 'skills': ['imprint:' + s for s in skills]}), 0),
    ('markdown fenced response', events('```json\n' + json.dumps(report) + '\n```'), 0),
    ('stale card', events(dict(report, card=card.replace('Every rule', 'Each rule'))), 1),
    ('missing skill', events(dict(report, skills=skills[:-1])), 1),
    ('no arrival', events({'card': '', 'skills': []}), 1),
    ('wrong result shape', events({'card': card, 'skills': 'knowledge-keeping'}), 2),
    ('unknown keys', events(dict(report, claimed_pass=True)), 2),
    ('non-json response', json.dumps({'status': 'SUCCESS', 'response': 'PASS'}), 2),
    ('malformed json', '{bad json\n', 2),
    ('duplicate skills', events(dict(report, skills=skills + skills[:1])), 2),
    ('extra skill', events(dict(report, skills=skills + ['unexpected-skill'])), 1),
    ('CLI failure status', events(report, status='ERROR'), 2),
    ('tool call detected', json.dumps({'status': 'SUCCESS', 'num_turns': 1, 'response': json.dumps(report), 'tool_calls': [{'name': 'view_file'}]}), 2),
    ('outer tool call in wrapper detected', json.dumps({'event': 'result', 'tool_calls': [{'name': 'view_file'}], 'result': {'status': 'SUCCESS', 'num_turns': 1, 'response': json.dumps(report)}}), 2),
    ('multiple turns detected', json.dumps({'status': 'SUCCESS', 'num_turns': 2, 'response': json.dumps(report)}), 2),
    ('boolean num_turns rejected', json.dumps({'status': 'SUCCESS', 'num_turns': True, 'response': json.dumps(report)}), 2),
    ('outer num_turns multiple detected', json.dumps({'event': 'result', 'num_turns': 2, 'result': {'status': 'SUCCESS', 'num_turns': 1, 'response': json.dumps(report)}}), 2),
    ('private status token not leaked', json.dumps({'status': {'detail': 'PRIVATE_STATUS_TOKEN'}, 'response': ''}), 2),
    ('empty output', '', 2),
]

with tempfile.TemporaryDirectory() as tmp:
    fixture = pathlib.Path(tmp) / 'events.json'
    for name, data, expected in cases:
        fixture.write_text(data)
        result = subprocess.run(['sh', str(root / 'tools/arrival-test-agy.sh'), '--events', str(fixture)],
                                capture_output=True, text=True)
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
    capture = pathlib.Path(tmp) / 'args.json'
    stub = bindir / 'agy'
    stub.write_text('#!' + sys.executable + '\n' + '''import json, os, pathlib, sys
if sys.argv[1:] == ['--version']:
    print('1.2.12'); sys.exit(0)
pathlib.Path(os.environ['CAPTURE']).write_text(json.dumps({'args': sys.argv[1:], 'cwd': os.getcwd()}))
print('PRIVATE_STDERR_TOKEN', file=sys.stderr)
print(pathlib.Path(os.environ['FIXTURE']).read_text(), end='')
sys.exit(int(os.environ.get('STUB_EXIT', '0')))
''')
    stub.chmod(0o755)

    timer = bindir / 'timeout'
    timer.write_text('#!/bin/sh\n[ "${STUB_TIMEOUT:-0}" = 0 ] || exit 124\nshift\nexec "$@"\n')
    timer.chmod(0o755)

    fixture.write_text(events(report))
    env = dict(os.environ, PATH=str(bindir) + os.pathsep + os.environ['PATH'],
               CAPTURE=str(capture), FIXTURE=str(fixture))
    script = str(root / 'tools/arrival-test-agy.sh')

    def run_live(*args, **overrides):
        return subprocess.run(['sh', script, *args], env=dict(env, **overrides),
                              capture_output=True, text=True)

    live = run_live()
    if live.returncode != 0:
        raise AssertionError(f'expected exit 0, got {live.returncode}\n{live.stderr}')
    if 'PASS (model report)' not in live.stdout:
        raise AssertionError("expected 'PASS (model report)' in live.stdout")
    if 'PRIVATE_' in live.stdout + live.stderr:
        raise AssertionError("PRIVATE_ token must not leak into stdout/stderr")

    cap_data = json.loads(capture.read_text())
    args = cap_data['args']
    if '-p' not in args or '--output-format' not in args or args[args.index('--output-format') + 1] != 'json':
        raise AssertionError('agy must be invoked with -p and --output-format json')
    if card in args[args.index('-p') + 1] or any(skill in args[args.index('-p') + 1] for skill in skills):
        raise AssertionError('card and skills must not be leaked into prompt')
    cwd = pathlib.Path(cap_data['cwd'])
    if cwd == root or cwd.exists():
        raise AssertionError('neutral temporary cwd must be cleaned up after execution')

    retained = pathlib.Path(tmp) / 'diagnostics'
    failed = run_live('--diagnostics-dir', str(retained), STUB_EXIT='7')
    if failed.returncode != 2 or 'CLI exit 7; cause not established' not in failed.stderr:
        raise AssertionError(f'CLI failure must return code 2 and expected message, got {failed.returncode}: {failed.stderr}')
    if 'PRIVATE_' in failed.stdout + failed.stderr:
        raise AssertionError('PRIVATE_ token must not leak')
    if (retained.stat().st_mode & 0o777) != 0o700:
        raise AssertionError('diagnostics directory must have mode 0700')
    if (retained / 'stderr').read_text().strip() != 'PRIVATE_STDERR_TOKEN':
        raise AssertionError('diagnostics stderr mismatch')
    if (retained / 'events.json').read_text() != fixture.read_text():
        raise AssertionError('diagnostics events mismatch')
    print('PASS: live stub arguments, no expectation leakage, private diagnostics retention')

    timed = run_live(STUB_TIMEOUT='1')
    if timed.returncode != 2 or 'timeout after 180 seconds' not in timed.stderr:
        raise AssertionError('timeout must be reported as exit 2 with timeout message')
    print('PASS: timeout classification')

    help_result = run_live('--help')
    if help_result.returncode != 0 or 'set -eu' in help_result.stdout:
        raise AssertionError('help must exit 0 and not leak script implementation')
    if run_live('--events').returncode != 2:
        raise AssertionError('--events without argument must exit 2')
    if run_live('--events', str(fixture), '--diagnostics-dir', str(retained)).returncode != 2:
        raise AssertionError('--diagnostics-dir with --events must exit 2')
    print('PASS: help and option handling')
PY
