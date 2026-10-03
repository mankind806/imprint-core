# The commit-msg hook

The detail behind [The commit-msg hook](../README.md#the-commit-msg-hook) in the README.

`.githooks/commit-msg` runs `ts-commit-check --msg-file`, and its local leak alarm refuses the
commit (exit 1) when it finds a leak shape in what this table names:

| The commit being made | Message | Lines | File names |
|---|---|---|---|
| A normal commit: no `MERGE_HEAD` (`git commit`, the commit after `git merge --squash`, the commit that concludes a conflicted `cherry-pick` or `revert`) | all of it | every line the index adds against `HEAD` (`git diff --cached`) | every path the index changes against `HEAD` |
| A merge: `MERGE_HEAD` is there (`git merge`, `git pull`, the `git commit` that concludes a conflicted merge) | all of it | only the lines the index adds against **every** parent, `HEAD` and each `MERGE_HEAD` entry: conflict resolutions, and lines an evil merge writes itself | only the paths no parent has |

A leak shape here means an e-mail address or a value that really looks like a secret. The
exact rules are next to `local_alarm` in `tools/typesafe/ts_common.py`, and the exemptions
are listed in [`tools/typesafe/README.md`](../tools/typesafe/README.md#lokaler-leak-block-auch-ohne-key).
An IBAN, a phone number or a postal address is **not** a leak shape for this hook
(*measured 2026-10-03*). Those are shapes [the pre-push hook](pre-push-hook.md) matches, on the
way out.

## A merge

A line from either parent was checked at that parent, or it is already published. Checking
the whole diff against the first parent checked it again, and once `main` carried test
fixtures that look like secrets, every local merge of `main` into an older branch was
refused (*measured 2026-10-03: merging `main` into #60's branch, three fixture lines from
#56*). So for a merge the hook applies the rule `.githooks/pre-push` applies to a pushed
merge:

- The index is diffed once per parent: `git diff-index --cached -p -M -U0 --text
  --no-ext-diff --no-textconv --no-color <parent>`, with `GIT_DIFF_OPTS` dropped and
  `GIT_NO_REPLACE_OBJECTS=1`. A line counts only when every one of those diffs adds it at
  the same place: same path, same line number in the index, same content. A line that is new
  against every parent is therefore always read. A line is skipped as soon as one parent's
  diff does not add it, which takes that parent having the same content in the same file;
  content a parent has only in another file is read.
- A file is read as text even when it holds a NUL byte or is marked binary. A byte that is
  not UTF-8 is read as U+FFFD rather than dropping the file. A lone CR, a form feed, U+2028
  and the other characters Python's `str.splitlines()` breaks at are read as spaces, so the
  rest of a line after one still reaches the alarm.
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
an evil merge that add a new secret-shaped line or file name (also in a file that is not UTF-8,
and after a lone CR, a form feed or U+2028), an octopus merge, a merge in a linked worktree,
and normal commits, a squash included.

Not enforced:

- **The hook is opt-in per clone.** It runs only after `git config core.hooksPath .githooks`,
  the same line pre-push needs. It is fail-open: without `ts-commit-check` on `PATH` it
  checks nothing and exits 0.
- **`git commit --no-verify` and `git merge --no-verify` skip it silently.**
- **A clean `git cherry-pick` or `git revert` does not run it**, with or without the editor
  (`-e`); only the `git commit` that concludes a conflicted one does (*measured with git
  2.55.0, 2026-10-03*).
- **Known gaps, normal commits only** (*measured 2026-10-03*): if a staged file holds a byte
  that is not UTF-8, the whole diff reads as empty and the commit passes, even with a leak
  line in it; and the rest of a line after a lone CR, a form feed or U+2028 is not read.
  Merge mode reads both.
