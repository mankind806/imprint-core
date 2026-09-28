#!/bin/sh
# tools/arrival-test.sh — Claude Code only. Proves whether the imprint core
# card a fresh Claude Code session actually receives at SessionStart matches
# hooks/kernkarte.md in this repo, instead of an older release's card. See
# docs/core-card-and-checks.md, section "The arrival test", for what this
# checks, why, and when to run it. For the Codex side, see
# tools/arrival-test-codex.sh.
#
# Usage:
#   tools/arrival-test.sh [--plugin-dir <path>] [--line <n>]
#
# Without --plugin-dir: tests whatever the imprint plugin currently installed
# for this account ships (`claude plugin list` says which version).
# With --plugin-dir <path>: tests that path's plugin tree instead (e.g. a
# release worktree, before anyone installs it).
#
# --line <n>: which line of hooks/kernkarte.md (read from THIS checkout) the
# test looks for in the injected card. Default: the line most recently added
# to hooks/kernkarte.md, per `git log` — the line a release just added is the
# one most likely to be missing from a stale install (Befund 1, 2026-09-27).
#
# Method: one non-interactive `claude -p` call asks a fresh session to
# reproduce verbatim the system-reminder that SessionStart injected, then this
# script greps the chosen line out of hooks/kernkarte.md and checks it appears
# as an exact line (byte for byte) somewhere in that output. Costs one billed
# call (model: haiku).
#
# Exit 0: the line arrived verbatim in the injected card.
# Exit 1: a card arrived, but not with that line — the failure this test
#         exists to catch (an installed plugin.json/marketplace lagging main).
# Exit 2: the test itself could not run (claude missing, non-zero exit, empty
#         output, or bad usage) — never confuse this with exit 1.
#
# Not wired into CI: it needs a logged-in `claude` account. Run it by hand as
# a release step (see CONTRIBUTING.md).

set -eu

ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
CARD="$ROOT/hooks/kernkarte.md"

plugin_dir=""
line=""
while [ $# -gt 0 ]; do
  case "$1" in
    --plugin-dir)
      [ $# -ge 2 ] || { echo "arrival-test: --plugin-dir needs a value" >&2; exit 2; }
      plugin_dir=$2
      shift 2
      ;;
    --line)
      [ $# -ge 2 ] || { echo "arrival-test: --line needs a value" >&2; exit 2; }
      line=$2
      shift 2
      ;;
    -h|--help)
      echo "usage: $0 [--plugin-dir <path>] [--line <n>]"
      exit 0
      ;;
    *)
      echo "arrival-test: unknown argument: $1" >&2
      echo "usage: $0 [--plugin-dir <path>] [--line <n>]" >&2
      exit 2
      ;;
  esac
done

if ! command -v claude >/dev/null 2>&1; then
  echo "arrival-test: claude is not on PATH" >&2
  exit 2
fi

if [ ! -f "$CARD" ]; then
  echo "arrival-test: cannot read $CARD" >&2
  exit 2
fi

if [ -z "$line" ]; then
  # Default: the line most recently added to hooks/kernkarte.md, per the last
  # commit that touched it. A '+' diff line that is not the '+++' file header.
  added=$(cd "$ROOT" && git log -p -1 -- hooks/kernkarte.md 2>/dev/null \
    | grep '^+[^+]' | tail -1 | cut -c2-) || true
  if [ -z "${added:-}" ]; then
    echo "arrival-test: could not find the last-added line of hooks/kernkarte.md via git; pass --line" >&2
    exit 2
  fi
  line=$(grep -nFx "$added" "$CARD" | head -1 | cut -d: -f1)
  if [ -z "${line:-}" ]; then
    echo "arrival-test: last-added line from git no longer appears verbatim in $CARD; pass --line" >&2
    exit 2
  fi
fi

expected=$(sed -n "${line}p" "$CARD")
if [ -z "$expected" ]; then
  echo "arrival-test: hooks/kernkarte.md has no line $line" >&2
  exit 2
fi

prompt="Your context includes a system-reminder that starts with the words: SessionStart hook additional context: imprint core card. Reproduce that entire system-reminder's text verbatim, including every line after the first, each on its own line. Do not add numbering, bullets, markdown, or any other commentary. Do not stop after one line."

work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT INT TERM

version=$(claude --version 2>/dev/null || echo "unknown")
if [ -n "$plugin_dir" ]; then
  installed="(not from the installed plugin: --plugin-dir $plugin_dir)"
else
  installed=$(claude plugin list 2>/dev/null | grep -A1 '^  ❯ imprint@' | tr '\n' ' ' | sed 's/  */ /g')
  [ -n "$installed" ] || installed="(imprint not found in: claude plugin list)"
fi

# Run from a neutral directory: cwd-local settings or CLAUDE.md must not be
# able to change what a fresh session receives.
cd "$work"
if [ -n "$plugin_dir" ]; then
  set -- claude -p --model haiku --disallowedTools="*" --plugin-dir "$plugin_dir" "$prompt"
else
  set -- claude -p --model haiku --disallowedTools="*" "$prompt"
fi

if ! output=$("$@" 2>&1); then
  echo "arrival-test: claude exited non-zero" >&2
  printf '%s\n' "$output" >&2
  exit 2
fi

echo "scope: Claude Code only — for Codex, see tools/arrival-test-codex.sh"
echo "claude --version: $version"
echo "installed plugin: $installed"
echo "checked line $line of hooks/kernkarte.md: $expected"
echo "--- raw output ---"
printf '%s\n' "$output"
echo "--- end raw output ---"

if [ -z "$output" ]; then
  echo "arrival-test: FAIL (exit 2) — empty output" >&2
  exit 2
fi

if printf '%s\n' "$output" | grep -Fxq "$expected"; then
  echo "arrival-test: PASS (Claude Code only) — line $line arrived verbatim"
  exit 0
else
  echo "arrival-test: FAIL (Claude Code only) — line $line did not arrive verbatim"
  exit 1
fi
