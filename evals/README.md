# Plugin eval suite

Cases for `claude plugin eval`, one set per skill in `skills/`. Each case is a
directory under `evals/` with a `prompt.md` (frontmatter + prompt) and one or
more files under `graders/`. See `claude plugin eval --help` and
[Test plugins with evals](https://code.claude.com/docs/en/plugin-evals)
(checked 2026-09-26) for the full format.

## What the suite covers

Per skill, six cases:

| Case                         | Checks                                                                  | Grader(s)                          |
| :---------------------------- | :----------------------------------------------------------------------- | :----------------------------------- |
| `<skill>-trigger-de-1/2`      | A German prompt phrased the way a user would type it fires the skill    | `tool_used` on `Skill`             |
| `<skill>-trigger-en-1/2`      | The same in English                                                     | `tool_used` on `Skill`             |
| `<skill>-negative`            | A similar-sounding prompt does **not** fire the skill                  | `tool_used`, `min: 0 max: 0`, `arm: both` |
| `<skill>-behavior`            | With the skill loaded, the outcome visibly differs from without it     | `llm` or `regex`/`tool_used`       |

24 cases in total (4 skills x 6). The `-behavior` cases rely on the default
two-arm run (`--ablation with-without`, the default whenever a plugin
resolves) for the with/without comparison; none of them use the `baseline`
grader type, since that needs a recorded reference transcript.

All cases grant only `[Read, Glob, Grep, Skill]` in `allowed_tools` — no
case can write, send, or delete anything. `measure-before-asserting-behavior`
seeds a throwaway `requirements.txt` fixture via `context.scaffold_script`
(only runs with `--scaffold`, and only touches the run's own temporary
workspace).

## Running the suite

```bash
claude plugin eval . --trust-plugin --judge-model sonnet --max-cost-usd <CAP>
```

Replace `<CAP>` with a hard cost ceiling in USD before running — every run
and every `llm`/`baseline` grader is a real, billed model call.
`--judge-model sonnet` is here because the default (haiku) misjudged a
correct `delegation-contract-behavior` response 3/3 times in run 2 below;
it costs more per judge vote but there is no cheaper per-case way to fix
that, since the case format has no per-case or per-grader judge-model
field. Useful variants:

```bash
# One arm only, cheaper, while iterating on a single case
claude plugin eval . --case delegation-contract-trigger-de-1 --runs 1 --ablation none

# Only the free-to-score trigger and negative cases (no llm graders)
claude plugin eval . --tag trigger --tag negative --max-cost-usd <CAP>

# The behavior cases (llm-graded): the case format has no per-case judge-model
# field (checked against the docs and prompt.md/case.yaml frontmatter,
# 2026-09-26), so use a stronger judge for these on the command line
claude plugin eval . --tag behavior --judge-model sonnet --max-cost-usd <CAP>
```

Only 3 of the 4 `-behavior` cases have an `llm` grader —
`delegation-contract-behavior`, `knowledge-keeping-behavior`, and
`session-handover-behavior`; `measure-before-asserting-behavior` uses only
`tool_used` and `regex`, so `--judge-model` has no effect on it. As of this
README, only `delegation-contract-behavior` has actually been re-run with
Sonnet (run 3, below); `knowledge-keeping-behavior` and
`session-handover-behavior` are still scored from run 2 under the default
Haiku judge.

Per the docs (checked 2026-09-26): if the account's usage limit or an API
rate limit is hit partway through a run, every later run ends with that
error and is graded on what it produced — usually 0 — with no warning and
without the suite being marked `partial`. Check the `NOTES` column or
`cases[].arms.with[].error` in the JSON result before trusting a low score,
and prefer `--max-cost-usd` over hoping the plan limit isn't reached first.

## Model-call cost for a full run

`claude plugin eval` has no mode that validates or lists cases without a
model call (confirmed against `claude plugin eval --help` on Claude Code
2.1.283, 2026-09-26); the closest read-only checks are `claude plugin
validate .` (manifest and skill frontmatter only, not eval cases) and the
static YAML/regex checks used while authoring this suite.

For `--runs 3` with both arms (the defaults), the docs give the model-call
count as roughly `cases x runs` agent sessions per arm, plus three short
judge votes per `llm`/`baseline` grader per run:

- Agent sessions: 24 cases x 3 runs x 2 arms = **144**
- Judge votes: 3 `llm` graders (one per `-behavior` case, weighted 2, on
  `delegation-contract`, `knowledge-keeping`, `session-handover`) x 3 runs x
  2 arms x 3 votes = **54**
- Total: **198** model interactions, on top of whatever each of the 144
  agent sessions itself takes internally (each is capped by that case's
  `max_turns`, 10 or 15 here)

The fourth `-behavior` case (`measure-before-asserting`) uses only
`tool_used` and `regex` graders, so it adds agent sessions but no judge
votes.

## Actual runs so far

All three runs used Claude Code 2.1.283, `--runs 3`, both arms. Runs 1 and 2
used the default judge model (haiku); run 3 used `--judge-model sonnet`.

**Run 1 — full suite, 2026-09-26.** `--max-cost-usd` not hit, $11.16, 396s. Overall
83.3% with the plugin loaded vs. 29.9% without; the without-arm never loaded a
skill in any case. Five weak cases found:

| Case | Problem |
| :--- | :--- |
| `delegation-contract-trigger-de-2` | Skill never fired (0/3) — prompt had no actual proposal to react to |
| `measure-before-asserting-trigger-de-2` / `-en-2` | Skill never fired (0/3) — Claude read the real dependency file correctly with or without the skill, so there was nothing to catch |
| `session-handover-trigger-en-2` | Skill never fired (0/3) — "handing off to a teammate" reads as a normal handoff note, not a session close |
| `delegation-contract-behavior`, `knowledge-keeping-behavior` | Scored 1.0 in both arms (`Δ = 0`) — the task was already done well without the skill |

**Run 2 — six changed cases only, 2026-09-26.** `--tag rerun2`, cap $5, actual
$3.01 + $0.64 for one follow-up fix = **$3.65 total**, 125s + 41s:

| Case | Change | With | Without | Δ |
| :--- | :--- | :--: | :--: | :--: |
| `delegation-contract-trigger-de-2` | Added a concrete short proposal | 0/3 | 0/3 | 0 |
| `measure-before-asserting-trigger-de-2` | Rewritten to tempt an answer from notes/memory | 3/3 | 0/3 | +1.0 |
| `measure-before-asserting-trigger-en-2` | Same, English | 3/3 | 0/3 | +1.0 |
| `session-handover-trigger-en-2` | Replaced with a clear "that's it for today" close | 3/3 | 0/3 | +1.0 |
| `knowledge-keeping-behavior` | Prompt now asks to correct an earlier decision with a due date, so source/date/due-date/supersede must show up | 1/3 | 0/3 | +0.33 |
| `delegation-contract-behavior` | Prompt sharpened once more mid-run to `Delegate this to subagents: ...` after the first rewrite dropped every delegation cue and never fired the skill | 0/3 (skill fired 3/3, judge still voted FAIL FAIL FAIL) | 1/3 | -0.33 |

`delegation-contract-trigger-de-2` still never fires the skill even with a
concrete proposal in it. Per this run, "zweite Meinung" (second opinion) on a
generic proposal reads to Claude as an ordinary review request it can just
answer, not as the multi-agent "get a second opinion from another agent"
concept the skill's description means by that phrase. A description change
was out of scope here, so it stays as a known gap: a clearer worded trigger
phrase for the description would be something like *"jemanden um eine
unabhängige zweite Einschätzung bitten"* / *"bring in a second reviewer"* —
distinct from Claude just being asked to weigh in on a proposal itself.

`delegation-contract-behavior`'s with-arm response was, on inspection of the
trace, an unusually thorough delegation plan (role/boundary/return headers,
one worktree per writer) — but the haiku judge voted FAIL on it in all three
runs. This matches a documented judge failure mode ("a small judge model can
mark a correct answer wrong... tighten the rubric, or use `--judge-model
sonnet`" — [Choose graders that give a stable signal](https://code.claude.com/docs/en/plugin-evals#choose-graders-that-give-a-stable-signal),
checked 2026-09-26). Not re-run with a stronger judge here to stay inside the
$5 cap; the reliable signal from this run is that the skill now fires 3/3,
which it did not before.

**Run 3 — all six `delegation-contract-*` cases, 2026-09-26.**
`--tag delegation-contract --judge-model sonnet`, cap $4, actual **$3.18**,
149s. Two things changed at once, so the two effects below are not cleanly
separated: `skills/delegation-contract/SKILL.md`'s `description` now
triggers on "second opinion from another agent" / "zweite Meinung von
anderen Agenten" instead of the bare "second opinion" / "zweite Meinung",
and the judge for all `llm` graders in this run was Sonnet instead of the
default Haiku.

> **Deviation from the chosen wording.** The option the user picked named
> the new trigger as *"unabhängige zweite Einschätzung von einem anderen
> Agenten" / "independent second reviewer"*. What actually shipped instead
> rewords the *existing* "second opinion" / "zweite Meinung" triggers to
> "second opinion from another agent" / "zweite Meinung von anderen
> Agenten", because the description was already at 399 of the 400-character
> limit (`imprint-dev check`, rule `skill-description-length`) and a first
> draft that kept the original lead-in and added the user's exact phrases
> alongside the old ones came to 557 characters, well over. The lead-in
> "Use when delegating, reviewing or choosing a
> model:" was also cut to "Triggers:" (matching the wording other skills in
> this plugin already use) to make room. No case in this suite tests a bare
> "second opinion" prompt without a proposal, so whether dropping the old,
> unqualified trigger loses something is unmeasured. This sits only on the
> unmerged `feat/evals` branch; the user's exact chosen wording was not
> used, so it should be confirmed or corrected before anyone treats this
> description as final.

