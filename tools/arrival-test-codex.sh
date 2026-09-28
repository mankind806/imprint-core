#!/bin/sh
# Installed Codex context probe; local CLI help verified with 0.157.1 (2026-09-28).
# Usage: tools/arrival-test-codex.sh [--events <JSONL fixture>]
#                                  [--diagnostics-dir <new private directory>]
# Live mode costs one model call and needs a logged-in Codex CLI plus timeout.
# Expected card and skill names stay here: they are never supplied to the model.
# A match is model-reported context arrival, NOT proof of hook execution, skill
# body loading, subagent inheritance, or adherence. Those remain not checked.
# --events validates saved/fixture output only; it never claims live arrival.
# Tests installed configuration. Codex can write state/logs and start MCPs/hooks.
# --diagnostics-dir preserves raw stderr and JSONL (may contain private context
# or credentials). Its directory must not exist; it is created with mode 0700.
# No raw diagnostics are printed. Without this option they are removed on exit.
# Exit 0: matching report; 1: valid report absent/differs; 2: unavailable probe.
set -eu
ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
mode=live
events=
diagnostics=
while [ $# -gt 0 ]; do
  case "$1" in
    -h|--help) sed -n '/^# Usage:/,/^set -eu/{ /^#/p; }' "$0"; exit 0 ;;
    --events)
      [ $# -ge 2 ] && [ -n "$2" ] && [ "$mode" = live ] || { echo 'arrival-test-codex: expected one --events <file>' >&2; exit 2; }
      mode=fixture; events=$2; shift 2 ;;
    --diagnostics-dir)
      [ $# -ge 2 ] && [ -n "$2" ] && [ -z "$diagnostics" ] || { echo 'arrival-test-codex: expected one --diagnostics-dir <new directory>' >&2; exit 2; }
      diagnostics=$2; shift 2 ;;
    *) echo 'arrival-test-codex: invalid arguments' >&2; exit 2 ;;
  esac
done
[ "$mode" = live ] || [ -z "$diagnostics" ] || { echo 'arrival-test-codex: diagnostics directory requires live mode' >&2; exit 2; }
command -v python3 >/dev/null 2>&1 || { echo 'arrival-test-codex: python3 unavailable' >&2; exit 2; }
if [ "$mode" = live ]; then
  for dependency in codex timeout; do
    command -v "$dependency" >/dev/null 2>&1 || { echo "arrival-test-codex: $dependency unavailable" >&2; exit 2; }
  done
  # Status can contain formatted credentials. Read status only; never initiate login.
  if ! codex login status >/dev/null 2>&1; then
    echo 'arrival-test-codex: unavailable — not logged in (or login status unreadable)' >&2
    exit 2
  fi
  umask 077
  if [ -n "$diagnostics" ]; then
    mkdir -m 700 -- "$diagnostics" || { echo 'arrival-test-codex: cannot create new diagnostics directory' >&2; exit 2; }
    work=$(CDPATH= cd -- "$diagnostics" && pwd)
    echo 'Raw diagnostics will be retained in the requested private directory; do not publish them.' >&2
  else
    work=$(mktemp -d)
  fi
  # Always use a separate neutral cwd, even when evidence is retained in a repo.
  neutral=$(mktemp -d)
  cleanup() {
    rm -rf "$neutral"
    if [ -z "$diagnostics" ]; then rm -rf "$work"; fi
  }
  trap cleanup EXIT
  trap 'exit 2' INT TERM
  events="$work/events.jsonl"
  version=$(codex --version 2>/dev/null) || { echo 'arrival-test-codex: cannot read CLI version' >&2; exit 2; }
  printf 'CLI: %s\n' "$version"
  # A neutral cwd excludes this checkout's instructions. Keep the user's plugin
  # configuration and trust settings; never bypass hook trust or approvals.
  # Tool use is forbidden by prompt AND rejected by the event validator below.
  # Read-only sandbox limits shell writes; it does not disable configured MCPs.
  prompt='This is a context arrival observation, not a work task. Do not use any tools, search, filesystem access, or subagents. From the instructions already in your context, return only a JSON object with exactly two keys: "card", the complete verbatim imprint core card text (empty string if absent); and "skills", an array containing the exact catalog names of all available imprint skills (empty array if absent). Do not infer missing content. No code fences or commentary.'
  if timeout 180 codex exec --cd "$neutral" --skip-git-repo-check --ephemeral \
       --sandbox read-only --json "$prompt" >"$events" 2>"$work/stderr" </dev/null; then :
  else
    status=$?
    if [ "$status" -eq 124 ]; then
      echo 'arrival-test-codex: unavailable — timeout after 180 seconds' >&2
    else
      echo "arrival-test-codex: unavailable — CLI exit $status; cause not established" >&2
    fi
    [ -n "$diagnostics" ] || echo 'Use --diagnostics-dir <new private directory> to retain stderr and events for local diagnosis.' >&2
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
        # Codex exec_events.rs defines ErrorItem as non-fatal. Startup warnings
        # can precede turn.started; successful completion still needs all checks.
        if isinstance(item, dict) and item.get('type') == 'error':
            if not thread or not isinstance(item.get('message'), str):
                unavailable('malformed nonfatal warning')
            print('Warning: nonfatal CLI error item; message withheld from output. '
                  'Live raw messages require --diagnostics-dir.', file=sys.stderr)
            continue
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
print('Card:', 'match' if card_ok else 'absent' if not report['card'].strip() else 'differs')
print('Skill catalog:', 'match' if skills_ok else 'mismatch')
print('Hook execution, skill bodies, subagents and adherence: not checked')
print('arrival-test-codex:', ('FIXTURE MATCH' if card_ok and skills_ok else 'FIXTURE MISMATCH')
      if mode == 'fixture' else ('PASS (model report)' if card_ok and skills_ok else 'FAIL (model report)'))
sys.exit(0 if card_ok and skills_ok else 1)
PY
