#!/bin/sh
#
# imprint - Claude Code update watch
#
# Registered for SessionStart in hooks/hooks.json. Reads the Claude Code version
# with `claude --version`, compares it with the version recorded last time in
#     ${CLAUDE_PLUGIN_DATA}/claude-code-version
# and records the new one there, appending it with the UTC date to
#     ${CLAUDE_PLUGIN_DATA}/claude-code-version-history
# so that a version whose report never got written stays findable after the
# next update. Only on an upgrade, a higher major.minor.patch, does it print:
# one instruction, as additionalContext, to have the changelog entries and the
# documentation pages between the two versions read, and a dated report saved to
#     ${CLAUDE_PLUGIN_DATA}/update-reports/<new version>.md
#
# SILENT. The first run and a downgrade, which only record the version; an
# unchanged version; a start after compaction, which leaves the record alone for the next real start;
# and every failure: no claude on PATH, an output that is not a version, an unset
# or unwritable CLAUDE_PLUGIN_DATA.
#
# WHAT IT NEVER DOES. Block a start: exit 0 on every path, no network. It does
# delay the first answer by as long as it runs, which hooks/hooks.json caps at
# 10 seconds; the one slow step is `claude --version`. A version must match a
# strict pattern before it reaches a file name or the text.
#
# Report format and limits: docs/update-watch.md.
# Dependencies: a POSIX sh, sed, grep, date, mkdir, mv, rm.

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
# Each number has at most 9 digits, so that test -gt can compare it anywhere.
is_version() {
  case "$1" in '' | *[[:cntrl:]]*) return 1 ;; esac
  printf '%s\n' "$1" | grep -Eq '^[0-9]{1,9}\.[0-9]{1,9}\.[0-9]{1,9}([-+][0-9A-Za-z.-]+)?$'
}

# is_newer A B - true if version A has a higher major.minor.patch than B. Both
# have passed is_version; a pre-release or build suffix is not compared.
is_newer() {
  saved_ifs=$IFS
  IFS=.
  # Unquoted on purpose, to split on the dots; both hold digits and dots only.
  set -- ${1%%[-+]*} ${2%%[-+]*}
  IFS=$saved_ifs
  [ "$1" -gt "$4" ] && return 0
  [ "$1" -lt "$4" ] && return 1
  [ "$2" -gt "$5" ] && return 0
  [ "$2" -lt "$5" ] && return 1
  [ "$3" -gt "$6" ]
}

command -v claude >/dev/null || exit 0
out="$(claude --version </dev/null)" || exit 0
new="${out%%[[:space:]]*}"
is_version "$new" || exit 0

file="$data/claude-code-version"
old=''
# Something at the record's path that is a link or not a plain file is left alone.
if [ -e "$file" ] || [ -L "$file" ]; then
  [ -f "$file" ] && [ ! -L "$file" ] || exit 0
fi
if [ -f "$file" ]; then
  # A record that exists but cannot be read is left alone, not overwritten.
  [ -r "$file" ] || exit 0
  # A record is one line; a second one makes all of it junk.
  {
    IFS= read -r old || :
    if IFS= read -r extra || [ -n "$extra" ]; then old=''; fi
  } <"$file"
fi
is_version "$old" || old=''
[ "$old" = "$new" ] && exit 0

hist="$data/claude-code-version-history"
# A history path that is a link or not a plain file is left alone: a write
# there could succeed without keeping the line.
if [ -e "$hist" ] || [ -L "$hist" ]; then
  [ -f "$hist" ] && [ ! -L "$hist" ] || exit 0
fi

# The history line goes first and the record moves last: if either write fails,
# the record stays as it was and the next start tries again, so no version can
# be recorded without also being in the history.
mkdir -p "$data" || exit 0
day="$(date -u +%Y-%m-%d)" || exit 0
case "$day" in [0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]) ;; *) exit 0 ;; esac
tmp="$file.tmp.$$"
if ! { printf '%s\n' "$new" >"$tmp" &&
  printf '%s %s\n' "$day" "$new" >>"$hist" &&
  mv -f "$tmp" "$file"; }; then
  rm -f "$tmp"
  exit 0
fi
[ -n "$old" ] || exit 0
# A downgrade, or a change in the suffix only, is recorded without a notice: the
# range to read runs forwards, and the next upgrade reads from here.
is_newer "$new" "$old" || exit 0

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
