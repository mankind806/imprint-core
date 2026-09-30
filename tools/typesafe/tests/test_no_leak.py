#!/usr/bin/env python3
"""Nothing unmasked leaves the machine: offline tests, no key, no network.

- Recorder in place of ts_common.post: for each of the 8 tools and each data class
  (api_key, email, name, street, postcode+city, opaque token) no sentinel value
  reaches tc.post.
- Central mask: tc.post masks every string leaf of state and questions itself
  (checked at the HTTP layer against a local server).
- Mask first, then cut (ts-done-check, ts-commit-check, ts-pr-triage).
- ts-commit-check: a local keyword/email hit is a finding even without a key.
- tc.post never follows a redirect (no request, no Authorization to a foreign host).

All names, addresses and keys below are synthetic. The names file is a temporary
file; the real ~/.config/typesafe/names.txt is never read or written.
"""
import contextlib
import importlib.machinery
import importlib.util
import io
import json
import os
import random
import string
import subprocess
import sys
import tempfile
import threading
import unittest
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from unittest import mock

sys.dont_write_bytecode = True  # loading the extension-less scripts must not write bin/__pycache__
HERE = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, os.path.join(HERE, ".."))
import ts_common as tc  # noqa: E402

BIN = os.path.join(HERE, "..", "bin")
TOOLS = ("ts-agent-dispatch", "ts-ci-triage", "ts-commit-check", "ts-decision-check",
         "ts-done-check", "ts-pr-triage", "ts-review-dedup", "ts-route")

# data class -> (text that carries it, fragments that must never reach tc.post)
CLASSES = {
    "api_key": ("api_key=wachWertAlpha", ["wachWertAlpha"]),
    "email": ("wache.sentinel@mail.example", ["wache.sentinel", "mail.example"]),
    "name": ("Wanda Wachtmeister", ["Wanda", "Wachtmeister"]),
    "street": ("Wachtelweg 17", ["Wachtelweg"]),
    "plz_ort": ("54321 Wachtstadt", ["54321", "Wachtstadt"]),
    "token": ("Zq7xK9pL2mN4vB8cR1tY6wE3", ["Zq7xK9pL2mN4vB8cR1tY6wE3", "Zq7xK9pL2mN4"]),
    # built from parts so this file's own diff carries none of them whole
    "pw_quoted": ("pass" + 'word = "mein geheimes Passwort 2024"', ["geheimes", "Passwort 2024"]),
    "db_pass": ("DB_PA" + "SS=Hunter" + "2024Secret!", ["Hunter2024"]),
    "aws_key_id": ("Schlüssel-ID AKIA" + "Q7X2M4P9R3T6W8Y1 im Log", ["Q7X2M4P9R3T6W8Y1"]),
    "iban": ("DE89 3704 0044 0532 0130 00", ["3704 0044", "0532 0130"]),
    "phone": ("+49 221 12345678", ["12345678", "221 1234"]),
}
NAMES = ("Wanda Wachtmeister", "Max Mustermann", "Erika Musterfrau")


def load_tool(name):
    path = os.path.join(BIN, name)
    loader = importlib.machinery.SourceFileLoader(name.replace("-", "_"), path)
    spec = importlib.util.spec_from_loader(loader.name, loader)
    mod = importlib.util.module_from_spec(spec)
    loader.exec_module(mod)
    return mod


class Recorder:
    """Stands in for tc.post: records what would be sent, answers like 'no key'."""

    def __init__(self):
        self.calls = []

    def __call__(self, state, questions, timeout=None):
        self.calls.append(json.dumps({"state": state, "questions": questions}, ensure_ascii=False))
        return None


class _NamesFileMixin:
    @classmethod
    def setUpClass(cls):
        fd, cls.names_path = tempfile.mkstemp(prefix="typesafe-noleak-names-", suffix=".txt")
        with os.fdopen(fd, "w", encoding="utf-8") as f:
            f.write("\n".join(NAMES) + "\n")
        cls._prev_names = os.environ.get("TYPESAFE_NAMES_FILE")
        os.environ["TYPESAFE_NAMES_FILE"] = cls.names_path
        tc.get_name_regex(cls.names_path)
        cls.tmp = tempfile.mkdtemp(prefix="typesafe-noleak-")
        cls.mods = {name: load_tool(name) for name in TOOLS}

    @classmethod
    def tearDownClass(cls):
        if cls._prev_names is None:
            os.environ.pop("TYPESAFE_NAMES_FILE", None)
        else:
            os.environ["TYPESAFE_NAMES_FILE"] = cls._prev_names
        os.remove(cls.names_path)
        for f in os.listdir(cls.tmp):
            os.remove(os.path.join(cls.tmp, f))
        os.rmdir(cls.tmp)


