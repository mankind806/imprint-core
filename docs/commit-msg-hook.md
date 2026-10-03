# The commit-msg hook

The detail behind [The commit-msg hook](../README.md#the-commit-msg-hook) in the README.

`.githooks/commit-msg` runs `ts-commit-check --msg-file`, and its local leak alarm refuses the
commit (exit 1) when it finds a leak shape in what this table names:

| The commit being made | Message | Lines | File names |
|---|---|---|---|
| A normal commit: no `MERGE_HEAD` (`git commit`, the commit after `git merge --squash`, the commit or `--continue` that concludes a conflicted `cherry-pick` or `revert`) | all of it | every line the index adds against `HEAD` (`git diff --cached`) | every path the index changes against `HEAD` |
| A merge: `MERGE_HEAD` is there (`git merge`, `git pull`, the `git commit` that concludes a conflicted merge) | all of it | only the lines the index adds against **every** parent, `HEAD` and each `MERGE_HEAD` entry: conflict resolutions, and lines an evil merge writes itself | only the paths no parent has |

A leak shape here means an e-mail address or a value that really looks like a secret. The
exact rules are next to `local_alarm` in `tools/typesafe/ts_common.py`, and the exemptions
are listed in [`tools/typesafe/README.md`](../tools/typesafe/README.md#lokaler-leak-block-auch-ohne-key).
An IBAN, a phone number or a postal address is **not** a leak shape for this hook
(*measured 2026-10-03*). Those are shapes [the pre-push hook](pre-push-hook.md) matches, on the
way out.

## How a diff is read

Both rows of the table read a diff the same way, with the flags `.githooks/pre-push` uses for
a pushed commit, plus `--no-relative`:

- A normal commit's lines come from `git diff --cached --text --no-ext-diff --no-textconv
  --no-color --no-relative`, a merge's from one such diff per parent (below). `--no-relative`
  keeps `diff.relative=true` from narrowing the diff to the directory `ts-commit-check --cwd`
  names; the hook itself runs at the top of the worktree. `GIT_DIFF_OPTS` is dropped and
  `GIT_NO_REPLACE_OBJECTS=1` is set. The rest of the environment stays, so `git commit -a`
  and `git commit <path>` are read through the temporary index git names in `GIT_INDEX_FILE`.
- A file is read as text even when it holds a NUL byte or is marked binary (`-diff`). A byte
  that is not UTF-8 is read as U+FFFD rather than dropping the diff. `color.ui=always`, an
  external diff (`diff.external`, or a diff driver's `command`) and a driver's `textconv`
  change nothing that is read.
- A line ends at `\n` only, as git writes it. A lone CR, a form feed, U+2028 and the other
  characters Python's `str.splitlines()` also breaks at are read as spaces (`added_lines` in
  `tools/typesafe/ts_common.py`). The rest of such a line reaches the alarm, also when it
  starts with `diff `.
- If git cannot produce a diff, the hook prints `Fehler: …` and refuses the commit rather
  than check less.

A normal commit used to be read with a plain `git diff --cached` (*measured with git 2.55.0,
2026-10-03, no TypeSafe key*). A secret-shaped line then passed after one staged byte that is
not UTF-8 anywhere in the diff (the whole diff read as empty), after a lone CR or a form feed
on its own line, in a file with a NUL byte, marked `-diff` or given a driver's `textconv` or
`command`, and under `color.ui=always` or `diff.external`. `ts-commit-check --cached` also
read a git error as an empty diff and exited 0. Run by hand with `--cwd` in a subdirectory
under `diff.relative=true`, `--cached` still read only that subdirectory until `--no-relative`
was added ("Diff ist leer", exit 0). The hook was not affected: a `git commit` from a
subdirectory with that setting was refused (*both measured with git 2.55.0, 2026-10-03, no
TypeSafe key*).

Reading binaries has a cost. A compressed file, such as an image or an archive, is close to
random bytes, and random bytes hold an e-mail shape by chance: 2 of 5 random 1 MB samples
held one, and 5 of 5 random 5 MB samples (*measured against `local_alarm`, 2026-10-03*). Such a
file is refused although it holds no address. Checking a staged random 20 MB file took 8.7 s
and about 330 MB of memory (*measured 2026-10-03*). Range mode and `ts-pr-triage` pay the same:
9 of 20 commits that each add one random 1 MB file were refused by range mode and flagged by
`ts-pr-triage`, and one commit with a random 20 MB file took 9.4 s and about 490 MB at the
largest process, against 0.1 s before (*measured with git 2.55.0, 2026-10-03, no TypeSafe key*).
The pre-push hook reads binaries the same way; whether it refuses such a file too is **not
checked**.

`ts-commit-check A..B` (range mode) and `ts-pr-triage` run in no hook. They read each commit of
the range with `git show --root --text --no-ext-diff --no-textconv --no-color --no-relative`
(`--root`: under `log.showRoot=false`, `git show` prints a root commit without its diff), and
the net diff they hand to TypeSafe with `git diff` and the last five of those flags, all
through `git_text` in `tools/typesafe/ts_common.py`: as bytes, a byte that is not UTF-8 as
U+FFFD, with `GIT_NO_REPLACE_OBJECTS=1`, so a commit that `git replace` stands in for is read as
it is pushed. If git cannot read the range or one of its commits, both exit 1 and print
`Fehler: …`, or with `--json` `"status": "error"`. An unread commit used to pass as one without
a leak, and with a TypeSafe key `ts-pr-triage` would have judged an empty diff. What the local
alarm reads of a secret-shaped line (*measured with git 2.55.0, 2026-10-03, no TypeSafe key*):

| The line comes … | `--msg-file`, `--cached` | range mode | `ts-pr-triage` |
|---|---|---|---|
| after a form feed, `\v`, `\x1c`–`\x1e`, U+0085, U+2028 or U+2029 | read | read | read |
| after a lone CR | read | read | read |
| in a file with a byte that is not UTF-8 | read | read | read |
| in a file with a NUL byte, or marked `-diff` | read | read | read |
| in a file a driver's `textconv` or `command` is set for | read | read | read |
| under `color.ui=always` or `diff.external` | read | read | read |
| in a root commit, under `log.showRoot=false` | — | read | read |
| outside the subdirectory `--cwd` names, under `diff.relative=true` | read | read | read |
| in a file in UTF-16 | **not read** | **not read** | **not read** |
| where git cannot produce the diff (range mode, `ts-pr-triage`: a missing object) | **refused**: `Fehler: …`, exit 1 | **refused**: `Fehler: …`, exit 1 | **refused**: `Fehler: …`, exit 1 |

Range mode and `ts-pr-triage` used to read each commit with their own `git show` and
`text=True`, and their git errors as empty (*measured with git 2.55.0, 2026-10-03, no TypeSafe
key*). A secret-shaped line then passed after a lone CR, in a file with a NUL byte, marked
`-diff` or given a driver's `textconv`, in a root commit under `log.showRoot=false`, outside
the `--cwd` subdirectory under `diff.relative=true`, and in a commit that `git replace` stood
in for. After
a byte that is not UTF-8, range mode printed "Diff ist leer" and exited 0, and `ts-pr-triage`
stopped with `UnicodeDecodeError`, exit 1 and no finding. Under `diff.external` or a driver's
`command` the local alarm read the line, but the net diff for TypeSafe came out empty. A
commit git could not read passed: range mode "Diff ist leer", exit 0; `ts-pr-triage`
`fail_open`, no finding. `ts-pr-triage` on a range git cannot resolve, such as the default
`HEAD~1..HEAD` in a repository with one commit, now exits 1 instead of reporting `fail_open`.

## A merge

A line from either parent was checked at that parent, or it is already published. Checking
the whole diff against the first parent checked it again, and once `main` carried test
fixtures that look like secrets, every local merge of `main` into an older branch was
refused (*measured 2026-10-03: merging `main` into #60's branch, three fixture lines from
#56*). So for a merge the hook applies the rule `.githooks/pre-push` applies to a pushed
merge:

- The index is diffed once per parent: `git diff-index --cached -p -M -U0 --text
  --no-ext-diff --no-textconv --no-color --no-relative <parent>`, read as [above](#how-a-diff-is-read). A
  line counts only when every one of those diffs adds it at the same place: same path, same
  line number in the index, same content. A line that is new against every parent is
  therefore always read. A line is skipped as soon as one parent's diff does not add it: that
  parent has the same content in the same file, or in a file that `-M` pairs with it as
  renamed. Content a parent has only in some other file is read.
- An octopus merge has one parent per `MERGE_HEAD` entry, plus `HEAD`. `MERGE_HEAD` is found
  with `git rev-parse --git-path`, so a linked worktree's own one is read.
- A new file name is a path no parent has. A path one parent already has passes, also when
  that parent renamed a file to it; a path the merge itself renames a file to is new.
- If `MERGE_HEAD` cannot be read, or a parent's diff cannot be read, the hook prints
  `Fehler: Merge: …` and refuses the commit rather than check less.
- With a TypeSafe key, Jev sees the same reduced diff and file list, not the first-parent
  diff. A merge that adds no lines of its own sends nothing and prints `Hinweis: Der Merge
  fügt keine Zeile hinzu, die gegen jeden Elternteil neu ist.` Jev's leak judgement still
  refuses at 0.5 or above. Whether Jev flags a masked `<iban>` is **not checked**.

`git merge --squash` writes no `MERGE_HEAD`, and its commit has one parent. It is a normal
commit, and every line it brings in is checked as its own.

## What enforces this, and what it does not enforce

`tools/imprint-dev/commitmsg_test.go` lets git run the real hook and the real
`ts-commit-check` in throwaway repositories, without a TypeSafe key. It covers a merge that
brings in only lines a parent has, a conflict resolved to one side, a conflict resolution and
an evil merge that add a new secret-shaped line or file name (below lines only one parent has,
in a file with a NUL byte, marked `-diff` or not UTF-8, after a lone CR, a form feed or
U+2028, and a file the merge renames), a merge that cannot be read, an octopus merge, a merge
in a linked worktree, and normal commits: a squash, `commit -a`, `commit <path>`, a
secret-shaped line in each of the cases the merge tests read plus under `color.ui=always` and
`diff.external`, and a file with a NUL byte but no leak shape, which passes.
`tools/typesafe/tests/test_no_leak.py` covers `added_lines` at every break character,
`--cached` on such files and a git error there, and range mode and `ts-pr-triage` on every
row of the table above but UTF-16 (`--cached` on the `diff.relative` row too), with the net diff
they would hand to TypeSafe, a commit git cannot read, a range git cannot resolve, and a
commit that `git replace` stands in for.

Not enforced:

- **The hook is opt-in per clone.** It runs only after `git config core.hooksPath .githooks`,
  the same line pre-push needs. It is fail-open: without `ts-commit-check` on `PATH` it
  checks nothing and exits 0.
- **`git commit --no-verify` and `git merge --no-verify` skip it silently.**
- **`MERGE_HEAD` is taken as it stands.** If someone writes it by hand and names an ancestor of
  `HEAD`, git can record a commit with one parent, while the hook still reads it as a merge.
  Only a hand edit of the git directory produces this; `--no-verify` is the shorter way past.
- **A clean `git cherry-pick` or `git revert` does not run it**, with or without the editor
  (`-e`); a conflicted one does, when `git commit` or `--continue` concludes it (*measured
  with git 2.55.0, 2026-10-03*).
- **A large compressed file can be refused without a leak in it**, by the hook, range mode
  and `ts-pr-triage` alike ([above](#how-a-diff-is-read)).
- **Range mode and `ts-pr-triage` run in no hook.** They check only when someone runs them.
- **A file in UTF-16 is not read as text.** Its ASCII letters arrive with a NUL byte between
  them, which no leak shape matches. A secret-shaped line in such a file passes `--cached`,
  range mode and `ts-pr-triage` alike (*measured with git 2.55.0, 2026-10-03, no TypeSafe
  key*).
