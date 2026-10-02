#!/usr/bin/env python3
"""ts-done-check must never run past its deadline, never fail open silently when it
does, write at most one log line and print a block iff that line says block.

The hook is a supervisor process that runs the real work in a worker process (see
bin/ts-done-check). Most tests here run the real script as a subprocess through
tests/done_check_harness.py: no TYPESAFE_API_KEY, fake secret-tools only, log/HOME/
TMPDIR in a temp dir, and a worker shim that points ts_common.URL at a local fake
server (or at a port nothing listens on). The budgets are shrunk through the
TS_DONE_CHECK_TESTING=1-gated overrides (1.2 s deadline, 0.6 s keyring cap) so the
timeout paths run in about a second; tests/done-check-acceptance.py measures the same
paths with the production 8 s / 2 s budgets.
"""
import importlib.machinery
import importlib.util
import json
import os
import signal
import subprocess
import sys
import tempfile
import time
import unittest
from unittest import mock

HERE = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, os.path.join(HERE, ".."))
sys.path.insert(0, HERE)
import ts_common as tc  # noqa: E402
import done_check_harness as h  # noqa: E402

BIN = h.BIN


def _load_bin_module(name="ts_done_check_under_test"):
    loader = importlib.machinery.SourceFileLoader(name, BIN)
    spec = importlib.util.spec_from_loader(loader.name, loader)
    mod = importlib.util.module_from_spec(spec)
    loader.exec_module(mod)
    return mod


MOD = _load_bin_module()
LOG_VERSION = MOD.LOG_VERSION

# Built from parts so this test file's own diff carries no real-looking secret.
FAKE_SECRET = "api" + "_key = " + "sk-liveFAKE1234567890abcdefFAKE"
CLAIM_MSG = f"Fertig, erledigt. Interner Hinweis: {FAKE_SECRET}"
DEADLINE = 1.2
FAST_ENV = {"TS_DONE_CHECK_DEADLINE_S": str(DEADLINE), "TS_DONE_CHECK_KEYRING_CAP_S": "0.6"}


class _SandboxCase(unittest.TestCase):
    def setUp(self):
        self.sb = h.Sandbox()
        self.addCleanup(self.sb.cleanup)
        self.transcript = self.sb.write_transcript("transcript.jsonl", [("Edit", None, False)])

    def run_hook(self, msg=CLAIM_MSG, env=None, session_id="sess-test-1", **kw):
        e = dict(FAST_ENV)
        e.update(env or {})
        stdin = kw.pop("stdin", None)
        if stdin is None:
            stdin = self.sb.stdin(msg, session_id=session_id, transcript=self.transcript)
        return self.sb.run(stdin, env=e, **kw)

    def server(self, *a, **kw):
        srv = h.SlowServer(*a, **kw)
        self.addCleanup(srv.close)
        return srv

    def assert_one_error_line(self, r, elapsed, info, stage, cause, bound=DEADLINE + 0.5):
        """cause None: the line must have no cause field."""
        self.assertEqual(r.returncode, 0, r.stderr)
        self.assertEqual(r.stdout, "")
        self.assertEqual(r.stderr, "")
        self.assertLess(elapsed, bound, f"hook ran {elapsed:.2f}s")
        lines = self.sb.log_lines()
        self.assertEqual(len(lines), 1, lines)
        rec = lines[0]
        self.assertEqual((rec["result"], rec["fail"], rec["stage"]), ("error", "open", stage), rec)
        self.assertEqual(rec.get("cause", None), cause, rec)
        self.assertIsInstance(rec["elapsed_ms"], int)
        self.assertEqual(rec["log_version"], LOG_VERSION)
        self.assertEqual(h.leftovers(info), [])
        return rec


