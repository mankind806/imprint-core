# Core card and checks

The detail behind [Core card and checks](../README.md#core-card-and-checks) in the README,
where the measurement of the card's arrival is recorded.

`hooks/kernkarte.md` is the plugin's core card: a short, plain-text summary of the
foundation layer — who leads, one writer per worktree, review before anything ships, measure
before asserting, and the rest of the handful of lines the skills argue for at length. Two
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
saying so on the same line. It checks that every row of an enforcement table in the skills
(a table with an *Enforcement* column) names at least one known classification, and none
unknown, in bold: **Enforced**, **Enforceable, not enforced**, **Behaviour rule** or
**Reserved to a person**. A row may name more than one, one per rule half it covers.
It lists every *Re-check by* date that has passed as a warning that leaves the exit code at
`0`, and only `check --release` turns such a date into a violation, because an overdue
re-check blocks a release and never the everyday test run (`--today YYYY-MM-DD` sets the day
it measures against). It also supports `--sarif` output for CI. The command exits `0`
clean, `1` on a violation, and `2` if a check itself could not run. `.github/workflows/check.yml` runs
`go test` and this check on every push and pull request; CI runs wherever GitHub Actions is
enabled.
