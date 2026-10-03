#!/usr/bin/env python3
"""Tests for typesafe-dev helper functions and CLI tools."""
import json
import os
import subprocess
import sys
import unittest

sys.path.insert(0, os.path.dirname(os.path.realpath(__file__)) + "/..")
import ts_common as tc  # noqa: E402


class TestTsCommon(unittest.TestCase):
    def test_mask_detail_secrets(self):
        # built from parts so this file's own diff carries no real-looking secret
        text = "Hier ist to" + "ken = abcdef1234567890abcdef123456 und pass" + "word = secret123"
        masked, hits = tc.mask_detail(text)
        self.assertNotIn("secret123", masked)
        self.assertGreaterEqual(hits["secret_kw"], 1)

    def test_mask_detail_emails(self):
        text = "Kontakt: max.mustermann@mail.example für Support"
        masked, hits = tc.mask_detail(text)
        self.assertNotIn("max.mustermann@mail.example", masked)
        self.assertEqual(hits["email"], 1)

    def test_mask_detail_address(self):
        text = "Post an Erika in der Hauptstraße 12 und Wohnort: 80331 München"
        masked, hits = tc.mask_detail(text)
        self.assertNotIn("Hauptstraße 12", masked)
        self.assertNotIn("80331 München", masked)
        self.assertEqual(hits["address"], 2)

    def test_mask_detail_names(self):
        text = "Treffen mit Max Mustermann in Berlin"
        masked, hits = tc.mask_detail(text)
        self.assertNotIn("Max", masked)
        self.assertNotIn("Mustermann", masked)
        self.assertEqual(hits["name"], 1)

    def test_mask_detail_variants(self):
        for addr in ["Musterstraße 12", "Hauptstr. 4b", "Am Markt 1", "Bahnhofstraße 99 a"]:
            masked, hits = tc.mask_detail(f"Adresse: {addr}")
            self.assertNotIn(addr, masked, f"Failed to mask {addr}")
            self.assertEqual(hits["address"], 1)


    def test_choice_helper(self):
        fake_answers = {
            "test_q": {
                "choice": "opt_b",
                "confidence": 0.85,
                "probabilities": {"opt_a": 0.15, "opt_b": 0.85}
            }
        }
        res = tc.choice(fake_answers, "test_q")
        self.assertIsNotNone(res)
        choice_str, conf, probs = res
        self.assertEqual(choice_str, "opt_b")
        self.assertEqual(conf, 0.85)
        self.assertEqual(probs["opt_b"], 0.85)


class TestOpaqueTokenLinear(unittest.TestCase):
    """tc.RX_OPAQUE used to be a plain compiled regex with two lookaheads
    ((?=...*\\d)(?=...*[A-Za-z])) that re's backtracking engine re-tried from
    every position in a long run of the charset with no digit (or no letter)
    reachable -- confirmed quadratic: 40k such characters took ~5.8s, scaling
    ~4x when the input doubled. It is now a small matcher class with the same
    .subn(repl, text) interface (the only method mask_detail() calls on it),
    finding the same matches in linear time. This differential-tests it
    against the ORIGINAL regex (kept here only as a reference for this test,
    not used anywhere else) over a large random corpus, and re-measures the
    pathological case."""

    ORIGINAL = __import__("re").compile(
        r"(?=[A-Za-z0-9_\-+/]*\d)(?=[A-Za-z0-9_\-+/]*[A-Za-z])[A-Za-z0-9_\-+/]{24,}={0,2}")

    def test_differential_against_original_regex(self):
        import random
        # Includes Unicode digits/letters/scripts adjacent to runs on purpose:
        # \d in the ORIGINAL's lookahead is Unicode, and its greedy [charset]*
        # always consumes a run WHOLE before checking \d, so a run with no
        # ASCII digit of its own can still qualify via a Unicode decimal digit
        # (outside the charset, never actually part of the match) immediately
        # after it -- found during review, 2026-10-02: "a"*24 + "３"
        # (fullwidth 3) is masked by the original and was NOT by an earlier,
        # incomplete version of this fix. [A-Za-z] has no such case (it's an
        # explicit ASCII range, a strict subset of the run's own charset, so
        # it can never be satisfied from outside the run -- also covered here).
        alphabet = (list("ABCDEFabcdef0123456789_-+/=.,: @<>\"'()[]{}") + [" ", "\n", "\t"]
                    + ["３", "４", "５", "Ａ", "Ｂ", "ä", "ö", "一", "٣", "۴"])
        rng = random.Random(1234)
        mismatches = []
        for trial in range(20000):
            n = rng.choice([0, 1, 5, 10, 20, 23, 24, 25, 30, 40, 50, 80, rng.randint(0, 150)])
            text = "".join(rng.choice(alphabet) for _ in range(n))
            want = self.ORIGINAL.subn("<redacted>", text)
            got = tc.RX_OPAQUE.subn("<redacted>", text)
            if want != got:
                mismatches.append((text, want, got))
        self.assertEqual(mismatches, [], f"{len(mismatches)} mismatches, first: {mismatches[:1]}")

    def test_pathological_case_is_no_longer_quadratic(self):
        import time
        # The original blow-up case: a long run with no digit anywhere ahead.
        text = "x" * 20000
        self.assertEqual(self.ORIGINAL.subn("<redacted>", text), tc.RX_OPAQUE.subn("<redacted>", text))
        t0 = time.monotonic()
        tc.RX_OPAQUE.subn("<redacted>", "x" * 200000)  # 10x longer than the differential check above
        elapsed = time.monotonic() - t0
        self.assertLess(elapsed, 0.5, f"RX_OPAQUE took {elapsed:.2f}s on 200k chars -- not linear")


