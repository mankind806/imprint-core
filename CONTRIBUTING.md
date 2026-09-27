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
to everyone, the owner included. Pure handiwork with no AI feature named anywhere in the text
owes no such line and may go without one; the moment the text already names one — a 🤖 line, a
`Generated with ...` line, a `Co-Authored-By:` trailer naming an AI tool, or an `Assisted-by:`
mention at all, filled in or not — the line has to be there, filled in, and last (below). The
pull request template's own `Assisted-by:` placeholder, left blank, already counts as such a
mention: fill it in, or delete the line (and the heading above it) if nothing helped.

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

## Commit identity and messages, checked in CI

`.github/workflows/check.yml`'s `check` job also runs `imprint-dev commit-check` over every
pull request and every push that lands on `main` (`push` is restricted to `branches: [main]`
for exactly this: a feature-branch push would otherwise report its own, separately green
`check` run on the same commit the pull request checks, skipping `commit-check` on that run
because it is not the pull request or `main` — two runs of the same required check name on one
SHA, one of them silently skipping the part that matters, reads as ambiguous, not as green for
a reason). It checks only the new commits — a pull request's `base..head`, or, on a push to
`main`, the one new commit — never the repository's existing history (N79); see
`tools/imprint-dev/commitcheck.go` for the exact rules. For each of those commits:

