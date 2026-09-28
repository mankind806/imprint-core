| Native CLI boundary | Measured on 2026-09-28, Codex CLI 0.157.1 |
|---|---|
| Custom TOML role selection | Explicit config binding exposes `agent_type`; native child dispatch succeeds |
| Read-only role | Reading through shell succeeds; shell write and `apply_patch` are denied |
| Role with shell disabled | Native router rejects shell calls; parent's shell remains available |
| Read/Grep/Glob reviewer parity | **Not established:** these tools are absent and rejected in both roles |

# Codex agent tool boundaries

CX015 measures the **actual Codex host using synthetic, local model responses**.
There is no model inference, login or evaluation of how an LLM follows instructions.
The host parses the configuration, spawns a child, advertises its tool schema and
executes or rejects the requested calls. This distinction is essential: the fixture
chooses tool calls, but never fabricates their results.

Source: the [reproducible probe](../tools/probes/codex-agent-tools.py) and its
[recorded observations](probes/codex-agent-tools-2026-09-28.json), measured on
2026-09-28 against core base `e6d2b0323db8470d9ccc8d350c6f37884b90ac71`.
This supersedes only the agent-dispatch and write-denial measurement gaps described
by the earlier CX014 report. It leaves CX014's hook-trust measurement unchanged.
The separately reported Codex TUI 0.158.0 was **not tested** by this probe.

## Reproduce

On the measured Linux environment, with Python 3, Codex, `/bin/sh`, `cat` and `rg`:

```sh
python3 tools/probes/codex-agent-tools.py
# Optional local raw request/response and event artifacts:
python3 tools/probes/codex-agent-tools.py --keep
```

The default removes its newly created temporary tree; `--keep` retains it and
prints its location to stderr. Do not publish raw requests without inspection.
The JSON on stdout contains only the selected observations. Assertions enforce the
fixture's expectations; they are not a persistent restriction on other sessions.
Each CLI invocation has a 60-second timeout. No plugin is installed or changed.

The probe uses a fresh `CODEX_HOME`, a scratch project and a loopback-only
Responses fixture with `requires_openai_auth = false`. It neither copies credentials
nor reads request headers. API-key environment variables are not inherited.
Shell snapshots are disabled, and the harmless shell probes use `/bin/sh` without
login startup files. User config and installed plugins are not used; normal Codex
runtime initialization may still read platform resources. No global configuration
is edited.

## What actually selected the role

The isolated parent config binds the role explicitly:

```toml
[features]
multi_agent_v2 = true
code_mode = false
code_mode_only = false
shell_snapshot = false

[agents.cx015_reader]
description = "Synthetic native role probe"
config_file = "reader.toml"
```

Both role files contain a unique `developer_instructions` marker and
`sandbox_mode = "read-only"`. The probe varies only this role-level setting:

```toml
[features]
shell_tool = false # true in the positive-control case
```

The captured parent request exposes `collaboration.spawn_agent` with an
`agent_type` property naming `cx015_reader`. A synthetic function call selects
that exact type with `fork_turns = "none"`. Native output returns `/root/probe`;
a separate child request contains the role-only instruction marker and its own
tool schema. This establishes config binding and native dispatch, rather than
inferring selection from a task label or a model's account.

A preliminary diagnostic with `multi_agent_v2 = false` exposed no agent tool in
this invocation. The successful, committed fixture uses the explicit binding and
V2 enabled. Standalone `.codex/agents/*.toml` discovery was not re-tested here;
no claim that it is unsupported follows from this measurement.

## Accepted and rejected calls

With `shell_tool = true`, the child exposes `exec_command` and `write_stdin`.
The host runs `cat`, `rg` content search and `rg --files` successfully against the
synthetic file. These are **shell commands**, not separate Read/Grep/Glob tools.
A shell redirection to a scratch sentinel fails with `Read-only file system`.
A native `apply_patch` call is rejected explicitly by the read-only sandbox and
approval settings. Both sentinels are independently absent afterward.

With `shell_tool = false`, the child schema omits `exec_command` and `write_stdin`.
Forced `exec_command` calls receive native `unsupported call: exec_command`
outputs. The parent still advertises its shell tools: the measured restriction
belongs to the selected role. This is a technical tool-routing restriction, not
prompt obedience. It is narrower than a complete reviewer tool allowlist.

Both child schemas still expose `request_user_input`, `apply_patch`, `view_image`,
`get_goal`, `create_goal` and `update_goal`. The probe invokes `apply_patch` and
observes its denial; it does not invoke the other remaining tools. `view_image`
is an image reader, not a measured replacement for text read/search/glob.
`Read`, `Grep` and `Glob` are absent from both actual schemas, and forced calls to
each receive native `unsupported call` errors. No accepted native text-reader
positive control exists with shell disabled in this measured surface.

## Consequence for Imprint

Do not advertise the restricted `foreign-material-reviewer` as equivalent across
hosts on this evidence. A Custom-Agent TOML can technically remove shell access,
but this fixture then has no measured native text read/search/glob facility.
Leaving shell enabled restores useful reading while failing the no-shell contract.
No production adapter is generated from these probe settings.

A future adapter needs a supported constrained reading surface and fresh accepted
read plus rejected shell/write events. An MCP reader is a possible separate design,
not an implemented or verified result here. Unknown tools being rejected does not
prove all alternative shell/write routes are blocked. Code Mode, MCP/plugin tools,
other model tool surfaces, other sandbox configurations and other Codex versions
remain **not checked**. Recheck on the next runtime change; no scheduled enforcer
is installed.

Official documentation fetched 2026-09-28 describes native custom-agent config
layers and the shell feature flag:
[Subagents](https://learn.chatgpt.com/docs/agent-configuration/subagents) and
[Configuration Reference](https://learn.chatgpt.com/docs/config-file/config-reference).
Those pages explain the candidate mechanism; the runtime observations above supply
the version-specific evidence.
