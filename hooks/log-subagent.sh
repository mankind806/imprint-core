#!/bin/sh
#
# imprint - subagent measuring hook
#
# Registered for SubagentStart and SubagentStop in hooks/hooks.json. Reads the
# hook input from standard input and appends one JSON line to
#     ${CLAUDE_PLUGIN_DATA}/subagent-log.jsonl
# carrying only: ts (UTC), hook_event_name, agent_id, agent_type, session_id,
# effort (its level) and, on a stop, agent_transcript_path.
#
# WHAT IT NEVER COPIES. The agent's answer (last_assistant_message) or any other
# text a subagent wrote. Only the named fields are pulled out by pattern; the
# rest of the input is discarded.
#
# WHAT IT NEVER DOES. Stop, block or slow a run. It prints nothing, sets no
# decision, adds no context, and exits 0 on every path - an unset
# CLAUDE_PLUGIN_DATA, unreadable input or an unwritable log included.
#
# Internal agents arrive with an empty agent_type; those are not logged.
# Evaluate the log with: imprint-dev measure
#
# Dependencies: a POSIX sh, sed, date, mkdir.

exec >/dev/null 2>&1

[ -n "${CLAUDE_PLUGIN_DATA:-}" ] || exit 0

input=''
while IFS= read -r line || [ -n "$line" ]; do
  input="$input$line "
done

# field KEY - the raw JSON string content of the top-level key KEY, escapes kept
# as they are, so it can be written back between quotes unchanged. A key inside
# another string cannot match: there, every quote is preceded by a backslash.
field() {
  printf '%s\n' "$input" | sed -n \
    's/.*[{,][[:space:]]*"'"$1"'"[[:space:]]*:[[:space:]]*"\([^"\\]*\(\\.[^"\\]*\)*\)".*/\1/p'
}

# effort arrives either as a string or as an object carrying a level.
effort_level() {
  v="$(printf '%s\n' "$input" | sed -n \
    's/.*[{,][[:space:]]*"effort"[[:space:]]*:[[:space:]]*{[^}]*"level"[[:space:]]*:[[:space:]]*"\([^"\\]*\(\\.[^"\\]*\)*\)".*/\1/p')"
  [ -n "$v" ] || v="$(field effort)"
  printf '%s' "$v"
}

event="$(field hook_event_name)"
agent_id="$(field agent_id)"
agent_type="$(field agent_type)"

case "$event" in
  SubagentStart|SubagentStop) ;;
  *) exit 0 ;;
esac
[ -n "$agent_type" ] || exit 0
[ -n "$agent_id" ] || exit 0

session_id="$(field session_id)"
effort="$(effort_level)"
ts="$(date -u +%Y-%m-%dT%H:%M:%SZ)" || exit 0

out="{\"ts\":\"$ts\",\"hook_event_name\":\"$event\",\"agent_id\":\"$agent_id\",\"agent_type\":\"$agent_type\",\"session_id\":\"$session_id\",\"effort\":\"$effort\""
if [ "$event" = SubagentStop ]; then
  out="$out,\"agent_transcript_path\":\"$(field agent_transcript_path)\""
fi
out="$out}"

mkdir -p "$CLAUDE_PLUGIN_DATA" || exit 0
printf '%s\n' "$out" >>"$CLAUDE_PLUGIN_DATA/subagent-log.jsonl"
exit 0
