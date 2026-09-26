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
claude plugin eval . --trust-plugin --max-cost-usd <CAP>
```

Replace `<CAP>` with a hard cost ceiling in USD before running — every run
and every `llm`/`baseline` grader is a real, billed model call. Useful
variants:

```bash
# One arm only, cheaper, while iterating on a single case
claude plugin eval . --case delegation-contract-trigger-de-1 --runs 1 --ablation none

# Only the free-to-score trigger and negative cases (no llm graders)
claude plugin eval . --tag trigger --tag negative --max-cost-usd <CAP>
```

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

Both runs used Claude Code 2.1.283, judge model haiku (the default), `--runs 3`, both arms.

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
| `knowledge-keeping-behavior` | Prompt now asks to correct an earlier decision with a due date, so source/date/due-date/supersede must show up | 0.33 | 0 | +0.33 |
| `delegation-contract-behavior` | Prompt sharpened once more mid-run to `Delegate this to subagents: ...` after the first rewrite dropped every delegation cue and never fired the skill | 0/3 (skill fired 3/3, judge still voted FAIL FAIL FAIL) | 0.33/3 | -0.33 |

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
