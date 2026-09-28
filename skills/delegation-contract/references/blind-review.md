# The blind first pass

*Reference for `delegation-contract`, which keeps a short version of this in its section on a
blind second opinion. Read this in full when you are deciding what goes into a reviewer's
context and what stays out of it, when deciding whether a second voice is worth asking for at
all rather than a second measurement, when consulting a stronger model at a decision point,
when two agents disagree and you are tempted to average them, or when a review keeps agreeing
with you. The envelope around the review — role, rights, one writer — is the skill body's job;
this file decides what goes inside it.*

A second voice is worth something because it did **not** watch your solution take shape.
Telling it how you got there destroys precisely that. Most disappointing reviews are
disappointing for this reason and not because the reviewing model was weak.

## Staged disclosure

**Stage A — blind.** Reproduction, expected behaviour, raw measurements, the diff, fixed
decisions already made by a human, acceptance criteria.

Not: your hypothesis about the cause. Not the alternatives you discarded. Not the order in
which you thought about it. And note that the *selection* of facts anchors too — "we have
already looked at the cache" points at the cache just as firmly as saying you suspect it.

**Stage B — the experiment log, neutrally.** Illustratively: "with the cache disabled the
fault still occurred in, say, a third of the runs" — not "so it is not the cache."

**Stage C — comparing hypotheses.** Your reading, last, explicitly labelled as a hypothesis.

Stage A is overridden when repeating the work would be expensive or dangerous, when it
touches external systems or real data, or when leftovers from an attempt are sitting in the
working tree and would otherwise be mistaken for intended code.

## When a second voice adds information, and when it only adds agreement

**Closed, checkable questions** — is this function called, does the test pass, what does the
specification say — gain nothing from a second *opinion*. What they want is a second
independent **measurement**: "determine this from the primary source, the code path, or an
executable test." Convergence along separate evidence paths is a result. Bare agreement
with no evidence of its own is placebo.

**Open search spaces** genuinely gain: overlooked error paths, architectural assumptions
that break under load, missing security boundaries, invariants the tests do not actually
prove, dominant trade-offs, causes nobody investigated. Code review belongs here.

Pseudo-diversity is produced by:

- letting the second agent read the first agent's reasoning first,
- phrasing the task as "check whether this solution is good,"
- having several models vote on the same unevidenced claim,
- routing the same model through a different interface. A different pipe to the same weights
  is the same weights. It buys latency and cost, not another perspective.

**A better second question usually beats a third model.** Ask the second voice for the
strongest counter-hypothesis, one decisive test, and its own falsification criterion. A
third voice earns its place when two evidence-backed positions stay incompatible, the search
space is open and a mistake is expensive, no cheap decisive test exists, or the decision is
hard to reverse.

## Keep the roles apart

- The **blind explorer** does not see the other answers and searches independently.
- The **arbiter** receives both positions and checks their load-bearing claims — and is by
  that fact no longer independent.

One agent cannot be both. If you want an arbiter, dispatch a third.

The cheapest mechanical way to get a blind pass is context separation: dispatch a **fresh**
agent and give it the artefact, not the transcript. What makes it blind is the *context
boundary*, not the fact that it is a different model.

🔴 **"Fresh" is a property of the dispatch, and one dispatch type is the exact opposite.**
Claude Code offers a subagent type called `fork`, and a fork **inherits the parent's full
conversation history** — the task, every tool call, every result, your reasoning. Dispatched
that way, the reviewer sees precisely what the blind pass exists to withhold, and you get a
confident second voice that has already read your hypothesis. A fork is an excellent way to
continue your own work in the background. It is not a second opinion, and asking it to
"ignore what you have seen" does not make it one. *(Measured against the Claude Code
subagent tool description, 2026-09-13, CLI 2.1.269; other harnesses have their own
context-inheriting dispatch — check yours before assuming a fresh context.)*

**Whoever picks the excerpt determines the verdict.** If the reviewer receives its slice
from the party being reviewed, it reviews that party's view of the problem — in a separate
context, but on the same slice. The checkable property is not "who reviews" but "who
selects." Where it matters, let the tool draw the sample, not the author.