class TestTimeoutPaths(_SandboxCase):
    def test_hanging_keyring_bounded_logged_and_reaped(self):
        r, elapsed, info = self.run_hook(keyring="sleep:30")
        rec = self.assert_one_error_line(r, elapsed, info, "keyring", "timeout")
        self.assertEqual(rec["session_id"], "sess-test-1")
        self.assertEqual(rec["transcript_path"], self.transcript)
        self.assertNotIn("payload_masked", rec)  # no TypeSafe call was attempted

    def test_keyring_near_cap_then_hanging_post_share_one_deadline(self):
        srv = self.server(delay=None)
        r, elapsed, info = self.run_hook(keyring="slowkey:0.25", url=srv.url)
        rec = self.assert_one_error_line(r, elapsed, info, "post", "timeout")
        self.assertIn("payload_masked", rec)

    def test_post_server_that_never_answers(self):
        srv = self.server(delay=None)
        r, elapsed, info = self.run_hook(keyring="key", url=srv.url)
        rec = self.assert_one_error_line(r, elapsed, info, "post", "timeout")
        flat = json.dumps(rec["payload_masked"])
        self.assertNotIn("sk-liveFAKE1234567890abcdefFAKE", flat)
        self.assertEqual(len(srv.requests), 1)
        self.assertNotIn(b"sk-liveFAKE1234567890abcdefFAKE", srv.requests[0])

    def test_post_call_that_ignores_its_timeout(self):
        """tc.post itself blocks for 60 s (stands in for glibc DNS, which ignores
        urllib's timeout): only the supervisor can end it."""
        r, elapsed, info = self.run_hook(keyring="key", mode="hang_post")
        self.assert_one_error_line(r, elapsed, info, "post", "timeout")

    def test_trickling_post(self):
        srv = h.TrickleServer(interval=0.3, n_bytes=30)  # ~9 s uncut
        self.addCleanup(srv.close)
        r, elapsed, info = self.run_hook(keyring="key", url=srv.url)
        self.assertEqual(r.returncode, 0, r.stderr)
        self.assertLess(elapsed, DEADLINE + 0.5)
        lines = self.sb.log_lines()
        self.assertEqual(len(lines), 1, lines)
        self.assertEqual(lines[0]["result"], "error")
        self.assertIn(lines[0]["stage"], ("post", "parse"))
        self.assertEqual(lines[0]["cause"], "timeout")
        self.assertEqual(h.leftovers(info), [])

    def test_quadratic_masking_is_cut_off_as_startup(self):
        """The exact master RX_ADDRESS is quadratic on a long run of capitalised words
        without a house number (20k chars take ~5 s); the GIL-holding match is cut off
        by the supervisor's SIGKILL, not by anything inside the worker."""
        r, elapsed, info = self.run_hook(msg="Fertig. " + "Weg " * 5000, session_id="sess-regex")
        rec = self.assert_one_error_line(r, elapsed, info, "startup", "timeout")
        self.assertEqual(rec["session_id"], "sess-regex")

    def test_stdin_never_closed(self):
        """A host that never closes stdin: the supervisor's own read is bounded too."""
        e = dict(self.sb.base_env)
        e.update(FAST_ENV)
        r_fd, w_fd = os.pipe()  # the write end stays open in this process
        self.addCleanup(os.close, w_fd)
        os.write(w_fd, b'{"session_id": "sess-open", "last_assistant_message": "Fertig"')
        start = time.monotonic()
        q = subprocess.Popen([sys.executable, BIN], stdin=r_fd, stdout=subprocess.PIPE,
                             stderr=subprocess.PIPE, env=e)
        os.close(r_fd)
        out, err = q.communicate(timeout=10)
        elapsed = time.monotonic() - start
        self.assertEqual(q.returncode, 0, err)
        self.assertEqual(out, b"")
        self.assertLess(elapsed, DEADLINE + 0.5)
        lines = self.sb.log_lines()
        self.assertEqual(len(lines), 1, lines)
        self.assertEqual((lines[0]["result"], lines[0]["stage"], lines[0]["cause"]),
                         ("error", "startup", "timeout"))


class TestWorkerFailures(_SandboxCase):
    def test_worker_crash_logs_last_reported_stage_fast(self):
        r, elapsed, info = self.run_hook(keyring="key", mode="crash_at_keyring")
        self.assert_one_error_line(r, elapsed, info, "keyring", "crash", bound=DEADLINE - 0.3)

    def test_worker_printing_garbage_is_killed_and_logged(self):
        r, elapsed, info = self.run_hook(mode="garbage")
        self.assert_one_error_line(r, elapsed, info, "startup", "bad_record", bound=DEADLINE - 0.3)

    def test_malformed_final_record_is_a_bad_record(self):
        r, elapsed, info = self.run_hook(mode="bad_final")
        self.assert_one_error_line(r, elapsed, info, "startup", "bad_record", bound=DEADLINE - 0.3)

    def test_worker_exiting_without_final_record(self):
        r, elapsed, info = self.run_hook(mode="exit_silently")  # exit status 0, no final record
        self.assert_one_error_line(r, elapsed, info, "startup", "crash", bound=DEADLINE - 0.3)

    def test_supervisor_exception_is_internal(self):
        """Popen fails (the worker's interpreter doesn't exist): main()'s except."""
        missing = os.path.join(self.sb.dir, "no-such-python")
        r, elapsed, info = self.run_hook(shim=False, executable=missing)
        self.assert_one_error_line(r, elapsed, info, "startup", "internal", bound=DEADLINE - 0.3)

    def test_connection_refused_is_network(self):
        r, elapsed, info = self.run_hook(keyring="key")  # DEAD_URL: connection refused
        rec = self.assert_one_error_line(r, elapsed, info, "post", "network", bound=DEADLINE - 0.3)
        self.assertIn("payload_masked", rec)
        self.assertNotIn("http_status", rec)

    def test_final_record_is_accepted_before_the_worker_exits(self):
        r, elapsed, info = self.run_hook(mode="final_then_hang")
        self.assertEqual(r.returncode, 0, r.stderr)
        self.assertLess(elapsed, DEADLINE - 0.3)
        self.assertEqual(r.stdout, h.BLOCK_STDOUT)
        lines = self.sb.log_lines()
        self.assertEqual([l.get("decision") for l in lines], ["block"])
        self.assertEqual(h.leftovers(info), [])

    def test_stdout_and_log_agree_around_the_deadline(self):
        """The worker's final (block) record is held back until deadline +/- a few ms.
        Whichever side of the deadline it lands on, the one log line and stdout must
        agree. tests/done-check-acceptance.py runs 500 of these at the 8 s budget."""
        srv = self.server(delay=0, body=h.answer_body(0.95, 0.05))
        outcomes = []
        for i in range(24):
            delta_ms = (-6, -3, -1, 0, 3, 10, 40, 80)[i % 8]
            r, _, info = self.run_hook(keyring="key", url=srv.url, mode="final_at",
                                       env={"TS_TEST_FINAL_DELTA_MS": str(delta_ms)},
                                       session_id=f"sess-sweep-{i}")
            self.assertEqual(r.returncode, 0, r.stderr)
            lines = self.sb.log_lines()
            self.assertEqual(len(lines), 1, lines)
            printed = r.stdout == h.BLOCK_STDOUT
            self.assertIn(r.stdout, ("", h.BLOCK_STDOUT))
            self.assertEqual(printed, lines[0].get("decision") == "block", (r.stdout, lines))
            if not printed:
                self.assertEqual(lines[0]["result"], "error")
            outcomes.append(printed)
            os.remove(self.sb.log_path)
        self.assertIn(True, outcomes, "the sweep never landed before the deadline")
        self.assertIn(False, outcomes, "the sweep never landed after the deadline")


