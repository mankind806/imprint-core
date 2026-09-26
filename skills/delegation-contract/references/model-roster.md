# Model roster

**Checked: 2026-09-26.** What this date certifies is the assignment of roles to model
*families* below — not a concrete model version. A family name (in Claude Code the aliases
`sonnet`, `opus`, `haiku`) resolves to that family's newest model, and the provider moves it.
The version behind an alias is deliberately not written here: a stored version is a second
number, and it would go stale without anything saying so.

| Role | Model family | Effort | What it covers |
|---|---|---|---|
| Lead | The session's own model — whatever the person started the session with | Not chosen here | Talking with the person, deciding, writing dispatches. This roster does not choose it. |
| Reader of foreign material | Sonnet family | Not yet measured | Triage of material you did not write, extraction against a schema. The plugin's `foreign-material-reviewer` agent names `model: sonnet` in its frontmatter, which matches. |
| Judgement before an irreversible step | Opus family | Not yet measured | Review before anything final or outward-facing, a security boundary, a consistency guarantee, an architectural commitment. |
| Cheap mechanical work | Haiku family — **only once measured** | Not yet measured | Work whose output a schema, a test or a comparison can judge. The family is used only after a dispatch to it has been measured to actually run on it. |

## Task → model → effort, from measurement

The rows above assign roles to families. The finer assignment — which kind of
task goes to which model at which effort — comes only from the per-dispatch numbers the skill
asks for (task kind, model read from the transcript, effort, duration, quality), and it is
**not yet measured**: no row of it exists, and none is written here before its numbers do.

## Where a family is not enough

A role whose output is stored and is only comparable within one model version pins an exact
version instead of a family. An embedding index is the standard case: a new embedding model
means a new index, and an alias that moved underneath it would mix two vector spaces without
an error.

## An instance may narrow this

An instance may restrict which models run in sessions that see private data — for example,
by keeping a model family out of those sessions. Such a restriction lives in
that instance's own configuration, not here, and within those sessions it takes precedence
over this table.

## Keeping it current

- **The next check date is computed from the check date above, never stored.** The roster is
  due as soon as a model generation newer than that date exists in any family it names, and
  the check is a new round of per-dispatch measurements on that generation, not a reading of
  release notes. Nothing detects a new generation; noticing one is a behaviour rule.
- **A missing date counts as due now.**
- **An overdue check is a finding**, and goes wherever findings go.
- **A check that confirms the table unchanged re-dates it**; it does not rewrite the rows.
- **Read the diff of any edit to this table as a possible rule change.** A date guards an
  entry that went stale, not an entry that vanished — see
  [model-routing.md](model-routing.md) for the case where a shortening silently removed a tier.

**Enforcement: enforceable, not enforced.** No check reads this date yet. A small script
that computes the due date and fails on a missing or past one would turn the list above into
a mechanism; it would also compare this table with the `model:` fields in the plugin's agent
files, since those are a second place naming a model.
