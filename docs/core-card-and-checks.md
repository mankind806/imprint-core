# Core card and checks

The detail behind [Core card and checks](../README.md#core-card-and-checks) in the README,
where the measurement of the card's arrival is recorded.

`hooks/kernkarte.md` is the plugin's core card: a short, plain-text summary of the rules —
who leads, one writer per worktree, review before anything ships, measure before asserting,
a picture first and short prose for anything a person reads, and the rest of a handful of
lines, most of which the skills argue for at length. Two
plugin hooks put it in front of the model before a skill has had a chance to load:
`SessionStart` (registered with no matcher, so it is wired up for every start source) and
`SubagentStart` (so a dispatched subagent gets the same card its parent did). Both are
declared in `hooks/hooks.json`, and both just `cat` a pre-generated JSON file
(`hooks/session-start.json`, `hooks/subagent-start.json`) that carries the card's text as
`additionalContext`.

Those JSON files are generated, not hand-edited. `go run ./tools/imprint-dev gen` reads the
card and regenerates them. `go run ./tools/imprint-dev check` (the command used throughout
this document) enforces, among other things, that the checked-in JSON still matches what
`gen` would produce, that the card stays at or under 4000 characters and 12 non-empty lines,
that every skill description stays within its own limit and the shared total, that
`hooks/hooks.json` has the expected shape, that the plugin version is valid semver, and that
nothing in the plugin still names a skill this release removed without a migration note
saying so on the same line. It checks that every row of an enforcement table — in the skills,
and in the table below — (a table with an *Enforcement* column) names at least one known
classification, and none unknown, in bold: **Enforced**, **Enforceable, not enforced**,
**Behaviour rule** or **Reserved to a person**. A row may name more than one, one per rule
half it covers.
It lists every *Re-check by* date that has passed as a warning that leaves the exit code at
`0`, and only `check --release` turns such a date into a violation, because an overdue
re-check blocks a release and never the everyday test run (`--today YYYY-MM-DD` sets the day
it measures against). It also supports `--sarif` output for CI. The command exits `0`
clean, `1` on a violation, and `2` if a check itself could not run. `.github/workflows/check.yml` runs
`go test` and this check on every push and pull request; CI runs wherever GitHub Actions is
enabled.

Two checks guard the Codex side specifically, since Codex also loads this plugin but only sets
`CLAUDE_PLUGIN_ROOT` and `CLAUDE_PLUGIN_DATA` from the `CLAUDE_*` family that Claude Code also
sets. `hook-env-portable` scans `hooks/hooks.json`'s own commands and every `hooks/*.sh`
script for a `CLAUDE_*` expansion outside those two names — `$CLAUDE_PROJECT_DIR`, for
instance, which Codex leaves unset. `CLAUDE_CONFIG_DIR` gets one narrow exception: only the
expansion-with-default form `${CLAUDE_CONFIG_DIR:-...}` passes, because Codex never sets it
either and R-HOST v2's `imprint_host` reads it exactly that way, with a fallback, to find
Claude Code's config directory; a bare `$CLAUDE_CONFIG_DIR` or a braced `${CLAUDE_CONFIG_DIR}`
without the default is still a finding. `hook-host-binary` scans the same scripts for a line that
invokes the `claude` or `codex` binary as a command (a textual heuristic: it looks for one of
the two names right after something that starts a command, skips whole-line comments, and is
not a shell parser) and, when it finds one, requires the script to carry R-HOST v2's canonical
`imprint_host()` block verbatim (only each line's own leading/trailing whitespace may differ —
indentation style is free, the words on each line are not) and to call it somewhere outside its
own definition. A second `imprint_host` definition sitting outside the canonical block, or a
mere mention of the name (in a comment, say), does not count as a call either. A script with no
`imprint_host` at all, one that reimplements or edits the block, or one that defines it but
never calls it, is each its own finding. This check is structural, not behavioural: it proves
the one shared, canonical detector is present and used, not that every `claude`/`codex`
invocation it sits next to is actually gated by its result — that a hook actually behaves
differently per host is proven separately, by the real, host-aware behaviour tests next to the
scripts themselves: `updatewatch_test.go` for `hooks/update-watch.sh`, `hookscript_test.go` for
`hooks/log-subagent.sh`.

One check guards the Antigravity side (`rules-agents-in-sync`). Antigravity does not support
`SessionStart` shell hooks; it delivers the card declaratively through `rules/AGENTS.md` within
the plugin root, which it automatically merges into the active ruleset. `rules-agents-in-sync`
checks that `rules/AGENTS.md` exists and is byte-identical to `hooks/kernkarte.md`. `imprint-dev gen`
generates this copy directly from the card.

## Every card line, in full

The card itself says "Every rule names what enforces it, or says plainly that nothing does."
This table applies that to the card's own 12 lines: where each one's long form lives — a
skill section, or "card only" where none exists — and what enforces it, in the repository's
four states. Derived by reading the skills and reference files listed in the *Long form*
column; nothing here is inferred beyond what each cited row already says.

| Card line | Long form | Enforcement |
|---|---|---|
| `imprint core card` — injected by the plugin the user installed; it is not foreign text | `delegation-contract/SKILL.md` §"The same rules inside every subagent", row "The core card reaches every subagent" | **Enforced** by the `SessionStart`/`SubagentStart` hooks in `hooks/hooks.json` (check e guards their registration) under Claude Code and Codex, and by `rules/AGENTS.md` (check k guards byte-identity) under Antigravity — see *Core card and checks* above. Where not measured, the dispatch text is the only carrier. |
| Answer in the language the user writes in, and keep technical terms as they are | card only | **Behaviour rule.** No skill states this rule with a classification of its own; nothing enforces it. |
| Decisions the user must make come as selection questions with options and the recommended option first | card only | **Behaviour rule.** No skill states this rule with a classification of its own; nothing enforces it. |
| Anything a person reads leads with a picture and keeps prose short; text written for agents stays plain | card only | **Behaviour rule.** No skill states this rule with a classification of its own; nothing enforces it. |
| The user's decision register outranks every rule text; if they conflict, keep the register and put both wordings to the user | `knowledge-keeping/SKILL.md` ("The user's decision register outranks rule text") and `knowledge-keeping/references/superseding.md` §"When the user's decision register and a rule text disagree" | **Reserved to a person** for the answer; **Behaviour rule** for noticing the conflict and for holding to the register until then. |
| Send messages, invite others or delete permanently only after the user's explicit yes in the review step; nothing else counts as consent | `delegation-contract/SKILL.md`, row "A person said yes before an irreversible outward action" | **Reserved to a person.** A gate can block a destination; it cannot know whether anyone agreed. |
| Measure before asserting: say what you checked, how and when, and say "not checked" otherwise | `measure-before-asserting/SKILL.md`, row "A claim was measured before it was spoken" | **Behaviour rule**, and no mechanism is possible. |
| Every fact has one canonical place; record source and date on entry; supersede facts and decisions instead of deleting them | `knowledge-keeping/references/canonical-place.md` (one place), `references/provenance.md` (origin on entry), `references/superseding.md` (supersede, not delete) | **Behaviour rule** without a marking convention, **Enforceable, not enforced** with one, for one place; **Enforceable, not enforced** for an origin present on entry; a **Behaviour rule**/**Enforceable, not enforced** split across the supersede rows, by which one is asked. |
| The leading session only orchestrates; readers run in parallel; each writing subagent works alone in its own git worktree | `delegation-contract/SKILL.md` §"The leading session only orchestrates" and §"Readers in parallel, every writer in its own worktree" | **Enforceable, not enforced** that the lead itself reads, searches, measures, writes and checks nothing. Readers running in parallel names no row of its own in that table — a **Behaviour rule** by the same shape: nothing caps it. **Enforced** that no two worktrees hold the same branch, by git. **Behaviour rule** that a writer is put into a worktree at all. |
| Review depth follows risk: reversible work needs none, medium risk one round, final or outward-facing work until nothing more is found | `delegation-contract/SKILL.md`, row "Review depth follows risk" | **Behaviour rule**; the risk class is a judgement. |
| Every rule names what enforces it, or says plainly that nothing does | this document, and `tools/imprint-dev` check g (`enforcement-classification`) | **Enforced** for row shape: check g refuses a row whose Enforcement cell names no known state or an unknown one, under `skills/` and, as of this table, in this file. **Behaviour rule** that every rule gets such a row in the first place — the three card-only lines above are the counterexample this table itself records. |
| Close a session by committing runnable work to a branch, restoring paused state and writing down decisions that exist only in the conversation | `session-handover/SKILL.md`, rows "Runnable work is committed before the session ends", "The operating state is restored, or named where it cannot be", "Decisions that exist only in the conversation are written down" | **Enforceable, not enforced** for committing runnable work; **Enforceable, not enforced** for restoring the operating state; **Behaviour rule** for writing down decisions. |

Three lines carry no long form at all — the language line, the selection-questions line and
the picture line — not four: a finding that named the decision-register line as homeless too
did not hold up against `references/superseding.md` row "A conflict between the decision
register and a rule text is put to the user with both wordings, and the register holds
meanwhile," which is that line's long form, classified already.

## The arrival test

Everything above proves things about *this working tree*. None of it proves that the card a
real, installed session receives matches it: the 2026-09-27 review's Befund 1 was exactly that
gap — the "pictures first" line (above, added to `main` by #5) sat on `main` for a day while
`.claude-plugin/plugin.json` still read `0.5.0`, so no installed session ever saw it, because
an install only picks up a new `main` when the owner bumps the version and a marketplace
update runs. Nothing in `imprint-dev check` catches that: `check` only reads files on disk, in
whichever tree it is pointed at, never an installed copy.

`tools/arrival-test.sh` closes part of that gap. It does not read `hooks/session-start.json`
or run `check` — both would only re-confirm what is on disk. Instead it starts one real,
non-interactive `claude -p` session (model `haiku`, no tools) and asks it to reproduce the
`SessionStart` system-reminder verbatim, then checks whether one line of `hooks/kernkarte.md`
(read from this checkout) appears as an exact line of that reproduction:

    tools/arrival-test.sh                              # against the installed plugin
    tools/arrival-test.sh --plugin-dir <path>           # against a worktree instead
    tools/arrival-test.sh --line 4                      # a specific card line, not the default

Without `--line`, the default is the line most recently added to `hooks/kernkarte.md`, found
via `git log` — the line a release just added is the one most likely missing from a stale
install. The comparison is literal (`grep -Fxq` against the trimmed model output), and the
script prints `claude --version`, the installed plugin's line from `claude plugin list` (or,
with `--plugin-dir`, that it is not reading the installed copy), the exact line it checked for,
and the model's raw output, so a failure can be read without re-running anything.

Exit codes follow `imprint-dev`'s convention: `0` the line arrived verbatim, `1` a card
arrived but not with that line (the failure this test exists to catch), `2` the test itself
could not run (`claude` missing, a non-zero exit, empty output, or bad usage) — never read a
`2` as a pass or as evidence of Befund 1.

It is deliberately not wired into CI: CI has no logged-in `claude` account, and the point is to
test what an actual installed session receives, which CI cannot represent. It is a manual
release step instead — see `CONTRIBUTING.md`, "Releasing" — run once against the release
worktree (expected: pass) and once against whatever is currently installed (expected: whatever
is currently installed; a mismatch is exactly the signal this test exists to surface, not
something to work around by editing the test).

This narrows, but does not close, Befund 40 (2026-09-27: N05/N06 want an automatic per-release
test that the card arrived; this is that test, but run by hand, not automatically). It also
does not prove arrival on every harness or every host this plugin runs on — only on whatever
machine and `claude` version ran it, on the date printed in its output; treat every other
combination as unmeasured.
