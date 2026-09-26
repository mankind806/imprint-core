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

`main` only takes squash merges with the commit message left blank, so a line written only in
the pull request body never reaches the merge commit. A maintainer merging such a pull request
carries the line over explicitly, with a real blank line before it so it lands as its own line
rather than run into the summary — a literal `\n` inside a shell double-quoted string does not
produce one:

    gh pr merge <n> --squash --body "$(printf '%s\n\n%s' '<title or summary>' 'Assisted-by: <tool or model>')"

so the merged commit still carries the line.

Commit messages carry no addresses: no `Signed-off-by` or `Co-authored-by` lines with one,
and no other line that holds one. The pre-push hook, `.githooks/pre-push`, refuses a push
whose commit messages or tracked files contain an address of the kind mail uses, a few other
shapes of personal data, or an author or committer identity the clone has not declared. It
runs only in a clone that opted in (`git config core.hooksPath .githooks`); the README
section "The pre-push hook" says what it checks and what it does not.

## Keep it publishable

- No personal names besides the handle `mankind806`, no paths from your machine, no host
  names, no employer or product names, no links to private repositories.
- No screenshots from private or employer contexts.
- Example domains only under `.example`, `.test` or `.invalid`.