| Case | With | Without | Δ |
| :--- | :--: | :--: | :--: |
| `delegation-contract-trigger-de-1` | 3/3 | 0/3 | +1.0 |
| `delegation-contract-trigger-de-2` | 0/3 | 0/3 | 0 |
| `delegation-contract-trigger-en-1` | 3/3 | 0/3 | +1.0 |
| `delegation-contract-trigger-en-2` | 3/3 | 0/3 | +1.0 |
| `delegation-contract-negative` | pass | pass | 0 (expected — `arm: both`) |
| `delegation-contract-behavior` | 1.0 | 0 | +1.0 |

`delegation-contract-behavior` went from Δ 0 to Δ +1.0: the same kind of
role/boundary/return plan that Haiku voted FAIL on 3/3 times in run 2 now
scores 1.0. Since the description and the judge model both changed here,
this is *likely* the Sonnet judge correcting run 2's misjudged verdict
(consistent with the skill firing 3/3 in both run 2b and run 3, unchanged),
but it was not isolated by re-running with the old description or the old
judge, so it is not proven.

`delegation-contract-trigger-de-2` still never fires the skill (0/3, all 6
runs), unchanged from run 2, even with the sharper description. This looks
like a prompt problem, not a description problem: the prompt itself ("Ich
brauche eine zweite Meinung zu diesem Vorschlag...") never says the second
opinion should come from *another agent* — that framing lives only in the
description now, and the prompt still reads as an ordinary request for
Claude's own feedback, which is arguably not something the delegation
skill should be firing on at all.

**Recommendation (untested, needs a rerun before it counts):** turn this
prompt into a second `delegation-contract-negative` case, since the data
already shows it does not fire the skill either way — and add a fresh
`delegation-contract-trigger-de-2` prompt that explicitly asks for another
agent's independent assessment, e.g. "Ich möchte eine unabhängige zweite
Einschätzung von einem anderen Agenten, bevor ich diesen Vorschlag
umsetze: ...". Both new/changed cases would use only free graders; at
roughly $0.10 per agent session (run 2b: $0.64 for 6 sessions; runs 2 and 3:
about $3 for 36 sessions each), 12 sessions comes to about $1 — suggest a
$2 cap to leave headroom. Alternative: reword only this one case's prompt
the same way, without adding a second
negative case. Either way, the merge gate on this case stays unmet until a
rerun confirms it.