class TestEmailLinear(unittest.TestCase):
    """tc.RX_EMAIL used to be a plain compiled regex ([\\w.+-]+@[\\w-]+\\.[\\w.-]+)
    with the same class of quadratic behavior as RX_OPAQUE: re retries the
    greedy local-part scan from every position in a long run of the charset
    with no '@' reachable from there. Confirmed empirically (2026-10-02): 50k
    such characters took 5.5s (letters) to 10.7s (hex) to mask -- inside
    redact(), which ts-done-check's deadline backstop must be able to cut off,
    but a GIL-holding C-level regex match can't be preempted by a watchdog
    thread (also confirmed separately: a watchdog woken after 1s could not run
    its own Python code, including os._exit(), until an 11s regex match on the
    main thread finally released the GIL). It is now a small matcher class
    supporting every way this module uses RX_EMAIL -- .subn(str, text),
    .sub(callable, text), .search(text) -- finding the same matches in linear
    time. Differentially tested against the ORIGINAL regex (kept here only as
    a reference, not used anywhere else) over a large random corpus including
    Unicode, plus realistic email-shaped strings and the pathological case."""

    ORIGINAL = __import__("re").compile(r"[\w.+-]+@[\w-]+\.[\w.-]+")

    ALPHABET = (list("ABCDEFabcdef0123456789_-+/=.,: @<>()[]{}") + [" ", "\n", "\t"]
                + ["３", "４", "Ａ", "Ｂ", "ä", "ö", "一", "٣", "۴"])

    def test_differential_subn_against_original_regex(self):
        import random
        rng = random.Random(42)
        mismatches = []
        for _ in range(20000):
            n = rng.choice([0, 1, 3, 5, 10, 15, 20, rng.randint(0, 80)])
            text = "".join(rng.choice(self.ALPHABET) for _ in range(n))
            want = self.ORIGINAL.subn("<email>", text)
            got = tc.RX_EMAIL.subn("<email>", text)
            if want != got:
                mismatches.append((text, want, got))
        self.assertEqual(mismatches, [], f"{len(mismatches)} mismatches, first: {mismatches[:3]}")

    def test_differential_sub_callable_and_search(self):
        import random

        def repl_func(m):
            g = m.group(0)
            return g.upper() if len(g) % 2 == 0 else ""

        rng = random.Random(7)
        sub_mismatches, search_mismatches = [], []
        for _ in range(10000):
            n = rng.choice([0, 1, 3, 5, 10, 15, 20, rng.randint(0, 60)])
            text = "".join(rng.choice(self.ALPHABET) for _ in range(n))
            want_sub, got_sub = self.ORIGINAL.sub(repl_func, text), tc.RX_EMAIL.sub(repl_func, text)
            if want_sub != got_sub:
                sub_mismatches.append((text, want_sub, got_sub))
            want_m, got_m = self.ORIGINAL.search(text), tc.RX_EMAIL.search(text)
            want_g = want_m.group(0) if want_m else None
            got_g = got_m.group(0) if got_m else None
            if want_g != got_g:
                search_mismatches.append((text, want_g, got_g))
        self.assertEqual(sub_mismatches, [], f"sub: {len(sub_mismatches)} mismatches")
        self.assertEqual(search_mismatches, [], f"search: {len(search_mismatches)} mismatches")

    def test_realistic_email_shapes(self):
        AT = "@"  # put together at runtime, as in test_no_leak.py
        cases = [f"a{AT}b.example", f"x.y+z{AT}sub.domain.example", f"weird-case{AT}a--b.c.d", "no-at-here.example",
                 f"{AT}no-local.example", f"no-domain{AT}", f"a{AT}b", f"a{AT}.example", f"a{AT}b.", f"ü{AT}b.test",
                 f"a{AT}b.c", f"two{AT}emails.example and another{AT}one.test here", f"trailing.{AT}dot.invalid.",
                 f"a{AT}b.c{AT}d.e"]
        for text in cases:
            with self.subTest(text=text):
                self.assertEqual(self.ORIGINAL.subn("<email>", text), tc.RX_EMAIL.subn("<email>", text))

    def test_pathological_case_is_no_longer_quadratic(self):
        import time
        text = "x" * 20000  # letters only, no digit/'@' -- the original blow-up shape
        self.assertEqual(self.ORIGINAL.subn("<email>", text), tc.RX_EMAIL.subn("<email>", text))
        t0 = time.monotonic()
        tc.RX_EMAIL.subn("<email>", "0123456789abcdef" * 31250)  # 500k hex chars
        elapsed = time.monotonic() - t0
        self.assertLess(elapsed, 0.5, f"RX_EMAIL took {elapsed:.2f}s on 500k chars -- not linear")


