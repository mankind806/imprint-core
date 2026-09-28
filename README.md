# imprint

**Working rules, shipped as a Claude Code plugin that Codex loads too, for two things that
turn out to be one: several AI coding agents cooperating without ruining each other's work,
and a knowledge base that grows alongside you without quietly rotting.**

<p align="center">
<picture>
  <source media="(prefers-color-scheme: dark)" srcset="docs/img/workbench-dark.svg">
  <img src="docs/img/workbench-light.svg" alt="A workbench seen from above. At the top, the lead session with a clipboard: it hands out the work and writes nothing. Arrows lead down to three cards on the bench: readers with two magnifying glasses, who look in parallel and report back, and writer A and writer B, each with one pen in its own worktree. Only the writers have arrows down to main, where their results land one at a time: first merge A, then the tests; then merge B, then the tests.">
</picture>
</p>

*One workbench, many agents, and one pen per worktree. The lead hands out the work and
never writes; readers look at the same time; each writer works alone; results come back one
at a time, with the tests run after each.*

## Runs in Claude Code and Codex

| Component | Claude Code | Codex |
|---|---|---|
| Core card, at `SessionStart` and `SubagentStart` | measured 2026-09-26, as additional context | measured 2026-09-28, as developer context, in the root session and in subagents |
| The four skills | measured 2026-09-26, in the [eval runs](evals/README.md) | measured 2026-09-28: all four arrive |
| Hooks: session start | measured 2026-09-26 | measured 2026-09-28: `hooks/hooks.json` runs, with `CLAUDE_PLUGIN_ROOT` and `CLAUDE_PLUGIN_DATA` set |
| Hooks: subagent log | measured 2026-09-26 | measured 2026-09-28: fires at start and stop; model from the transcript first, the hook's `model` only as a fallback; effort from the hook, the transcript only where it is empty ([measuring subagents](#measuring-subagents)) |
| Hooks: update watch | host-aware: watches `claude --version` ([update watch](docs/update-watch.md)); end to end not measured | host-aware: watches `codex --version` ([update watch](docs/update-watch.md)); end to end not measured, but the state file `codex-version` was written (measured 2026-09-28) |
| `foreign-material-reviewer` agent | measured 2026-09-13: the `tools:` allowlist holds, at session scope | **not verified**: neither that it loads nor that its allowlist holds ([known limits](#known-limits)) |

Codex (OpenAI; measured 2026-09-28 in `codex-tui`, `cli_version` 0.158.0 per its transcript)
loads the plugin from the same Claude-format manifest; there is no separate Codex manifest.
That TUI observation is separate from the codex-cli 0.157.1 arrival and isolated
user-hook trust probes on the same date; their results are recorded below.
Each host keeps its own data directory, `~/.claude/plugins/data/imprint-imprint` and
`~/.codex/plugins/data/imprint-imprint`. The hooks tell the two hosts apart by the plugin's
paths (rule R-HOST): the update watch follows the host's own version and changelog and stays
silent under an unknown host, and each subagent log line carries the host. Codex requires hook
definition approval. The isolated user-hook probe found that referenced script bytes
are not covered; plugin-hook/cache behavior remains untested
([dated trust measurement](docs/codex-native-boundaries.md)). Under Codex, the agent's
read-only boundary is a behaviour rule, not a technical tool lock.

## The picture in 30 seconds

For anyone working with more than one coding agent who has noticed that the interesting
failures are not bad code. They look like this:

| What goes wrong | What imprint puts in the way | Skill |
|---|---|---|
| Two agents write the same file at once | Readers in parallel, every writer in its own worktree, results merged one at a time | `delegation-contract` |
| A fact that was true when recorded is still quoted as current | One canonical place per fact, how it was obtained recorded on entry, expiry when it is reached for | `knowledge-keeping` |
| A session ends without leaving a way back in | Close as the last step of the work: commit, restore, write the decisions down, name the next step | `session-handover` |
| An assurance sounds careful and was never measured | Run it or look it up before saying it; your own notes are a dated snapshot | `measure-before-asserting` |

If you use a single assistant for single tasks, this is more machinery than you need.

## How it reaches the model

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="docs/img/loading-timeline-dark.svg">
  <img src="docs/img/loading-timeline-light.svg" alt="A timeline of one session in two lanes. In the hook lane, the core card arrives at SessionStart, before the model frames the work, and again at SubagentStart, before a subagent frames its task. In the model lane, the model first decides who leads, whether to dispatch and who holds the pen; only later, when the task matches a skill description, does the full skill load. A bracket under the framing stretch reads: the card is already here; the skill is not yet.">
</picture>

A skill loads when the model recognises that the task matches it, which is *after* it has
decided who leads, whether to dispatch and who holds the pen. So two hooks put a short core
card in front of the model *before* that point, at `SessionStart` and at every
`SubagentStart`; the card is a floor under the skills, not a replacement, and the skills
still work best when you invoke them deliberately at the start of multi-agent work.

## How a session runs

```mermaid
sequenceDiagram
    participant CC as Host (Claude Code or Codex)
    participant H as imprint hooks
    participant L as Lead session
    participant S as Subagent
    participant Log as subagent-log.jsonl
    CC->>H: SessionStart
    H-->>L: core card, as context
    H-->>L: if the host was upgraded:<br/>a note that an update review is due
    Note over L: a skill loads when the task matches it,<br/>or when you invoke it
    L->>CC: dispatch a subagent
    CC->>H: SubagentStart
    H-->>S: the same core card
    H->>Log: one start line
    Note over S: works alone in its own worktree
    CC->>H: SubagentStop
    H->>Log: one stop line, with the transcript path
    S-->>L: a report, which is data and not evidence
    Note over L: merge one result at a time,<br/>run the tests after each
```

The arrows from the hooks are what the plugin is wired to do without anyone asking; the notes
are rules the model follows. How far the log hook is measured in a live session:
[Measuring subagents](#measuring-subagents). Afterwards, `go run ./tools/imprint-dev measure` turns the log into one row per run: model,
effort, duration, and whether it went over the target. The log never holds what an agent
wrote.

## Four kinds of rules

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="docs/img/four-kinds-dark.svg">
  <img src="docs/img/four-kinds-light.svg" alt="Four cards in a two-by-two grid. Enforced, drawn as a closed barrier: something really stops you, for example the pre-push hook refusing an undeclared identity. Enforceable, not enforced, drawn as the dashed outline of a barrier nobody has built: for example the five-minute target for a subagent run, measured but never stopped. Reserved to a person, drawn as a person: the rule's content is a human decision, for example resolving a contradiction instead of taking the newer value. Behaviour rule, drawn as a written note: it holds only as long as the discipline does, for example reading a page for abstraction.">
</picture>

Every rule in every skill says which of the four it is. The second is the one usually left
out and the most useful: it lists the places where a few lines of tooling would pay. Why
the states are four and not three: [docs/skills.md](docs/skills.md#the-four-states).

## What's in the box

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="docs/img/layers-dark.svg">
  <img src="docs/img/layers-light.svg" alt="Two layers. On top, the knowledge layer: knowledge-keeping, with one place per fact, provenance, supersede and expiry on use. One arrow from it, labelled needs a rule on who holds the pen, points to delegation-contract; another, labelled leans on, points to measure-before-asserting. Underneath, as the foundation, the agent collaboration layer: delegation-contract, measure-before-asserting and session-handover.">
</picture>

| Part | Kind | In one line |
|---|---|---|
| `delegation-contract` | skill | Who leads and who advises, the delegation header, readers in parallel, one writer per worktree, review depth by risk, second opinions, which model |
| `measure-before-asserting` | skill | Anything that can be run or looked up is, before it is said |
| `session-handover` | skill | Closing a session as the last step of the work, with no handover note |
| `knowledge-keeping` | skill | One canonical place per fact, provenance on entry, supersede instead of delete, expiry on use |
| `foreign-material-reviewer` | agent | Read-only triage of material you did not write; its tools are an allowlist of `Read`, `Grep` and `Glob` |
| Core card | hooks on `SessionStart`, `SubagentStart` | Inject `hooks/kernkarte.md` as additional context |
| Measuring hook | hook on `SubagentStart`, `SubagentStop` | Appends one JSON line per event to the plugin's data directory |
| Update watch | hook on `SessionStart`, on by default; `IMPRINT_UPDATE_WATCH=0` turns it off | After an upgrade of the host that runs the session, Claude Code or Codex, notes that a review of its changelog is due ([docs/update-watch.md](docs/update-watch.md)) |
| `imprint-dev` | Go tool in `tools/` | `gen` the card payloads, `check` the repository's own rules, `measure` subagent runs |
| Pre-push hook | `.githooks/pre-push`, this repository, opt-in | Refuses undeclared identities and the shapes personal data takes |

Each skill, the agent and the two layers in full: [docs/skills.md](docs/skills.md).

*Measured 2026-09-13 on Claude Code 2.1.269 through `--plugin-dir`, at session scope: the
agent gets exactly `Read`, `Grep` and `Glob`; a subagent dispatch and the filtering of MCP
tools are not measured. Re-check by 2026-12-13.*

## Installation

```
/plugin marketplace add mankind806/imprint-core
/plugin install imprint@imprint
```

The repository is its own marketplace, which is why the name appears twice. Once installed,
the skills are addressed as `/imprint:delegation-contract` and so on, with the agent as
`imprint:foreign-material-reviewer`. Claude also reaches for them on its own when a task
matches their description.

<details>
<summary>Where installation was measured, and on which version</summary>

Both the qualified form above and plain `/plugin install imprint` resolve to the same plugin.

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

</details>

## Status

Early, and deliberately partial.

**0.5.0** adds checks `g` (enforcement classification) and `h` (overdue re-check dates; a
release gate behind `--release`) to `imprint-dev`, and the hook that measures how long a
subagent ran. Released 2026-09-26, without behavioural skill evals; those are the next build
step.

**0.6.0** (released 2026-09-27):

- Card line: pictures first for anything a person reads (#5).
- Table of all 12 card lines against their enforcement; check `g` reads it too (#8).
- Fix for two CodeQL alerts in `imprint-dev` (#11).
- `claude plugin eval` suite for all four skills, under `evals/` (#7).
- Check `e` now requires `SubagentStop` to actually run the measuring hook (#13).
- CI fails on zero tests or a dirty working tree (N16, #13).
- Doc fixes (#13).
- `tools/arrival-test.sh`: a release-time check that a fresh session actually receives the
  card this release ships — see [Core card and checks](#core-card-and-checks).

Not included: the update watcher (PR #12).

**0.7.0** (2026-09-27; ships when the owner merges it):

- Update watcher: on by default, checks once at session start, and only for an upgrade;
  set `IMPRINT_UPDATE_WATCH=0` to turn it off (#12).
- Co-browsing exception in the delegation contract: while the person is watching a shared
  browser session, the lead may drive the one visible tab itself (#15).
- CONTRIBUTING.md and the pull request template now match the repository's
  `squash_merge_commit_message: PR_BODY` setting — the pull request text becomes the squash
  merge commit message (#15).

**0.8.0** (2026-09-28; ships when the owner merges it):

- Runs in Claude Code and Codex, measured as the table under
  [Runs in Claude Code and Codex](#runs-in-claude-code-and-codex) says (c5e5a3d).
- The update watch follows the host that runs it, by R-HOST v2 (c34c421); each subagent log
  line carries the host, and `imprint-dev measure` reads a Codex transcript's `turn_context`
  (6e7b0e8).
- Checks `i` (`hook-env-portable`) and `j` (`hook-host-binary`) (5abc4df).
- `tools/arrival-test-codex.sh`, a Codex arrival test (45c3b8d); its offline fixture test runs
  in CI (499f56f).
- Live arrival runs, measured 2026-09-28 at worktree HEAD bccb4b7 with 0.8.0 installed:
  Claude Code 2.1.284, `tools/arrival-test.sh` exit 0, "PASS (Claude Code only)", also with
  `--plugin-dir .`; codex-cli 0.157.1, `tools/arrival-test-codex.sh` exit 0, "PASS (model
  report)". The Codex pass is model-reported arrival (card and skill catalog match), not proof
  that a hook ran; that run also printed one nonfatal CLI error item warning, message not
  captured.
- Superseding the earlier hook-trust uncertainty (c5e5a3d): the isolated user-hook
  measurement under codex-cli 0.157.1 on 2026-09-28 binds trust to the definition,
  not referenced script bytes. Plugin-hook/cache behavior remains untested;
  [measurement and limits](docs/codex-native-boundaries.md).
- Still not verified under Codex: the `foreign-material-reviewer` agent's allowlist.

**Each of the four skills goes back to text that had at least one
adversarial read by a party that did not write it, but not every current version has had
one.** **Not read yet:** `session-handover` as rewritten
on 2026-09-26 without a handover note. **No read recorded:** `knowledge-keeping` as one merged
skill, the 2026-09-26 changes to `measure-before-asserting`, and the 2026-09-26 measurement
rows of `delegation-contract` (see [Measuring subagents](#measuring-subagents)).

<details>
<summary>What is still open</summary>

- **Whether any limit truncates a skill description.** No longer as open as it was: this
  release's own tooling checks (see [Core card and checks](#core-card-and-checks)) that each
  skill description stays at or under 400 characters and all of them together at or under
  3000, and CI runs it wherever GitHub Actions is enabled. Measured locally 2026-09-26 with
  `imprint-dev check`: four skills at 1572 characters combined, none near the cap. Still unmeasured is whether the
  *host*, Claude Code itself, imposes any separate limit of its own; no specification for one
  has been located either way. *Re-check by 2026-12-15.*
- **The remaining rows that say no mechanism exists have not been re-searched.** The most
  recent round found two places where this repository declared a mechanism impossible or
  harmful and was wrong both times, because nobody had gone looking — an over-cautious
  assurance is the kind nothing ever makes fail. Those two are corrected and now say where
  they looked. The other behaviour-rule rows across the four skills carry no such sentence
  yet. *Re-check by 2026-12-15.*

</details>

How both layers came to ship and the review history in full: [docs/status.md](docs/status.md).
Expect the structure to move again before it settles.

## Going deeper

### Known limits

Each limit says what kind of claim it is and when to look again; why each one matters is in
[docs/known-limits.md](docs/known-limits.md).

- **Whether plugin hooks run in claude.ai cloud sessions: unmeasured, and not documented
  anywhere we could find.** *Re-check by 2026-12-13.*
- **Whether a plugin's skills and agents are available in the IDE integrations the same way
  they are in the terminal: unmeasured.** *Re-check by 2026-12-13.*
- **A plugin cannot ship a `CLAUDE.md`: measured 2026-09-13 on Claude Code 2.1.269 through
  `--plugin-dir`; an installed plugin is not separately measured.** *Re-check by 2026-12-13.*
- **`hooks`, `mcpServers` and `permissionMode` in a plugin agent's frontmatter are silently
  ignored: carried over from the first release's notes, and the source for it was not
  re-located when this section was written.**
  *Measure the three ignored fields, or cite a source for them, by 2026-12-13.*
- **Under Codex, whether the `foreign-material-reviewer` agent loads and whether its `tools:`
  allowlist holds: not verified.** Checked 2026-09-28: no named plugin agent in Codex's spawn
  schema, and no `[agents]` config; `model: sonnet` is a Claude model name. Until verified,
  the read-only boundary there is a behaviour rule, not a technical tool lock.
  *Re-check by 2026-12-28.*
- **Hook trust covers definitions, not referenced script bytes in the measured user-hook
  case.** Measured 2026-09-28 with codex-cli 0.157.1 and isolated `CODEX_HOME`.
  Plugin hooks and cache paths remain untested. See the canonical
  [measurement and limits](docs/codex-native-boundaries.md).
  *Re-check by 2026-12-28.*

### Core card and checks

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="docs/img/card-flow-dark.svg">
  <img src="docs/img/card-flow-light.svg" alt="A flow from top to bottom: hooks/kernkarte.md, the core card written by hand, goes through imprint-dev gen into session-start.json and subagent-start.json, which hooks/hooks.json serves at SessionStart and SubagentStart. Beside it, imprint-dev check points with dashed arrows at the card and at the JSON: it checks the card's size and that the JSON is still what gen would produce, and it runs in CI on every push and pull request.">
</picture>

The card is hand-written, its two JSON payloads are generated from it, and `imprint-dev check`
guards both, among other things the skill descriptions, the hook wiring, the plugin version,
the enforcement tables and overdue re-check dates. What each check enforces:
[docs/core-card-and-checks.md](docs/core-card-and-checks.md).

Measured 2026-09-26 on one Linux setup, Claude Code 2.1.283, with `claude -p --plugin-dir` in a
fresh session against this repository's working tree: both the `SessionStart` and the
`SubagentStart` payload arrive as additional context. That is a measurement of this working
tree, not of an installed copy from the marketplace; the two can differ whenever an installed
plugin has not picked up a `main` change. Native Windows is **not measured**; treat the card's
arrival there as unknown.

Measured 2026-09-26 with `imprint-dev check` on `main`: the card holds 1421 characters and 12
non-empty lines, which is the line limit, so a new rule can join it only by replacing or
merging an existing line.

### The arrival test

`tools/arrival-test.sh` asks a fresh, non-interactive session to reproduce the `SessionStart`
card verbatim, then checks whether one chosen line of `hooks/kernkarte.md` — by default, the
line this repository's history added most recently — actually arrived. It costs one billed
`claude -p` call, is not part of CI (it needs a logged-in account), and is meant to run once
per release: against the release worktree, and again against whatever is already installed.
See [Contributing](#contributing) for when, and
[docs/core-card-and-checks.md](docs/core-card-and-checks.md) for what it checks and its exit
codes.

**Measured 2026-09-27** on Claude Code 2.1.283, model `haiku`. Against the 0.6.0 release
worktree (`--plugin-dir`): the card carried this release's added line ("Anything a person
reads …") verbatim — exit 0. Against the plugin installed from the marketplace at the time
(`imprint@imprint`, version 0.5.0): the same line did not arrive; the card stopped at 11 lines
without it — exit 1. That is this tool proving Befund 1 from the 2026-09-27 review: a finished
release can sit on `main` without a single installed session ever seeing it, because
`.claude-plugin/plugin.json`'s version is the only thing anyone's install picks up.

### Measuring subagents

`hooks/log-subagent.sh` writes one line per subagent start and stop; `imprint-dev measure`
reads them back. What a line holds, what it never holds, and how the rows are built:
[docs/measuring-subagents.md](docs/measuring-subagents.md).

**Measured live.** Measured 2026-09-26 on Claude Code 2.1.283 in a live `claude -p` session
with `--plugin-dir`: one general-purpose subagent produced one start and one stop line, and
`measure` read model `claude-sonnet-5`, effort `high`, 10 s. Measured 2026-09-26 with 0.5.0
installed from the marketplace, in a fresh session: one subagent again produced a start and a
stop line in the plugin's data directory; the start line carries an empty effort, the stop
line `high`.

**Under Codex.** Measured 2026-09-28 from the log in Codex's data directory: three starts and
three stops. The lines, written by the 0.7.0 hook, held an `agent_id`, `agent_type` `default`
and an empty effort; that logger did not record a model, so whether Codex's subagent hook
input carries one is not verified. Start and stop pair by `agent_id`, but one agent can log
one start followed by several stops (follow-up turns); `measure` gives the first pair a
duration and reports the later stops as gaps rather than guessing one. For a run whose log
line says host `codex`, the model comes from the transcript's `turn_context` first; a `model`
in the hook input is used only when the transcript has none, because whether it names the
subagent's model or the parent's is not verified; Codex's
[hooks documentation](https://learn.chatgpt.com/docs/hooks) calls it only the "Active model
slug" (read 2026-09-28). The effort, in both hosts, is the hook's when it is not empty, and the
transcript fills only an empty one; with the empty hook effort above, under Codex it comes
from `turn_context` in practice. Transcript values are every distinct value over the whole
file, in order of first appearance and joined by commas (an effort of `medium,high`, say), not
per turn, so a reused agent's row can list values from other turns. Under Claude Code nothing
changes: the subagent hook input has no model (per Claude Code's hooks documentation, read
2026-09-26), so it comes from the transcript.

**Limits.** Tested with invented input under `sh` in CI and locally. Whether it fires for
background agents is **not measured**, and neither is native Windows, where the hook needs an
`sh` on the path. Whether the hook's `agent_id` equals the id in the transcript file name is
**not measured** either; `--projects` depends on it. *Re-check by 2026-12-26.*

### The pre-push hook

For this repository, not for the plugin: `.githooks/pre-push` refuses a push that carries an
undeclared identity or the shape of personal data, and it fails closed. It does not arrive
with a clone. How to switch it on, and what it does not enforce:
[docs/pre-push-hook.md](docs/pre-push-hook.md).

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
