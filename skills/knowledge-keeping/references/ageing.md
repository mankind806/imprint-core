# Knowledge ages

Permission to record what you learn only works alongside an obligation to look again.
Without it what grows is not a body of knowledge but **a stock of dated assertions**, and the
dating is what makes them convincing.

*Detail for rule 4 of the `knowledge-keeping` skill. The rule itself, its triggers and its
checks are in `../SKILL.md`; this file carries the reasoning, the worked cases and the full
enforcement table.*

**The trigger is use, never the calendar.** Nothing has to sweep the store on a schedule.
When an entry is about to be relied on, its age is the question — and that is also the only
moment at which the answer is worth anything, because an entry nobody reaches for costs
nothing by being stale.

## The dangerous age is the recent one

Read something from seven years ago and you distrust it without being told. The entry that
does damage is the one from a few months back: it carries a date from this year, it reads as
current, and it gets quoted without a thought.

So the reflex to build is not suspicion of old material. It is a check on material that
**feels** current. That is the same failure mode `measure-before-asserting` is about — the
claim gets waved through because it fits — arriving here as a property of dates rather than
of statements.

## Which values age, and which may never be quoted from storage at all

The routing question, and it decides every borderline case: **who is entitled to change this
value?**

**This question is asked first.** The evidence-kind split in the next section applies only to
what this one leaves behind, and taking them in the other order produces a specific wrong
answer: a cloud service's price list or an API's published rate limits, saved as a document,
are document-backed, so the split says "does not age" — and the document is a snapshot of
something its issuer may already have changed. Whoever may change it decides whether the value may be quoted at all; only then does
the kind of evidence decide how long a quotable value lasts.

If a single organisation sets it and may change it without telling you — an authority, an
institution, a provider setting its own terms — then **no age is acceptable**, not even a
day, and the entry is not quoted from storage at all. That is `measure-before-asserting`'s
territory: look it up now, and if the stored state differs, supersede it in the same move.
A provider's published limits, a stated term, a documented procedure, who is responsible for
which part — anything a single body announces and can re-announce without consulting you —
belongs there entirely.

What is left for this rule is everything **nobody sets individually**: market levels, the
state of the art, professional recommendations, the research picture, how a material or a
technique behaves. No one can change those by publishing a decision, so they drift instead of
jumping, and a period is a sensible way to handle drift.

**A pattern worth recognising in a rule's own drafts:** a draft can offer, as its model
example of a value that ages after a few months, a per-unit price that a cloud provider
publishes and also bills your account at — exactly the class for which *any* age is too old.
The rule then illustrates itself with a case it should route away. Such an example survives a
first read because it is plausible and the sentence scans. What flushes it out is an
adversarial read by a party that did not write it; a test question like "is this an individual
arrangement or a general condition?" does not decide the case either, because the value is
both. Only "who may change it" separates them.

If your own version of this rule carries an example, check that the example is on the right
side of it.

## What ages is decided by the kind of evidence, not the kind of content

This is the part that is usually got wrong, and getting it wrong in the obvious direction
produces a rule that fires on everything. It applies to what the routing question above has
already left here — not to values a single body sets, whatever they are written on.

- A figure supported by a **document** does not age. The document goes on saying the same
  thing; nothing about it moves. If you want to use that figure as **today's** value, you do
  not re-check the old one — you establish today's value afresh, which is a different act
  with a different result.
- A figure supported by a **look-up** ages. It was never a fact about a document; it was a
  report about the state of the world on the day you looked, and the world was under no
  obligation to tell you it had moved on.

An earlier version of this rule tried to split "timeless formula" from "its example numbers"
instead. That could not be applied to a real page, on which a single line frequently carries
both and cites one source for them.

## Confirming is not superseding

When you look again and the value is **unchanged**, the entry still needs its date moved
forward. What it must not go through is the replacement path.

The failure is worth the warning. Repeated confirmations of one unchanged line, each recorded
as a replacement, produce one history reference per confirmation nested inside the entry's own
formatting, history text growing faster than the number of confirmations, and — each time — a
line reading that the value had been superseded on that date. **The history reports a change
at precisely the place where unchangedness had just been established.**

The correct operation is a direct edit of the origin note: the value stays, the wording stays,
the date of the most recent confirmation is carried forward alongside the original one, so the
entry says both when it was first obtained and when it was last found to still hold.

Three limits on that, because it is a licence to edit in place and those need boundaries:

- It applies to **the origin note only**, never to the statement in front of it. The moment
  the statement changes, this is not a confirmation and superseding
  (rule 3, [superseding.md](superseding.md)) applies.
