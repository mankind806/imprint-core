# The skills, the agent and the two layers

The detail behind [What's in the box](../README.md#whats-in-the-box) in the README.

## The four states

Four skills and one agent, in the two layers described below, plus two plugin hooks that
inject a summary of them at session and subagent start (see [Core card and
checks](core-card-and-checks.md)) and a hook that records how long each subagent ran (see
[Measuring subagents](measuring-subagents.md)). Each skill carries a section
on what actually enforces it, and sorts every rule it holds into one of four states:
**enforced** by something that really stops you, **enforceable, not enforced** where a
mechanism is possible and nobody has built it, **reserved to a person** where the rule's
content *is* a person's decision rather than a mechanism nobody wrote, and a plain **behaviour
rule** that holds only as long as the discipline does.

The second state is the one usually left out, and it is the most useful one — it is a list of
the places where a few lines of tooling would pay. The fourth state started as three and gained
its extra member from a rule that would not fit: the superseding half of `knowledge-keeping`
argues for it at length, including the fact that some rulebooks file that particular rule
one stage lower, and why that is worth saying rather than smoothing over. The two states
fail differently — an unbuilt mechanism fails by never being built, a consent step fails by
somebody deciding it was obvious enough to skip — and only one of the two is repaired by
writing code.

## The skills

- **`delegation-contract`** – who leads and who advises, and how a delegated task tells the
  receiver which of the two it is – a delegation header, not the flag you were invoked with.
  Why the leading session only orchestrates and hands even the smallest edit to a subagent.
  The header to put in front of a dispatch. Readers in parallel, every writer in its own
  worktree, and results merged one at a time with the tests run after each. Why the rules
  reach a subagent only if something carries them there. Why a subagent's own success report
  does not count as evidence that it wrote anything, and why a required human yes does not
  travel with a delegation. How deep a review goes, by risk. How to set up a second opinion
  so it is worth having – staged disclosure, a fresh context rather than a context-inheriting
  dispatch, an independent *measurement* for a closed question, and disagreement put in front
  of a person as a table of claims instead of averaged away. Which model the dispatch goes to
  – mechanism downward, judgement upward, the cheapest one that clearly passes – and a dated
  roster of which model family does which job.

- **`measure-before-asserting`** – anything that can be executed, queried or looked up is,
  before it is said. Your own notes are a snapshot with a date on them, not a measurement.
  The failure mode is plausibility rather than ignorance, and an over-cautious false
  assurance is the dangerous kind because nothing ever makes it fail.

- **`session-handover`** – closing a session as the last step of the work rather than stopping
  mid-air. The four things that are lost if nobody writes them down, and why the one everybody
  does is the least valuable: uncommitted files are visible, while a paused job and an unwritten
  decision are silent. Why the next step is the expensive item — it cannot be re-measured, only
  re-thought. Why the trigger is the signal rather than a particular phrase — a mechanism keyed
  on the phrase would make the rule worse, while one keyed on the *event* is buildable and
  named here, along with the four observable moments that are the floor.
  Why there is no handover note: each fact goes to its canonical place, the next session
  measures the state instead of reading it, and only the next step with its reason is left in
  the closing message. And what the close does when you are the advisor rather than the one
  holding the pen.

**What changed in 0.4.0:** `one-canonical-place`, `provenance-on-entry`, `supersede-dont-delete` and `knowledge-ages` merged into one skill, `knowledge-keeping`, below; `blind-first-pass` had already merged into `delegation-contract` above.

**What changed in 0.5.0:** a measuring hook now also runs at `SubagentStart`, alongside the
card hook already there, and at the new `SubagentStop`; it only appends a measurement line
per event — see [Measuring subagents](measuring-subagents.md). Two `imprint-dev check` rules
were added: that every Enforcement-table row carries a known classification, and that no
dated re-check
is overdue when run with `--release`.

One skill follows: `knowledge-keeping`, the knowledge layer. The read order the four merged
rules were written in still holds inside it — each part assumes the one before it, and the
last is unusable without the second — and their fuller text now lives under
`skills/knowledge-keeping/references/`.

- **`knowledge-keeping`** – one authoritative place per fact, with every other view
  generated, linked or embedded rather than copied, because aligning drifted copies
  perpetuates the defect instead of repairing it — the scope is any changeable fact, not
  only numbers, and the habit that actually carries the rule is a search performed before
  writing rather than a check performed afterwards. Provenance recorded when an entry is
  written, with *how* it was obtained as the load-bearing half — read, heard, measured,
  computed or inferred are five different futures for the same number — and a due date kept
  apart from the date of recording, since attribution collected at the foot of a page is
  attribution destroyed. Superseding instead of deleting, so the one question an overwrite
  makes unanswerable stays answerable: was the old value wrong, or right at the time and
  then changed; completion and follow-up as two lines, and a contradiction as a full stop
  rather than a merge. And expiry triggered by use rather than by a schedule, with the
  dangerous entry being the recent-looking one, routed by who is entitled to change a value —
  the question that separates what may expire from what must never be quoted from storage at
  all — with confirming an unchanged entry recorded as a re-check rather than misreported as
  a change.

## The agent

- **`foreign-material-reviewer`** (agent, unchanged in 0.4.0) – a read-only triage role for material you did not
  write. Its `tools:` frontmatter is an allowlist of `Read`, `Grep` and `Glob`, which is the
  point: a prompt asking an agent to stay read-only is a behaviour rule, and behaviour rules
  are broken by exactly the input this agent exists to handle. What it does *not* close is
  named in its own description.

## The two layers

**Agent collaboration is the foundation, and it shipped first.** Who leads and who advises,
and how a delegated task tells the receiver which of the two it is. Readers in parallel,
every writer in its own worktree, results merged one at a time with tests. When a second
opinion adds information and when it only adds agreement – and why a closed, checkable
question wants an independent measurement rather than another opinion. How to put disagreement
in front of a human instead of averaging it away. Escalating to a stronger voice rather than
resampling the same one after it has already failed twice. And, at the other end of the same
work, closing a session so that the next one inherits a state rather than a puzzle — which
belongs in this layer rather than the one above it, because what a session hands over is the
work itself and not the knowledge it happened to record.

**A knowledge system sits on top, and four of its rules now ship, as one skill,
`knowledge-keeping`.** One canonical place per
fact, with every other view generated, linked or embedded rather than copied. Provenance on
every entry, including how it was obtained, and a date something is due kept apart from the
date it was recorded. Superseding instead of deleting, so the replaced state stays provable
after the visible one changes. Knowledge that expires when it is reached for rather than on a
schedule, routed by the question of who is entitled to change a value. All four lean on a
standing preference for measuring a property over citing your own notes about it, which
shipped first, as `measure-before-asserting` — a skill of the collaboration layer, not a fifth
rule of this one.

**One item that stood in this list has been narrowed rather than built, because practice
refuted it.** It asked for *gates that stop an action rather than warn about it*, as a
universal. It does not hold as one: an unattended run, with no person in the loop, should
fail closed on a finding, while a supervised run can perform the same checks in full and only
warn, deliberately, even where the finding is in executable code. The defensible version of the rule is therefore narrower and
has a measurement in it: **a gate blocks when nobody is watching and warns when somebody is,
and which of the two you are in is measured rather than assumed.** A rule stated more
strongly than that gets switched off by the first person it interrupts, which leaves neither
a gate nor a warning.

The other half of that item — *an explicit note wherever nothing but discipline holds a rule
in place* — was always the stronger half, and it is already delivered: it is the four-state
section that every skill here carries, and the reason the middle state has its own name.

The second layer needs the first, which is why it is second. A knowledge base with several
writers and no rule about who holds the pen produces contradictory states that nobody
reports — and every rule in the knowledge layer is about keeping a fact answerable, which a
silent conflicting write defeats before any of them get a turn.
