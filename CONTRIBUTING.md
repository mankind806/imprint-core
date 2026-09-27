# Contributing

Changes arrive as pull requests, and every pull request lands on `main` as a squash merge.
Every pull request not opened by Claude on the owner's own behalf — including one from a
human contributor, Copilot, Dependabot or another agent — is merged only by the owner,
`mankind806`, by hand. Claude's own pull requests are merged by a Claude subagent once the
required `check` is green, formally through the owner's own account.

## How

1. Fork the repository and create a branch for one change.
2. Make the change and run the checks below.
3. Open a pull request against `main` and fill in the template.

## What is checked

Run both from the repository root before you open the pull request:

    go run ./tools/imprint-dev check --root .
    cd tools/imprint-dev && go test ./...

`check` has to exit 0 and the tests have to pass. `.github/workflows/check.yml` runs the same
two commands; run them locally rather than relying on CI.

## New rules and skills need evidence

A new rule, skill or enforcement claim has to show that it is needed: a real incident, told
generically, or a test that fails without it. Corrections and counterexamples to the present
text are welcome on the same terms.

## Licence: inbound = outbound

The repository is MIT-licensed (see `LICENSE`), and a contribution is licensed under the same
terms — inbound = outbound, as section D.6 of GitHub's Terms of Service sets out. There is no
CLA and no DCO sign-off.

By opening a pull request you confirm that you have the right to license the contribution
under MIT. If you contribute through your work, you confirm that your contract or your
employer's policies allow it.

## AI assistance and commit messages

If an AI tool helped, say so in the commit message or the pull request with one line,
`Assisted-by: <tool or model>`, naming the tool or model and never an address. This applies
to everyone, the owner included.

The repository setting `squash_merge_commit_message` is `PR_BODY` since 2026-09-27 (checked
against the GitHub API on that date): a squash merge on `main` uses the pull request body as
the merge commit message, not a blank one. The pull request text becomes the commit, so:

- The `Assisted-by: <tool or model>` line has to be the **last** line of the pull request
  text — trailers read from the bottom, and a line after it would bury it.
- If a tool adds its own line above `Assisted-by:` (for example a "🤖 Generated with ..."
  line), leave a blank line between that line and `Assisted-by:`. Git reads trailers as one
  unbroken block from the bottom of the text; a non-trailer line touching `Assisted-by:`
  keeps the whole block from being read as trailers at all, so the line is in the commit but
  `git log --format='%(trailers)'` comes back empty — this happened silently in #15
  (`git log -1 --format='%(trailers:key=Assisted-by)' 9736440` returns nothing). Keep
  `Assisted-by:` alone in the last paragraph, or sharing it only with other `Key: value`
  trailers.
- The pull request text carries no addresses, paths, host names or employer terms either,
  same as the rest of the checklist, because it is about to become part of the permanent
  commit history.
- Merging with an explicit `--body` to carry the line over by hand is superseded by this
  setting (it was needed only while the commit message was left blank) and should not be
  used to add a second, conflicting body; a plain `gh pr merge <n> --squash --delete-branch`
  now carries the pull request text — trailer included — into the merge commit on its own.

Commit messages carry no addresses: no `Signed-off-by` or `Co-authored-by` lines with one,
and no other line that holds one. The pre-push hook, `.githooks/pre-push`, refuses a push
whose commit messages or tracked files contain an address of the kind mail uses, a few other
shapes of personal data, or an author or committer identity the clone has not declared. It
runs only in a clone that opted in (`git config core.hooksPath .githooks`); the README
section "The pre-push hook" says what it checks and what it does not.

## Releasing

Bump `.claude-plugin/plugin.json`'s `version` (and a `version` field in
`.claude-plugin/marketplace.json`, only if one is present there) and add a dated sentence to
the README's Status section saying what the release adds, derived from `git log` between the
previous release tag and this one — no claim without a commit or PR to point at.

Before opening the release pull request, run `tools/arrival-test.sh` twice by hand (it is not
in CI — see `docs/core-card-and-checks.md`, "The arrival test" — because CI has no logged-in
`claude` account):

    tools/arrival-test.sh --plugin-dir <path to the release worktree>   # expected: pass
    tools/arrival-test.sh                                               # against whatever
                                                                         # is currently installed

Record both results, verbatim, in the pull request. A pass against the worktree and a fail
against the current install is normal and expected — it is the gap this release is about to
close, not a bug in the test. Only the owner's merge, followed by a marketplace update and a
restart on their own machine, actually closes it; re-run `tools/arrival-test.sh` (without
`--plugin-dir`) afterwards to confirm it did.

## Keep it publishable

- No personal names besides the handle `mankind806`, no paths from your machine, no host
  names, no employer or product names, no links to private repositories.
- No screenshots from private or employer contexts.
- Example domains only under `.example`, `.test` or `.invalid`.
