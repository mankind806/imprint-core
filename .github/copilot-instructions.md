# Repository purpose

This repo is `imprint-core`, the public part of the imprint Claude Code plugin: a small
core card (`hooks/kernkarte.md`) injected at session and subagent start, the checks that
keep it consistent, and the skills built on top.

# What to check in review

- Read `CONTRIBUTING.md` first — it states what every pull request must satisfy
  (checks, evidence for new rules/skills, licensing, no personal data).
- `hooks/kernkarte.md` is generated content's source, not a place for ad hoc edits.
  A change to it must come with the regenerated `hooks/session-start.json` and
  `hooks/subagent-start.json` (`go run ./tools/imprint-dev gen`); flag a diff that
  touches the card without regenerating those two files, or that hand-edits them.
- Treat `go run ./tools/imprint-dev check --root .` (checks a–h, see
  `docs/core-card-and-checks.md`) as the authoritative review, not a suggestion.
  If CI shows it green, do not re-litigate what it already covers (card size,
  generated-file match, removed-skill references, plugin version, enforcement
  classification, overdue re-check dates).
- The README carries dated measurement sentences ("measured YYYY-MM-DD on Claude
  Code …"). Do not suggest rewording or softening them — they are dated snapshots,
  not general claims, and rewording changes their meaning.
- Never suggest adding a person's name, a local file path, a host name, or any
  address-shaped text to a commit message or a tracked file — the pre-push hook
  enforces this once a clone opts in, and it is a hard project rule either way.
- Commit messages and PR descriptions use a plain `Assisted-by: <tool or model>`
  trailer for AI help, never `Co-authored-by` or `Signed-off-by` with an address.

# Go style

- Standard library only in `tools/imprint-dev` — no new third-party dependencies.
- Keep functions small and tested; `go test ./...` in `tools/imprint-dev` must pass.

# Review tone

Give concrete findings tied to `file:line`. Skip style nitpicking that `gofmt` or
the checks above already cover.
