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
