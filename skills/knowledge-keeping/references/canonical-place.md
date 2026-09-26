# One canonical place

*Detail for rule 1 of the `knowledge-keeping` skill. The rule itself, its triggers and its
checks are in `../SKILL.md`; this file carries the reasoning, the worked cases and the full
enforcement table.*

**Every fact has exactly one authoritative place. Every other view of it is generated,
linked or embedded — never copied.** A wrong value is fixed at that place, never in
something the place produced.

This is the cheapest rule in the set to state and the one most often broken by accident,
because copying a number is never a decision. It is what happens when the alternative
requires a moment's thought and the number is right there.

## Why copying is a defect and not a convenience

A copied value has no owner. Nothing in either location says which of the two is the one
that gets maintained, so both do, or neither does — and the way you find out is that they
have drifted, at the moment you need the value.

The cost is not the drift. The cost is that after the drift you cannot tell **which copy was
wrong**. Two figures that disagree carry no information about which is current, and
reconstructing that needs the history of both places, which is exactly what nobody kept.

So the rule is not about tidiness. It is about keeping the number of things that can be
wrong equal to the number of things that can be fixed.

## The scope is anything that can change

The rule is usually stated for numbers, and stated that way it is too narrow. It covers
**any fact with a changeable state**: a status, a date, an arrangement, who is responsible,
what was decided, where something lives. Those drift exactly like figures do, and they drift
more quietly because nobody expects a sentence to be a data point.

Where an entity is involved — a person, an organisation, a component, a device — that
entity's own entry is the authoritative place for facts about it, and the pages that use
those facts link to it. Otherwise the authoritative place is the one whose subject the fact
actually is, which is not always the one that happens to have discovered it.

**One exemption, and it is narrow**: compiled reference material with no changeable state —
a derivation, a definition, an explanation of how something works — may repeat itself. It
cannot drift, because there is nothing in it that moves. Do not stretch this to cover a
reference page that quotes a current figure; the figure is not reference material just
because its surroundings are.

## One place per kind, not only per fact

The rule scales up. Not only each fact but each **kind** of knowledge has exactly one home,
named once:

- decisions live only in the user's decision register;
- working rules live only in the rule set — this plugin's skills and, where a setup has one,
  its always-loaded card;
- preferences live where the user decided: neutral ones in the always-loaded card, private
  ones in the user's private store;
- an auto-memory, a scratch note or a session transcript is an **inbox**. Its entries get
  sorted into those homes; it is never cited as the place of record.

There is no second rulebook restating the first, and there are no cross-references that run
in a circle. A rule that defers to another text, which defers back to it, is one rule in two
places with neither of them authoritative — the same defect as a copied figure, one level up.

## A worked case: the fix that preserves the defect

A figure lived in three places: the page that owned the subject, a summary that quoted it,
and a status view that displayed it. The figure changed. Someone noticed, and did the
conscientious thing — found all three and updated all three.

That reads like a success, and it is the failure this rule is about. Nothing was left
inconsistent, so nothing looked wrong; and the next time the figure changes there are still
three places, one of which will be missed. **Aligning every copy perpetuates the
duplication.** Alignment is half the repair; the other half is removing the duplication, so
that one place holds the value and the other two derive it or point at it.

The same shape appears in the other direction. When a generated report shows a wrong value,
editing the report is the fastest thing available and costs the most: the next generation
overwrites your edit and restores the wrong value, and the lesson everyone draws is that the
generator is unreliable. The value was never the report's to hold.

**The check**: after fixing a value, count how many places would have to change next time.
If the answer is still more than one, the repair is not finished.

## When you cannot tell which copy is current

Aligning copies presupposes you know which of them holds. Often you do not — and that case is
not a duplication problem at all. It is a **contradiction**, and it is handled first.

Superseding (rule 3, [superseding.md](superseding.md)) carries the procedure: change nothing, show both states in full, and
let a person decide which one holds. Align and remove the duplication only after that is
settled.