class TestDecisionsAndSilentPaths(_SandboxCase):
    def test_block_printed_byte_identical_and_logged(self):
        srv = self.server(delay=0, body=h.answer_body(0.95, 0.05))
        r, elapsed, info = self.run_hook(keyring="key", url=srv.url)
        self.assertEqual(r.returncode, 0, r.stderr)
        self.assertEqual(r.stdout, h.BLOCK_STDOUT)
        lines = self.sb.log_lines()
        self.assertEqual(len(lines), 1, lines)
        rec = lines[0]
        self.assertEqual(list(rec)[:4], ["ts", "log_version", "session_id", "transcript_path"])
        self.assertEqual((rec["claim"], rec["backed"], rec["decision"]), (0.95, 0.05, "block"))
        self.assertNotIn("result", rec)
        self.assertEqual(rec["log_version"], LOG_VERSION)
        self.assertEqual(rec["session_id"], "sess-test-1")
        self.assertEqual(rec["transcript_path"], self.transcript)
        self.assertNotIn("sk-liveFAKE1234567890abcdefFAKE", json.dumps(rec["payload_masked"]))
        self.assertEqual(h.leftovers(info), [])

    def test_pass_is_logged_and_not_printed(self):
        self.transcript = self.sb.write_transcript(
            "backed.jsonl", [("Edit", None, False), ("Bash", "pytest -q", False)])
        srv = self.server(delay=0, body=h.answer_body(0.95, 0.9))
        r, elapsed, info = self.run_hook(keyring="key", url=srv.url)
        self.assertEqual((r.returncode, r.stdout), (0, ""), r.stderr)
        self.assertEqual([l.get("decision") for l in self.sb.log_lines()], ["pass"])

    def test_malformed_answer_logged_as_parse_stage(self):
        srv = self.server(delay=0, body=b'{"answers": {"claim": {}, "backed": {}}}')
        r, elapsed, info = self.run_hook(keyring="key", url=srv.url)
        rec = self.assert_one_error_line(r, elapsed, info, "parse", "parse", bound=DEADLINE)
        self.assertIn("payload_masked", rec)

    def test_unreadable_transcript_logged_as_internal(self):
        bad = os.path.join(self.sb.dir, "bad.jsonl")
        with open(bad, "wb") as f:
            f.write(b"\xff\xfe\x00not valid utf-8 at all \x80\x81")
        stdin = self.sb.stdin(CLAIM_MSG, session_id="sess-bad", transcript=bad)
        r, elapsed, info = self.run_hook(stdin=stdin, shim=False)
        rec = self.assert_one_error_line(r, elapsed, info, "internal", "internal", bound=DEADLINE)
        self.assertEqual(rec["session_id"], "sess-bad")

    def test_silent_paths_write_no_line(self):
        cases = {
            "no claim": self.sb.stdin("Ich habe die Datei gelesen.", transcript=self.transcript),
            "stop_hook_active": self.sb.stdin(CLAIM_MSG, transcript=self.transcript,
                                              stop_hook_active=True),
            "no key configured": self.sb.stdin(CLAIM_MSG, transcript=self.transcript),
        }
        for name, stdin in cases.items():
            with self.subTest(name):
                r, elapsed, info = self.run_hook(stdin=stdin, shim=False)
                self.assertEqual((r.returncode, r.stdout), (0, ""), r.stderr)
                self.assertLess(elapsed, DEADLINE)
                self.assertEqual(self.sb.log_lines(), [])

    def test_old_format_lines_still_readable(self):
        """Lines from before log_version (master's {ts, claim, backed, decision}) and
        error lines without the newer fields still parse with .get() on known keys."""
        with open(self.sb.log_path, "w", encoding="utf-8") as f:
            f.write(json.dumps({"ts": "2026-09-30T00:00:00+00:00", "claim": 0.8, "backed": 0.2,
                                "decision": "block"}) + "\n")
            f.write(json.dumps({"ts": "2026-09-30T00:00:01+00:00", "verdict": "error",
                                "fail": "open", "stage": "post", "elapsed_ms": 9321}) + "\n")
        lines = self.sb.log_lines()
        self.assertEqual((lines[0]["decision"], lines[0]["claim"]), ("block", 0.8))
        self.assertIsNone(lines[0].get("log_version"))
        self.assertIsNone(lines[0].get("payload_masked"))
        self.assertEqual((lines[1]["stage"], lines[1]["elapsed_ms"]), ("post", 9321))

    def test_supervisor_does_not_import_ts_common_or_urllib(self):
        e = dict(self.sb.base_env)
        r = subprocess.run([sys.executable, "-X", "importtime", BIN],
                           input=self.sb.stdin(CLAIM_MSG, transcript=self.transcript),
                           capture_output=True, text=True, env=e, timeout=10)
        self.assertEqual(r.returncode, 0)
        imported = {line.rsplit("|", 1)[-1].strip() for line in r.stderr.splitlines()}
        self.assertIn("subprocess", imported)  # the importtime output really is the supervisor's
        self.assertEqual([m for m in imported
                          if m == "ts_common" or m.startswith(("urllib", "http", "ssl"))], [])


