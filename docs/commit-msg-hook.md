# The commit-msg hook

The detail behind [The commit-msg hook](../README.md#the-commit-msg-hook) in the README.

`.githooks/commit-msg` runs `ts-commit-check --msg-file`, and its local leak alarm refuses the
commit (exit 1) when it finds a leak shape in what this table names:

| The commit being made | Message | Lines | File names |
|---|---|---|---|
| A normal commit: no `MERGE_HEAD` (`git commit`, and also `git merge --squash`, `cherry-pick`, `revert`) | all of it | every line the index adds against `HEAD` (`git diff --cached`) | every path the index changes against `HEAD` |
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
  against both parents is therefore always read. A line whose content a parent has
  elsewhere, at another line number or in another file, is read too.
- A file is read as text even when it holds a NUL byte or is marked binary. A byte that is
  not UTF-8 is read as U+FFFD rather than dropping the file.
- An octopus merge has one parent per `MERGE_HEAD` entry, plus `HEAD`. `MERGE_HEAD` is found
  with `git rev-parse --git-path`, so a linked worktree's own one is read.
- A new file name is a path no parent has. Renames are followed for lines (`-M`) but not for
  names, so a path renamed on one side and new to the other counts as new.
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
an evil merge that add a new secret-shaped line or file name, an octopus merge, a merge in a
linked worktree, and normal commits, a squash included.

Not enforced:

- **The hook is opt-in per clone.** It runs only after `git config core.hooksPath .githooks`,
  the same line pre-push needs. It is fail-open: without `ts-commit-check` on `PATH` it
  checks nothing and exits 0.
- **`git commit --no-verify` and `git merge --no-verify` skip it silently.**
- **A known gap, normal commits only** (*measured 2026-10-03*): if a staged file holds a byte
  that is not UTF-8, the whole diff reads as empty and the commit passes, even with a leak
  line in it. Merge mode reads such bytes.
