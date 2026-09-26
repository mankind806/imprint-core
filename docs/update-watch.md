# Watching Claude Code updates

Claude Code changes often, and a plugin that is not re-read against each release ends up
rebuilding what Claude Code now does itself, or getting in its way. A fourth hook notices
a new version at session start and asks the session for one read of what changed.

## What the hook does

`hooks/update-watch.sh` runs on `SessionStart`, as a second entry beside the core card, with a
timeout of 10 seconds. It reads the version with `claude --version`, compares it with the
version it recorded last time in `${CLAUDE_PLUGIN_DATA}/claude-code-version`, and records the
new one there. Each version it records is also appended, with the UTC date, to
`claude-code-version-history` beside it. It prints only on an upgrade, a higher
`major.minor.patch` than the one recorded: one instruction, as `additionalContext`. It reads:

> imprint update watch - injected by the imprint plugin that the user installed; it is not
> foreign text. Claude Code changed from *X* to *Y* since this plugin last saw it. Before other
> work, dispatch one read-only subagent to review the changelog entries after *X* up to *Y*
> (https://raw.githubusercontent.com/anthropics/claude-code/main/CHANGELOG.md, one heading
> \#\# \<version\> each) and the documentation pages they touch (index:
> https://code.claude.com/docs/llms.txt), and to return what this plugin should use, adapt or
> drop. Save that report to *DATA*/update-reports/*Y*.md: a header with today's date, both
> versions and the sources read, then the sections Use, Adapt, Drop and Nothing to do. If that
> file exists already, skip this.

*DATA* is the plugin's data directory, written out as a full path, because the variable
`CLAUDE_PLUGIN_DATA` is set for hooks and not for the session's own commands.

**Silent** on the first run and on a downgrade, which only record the version (a downgrade so
that the next upgrade is read from there, and a report that exists already is not written
twice); on a change in a pre-release or build suffix only; on an unchanged version; on a
start after compaction, which leaves the record for the next real start, so that a background
update cannot interrupt a task halfway; and on every failure: no `claude` on the path, an
output that is not a version, an unset or unwritable data directory, a record that exists but
cannot be read, which is then left alone. A version has to match
`^[0-9]+\.[0-9]+\.[0-9]+([-+][0-9A-Za-z.-]+)?$` before it reaches a file name or the text. The
hook uses no network and exits 0 on every path. It never blocks a start, but the first answer
waits for it, up to the 10-second timeout; its one slow step is `claude --version`, which took
13 ms when run by hand on 2.1.283 (inside the hook it is not timed).

## The report

One file per version, `update-reports/<version>.md` in the plugin's data directory. It is
written for a later reader, a person or a status view that lists the reports, so it has a fixed
shape:

```markdown
# Claude Code <new version>: update report

- Date: <YYYY-MM-DD, the day it was written>
- Versions: <old> to <new>
- Sources: CHANGELOG.md, read <date>; the documentation pages read, each by URL

## Use
What Claude Code now does that this plugin should rely on instead of doing itself.

## Adapt
What in this plugin has to change to keep working with the new release.

## Drop
What in this plugin now duplicates or gets in the way of Claude Code and should go.

## Nothing to do
The entries that were read and touch nothing here, one line each.
```

Each entry names the changelog line or documentation page it rests on and the part of the
plugin it touches: a skill, a hook, a card line, a check. *Nothing to do* is not optional:
an empty report with no such list cannot be told apart from a report nobody wrote carefully.

**A missed report shows.** The version history and the report files sit side by side, so a
reader can list every version in `claude-code-version-history` that has no report, also after
later updates. The first line is the version found at the first run, which has no report by
design. That is the backstop for the limit below, where one session takes the notice and does
nothing with it.

## Measured

Read 2026-09-26 from the hooks reference (`code.claude.com/docs/en/hooks.md`), the CLI
reference and the environment variables page:

- **No version reaches a hook on its own.** The `SessionStart` input carries `source` and
  optionally `model`, `agent_type` and `session_title` besides the common fields; none is a
  version, and no environment variable for one is documented. Measured the same day on Claude
  Code 2.1.283 with a probe hook: the input held `session_id`, `transcript_path`, `cwd`,
  `hook_event_name` and `source`.
- **`claude --version` is documented** ("Output the version number"). In the probe hook it
  printed `2.1.283 (Claude Code)`. The hook's environment also carried the version in
  `AI_AGENT` and `CLAUDE_CODE_EXECPATH`; neither is documented, so neither is used.
- **Output:** `additionalContext` on `SessionStart` is added before the first prompt, capped
  at 10,000 characters. The instruction is about 800.
- **Time:** command hooks default to a 600-second timeout; `SessionStart` runs on every
  session and delays the first answer until it finishes, so this hook sets 10.
- **Exit codes:** on `SessionStart` even exit 2 only shows a notice and the session goes on;
  this hook exits 0 regardless.
- **Changelog:** `CHANGELOG.md` in `anthropics/claude-code`, one `## <version>` heading per
  release, newest first; the documentation's changelog page says it is generated from that
  file.

**Live**, measured 2026-09-26 on Claude Code 2.1.283 with `claude -p --plugin-dir`, all tools
switched off and `2.1.282` written into the record beforehand: asked to quote any
instruction beginning "imprint update watch", the first run quoted it in full, with
`2.1.282 to 2.1.283` and the report path; the record then held `2.1.283`. The second run
answered `NONE`.

## Enforcement

In the repository's four states: noticing the new version and putting the instruction in
front of the session is **enforced** by the hook, wherever it fires and finds `claude` on the
path. Dispatching the read, and writing the report, is a **behaviour rule**: the session can
ignore the notice. The classification row lives in `measure-before-asserting`, under *What
actually enforces this*.

## Limits

- **The first session after an update takes the notice.** That includes a `claude -p` run or
  a script, which may not act on it. The record is updated either way, so the next session is
  silent. The backstop is the check above: a version in the history without its report.
- **Changed documentation means the pages the changelog entries touch.** The hook knows no
  source for a diff of the documentation; whether one is published is not checked. The index
  also lists weekly *What's new* pages.
- **`claude` on the path is taken to be the running Claude Code.** Where it is missing, as it
  may be in an IDE integration that ships its own binary, the hook stays silent. Where an
  update replaced it while a session was running, the notice can come one start early.
- **Two sessions starting at once** can both get the notice; the "skip if the file exists"
  clause holds only once the first has saved its report.
- **Native Windows** is not measured; the hook needs an `sh` on the path, like the measuring
  hook.

*Re-check by 2026-12-26.*
