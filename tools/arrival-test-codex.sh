#!/bin/sh
# Installed Codex context probe; local CLI help verified with 0.157.1 (2026-09-28).
# Usage: tools/arrival-test-codex.sh [--events <JSONL fixture>]
# Live mode costs one model call and needs a logged-in Codex CLI plus timeout.
# Expected card and skill names stay here: they are never supplied to the model.
# A match is model-reported context arrival, NOT proof of hook execution, skill
# body loading, subagent inheritance, or adherence. Those remain not checked.
# --events validates saved/fixture output only; it never claims live arrival.
# No --plugin-dir: tests installed Codex configuration without modifying it.
# Exit 0: matching report; 1: valid report differs; 2: invalid/unavailable probe.
set -eu
ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
mode=live
events=
case $# in
  0) ;;
  1) case "$1" in -h|--help) sed -n '2,11p' "$0"; exit 0;; *) echo 'arrival-test-codex: invalid arguments' >&2; exit 2;; esac ;;
  2) [ "$1" = --events ] || { echo 'arrival-test-codex: expected --events <file>' >&2; exit 2; }
     mode=fixture; events=$2 ;;
  *) echo 'arrival-test-codex: invalid arguments' >&2; exit 2 ;;
esac
command -v python3 >/dev/null 2>&1 || { echo 'arrival-test-codex: python3 unavailable' >&2; exit 2; }
if [ "$mode" = live ]; then
  for dependency in codex timeout; do
    command -v "$dependency" >/dev/null 2>&1 || { echo "arrival-test-codex: $dependency unavailable" >&2; exit 2; }
  done
  work=$(mktemp -d)
  trap 'rm -rf "$work"' EXIT
  trap 'exit 2' INT TERM
  events="$work/events.jsonl"
  version=$(codex --version 2>/dev/null) || { echo 'arrival-test-codex: cannot read CLI version' >&2; exit 2; }
  printf 'CLI: %s\n' "$version"
  # A neutral cwd excludes this checkout's instructions. Keep the user's plugin
  # configuration and trust settings; never bypass hook trust or approvals.
  # Tool use is forbidden by prompt AND rejected by the event validator below.
  # Read-only sandbox limits shell writes; it does not disable configured MCPs.
  prompt='This is a context arrival observation, not a work task. Do not use any tools, search, filesystem access, or subagents. From the instructions already in your context, return only a JSON object with exactly two keys: "card", the complete verbatim imprint core card text (empty string if absent); and "skills", an array containing the exact catalog names of all available imprint skills (empty array if absent). Do not infer missing content. No code fences or commentary.'
  if timeout 180 codex exec --cd "$work" --skip-git-repo-check --ephemeral \
       --sandbox read-only --json "$prompt" >"$events" 2>"$work/stderr" </dev/null; then :
  else
    status=$?
    echo "arrival-test-codex: unavailable (CLI/timeout exit $status); inspect local CLI auth, network and trust settings" >&2
    exit 2
  fi
fi
python3 - "$ROOT" "$events" "$mode" <<'PY'
import datetime, json, pathlib, sys
root, source, mode = pathlib.Path(sys.argv[1]), pathlib.Path(sys.argv[2]), sys.argv[3]
def unavailable(reason):
    print('arrival-test-codex: invalid/unavailable — ' + reason, file=sys.stderr)
    sys.exit(2)
def object_only(pairs):
    value = {}
    for key, item in pairs:
        if key in value:
            raise ValueError('duplicate JSON key')
        value[key] = item
    return value
def parse(text):
    return json.loads(text, object_pairs_hook=object_only)
try:
    stream = [parse(line) for line in source.read_text().splitlines() if line.strip()]
    expected = (root / 'hooks/kernkarte.md').read_text().rstrip('\n')
    skills = sorted('imprint:' + p.name for p in (root / 'skills').iterdir() if (p / 'SKILL.md').is_file())
except (OSError, UnicodeError, ValueError):
    unavailable('cannot read valid JSONL and local expectations')
if not expected or not skills:
    unavailable('empty local expectations')
reports, completed, started, thread = [], False, False, False
for event in stream:
    if not isinstance(event, dict):
        unavailable('event must be an object')
    kind = event.get('type')
    if completed:
        unavailable('event after completed turn')
    if kind == 'thread.started':
        if thread or started:
            unavailable('unexpected thread start')
        thread = True
    elif kind == 'turn.started':
        if not thread or started:
            unavailable('unexpected turn start')
        started = True
    elif kind == 'turn.completed':
        if not started:
            unavailable('completed turn without start')
        completed = True
    elif kind in ('item.started', 'item.updated', 'item.completed'):
        item = event.get('item')
        if isinstance(item, dict) and item.get('type') == 'error':
            unavailable('CLI runtime reported an error; check configured services')
        if not started or not isinstance(item, dict):
            unavailable('malformed item')
        if item.get('type') not in ('agent_message', 'reasoning'):
            unavailable('tool use or unsupported item; no arrival claim')
        if kind == 'item.completed' and item.get('type') == 'agent_message':
            reports.append(item.get('text'))
    else:
        unavailable('failed or unsupported event; no arrival claim')
if not completed or len(reports) != 1 or not isinstance(reports[0], str):
    unavailable('need one final report and a completed turn')
try:
    report = parse(reports[0])
except ValueError:
    unavailable('final report is not JSON')
if (not isinstance(report, dict) or set(report) != {'card', 'skills'}
    or not isinstance(report['card'], str) or not isinstance(report['skills'], list)
    or any(not isinstance(s, str) for s in report['skills'])
    or len(report['skills']) != len(set(report['skills']))):
    unavailable('final report has wrong shape')
card_ok = report['card'].rstrip('\n') == expected
skills_ok = sorted(report['skills']) == skills
print('Checked UTC:', datetime.datetime.now(datetime.timezone.utc).isoformat())
print('Mode:', 'fixture validation; live arrival not checked' if mode == 'fixture' else 'live model-reported context')
print('Card:', 'match' if card_ok else 'mismatch')
print('Skill catalog:', 'match' if skills_ok else 'mismatch')
print('Hook execution, skill bodies, subagents and adherence: not checked')
print('arrival-test-codex:', 'PASS' if card_ok and skills_ok else 'FAIL')
sys.exit(0 if card_ok and skills_ok else 1)
PY