class TestAddressKeyValExactMaster(unittest.TestCase):
    """RX_ADDRESS and RX_KEY_VAL are master's plain regexes again (2026-10-02). The
    windowed rewrites on the done-check-timeout branch bounded each match attempt to a
    fixed window (100/60 chars around an address anchor, 150 chars from a keyword to
    its '='/':') and so left the parts beyond the window unmasked. Exact masking is
    slower on adversarial input (quadratic), which ts-done-check now bounds by time in
    its supervisor process. Each shape below leaked under the windowed version."""

    _STREET_SUFFIXES = (r"(?:stra[ßs]e|str\b\.?|weg|gasse|platz|allee|ring|damm|ufer|chaussee"
                        r"|zeile|stieg|gässchen|pfad|markt)")
    _PAT_STREET = (
        r"\b(?:"
        r"(?:[A-ZÄÖÜ][a-zäöüß]+(?:\s+|-))*"
        r"(?:[A-ZÄÖÜ][a-zäöüß]+)?(?i:" + _STREET_SUFFIXES + r")"
        r"|"
        r"[a-zäöüß]+(?i:" + _STREET_SUFFIXES + r")"
        r")"
        r"\s+\d+(?:\s*[a-zA-Z])?(?:\s*[-/]\s*\d{1,4}(?:\s*[a-zA-Z])?)?\b"
    )
    _PAT_PLZ = (
        r"\b\d{5}\s+[A-ZÄÖÜ][a-zäöüß]+(?:[-/][A-ZÄÖÜ][a-zäöüß]+)*"
        r"(?:\s+(?:(?:am|an\s+der|im)\s+)?[A-ZÄÖÜ][a-zäöüß]+)?\b"
    )
    _MASK_KW = (r"(?:api[_-]?key|token|secret|passw(?:or)?d|pass(?:phrase|wort)?|pwd|credential"
                r"|private[_-]?key|access[_-]?key|auth(?:orization)?)")
    MASTER_ADDRESS = rf"{_PAT_STREET}|{_PAT_PLZ}"
    MASTER_KEY_VAL = (
        r"(?i)(" + _MASK_KW + r"[\w.-]*[\"']?\s*[=:]\s*)"
        r"(?:(?P<dq>\")(?!<redacted>\")[^\"\n]+\"|(?P<sq>')(?!<redacted>')[^'\n]+'|[\"']?(?!<redacted>)[^\s\"',;]+)"
    )

    def test_patterns_are_the_master_regexes(self):
        self.assertEqual(tc.RX_ADDRESS.pattern, self.MASTER_ADDRESS)
        self.assertEqual(tc.RX_KEY_VAL.pattern, self.MASTER_KEY_VAL)

    def test_shapes_that_leaked_past_a_fixed_window_are_masked(self):
        value = "wachWert" + "Delta4711"  # built from parts: no real-looking secret in the diff
        chain = "-".join(["Wachtelberg", "Oberhausener", "Unterbacher", "Mittelfelder", "Kaiserin",
                          "Friedrichs", "Wilhelminen", "Viktorias", "Luisen", "Augusta"])
        city = "-".join(["Wachtstadt", "Oberdorf", "Unterdorf", "Mitteldorf", "Hinterdorf",
                         "Vorderdorf", "Wachtneudorf"])
        self.assertGreater(len(chain), 100)  # words before the suffix, past a 100-char lookback
        self.assertGreater(len("80331 " + city), 60)
        cases = [
            ("pass" + "word:" + " " * 200 + value, [value]),
            ("tok" + "en" + "a1b2c3d4" * 20 + "=" + value, [value]),  # 160 identifier chars
            ("Wohnt in der " + chain + "straße 12 im Hinterhaus", ["Wachtelberg", "Augusta"]),
            ("Wohnort: 80331 " + city + ", Deutschland", ["Wachtstadt", "Wachtneudorf"]),
        ]
        for text, fragments in cases:
            masked = tc.mask(text)[0]
            for frag in fragments:
                with self.subTest(text=text[:40], fragment=frag):
                    self.assertNotIn(frag, masked)


