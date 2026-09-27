---
name: measure-before-asserting
description: "Look it up or run it before saying it; notes are a dated snapshot, not evidence. Use before stating or recording a foreign system's property, what a safeguard covers (careful ones too) or a changeable state (config, version, path), and when a check refutes your notes. Foreign-set values never come from storage; other ageing: knowledge-keeping. Triggers: 'stimmt das noch', 'is that still true'."
---

# Measure before asserting

**Anything that can be executed, queried or looked up is executed, queried or looked up
before it is said.** This covers properties of foreign systems and your own inventory alike:
whether a repository is public, which version is running, what a configuration file
contains, whether an enforcement actually fires, whether a path exists, how big something
is.

The cost is a few seconds. The alternative is a statement that sounds right.

## Where this bites, and where it does not

A rule that fires on every sentence gets ignored, which is the same outcome as not having
one. Three kinds of claim are worth the interruption:

- **A property of a system you do not control.** Someone else may have changed it since you
  last looked, and they owed you no notice.
- **What one of your own safeguards actually covers.** This is the class nobody audits; the
  section below on over-cautious assurances is about why.
- **A state that can change without your involvement** — a configuration, a version, a
  permission, a path.

A claim of any of those kinds that is about to be **written down** is the strongest trigger
of all, because writing is what converts it into something other people will quote
instead of check. Arithmetic you just did, a preference, a judgement call about what to
build next: not this skill's business.

## The values that are never answered from storage

One class of value deserves a procedure rather than a preference, and the question that
identifies it is **who is entitled to change this value?**

If a single outside body sets it and may change it without telling you — an authority, an
institution, a provider stating its own terms — then **no stored age is acceptable**, not even
a day. Published conditions, a stated term, a documented procedure, a price, a timetable, an
availability, who is responsible for which part: anything one body announces and can
re-announce without consulting you belongs here. Some rulebooks make that list binding
rather than illustrative, and yours is worth writing down as a list too — a criterion
re-applied from memory each time decides borderline cases inconsistently.

Four steps, and the order is part of the rule:

1. **Consult the stored state silently.** Saying it first is the mistake, not saying it at
   all: a figure that has been spoken lands, and a caveat after it does not retract it.
2. **Look it up now**, before answering.
3. **If the two differ, give both**, mark the looked-up one as the one that governs, keep the
   retrieved source, and **supersede the stored entry in the same move** — not afterwards, not
   as a note to self. Skipping this is the quiet failure: the answer is right, and the store
   still holds the wrong value for whoever reads it next.
4. **If nothing is found**, give the stored state *with its age*. An entry carrying no date is
   reported as undated. Never estimate one.

Say what the source does not settle. That often yields "not answerable" rather than a figure,
and "not answerable" is an answer.

**Where the stored state governs and the published one does not** — these follow from the same
criterion rather than from a list to be memorised: **anything you were a party to.** Terms
fixed by an agreement you hold — licence terms as issued to you, a quota or a price fixed in
an order you placed — because the conditions published today govern whoever agrees today and
not an agreement already made. Your own arrangements, measurements you took yourself. Values
your own store actively monitors. And private individuals, who are not looked up at all. If
nobody outside could have changed it without your involvement, the store is the better source
and the public page is the wrong one.

What drifts on a horizon instead of falling under this section — anything no single body sets
— belongs to `knowledge-keeping`.

## Your own documentation is not a measurement

It is a compilation with a date on it. What is written there may have become false since it
was entered, or may have been false from the start — and **both look identical when you read
it**. Quoting a property from your own rulebook is not checking it; it is passing it on.

The same applies to a comment, a README, a previous agent's summary, and the sentence you
yourself wrote three turns ago.

So **documentation names a place or a state only together with the date it was measured** —
a path, where something lives, a setting, a version, whether something is running, whether a
repository is visible, on your own machine as much as on a foreign one. An undated place or
state reads as permanent, and nothing tells the next reader that it may have moved. If you
cannot measure it now, do not write it down as a fact; write it as unmeasured.