- A user confirming one of your **readings** is not this case. That changes the origin from
  your inference to the user's words, and it is recorded as a new entry — see
  provenance (rule 2, [provenance.md](provenance.md)).
- If your store has no verb for "re-date without replacing", you will be reaching past
  whatever mechanism it does have, by hand. That is a named gap rather than a licence —
  note it, and prefer building the verb over normalising the reach-around.

## Where the rule applies, and the third answer nobody writes down

The scope is **living compiled text**. Not immutable sources — they are snapshots and are
supposed to be old. Not append-only history — carrying anything forward there is forbidden
outright, so a period would have no object. Not already-superseded text: measuring dead
states means measuring something other than what is in use.

And then the exit that gets silently skipped. Entries with **no origin marker at all** cannot
be aged: nothing computes a date from an absence. Those entries are **not checkable**, which
is a different answer from **fine**, and it belongs in the report as its own number. A check
that quietly passes everything it cannot see looks exactly like a check that found nothing
wrong. The unmarkable entries can be most of the material — so if your
count of "aged entries" is reassuringly small, find out how large the invisible class is
before believing it.

Every check has three outcomes, not two: satisfied, violated, and not checkable. The third
one is the checker's gap and has to be stated as such.

## How long, and why no number appears here

A period has to come from the rate at which its subject actually moves, and that is a property
of your domain rather than of this rule. What transfers is the shape: **a short horizon on an
entry with a look-up origin, treated as an obligation, and a longer one on the page around it,
treated as a reminder to read the whole thing through.**

The second has only the remainder as its scope — if the first has already fired on a line,
the longer period has nothing left to say about it, since anything older than the long horizon
is more than old enough for the short one.

Pick the two numbers deliberately, write down what they were chosen against, and treat them
as a setting rather than as part of the principle.

## What actually enforces this

This plugin ships nothing that runs when you write or when you read.

| Rule | Enforcement |
|---|---|
| An entry carries an origin from which an age can be computed | **Enforceable, not enforced** — this is provenance's row (rule 2, [provenance.md](provenance.md)), and this rule is entirely parasitic on it. Without a machine-readable origin and method, nothing below is even arithmetic. |
| Entries past their horizon can be listed | **Enforceable, not enforced**, and the easiest win in the knowledge layer: read the origin dates, compare against a horizon, print what is over it. No judgement in it at all. |
| The check happens at the moment of use | **Enforceable, not enforced** where entries are read through a tool; **Behaviour rule** where they are read directly. A retrieval path can print an entry's age alongside it, which puts the age in front of the reader at exactly the moment the rule is about. What no mechanism reaches either way is the reading that bypasses the tool, and the acting on what the age says. A list of stale entries is not the rule; the rule is that you notice before you quote, and a list is consulted only by someone who already thought of it. |
| A re-date of an unchanged entry does not travel the replacement path | **Enforceable, not enforced** — a store with a distinct verb for it enforces the distinction by having somewhere correct to go. Where there is no such verb, it is a behaviour rule and a fragile one, because the replacement verb is right there and does something that looks close enough. |
| Whether a value is one a foreign body sets | **Behaviour rule**, and no mechanism is in sight. Nothing in stored text distinguishes a value somebody else controls from one you were a party to, and the cases where the store governs instead (named in `measure-before-asserting`) are no more distinguishable than the rest. The answering party decides it every time, unobserved. This gap is named rather than closed: a named gap is checkable, an unnamed one looks like an absence of errors. |
| Entries with no origin marker are counted and reported separately | **Enforceable, not enforced**. Listing entries that carry no origin at all is as short as listing the ones past a horizon, and the point of it is that this class is a check *outcome* — not checkable — rather than a state of the rule. Reporting it apart from the pass is mechanical; treating its silence as freshness is what the row prevents. |

*Every "enforceable" row above is unbuilt here. The dated measurement behind that statement is kept once,
in `../SKILL.md` under "What enforces this, measured".*

## A cheap check before you quote a stored value

- **First:** who is entitled to change this value? If it is one outside body, do not quote
  this at all; look it up. The rest of this list does not apply to it.
- Then: how was this obtained — read off a document, or looked up? Only the second one ages,
  and a document does not exempt a value the question above already routed away.
- Does the date feel recent? That is the trigger, not the exemption.
- If I look again and nothing changed, do I have a way to record that which does not claim a
  change?
- Does this entry carry any origin at all? If not, the answer is "not checkable" — say so
  rather than treating silence as freshness.
