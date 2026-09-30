#!/usr/bin/env python3
"""Python side of the mask-parity test (tests/mask-parity-cases.json).

Runs the shared, language-agnostic case list against ts_common.mask_detail so the
Python implementation is pinned to concrete expected behavior. A later package adds
the Go side (imprint-core tools/imprint-dev/typesafe.go MaskDetail) against the same
JSON file, so both implementations stay in parity without duplicating test data.

Uses its own temporary TYPESAFE_NAMES_FILE (populated from the "names" list in the
JSON, all synthetic) so this test never reads or depends on the real, private
~/.config/typesafe/names.txt.
"""
import json
import os
import sys
import tempfile
import unittest

sys.path.insert(0, os.path.join(os.path.dirname(os.path.abspath(__file__)), ".."))
import ts_common as tc  # noqa: E402

CASES_FILE = os.path.join(os.path.dirname(os.path.abspath(__file__)), "mask-parity-cases.json")


class TestMaskParity(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        with open(CASES_FILE, "r", encoding="utf-8") as f:
            cls.spec = json.load(f)
        fd, cls.names_path = tempfile.mkstemp(prefix="typesafe-parity-names-", suffix=".txt")
        with os.fdopen(fd, "w", encoding="utf-8") as f:
            for name in cls.spec["names"]:
                f.write(name + "\n")
        cls._prev_env = os.environ.get("TYPESAFE_NAMES_FILE")
        os.environ["TYPESAFE_NAMES_FILE"] = cls.names_path
        tc.get_name_regex(cls.names_path)  # force reload under the isolated names file

    @classmethod
    def tearDownClass(cls):
        if cls._prev_env is None:
            os.environ.pop("TYPESAFE_NAMES_FILE", None)
        else:
            os.environ["TYPESAFE_NAMES_FILE"] = cls._prev_env
        try:
            os.remove(cls.names_path)
        except OSError:
            pass

    def test_cases(self):
        for case in self.spec["cases"]:
            with self.subTest(case=case["id"]):
                masked, counts = tc.mask_detail(case["text"])
                for secret in case.get("must_not_contain", []):
                    self.assertNotIn(secret, masked,
                                      f"[{case['id']}] secret survived masking: {secret!r}")
                for cat, minimum in case.get("min_counts", {}).items():
                    self.assertGreaterEqual(counts.get(cat, 0), minimum,
                                             f"[{case['id']}] category {cat} count "
                                             f"{counts.get(cat, 0)} < {minimum}")
                for cat, exact in case.get("exact_counts", {}).items():
                    self.assertEqual(counts.get(cat, 0), exact,
                                      f"[{case['id']}] category {cat} count "
                                      f"{counts.get(cat, 0)} != {exact}")


if __name__ == "__main__":
    unittest.main()