## The failure mode is plausibility, not ignorance

You do not skip the check because you do not know. You skip it because the claim fits.

The pattern: a rulebook can assume a visibility that one query disproves. It states in
several places that a code repository is public and derives its sharpest safeguard from that
— a push is the channel with the highest consequence, not retractable, made only on request.
The statement stays internally consistent for years, matches how everyone speaks about it,
and produces a coherent picture. **That is exactly why nobody checks it.** One query to the
provider settles it in seconds, and the answer can be the opposite. The same pass tends to
fell a second assurance the same way: a safeguard the notes describe as in place is not in
place anywhere that would have to carry it. Nobody opens the configurations that would have
to carry it, because a safeguard that only slows things down is the last thing anyone thinks
to audit — the next section is about that class.

Nothing about that story is unusual. A false statement that contradicts its surroundings
gets caught. A false statement that fits does not.

## Over-cautious false statements are the dangerous class

An over-optimistic assurance fails on the first real run. An over-cautious one looks like
prudence and is never doubted — nobody audits a safeguard that only slows things down.

Its damage is real anyway. It prevents correct work, it points attention at the harmless
channel instead of the dangerous one, and when it finally falls it takes the whole rule with
it — including the parts that were justified for other reasons.

So the direction of an error tells you how it will be found. Only one of the two directions
is self-correcting.

## When a measurement refutes a rule

Correct the fact immediately. Do **not** drop the protective rule until you have checked its
*remaining* justifications. Usually it still holds — just for a different reason, and that
reason now needs to be written down explicitly.

The reverse is also true: a rule that turns out to rest on nothing but a false premise should
be retired out loud, not quietly left standing to be obeyed for no reason.

## Adjacent habits that come from the same root

**A named gap without a date becomes a property.** Wherever your text says "not enforced,"
"unmeasured," or "assumed," put a re-check date next to it. Without one, the gap stops being
a gap and turns into how the system simply is.

**A dated check note about a foreign surface goes stale silently.** "Checked on «date»"
describes a state that moves without your involvement. The remedy is not a more diligent list
but a checker — and until that exists, a sentence that says out loud the note will expire.

**Internal consistency is not a correctness proof.** A result can be contradiction-free and
wrong. It needs an independently computed comparison value; if there is none, that absence is
the finding.

**A retry is not a diagnostic instrument.** It hides the persistent cause. Whoever retries
names the time-dependent cause they are assuming first; if they cannot name one, looking is
the correct action.

## What actually enforces this

The central rule is a behaviour rule and that will not change. But this skill also carries a
procedure now, and a procedure has parts that a mechanism reaches — so the section is a table
like every other one here rather than a single sentence that writes all of it off.