## Put disagreement in front of the human — do not average it

Conflict between voices is the desired outcome. It is never suppressed and never stirred
into a mean. Break it into checkable claims so that only the real value or risk decision
reaches a person:

| Claim | Position A + evidence | Position B + evidence | Decisive test |
|---|---|---|---|

Then, in this order: run the deterministic test; failing that, take the smallest reversible
probe that produces information; failing that, compare error cost and reversibility; only
the remainder goes to the human.

Shared norm: **no agent owes another agreement. Both owe the human a falsifiable claim, the
best counter-evidence, and a visible revision trail.** Forced consensus destroys exactly the
diversity being paid for. "Unchanged" is a position, not a cooperation failure, when the new
evidence does not touch the claim.

You have merely been polite if your conclusion changes but no premise does; if you repeat
the other voice's wording without resolving your earlier evidence; or if your supposed
revision implies no different prediction.

## Placement, not frequency

Consulting a stronger model is worth its latency at decision points, not continuously:
before expensive or irreversible work, when you are stuck, and before a binding artefact is
finished. The consulting agent stays responsible for the outcome; the advice is material,
not an order.

The initial assignment of which voice is good at what is a starting value, not a law. Watch
who actually delivers. After two failed attempts change the level rather than resampling the
same agent; resampling is not diagnosis, and `measure-before-asserting` says why.

## What actually enforces this

| Rule | Enforcement |
|---|---|
| The reviewer does not see your reasoning | **Enforced** by the context boundary — but only for a dispatch that starts a fresh context. A context-inheriting dispatch (in Claude Code, `fork`) enforces the opposite, and the wrong choice is silent. Check which one your dispatch is. Asking a model to ignore what it already read enforces nothing. |
| The reviewer's sample was not chosen by the author | **Enforceable, not enforced** — have the tool draw the sample. Nothing does it for you today. |
| Stages A/B/C in order | **Behaviour rule**. |
| Disagreement is surfaced rather than averaged | **Behaviour rule**, and the one most quietly broken, because a smoothed summary reads better than a table. |

Four states, not two, and this table uses three of them. Row two is the middle one: a rule
that *could* be mechanical — a script picks the diff hunks, not the author — and today is not.
That state deserves its own name rather than being rounded up to "enforced" or down to "we
try". It is also the only one of the four that tells you where a few lines of tooling would
pay. The fourth, **reserved to a person**, has no row here, because nothing in this reference is
a consent step; knowledge-keeping, rule 3 (superseding), and the table in the
`delegation-contract` body are where it earns its place.

## Zero findings, and the pressure it creates

How many rounds a piece of work gets at all is set by its risk — see the section on review
depth in the skill body. This section is about what a round that finds nothing means.

A review that produces zero findings on a non-trivial artefact in its first round is itself
a finding: either the disclosure was not blind, or the task was phrased as a request for
approval.

**The remedy is to re-examine those two things. It is never to produce a finding.** Stated
without that limit, the heuristic becomes a quota, and a reviewer under a quota invents —
which costs strictly more than the empty round it was meant to prevent, because an invented
finding gets read, believed and worked on.

The stopping condition is the shape of the work, not the count. **A reviewer that formed a
suspicion, went and checked it, and reports it refuted has delivered a full result.** That
is not an empty round and must not be padded into a full-looking one. The adversarial read
of this repository did exactly that: it took an unfamiliar agent frontmatter field for a
defect, went looking, found the same field in every agent file of a published first-party
plugin, and reported its own suspicion as disproved rather than filing it.

So there are two honest ways for a round to end without a defect, and only one of them is
the finding above:

- the reviewer looked, formed hypotheses, tested them and they failed — report the tested
  hypotheses and what refuted them, and the round is done;
- the reviewer found nothing to be suspicious *about* — that is the signal that the
  disclosure or the phrasing was wrong, and the thing to fix is the dispatch, not the
  artefact.