class TestPostCauses(_SandboxCase):
    """Every error line has a cause; a non-200 answer also carries its status, so
    a revoked key (401) is distinguishable from a server error (500)."""

    def post_error(self, status, body, cause, http_status=None, headers=()):
        srv = self.server(delay=0, status=status, body=body, headers=headers)
        r, elapsed, info = self.run_hook(keyring="key", url=srv.url)
        rec = self.assert_one_error_line(r, elapsed, info, "post", cause, bound=DEADLINE)
        self.assertEqual(rec.get("http_status"), http_status, rec)
        self.assertIn("payload_masked", rec)
        self.assertEqual(len(srv.requests), 1)
        return rec

    def test_http_401_revoked_key(self):
        self.post_error(401, b'{"error": "invalid key"}', "http_status", 401)

    def test_http_500(self):
        self.post_error(500, b"Internal Server Error", "http_status", 500)

    def test_http_302_is_not_followed(self):
        self.post_error(302, b"", "http_status", 302,
                        headers=(("Location", "http://127.0.0.1:1/elsewhere"),))

    def test_non_json_body_is_bad_response(self):
        self.post_error(200, b"<html>not json</html>", "bad_response")

    def test_answers_list_is_bad_response(self):
        self.post_error(200, b'{"answers": []}', "bad_response")

    def test_missing_noul_is_parse(self):
        srv = self.server(delay=0, body=b'{"answers": {"claim": {"noul": 0.9}, "backed": {}}}')
        r, elapsed, info = self.run_hook(keyring="key", url=srv.url)
        rec = self.assert_one_error_line(r, elapsed, info, "parse", "parse", bound=DEADLINE)
        self.assertNotIn("http_status", rec)

    def test_non_finite_noul_is_a_parse_failure_not_a_decision(self):
        """NaN, Infinity, -Infinity and an overflowing 1e400, on claim and on backed:
        before, claim Infinity/1e400 or backed -Infinity blocked and NaN passed."""
        bodies = []
        for value in ("NaN", "Infinity", "-Infinity", "1e400"):
            bodies.append((f"claim={value}", '{"answers": {"claim": {"noul": %s}, '
                           '"backed": {"noul": 0.05}}}' % value))
            bodies.append((f"backed={value}", '{"answers": {"claim": {"noul": 0.95}, '
                           '"backed": {"noul": %s}}}' % value))

        def reject(token):
            raise ValueError(f"non-finite token {token} in the log")
        for name, body in bodies:
            with self.subTest(name):
                srv = self.server(delay=0, body=body.encode())
                log = os.path.join(self.sb.dir, f"nonfinite-{name}.jsonl")
                r, elapsed, info = self.run_hook(keyring="key", url=srv.url,
                                                 env={"TS_DONE_CHECK_LOG": log})
                self.assertEqual((r.returncode, r.stdout, r.stderr), (0, "", ""))
                with open(log, encoding="utf-8") as f:
                    lines = [json.loads(l, parse_constant=reject) for l in f if l.strip()]
                self.assertEqual(len(lines), 1, lines)
                rec = lines[0]
                self.assertEqual((rec.get("result"), rec.get("stage"), rec.get("cause")),
                                 ("error", "parse", "parse"), rec)
                self.assertNotIn("decision", rec)
                self.assertIn("payload_masked", rec)
                self.assertEqual(h.leftovers(info), [])


