"""Shared harness for running the real bin/ts-done-check as a subprocess, safely.

Safety, enforced here rather than left to the caller:
- TYPESAFE_API_KEY is removed from every child environment; a key only ever comes
  from a fake secret-tool, and a key-printing fake is refused unless the run goes
  through the worker shim, which points ts_common.URL at a local URL.
- TS_DONE_CHECK_TESTING=1, TS_DONE_CHECK_LOG, HOME, TMPDIR and TYPESAFE_NAMES_FILE all
  point into the run's own temp dir; Sandbox refuses a log path outside the temp dir.
- No production hook for the request URL exists. The shim replaces the worker's
  interpreter: the hook is started through `python -c` with sys.executable set to the
  shim, so the supervisor spawns `<shim> bin/ts-done-check --worker ...`; the shim sets
  tc.URL (and an optional test behaviour, TS_TEST_WORKER_MODE) and then runs the real
  worker via runpy. Production code reads none of the TS_TEST_* variables.
"""
import http.server
import json
import os
import socket
import subprocess
import sys
import tempfile
import threading
import time
import uuid

HERE = os.path.dirname(os.path.abspath(__file__))
ROOT = os.path.normpath(os.path.join(HERE, ".."))
BIN = os.path.join(ROOT, "bin", "ts-done-check")
DEAD_URL = "http://127.0.0.1:1/nothing-listens-here"  # nothing listens on port 1
TEST_KEY = "dummy-test-key-from-fake-keyring"
BLOCK_STDOUT = json.dumps({"decision": "block", "reason": (
    "TypeSafe: Erfolgsbehauptung ohne sichtbaren Beleg – bitte erst prüfen "
    "(Befehl/Test ausführen) oder 'nicht geprüft' sagen.")}, ensure_ascii=False) + "\n"

SHIM = r'''#!{python}
import json, os, runpy, sys, time
sys.path.insert(0, {root!r})
import ts_common as tc
tc.URL = os.environ.get("TS_TEST_URL") or {dead!r}
if os.environ.get("TS_TEST_PIDFILE"):
    with open(os.environ["TS_TEST_PIDFILE"], "w") as f:
        f.write(str(os.getpid()))
idx = sys.argv.index("--worker")
script, wargs = sys.argv[idx - 1], sys.argv[idx + 1:]
deadline = float(wargs[1])
mode = os.environ.get("TS_TEST_WORKER_MODE", "")
if mode == "crash_at_keyring":
    tc.get_key = lambda timeout=5, **kw: os._exit(3)
elif mode == "hang_post":
    def _hang(*a, **k):
        time.sleep(60)
    tc.post = _hang
elif mode == "garbage":
    sys.stdout.write("this is not json\n"); sys.stdout.flush(); time.sleep(60)
elif mode == "exit_silently":
    os._exit(0)
elif mode == "bad_final":
    sys.stdout.write(json.dumps({{"final": True, "log": "not an object"}}) + "\n")
    sys.stdout.flush(); time.sleep(60)
elif mode == "final_then_hang":
    sys.stdout.write(json.dumps({{"stage": "parse"}}) + "\n")
    sys.stdout.write(json.dumps({{"final": True, "session_id": "sess-fth", "transcript_path": None,
        "log": {{"claim": 0.9, "backed": 0.1, "decision": "block", "payload_masked": {{}}}}}}) + "\n")
    sys.stdout.flush(); time.sleep(60)
elif mode == "final_at":
    # Hold the final record back until deadline + TS_TEST_FINAL_DELTA_MS.
    target = deadline + float(os.environ["TS_TEST_FINAL_DELTA_MS"]) / 1000.0
    class _Delay:
        def __init__(self, inner):
            self.inner = inner
        def write(self, s):
            if '"final": true' in s:
                while True:
                    left = target - time.monotonic()
                    if left <= 0:
                        break
                    time.sleep(min(left, 0.0005) if left < 0.003 else left - 0.002)
            return self.inner.write(s)
        def flush(self):
            return self.inner.flush()
    sys.stdout = _Delay(sys.stdout)
sys.argv = [script, "--worker"] + wargs
runpy.run_path(script, run_name="__main__")
'''