class TestNoSentinelReachesPost(_NamesFileMixin, unittest.TestCase):
    def drive(self, tool, text):
        """Run one tool on text; returns the recorder."""
        mod = self.mods[tool]
        rec = Recorder()
        with mock.patch.object(tc, "post", rec), contextlib.redirect_stdout(io.StringIO()), \
                contextlib.redirect_stderr(io.StringIO()):
            if tool == "ts-agent-dispatch":
                mod.dispatch_task(f"Aufgabe: {text} bitte umsetzen")
            elif tool == "ts-ci-triage":
                mod.classify(f"FAIL test_x\nerror near {text}\nexit 1")
            elif tool == "ts-commit-check":
                mod.check_commit(f"feat: add {text}", f"+ config {text}\n", f"cfg/{text} | 1 +")
            elif tool == "ts-decision-check":
                reg = os.path.join(self.tmp, "register.txt")
                with open(reg, "w", encoding="utf-8") as f:
                    f.write(f"1. Kontakt im Register: {text}\n")
                mod.check_decision(f"Vorschlag mit {text}", invariants_file=reg)
            elif tool == "ts-done-check":
                tp = os.path.join(self.tmp, "transcript.jsonl")
                with open(tp, "w", encoding="utf-8") as f:
                    f.write(json.dumps({"message": {"content": [
                        {"type": "tool_use", "id": "t1", "name": "Bash",
                         "input": {"command": f"echo {text}"}}]}}) + "\n")
                    f.write(json.dumps({"message": {"content": [
                        {"type": "tool_result", "tool_use_id": "t1", "is_error": False}]}}) + "\n")
                stdin = json.dumps({"transcript_path": tp,
                                    "last_assistant_message": f"Fertig, erledigt für {text}."})
                with mock.patch.object(sys, "stdin", io.StringIO(stdin)):
                    mod.main()
            elif tool == "ts-pr-triage":
                def fake_run(cmd, cwd=None):
                    if "--name-only" in cmd:
                        return "src/app.py"
                    return f"change touching {text}"
                with mock.patch.object(mod, "run_cmd", fake_run):
                    mod.evaluate_diff("A..B")
            elif tool == "ts-review-dedup":
                mod.triage_findings([f"Befund eins zu {text} in app.py",
                                     f"Befund zwei, wieder {text} in app.py"])
            elif tool == "ts-route":
                with mock.patch.object(sys, "argv", ["ts-route", f"Aufgabe: {text} anpassen"]):
                    mod.main()
        return rec

    def test_every_tool_every_class(self):
        for tool in TOOLS:
            for cls_name, (text, fragments) in CLASSES.items():
                with self.subTest(tool=tool, data=cls_name):
                    rec = self.drive(tool, text)
                    self.assertGreaterEqual(len(rec.calls), 1, f"{tool} never called tc.post")
                    for payload in rec.calls:
                        for frag in fragments:
                            self.assertNotIn(frag, payload, f"{tool}/{cls_name}: {frag!r} reached tc.post")