class TestFiniteSerialization(unittest.TestCase):
    def test_finite_and_dumps(self):
        rec = {"a": float("nan"), "b": [float("inf"), -float("inf"), 1.5, 2], "c": {"d": 0.0},
               "e": "NaN", "f": True}
        self.assertEqual(MOD._finite(rec), {"a": None, "b": [None, None, 1.5, 2], "c": {"d": 0.0},
                                            "e": "NaN", "f": True})
        out = MOD._dumps(rec)
        self.assertNotIn("NaN,", out)
        self.assertNotIn("Infinity", out)

    def test_log_writer_and_worker_pipe_use_it(self):
        with tempfile.TemporaryDirectory(prefix="typesafe-nan-") as tmp:
            mod = _load_bin_module("ts_done_check_nan")
            mod.LOG = os.path.join(tmp, "log.jsonl")
            mod._append_log({"claim": float("nan"), "backed": float("inf")})
            with open(mod.LOG, encoding="utf-8") as f:
                self.assertEqual(f.read(), '{"claim": null, "backed": null}\n')
        with mock.patch.object(tc, "get_key", lambda timeout=5, die_with_parent=False: "k"), \
                mock.patch.object(tc, "post", lambda *a, **k: {"claim": {"noul": "nan"},
                                                               "backed": {"noul": "inf"}}):
            fin = MOD.check(json.dumps({"last_assistant_message": CLAIM_MSG}).encode())
        self.assertEqual((fin["log"]["stage"], fin["log"]["cause"]), ("parse", "parse"))
        self.assertNotIn("claim", fin["log"])


class TestSecretToolDiesWithTheWorker(_SandboxCase):
    """PR_SET_PDEATHSIG isn't inherited across fork, and a hung secret-tool uses no
    CPU: get_key(die_with_parent=True) gives secret-tool its own death signal."""

    def start_with_sleeping_keyring(self, env=None):
        p, info = self.sb.start(self.sb.stdin(CLAIM_MSG, transcript=self.transcript),
                                keyring="sleep:30", env=env)
        self.addCleanup(lambda: p.poll() is None and p.kill())
        self.addCleanup(p.stderr.close)
        self.addCleanup(p.stdout.close)
        self.assertIsNotNone(info["pgid"], "the worker never started")
        end = time.monotonic() + 5
        tool = None
        while tool is None and time.monotonic() < end:
            for pid in h.live_processes_with_marker(info["marker"]):
                try:
                    with open(f"/proc/{pid}/cmdline", "rb") as f:
                        if f.read().split(b"\0")[0].endswith(b"sleep"):
                            tool = pid
                except OSError:
                    pass
            time.sleep(0.02)
        self.assertIsNotNone(tool, "the fake secret-tool never started")
        return p, info, tool

    def test_secret_tool_gone_within_1s_of_a_supervisor_sigkill(self):
        p, info, tool = self.start_with_sleeping_keyring()  # production budgets: 2 s keyring cap
        os.kill(p.pid, signal.SIGKILL)
        p.wait(timeout=5)
        gone = h.group_is_gone(info["pgid"], 1.0)
        if not gone:  # the group still exists, so the id is still this run's: clean up now
            _killpg_quietly(info["pgid"])
        self.assertTrue(gone, f"worker group {info['pgid']} (secret-tool {tool}) survived > 1 s")
        self.assertEqual(h.leftovers(info, grace_s=0.2), [])

    def test_worker_is_single_threaded_while_secret_tool_runs(self):
        p, info, tool = self.start_with_sleeping_keyring(env=FAST_ENV)
        with open(f"/proc/{info['pgid']}/status") as f:
            threads = next(l for l in f if l.startswith("Threads:")).split()[1]
        p.communicate(timeout=10)
        self.assertEqual(threads, "1")


class TestOrphanBackstops(_SandboxCase):
    """The host may SIGKILL the supervisor before its deadline (e.g. a cancelled
    turn). The worker runs in its own session, so killpg by the host can't reach it;
    PR_SET_PDEATHSIG (set in the supervisor's spawn) makes the kernel kill it."""

    def kill_supervisor_mid_run(self, **kw):
        p, info = self.sb.start(self.sb.stdin(kw.pop("msg", CLAIM_MSG), transcript=self.transcript),
                                **kw)  # production 8 s deadline: only the kill can end it early
        self.addCleanup(lambda: p.poll() is None and p.kill())  # the supervisor, if a check failed
        self.addCleanup(p.stderr.close)
        self.addCleanup(p.stdout.close)
        self.assertIsNotNone(info["pgid"], "the worker never started")
        time.sleep(0.5)
        self.assertFalse(h.group_is_gone(info["pgid"], 0), "the worker ended on its own")
        os.kill(p.pid, signal.SIGKILL)
        p.wait(timeout=5)
        gone = h.group_is_gone(info["pgid"], 1.0)
        if not gone:  # the group still exists, so the id is still this run's: clean up now
            _killpg_quietly(info["pgid"])
        self.assertTrue(gone, f"worker group {info['pgid']} survived its supervisor by > 1 s")
        self.assertEqual(h.leftovers(info, grace_s=0.2), [])

    def test_worker_blocked_in_post_dies_with_its_supervisor(self):
        self.kill_supervisor_mid_run(keyring="key", mode="hang_post")

    def test_worker_in_a_gil_holding_regex_dies_with_its_supervisor(self):
        self.kill_supervisor_mid_run(msg="Fertig. " + "Weg " * 12500)

    def test_worker_has_a_cpu_time_limit(self):
        p, info = self.sb.start(self.sb.stdin(CLAIM_MSG, transcript=self.transcript),
                                keyring="key", mode="hang_post", env=FAST_ENV)
        self.assertIsNotNone(info["pgid"])
        with open(f"/proc/{info['pgid']}/limits") as f:
            cpu = next(l for l in f if l.startswith("Max cpu time")).split()
        p.communicate(timeout=10)
        self.assertEqual(cpu[3:5], [str(MOD.WORKER_CPU_LIMIT_S), str(MOD.WORKER_CPU_LIMIT_S + 1)])
        self.assertEqual(h.leftovers(info), [])


