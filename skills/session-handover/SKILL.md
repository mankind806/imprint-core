---
name: session-handover
description: "Closes a session as the last step of the work: runnable work committed by whoever holds the pen, paused state restored or named, each fact in its canonical place, no note, next step and reason in the closing message. Use when stopping, context is nearly full, a delegated task returns, or agreed work has no next item, not each turn. Triggers: 'that's it for today', 'Schluss für heute', 'Übergabe'."
---

# Session handover

**Closing a session is the last step of the work, not the end of it.** Whatever exists only in
the conversation stops existing when the conversation does, and that is true whether the
session ends because somebody said so, because time ran out, or because the context window
filled up.

So the close is work, and it has a deliverable: a state somebody — possibly you, with no
memory of this — can pick up.

## What is lost if nobody writes it down

Four things, and none of them can be recovered by looking. Each has to be worked out again.
Three of the four are silent, which is the asymmetry the next section is about.

- **Half-finished changes in the working tree.** At the next start they look like an
  unexplained intermediate state, and nothing says whether they are deliberate.
- **The changed operating state.** A paused timer, an opened maintenance window, a check
  switched off to get something done. These do not announce themselves.
- **The next step.** What would be done next and *why that*. The most expensive item in the
  list, because it is the product of thinking rather than of measurement.
- **Decisions taken and recorded nowhere.** They come back up at the next run, and the person
  has to answer the same question twice — which is worse than merely wasteful, because the
  second answer may differ from the first and nothing will notice.

**The rule:** before a session ends, make the state durable. Runnable work committed — by
whoever holds the pen. An advisor with no write permission does not acquire it here: it
*reports* the uncommitted state, names the paths, and leaves the commit to the party that
holds the write right. A handover that breaks the one-writer invariant to satisfy itself has
cost more than it saved; `delegation-contract` holds that invariant. The operating state
restored, or named explicitly where it cannot be. The resumption point left in words, in the
closing message rather than in a note file. Every decision that lives only in the transcript
written to the place it belongs.

Where the close leaves a command for a person to run, show it **visibly and one at a time**
rather than bundled into a paragraph. A list of four commands in prose is read as background.

## Only one of the four losses is visible

This is the asymmetry that makes the rule counter-intuitive, and it is why the item everybody
does is the least valuable one.

Of the four, exactly one announces itself. Uncommitted files are **visible**: the tooling
announces them, and the next session trips over them immediately. Of the three kinds of
*state* the list names, two are silent — a paused timer does not report that it is paused, and
a suppressed check looks exactly like a check that passed; an unwritten decision is silent in
the same way, and worse, because nothing anywhere records that it was ever taken. The fourth
loss is not a state at all but the reasoning — the next step and *why that one* — and it is
the quietest of the lot, because nothing was ever there to fall silent.

The consequence: the one loss class that has tooling behind it is the one you would have caught
anyway, and the three with nothing behind them are the ones a habit has to carry. If a handover
routine only ever produces a commit, it has addressed the class that needed it least.

## Why this is an invariant and not a courtesy

A system whose sessions hand over is the same system across any number of sessions. One whose
sessions simply stop decomposes into unconnected episodes, and every episode begins with
reconstruction.

The arithmetic is the whole argument. **The cost of handing over is constant and small. The
cost of reconstruction grows with the gap and is never complete** — some of what was in the
previous session is not recoverable at any price, only re-decidable, and re-deciding is how a
system quietly changes direction without anyone choosing to.

## The next session measures, it does not read a note

There is no handover note. A note would be a second copy of facts that already have a place,
and it starts ageing the moment it is written (`knowledge-keeping` holds the one-place rule).
So the close writes each fact to its canonical place:

- **Work** goes into git, committed on a branch by whoever holds the pen.
- **Decisions and changes to the operating state** go into the decision register, each with
  the way back — how to undo it, and by when.
- **Open questions** go to the place kept for open questions, marked open.
- **Measurements** go to the place kept for measurements.

The next session establishes the state by measuring rather than by reading a summary of it:
which branches and worktrees exist, what `git log` says, which entries are marked open.

What cannot be measured is the next step and *why that one*. It goes into the closing message,
as the start prompt for the next session, and stays in the chat. It points at the places above
and names the next step with its reason; it does not copy what the places hold.

## The signal, not the word

"Stop", "that's enough for today", a terminal instruction, a quiet trailing-off, a context
window with little left in it: all of these are the same signal. Implementing this rule as a
trigger on a particular phrase implements it at its least important point, and it will be right
most of the time, which is what keeps the gap open.