def free_port():
    s = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
    s.bind(("127.0.0.1", 0))
    port = s.getsockname()[1]
    s.close()
    return port


class _HTTPServer(http.server.ThreadingHTTPServer):
    daemon_threads = True
    request_queue_size = 128  # many parallel hook runs connect at once


class SlowServer:
    """Local HTTP server that sleeps `delay` seconds before answering `status`/`body`.
    Records every request body. delay=None: never answers (holds the socket)."""

    def __init__(self, delay, status=200, body=b'{"answers": {}}', headers=()):
        self.requests = []
        outer = self

        class H(http.server.BaseHTTPRequestHandler):
            def do_POST(self):
                n = int(self.headers.get("Content-Length") or 0)
                outer.requests.append(self.rfile.read(n) if n else b"")
                time.sleep(3600 if delay is None else delay)
                try:
                    self.send_response(status)
                    self.send_header("Content-Type", "application/json")
                    self.send_header("Content-Length", str(len(body)))
                    for k, v in headers:
                        self.send_header(k, v)
                    self.end_headers()
                    self.wfile.write(body)
                except Exception:
                    pass

            def log_message(self, *a):
                pass

        self.httpd = _HTTPServer(("127.0.0.1", 0), H)
        self.port = self.httpd.server_address[1]
        self.url = f"http://127.0.0.1:{self.port}/v1/systemone"
        threading.Thread(target=self.httpd.serve_forever, daemon=True).start()

    def close(self):
        self.httpd.shutdown()
        self.httpd.server_close()


class TrickleServer:
    """200 headers with a large Content-Length, then one body byte every `interval`
    seconds: urllib's timeout= is per recv(), so it never fires on this."""

    def __init__(self, interval, n_bytes, body_target=4000):
        self.port = free_port()
        self.url = f"http://127.0.0.1:{self.port}/v1/systemone"
        self._srv = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
        self._srv.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
        self._srv.bind(("127.0.0.1", self.port))
        self._srv.listen(1)
        threading.Thread(target=self._serve, args=(interval, n_bytes, body_target),
                         daemon=True).start()

    def _serve(self, interval, n_bytes, body_target):
        try:
            conn, _ = self._srv.accept()
        except OSError:
            return
        try:
            conn.recv(65536)
            conn.sendall((f"HTTP/1.1 200 OK\r\nContent-Type: application/json\r\n"
                          f"Content-Length: {body_target}\r\n\r\n").encode())
            payload = (b'{"answers": {}}' + b" " * body_target)[:body_target]
            for b in payload[:n_bytes]:
                conn.sendall(bytes([b]))
                time.sleep(interval)
        except OSError:
            pass
        finally:
            conn.close()

    def close(self):
        try:
            self._srv.close()
        except Exception:
            pass


def answer_body(claim, backed):
    return json.dumps({"answers": {"claim": {"noul": claim}, "backed": {"noul": backed}}}).encode()