def _killpg_quietly(pgid):
    try:
        os.killpg(pgid, signal.SIGKILL)
    except OSError:
        pass


class TestCheckInProcess(unittest.TestCase):
    """The worker's logic as a plain function (no subprocess, mocked tc.get_key/post)."""

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory(prefix="typesafe-check-")
        self.addCleanup(self.tmp.cleanup)

    def check(self, payload, post_answer=None, key="dummy-test-value"):
        sent = []

        def fake_post(state, questions, timeout=None, key=None, detail=None):
            sent.append((state, timeout, key))
            if post_answer is None:
                detail.update(reason="http_status", status=401)
            return post_answer

        emitted = []
        with mock.patch.object(tc, "get_key", lambda timeout=5, die_with_parent=False: key), \
                mock.patch.object(tc, "post", fake_post):
            fin = MOD.check(json.dumps(payload).encode(), emitted.append)
        return fin, emitted, sent

    def test_block_record_and_progress(self):
        fin, emitted, sent = self.check({"session_id": "s", "last_assistant_message": CLAIM_MSG},
                                        {"claim": {"noul": 0.9}, "backed": {"noul": 0.1}})
        self.assertEqual([e["stage"] for e in emitted], ["keyring", "post", "parse"])
        self.assertIn("payload_masked", emitted[1])
        self.assertEqual(fin["final"], True)
        self.assertEqual(fin["session_id"], "s")
        self.assertEqual(fin["log"]["decision"], "block")
        self.assertEqual(sent[0][2], "dummy-test-value")
        self.assertLess(sent[0][1], MOD.DEADLINE_S)

    def test_silent_and_error_records(self):
        fin, _, _ = self.check({"last_assistant_message": "nur gelesen"})
        self.assertIsNone(fin["log"])
        fin, _, _ = self.check({"last_assistant_message": CLAIM_MSG}, key=None)
        self.assertIsNone(fin["log"])  # no key configured: silent
        fin, _, _ = self.check({"last_assistant_message": CLAIM_MSG}, post_answer=None)
        self.assertEqual((fin["log"]["stage"], "payload_masked" in fin["log"]), ("post", True))
        self.assertEqual((fin["log"]["cause"], fin["log"]["http_status"]), ("http_status", 401))
        fin = MOD.check(b"not json")
        self.assertEqual((fin["log"]["stage"], fin["log"]["cause"]), ("internal", "internal"))
        fin = MOD.check(json.dumps({"last_assistant_message": CLAIM_MSG}).encode(),
                        start=time.monotonic() - 10, deadline=time.monotonic() - 1)
        self.assertEqual((fin["log"]["stage"], fin["log"]["cause"]), ("keyring", "timeout"))

    def test_tool_calls_redacts_only_the_kept_calls_and_keeps_key_order(self):
        transcript = os.path.join(self.tmp.name, "mixed.jsonl")
        secrets = [f"api_key=realsecret{i:04d}0000000000000000" for i in range(50)]
        with open(transcript, "w", encoding="utf-8") as f:
            for i, secret in enumerate(secrets):
                f.write(json.dumps({"message": {"content": [
                    {"type": "tool_use", "id": f"t{i}", "name": "Bash",
                     "input": {"command": f"echo {secret}"}}]}}) + "\n")
                f.write(json.dumps({"message": {"content": [
                    {"type": "tool_use", "id": f"e{i}", "name": "Edit", "input": {}}]}}) + "\n")
        redacted = []
        orig = MOD.redact
        with mock.patch.object(MOD, "redact", lambda t: redacted.append(t) or orig(t)):
            calls = MOD.tool_calls(transcript)
        self.assertLessEqual(len(redacted), MOD.MAX_TOOLS)
        self.assertEqual(len(calls), MOD.MAX_TOOLS)
        for c in calls:
            self.assertEqual(list(c), ["tool", "cmd", "error"] if c["tool"] == "Bash"
                             else ["tool", "error"])
            if c["tool"] == "Bash":
                self.assertNotIn("realsecret", c["cmd"])
                self.assertIn("<redacted>", c["cmd"])


