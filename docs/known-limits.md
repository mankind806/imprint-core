# Known limits

The dated state of each limit and its re-check date are in the README under
[Known limits](../README.md#known-limits). This page holds why each one matters.

Each of these says what kind of claim it is — measured here, or merely carried forward — and
carries a date by which it should be looked at again. A named gap without a date stops being
a gap and turns into how the system simply is. The horizon is three months for all of them:
Claude Code and Codex ship frequently enough that a longer one would be fiction, and often
enough that a shorter one would be busywork.

## Plugin hooks in claude.ai cloud sessions

This release ships hooks on `SessionStart`, `SubagentStart` and
`SubagentStop` (see [Core card and checks](core-card-and-checks.md) and
[Measuring subagents](measuring-subagents.md)), so the question is no longer moot —
but cloud sessions specifically are still not measured either way; do not assume the card
fires there just because it fires elsewhere.

## Skills and agents in the IDE integrations

The plugin documentation does not mention IDE
support either way, and we have not tested it.

## No `CLAUDE.md` from a plugin

A hook is the route in
instead: the `SessionStart` and `SubagentStart` hooks inject a summary card (see
[Core card and checks](core-card-and-checks.md)), so the full skills still take effect only
when invoked.

## Ignored fields in a plugin agent's frontmatter

It is why the agent relies on `tools:` and
claims nothing else — a conservative choice that costs nothing even if the claim turns out
to be wrong. "Silently" is the load-bearing word: there would be no error to notice either
way, which is precisely why this one wants a measurement rather than a re-reading. The
positive half *is* now measured: in a plugin agent's frontmatter, `tools:` and `model:`
are both honoured — see the `foreign-material-reviewer` entry in the
[README](../README.md#whats-in-the-box), where the same run
that showed the three-tool allowlist also showed the session running on the model the
frontmatter names rather than the session default. So plugin agent frontmatter is read
**selectively**, and which fields survive is a per-field question.

## Codex

Codex loads the plugin, and most of it is measured there (measured 2026-09-28; the table is
in the README under [Runs in Claude Code and Codex](../README.md#runs-in-claude-code-and-codex)).
Two points are not verified:

- **The `foreign-material-reviewer` agent.** No named plugin agent appeared in Codex's spawn
  schema, and no `[agents]` config was found, so neither loading the agent nor its `tools:`
  allowlist is verified there; `model: sonnet` is a Claude model name. This is the one that
  matters for safety: in Claude Code the allowlist is the enforced control, and the reason
  the agent exists. Under Codex, treat foreign material by the same procedure, but the
  read-only boundary is then a behaviour rule, not a technical tool lock.
- **Hook approval.** Codex keeps a trust hash per hook definition, and the user approves the
  hooks once. Whether that hash covers the scripts the definitions call is not verified; the
  documentation speaks of "the exact hook definition". If it covers only the definition, a
  changed script runs without a new approval; if it covers the script, a release that
  changes one asks again. So changes to the hooks may require re-approval in Codex, and
  neither outcome is promised here.

## A gap named elsewhere

One further gap is named inside `delegation-contract` rather than here, because it is a
property of the rules and not of the packaging: a triage agent's *findings* flow back into
an agent that does hold Bash and write access, and nothing marks that return as foreign.
That seam is named, not closed.