| Rule | Enforcement |
|---|---|
| A claim was measured before it was spoken | **Behaviour rule**, and no mechanism is possible. No checker can determine whether a spoken claim was measured beforehand — it sees only the result, and a guessed value can happen to be right. This row is the reason this section used to say "nothing", and it is the only row for which that answer was ever correct. |
| The class of values never answered from storage is written down as a list | **Enforceable, not enforced**. A criterion re-applied from memory decides borderline cases inconsistently, which is why the section above asks for a list rather than a habit. A list is a file; a retrieval path can read it and mark an entry as belonging to the class. Nothing here ships one, and the rule deliberately stops at the shape, because a list naming real authorities would go stale faster here than in the system using it. |
| Documentation names places and states only with a measurement date — foreign surfaces and your own paths and settings alike | **Enforceable, not enforced**, and this is the cheapest row: scanning your own documents for a place or state that carries no measurement date is a short script, and the result is a number that can be tracked and driven down. Pick that as the metric rather than "did the agent check" — the first is observable and the second is not. A second mechanism reaches your own places and states: a check that compares the paths and settings the documentation names with the machine and reports every difference. Nothing here ships either. |
| A look-up that differs from the store supersedes the entry in the same move | **Enforceable, not enforced**. The quiet failure is an answer that is right while the store keeps the wrong value for the next reader. A write path that will not close a corrected answer without also writing the replacement is a real mechanism; so is a check that lists entries contradicted by a retrieval recorded later than they were. Neither exists here. |
| The stored state is consulted silently, before the look-up rather than after | **Behaviour rule**. The order is the rule, and nothing observes the order in which a party thought. A figure once spoken lands, and a caveat after it does not retract it — which is exactly the kind of failure no log shows. |
| An assurance says what it does **not** cover | **Behaviour rule**. Whether a stated limit is the real one is a judgement about meaning. A checker can see that a sentence about coverage exists; it cannot see that it is honest. |
| An unmeasurable thing is reported as unmeasured rather than guessed cautiously | **Behaviour rule**, and the one this skill exists for. A cautious-sounding wrong answer and a measured right one are the same shape on the page, which is why the over-cautious assurance is the dangerous class — nothing ever makes it fail. |
| Claude Code's changelog and the documentation it touches are read after every upgrade (a higher `major.minor.patch` than last recorded), and a dated report says what this plugin should use, adapt or drop | **Enforced** that the upgrade is noticed and a review offered: `hooks/update-watch.sh` (added 2026-09-27, after 0.5.0; on for every user, off only with `IMPRINT_UPDATE_WATCH=0`) compares the version at every fresh session start and, when it went up, puts one factual note in front of one session; a first run, a downgrade and a suffix-only change are only recorded. **Behaviour rule** that the review runs and the report is written; the session or the user can decline, and a line of the version history without its report is how a later reader sees that. That the subagent only reads, and treats what it fetches as data, is a behaviour rule as well: the note asks for it, and nothing restricts its tools; `foreign-material-reviewer`, whose tools are restricted, has no tool to fetch the sources. Details in the repository's `docs/update-watch.md`. |

*First measured 2026-09-15; measured again 2026-09-26 by listing this repository and reading
`hooks/hooks.json` and the checks table in `tools/imprint-dev/checks.go`: nothing in this
repository executes any of the rows marked *Enforceable, not enforced*. `SessionStart` and `SubagentStart` print the
core card; since 0.5.0, `SubagentStart` and `SubagentStop` each also run a hook that only
appends one measurement line — time, agent id, type, effort, and on a stop the transcript
path — to a log file, measuring that a subagent started or returned and enforcing nothing. Since
the update watch (2026-09-27, after 0.5.0), `SessionStart` also runs a hook that records the
Claude Code version and, unless switched off, enforces the noticing half of the last row
above. The
check tool `tools/imprint-dev` checks the plugin's own files — description lengths, the card
and its generated payloads, references to removed skills, the hooks file's shape, the plugin
version, that every enforcement-table row carries a known classification, and (with
`--release`) that no dated re-check is overdue; a workflow file under `.github/workflows/` runs
it with the tests on push and pull request wherever CI is enabled; and `.githooks/pre-push`,
active only in a clone that points `core.hooksPath` at it, checks commit identity and
personal-data shapes before a push. This replaces a note, measured 2026-09-26, that read "ships
two start hooks, SessionStart and SubagentStart" without the SubagentStop measuring hook that
had landed in 0.5.0 (PR #3) the same day. That earlier note's finding — that nothing executes
the enforceable rows — still held; only its inventory of what the tree ships was already stale.
Re-check by 2026-12-26.*

## A cheap check before you assert

- Can this be executed, queried or looked up in under a minute? Then it must be, now.
- Am I about to quote my own notes? Say so, and name their date.
- Does this claim feel obviously true? That is the trigger, not the exemption.
- Am I about to write an assurance? Then also write down what it does **not** cover.
- If I cannot measure it, do I say "unmeasured" rather than picking the cautious-sounding
  answer?
