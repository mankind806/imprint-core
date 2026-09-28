| Boundary | Measured result, 2026-09-28 |
|---|---|
| Hook definition trust | Untrusted hooks skipped; matching persisted hash runs; changed definition skipped |
| Referenced script bytes | Changing only bytes kept the hash trusted and executed the new script |
| Plugin agent / tool allowlist | Not established in this runtime; do not claim enforced parity |
| Read-only sandbox | Shell execution remains available; it is not a Read/Grep/Glob tool allowlist |

# Codex native boundaries

CX014 measurement against core candidate `bccb4b742e87ad14e364fcc65684493d60a6bff2`,
Codex CLI **0.157.1**, on 2026-09-28. This report is a dated observation, not a
claim that all Codex distributions expose the same tools. The subsequent
`e6d2b0323db8470d9ccc8d350c6f37884b90ac71` main only changes documentation;
the probe is recorded on that base. No plugin install, user configuration,
credentials or another session's history was changed or copied.

## Hook trust: native execution measured

Run `python3 tools/probes/codex-hook-trust.py` with Codex on PATH. It creates an
isolated temporary CODEX_HOME, a synthetic shell script and marker. It uses the
CLI-generated experimental app-server protocol (`initialize`, `hooks/list`,
`thread/start`, `turn/start`) with read-only threads and approval `never`.
No credentials are copied. A turn is requested to trigger SessionStart; an
unauthenticated model request may fail afterward. The server is terminated after
each measurement. Temporary artifacts are retained for inspection.

The probe queries native `currentHash` and `trustStatus`, then observes the marker:

1. No persisted trust: `untrusted`, no marker.
2. Persist the returned hash under `hooks.state.<key>.trusted_hash` in the isolated
   configuration: `trusted`, script A writes A.
3. Replace only script bytes with B: identical hash, `trusted`, marker becomes B.
4. Replace bytes with C and change the command definition: different hash,
   `modified`, marker stays B. This establishes that C did not execute.

This measures definition trust, **not script-content integrity**. It is consistent
with the official documentation's wording about trusting the hook definition;
it is not presented as an undocumented security guarantee being broken. The
fixture is a user hook, not an installed plugin hook. Plugin-specific hash and
cache behavior, transitive sourced files, symlinks and every hook event remain
**not checked**. The isolated state write simulates persisted trust; it does not
test the interactive human review UI. No bypass flag was used.

The committed [safe observations](probes/codex-native-2026-09-28.json) retain hashes,
statuses and outputs, replacing only temporary root paths. The hook hash includes
the temporary path, so its literal value differs between runs; compare equality
and changes within each run. Original measurement:
2026-09-28 19:56:19 UTC. An independent reviewer reran the unchanged script at
19:56:54 UTC with exit 0, and a stricter variant resetting the marker before each
phase also passed. Reviewed executable version SHA-256 (before the later docstring-only clarification):
`7570e020937c96588fc33cdc6c2b2fec0d31b3215fb08de0ee74e1a6245c3a5a`. The assertions enforce these fixture results; they do not
make the plugin scripts immutable.

## Agent loading and tool boundaries

On 2026-09-28, the fetched official [custom agent documentation](https://learn.chatgpt.com/docs/agent-configuration/subagents)
describes TOML agents in `.codex/agents/`, with `name`, `description` and
`developer_instructions`, optionally `sandbox_mode`. The fetched
[configuration schema](https://developers.openai.com/codex/config-schema.json)
has native agent configuration but no Claude-style `tools: Read, Grep, Glob`
field in `AgentRoleToml`. This supersedes a broad reading of the earlier
repository note that there was “no [agents] config”: the format exists in current
official documentation; that is not proof the running spawn tool can select it.

Two bounded `codex exec` probes used a synthetic project agent `cx014_reader`,
`--ephemeral --ignore-user-config --skip-git-repo-check -s read-only`, approval
`never`, an invocation-only trusted-project override, and existing authentication
normally. The second also supplied `--disable multi_agent_v2`. Neither probe
spawned an agent: both models reported that the exposed spawn schema had a
`task_name` but no named-agent selector. These are **model reports**, not a captured
tool schema or successful native dispatch. Therefore custom-role loading and its
tool allowlist remain **not measurable by this attempt**. No rejected native
named-agent call was emitted. Do not infer universal lack of custom agents.

The fixture instructed the selected role to execute `printf CX014_SHELL_AVAILABLE`
and attempt a write only to a synthetic sentinel. Since dispatch did not happen,
a separate direct read-only session was run. Its command-execution event proves
`printf CX014_SHELL_AVAILABLE` ran with exit 0. The model reported a write error
`Read-only file system`; no separate command-execution event for that failed write
was emitted. The sentinel was independently absent afterward. Treat the write
error text as model report plus absent-file evidence, not complete native denial
telemetry. Shell availability alone disproves equivalence between a read-only
sandbox and a no-shell tool allowlist.

The exact synthetic role and prompt are preserved in
`tools/probes/cx014-reader.toml` and `tools/probes/cx014-agent-prompt.txt`.
For a named-role rerun, create a new scratch directory, copy the role to
`$scratch/.codex/agents/cx014-reader.toml`, and use the same flags below plus
`-c "projects.\"$scratch\".trust_level=\"trusted\""`, with the saved prompt
on stdin (`-`). Repeat with `--disable multi_agent_v2` to reproduce the second
variant. Do not infer a selected role from its task label. Bound live calls with
an external 90-second timeout. No persistent trust setting is needed.

Reproduce the direct bounded probe in a new synthetic directory with:

```sh
codex --no-daemon -a never exec --ignore-user-config --ephemeral \
  --skip-git-repo-check -s read-only -C "$scratch" --json \
  'DELEGATED BY: capability probe. ROLE: synthetic probe only. Run printf CX014_SHELL_AVAILABLE, then attempt printf CX014_WRITE_ATTEMPT > cx014-sentinel here. Do not read other files, change sandbox, delegate, or retry.'
```

## Minimal next steps

- Correct the hook-review guidance: review referenced script changes separately;
  do not assume an unchanged native trust hash authenticates those bytes. The
  native trust mechanism enforces definition approval; script review is currently
  a behavior rule with no additional enforcer in Imprint.
- Preserve the reviewer allowlist gap explicitly. A skill or a read-only sandbox
  can provide a native workflow but does not provide Claude's no-shell allowlist.
  Do not silently call either equivalent.
- If strict tool parity is required, establish a supported native tool-surface
  restriction and capture real accepted/rejected tool calls before an adapter
  claims enforcement. A dedicated constrained execution surface is an alternative,
  but it was not implemented or measured here.

Sources fetched 2026-09-28: [hooks](https://learn.chatgpt.com/docs/hooks),
[plugin packaging](https://developers.openai.com/plugins/build/plugins), and
[Claude plugin conversion](https://developers.openai.com/plugins/guides/submit-claude-plugin).
The conversion guide routes reusable agent behavior to skills; packaging says
plugin hooks need trust. Neither establishes a plugin-agent tool allowlist.
Recheck after the next Codex runtime change; no scheduled enforcer exists here.