The reliable form is to ask the question at the moment the *work* changes shape rather than at
the moment a particular sentence arrives: am I about to stop being able to add to this?

**A signal has to be nameable or the rule never fires reliably.** "Ask at the right moment" with
no observable attached is a rule that is either never triggered or triggered at every turn. So
name the moments. These four are observable without judgement, and the question is asked at
each of them as a minimum:

- **An explicit terminal instruction**, in whatever words — the easy case, and the only one a
  phrase trigger would catch.
- **A context window near its limit**, on whatever indicator the harness exposes. The threshold
  is the point at which the close itself would no longer fit: a handover you cannot finish is
  not a handover, so the trigger has to come before the last usable stretch rather than at
  exhaustion. Where the harness signals an imminent compaction, that signal *is* the threshold.
- **A delegated task reaching its return.** For a subagent the return is the session end; there
  is no later moment.
- **The last item of the agreed piece of work being done**, with nothing named to follow it.

The question is what these trigger; the *action* still depends on who you are. An advisor's
close is a report. A lead never writes itself: its close has the writer holding the worktree
commit there, and the result is merged only after the check is green, which nothing this
plugin ships enforces (`delegation-contract` holds the merge rules). The asking is
unconditional, the committing is not.

What stays judgement is only the residue: a quiet trailing-off, an ambiguous pause. The four
above are the floor, not the ceiling, and a rule with a floor is one that can be checked.

## A case of this shape

Told as a shape, not as an incident — it is the failure the rule is built against, and an
example that presents itself as an event owes a source it does not have here.

A session pauses a scheduled job so that a long operation can run without interference, and
switches off one check that was firing on a known-good intermediate state. The work goes well.
The session ends on a commit, tidily, and the commit is genuinely complete.

Days pass. The scheduled job has not run, and nothing reports that, because a job that is not
running produces no output — which is indistinguishable from a job that ran and found nothing.
The check is still off, so the class of problem it existed to catch accumulates in silence. The
next session sees a clean tree, a green status and no open question, and has no reason to look
for either.

**The commit was the visible item. The two silent ones were the whole cost.** Both would have
been a sentence each in the decision register, with the way back, where the next session would
have measured them as entries still open. Without those entries neither is discoverable
afterwards by any amount of reading the repository.

## What actually enforces this

| Rule | Enforcement |
|---|---|
| Runnable work is committed before the session ends | **Enforceable, not enforced** here. Nothing this plugin ships looks at the working tree when a session ends: `SessionStart` prints the core card and, since 2026-09-27 (after 0.5.0), runs the update watch, which records the Claude Code version; `SubagentStart` prints the core card and, since 0.5.0, also appends a measurement line; `SubagentStop`, added in 0.5.0, only appends a measurement line (time, agent id, type, effort, transcript path) — none of the three looks at the working tree. Its other checks run before a push or against the plugin's own files. The mechanism is ordinary to build — a check at the end of a turn, or at the next start, that refuses on a dirty tree. Where one is built, the finding to look for is its coverage rather than its existence: a gate that watched a fixed list of tooling and configuration paths would say nothing about the knowledge a session actually produced, and a gate built for one repository would not protect its siblings. |
| Every fact is at its canonical place rather than in a note — work in git, decisions and operating-state changes with their way back in the decision register, open questions and measurements at theirs | **Behaviour rule** for whether each fact reached its place. The parts with a mechanical half have rows of their own — the commit row above and the operating-state row below — and the decisions row below says why nothing can tell that a decision is missing. Looked for: `hooks/hooks.json` registers no hook at a session's end, and `imprint-dev check` reads the plugin's own files, never a register. |
| The next session measures the state, and the next step with its reason stays in the closing message | **Enforceable, not enforced** for the measuring; **Behaviour rule** for the closing message. Something at the next start could list branches, worktrees and entries marked open; the session-start hooks this plugin ships inject the core card and, since 2026-09-27 (after 0.5.0), record the Claude Code version, and look at no branch, worktree or entry. Whether the closing message names a next step and gives its reason no mechanism reaches. |
| The operating state is restored, or named where it cannot be | **Enforceable, not enforced**, and this row read "no mechanism anywhere in sight" until somebody looked. The mechanism is a ledger: every suspension of a check or a schedule is entered with a path or a name **and an expiry**, and the close reads the ledger back. A ledger of this shape is buildable — it could hold each entry with its expiry and report it back at the turn's end with the time the exemption runs out. This plugin ships none; the decision register, where each suspension is entered with its way back and its date, is the place such a ledger would read. What stays unenforced even with a ledger is the entering: a suspension made without going through the ledger is invisible to it, the same shape a write path has when a file is opened in an editor instead. |
| Decisions that exist only in the conversation are written down | **Behaviour rule**. Nothing can tell that a decision was taken, so nothing can tell that one is missing. An absent record and a session in which nothing was decided are the same text. Unlike the two rows this round corrected, this one has been looked for and there is genuinely nothing. |
| The close triggers on the signal rather than on a phrase | **Enforceable, not enforced** for the observable signals; **Behaviour rule** for the judgement inside them. This row was wrong in the more dangerous direction. An explicit stop, a context window near its limit, a delegated task returning, and a session ending are events a harness can name: the runtime measured below names events for an end of turn, an imminent compaction, a subagent's return and a session's end, which bracket the observables the section above lists. A check that fires on the *event* rather than on a word neither narrows the reading nor needs a phrase list. This plugin registers hooks at a session's start, a subagent's start and (since 0.5.0) a subagent's stop, and the hook at the last two also appends a measurement line, but none of the three runs a close-triggering check, so none of these events causes a handover check here. What no mechanism reaches is only the judgement inside the observable: is this the end? The original objection — that a phrase trigger would establish the narrow reading — is true of a phrase trigger and was wrongly generalised to every mechanism. |
| Commands left for a person are shown singly | **Behaviour rule**. |

