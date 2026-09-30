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
        text = "Hier ist token = abcdef1234567890abcdef123456 und password = secret123"
        masked, hits = tc.mask_detail(text)
        self.assertNotIn("secret123", masked)
        self.assertGreaterEqual(hits["secret_kw"], 1)

    def test_mask_detail_emails(self):
        text = "Kontakt: max.mustermann@example.org für Support"
        masked, hits = tc.mask_detail(text)
        self.assertNotIn("max.mustermann@example.org", masked)
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


class TestCliTools(unittest.TestCase):
    def setUp(self):
        self.bin_dir = os.path.join(os.path.dirname(os.path.realpath(__file__)), "..", "bin")

    def test_agent_dispatch_cli(self):
        # No TypeSafe key/network in CI: status is "fail_open" there, "ok" only
        # when a real key is configured (e.g. local dev via secret-tool). Both
        # are valid CLI outcomes (fail-open by design), so accept either and
        # only check the model/workspace fields when a live answer came back.
        cmd = [
            os.path.join(self.bin_dir, "ts-agent-dispatch"),
            "--json",
            "DELEGATED BY: lead ROLE: advisor, read-only. Führe Lese-Review durch."
        ]
        r = subprocess.run(cmd, capture_output=True, text=True, timeout=15)
        self.assertEqual(r.returncode, 0, f"Stderr: {r.stderr}")
        payload = json.loads(r.stdout)
        self.assertIn(payload.get("status"), ("ok", "fail_open"))
        self.assertIn("masked_detail", payload)
        if payload["status"] == "ok":
            self.assertEqual(payload["model_tier"], "flash")
            self.assertEqual(payload["workspace_mode"], "inherit")

    def test_decision_check_cli(self):
        # See test_agent_dispatch_cli: fail_open (no key/network) is a valid
        # outcome too, expected in CI.
        cmd = [
            os.path.join(self.bin_dir, "ts-decision-check"),
            "--json",
            "Implementiere Pure-Go ICS Parser ohne externe Binärdateien mit Unit-Tests."
        ]
        r = subprocess.run(cmd, capture_output=True, text=True, timeout=15)
        self.assertEqual(r.returncode, 0, f"Stderr: {r.stderr}")
        payload = json.loads(r.stdout)
        self.assertIn(payload.get("status"), ("ok", "fail_open"))
        self.assertIn("masked_detail", payload)

    def test_review_dedup_cli(self):
        # See test_agent_dispatch_cli: fail_open (no key/network) is a valid
        # outcome too, expected in CI. Local dedup findings are present in
        # both cases.
        cmd = [
            os.path.join(self.bin_dir, "ts-review-dedup"),
            "--json",
            "1. Dead code in file.go line 10",
            "2. Unused var in file.go line 10"
        ]
        r = subprocess.run(cmd, capture_output=True, text=True, timeout=15)
        self.assertEqual(r.returncode, 0, f"Stderr: {r.stderr}")
        payload = json.loads(r.stdout)
        self.assertIn(payload.get("status"), ("ok", "fail_open"))
        self.assertIn("findings", payload)


if __name__ == "__main__":
    unittest.main()
