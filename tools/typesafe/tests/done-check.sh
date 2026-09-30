#!/usr/bin/env bash
# Manual scenario tests for ts-done-check (real API calls except where noted).
set -u
D=$(mktemp -d); trap 'rm -rf "$D"' EXIT
export HOME_LOG=$HOME/.local/state/typesafe-dev/done-check.jsonl
tr() {  # tr <file> <bash-cmd> <is_error> : transcript with one Edit and one Bash call
  cat > "$1" <<J
{"type":"assistant","message":{"content":[{"type":"tool_use","id":"t1","name":"Edit","input":{"file_path":"/x/a.py","new_string":"SECRET-CONTENT"}}]}}
{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"t1","content":"ok"}]}}
$( [ -n "$2" ] && cat <<K
{"type":"assistant","message":{"content":[{"type":"tool_use","id":"t2","name":"Bash","input":{"command":"$2"}}]}}
{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"t2","is_error":$3,"content":"all 12 passed"}]}}
K
)
J
}
run() { # run <name> <json> [env-prefix...]
  echo "--- $1"; out=$(printf '%s' "$2" | ${3:-} ts-done-check); rc=$?; echo "exit=$rc stdout=[$out]"; }
tr $D/unbacked.jsonl "" false
tr $D/backed.jsonl "pytest -q" false
MSG='Alles erledigt, der Fix funktioniert und die Tests sind grün.'
run "1 unbacked claim -> block" "{\"transcript_path\":\"$D/unbacked.jsonl\",\"last_assistant_message\":\"$MSG\"}"
run "2 backed by passing test -> pass" "{\"transcript_path\":\"$D/backed.jsonl\",\"last_assistant_message\":\"$MSG\"}"
run "3 stop_hook_active -> pass" "{\"stop_hook_active\":true,\"transcript_path\":\"$D/unbacked.jsonl\",\"last_assistant_message\":\"$MSG\"}"
n0=$(wc -l < "$HOME_LOG")
run "4 no claim -> pass, no API call" "{\"transcript_path\":\"$D/unbacked.jsonl\",\"last_assistant_message\":\"Ich habe die Datei gelesen, was soll ich als nächstes tun?\"}"
echo "log lines added by test 4: $(( $(wc -l < "$HOME_LOG") - n0 ))"
mkdir -p $D/fakebin; printf '#!/bin/sh\nexit 1\n' > $D/fakebin/secret-tool; chmod +x $D/fakebin/secret-tool
echo "--- 5 key missing (env unset, secret-tool shadowed)"
out=$(printf '%s' "{\"transcript_path\":\"$D/unbacked.jsonl\",\"last_assistant_message\":\"$MSG\"}" | env -u TYPESAFE_API_KEY PATH="$D/fakebin:$PATH" ts-done-check); echo "exit=$? stdout=[$out]"
echo "--- log tail (no message text expected)"; tail -n 3 "$HOME_LOG"