*Measured again 2026-09-26 by listing this repository and reading `hooks/hooks.json` and the
checks table in `tools/imprint-dev/checks.go`: nothing here observes a session ending.
`SessionStart` and `SubagentStart` print the core card, and since 2026-09-27 (after 0.5.0)
`SessionStart` also runs `hooks/update-watch.sh`, which records the Claude Code version and,
only if switched on, notes after an upgrade that a review is due; since 0.5.0, `SubagentStart` and `SubagentStop` each also run
`hooks/log-subagent.sh`, which appends one measurement line — time,
agent id, type, effort, and on a stop the transcript path, no response text — to a log file.
That measuring hook records that a subagent started or returned; it runs no check and enforces
no handover. None of the hooks looks at whether the returning session committed, restored state
or wrote down a decision, and none is registered for an end of turn, a compaction or a session's
end. `tools/imprint-dev` checks the plugin's own files — description lengths, the card and its
generated payloads, references to removed skills, the hooks file's shape, the plugin version,
that every enforcement-table row carries a known classification, and (with `--release`) that no
dated re-check is overdue — and a workflow file under `.github/workflows/` runs it with the
tests on push and pull request wherever CI is enabled. `.githooks/pre-push`, active only in a
clone that points `core.hooksPath` at it, checks commit identity and personal-data shapes before
a push. None of them has sessions as its subject. This replaces a note, measured 2026-09-26,
that read "The plugin's two hooks are registered for SessionStart and SubagentStart and print
the core card; none is registered for … a subagent's return" — true of the 0.4.0 tree it was
written against, and already false the same day once the SubagentStop measuring hook (0.5.0,
PR #3) landed. Separately, measured 2026-09-15, the harness running this: its hook events
include an end of turn, an imminent context compaction, a subagent's return and a session's
end — read as event names in the binary of the version in use, not executed, so what is
established is that such events are named and not what a handler may do in them.
Re-check by 2026-12-15.*

**Two rows in this table said a mechanism was impossible or harmful, and both were wrong in
the direction that never gets audited.** An assurance that something cannot be built only ever
slows people down, so nobody goes looking for the counterexample — and a counterexample can
already be running somewhere while such a row is being written. The rule that follows from it is
narrow and belongs here rather than in a postscript: **a row that says no mechanism exists owes
a sentence about where it looked.** If it cannot name the search, it is a guess wearing the
clothes of a finding.

## A cheap check before a session ends

- Is anything uncommitted that will look like an unexplained intermediate state tomorrow?
- What did I change about the environment that will not announce itself — a paused job, a
  suppressed check, a widened permission, an open window?
- What is the next step, and *why that one* rather than another? It goes into the closing
  message with its reason; if I cannot say why, the closing message hands over a task list
  rather than a direction.
- Was anything decided here that exists nowhere but this conversation?
- Is there a command somebody has to run? Then it is shown on its own, not inside a sentence.
- Am I treating "no one said stop" as evidence that the session is not ending?
- Do I hold the write right? If not, the close is a report of what is uncommitted and where,
  not a commit.
