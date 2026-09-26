---
description: >-
  Weekly read-only review of this repository. Files at most five issues
  (defects, missing tests, feature ideas); never changes code.
on:
  schedule: weekly on monday around 05:00 # Monday early morning UTC; the compiler picks a fixed minute
  workflow_dispatch:

# The agent job only reads. The one write, creating issues, runs in the
# separate safe-outputs job with its own scoped token.
permissions:
  contents: read
  issues: read

engine: claude

# Cost cap for the agent job in AI Credits (1 AIC = 0.01 USD; default 1000).
# The repository is about 290k characters of text; one full read-through
# should stay well under 300. The proxy steers the model to finish at 80%.
max-ai-credits: 300
timeout-minutes: 20

# No outbound domains for the agent: it reads the checked-out tree and
# talks to the model and the GitHub MCP server through gh-aw's proxies.
network: {}

tools:
  github:
    toolsets: [issues]
    # Same level gh-aw applies to public repositories by default: only
    # content from trusted authors reaches the agent. Issues this workflow
    # filed are authored by the Actions bot, so the label lifts them; only
    # people with triage rights can set a label.
    min-integrity: approved
    approval-labels: [from-weekly-review]

safe-outputs:
  create-issue:
    title-prefix: "[weekly-review] "
    labels: [from-weekly-review]
    max: 5
    # Mechanical backstop to the dedup step in the prompt.
    deduplicate-by-title: true
  # No other write paths: no failure, failed-job or incomplete-run issues.
  report-failure-as-issue: false
  report-failed-jobs: false
  report-incomplete:
    create-issue: false
  # The detection job reviews the agent's output before any issue is filed.
  # It runs a small model and has its own cap (default 400); 100 is an
  # estimate to adjust after the first runs.
  threat-detection:
    max-ai-credits: 100
---

# Weekly review

You review this repository once a week and report what you find as GitHub
issues. You only read. You do not change files, open pull requests or
comment on anything; the only output you can produce is a new issue.

## 1. Read what is already reported

Before you read the code, list the **open** issues that carry the label
`from-weekly-review`. Read their titles and bodies. Do not file anything
that one of them already covers, even in different words. If a finding
adds real evidence to an existing issue, drop it this week; do not file a
near-duplicate.

## 2. Read the repository

Read, in this order, whatever of these exists:

- `README.md`
- `docs/`
- `skills/` (every `SKILL.md` and the files next to it)
- `tools/imprint-dev/` (Go sources and tests)
- `evals/`
- `hooks/`, `.claude-plugin/`, `CONTRIBUTING.md`, `SECURITY.md`

The core card (the short rule list the hooks inject) and the skills are the
product. The checks in `tools/imprint-dev` are how the repository enforces
its own rules.

## 3. Look for three kinds of findings

**(a) Defects and contradictions.** Concrete errors, or places where two
files say different things (a rule text against its check, a doc against
the code, a count or version that does not match). Cite every side as
`path:line`.

**(b) Missing tests.** A behaviour in `tools/imprint-dev` or a rule the
docs say is enforced, with no test or check that would fail if it broke.
Name the function or rule and where a test would go.

**(c) Feature ideas, at most two.** Only ideas that serve the core card's
rules (cooperating agents without clobbering each other's work, knowledge
that stays true over time). No general tooling wishes.

## 4. Rules for every finding

- Back each claim with a `path:line` citation or a quoted line. If you
  cannot, mark the sentence **(unverified guess)** or leave it out.
- Say how you checked it (which files you read and compared).
- No names of people, no user names, no email addresses, no local or
  absolute file-system paths. Use repository-relative paths only.
- Prefer fewer, stronger findings over many weak ones. Filing nothing is a
  valid result if nothing clears the bar.

## 5. Output

File at most five issues in total across (a), (b) and (c), strongest
first. One finding per issue. Title: short and specific (the prefix is
added for you). Body:

```
**Kind:** defect | contradiction | missing test | feature idea
**Where:** path:line[, path:line]
**What:** two to five sentences.
**Evidence:** the quoted lines or the comparison you made.
**Suggested next step:** one sentence.
```

If you find nothing worth filing, file no issue and report that no action
was needed.
