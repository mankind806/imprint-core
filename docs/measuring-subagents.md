# Measuring subagents

The detail behind [Measuring subagents](../README.md#measuring-subagents) in the README,
where the limits of this measurement and their re-check date are recorded.

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
sense that it no longer rests on anyone's discipline; that it fires in a live session is
measured, and dated, in the [README](../README.md#measuring-subagents). Nothing is stopped, so the five-minute target stays *enforceable, not
enforced* by design. Quality and task kind stay a **behaviour rule**.