class TestPostFailureDetail(unittest.TestCase):
    """tc.post(detail={}) says why it returned None; the return value is unchanged
    (still None), so the other CLIs, which don't pass detail, behave as before."""

    def reason(self, exc):
        from unittest import mock
        detail = {}
        with mock.patch.object(tc._OPENER, "open", side_effect=exc):
            self.assertIsNone(tc.post({"x": 1}, {}, timeout=1, key="dummy-test-key", detail=detail))
            self.assertIsNone(tc.post({"x": 1}, {}, timeout=1, key="dummy-test-key"))
        return detail

    def test_exceptions_map_to_reasons(self):
        import http.client
        import io
        import socket
        import ssl
        import urllib.error
        http_err = urllib.error.HTTPError("http://127.0.0.1:1/", 401, "Unauthorized", {}, io.BytesIO(b""))
        cases = [
            (http_err, {"reason": "http_status", "status": 401}),
            (urllib.error.URLError(ConnectionRefusedError(111, "refused")), {"reason": "network"}),
            (urllib.error.URLError(socket.gaierror(-2, "Name or service not known")), {"reason": "network"}),
            (urllib.error.URLError(ssl.SSLError(1, "CERTIFICATE_VERIFY_FAILED")), {"reason": "network"}),
            (ConnectionResetError(104, "reset"), {"reason": "network"}),
            (http.client.IncompleteRead(b"x", 10), {"reason": "network"}),
            (urllib.error.URLError(TimeoutError("timed out")), {"reason": "timeout"}),
            (TimeoutError("timed out"), {"reason": "timeout"}),
            (RuntimeError("unexpected"), {"reason": "internal"}),
        ]
        for exc, want in cases:
            with self.subTest(exc=repr(exc)):
                self.assertEqual(self.reason(exc), want)

    def test_no_key(self):
        from unittest import mock
        detail = {}
        with mock.patch.object(tc, "get_key", lambda *a, **k: None):
            self.assertIsNone(tc.post({"x": 1}, {}, detail=detail))
        self.assertEqual(detail, {"reason": "no_key"})


