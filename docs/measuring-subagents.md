# Measuring subagents

| Input | What is measured | Limit |
|---|---|---|
| Start/stop hook log | Time, agent identity, optional model/effort, inferred host | A stop without a new start has no measured duration |
| Claude transcript | `assistant.message.model` and top-level `effort` | Only recognised metadata records |
| Codex rollout | `turn_context.payload.model` and `payload.effort` | Observed format, not a stable API |

The detail behind [Measuring subagents](../README.md#measuring-subagents) in the README,
where the limits of this measurement and their re-check date are recorded.

`delegation-contract` asks for every dispatch to be measured: task kind, model, effort,
duration and quality. A third hook takes over the part a program can see.
`hooks/log-subagent.sh` runs on `SubagentStart` and `SubagentStop` and appends one JSON line
per event to `${CLAUDE_PLUGIN_DATA}/subagent-log.jsonl`, the plugin's data directory on the
machine it runs on. Claude normally uses `~/.claude/plugins/data/<id>/`
([Claude plugin documentation](https://code.claude.com/docs/en/plugins-reference), read
2026-09-26); Codex provides
`CLAUDE_PLUGIN_DATA` as a compatibility alias for its own plugin data directory
([OpenAI plugin documentation](https://developers.openai.com/plugins/build/plugins), read
2026-09-28). The plugin ships no measurements of its own.
`imprint-dev check`'s check e guards that `SubagentStop` stays registered in `hooks/hooks.json`,
the same way it guards `SessionStart` and `SubagentStart`; that check protects the
registration, not what the hook does once it runs.

**What a line holds:** the time in UTC, the event, `agent_id`, `agent_type`, `session_id`, the
effort level if the hook input carries one, `model` when present, an inferred `host`, and
on a stop the path of the subagent's transcript. **What it never holds:** the subagent's
answer or any other text it wrote. The script picks the named fields out by pattern and discards the rest of its input, and a test
feeds it an answer and checks that none of it reaches the log. Internal agents, which arrive
with an empty `agent_type`, are not logged. The hook never blocks a run deliberately: it prints
nothing, adds no context and exits 0 on every path.

**Reading it back:** `go run ./tools/imprint-dev measure` (the log defaults to
`$CLAUDE_PLUGIN_DATA/subagent-log.jsonl`; outside a hook, pass `--log`). It pairs each stop
with the latest unused start of the same `agent_id` — some runtimes emit another start on resume —
and prints one row per run: start, agent type, model, effort, duration, and whether the run
went over the target (`--target`, default `5m`). For `host=codex`, the subagent rollout
`turn_context.payload.model` takes precedence; the hook model is a fallback. The common
Codex hook field is documented as the active slug, but whether it names the parent or child
is not yet measured. For Claude and older logs without a host, distinct hook models take
precedence and recognised transcript metadata is the fallback. Effort missing from the hooks falls back to Claude's
top-level `effort` or Codex's `turn_context.payload.effort`. Unknown record shapes supply
nothing; a missing transcript leaves the model `unknown` unless a hook recorded it.
Transcript metadata lists distinct values in first-occurrence order (efforts are
comma-separated) from the entire file, not a verified
per-turn history; a reused agent's file can contain other turns. `--projects DIR` searches `DIR` for
`agent-<id>.jsonl` when the logged path is missing. That filename fallback is Claude-specific;
it does not discover Codex rollout files, which require their recorded path. `--format json` gives the same rows as
JSON. An overrun is reported, not treated as a failure: the command exits 0 whatever it finds.
The **quality** column stays empty: whether the acceptance criterion was met is a judgement,
and the lead enters it by hand. So does the task kind, of which `agent_type` is only a proxy.

**In the repository's four states:** recording available event metadata is
**enforced** by the hook wherever it fires — durations require a pair of events — in the
sense that it no longer rests on anyone's discipline; that it fires in a live session is
measured, and dated, in the [README](../README.md#measuring-subagents). Nothing is stopped, so the five-minute target stays *enforceable, not
enforced* by design. Quality and task kind stay a **behaviour rule**.

## Runtime observations and boundaries

On 2026-09-28, a read of an installed Codex plugin log found three starts and three stops.
The runtime metadata in the referenced rollout reported `0.158.0`; the separately installed
PATH CLI reported `0.157.1`. One reused agent had one start followed by three stops. `measure`
therefore gives the first pair a duration and leaves both later durations unknown. It never
manufactures starts or treats a stop as the next start.

The same read observed `model` and `effort` in structured Codex `turn_context.payload`
records. Tests use synthetic values with that shape and retain Claude fixtures.
[OpenAI's hooks reference](https://learn.chatgpt.com/docs/hooks), read 2026-09-28,
documents the common hook `model` input and warns that transcript formats are not stable.
The old installed logger omitted `model` unconditionally, so absence in its log does not
prove absence in hook input. The parent/child meaning of the common model field remains
unverified; the Codex rollout therefore takes precedence when readable. These observations
prove the inspected events, not every Codex surface or version.

`host` follows the shared R-HOST v2 path heuristic: check `CLAUDE_PLUGIN_DATA`, then
`CLAUDE_PLUGIN_ROOT`, against `CODEX_HOME` (default `~/.codex`) and `CLAUDE_CONFIG_DIR`
(default `~/.claude`). Otherwise it is `unknown`; logging still works. This is not host
attestation. Mixed paths use the first match; symlinks, overlapping homes and trailing
slashes can misclassify or miss a host. No runtime identity is inferred from an absent
Codex signal alone. Host classification does not select a model or confer permissions.

The shell field extractor is not a full JSON parser: an unexpected nested object with
a `model` key can be mistaken for the top-level field. The documented subagent payload
is the input contract; arbitrary nested payloads are not validated. No new parser
dependency was introduced for that unobserved shape.

Hook registration, trust, and actual invocation remain separate checks. No tool allowlist,
model mapping, or hook trust guarantee follows from the measurement log.
