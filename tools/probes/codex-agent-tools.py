#!/usr/bin/env python3
"""Measure native role/tool routing with synthetic local Responses, without login.

Creates disposable CODEX_HOME/project state and never reads credentials. Model
responses are fixture-generated; native Codex parses configuration, starts the
child and executes/rejects tool calls. This does not test real-model behavior.
Raw artifacts are retained only with --keep. Requires Codex CLI and Python 3.
"""
import argparse
import datetime
import http.server
import json
import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import threading

ROLE_MARKER = "CX015_NATIVE_ROLE_BINDING"


def tool_names(tools):
    names = []
    for tool in tools:
        if tool["type"] == "namespace":
            names.extend(tool["name"] + "." + item["name"] for item in tool["tools"])
        else:
            names.append(tool.get("name", tool["type"]))
    return names


def function(name, args, call_id, namespace=None):
    item = dict(type="function_call", id="fc_" + call_id,
                call_id=call_id, name=name, arguments=json.dumps(args))
    if namespace:
        item["namespace"] = namespace
    return item


def measure(binary, root, no_shell):
    home, project = root / "home", root / "project"
    home.mkdir(parents=True)
    project.mkdir()
    (project / "sample.txt").write_text("CX015_READ_NEEDLE\n")
    requests = []
    responses = []
    finished = threading.Event()
    lock = threading.Lock()
    child_calls = [
        function("exec_command", {
            "cmd": "cat sample.txt && rg CX015_READ_NEEDLE sample.txt && rg --files -g '*.txt'",
            "login": False, "shell": "/bin/sh", "max_output_tokens": 1000}, "read_via_shell"),
        function("exec_command", {
            "cmd": "printf CX015_WRITE > shell-sentinel", "login": False, "shell": "/bin/sh",
            "max_output_tokens": 1000}, "write_via_shell"),
        dict(type="custom_tool_call", id="fc_patch", call_id="patch",
             name="apply_patch", input="*** Begin Patch\n*** Add File: patch-sentinel\n+CX015_WRITE\n*** End Patch"),
        function("Read", {"file_path": "sample.txt"}, "read_native"),
        function("Grep", {"pattern": "CX015_READ_NEEDLE", "path": "."}, "grep_native"),
        function("Glob", {"pattern": "*.txt"}, "glob_native"),
    ]

    class Handler(http.server.BaseHTTPRequestHandler):
        def log_message(self, *args):
            pass

        def do_POST(self):
            # Neither record nor inspect request headers (including credentials).
            request = json.loads(self.rfile.read(int(self.headers["Content-Length"])))
            child = ROLE_MARKER in str(request.get("instructions", "")) or ROLE_MARKER in str(request.get("input", []))
            outputs = [i for i in request.get("input", [])
                       if i.get("type") in ("function_call_output", "custom_tool_call_output")]
            with lock:
                number = len(requests) + 1
                requests.append(request)
                (root / f"request-{number}.json").write_text(json.dumps(request, indent=2))
            if child and len(outputs) < len(child_calls):
                item = child_calls[len(outputs)]
            elif not child and not outputs:
                item = function("spawn_agent", {
                    "task_name": "probe", "agent_type": "cx015_reader", "fork_turns": "none",
                    "message": "DELEGATED BY: native boundary fixture. ROLE: synthetic probe only. Return DONE."
                }, "spawn", "collaboration")
            elif not child and not finished.is_set():
                item = function("wait_agent", {"timeout_ms": 10000},
                                "wait_" + str(number), "collaboration")
            else:
                if child:
                    finished.set()
                item = dict(type="message", id="done", role="assistant",
                            content=[dict(type="output_text", text="DONE")])
            responses.append(item)
            self.send_response(200)
            self.send_header("Content-Type", "text/event-stream")
            self.end_headers()
            events = [dict(type="response.created", response=dict(id=f"r{number}")),
                      dict(type="response.output_item.done", output_index=0, item=item),
                      dict(type="response.completed", response=dict(id=f"r{number}",
                           status="completed", output=[], usage=dict(input_tokens=1, output_tokens=1, total_tokens=2)))]
            for event in events:
                self.wfile.write(("data: " + json.dumps(event) + "\n\n").encode())

    server = http.server.ThreadingHTTPServer(("127.0.0.1", 0), Handler)
    threading.Thread(target=server.serve_forever, daemon=True).start()
    (home / "config.toml").write_text(f'''model = "gpt-5.4"
model_provider = "fixture"
approval_policy = "never"
sandbox_mode = "read-only"
web_search = "disabled"
[model_providers.fixture]
name = "Synthetic local fixture (no inference)"
base_url = "http://127.0.0.1:{server.server_port}/v1"
wire_api = "responses"
requires_openai_auth = false
request_max_retries = 0
stream_max_retries = 0
[features]
multi_agent_v2 = true
code_mode = false
code_mode_only = false
shell_snapshot = false
[agents.cx015_reader]
description = "Synthetic native role probe"
config_file = "reader.toml"
''')
    (home / "reader.toml").write_text(f'''developer_instructions = "{ROLE_MARKER}"
sandbox_mode = "read-only"
[features]
shell_tool = {str(not no_shell).lower()}
''')
    # Keep just the environment needed to locate/run local binaries, no API keys.
    env = {key: os.environ[key] for key in ("PATH", "HOME", "TMPDIR", "LANG") if key in os.environ}
    env["CODEX_HOME"] = str(home)
    command = [binary, "--no-daemon", "exec", "--ephemeral", "--skip-git-repo-check",
               "-C", str(project), "--json",
               "DELEGATED BY: native boundary measurement. ROLE: synthetic fixture. Spawn the configured probe only."]
    try:
        result = subprocess.run(command, env=env, stdin=subprocess.DEVNULL,
                                capture_output=True, text=True, timeout=60)
    finally:
        server.shutdown()
        server.server_close()
    (root / "stdout.jsonl").write_text(result.stdout)
    (root / "stderr.txt").write_text(result.stderr)
    (root / "responses.json").write_text(json.dumps(responses, indent=2))
    assert result.returncode == 0, result.stderr
    child_requests = [r for r in requests if ROLE_MARKER in str(r.get("input", [])) or ROLE_MARKER in str(r.get("instructions", ""))]
    assert child_requests, "No role-bound child request"
    tools = tool_names(child_requests[0]["tools"])
    parent_tools = tool_names(requests[0]["tools"])
    namespace = next(t for t in requests[0]["tools"] if t.get("name") == "collaboration")
    spawn_schema = next(t for t in namespace["tools"] if t["name"] == "spawn_agent")
    selector = spawn_schema["parameters"]["properties"]["agent_type"]
    assert "cx015_reader" in selector["description"]
    assert "exec_command" in parent_tools, "Parent positive control missing"
    outputs = {}
    for request in requests:
        for item in request.get("input", []):
            if item.get("type") in ("function_call_output", "custom_tool_call_output"):
                outputs[item["call_id"]] = item["output"]
    assert json.loads(outputs["spawn"])["task_name"] == "/root/probe"
    assert finished.is_set(), "Child did not complete all fixture calls"
    assert not (project / "shell-sentinel").exists()
    assert not (project / "patch-sentinel").exists()
    for name in ("Read", "Grep", "Glob"):
        assert name not in tools
    for call, name in (("read_native", "Read"), ("grep_native", "Grep"), ("glob_native", "Glob")):
        assert outputs[call] == f"unsupported call: {name}", outputs[call]
    if no_shell:
        assert "exec_command" not in tools
        assert outputs["read_via_shell"] == "unsupported call: exec_command"
        assert outputs["write_via_shell"] == "unsupported call: exec_command"
    else:
        assert "exec_command" in tools
        assert "Process exited with code 0" in outputs["read_via_shell"]
        assert "CX015_READ_NEEDLE" in outputs["read_via_shell"] and "sample.txt" in outputs["read_via_shell"]
        assert "Read-only file system" in outputs["write_via_shell"], outputs["write_via_shell"]
    assert "apply_patch" in tools
    assert "rejected" in outputs["patch"].lower(), outputs["patch"]
    # Full requests remain optional local artifacts; publish only this safe subset.
    for key in outputs:
        outputs[key] = outputs[key].replace(str(root), "<scratch>")
    return dict(shell_tool=not no_shell, role_binding=True, native_spawn_result=outputs.pop("spawn"),
                role_selector="collaboration.spawn_agent.agent_type=cx015_reader",
                role_binding_marker=ROLE_MARKER, parent_tools=parent_tools,
                child_tools=tools, tool_outputs={k:v for k,v in outputs.items() if not k.startswith("wait_")},
                shell_sentinel_exists=False, patch_sentinel_exists=False)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--codex", default="codex")
    parser.add_argument("--keep", action="store_true", help="retain raw requests in the printed temporary directory")
    args = parser.parse_args()
    binary = shutil.which(args.codex)
    if not binary:
        parser.error("Codex binary not found")
    root = Path(tempfile.mkdtemp(prefix="codex-agent-tools-"))
    try:
        result = dict(measured_at=datetime.datetime.now(datetime.timezone.utc).isoformat(),
                      cli_version=subprocess.check_output([binary, "--version"], text=True).strip(),
                      model_responses="synthetic local fixture; no model inference", host_dispatch="native",
                      cases=[measure(binary, root / "read-only", False), measure(binary, root / "no-shell", True)])
        print(json.dumps(result, indent=2))
        if args.keep:
            print("Raw artifacts retained: " + str(root), file=__import__("sys").stderr)
    finally:
        if not args.keep:
            shutil.rmtree(root)


if __name__ == "__main__":
    main()
