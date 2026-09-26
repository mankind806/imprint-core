---
name: knowledge-keeping
description: "Keeps recorded knowledge true: one canonical place per fact, source and method on entry, due date apart from recording date, supersede instead of overwrite, re-check looked-up values before quoting. Use when recording or correcting a fact, decision, source or date (Fakt, Entscheid, Quelle, Frist, Korrektur), or on 'merk dir', 'halte fest', 'notier', 'remember this', 'note that'."
---

# Knowledge keeping

Four rules for anything written down that outlives the session: a fact, a decision, a source,
a date, a correction, a note the user asked you to keep. Each rule says when it fires, what to
check and what enforces it. The reasoning, the worked cases and the full enforcement tables are
in `references/`, one file per rule.

## Before the four rules: which record wins, and where things live

- **The user's decision register outranks rule text.** Where an entry in the user's decision
  register contradicts a rule text — these skills, an always-loaded card, an older rule file —
  the register entry holds until the user answers. Leave the rule file unchanged, hold every
  irreversible step, and put the conflict to the user as a question quoting both wordings.
  *Enforced by:* nothing. Noticing the conflict is a behaviour rule; the answer is reserved to
  the user.
- **One place per kind of knowledge.** Decisions live only in the decision register. Working
  rules live only in the rule set: these skills and, where the setup has one, its always-loaded
  card. Preferences live where the user decided: neutral ones in that card, private ones in the
  user's private store. An auto-memory or a scratch note is an inbox whose entries get sorted
  into those homes, never a place of record. No second rulebook, and no cross-references that
  run in a circle. *Enforced by:* nothing; behaviour rule.

## 1. One canonical place

**Rule.** Every fact has exactly one authoritative place; every other view of it is generated,
linked or embedded, never copied. A wrong value is fixed at that place, never in something the
place produced. The scope is any fact with a changeable state — a status, a date, who is
responsible, what was decided — not only numbers. A fact about an entity lives on that
entity's own entry.

**Fires when** you are about to write a value into a second place (a summary, an index, a
dashboard, a status line, a README), decide where a changeable fact should live, find one fact
in two places disagreeing, or correct a value in something that was generated.

**Check.**
- Search for the subject before writing it. If it exists, update it there or point at it.
- After a fix, count the places that would have to change next time. More than one means the
  repair is not finished: aligning every copy keeps the duplication.
- Two copies disagree and you do not know which holds: that is a contradiction first (rule 3).
  Stop, show both, ask; align only afterwards.
- Reference material with no changeable state (a definition, a derivation) may repeat. A current
  figure inside it may not.

**Enforced by:** nothing here. Generating views from their source would make copying impossible
and is not built. Read `references/canonical-place.md` when deciding where a fact lives,
before embedding a value in a table or mid-sentence, or before building a duplicate check.

## 2. Provenance on entry

**Rule.** Every entry names where it came from and how it was obtained: read from, asked of,
measured with, computed from, inferred from, found by a search on a given date. A deadline or a
validity date is recorded as such, apart from the date of recording; if you have only one of
the two, say which one it is and that the other is unknown. Attribution sits next to the claim
it supports, not in a list at the end.

**The user's words and your reading are two origins.** Quote what the user said, with where it
was said, and mark your interpretation as a reading ("recorded as: …"). When the user later
confirms a reading, write a **new dated entry** with the user's own wording and its source. The
reading is not promoted in place: it stays marked as a reading and gets a pointer to the new
entry. This is not rule 4's in-place re-date — there value and origin stay the same; here the
origin changes from your inference to the user's words.

**Fires when** you write a fact, decision or figure into anything that outlives the session,
record a deadline or validity period, summarise several sources into one passage, record what
the user told you to keep ('merk dir', 'remember this'), or write that some mechanism enforces
something.

**Check.**
- Where from, and how? Could a stranger check this in a year, or only trust it?
- Is there a date somebody will act on, and does the entry say which kind of date it is?
- A search result without its search date cannot be aged.
- An assurance about a mechanism: name the file that enforces it, and open that file first.

**Enforced by:** nothing here. A required origin field and a two-date schema are enforceable
and not built; which date goes in which field stays a behaviour rule. Read
`references/provenance.md` when designing an entry format or recording a deadline.

## 3. Supersede, don't delete — facts and decisions only

**Scope.** This governs recorded facts and decisions: knowledge pages, decision registers,
evidence archives and evidence identifiers. It does not govern code. Code that is no longer
used is deleted, and version control is the archive. A transition period exists only at a seam
that other things call by name: a tool name, a schema, a service unit or a skill name.