The mistake this ordering prevents is specific and it feels like diligence. Having decided
which place *should* hold the fact, you propagate from it — and **which place should hold a
fact and which copy currently holds the right value are different questions.** The
authoritative place is perfectly capable of carrying the stale copy; the value that was
written later may sit in the summary. Deciding the location first and then aligning outward
makes the wrong value the only one, with the whole weight of this rule behind it, and destroys
the second reading in the process.

One difference is not a contradiction: where the change-authority is settled and the party
entitled to change the value did change it, that is an update. Supersede it on the spot and
align; there is nothing to ask.

## Where embedding actually works

"Generated, linked or embedded" are not interchangeable, and the third one has a practical
limit worth knowing before you rely on it: embedding is unreliable in some tools, and where
it fails the failure is silent — what a reader sees is a gap or a raw reference, not an error.

So: where the presentation is delicate, generate or link rather than embed, and check what a
reader actually sees rather than what the source says. Whichever of the three you use, the
property that matters is the same one — **is there still exactly one place where a change is
made?**

## What actually enforces this

This plugin ships nothing that runs when you write, so nothing in this table is enforced by
it. What the states distinguish is whether a mechanism is *possible* — because that is the
part that tells you where a little tooling would convert discipline into a check.

| Rule | Enforcement |
|---|---|
| Each fact has one authoritative place | **Behaviour rule** without a marking convention; **Enforceable, not enforced** with one. Left as plain prose, nothing can tell an authoritative entry from a copy — both are text that says the same thing, and the distinction lives in intent. Where the store marks the authoritative entry instead, with a named anchor or an equivalent addressable marker, the distinction becomes readable and a scanner can flag an unmarked repetition of a marked value. The convention is the mechanism; adopting one is what moves this row. |
| Other views are generated or linked rather than copied | **Enforceable, not enforced**. A build step that renders a view from its source makes copying structurally impossible for everything it covers — not as a check that complains, but as a path that offers no way to type the value in. This is the strongest available form of the rule and it needs no cleverness. |
| A repeated figure or amount is detected | **Enforceable, not enforced**. Scanning your own text for the same number in two places is a short script. The instructive part is what can happen to such a script: it can drop out of the check chain unnoticed, or break where nobody sees it fail, and whether it was stood down deliberately or simply broke need not be clear from what was written down — which is itself the lesson, because a checker nobody calls is indistinguishable from a checker that passes. If you build this one, the thing to verify is not that it works but that something runs it. |
| The same statement appearing twice in prose is detected | **Behaviour rule**, and no mechanism is in sight. Two paragraphs can carry one fact with no shared string. |
| A wrong value is corrected at the source | **Behaviour rule**, and the one that most resembles success when it fails — see the worked case. |
| Each kind of knowledge has one home, and memory is only an inbox | **Behaviour rule** for sorting an entry into its kind: nothing reads an entry and knows whether it is a decision, a rule or a preference. **Enforceable, not enforced** for the circular half: references that name another rule file are text a script can follow, and a cycle among them is decidable. Nothing here follows them. |

*Every "enforceable" row above is unbuilt here. The dated measurement behind that statement is kept once,
in `../SKILL.md` under "What enforces this, measured".*

## The habit that carries the rule

Since no mechanism catches a duplicate, what actually carries this is a step taken **before**
writing: search for the fact before recording it. Not for the wording — for the subject. If
it is already somewhere, you are looking at the authoritative place, and your options are to
update it or to point at it.

That search is the enforcement. Skipping it is how every duplicate in every system got
there, and it is skipped because you already know the value you were about to write.

## A cheap check before you write a fact down

- Does this fact already exist somewhere? Did I look, or do I just not remember seeing it?
- If it changes next month, how many places have to change? More than one is the finding.
- Is this a view? Then can it be generated or linked instead of typed?
- Am I fixing a value in something that was produced from somewhere else?
- Two copies disagree — do I actually know which one holds? If not, this is a contradiction
  before it is a duplication: stop, show both, ask. Aligning now would make one of them
  canonical by accident.
- Is this a "just a status line" exception? Those are facts with changeable state, which is
  precisely the scope.
