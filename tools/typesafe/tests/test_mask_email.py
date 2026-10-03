#!/usr/bin/env python3
"""Email step of ts_common.mask_detail: linear, and the same matches as the regex it
replaced (mask parity spec, email step, 2026-10-02).

ts_common.RX_EMAIL is master's _LinearEmailMatcher: it anchors on '@' (as Go's
maskEmail does) and runs in linear time; alarm_view and local_alarm use it directly,
mask_detail takes its spans (_email_spans) for the NFC union _mask_emails. The regex it
replaced (the spec pattern, local part, '@', domain) is compiled in this file only, as
the oracle OLD_RX_EMAIL: the matcher has no finditer, and ts_common no longer holds the
regex. tests/test_tools.py (TestEmailLinear) checks .subn, .sub and .search against the
same regex on its own random alphabet; this file pins the exact spans
(_LinearEmailMatcher._spans) on the golden and parity corpora and on a soup that
stresses the local/domain boundary.

- Equivalence: _email_spans (RX_EMAIL._spans) gives the same spans as
  OLD_RX_EMAIL.finditer, leftmost-first, on every text of the golden and parity corpora
  and on a seeded random soup built from an alphabet that stresses the local/domain
  boundary ('@', '.', '+', '-', '_', ASCII letters and digits, ae-with-umlaut, sharp s,
  a combining diaeresis, a space, a non-ASCII digit, an NBSP); RX_EMAIL.subn (splice
  and count) matches OLD_RX_EMAIL.subn on every text, and on text in NFC so does
  _mask_emails, the email step of mask_detail. Superseded 2026-10-02 (user decision
  "Union"): this said the email step is RX_EMAIL.subn and matches OLD_RX_EMAIL.subn on
  every text; on text that is not NFC the step is now an NFC union (also the matches on
  an NFC view, mapped back), checked against its own reference in
  tests/test_mask_unicode.py (TestNfcUnionPasses).
- Linearity: the shapes that make Python's backtracking regex slow finish well under a
  time bound, in the style of the existing test_long_runs tests.

Synthetic data only; local/domain fragments are joined at runtime (never written next
to each other as one string literal) so no line here reads as an email address or
matches the pre-push leak shapes.
"""
import json
import os
import random
import re
import sys
import time
import unicodedata
import unittest

sys.path.insert(0, os.path.join(os.path.dirname(os.path.abspath(__file__)), ".."))
import ts_common as tc  # noqa: E402

HERE = os.path.dirname(os.path.abspath(__file__))
AT = chr(0x40)  # '@', always kept apart from surrounding text in this file
# The regex RX_EMAIL replaced (the spec pattern), here only as the oracle.
OLD_RX_EMAIL = re.compile(r"[\w.+-]+" + AT + r"[\w-]+\.[\w.-]+")


def _load_texts(filename):
    with open(os.path.join(HERE, filename), "r", encoding="utf-8") as f:
        spec = json.load(f)
    return [case["text"] for case in spec["cases"]]


def _ref_spans(text):
    return [m.span() for m in OLD_RX_EMAIL.finditer(text)]


class TestEmailSpansEquivalence(unittest.TestCase):
    """_email_spans and RX_EMAIL.subn against OLD_RX_EMAIL.finditer / .subn on every
    text, _mask_emails against OLD_RX_EMAIL.subn on text in NFC (the NFC union adds
    nothing there), leftmost-first."""

    def check(self, text):
        self.assertEqual(tc._email_spans(text), _ref_spans(text), repr(text))
        self.assertEqual(tc.RX_EMAIL.subn("<email>", text), OLD_RX_EMAIL.subn("<email>", text),
                         repr(text))
        if unicodedata.is_normalized("NFC", text):
            self.assertEqual(tc._mask_emails(text, "<email>"), OLD_RX_EMAIL.subn("<email>", text),
                             repr(text))

    def test_golden_corpus(self):
        texts = _load_texts("mask-golden.json")
        self.assertGreater(len(texts), 0, "no cases loaded")
        for text in texts:
            with self.subTest(text=text[:40]):
                self.check(text)

    def test_parity_corpus(self):
        texts = _load_texts("mask-parity-cases.json")
        self.assertGreater(len(texts), 0, "no cases loaded")
        for text in texts:
            with self.subTest(text=text[:40]):
                self.check(text)

    def test_random_soup(self):
        # ASCII letters/digits/local-part punctuation, '@' kept as its own element,
        # two non-ASCII letters, a combining diaeresis, a space, a non-ASCII digit
        # (Arabic-Indic zero) and an NBSP.
        pool = (list("abcABC019_") + [".", "+", "-", AT, "ä", "ß", chr(0x308),
                                       " ", chr(0x660), chr(0xA0)])
        rng = random.Random(20261002)
        for i in range(4000):
            n = rng.randint(0, 60)
            text = "".join(rng.choice(pool) for _ in range(n))
            with self.subTest(i=i, text=text[:40]):
                self.check(text)


class TestEmailStepIsLinear(unittest.TestCase):
    """Spec: OLD_RX_EMAIL.finditer backtracks over long non-matching runs (measured
    2026-10-02: "a"*20000 takes about 0.9 s, "a"*20000+"@"+"b"*20000 about 3.2 s).
    Anchoring on '@' turns this into a bounded number of steps per character. The third
    shape ("a@"*n) was already fast under the old regex (each '@' only ever starts one
    short, failing domain attempt); it stays here as a guard against the backward walk
    over the local part turning quadratic instead."""

    def test_long_runs_are_linear(self):
        n = 20000
        cases = ("a" * n, "a" * n + AT + "b" * n, ("a" + AT) * n)
        for text in cases:
            with self.subTest(text=text[:20]):
                t0 = time.perf_counter()
                tc._mask_emails(text, "<email>")
                self.assertLess(time.perf_counter() - t0, 2.0)


if __name__ == "__main__":
    unittest.main()
