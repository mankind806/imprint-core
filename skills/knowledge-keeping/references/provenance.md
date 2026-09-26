# Provenance on entry

*Detail for rule 2 of the `knowledge-keeping` skill. The rule itself, its triggers and its
checks are in `../SKILL.md`; this file carries the reasoning, the worked cases and the full
enforcement table.*

**Every entry names where it came from, including how it was obtained.** And where an entry
carries a deadline or a validity period, that date is stated explicitly, because **the date
something was recorded is not the date it is due.**

Both halves are about the same moment: the only time the origin of a fact is free to capture
is while you still have it. Afterwards it costs a search, and a search that frequently fails.

## Why "including how" is the load-bearing word

"From the supplier" is not provenance. It does not distinguish between a signed document, a
sentence somebody said on the phone, a figure read off a public page, a number you measured
yourself, and a conclusion you drew from two of those.

Those five things have completely different reliability and completely different behaviour
over time. A figure from a document does not change — the document keeps saying it. A figure
from a page you read once changes without telling you. A figure you inferred is only as good
as the inference, and the inference is exactly what disappears first.

So the method is not bureaucratic detail. It is the field that decides, later, whether the
entry can be quoted as it stands, has to be checked again, or was never a measurement at
all. Ageing (rule 4, [ageing.md](ageing.md)) routes on this field and cannot
work without it.

State it in whatever form your store uses, but state the mechanism: read from, asked of,
measured with, computed from, inferred from, quoted from a search on a given date.

## A recording date is not a deadline

This is the cheapest expensive mistake in the set. An entry is made carrying an obligation
and a single date, and the date is the day the obligation was noticed. Months later somebody
reads the entry and acts on the only date in it.

Both readings are plausible from the text, and the difference between them is the entire
value of the entry. If the true deadline was earlier, the entry caused a miss while looking
like diligence. If it was later, the entry caused a scramble. Either way the person acting
had no way to tell, because **one date in an entry that needs two is not ambiguous-looking —
it looks complete.**

So: the due or valid-until date is named as such, the recording date is named as such, and
if you only have one of them, say which one you have and that the other is unknown. An entry
that says "recorded on this date; the deadline itself is not stated in the source" is
strictly more useful than one that quietly offers a date.

## Attribution belongs in the same point as the claim

Provenance collected at the bottom is provenance destroyed. A pile of attachments listed
under a longer list of statements is not a set of supported statements; it is a set of
questions, and a reader who has to guess which source carries which claim will either guess or give up.

More evidence with no assignment is **less** proof than less evidence with assignment. Put
the origin next to the sentence it supports, even when that means repeating the source name
four times on one page. The repetition is not duplication in the sense
the canonical-place rule (rule 1, [canonical-place.md](canonical-place.md)) forbids — it
is an attribution edge, not a second copy of the fact.

## The user's words and your reading are two different origins

When an entry rests on something the user said, the user's wording is the source: it goes in
quotation marks, with where it was said. What you made of it is a **reading**, and it is marked
as one — "recorded as: …" is one form — so that a later reader can see where the quotation
stops and the interpretation starts. An unmarked paraphrase reads as the user's decision, and
it is not.

When the user later confirms a reading, the confirmation is a **new entry**: dated, carrying
the user's own wording and its source. The reading is not promoted in place. It stays marked
as a reading and gets a pointer to the entry that now carries the user's words — which is the
superseding's shape (rule 3, [superseding.md](superseding.md)) applied to an
origin.

This looks as though it collides with the ageing rule's "confirming is not superseding"
(rule 4, [ageing.md](ageing.md)), which re-dates an unchanged entry in place. It does
not. A re-check leaves both the value and the kind of origin as they were: the same look-up,
found to still hold, so only the date moves. A user's confirmation changes the origin itself,
from your inference to the user's words, and brings wording that was not there before. A
different origin is a different entry.

## Two cases, the first a shape and the second measured

The first is written as a shape rather than as an incident, because a shape is what it is. An
example that claims to be an event owes a source, and this one has none — which is this very
rule turned on this file.

**The unattributable figure.** Consider a number recorded on a page, correct when it was
written, with no origin against it. Some time later it disagrees with a number somebody else
holds. Resolving that means finding out where each came from, and for an entry with no origin
there is nothing to go back to. The figure is not wrong. It is **unusable**, which in practice
is the same thing and feels worse, because it looks like information right up to the moment you
need to rely on it.

**The rule that claims a mechanism it does not have.** A rulebook can rate a rule like this
one as mechanically enforced and, as such ratings should, name a file as the enforcer — while
in the tree that rulebook ships in, the named file does not exist. The rating was written where
the enforcing tool is not.

The rulebook's own preamble may even say that its ratings describe an intended state, so the
mistake is disclosed somewhere. That is exactly what makes the pattern instructive rather than
excusable: the entry looks *more* rigorous than the honest version would, because it carries
a citation, and the citation is what stops anyone opening the path. **An assurance is
a claim like any other, and a claim about your own enforcement is the one nobody audits** —
`measure-before-asserting` is about that class in general, and this is what it looks like when
it lands on provenance itself.

That is why the table below rates this rule as enforceable rather than enforced, and why the
rating is checkable against this repository's own tree rather than asserted.

## What actually enforces this

This plugin ships nothing that runs when you write, so nothing here is enforced by it. The
states say what is *possible*, which is the useful distinction.

| Rule | Enforcement |
|---|---|
| Some origin is present on the entry | **Enforceable, not enforced**. A write path that refuses an entry with an empty origin field is a real mechanism and a small one. But note its shape before you trust it: it covers **only the writes that go through it.** The same file opened in an editor bypasses it completely, and there is no way to close that from inside the tool — so even where this row reads "enforced" in some system, it means "enforced on one of two paths", and the rule applies unchecked on the other. |
| The origin named is the true one | **Enforceable, not enforced** for the half that is decidable; **Behaviour rule** for the half that is not. That the named source *exists* — a file at that path, a snapshot with that date, a reachable page — is decidable, and so is whether a quoted string occurs in it: both are worth building before the harder half is written off. Whether the source actually *says what the entry claims* is a question about meaning, and no mechanism reaches it. Stating the whole row as impossible concedes the cheap half along with the expensive one. |
| The method is stated, not just the source | **Enforceable, not enforced** — a fixed vocabulary of methods in a required field would do it, and would also make the field machine-readable for the ageing rule. Nothing here has one. |
| A due date is recorded separately from the recording date | **Enforceable, not enforced** for the *separation*: two fields instead of one is a schema decision, and a schema can refuse an entry that has a deadline in prose and only one date. |
| The right date went in each field | **Behaviour rule**. Two dates are two dates; no checker knows which is which. This half stays yours no matter how good the schema is. |
| Attribution sits in the same point as the claim | **Behaviour rule**. Concerns the shape of a passage; a checker for it is not in sight. |
| The user's wording is quoted, a reading is marked as a reading, and a later confirmation is a new entry | **Behaviour rule**. Nothing tells a quotation from a paraphrase, or a reading from a record of what was said. |

*Every "enforceable" row above is unbuilt here. The dated measurement behind that statement is kept once,
in `../SKILL.md` under "What enforces this, measured".*

## A cheap check before you record a fact

- Where did this come from, and **how** — read, heard, measured, computed, inferred?
- Is there a date in this entry that somebody might act on? Which kind of date is it, and
  does the entry say which kind?
- If the source is a search rather than a document, is the search date in there? Without it
  the entry cannot be aged.
- Would a stranger reading this in a year be able to check it, or only to trust it?
- Am I about to write an assurance about a mechanism? Then name the file that stops you, and
  open it first.