class TestMaskThenCut(_NamesFileMixin, unittest.TestCase):
    SECRET = "api_key=wachWertBeta"
    TOKEN = "Qw8eR7tY6uI5oP4aS3dF2gH1jK9lZx"  # 30 chars; a 20-char fragment is no longer "opaque"
    MAIL = "wache.sentinel@mail.example"

    def test_done_check_tail_cut_inside_api_key(self):
        mod = self.mods["ts-done-check"]
        # the last MAX_MSG chars start 3 chars into "api_key=", i.e. at "_key=wachWertBeta"
        suffix = " z" * ((mod.MAX_MSG - len(self.SECRET) + 3) // 2)
        suffix += " " * (mod.MAX_MSG - len(self.SECRET) + 3 - len(suffix))
        msg = "Fertig, erledigt. " + "a " * 100 + self.SECRET + suffix
        self.assertTrue(msg[-mod.MAX_MSG:].startswith("_key=wachWertBeta"))
        rec = Recorder()
        stdin = json.dumps({"last_assistant_message": msg})
        with mock.patch.object(tc, "post", rec), mock.patch.object(sys, "stdin", io.StringIO(stdin)):
            mod.main()
        self.assertEqual(len(rec.calls), 1)
        self.assertNotIn("wachWertBeta", rec.calls[0])

    @staticmethod
    def cut_inside(fragment, offset, cut_at=12000):
        """Diff whose head cut at cut_at lands `offset` chars into `fragment`."""
        diff = ("+" + "y " * ((cut_at - offset) // 2)).ljust(cut_at - offset)[:cut_at - offset]
        diff += fragment + "\n"
        assert diff[:cut_at].endswith(fragment[:offset])
        return diff

    def head_cut_cases(self):
        # (diff, fragments that must not reach tc.post)
        return [
            (self.cut_inside(self.SECRET.replace("Beta", "Gamma"), 5), ["wachWertGamma"]),  # inside "api_key="
            (self.cut_inside(self.TOKEN, 20), [self.TOKEN[:12]]),  # 20-char token fragment
            (self.cut_inside(self.MAIL, len("wache.sentinel@mai")), ["wache.sentinel"]),  # inside the domain
        ]

    def test_commit_check_head_cut(self):
        mod = self.mods["ts-commit-check"]
        for diff, fragments in self.head_cut_cases():
            rec = Recorder()
            with mock.patch.object(tc, "post", rec):
                mod.check_commit("feat: config", diff, "cfg | 1 +")
            self.assertEqual(len(rec.calls), 1)
            for frag in fragments:
                self.assertNotIn(frag, rec.calls[0])

    def test_pr_triage_head_cut(self):
        mod = self.mods["ts-pr-triage"]
        for diff, fragments in self.head_cut_cases():
            def fake_run(cmd, cwd=None, diff=diff):
                if "--name-only" in cmd:
                    return "src/app.py"
                if cmd[:2] == ["git", "diff"] and "--stat" not in cmd:
                    return diff
                return "abc feat: config"
            rec = Recorder()
            with mock.patch.object(tc, "post", rec), mock.patch.object(mod, "run_cmd", fake_run):
                mod.evaluate_diff("A..B")
            self.assertEqual(len(rec.calls), 1)
            for frag in fragments:
                self.assertNotIn(frag, rec.calls[0])


# Built from parts so this test file's own diff carries no real-looking secret.
REAL_KEY_LINE = "api" + '_key = "sk-live-' + '4f9a8b7c6d5e4f3a2b1c"'
REAL_BEARER_LINE = "Authorization: " + "Bearer " + "eyJhbGciOiJIUzI1NiJ9" + "abcdef123456"
GO_CODE_LINES = [
    '// Laufs (seitenToken == "").',
    'if err != nil && seitenToken == "" {',
    'token = s.Weiter',
    'req.Header.Set("Authorization", "Bearer " + token)',
    'bearer := "Bearer " + tok',
    # synthetic fixtures from an OAuth adapter test (_test.go)
    '[]byte(`{"client_id": "synth-client-id", "client_secret": "synth-secret"}`)',
    '"access_token": "gueltiger-access-token",',
    '"refresh_token": "synth-refresh",',
    'req.Header.Set("Authorization", "Bearer gueltiger-access-token")',
]
AT = "@"
RESERVED_LINES = [f'owner = "nutzer{AT}example.com"', f'fallback = "a{AT}b.test"',
                  f'relay = "x{AT}mail.example.com"']
NOREPLY_TRAILERS = (f"Co-Authored-By: Claude <noreply{AT}anthropic.com>\n"
                    f"Co-Authored-By: Gemini <noreply{AT}google.com>\n"
                    "Assisted-by: Claude Code")
GHP = "ghp_" + "4f9a8b7c6d5e4f3a2b1c0d9e8f7a6b5c4d3e"
FREE_PREFIX_LINES = [  # known token prefixes without a secret keyword in front
    'auth: "' + GHP + '"',
    'client = OpenAI("sk-' + 'proj-4f9a8b7c6d5e4f3a2b1c")',
    'slack = WebClient("xoxb-' + '1234567890-abcdefghij")',
    "Authorization: token " + GHP,
    'stripe.api = "sk_' + 'test_51Hx4f9a8b7c6d5e"',
]


class TestCommitCheckLocalLeak(unittest.TestCase):
    """A real-looking secret in the diff is a finding even without a key (fail-open mode)."""

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory(prefix="typesafe-commitcheck-")
        self.addCleanup(self.tmp.cleanup)
        fakebin = os.path.join(self.tmp.name, "fakebin")
        os.mkdir(fakebin)
        with open(os.path.join(fakebin, "secret-tool"), "w") as f:
            f.write("#!/bin/sh\nexit 1\n")
        os.chmod(os.path.join(fakebin, "secret-tool"), 0o755)
        self.env = {k: v for k, v in os.environ.items() if k != "TYPESAFE_API_KEY"}
        self.env["PATH"] = fakebin + os.pathsep + self.env.get("PATH", "")
        self.env["PYTHONDONTWRITEBYTECODE"] = "1"
        self.repo = os.path.join(self.tmp.name, "repo")
        subprocess.run(["git", "init", "-q", self.repo], check=True)

    def run_check(self, content, msg="feat: add config", path="config.py"):
        full = os.path.join(self.repo, path)
        os.makedirs(os.path.dirname(full), exist_ok=True)
        with open(full, "w") as f:
            f.write(content)
        subprocess.run(["git", "-C", self.repo, "add", path], check=True)
        return self.run_tool("ts-commit-check", "--cached", "--msg", msg)

    def run_tool(self, tool, *args):
        return subprocess.run([os.path.join(BIN, tool), *args, "--cwd", self.repo],
                              capture_output=True, text=True, timeout=60, env=self.env)

    def commit(self, content, msg):
        with open(os.path.join(self.repo, "config.py"), "w") as f:
            f.write(content)
        ident = ["-c", "user.name=t", "-c", "user.email=t" + AT + "t.invalid"]
        subprocess.run(["git", "-C", self.repo, "add", "config.py"], check=True)
        subprocess.run(["git", "-C", self.repo, *ident, "commit", "-q", "-m", msg], check=True)

    # --- regression suite: these must stay exit 0 ---
    def test_regression_suite_passes(self):
        r = self.run_check("\n".join(GO_CODE_LINES + RESERVED_LINES) + "\n",
                           msg="feat: adapter\n\nBody.\n\n" + NOREPLY_TRAILERS)
        self.assertEqual(r.returncode, 0, r.stdout + r.stderr)

    def test_regression_empty_commit_with_trailer_passes(self):
        r = self.run_tool("ts-commit-check", "--cached", "--msg", "docs: x\n\n" + NOREPLY_TRAILERS)
        self.assertEqual(r.returncode, 0, r.stdout + r.stderr)

    # --- blind round 3, A: executor identities and non-addresses ---
    def test_declared_identities_pass(self):
        subprocess.run(["git", "-C", self.repo, "config", "user.email", "dev" + AT + "firma-intern.de"], check=True)
        subprocess.run(["git", "-C", self.repo, "config", "--add", "imprint.allowedIdentity",
                        "Jules <jules" + AT + "google.com>"], check=True)
        msg = ("feat: x\n\nCo-authored-by: Jules <jules" + AT + "google.com>\n"
               "Co-authored-by: Codex <noreply" + AT + "openai.com>\nReviewed-by: dev" + AT + "firma-intern.de")
        r = self.run_check("retries = 3\n", msg=msg)
        self.assertEqual(r.returncode, 0, r.stdout + r.stderr)
        # not declared in this clone: still an address
        r = self.run_check("retries = 4\n", msg="feat: y\n\nCo-authored-by: Antigravity <antigravity" + AT + "google.com>")
        self.assertEqual(r.returncode, 1, r.stdout + r.stderr)

    def test_units_calendar_ids_and_word_tokens_pass(self):
        lines = [  # from imprint 15b17a3, 73cdc2c, c5c6d65, 9c4da41, 98344bb
            "cp betrieb/systemd/betrieb-job" + AT + "wissen-vault.service ~/.config/systemd/user/",
            "systemctl --user enable --now betrieb-job" + AT + "wissen-vault.timer",
            'ad := adapterFuer(u, c, "arbeit' + AT + 'group.calendar.google.com")',
            'case "/calendar/v3/calendars/arbeit' + AT + 'import.calendar.google.com/events/t1":',
            'NextPageToken: "page-token-3",',
            'ersterToken:  "expired_tok_1",',
            'if auth := r.Header.Get("Authorization"); auth != "Bearer geheimer-schluessel" {',
        ]
        for line in lines:
            with self.subTest(line=line):
                self.assertFalse(tc.local_alarm(tc.alarm_view(line)))
        r = self.run_check("\n".join(lines) + "\n",
                           msg="feat: unit betrieb-job" + AT + "wissen-vault.service",
                           path="betrieb/systemd/betrieb-job" + AT + "wissen-vault.service")
        self.assertEqual(r.returncode, 0, r.stdout + r.stderr)
        # a real-looking address on a unit-like but different domain still counts
        self.assertTrue(tc.local_alarm(tc.alarm_view("mail max" + AT + "service.de")))
        # one-letter fixtures are synthetic, two letters are not
        self.assertFalse(tc.local_alarm(tc.alarm_view('raw: "From: a' + AT + 'b.com\\r\\n"')))
        self.assertTrue(tc.local_alarm(tc.alarm_view("From: ab" + AT + "b.com")))

    # --- blind round 2 ---
    def test_real_address_in_trailer_blocks(self):
        r = self.run_check("retries = 3\n", msg=f"docs: x\n\nCo-Authored-By: Max <max.privat{AT}gmail.com>")
        self.assertEqual(r.returncode, 1, r.stdout + r.stderr)

    def test_address_in_file_name_blocks(self):
        r = self.run_check("hallo\n", path=f"max.mustermann{AT}gmx.de/notizen/liste.txt")
        self.assertEqual(r.returncode, 1, r.stdout + r.stderr)

    def test_range_mode_checks_every_commit(self):
        self.commit("retries = 3\n", "chore: base")
        self.commit(REAL_KEY_LINE + "\n", "feat: key")
        self.commit("retries = 3\n", "fix: drop key")
        r = self.run_tool("ts-commit-check", "HEAD~2..HEAD")
        self.assertEqual(r.returncode, 1, r.stdout + r.stderr)
        r = self.run_tool("ts-commit-check", "HEAD~1..HEAD")  # only the removal
        self.assertEqual(r.returncode, 0, r.stdout + r.stderr)

    def test_added_line_starting_with_plus_plus_counts(self):
        diff = "diff --git a/x b/x\n--- a/x\n+++ b/x\n@@ -0,0 +1 @@\n++" + REAL_KEY_LINE + "\n"
        self.assertTrue(tc.leak_found("feat: x", diff, "x"))
        header_only = "diff --git a/x b/x\n--- a/x\n+++ b/" + REAL_KEY_LINE + "\n@@ -0,0 +1 @@\n+ok\n"
        self.assertFalse(tc.leak_found("feat: x", header_only, "x"))

    def test_known_prefix_without_keyword_blocks(self):
        for line in FREE_PREFIX_LINES:
            with self.subTest(line=line):
                self.assertTrue(tc.local_alarm(tc.alarm_view(line)))
        r = self.run_check(FREE_PREFIX_LINES[1] + "\n")
        self.assertEqual(r.returncode, 1, r.stdout + r.stderr)

    def test_random_keys_alarm(self):
        rng = random.Random(20260930)
        alphabets = {"hex": "0123456789abcdef", "base62": string.ascii_letters + string.digits}
        for name, alphabet in alphabets.items():
            for label, lengths in (("len12", [12]), ("len12-64", list(range(12, 65)))):
                hits = 0
                for _ in range(2000):
                    v = "".join(rng.choice(alphabet) for _ in range(rng.choice(lengths)))
                    hits += tc.local_alarm('api_key = "' + v + '"')
                with self.subTest(alphabet=name, lengths=label):
                    self.assertGreaterEqual(hits / 2000, 0.99, f"{name} {label}: {hits}/2000")

    def test_password_rules(self):
        loud = ["pass" + 'word = "Contest-Winner-2024"', "pass" + 'word = "CorrectHorseBatteryStaple"',
                "db_pa" + 'ss = "mein geheimes Passwort 2024"', "pw" + 'd = "Sommer2024!"',
                "DB_PA" + "SSWORD=Hunter2024Secret"]
        quiet = ['password = "synth-password"', 'password_label = "Passwort eingeben"',
                 'passwordPlaceholder = "Mindestens 8 Zeichen"', 'bypass = "compassionate-mode"',
                 'pwd := os.Getwd()', 'password := cfg.Password', 'password = ""']
        for line in loud:
            with self.subTest(loud=line):
                self.assertTrue(tc.local_alarm(tc.alarm_view(line)))
        for line in quiet:
            with self.subTest(quiet=line):
                self.assertFalse(tc.local_alarm(tc.alarm_view(line)))

    def test_pr_triage_without_key_reports_local_leak(self):
        self.commit("retries = 3\n", "chore: base")
        self.commit(REAL_KEY_LINE + "\n", "feat: key")
        r = self.run_tool("ts-pr-triage", "HEAD~1..HEAD", "--json")
        self.assertEqual(r.returncode, 0, r.stdout + r.stderr)
        res = json.loads(r.stdout)
        self.assertEqual((res["status"], res["leak_prob"]), ("fail_open", 1.0))
        r = self.run_tool("ts-pr-triage", "HEAD~1..HEAD")
        self.assertEqual(r.returncode, 0, r.stdout + r.stderr)
        self.assertIn("Leak-Verdacht", r.stdout)
        self.commit("retries = 4\n", "fix: retries")
        res = json.loads(self.run_tool("ts-pr-triage", "HEAD~1..HEAD", "--json").stdout)
        self.assertEqual((res["status"], res["leak_prob"]), ("fail_open", None))

    def test_trailer_address_in_body_does_not_block(self):
        # Built from parts so this very test file carries no address in its diff.
        trailer = "Co-Authored-By: Bot <" + "noreply" + "@" + "bots.example>"
        r = self.run_check("retries = 3\n", msg=f"docs: tidy\n\n{trailer}")
        self.assertEqual(r.returncode, 0, r.stdout + r.stderr)

    def test_removing_a_secret_does_not_block(self):
        # Built from parts so this very test file adds no keyword secret in its diff.
        self.run_check(REAL_KEY_LINE + "\n")
        ident = ["-c", "user.name=t", "-c", "user.email=t" + "@" + "t.invalid"]
        subprocess.run(["git", "-C", self.repo, *ident, "commit", "-q", "-m", "seed"], check=True)
        r = self.run_check("retries = 3\n", msg="fix: drop the leaked value")
        self.assertEqual(r.returncode, 0, r.stdout + r.stderr)

    def test_noreply_trailers_and_identities_pass(self):
        # Addresses built from parts so this test file's own diff carries none.
        at = "@"
        msg = ("feat: add config\n\nBody text.\n\n"
               f"Co-Authored-By: Claude <noreply{at}anthropic.com>\n"
               f"Signed-off-by: Dev <12345+dev{at}users.noreply.github.com>\n"
               "Assisted-by: Claude Code")
        diff_line = f"owner = 12345+dev{at}users.noreply.github.com, bot = noreply{at}github.com\n"
        r = self.run_check(diff_line, msg=msg)
        self.assertEqual(r.returncode, 0, r.stdout + r.stderr)

    # Real-looking addresses below are synthetic and built from parts, so this
    # test file's own diff carries none.
    def test_real_address_in_body_blocks(self):
        at = "@"
        r = self.run_check("retries = 3\n", msg=f"docs: tidy\n\nKontakt: max.mustermann{at}gmx.de\n")
        self.assertEqual(r.returncode, 1, r.stdout + r.stderr)

    def test_real_address_in_diff_blocks(self):
        at = "@"
        r = self.run_check(f"owner = max.mustermann{at}gmx.de\n")
        self.assertEqual(r.returncode, 1, r.stdout + r.stderr)

    def test_lookalike_noreply_and_reserved_block(self):
        at = "@"
        for addr in (f"noreply{at}github.com.attacker.io", f"nutzer{at}example.com.attacker.io",
                     f"nutzer{at}mail.test.de"):
            with self.subTest(addr=addr):
                r = self.run_check(f"owner = {addr}\n")
                self.assertEqual(r.returncode, 1, r.stdout + r.stderr)

    def test_reserved_domains_pass(self):
        at = "@"
        addrs = [f"nutzer{at}example.com", f"a{at}example.org", f"b{at}example.net",
                 f"c{at}mail.example.com", f"a{at}b.test", f"d{at}mail.example",
                 f"e{at}host.invalid", f"f{at}dev.localhost", f"G{at}Mail.Example.COM"]
        r = self.run_check("fixtures = [" + ", ".join(addrs) + "]\n",
                           msg=f"test: fixtures\n\nSee x{at}b.test.")
        self.assertEqual(r.returncode, 0, r.stdout + r.stderr)

    def test_reserved_domains_still_masked_toward_typesafe(self):
        mod = load_tool("ts-commit-check")
        rec = Recorder()
        at = "@"
        with mock.patch.object(tc, "post", rec):
            res = mod.check_commit("test: fixtures", f"+owner = nutzer{at}example.com\n", "a | 1 +")
        self.assertIsNone(res["leak_prob"])
        self.assertNotIn(f"nutzer{at}example.com", rec.calls[0])
        self.assertIn("<email>", rec.calls[0])

    def test_address_in_subject_blocks(self):
        r = self.run_check("retries = 3\n", msg="docs: mail " + "ops" + "@" + "gmx.de")
        self.assertEqual(r.returncode, 1, r.stdout + r.stderr)

    def test_api_key_without_key_blocks(self):
        r = self.run_check(REAL_KEY_LINE + "\n")
        self.assertEqual(r.returncode, 1, r.stdout + r.stderr)
        self.assertIn("ABBRUCH", r.stdout)
        self.assertNotIn("4f9a8b7c6d5e4f3a2b1c", r.stdout)

    def test_bearer_literal_without_key_blocks(self):
        r = self.run_check(REAL_BEARER_LINE + "\n")
        self.assertEqual(r.returncode, 1, r.stdout + r.stderr)

    def test_go_code_with_token_names_passes(self):
        r = self.run_check("\n".join(GO_CODE_LINES) + "\n")
        self.assertEqual(r.returncode, 0, r.stdout + r.stderr)

    def test_local_alarm_rules(self):
        quiet = GO_CODE_LINES + [
            'api_key = ""', 'api_key = "<redacted>"', 'token = "xxxxxxxxxxxxxxxx"',
            'password = "${DB_PASSWORD_VALUE}"', 'secret = "REPLACE_ME_BEFORE_USE"',
            'token := os.Getenv("SERVICE_TOKEN")', 'token: see the setup guide',
            'token = "Bitte Token eingeben"', 'pageToken = nextPageTokenFromResponse2',
            "Basic authentication", 'password = "hunter2"',
            'token = "aaaa1111aaaa1111"',  # 12+ chars with digits, entropy 1.0
            'token = "risk-level-medium"', 'token = "task-list-overview"',  # "sk-" mid-word
            'token = "fake-4f9a8b7c6d5e4f3a2b1c"', 'api_key = "beispiel-Schluessel-2024"',
            'token_type = "access_token_type"', 'npm_config_cache = "/tmp/npm-cache"']
        # Built from parts so this test file's own diff carries none of them whole.
        loud = [REAL_KEY_LINE, REAL_BEARER_LINE,
                '"to' + 'ken": "abcd1234efgh5678"',
                "API" + "_KEY=sk_live_4f9a8b7c6d5e4f3a2b",
                "Basic " + "dXNlcjpwYXNzd29yZDEyMzQ=",
                '"client_' + 'secret": "GOCSPX-' + '4f9a8b7c6d5e4f3a2b1c"',
                '"refresh_' + 'token": "1//' + '0gAbCdEf123456"',
                '"api_' + 'key": "k9Xq2mV7' + 'pL4rT8wZ3nB6"',
                '"to' + 'ken": "synth-ghp_' + '4f9a8b7c6d5e4f3a2b1c0d9e"',
                '"to' + 'ken": "ghp_' + 'test"',  # known prefix at the start: always
                'token = "fake-' + 'glp' + 'at-4f9a8b7c6d5e"',
                'to' + 'ken = "sk_' + 'test_51Hx4f9a8b7c6d5e"',
                "aws_id = AKIA" + "Q7X2M4P9R3T6W8Y1"]
        for line in quiet:
            with self.subTest(quiet=line):
                self.assertFalse(tc.local_alarm(tc.alarm_view(line)))
        for line in loud:
            with self.subTest(loud=line):
                self.assertTrue(tc.local_alarm(tc.alarm_view(line)))

    def test_mask_phone_iban_no_false_hits(self):
        text = "Datum 01.01.2024, 04:06 Uhr, Version 0.12.3, PR #124, Port 8080"
        masked, counts = tc.mask_detail(text)
        self.assertEqual(masked, text)
        self.assertEqual((counts["phone"], counts["iban"]), (0, 0))

    def test_clean_diff_without_key_passes(self):
        r = self.run_check("retries = 3\n")
        self.assertEqual(r.returncode, 0, r.stdout + r.stderr)

    def test_json_carries_leak_prob(self):
        mod = load_tool("ts-commit-check")
        with mock.patch.object(tc, "post", Recorder()):
            res = mod.check_commit("feat: config", "+" + REAL_KEY_LINE + "\n", "config.py | 1 +")
        self.assertEqual(res["status"], "fail_open")
        self.assertEqual(res["leak_prob"], 1.0)


class _Server:
    """Local HTTP server on 127.0.0.1 that records requests and answers per `reply`."""

    def __init__(self, reply):
        self.requests = []
        outer = self

        class H(BaseHTTPRequestHandler):
            def _handle(self):
                n = int(self.headers.get("Content-Length") or 0)
                outer.requests.append({"path": self.path, "headers": dict(self.headers),
                                       "body": self.rfile.read(n) if n else b""})
                code, headers, body = reply(self)
                self.send_response(code)
                for k, v in headers.items():
                    self.send_header(k, v)
                self.send_header("Content-Length", str(len(body)))
                self.end_headers()
                self.wfile.write(body)

            do_GET = do_POST = _handle

            def log_message(self, *a):
                pass

        self.httpd = ThreadingHTTPServer(("127.0.0.1", 0), H)
        self.port = self.httpd.server_address[1]
        threading.Thread(target=self.httpd.serve_forever, daemon=True).start()

    def close(self):
        self.httpd.shutdown()
        self.httpd.server_close()


class TestPostTransport(_NamesFileMixin, unittest.TestCase):
    def test_redirect_to_foreign_host_is_not_followed(self):
        foreign = _Server(lambda h: (200, {}, b'{"answers": {}}'))
        self.addCleanup(foreign.close)
        origin = _Server(lambda h: (302, {"Location": f"http://localhost:{foreign.port}/landing"}, b""))
        self.addCleanup(origin.close)
        with mock.patch.object(tc, "URL", f"http://127.0.0.1:{origin.port}/v1/systemone"), \
                mock.patch.object(tc, "get_key", lambda: "dummy-test-value"):
            ans = tc.post({"x": "y"}, {"q": {"type": "noul", "instructions": "z"}}, timeout=5)
        self.assertIsNone(ans)
        self.assertEqual(len(origin.requests), 1)
        self.assertEqual(foreign.requests, [], "redirect was followed to a foreign host")

    def test_central_mask_on_all_string_leaves(self):
        srv = _Server(lambda h: (200, {"Content-Type": "application/json"},
                                 b'{"answers": {"sev_0": {"noul": 0.1}}}'))
        self.addCleanup(srv.close)
        state = {"raw": [c[0] for c in CLASSES.values()],
                 "nested": {"deep": "Kontakt " + ", ".join(c[0] for c in CLASSES.values())}}
        questions = {"sev_0": {"type": "choice",
                               "instructions": "Bewerte " + CLASSES["email"][0],
                               "criteria": {"a_key": CLASSES["street"][0], "b_key": CLASSES["name"][0]}},
                     "score_q": {"type": "score", "criteria": [CLASSES["plz_ort"][0], CLASSES["token"][0]]}}
        with mock.patch.object(tc, "URL", f"http://127.0.0.1:{srv.port}/v1/systemone"), \
                mock.patch.object(tc, "get_key", lambda: "dummy-test-value"):
            ans = tc.post(state, questions, timeout=5)
        self.assertEqual(ans, {"sev_0": {"noul": 0.1}})
        sent = json.loads(srv.requests[0]["body"])
        flat = json.dumps(sent, ensure_ascii=False)
        for _, fragments in CLASSES.values():
            for frag in fragments:
                self.assertNotIn(frag, flat)
        # keys are the answer contract and stay untouched
        self.assertEqual(set(sent["questions"]), {"sev_0", "score_q"})
        self.assertEqual(set(sent["questions"]["sev_0"]["criteria"]), {"a_key", "b_key"})

    def test_central_mask_leaves_tool_questions_intact(self):
        for tool, mod in self.mods.items():
            qs = mod.build_questions(["Tippfehler in der Doku", "Fehlender Test"]) \
                if tool == "ts-review-dedup" else mod.QUESTIONS
            with self.subTest(tool=tool):
                self.assertEqual(tc._mask_tree(qs), json.loads(json.dumps(qs)))


if __name__ == "__main__":
    unittest.main()