class TestEnvOverrides(unittest.TestCase):
    def test_env_float(self):
        f = MOD._env_float
        with mock.patch.dict(os.environ, {"X": "nan"}):
            self.assertEqual(f("X", 8.0, floor=1.0), 8.0)
        for raw, want in (("", 8.0), ("abc", 8.0), ("-1", 8.0), ("0", 8.0), ("0.2", 1.0),
                          ("3", 3.0), ("30", 8.0), ("inf", 8.0)):
            with mock.patch.dict(os.environ, {"X": raw}):
                self.assertEqual(f("X", 8.0, floor=1.0), want, raw)

    def test_overrides_need_testing_flag(self):
        env = {"TS_DONE_CHECK_DEADLINE_S": "1.5", "TS_DONE_CHECK_KEYRING_CAP_S": "0.7",
               "TS_DONE_CHECK_LOG": os.path.join(tempfile.gettempdir(), "never-used.jsonl")}
        with mock.patch.dict(os.environ, env):
            os.environ.pop("TS_DONE_CHECK_TESTING", None)
            prod = _load_bin_module("ts_done_check_prod_consts")  # constants only, writes nothing
            os.environ["TS_DONE_CHECK_TESTING"] = "1"
            test = _load_bin_module("ts_done_check_test_consts")
        self.assertEqual((prod.DEADLINE_S, prod.KEYRING_CAP_S), (8.0, 2.0))
        self.assertTrue(prod.LOG.endswith("/.local/state/typesafe-dev/done-check.jsonl"))
        self.assertEqual((test.DEADLINE_S, test.KEYRING_CAP_S, test.LOG),
                         (1.5, 0.7, env["TS_DONE_CHECK_LOG"]))


class TestLogAtRest(unittest.TestCase):
    """The supervisor's log writer: 0600, tightening, one-generation rotation, a lock
    for concurrent writers, a bounded wait for a stuck lock holder, never raising."""

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory(prefix="typesafe-logrest-")
        self.addCleanup(self.tmp.cleanup)
        self.mod = _load_bin_module("ts_done_check_logrest")
        self.log_path = os.path.join(self.tmp.name, "done-check.jsonl")
        self.mod.LOG = self.log_path

    def lines(self, path=None):
        with open(path or self.log_path, encoding="utf-8") as f:
            return [json.loads(l) for l in f if l.strip()]

    def test_fresh_file_created_at_0600(self):
        self.mod._append_log({"x": 1})
        self.assertEqual(os.stat(self.log_path).st_mode & 0o777, 0o600)

    def test_existing_looser_file_is_tightened_on_write(self):
        with open(self.log_path, "w") as f:
            f.write('{"old": true}\n')
        os.chmod(self.log_path, 0o644)
        self.mod._append_log({"x": 1})
        self.assertEqual(os.stat(self.log_path).st_mode & 0o777, 0o600)
        self.assertEqual(len(self.lines()), 2)

    def test_rotation_past_max_bytes_keeps_one_generation(self):
        with open(self.log_path, "wb") as f:
            f.write(b"x" * (self.mod.LOG_MAX_BYTES + 1))
        self.mod._append_log({"marker": "after-rotation"})
        self.assertEqual(os.path.getsize(self.log_path + ".1"), self.mod.LOG_MAX_BYTES + 1)
        self.assertEqual(self.lines(), [{"marker": "after-rotation"}])

    def test_non_ascii_is_escaped(self):
        self.mod._append_log({"m": "grün ✅"})
        with open(self.log_path, "rb") as f:
            raw = f.read()
        self.assertTrue(raw.isascii())
        self.assertEqual(self.lines(), [{"m": "grün ✅"}])

    def test_concurrent_writers_lose_no_lines(self):
        """24 processes race one rotation; os.path.getsize sleeps to widen the
        check-then-rename window (without the lock this lost lines in 25/25 trials)."""
        n = 24
        worker = os.path.join(self.tmp.name, "writer.py")
        with open(worker, "w") as f:
            f.write(
                "import sys, time, os as _os, importlib.machinery, importlib.util\n"
                f"loader = importlib.machinery.SourceFileLoader('m', {BIN!r})\n"
                "spec = importlib.util.spec_from_loader(loader.name, loader)\n"
                "mod = importlib.util.module_from_spec(spec)\n"
                "loader.exec_module(mod)\n"
                "_orig = _os.path.getsize\n"
                "def _slow(p):\n"
                "    r = _orig(p)\n"
                "    time.sleep(0.05)\n"
                "    return r\n"
                "mod.os.path.getsize = _slow\n"
                "mod.LOG = sys.argv[2]\n"
                "mod.LOG_MAX_BYTES = 1500\n"
                "mod._append_log({'pid': sys.argv[1]}, 5.0)\n")
        with open(self.log_path, "wb") as f:
            f.write(b"x" * 1600)
        env = {k: v for k, v in os.environ.items() if k != "TYPESAFE_API_KEY"}
        env.update(TS_DONE_CHECK_TESTING="1", TS_DONE_CHECK_LOG=self.log_path)
        procs = [subprocess.Popen([sys.executable, worker, str(p), self.log_path], env=env)
                 for p in range(n)]
        for p in procs:
            self.assertEqual(p.wait(timeout=30), 0)
        self.assertEqual(sorted(int(l["pid"]) for l in self.lines()), list(range(n)))

    def test_lock_wait_is_bounded_when_the_holder_is_stopped(self):
        holder_script = os.path.join(self.tmp.name, "lock_holder.py")
        with open(holder_script, "w") as f:
            f.write("import fcntl, os, sys\n"
                    "fd = os.open(sys.argv[1], os.O_CREAT | os.O_RDWR, 0o600)\n"
                    "fcntl.flock(fd, fcntl.LOCK_EX)\n"
                    "sys.stdout.write('LOCKED\\n'); sys.stdout.flush()\n"
                    "os.kill(os.getpid(), 19)\n")  # SIGSTOP while holding the lock
        holder = subprocess.Popen([sys.executable, holder_script, self.log_path + ".lock"],
                                  stdout=subprocess.PIPE, text=True)

        def cleanup():
            if holder.poll() is None:
                os.kill(holder.pid, signal.SIGCONT)
            holder.wait(timeout=5)
            holder.stdout.close()
        self.addCleanup(cleanup)
        self.assertEqual(holder.stdout.readline().strip(), "LOCKED")
        _wait_stopped(self, holder.pid)

        for budget in (0.0, 0.3):
            start = time.monotonic()
            self.mod._append_log({"probe": budget}, budget)
            elapsed = time.monotonic() - start
            self.assertGreaterEqual(elapsed + 0.01, budget)
            self.assertLess(elapsed, budget + 0.2)
        self.assertEqual([l["probe"] for l in self.lines()], [0.0, 0.3])

    def test_append_log_never_raises_even_if_log_path_is_unwritable(self):
        blocker = os.path.join(self.tmp.name, "blocker")
        open(blocker, "w").close()
        self.mod.LOG = os.path.join(blocker, "sub", "done-check.jsonl")
        self.mod._append_log({"x": 1})