**Rule.** An outdated fact or decision is superseded, not overwritten. The living line carries
the current value and a pointer to what it replaced; the replaced state stays readable, with its
date. That keeps one question answerable later: was the old value wrong, or right at the time
and then changed?

- Completion and the next step are two lines, never one.
- A value authoritative for an area is superseded everywhere it is authoritative.
- A new convention for recording replacements is no reason to migrate old entries.

**A contradiction stops the process.** When a new statement disagrees with a stored one, or two
stored copies disagree, and it is not clear which holds: change nothing, show both in full, let
the user decide, and keep the open point visible as its own line. Taking the newer value is the
tempting error. Not a contradiction: the party entitled to change the value did change it. That
is an update — supersede it on the spot. The decision-register case above is the one
contradiction with a provisional winner, and it still goes to the user.

**Fires when** you are about to type over a stored value, delete a completed item, write "done,
next …" into one line, take the newer of two disagreeing statements, or migrate a stock of
entries to a new form.

**Enforced by:** nothing here. Noticing a contradiction is a behaviour rule; resolving it is
reserved to the user. A commit-time check that refuses changes under a path is enforceable and
not built. Read `references/superseding.md` when choosing a replacement form, or when
unsure whether a difference is an update or a contradiction.

## 4. Knowledge ages

**Rule.** Recorded knowledge expires, and the trigger is use, never the calendar: when you reach
for an entry, its age is the question.

1. **First ask who may change the value.** If a single outside body sets it and may change it
   without telling you, no stored age is acceptable. Do not quote it from storage; look it up
   now (`measure-before-asserting`) and supersede the stored state if it differs.
2. **Then the kind of evidence.** A value read off a document does not age; to use it as today's
   value, establish today's value afresh. A value obtained by a look-up ages.
3. **Recent dates are the risk.** An entry from this year reads as current; that is the one to
   check.
4. **Confirming is not superseding.** A re-check that finds the value unchanged re-dates the
   origin note in place, keeping the first date and adding the latest confirmation. Once the
   statement itself changes, rule 3 applies.
5. **No origin marker means "not checkable",** which is not "fine". Count it and report it apart.

Horizons come from how fast the subject moves: a short one on look-up entries, treated as an
obligation, and a longer one on the surrounding page, as a reminder to read it through. The
numbers are a setting for your domain, not part of the rule.

**Fires when** you are about to quote a stored figure, an entry looks fresh, a re-check comes
back unchanged, or you set how long a class of knowledge stays usable.

**Enforced by:** nothing here. Listing entries past their horizon or without any origin is
enforceable and not built; whether a value is one an outside body sets is a behaviour rule.
Read `references/ageing.md` before setting a horizon or when the kind of evidence is
unclear.

## What enforces this, measured

This plugin ships nothing that runs when knowledge is written or read. *First measured
2026-09-15; measured again 2026-09-26 by listing this repository and reading `hooks/hooks.json`
and the checks table in `tools/imprint-dev/checks.go`. `SessionStart` and `SubagentStart` print
the core card; since 0.5.0, `SubagentStart` and `SubagentStop` each also run a hook that only
appends one measurement line — time, agent id, type, effort, and on a stop the transcript
path — to a log file, measuring that a subagent started or returned without enforcing anything;
the check tool `tools/imprint-dev` checks the plugin's own files, including that every
enforcement-table row carries a known classification and (with `--release`) that no dated
re-check is overdue; a workflow file under `.github/workflows/` runs it with the tests wherever
CI is enabled; and `.githooks/pre-push`, active only in a clone that points `core.hooksPath` at
it, checks commit identity and personal-data shapes before a push. None of them reads a
knowledge page, a register or an entry, so every "enforceable" row in `references/` is unbuilt.
This replaces two earlier notes: "the only script this repository ships is a pre-push hook"
(true when written, false the same day once the hooks and the check tool landed) and, measured
2026-09-26, "ships two start hooks, SessionStart and SubagentStart" without the SubagentStop
measuring hook that had landed in 0.5.0 (PR #3) that same day. Re-check by 2026-12-26.*

## Before you write a fact down

- Does it already exist somewhere? Did I look?
- Where is it from, how was it obtained, and which date is which?
- Is this the user's wording or my reading of it?
- Am I typing over something? Where does the old value go?
- Is this a contradiction, or an update by somebody entitled to make it?
- Who may change this value? If an outside body, why am I quoting it from storage?