class Sandbox:
    """A temp dir with its own log, HOME, TMPDIR, names file, fake secret-tools and shim."""

    def __init__(self, prefix="typesafe-donecheck-"):
        self._tmp = tempfile.TemporaryDirectory(prefix=prefix)
        self.dir = self._tmp.name
        self.log_path = os.path.join(self.dir, "done-check.jsonl")
        home = os.path.join(self.dir, "home")
        tmpdir = os.path.join(self.dir, "tmp")
        os.mkdir(home)
        os.mkdir(tmpdir)
        names = os.path.join(self.dir, "names.txt")
        with open(names, "w", encoding="utf-8") as f:
            f.write("Max Mustermann\nErika Musterfrau\nWanda Wachtmeister\n")
        self.shim = os.path.join(self.dir, "worker-shim")
        with open(self.shim, "w") as f:
            f.write(SHIM.format(python=sys.executable, root=ROOT, dead=DEAD_URL))
        os.chmod(self.shim, 0o755)
        self.fakebins = {}
        env = {k: v for k, v in os.environ.items()
               if k != "TYPESAFE_API_KEY" and not k.startswith(("TS_DONE_CHECK_", "TS_TEST_"))}
        env.update({"HOME": home, "TMPDIR": tmpdir, "TYPESAFE_NAMES_FILE": names,
                    "PYTHONDONTWRITEBYTECODE": "1", "TS_DONE_CHECK_TESTING": "1",
                    "TS_DONE_CHECK_LOG": self.log_path})
        self.base_env = env
        self.base_env["PATH"] = self.fakebin("fail") + os.pathsep + env.get("PATH", "")
        if not os.path.realpath(self.log_path).startswith(
                os.path.realpath(tempfile.gettempdir()) + os.sep):
            raise RuntimeError(f"refusing: log path {self.log_path} is not under the temp dir")

    def cleanup(self):
        self._tmp.cleanup()

    def fakebin(self, kind):
        """Dir with a fake secret-tool: 'fail' (exit 1), 'key' (prints TEST_KEY),
        'sleep:N' (one process that sleeps N s and prints nothing, like a hung
        secret-tool), 'slowkey:N' (sleeps N s, prints TEST_KEY)."""
        if kind in self.fakebins:
            return self.fakebins[kind]
        d = os.path.join(self.dir, "fakebin-" + kind.replace(":", "-").replace(".", "_"))
        os.mkdir(d)
        name, _, arg = kind.partition(":")
        body = {"fail": "exit 1\n", "key": f"echo {TEST_KEY}\n",
                "sleep": f"exec sleep {arg}\n", "slowkey": f"sleep {arg}\necho {TEST_KEY}\n"}[name]
        with open(os.path.join(d, "secret-tool"), "w") as f:
            f.write("#!/bin/sh\n" + body)
        os.chmod(os.path.join(d, "secret-tool"), 0o755)
        self.fakebins[kind] = d
        return d

    def stdin(self, msg, session_id="sess-test-1", transcript=None, **extra):
        d = {"session_id": session_id, "last_assistant_message": msg}
        if transcript is not None:
            d["transcript_path"] = transcript
        d.update(extra)
        return json.dumps(d)

    def _prepare(self, keyring, shim, url, mode, env, executable=None):
        if keyring.startswith(("key", "slowkey")) and not shim:
            raise RuntimeError("refusing: a key-printing secret-tool needs the URL shim")
        e = dict(self.base_env)
        e["PATH"] = self.fakebin(keyring) + os.pathsep + self.base_env["PATH"]
        marker = uuid.uuid4().hex
        e["TS_TEST_MARKER"] = marker
        pidfile = os.path.join(self.dir, f"pid-{marker}")
        if shim:
            e["TS_TEST_PIDFILE"] = pidfile
            e["TS_TEST_URL"] = url or DEAD_URL
            if mode:
                e["TS_TEST_WORKER_MODE"] = mode
        if env:
            e.update(env)
        assert "TYPESAFE_API_KEY" not in e and e["TS_DONE_CHECK_TESTING"] == "1"
        assert e["TS_DONE_CHECK_LOG"].startswith(self.dir)
        if shim or executable:
            cmd = [sys.executable, "-c",
                   f"import sys, runpy; sys.executable = {(executable or self.shim)!r}; "
                   f"runpy.run_path({BIN!r}, run_name='__main__')"]
        else:
            cmd = [sys.executable, BIN]
        return cmd, e, {"marker": marker, "pgid": None, "pidfile": pidfile}

    def run(self, stdin, keyring="fail", shim=True, url=None, mode=None, env=None,
            timeout=30, executable=None):
        """Runs the hook once; returns (CompletedProcess, elapsed_s, info) where info has
        'marker' (an env marker every process of this run inherits) and 'pgid' (the
        worker's pid = pgid, when run through the shim). `executable` replaces the
        worker's interpreter instead of the shim (e.g. a path that doesn't exist)."""
        cmd, e, info = self._prepare(keyring, shim, url, mode, env, executable)
        start = time.monotonic()
        r = subprocess.run(cmd, input=stdin, capture_output=True, text=True, env=e,
                           timeout=timeout)
        elapsed = time.monotonic() - start
        info["pgid"] = read_pid(info["pidfile"])
        return r, elapsed, info

    def start(self, stdin, keyring="fail", url=None, mode=None, env=None, wait_s=5.0):
        """Starts the hook through the shim without waiting for it; returns
        (Popen of the supervisor, info) once the worker has written its pid."""
        cmd, e, info = self._prepare(keyring, True, url, mode, env)
        p = subprocess.Popen(cmd, stdin=subprocess.PIPE, stdout=subprocess.PIPE,
                             stderr=subprocess.PIPE, env=e)
        p.stdin.write(stdin.encode())
        p.stdin.close()
        p.stdin = None  # communicate() on Python <= 3.12 flushes a closed stdin and raises ValueError
        end = time.monotonic() + wait_s
        while info["pgid"] is None and time.monotonic() < end:
            info["pgid"] = read_pid(info["pidfile"])
            time.sleep(0.01)
        return p, info

    def log_lines(self):
        if not os.path.exists(self.log_path):
            return []
        with open(self.log_path, encoding="utf-8") as f:
            return [json.loads(line) for line in f if line.strip()]

    def write_transcript(self, name, calls):
        """calls: [(tool, command_or_None, is_error_or_None)]; None error = no result."""
        path = os.path.join(self.dir, name)
        with open(path, "w", encoding="utf-8") as f:
            for i, (tool, cmd, err) in enumerate(calls):
                inp = {"command": cmd} if cmd is not None else {"file_path": "x.py"}
                f.write(json.dumps({"message": {"content": [
                    {"type": "tool_use", "id": f"t{i}", "name": tool, "input": inp}]}}) + "\n")
                if err is not None:
                    f.write(json.dumps({"message": {"content": [
                        {"type": "tool_result", "tool_use_id": f"t{i}", "is_error": err}]}}) + "\n")
        return path