class TestGetKeyDieWithParent(unittest.TestCase):
    """get_key(die_with_parent=True) starts secret-tool with a PR_SET_PDEATHSIG
    preexec_fn, but only while the caller has one thread; the default is unchanged."""

    def kwargs_used(self, threads, **get_key_kw):
        from unittest import mock
        seen = {}

        def fake_run(cmd, **kw):
            seen.update(kw)
            return subprocess.CompletedProcess(cmd, 0, "dummy-test-key\n", "")
        env = {k: v for k, v in os.environ.items() if k != "TYPESAFE_API_KEY"}
        with mock.patch.dict(os.environ, env, clear=True), \
                mock.patch.object(tc.subprocess, "run", fake_run), \
                mock.patch("threading.active_count", lambda: threads):
            self.assertEqual(tc.get_key(timeout=1, **get_key_kw), "dummy-test-key")
        return seen

    def test_preexec_only_when_asked_and_single_threaded(self):
        self.assertNotIn("preexec_fn", self.kwargs_used(1))
        self.assertIn("preexec_fn", self.kwargs_used(1, die_with_parent=True))
        self.assertNotIn("preexec_fn", self.kwargs_used(2, die_with_parent=True))

    def test_preexec_sets_the_death_signal_in_a_real_child(self):
        preexec = tc._die_with_parent_preexec()
        self.assertIsNotNone(preexec)
        r = subprocess.run([sys.executable, "-c",
                            "import ctypes; s = ctypes.c_int(); "
                            "ctypes.CDLL(None).prctl(2, ctypes.byref(s), 0, 0, 0); print(s.value)"],
                           preexec_fn=preexec, capture_output=True, text=True, timeout=10)
        self.assertEqual(r.stdout.strip(), "9")  # PR_GET_PDEATHSIG: SIGKILL


class TestCliTools(unittest.TestCase):
    def setUp(self):
        self.bin_dir = os.path.join(os.path.dirname(os.path.realpath(__file__)), "..", "bin")

    def test_agent_dispatch_cli(self):
        # The expected status follows from the key: "ok" when a key is configured
        # (env or secret-tool), "fail_open" without one (CI, offline runs). A key
        # with an unreachable API is a real failure, not an accepted outcome.
        expected = "ok" if tc.get_key() else "fail_open"
        cmd = [
            os.path.join(self.bin_dir, "ts-agent-dispatch"),
            "--json",
            "DELEGATED BY: lead ROLE: advisor, read-only. Führe Lese-Review durch."
        ]
        r = subprocess.run(cmd, capture_output=True, text=True, timeout=15)
        self.assertEqual(r.returncode, 0, f"Stderr: {r.stderr}")
        payload = json.loads(r.stdout)
        self.assertEqual(payload.get("status"), expected)
        self.assertIn("masked_detail", payload)
        if expected == "ok":
            self.assertEqual(payload["model_tier"], "flash")
            self.assertEqual(payload["workspace_mode"], "inherit")

    def test_decision_check_cli(self):
        # See test_agent_dispatch_cli for how the expected status is derived.
        expected = "ok" if tc.get_key() else "fail_open"
        cmd = [
            os.path.join(self.bin_dir, "ts-decision-check"),
            "--json",
            "Implementiere Pure-Go ICS Parser ohne externe Binärdateien mit Unit-Tests."
        ]
        r = subprocess.run(cmd, capture_output=True, text=True, timeout=15)
        self.assertEqual(r.returncode, 0, f"Stderr: {r.stderr}")
        payload = json.loads(r.stdout)
        self.assertEqual(payload.get("status"), expected)
        self.assertIn("masked_detail", payload)

    def test_review_dedup_cli(self):
        # See test_agent_dispatch_cli for how the expected status is derived.
        # Local dedup findings are present in both cases.
        expected = "ok" if tc.get_key() else "fail_open"
        cmd = [
            os.path.join(self.bin_dir, "ts-review-dedup"),
            "--json",
            "1. Dead code in file.go line 10",
            "2. Unused var in file.go line 10"
        ]
        r = subprocess.run(cmd, capture_output=True, text=True, timeout=15)
        self.assertEqual(r.returncode, 0, f"Stderr: {r.stderr}")
        payload = json.loads(r.stdout)
        self.assertEqual(payload.get("status"), expected)
        self.assertIn("findings", payload)


if __name__ == "__main__":
    unittest.main()
