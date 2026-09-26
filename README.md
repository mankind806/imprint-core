# imprint

A suite of working rules for two things that turn out to be one thing: how several AI
coding agents cooperate without ruining each other's work, and how a knowledge base grows
alongside the person using it without quietly rotting.

Both are written as mechanisms rather than as advice, and every rule says which of four
things it is: enforced by something that stops you, *enforceable* but currently resting on
discipline, reserved to a person because the rule's content *is* a human decision, or a
behaviour rule with nothing behind it at all.

It ships as a Claude Code plugin, so the rules arrive as skills the agent can reach for
during a session rather than as a document somebody has to remember to open.

There is an awkwardness in that sentence worth saying out loud rather than burying at the
bottom. A skill loads when the model recognises that the task matches it — which is *after*
the model has decided how to approach the task. But the delegation contract is mostly about
decisions taken before that point: who leads, whether to dispatch at all, who ends up
holding the pen. By the time a model thinks "this is a delegation question, I should open
the delegation skill," it has usually already framed the delegation. A plugin cannot ship a
`CLAUDE.md` (see [Known limits](#known-limits)), but it is not limited to loading on demand
either: a hook can inject text as additional context before the model has framed anything, at
`SessionStart` and, for a dispatched task, `SubagentStart`. This version does exactly that —
see [Core card and checks](#core-card-and-checks) — but what it injects is a short summary
card, not the skills themselves, so the gap it closes is partial. The honest reading is still
that these skills work best when you invoke them deliberately at the start of a piece of
multi-agent work; the card is a floor under that, not a replacement for it.

## Status

Early, and deliberately partial.

**Both layers now ship.** Three skills in the agent-collaboration layer —
`delegation-contract`, `measure-before-asserting` and `session-handover` — and one in the
knowledge layer, `knowledge-keeping`, which holds four rules: the three that the section below
announced as shape rather than content, plus one for a failure mode named under *Who this is
for* — a fact that was true when it was recorded and is still being quoted as current. The
other failure mode named there, a session that ended without leaving a way back in, is
`session-handover`, in the collaboration layer. Of the other two things that section
announced, one was already here, and one turned out to be half delivered and half wrong; both
are dealt with where they stand rather than quietly dropped.

That does not make either layer complete. It makes the announced part of it real.

The skills here were practice before they were text, which is the right order but means the
text lags the practice. **Each of the four skills goes back to text that had at least one
adversarial read by a party that did not write it, but not every current version has had
one.** The earlier forms were read first: the three original agent-collaboration skills
earliest, the four knowledge rules as separate skills on 2026-09-15, and `session-handover`
later the same day, in a round of its own that also re-read the seven around it. Of the
current texts, a read of its own is recorded only for `delegation-contract` as it stands
since `blind-first-pass` was merged into it. **Not read yet:** `session-handover` as rewritten
on 2026-09-26 without a handover note. **No read recorded:** `knowledge-keeping` as one merged
skill, the 2026-09-26 changes to `measure-before-asserting`, and the 2026-09-26 measurement
rows of `delegation-contract` (see [Measuring subagents](#measuring-subagents)).

This repository's own rule is that zero findings in a first adversarial round on a non-trivial
artefact is itself a finding, and no round here has come close to zero. What those rounds are
worth saying is not how many were closed but **what is still open**:

- **Whether any limit truncates a skill description.** No longer as open as it was: this
  release's own tooling checks (see [Core card and checks](#core-card-and-checks)) that each
  skill description stays at or under 400 characters and all of them together at or under
  3000, and CI runs it wherever GitHub Actions is enabled. Measured locally 2026-09-26: four
  skills at 1573 characters combined, none near the cap. Still unmeasured is whether the
  *host*, Claude Code itself, imposes any separate limit of its own; no specification for one
  has been located either way. *Re-check by 2026-12-15.*
- **The remaining rows that say no mechanism exists have not been re-searched.** The most
  recent round found two places where this repository declared a mechanism impossible or
  harmful and was wrong both times, because nobody had gone looking — an over-cautious
  assurance is the kind nothing ever makes fail. Those two are corrected and now say where
  they looked. The other behaviour-rule rows across the four skills carry no such sentence
  yet. *Re-check by 2026-12-15.*

Expect the structure to move again before it settles.

## Who this is for

Anyone working with more than one coding agent who has noticed that the interesting
failures are not bad code. They are two agents writing the same file at once, a fact that
was true when it was recorded and is still being quoted as current, a session that ended
without leaving a way back in, and an assurance that sounds careful and was never measured.

If you use a single assistant for single tasks, this is more machinery than you need.

## Installation

```
/plugin marketplace add mankind806/imprint-core
/plugin install imprint@imprint
```

The repository is its own marketplace, which is why the name appears twice. Both the
qualified form above and plain `/plugin install imprint` resolve to the same plugin, and
once installed the skills are addressed as `/imprint:delegation-contract` and so on, with
the agent as `imprint:foreign-material-reviewer`. Claude also reaches for them on its own
when a task matches their description.

- **Local checkout:** measured 2026-09-13 on Claude Code 2.1.269, when the plugin had three
  skills: `marketplace add` and both the qualified and the bare `install` succeed, with no
  competing plugin named `imprint` present.
- **`--plugin-dir`:** measured 2026-09-15 on Claude Code 2.1.272 at plugin version 0.3.0: all
  eight skills of that release and the agent are listed under the `imprint:` prefix; the
  present four are not separately measured. *Re-check by 2026-12-15.*
- **Over the network:** measured 2026-09-26 on Claude Code 2.1.283: `/plugin marketplace
  add` with the HTTPS URL of this repository, then `install imprint@imprint`, installs
  0.4.0; a new session receives the core card from the SessionStart hook. The shorthand
  `mankind806/imprint-core` in the block above is not separately measured.
- Whether the bare `install imprint` stays unambiguous depends on the other marketplaces
  *you* have added; if you already have an `imprint` from somewhere else, use the qualified
  form.

## What is in the box

Four skills and one agent, in the two layers described below, plus two plugin hooks that
inject a summary of them at session start (see [Core card and checks](#core-card-and-checks))
and one that records how long each subagent ran (see [Measuring subagents](#measuring-subagents)). Each skill carries a section
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

- **`foreign-material-reviewer`** (agent, unchanged in 0.4.0) – a read-only triage role for material you did not
  write. Its `tools:` frontmatter is an allowlist of `Read`, `Grep` and `Glob`, which is the
  point: a prompt asking an agent to stay read-only is a behaviour rule, and behaviour rules
  are broken by exactly the input this agent exists to handle. What it does *not* close is
  named in its own description.

  *Measured 2026-09-13 on Claude Code 2.1.269 through `--plugin-dir`, at session scope: the
  agent gets exactly `Read`, `Grep` and `Glob`; a subagent dispatch and the filtering of MCP
  tools are not measured. Re-check by 2026-12-13.*

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

## Known limits

Each of these says what kind of claim it is — measured here, or merely carried forward — and
carries a date by which it should be looked at again. A named gap without a date stops being
a gap and turns into how the system simply is. The horizon is three months for all four:
Claude Code ships frequently enough that a longer one would be fiction, and often enough
that a shorter one would be busywork.

- **Whether plugin hooks run in claude.ai cloud sessions: unmeasured, and not documented
  anywhere we could find.** This release ships hooks on `SessionStart`, `SubagentStart` and
  `SubagentStop` (see [Core card and checks](#core-card-and-checks) and
  [Measuring subagents](#measuring-subagents)), so the question is no longer moot —
  but cloud sessions specifically are still not measured either way; do not assume the card
  fires there just because it fires elsewhere. *Re-check by 2026-12-13.*
- **Whether a plugin's skills and agents are available in the IDE integrations the same way
  they are in the terminal: unmeasured.** The plugin documentation does not mention IDE
  support either way, and we have not tested it. *Re-check by 2026-12-13.*
- **A plugin cannot ship a `CLAUDE.md`: measured 2026-09-13 on Claude Code 2.1.269 through
  `--plugin-dir`; an installed plugin is not separately measured.** A hook is the route in
  instead: the `SessionStart` and `SubagentStart` hooks inject a summary card (see
  [Core card and checks](#core-card-and-checks)), so the full skills still take effect only
  when invoked. *Re-check by 2026-12-13.*
- **`hooks`, `mcpServers` and `permissionMode` in a plugin agent's frontmatter are silently
  ignored: carried over from the first release's notes, and the source for it was not
  re-located when this section was written.** It is why the agent relies on `tools:` and
  claims nothing else — a conservative choice that costs nothing even if the claim turns out
  to be wrong. "Silently" is the load-bearing word: there would be no error to notice either
  way, which is precisely why this one wants a measurement rather than a re-reading. The
  positive half *is* now measured: in a plugin agent's frontmatter, `tools:` and `model:`
  are both honoured — see the `foreign-material-reviewer` entry above, where the same run
  that showed the three-tool allowlist also showed the session running on the model the
  frontmatter names rather than the session default. So plugin agent frontmatter is read
  **selectively**, and which fields survive is a per-field question.
  *Measure the three ignored fields, or cite a source for them, by 2026-12-13.*

One further gap is named inside `delegation-contract` rather than here, because it is a
property of the rules and not of the packaging: a triage agent's *findings* flow back into
an agent that does hold Bash and write access, and nothing marks that return as foreign.
That seam is named, not closed.

## Core card and checks

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

Measured once on one Linux setup with `claude -p`, date not recorded: both the `SessionStart`
and the `SubagentStart` payload arrive as additional context. Native Windows is **not
measured**; treat the card's arrival there as unknown.

## Measuring subagents

`delegation-contract` asks for every dispatch to be measured: task kind, model, effort,
duration and quality. A third hook takes over the part a program can see.
`hooks/log-subagent.sh` runs on `SubagentStart` and `SubagentStop` and appends one JSON line
per event to `${CLAUDE_PLUGIN_DATA}/subagent-log.jsonl`, the plugin's data directory on the
machine it runs on — `~/.claude/plugins/data/<id>/` per the plugins documentation, read
2026-09-26; the form of `<id>` is not checked here. The plugin ships no measurements of its own.

**What a line holds:** the time in UTC, the event, `agent_id`, `agent_type`, `session_id`, the
effort level if the hook input carries one, and on a stop the path of the subagent's
transcript. **What it never holds:** the subagent's answer or any other text it wrote. The
script picks the named fields out by pattern and discards the rest of its input, and a test
feeds it an answer and checks that none of it reaches the log. Internal agents, which arrive
with an empty `agent_type`, are not logged. The hook never stops or slows a run: it prints
nothing, adds no context and exits 0 on every path.

**Reading it back:** `go run ./tools/imprint-dev measure` (the log defaults to
`$CLAUDE_PLUGIN_DATA/subagent-log.jsonl`; outside a hook, pass `--log`). It pairs each stop
with the latest start of the same `agent_id` — a resumed agent starts again under its old id —
and prints one row per run: start, agent type, model, effort, duration, and whether the run
went over the target (`--target`, default `5m`). The model is read from the subagent's
transcript, where each assistant line records it in `message.model`; every distinct model is
listed, and a transcript that is gone shows `unknown`. `--projects DIR` searches `DIR` for
`agent-<id>.jsonl` when the logged path is missing. `--format json` gives the same rows as
JSON. An overrun is reported, not treated as a failure: the command exits 0 whatever it finds.
The **quality** column stays empty: whether the acceptance criterion was met is a judgement,
and the lead enters it by hand. So does the task kind, of which `agent_type` is only a proxy.

**In the repository's four states:** recording the duration, effort and agent type of a run is
**enforced** by the hook wherever it fires — measured by hook, reported on demand — in the
sense that it no longer rests on anyone's discipline; that it fires in a live session is not
measured yet (below). Nothing is stopped, so the five-minute target stays *enforceable, not
enforced* by design. Quality and task kind stay a **behaviour rule**.

**Limits.** Tested with invented input under `sh` in CI and locally; **the hook has not been
measured firing in a live session yet.** Whether it fires for background agents is **not
measured**, and neither is native Windows, where the hook needs an `sh` on the path. Whether
the hook's `agent_id` equals the id in the transcript file name is **not measured** either;
`--projects` depends on it. *Re-check by 2026-12-26.*

## The pre-push hook

One rule here has a mechanical half, and this repository now runs it: nothing leaves this
repository except its git identity. `.githooks/pre-push` refuses a push whose commits carry
an identity this clone has not declared, and it refuses a push whose tracked content or
commit messages match a shape that personal data takes — an address of the kind mail uses, a
phone number, a bank account number, a postal address, each only in the format its pattern
spells out, so a format it was not written for passes. It reads every
commit in the pushed range rather than the tip alone, because a push publishes the whole
range, and a file removed in a later commit stays reachable by its hash for anyone who
clones. Commit messages are checked alongside the trees, since a message is as public as a
blob and trailers are where addresses ride in.

**It does not arrive with a clone.** Git runs hooks out of `.git/hooks` unless it is told
otherwise, and nothing in a checkout can tell it for you. Each clone needs one line:

```
git config core.hooksPath .githooks
```

A co-author or a fork declares a second identity with
`git config --add imprint.allowedIdentity 'Name <address>'`. Your own `user.name` and
`user.email` count as declared without being listed.

**What it does not enforce is the larger half.** The rule asks for abstraction: a worked case
told generically, with no organisation, no product, no ticket number, no path off anybody's
machine. Whether a passage is abstract is a question of meaning, and no pattern answers it. A
page naming a real employer in plain words passes this hook exactly as a properly abstracted
one does, and a blocklist of real names would not change that — it would only look as though
it had. In the four states this repository sorts every rule into: the shapes and the
identity are **enforced**; reading for abstraction stays a **behaviour rule** with nothing
behind it. The hook says so itself, in every report it prints.

It fails closed. Every way it can fail to finish — a git command that errors, an identity
this clone never set, a temporary directory it cannot create — ends in a refused push,
because a gate that waves you through when it breaks is indistinguishable from one that
checked. One empty case is not such a failure and took a refused push to find: git runs the
hook even when the remote is already up to date, and pipes in an empty ref list. Nothing is
published in that run, so there is nothing to check, and the hook says so and lets it
through — but only when git is the one calling, which is a hook invoked with a remote name
and location and handed a pipe rather than a terminal. An empty list from anything else is
still refused. A check that could not run is reported apart from a finding, and no override
covers it: "I could not look" and "I looked and found nothing" must never share an exit code.

`git push --no-verify` skips every hook silently, and the script cannot see that it happened.
`IMPRINT_PUSH_ANYWAY='reason' git push` is the loud alternative — the findings are printed in
full, the reason is echoed back, and the push proceeds.

## License

MIT, and it covers the whole work – the prose as much as any code. The MIT text speaks of
"the Software", which reads oddly for a repository that is mostly writing, so to be
explicit: the rules, the explanations and the examples are licensed on the same terms as
anything executable here. Copy them, adapt them, ship them inside your own systems.

## Contributing

Changes arrive as pull requests and are read and judged one at a time. A rule earns its
place by the failure it prevents, so the most useful thing a proposal can carry is the
case where the present text goes wrong. Corrections, counterexamples and "this does not
survive contact with my setup" are all welcome.

How to send one, what is checked and the licence terms of a contribution are in
[CONTRIBUTING.md](CONTRIBUTING.md). Security problems go through
[SECURITY.md](SECURITY.md), not through a public issue. For Claude Code users,
`.claude/settings.json` sets the commit attribution in this repository to
`Assisted-by: Claude Code` instead of a `Co-Authored-By` line with an address — a default,
which your own attribution instructions (CLAUDE.md or memory) take precedence over.