def _wait_stopped(test, pid):
    end = time.monotonic() + 3.0
    state = None
    while time.monotonic() < end:
        try:
            with open(f"/proc/{pid}/status") as f:
                state = next(l for l in f if l.startswith("State:")).split()[1]
        except (OSError, StopIteration):
            pass
        if state == "T":
            return
        time.sleep(0.02)
    test.fail(f"process {pid} never stopped: {state!r}")


class TestStoppedLockHolderEndToEnd(_SandboxCase):
    def test_one_line_and_bounded_extra_cost(self):
        holder = subprocess.Popen(
            [sys.executable, "-c",
             "import fcntl, os, sys\n"
             "fd = os.open(sys.argv[1], os.O_CREAT | os.O_RDWR, 0o600)\n"
             "fcntl.flock(fd, fcntl.LOCK_EX)\n"
             "sys.stdout.write('LOCKED\\n'); sys.stdout.flush()\n"
             "os.kill(os.getpid(), 19)\n", self.sb.log_path + ".lock"],
            stdout=subprocess.PIPE, text=True)

        def cleanup():
            if holder.poll() is None:
                os.kill(holder.pid, signal.SIGCONT)
            holder.wait(timeout=5)
            holder.stdout.close()
        self.addCleanup(cleanup)
        self.assertEqual(holder.stdout.readline().strip(), "LOCKED")
        _wait_stopped(self, holder.pid)
        srv = self.server(delay=0, body=h.answer_body(0.95, 0.05))
        r, elapsed, info = self.run_hook(keyring="key", url=srv.url)
        self.assertEqual(r.returncode, 0, r.stderr)
        self.assertEqual(r.stdout, h.BLOCK_STDOUT)
        self.assertLess(elapsed, DEADLINE + 0.3)
        self.assertLess(elapsed, MOD.LOCK_WAIT_MAX_S + 0.8)
        self.assertEqual([l.get("decision") for l in self.sb.log_lines()], ["block"])


class TestWorstCaseLineSize(unittest.TestCase):
    def test_worst_case_line_size_with_real_serializer_and_non_ascii(self):
        msg = "🎉" * MOD.MAX_MSG
        calls = [{"tool": "Bash", "cmd": "🎉" * 200, "error": False} for _ in range(MOD.MAX_TOOLS)]
        masked = tc._mask_tree({"assistant_message": msg, "tool_calls": calls})
        rec = {"ts": "2026-10-02T12:00:00+00:00", "log_version": LOG_VERSION,
               "session_id": "sess-" + "a" * 40, "transcript_path": "/home/u/.claude/p/t.jsonl",
               "claim": 0.95, "backed": 0.1, "decision": "block", "payload_masked": masked}
        size = len(json.dumps(rec).encode("utf-8"))
        self.assertLess(size, 200_000)
        self.assertLess(size, MOD.WORKER_LINE_MAX)  # the worker's final record fits its line cap


if __name__ == "__main__":
    unittest.main()