def read_pid(path):
    try:
        with open(path) as f:
            return int(f.read())
    except (OSError, ValueError):
        return None


def live_processes_with_marker(marker):
    """PIDs of live processes whose environment carries the marker (zombies have an
    empty environ, so they don't count)."""
    needle = f"TS_TEST_MARKER={marker}".encode()
    found = []
    for pid in os.listdir("/proc"):
        if not pid.isdigit():
            continue
        try:
            with open(f"/proc/{pid}/environ", "rb") as f:
                if needle in f.read().split(b"\0"):
                    found.append(int(pid))
        except OSError:
            pass
    return found


def group_is_gone(pgid, grace_s=1.0):
    """True once no process (a not-yet-reaped zombie included) is left in the group."""
    end = time.monotonic() + grace_s
    while True:
        try:
            os.killpg(pgid, 0)
        except ProcessLookupError:
            return True
        except PermissionError:
            return False
        if time.monotonic() >= end:
            return False
        time.sleep(0.02)


def leftovers(info, grace_s=1.0):
    """[] if nothing from this run is still alive, else a description."""
    out = []
    if info.get("pgid") is not None and not group_is_gone(info["pgid"], grace_s):
        out.append(f"process group {info['pgid']} still has members")
    end = time.monotonic() + grace_s
    while True:
        pids = live_processes_with_marker(info["marker"])
        if not pids or time.monotonic() >= end:
            break
        time.sleep(0.02)
    if pids:
        out.append(f"live processes with the run's marker: {pids}")
    return out