- The author and the committer (name and email, exactly as `git log` reports them) each have
  to be in `.imprint/commit.conf`: the owner's own identity, the GitHub web-flow identity a
  merge, a web-UI edit, an "Update branch", or an accepted Copilot suggestion produces as
  committer, or a bot listed there, gated to its own pull request or to a push that
  squash-merged it. Whether a name+email match alone is enough depends on where the pull
  request's head branch lives, `--same-repo`
  (`github.event.pull_request.head.repo.full_name == github.repository`, computed by the
  workflow's own `${{ }}` expression, never a value read from the fork): a same-repo pull
  request — Dependabot's own branches, the owner's own branches, or a commit the owner added
  straight onto someone else's such branch — can only carry commits from someone who already
  has write access here, so a listed identity's name and email are enough on their own, the
  same as on a push. Only a fork pull request, where anyone can set their own
  `user.name`/`user.email` to anything on their own commits, still needs the login
  `.imprint/commit.conf` binds to that identity to equal the login that actually opened the
  pull request (GitHub's own event data, never anything the pull request's own commits could
  shape). A match that fails only this login binding is reported as "external contributions
  need an entry in `.imprint/commit.conf` added by the owner", not as an unrecognized identity,
  since name and email did match. Measured against a real, still-open Dependabot pull request
  (`gh api repos/<owner>/<repo>/pulls/<n>/commits` on cli/cli #14487 and actions/checkout
  #2578): its own commits already carry the web-flow identity as committer, not
  `dependabot[bot]` itself, even before any merge — this is exactly the same-repo case, not the
  merge-only one the previous wording assumed.
- The commit message carries no `Claude-Session:` line (nor an equivalent spelling — spaces,
  underscores or hyphens between the words, a zero-width character spliced into either one, or
  a Markdown backslash escape are all still read as the same line), no `claude.ai/code/session_`
  URL (nor its `%5f`-encoded underscore), and no email address the `.imprint/commit.conf` this
  same range is checked against does not itself already declare (the owner's own noreply
  address, quoted in their own `Signed-off-by`, adds nothing that address and every commit's
  own metadata do not already carry) — now also enforced here for everyone, except that the
  address check alone is skipped for a commit whose author or committer was let in only
  through a `botAuthors` entry (see "bot pull requests" below); the two session-link checks
  stay on regardless. Unicode normalization beyond what `normalizeForSession` already does
  (CRLF/lone-CR line endings, zero-width characters, a Markdown backslash escape — see
  `tools/imprint-dev/commitcheck.go`) is not implemented, because it needs a library beyond the
  standard one this module depends on. Left open, not yet measured against a real incident:
  a session marker split by U+2010 (hyphen), U+00AD (soft hyphen), a full-width variant of a
  letter or the colon, `%2E` in place of a URL's literal dot, `//` in place of `/`, or a URL
  wrapped across two lines by a renderer; an AI tool named only as Devin or aider, which
  `ccCoAuthoredAIRE` and `ccAIToolNameRE` do not list (no incident or documented identity
  evidenced yet, the same bar CONTRIBUTING already holds a bot identity to); and U+3164
  (Hangul filler) as an `Assisted-by:` value, which reads as visually blank but is not itself
  whitespace to Go's `strings.TrimSpace` or covered by `ccZeroWidthRE`.
- If the commit message discloses AI assistance at all — a 🤖 line, a `Generated with/by/using`
  sentence that also names an AI tool or the word "AI" (not a code generator's own unrelated
  boilerplate, such as `protoc`, `stringer` or `mockgen` output — N5), `Co-Authored-By:` naming
  Claude or Copilot, or any `Assisted-by:` mention, filled in or not — its own last non-blank
  line has to be a well-formed, non-empty `Assisted-by:` trailer, read as such by `git
  interpret-trailers`; an empty value (the pull request template's own unfilled placeholder
  counts) is its own finding, worded as fill in or remove.

On a pull-request event, `.imprint/commit.conf` itself is read from the base commit
(`--conf-rev "$BASE_SHA"`, which runs `git show base:.imprint/commit.conf` internally rather
than from the pull request's own tree), so a pull request cannot grant itself an entry by
editing the file it is checked against. This has no fallback to the pull request's own tree
copy: a base commit without the file fails the check outright (rc=2, a clear message naming
the missing path and rev) rather than letting a pull request supply the file's first copy
itself, which would be exactly the self-granted entry this rule exists to refuse (N4;
`.imprint/commit.conf` has lived on `main` since #18, so this is not expected to fire in
practice). This closes only the list itself — the checking code and the workflow file that
runs it still come from the pull request's own tree, so a pull request that edits
`tools/imprint-dev/commitcheck.go` or `.github/workflows/check.yml` together with
`.imprint/commit.conf` is not stopped by this alone. Closing that the rest of the way needs
`CODEOWNERS` with required review on those paths, which this repository does not have; left as
the owner's own call, not decided here.

The same message rules run over the pull request's title and body together, joined as
title-blank line-body — the exact shape a squash merge turns them into (see "AI assistance and
commit messages" above), so an AI marker or an address in the title alone is caught the same
as one in the body — with the Assisted-by rule's own pull-request-only addition: if a 🤖 line
appears anywhere, the text's very last three lines have to be exactly that line, one blank
line, then `Assisted-by:` — CONTRIBUTING's own required shape above, checked here rather than
only described. Naming an AI tool also covers a few more shapes than a literal "Assisted-by":
"Generated with", "Generated by" or "Generated using" *in the same sentence as* an AI tool,
lab or the word "AI" itself — CONTRIBUTING's own canonical "Generated with [Claude Code](...)"
line included, since "Claude" is right there in its own brackets, but not a code generator's
own unrelated boilerplate such as "Code generated by protoc-gen-go. DO NOT EDIT." (N5) — a
`Co-Authored-By:` naming Claude, Copilot, Cursor, Gemini, Codex, ChatGPT or OpenAI, the robot
emoji with or without a trailing variation selector, and an Assisted-by value made only of
zero-width characters or non-breaking spaces (reads as empty to a person, so it is treated as
empty).

**Bot pull requests are exempt from every pull-request-body check outright**, Dependabot's
today: their body is generated by the bot, not written by a repository contributor, and can
embed a third party's changelog with addresses nobody here can edit. This is the same
reasoning as the address exception for the fixed `github-actions[bot]` system address in a
generated file (N102) — a fixed, non-personal, machine-generated address or link is not the
kind of exposure either rule exists to catch. Their commit messages get a narrower version of
the same exemption: the email-address check *and* the Assisted-by check are both skipped, and
only for a commit whose author or committer was let in through a `botAuthors` entry — never
for a commit by the owner or by a human contributor, and never for the two session-link
checks, which stay on regardless of who committed.

**A pull request from anyone else — a human contributor, Copilot, or another agent — is not
added to `.imprint/commit.conf` by a label, a setting, or anything the check itself decides.**
Its author and committer are checked exactly like anyone else's, and it fails the check the
same way a stranger's would if it is not already listed, and it does not pass by spoofing the
owner's name and email onto its own commits either (see the login binding above). "Fremde PRs
du, eigene Claude" (see above) already means the owner merges it by hand regardless of a green
check; extending the list first — through the owner's own pull request against `main` — is how
the owner then lets a repeat contributor's future pull requests pass on their own. That
contributor's pull request needs a fresh event after the list changes (closing and reopening
it is the clean way; a plain re-run reuses the original event's data): read the list from the
event's own base commit above means an already-open pull request's later re-run would still
read the list as it stood before the owner's addition merged. Recording a contributor's real
name and address (and now also their login) in `.imprint/commit.conf` for that purpose runs
against "no personal names besides the handle `mankind806`" a few lines below; the owner
decides case by case, and a handle-plus-noreply-address identity, where the platform offers
one, avoids the conflict.

As of this writing, no bot beyond Dependabot is listed. GitHub's own coding-agent
documentation does not pin the Copilot coding agent's commit identity to a fixed address —
by GitHub's own account it is a `Copilot`-named bot account, but the numbered noreply address
underneath has varied across rollouts — and no official GitHub documentation states a fixed
git identity for a Claude-based partner agent at all. Either one is added only once its
identity is confirmed in official documentation and evidenced in this repository's own
history, the same rule already written into `.imprint/commit.conf` for Dependabot.

`imprint-dev commit-check` refuses to run against a shallow git clone (`git rev-parse
--is-shallow-repository`) rather than let a commit range silently resolve against history that
was never fetched; `check.yml`'s checkout already sets `fetch-depth: 0` for this.

A committer identity such as the GitHub web-flow one above is exactly as spoofable, as git
metadata, as any other name and email a clone's own configuration sets — nothing in git itself
ties it to a real merge. What actually does is the repository's ruleset (pull-request-only,
squash-merge-only): nothing but GitHub's own merge action can make a commit carry that
identity before such a merge happens, so a fork's own commits, checked while its pull request
is still open, never legitimately show it — and now cannot claim to, either, since a
pull-request event never satisfies this identity's login binding (it has none, so it is never
trusted while `--pr-author` is set; see above).

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
