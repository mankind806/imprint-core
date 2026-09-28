#!/bin/sh
# Installed Antigravity context probe; CLI help verified with 1.2.12 (2026-09-28).
# Usage: tools/arrival-test-agy.sh [--events <JSON fixture>]
#                                 [--diagnostics-dir <new private directory>]
# Live mode costs one model call and needs a functioning agy CLI plus timeout.
# Expected card and skill names stay here: they are never supplied to the model.
# A match is model-reported context arrival, NOT proof of hook execution, skill
# body loading, subagent inheritance, or adherence. Those remain not checked.
# --events validates saved/fixture output only; it never claims live arrival.
# Tests installed configuration. Antigravity can write state/logs and run plugins.
# --diagnostics-dir preserves raw stderr and JSON (may contain private context
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
      [ $# -ge 2 ] && [ -n "$2" ] && [ "$mode" = live ] || { echo 'arrival-test-agy: expected one --events <file>' >&2; exit 2; }
      mode=fixture; events=$2; shift 2 ;;
    --diagnostics-dir)
      [ $# -ge 2 ] && [ -n "$2" ] && [ -z "$diagnostics" ] || { echo 'arrival-test-agy: expected one --diagnostics-dir <new directory>' >&2; exit 2; }
      diagnostics=$2; shift 2 ;;
    *) echo 'arrival-test-agy: invalid arguments' >&2; exit 2 ;;
  esac
done
[ "$mode" = live ] || [ -z "$diagnostics" ] || { echo 'arrival-test-agy: diagnostics directory requires live mode' >&2; exit 2; }
command -v python3 >/dev/null 2>&1 || { echo 'arrival-test-agy: python3 unavailable' >&2; exit 2; }
if [ "$mode" = live ]; then
  AGY_CMD=""
  for cmd in agy antigravity; do
    if command -v "$cmd" >/dev/null 2>&1; then
      AGY_CMD="$cmd"
      break
    fi
  done
  if [ -z "$AGY_CMD" ]; then
    echo 'arrival-test-agy: agy unavailable' >&2
    exit 2
  fi
  command -v timeout >/dev/null 2>&1 || { echo 'arrival-test-agy: timeout unavailable' >&2; exit 2; }

  umask 077
  if [ -n "$diagnostics" ]; then
    mkdir -m 700 -- "$diagnostics" || { echo 'arrival-test-agy: cannot create new diagnostics directory' >&2; exit 2; }
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
  events="$work/events.json"
  version=$("$AGY_CMD" --version 2>/dev/null) || { echo 'arrival-test-agy: cannot read CLI version' >&2; exit 2; }
  printf 'CLI: %s\n' "$version"
  # A neutral cwd excludes this checkout's instructions. Keep the user's plugin
  # configuration and trust settings.
  # Tool use is forbidden by prompt.
  prompt='This is a context arrival observation, not a work task. Do not use any tools, search, filesystem access, or subagents. From the instructions already in your context, return only a JSON object with exactly two keys: "card", the complete verbatim imprint core card text (empty string if absent); and "skills", an array containing the exact catalog names of all available imprint skills (empty array if absent). Do not infer missing content. No code fences or commentary.'
  if (cd "$neutral" && timeout 180 "$AGY_CMD" -p "$prompt" --output-format json >"$events" 2>"$work/stderr" </dev/null); then :
  else
    status=$?
    if [ "$status" -eq 124 ]; then
      echo 'arrival-test-agy: unavailable — timeout after 180 seconds' >&2
    else
      echo "arrival-test-agy: unavailable — CLI exit $status; cause not established" >&2
    fi
    [ -n "$diagnostics" ] || echo 'Use --diagnostics-dir <new private directory> to retain stderr and events for local diagnosis.' >&2
    exit 2
  fi
fi
python3 - "$ROOT" "$events" "$mode" <<'PY'
import datetime, json, pathlib, sys
root, source, mode = pathlib.Path(sys.argv[1]), pathlib.Path(sys.argv[2]), sys.argv[3]
def unavailable(reason):
    print('arrival-test-agy: invalid/unavailable — ' + reason, file=sys.stderr)
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
    content = source.read_text().strip()
    raw_event = parse(content)
    expected = (root / 'hooks/kernkarte.md').read_text().rstrip('\n')
    skills = sorted(p.name for p in (root / 'skills').iterdir() if (p / 'SKILL.md').is_file())
except (OSError, UnicodeError, ValueError) as e:
    unavailable('cannot read valid JSON output and local expectations')
if not expected or not skills:
    unavailable('empty local expectations')
if not isinstance(raw_event, dict):
    unavailable('output must be a JSON object')

# Support either direct result object {"status": "SUCCESS", "response": "..."}
# or stream-json result {"event": "result", "result": {...}}
if 'result' in raw_event and isinstance(raw_event['result'], dict):
    result_obj = raw_event['result']
else:
    result_obj = raw_event

status = result_obj.get('status')
if status != 'SUCCESS':
    unavailable('CLI run status was not SUCCESS')

num_turns = result_obj.get('num_turns')
if type(num_turns) is not int or num_turns != 1:
    unavailable('tool use or multiple turns detected; no arrival claim')

outer_turns = raw_event.get('num_turns')
if outer_turns is not None and (type(outer_turns) is not int or outer_turns != 1):
    unavailable('tool use or multiple turns detected; no arrival claim')

if raw_event.get('tool_calls') or result_obj.get('tool_calls'):
    unavailable('tool use detected; no arrival claim')

response_text = result_obj.get('response')
if not isinstance(response_text, str) or not response_text.strip():
    unavailable('missing or empty response in result')

response_clean = response_text.strip()
if response_clean.startswith('```'):
    lines = response_clean.splitlines()
    if lines[0].startswith('```') and lines[-1].startswith('```'):
        response_clean = '\n'.join(lines[1:-1]).strip()

try:
    report = parse(response_clean)
except ValueError:
    unavailable('model response is not valid JSON')

if (not isinstance(report, dict) or set(report) != {'card', 'skills'}
    or not isinstance(report['card'], str) or not isinstance(report['skills'], list)
    or any(not isinstance(s, str) for s in report['skills'])
    or len(report['skills']) != len(set(report['skills']))):
    unavailable('final report has wrong shape')

card_ok = report['card'].rstrip('\n') == expected
reported_skills = sorted(s.removeprefix('imprint:') for s in report['skills'])
skills_ok = reported_skills == skills

print('Checked UTC:', datetime.datetime.now(datetime.timezone.utc).isoformat())
print('Mode:', 'fixture validation; live arrival not checked' if mode == 'fixture' else 'live model-reported context')
print('Card:', 'match' if card_ok else 'absent' if not report['card'].strip() else 'differs')
print('Skill catalog:', 'match' if skills_ok else 'mismatch')
print('Hook execution, skill bodies, subagents and adherence: not checked')
print('arrival-test-agy:', ('FIXTURE MATCH' if card_ok and skills_ok else 'FIXTURE MISMATCH')
      if mode == 'fixture' else ('PASS (model report)' if card_ok and skills_ok else 'FAIL (model report)'))
sys.exit(0 if card_ok and skills_ok else 1)
PY
