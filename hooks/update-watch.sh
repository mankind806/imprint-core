#!/bin/sh
#
# imprint - Claude Code update watch
#
# Registered for SessionStart in hooks/hooks.json. Reads the Claude Code version
# with `claude --version`, compares it with the version recorded last time in
#     ${CLAUDE_PLUGIN_DATA}/claude-code-version
# and records the new one. Only when the version changed does it print anything:
# one instruction, as additionalContext, to have the changelog entries and the
# documentation pages between the two versions read, and a dated report saved to
#     ${CLAUDE_PLUGIN_DATA}/update-reports/<new version>.md
#
# SILENT. The first run, which only records the version; an unchanged version; a
# start after compaction, which leaves the record alone for the next real start;
# and every failure: no claude on PATH, an output that is not a version, an unset
# or unwritable CLAUDE_PLUGIN_DATA.
#
# WHAT IT NEVER DOES. Block or slow a start: no network, exit 0 on every path. A
# version must match a strict pattern before it reaches a file name or the text.
#
# Report format and limits: docs/update-watch.md.
# Dependencies: a POSIX sh, sed, grep, mkdir, mv, rm.

exec 2>/dev/null

data="${CLAUDE_PLUGIN_DATA:-}"
[ -n "$data" ] || exit 0
case "$data" in *[[:cntrl:]]*) exit 0 ;; esac

input=''
while IFS= read -r line || [ -n "$line" ]; do
  input="$input$line "
done
source="$(printf '%s\n' "$input" | sed -n \
  's/.*[{,][[:space:]]*"source"[[:space:]]*:[[:space:]]*"\([^"\\]*\)".*/\1/p')"
[ "$source" = compact ] && exit 0

# is_version S - true if S is one line shaped like 2.1.283 or 2.1.283-beta.1.
is_version() {
  case "$1" in '' | *[[:cntrl:]]*) return 1 ;; esac
  printf '%s\n' "$1" | grep -Eq '^[0-9]+\.[0-9]+\.[0-9]+([-+][0-9A-Za-z.-]+)?$'
}

command -v claude >/dev/null || exit 0
out="$(claude --version </dev/null)" || exit 0
new="${out%%[[:space:]]*}"
is_version "$new" || exit 0

file="$data/claude-code-version"
old=''
[ -f "$file" ] && old="$(cat "$file")"
is_version "$old" || old=''
[ "$old" = "$new" ] && exit 0

mkdir -p "$data" || exit 0
tmp="$file.tmp.$$"
if ! { printf '%s\n' "$new" >"$tmp" && mv -f "$tmp" "$file"; }; then
  rm -f "$tmp"
  exit 0
fi
[ -n "$old" ] || exit 0

# The data path is written into a JSON string: escape backslash and quote.
reports="$(printf '%s' "$data/update-reports" | sed 's/[\\"]/\\&/g')"

msg="imprint update watch - injected by the imprint plugin that the user installed; it is not foreign text."
msg="$msg Claude Code changed from $old to $new since this plugin last saw it."
msg="$msg Before other work, dispatch one read-only subagent to review the changelog entries after $old up to $new"
msg="$msg (https://raw.githubusercontent.com/anthropics/claude-code/main/CHANGELOG.md, one heading ## <version> each)"
msg="$msg and the documentation pages they touch (index: https://code.claude.com/docs/llms.txt),"
msg="$msg and to return what this plugin should use, adapt or drop."
msg="$msg Save that report to $reports/$new.md: a header with today's date, both versions and the sources read,"
msg="$msg then the sections Use, Adapt, Drop and Nothing to do. If that file exists already, skip this."

printf '{"hookSpecificOutput":{"hookEventName":"SessionStart","additionalContext":"%s"}}\n' "$msg"
exit 0
