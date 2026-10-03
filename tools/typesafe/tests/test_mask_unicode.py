#!/usr/bin/env python3
"""Unicode and union rules of ts_common.mask_detail (mask parity spec, 2026-10-02).

- Addresses: the fast leftmost-longest search (anchors + automata) against a plain
  brute-force reference over the union grammars, typed here independently of
  ts_common: every start in increasing order, every end in decreasing order,
  re.fullmatch, Python \\b at both ends; each pass as an NFC union (ref_union).
- NFC union of the email and address passes (user decision "Union", 2026-10-02): a
  pass finds its spans on the text and, if the text is not NFC, on an NFC copy whose
  spans map back (ref_nfc_map: built from the reference segments, widened like names);
  strictly overlapping spans merge, one count per merged span. Both steps against
  this reference on the golden and parity texts and on seeded soups with decomposed
  letters and marks, pinned examples, the NFC fast path, linear time on NFD input.
- Names: the NFC view (segments, map, widening, merging) against a plain reference
  segmentation (spec A1: a segment starts before combining class 0 with NFC quick
  check Yes), the shipped quick check Maybe set against one derived from unicodedata,
  linear time on long runs, and the NFC-then-length rule for names (A2).
- Names, local widening (A8): a property test that the masking of a text T is the
  same alone, before an NFC suffix and before a suffix that is not NFC, and pinned
  cases (every quick check Maybe starter after a name, a mark NFC keeps, a changed
  segment that still widens, the Kelvin sign that takes the space before it).
- Bearer/Basic, key=value (case-sensitive placeholder skip, A3) and phone rules on
  their named examples (exact output).

Synthetic data only. Values that look like a postcode, phone number or IBAN are built
from parts or with escapes so no line of this file matches the pre-push shapes.
"""
import json
import os
import random
import re
import sys
import tempfile
import time
import unicodedata
import unittest
from unittest import mock

sys.path.insert(0, os.path.join(os.path.dirname(os.path.abspath(__file__)), ".."))
import ts_common as tc  # noqa: E402

# --- Reference grammars (spec section 4), Python syntax, no \b inside -----------------
SUF_PY = ("stra[ßs]e|strasse|str\\.?|weg|gasse|platz|allee|ring|damm|ufer|chaussee|zeile|stieg"
          "|gässchen|pfad|markt")
SUF_GO = ("straße|strasse|str\\.|str|weg|gasse|platz|allee|ring|ufer|damm|chaussee|zeile|pfad"
          "|steig|gäßchen|gaesschen")
W_GO = "[a-zäöüßA-ZÄÖÜ0-9.-]"
PY_STREET = ("(?:(?:[A-ZÄÖÜ][a-zäöüß]+(?:\\s+|-))*(?:[A-ZÄÖÜ][a-zäöüß]+)?(?i:" + SUF_PY + ")"
             "|[a-zäöüß]+(?i:" + SUF_PY + "))"
             "\\s+\\d+(?:\\s*[a-zA-Z])?(?:\\s*[-/]\\s*\\d{1,4}(?:\\s*[a-zA-Z])?)?")
GO_STREET = ("(?:(?:Am|An der|Auf dem|Auf der|Im|In der|Vor dem|Hinter dem|Zum|Zur)\\s+[A-ZÄÖÜ]"
             + W_GO + "+(?:\\s+[A-ZÄÖÜ]" + W_GO + "+)*"
             "|(?:[A-ZÄÖÜ]" + W_GO + "+\\s+)*(?:Straße|Strasse|Str\\.|Str|[A-ZÄÖÜ]" + W_GO
             + "*(?i:" + SUF_GO + ")))"
             "\\s+\\d+[a-zA-Z]?(?:\\s*[-/]\\s*\\d{1,4}[a-zA-Z]?)?")
PY_PLZ = ("\\d{5}\\s+[A-ZÄÖÜ][a-zäöüß]+(?:[-/][A-ZÄÖÜ][a-zäöüß]+)*"
          "(?:\\s+(?:(?:am|an\\s+der|im)\\s+)?[A-ZÄÖÜ][a-zäöüß]+)?")
GO_PLZ = ("\\d{5}\\s+[A-ZÄÖÜ][a-zäöüßA-ZÄÖÜ.-]+"
          "(?:\\s+(?:(?:am|an der)\\s+[A-ZÄÖÜ][a-zäöüßA-ZÄÖÜ.-]+|im Breisgau|ob der Tauber))?")
REF_STREET = re.compile("(?:%s)|(?:%s)" % (PY_STREET, GO_STREET))
REF_PLZ = re.compile("(?:%s)|(?:%s)" % (PY_PLZ, GO_PLZ))


def is_word(c):
    return c == "_" or c.isalnum()


def boundary(text, p):
    return (p > 0 and is_word(text[p - 1])) != (p < len(text) and is_word(text[p]))


def ref_spans(rx, text):
    """Smallest valid start, at it the largest valid end, continue at the end."""
    bounds = [p for p in range(len(text) + 1) if boundary(text, p)]
    spans, pos = [], 0
    for i in bounds:
        if i < pos:
            continue
        for j in reversed(bounds):
            if j <= i:
                break
            if rx.fullmatch(text, i, j):
                spans.append((i, j))
                pos = j
                break
    return spans


def ref_nfc_map(text):
    """NFC copy of text, built segment by segment (ref_segment_starts, spec A1), and for
    every copy index the original offset of its first character and the one just past
    its last: one-to-one inside a segment NFC leaves unchanged, the whole segment
    inside one NFC changes (spec A8)."""
    bounds = ref_segment_starts(text) + [len(text)]
    pieces, first, past = [], [], []
    for a, b in zip(bounds, bounds[1:]):
        piece = unicodedata.normalize("NFC", text[a:b])
        if piece == text[a:b]:
            first += range(a, b)
            past += range(a + 1, b + 1)
        else:
            first += [a] * len(piece)
            past += [b] * len(piece)
        pieces.append(piece)
    return "".join(pieces), first, past


def ref_union(text, find, repl):
    """One pass as an NFC union (user decision "Union", 2026-10-02), plainly: the spans
    find gives on text; if text is not NFC, also the spans find gives on its NFC copy,
    each mapped back (ref_nfc_map); all of them sorted by start and merged when one
    starts strictly before the current end; every merged span replaced by repl, counted
    once. Text in NFC gives find's spans alone (they never overlap)."""
    spans = find(text)
    if unicodedata.normalize("NFC", text) != text:
        copy, first, past = ref_nfc_map(text)
        spans = spans + [(first[s], past[e - 1]) for s, e in find(copy)]
    merged = []
    for a, b in sorted(spans):
        if merged and a < merged[-1][1]:
            merged[-1][1] = max(merged[-1][1], b)
        else:
            merged.append([a, b])
    out, last = [], 0
    for a, b in merged:
        out += [text[last:a], repl]
        last = b
    out.append(text[last:])
    return "".join(out), len(merged)


def ref_addresses(text):
    text, n1 = ref_union(text, lambda t: ref_spans(REF_STREET, t), "<address>")
    text, n2 = ref_union(text, lambda t: ref_spans(REF_PLZ, t), "<address>")
    return text, n1 + n2


# The email pattern of the spec (README.md "Datenschutz & Maskierung"), typed here.
AT = chr(0x40)
REF_EMAIL = re.compile(r"[\w.+-]+" + AT + r"[\w-]+\.[\w.-]+")


def ref_emails(text):
    return ref_union(text, lambda t: [m.span() for m in REF_EMAIL.finditer(t)], "<email>")


TOKENS = (["Am", "An der", "Auf dem", "Auf der", "Im", "In der", "Vor dem", "Hinter dem", "Zum",
           "Zur", "An", "der", "am", "an", "im", "ob", "Breisgau", "Tauber", "ob der Tauber",
           "Haupt", "Linde", "Muster", "Bad", "Homburg", "Halle", "Saale", "Berlin", "BERLIN",
           "München", "Ku", "Ab1.-", "Aaa", "aaa", "bitte", "Brief", "See", "Post",
           "Weg", "weg", "straße", "Straße", "strasse", "Strasse", "STRASSE", "Str.", "Str", "str",
           "str.", "damm", "Markt", "markt", "allee", "gässchen", "gäßchen", "gaesschen", "steig",
           "stieg", "ring", "Ring", "zeile", "pfad", "ufer", "Chaussee", "platz", "gasse"]
          + ["1", "2", "12", "123", "1234", "4b", "99", "1" + "2345", "5432" + "1", "007",
             "\u0661\u0662", "\u0661\u0662\u0663\u0664\u0665", "1\u0662"]
          + ["a", "b", "Z", "-", "/", "'", ".", ",", ":", "\u017f", "\u212a", "\u0130", "\u0131",
             "\u1e9e", "\u00e9", "\u00b2", "\u216b", "_", "x", "ä", "Ä"])
SEPS = ["", " ", " ", " ", "  ", "\u00a0", "\u001c", "\t", "-", "\n", "\u3000"]


def soup(rng):
    out = []
    for _ in range(rng.randint(1, 9)):
        out.append(rng.choice(TOKENS))
        out.append(rng.choice(SEPS))
    return "".join(out).strip(" ") if rng.random() < 0.5 else "".join(out)


CAPS = ["Haupt", "Linde", "Muster", "Bad", "Berlin", "BERLIN", "München", "Ab1.-", "Aaa", "See",
        "Halle", "Goethe", "Freiburg", "Rothenburg", "Äu", "Breisgau", "Tauber", "Saale"]
SUFS = ["weg", "Weg", "straße", "Straße", "strasse", "Strasse", "STRASSE", "str.", "Str.", "str", "Str",
        "\u017ftraße", "\u017ftr", "damm", "markt", "Mar\u212at", "allee", "ring", "R\u0130NG",
        "zeile", "ze\u0131le", "stieg", "steig", "gässchen", "gäßchen", "gaesschen", "pfad", "ufer",
        "chaussee", "platz", "gasse", "stra\u1e9ee"]
PREPS = ["Am", "An der", "Auf dem", "Auf der", "Im", "In der", "Vor dem", "Hinter dem", "Zum", "Zur",
         "am", "an der", "im", "ob der", "An  der"]
DIGITS = ["1", "12", "123", "1234", "1" + "2345", "\u0661\u0662", "1\u0662", "99"]


def structured(rng):
    """Street- and postcode-like text with random near misses."""
    sp = lambda: rng.choice([" ", " ", " ", "\u00a0", "\u001c", "\t", "  ", "", "-"])  # noqa: E731
    out = []
    for _ in range(rng.randint(1, 3)):
        kind = rng.random()
        if kind < 0.45:  # street
            words = [rng.choice(CAPS + ["bitte", "an"]) for _ in range(rng.randint(0, 3))]
            if rng.random() < 0.4:
                words.insert(0, rng.choice(PREPS))
            last = rng.choice(CAPS + [""]) + rng.choice(SUFS) if rng.random() < 0.8 else rng.choice(CAPS)
            out.append(sp().join(words + [last]) + sp() + rng.choice(DIGITS))
            if rng.random() < 0.4:
                out.append(rng.choice(["a", "b", " a", "ä", "\u00e9", "_"]))
            if rng.random() < 0.3:
                out.append(sp() + rng.choice("-/") + sp() + rng.choice(DIGITS) + rng.choice(["", "b", " c"]))
        else:  # postcode + place
            out.append(rng.choice(["1234", "5432", "\u0661\u0662\u0663\u0664", "123"]) + rng.choice("5\u0665")
                       + sp() + rng.choice(CAPS))
            if rng.random() < 0.3:
                out.append(rng.choice("-/") + rng.choice(CAPS))
            if rng.random() < 0.5:
                out.append(sp() + rng.choice(PREPS + ["im Breisgau", "ob der Tauber"]) + sp() + rng.choice(CAPS))
            if rng.random() < 0.3:
                out.append(rng.choice(["1", ".", "\u00e9", "x", "-"]))
        out.append(rng.choice([" ", ", ", ". ", "\n", " und ", "ä", "x"]))
    text = "".join(out)
    return rng.choice(["", "x", "ä", "_", " ", "Termin "]) + text


class TestAddressUnion(unittest.TestCase):
    def check(self, text):
        self.assertEqual(tc._mask_addresses(text), ref_addresses(text), repr(text))

    def test_named_examples(self):
        plz = "1011" + "5"
        cases = [
            ("Musterstrasse 12a", "<address>", 1),
            ("An der Linde 2", "<address>", 1),
            ("Zum See 6", "<address>", 1),
            ("1234" + "5 BERLIN", "<address>", 1),
            ("1234" + "5 Bad Homburg", "<address>", 1),
            ("1234" + "5 Halle/Saale", "<address>", 1),
            ("Ku'damm 3", "Ku'<address>", 1),
            ("7909" + "8 Freiburg im Breisgau", "<address>", 1),
            ("9154" + "1 Rothenburg ob der Tauber", "<address>", 1),
            ("PLZ ist " + plz + " Berlin im Brief.", "PLZ ist <address>.", 1),
            ("8033" + "1 München1", None, 0),
            (plz + " Berlin\u00e9", None, 0),
            ("Musterstraße 12ä", None, 0),
            ("äMusterstraße 12", None, 0),
            ("Am Am Am Am 1", "<address>", 1),
            ("Am 1", None, 0),
            ("Aaa Aaa Aaa 1", None, 0),
        ]
        for text, want, n in cases:
            with self.subTest(text=text):
                got = tc._mask_addresses(text)
                self.assertEqual(got, (text if want is None else want, n))
                self.assertEqual(got, ref_addresses(text))

    def test_long_runs(self):
        for text in (" ".join(["Am"] * 40) + " 1", " ".join(["Aaa"] * 40) + " 1",
                     "-".join(["Aaa"] * 40) + " Weg 1", "1" + "2345 " + "-".join(["Aaa"] * 40),
                     " ".join(["Aaa"] * 20 + ["aaa"] * 20) + " Aweg 1 Bweg 2"):
            with self.subTest(text=text[:20]):
                self.check(text)
        self.assertEqual(tc._mask_addresses(" ".join(["Am"] * 3000) + " 1"), ("<address>", 1))
        self.assertEqual(tc._mask_addresses(" ".join(["Aaa"] * 3000) + " 1")[1], 0)

    def test_random_soup_matches_reference(self):
        rng = random.Random(20261002)
        for _ in range(3000):
            text = soup(rng)
            with self.subTest(text=text):
                self.check(text)

    def test_random_structured_matches_reference(self):
        rng = random.Random(1002)
        hits = 0
        for _ in range(4000):
            text = structured(rng)
            with self.subTest(text=text):
                got = tc._mask_addresses(text)
                self.assertEqual(got, ref_addresses(text), repr(text))
                hits += got[1] > 0
        self.assertGreater(hits, 1000)  # the generator must really exercise the grammars

    def test_grammars_match_spec_strings(self):
        """ts_common builds its automata from these strings; they must equal the spec's."""
        self.assertEqual("(?:" + tc._STREET_PREFIX_PY + ")\\s+" + tc._STREET_NUMBER_PY, PY_STREET)
        self.assertEqual("(?:" + tc._STREET_PREFIX_GO + ")\\s+" + tc._STREET_NUMBER_GO, GO_STREET)
        self.assertEqual(tc._PLZ_PY, PY_PLZ)
        self.assertEqual(tc._PLZ_GO, GO_PLZ)


class TestCharacterPredicates(unittest.TestCase):
    ALL = "".join(chr(c) for c in range(0x110000) if not 0xD800 <= c <= 0xDFFF)

    def test_word_predicate_is_python_w(self):
        self.assertEqual(re.findall(r"\w", self.ALL), [c for c in self.ALL if tc._is_word(c)])

    def test_no_composition_ends_in_ascii(self):
        """The NFC view treats every ASCII character as a segment boundary; that holds
        if no canonical pair composes with an ASCII second character."""
        for c in self.ALL:
            d = unicodedata.decomposition(c)
            if d and not d.startswith("<"):
                parts = [chr(int(x, 16)) for x in d.split()]
                if len(parts) == 2 and parts[1] < "\x80":
                    self.assertNotEqual(unicodedata.normalize("NFC", "".join(parts)), c, repr(c))

    def test_nfc_qc_maybe_starters(self):
        """ts_common ships the class-0 NFC_Quick_Check=Maybe characters as a literal
        (spec A1); it must equal the set derived from unicodedata here (93 in 16.0.0)."""
        got, want = tc._NFC_QC_MAYBE_STARTERS, nfc_data()["maybe"]
        self.assertEqual(["U+%04X" % ord(c) for c in sorted(got - want)], [], "in ts_common only")
        self.assertEqual(["U+%04X" % ord(c) for c in sorted(want - got)], [], "missing in ts_common")

    def test_boundaries_do_not_interact(self):
        """Nothing before a boundary character composes or reorders with it or with what
        follows it: for every character with a canonical mapping, a combining class or
        quick check Maybe, after prefixes that end in a starter which composes with
        something (or in marks), NFC(p + c + s) == NFC(p) + NFC(c + s) if c is a boundary."""
        data = nfc_data()
        prefixes = sorted(data["partners"]) + ["a", "a\u0301", "\u1100", "\uac00", "\u0f71", "x\u0f74"]
        suffixes = ["", "\u0301", "\u0f71", "\u1161"]
        nfc = unicodedata.normalize
        checked = 0
        for c in sorted(data["interesting"]):
            if not tc._nfc_boundary(c):
                continue
            for p in prefixes:
                for s in suffixes:
                    self.assertEqual(nfc("NFC", p + c + s), nfc("NFC", p) + nfc("NFC", c + s),
                                     "U+%04X after %r" % (ord(c), p))
                    checked += 1
        self.assertGreater(checked, 10000)


_NFC_DATA = {}


def nfc_data():
    """Facts derived from unicodedata alone (independent of ts_common):
    maybe       class-0 characters with NFC_Quick_Check=Maybe: the second character of
                every primary composite (a canonical pair NFC keeps), the Hangul vowels
                and trailing consonants (they compose with a syllable before them), and
                every primary composite whose full decomposition starts with one of
                those (Unicode 16 vowel signs), restricted to combining class 0;
    partners    first characters of the primary composites whose second is in maybe;
    no          class-0 characters NFC changes (quick check No);
    interesting characters with a canonical mapping, a class or in maybe."""
    if not _NFC_DATA:
        nfc = unicodedata.normalize
        raw = {}
        for cp in range(0x110000):
            if 0xD800 <= cp <= 0xDFFF:
                continue
            d = unicodedata.decomposition(chr(cp))
            if d and not d.startswith("<"):
                raw[chr(cp)] = [chr(int(x, 16)) for x in d.split()]
        pairs = [(m[0], m[1], c) for c, m in raw.items() if len(m) == 2 and nfc("NFC", c) == c]
        jamo = [chr(cp) for cp in range(0x1100, 0x1200)]
        second = {b for _, b, _ in pairs}
        second |= {j for j in jamo if len(nfc("NFC", "\u1100" + j)) == 1}  # vowels
        second |= {j for j in jamo if len(nfc("NFC", "\uac00" + j)) == 1}  # trailing consonants

        def first(c):
            while c in raw:
                c = raw[c][0]
            return c

        maybe = second | {c for _, _, c in pairs if first(c) in second}
        maybe = frozenset(c for c in maybe if unicodedata.combining(c) == 0)
        _NFC_DATA.update(
            maybe=maybe,
            partners={a for a, b, _ in pairs if b in maybe} | {"\u1100", "\uac00"},
            no=frozenset(c for c in raw if unicodedata.combining(c) == 0 and nfc("NFC", c) != c),
            interesting=set(raw) | maybe | {c for c in TestCharacterPredicates.ALL if unicodedata.combining(c)},
        )
    return _NFC_DATA


def ref_segment_starts(text):
    """Spec A1, plainly: a segment starts at 0 and before every character with
    combining class 0 that is neither quick check Maybe nor changed by NFC (No)."""
    maybe = nfc_data()["maybe"]
    return [k for k, c in enumerate(text)
            if k == 0 or (unicodedata.combining(c) == 0 and c not in maybe
                          and unicodedata.normalize("NFC", c) == c)]


POOL = ["a", "e", "A", "o", " ", ".", "J", "\u0301", "\u0308", "\u0323", "\u0327", "\u00e9", "\u00c5",
        "\u212b", "\u2126", "\u1100", "\u1161", "\u11a8", "\uac00", "\u0b47", "\u0b3e", "\u0f73",
        "\u0f71", "\u0f72", "\u0f74", "\u0344", "ä", "\u1e9e", "\u0130", "\u0131", "\u3099",
        "\u304b", "\u05d0", "\u05b7", "\u0915", "\u093c", "\U0001f600", "\u200d", "x", "\u0338",
        "<", "="]
MARKS = ["\u0301", "\u0308", "\u0323", "\u0327", "\u05b7", "\u093c", "\u3099", "\u0f71", "\u0f72",
         "\u0f74", "\u0f80", "\u0338", "\u0345", "\u0340", "\u0344", "\U0001133c"]
QC_NO_STARTERS = ["\u0f73", "\u0f75", "\u0f81", "\u2126", "\u212b", "\uf900", "\u2000", "\u037e"]


def pool_groups():
    """Character groups for random strings: the fixed pool, every quick check Maybe
    starter, their composition partners, quick check No starters, marks."""
    data = nfc_data()
    return [POOL, sorted(data["maybe"]), sorted(data["partners"]), QC_NO_STARTERS, MARKS]


class TestNfcView(unittest.TestCase):
    def view_starts(self, text):
        # A block is one segment, or a stretch of ASCII characters between non-ASCII runs
        # (each one a segment). Superseded 2026-10-02 (spec A8): this helper read every
        # identity block as one-character segments; an unchanged segment of several
        # characters is an identity block now, so the ASCII test tells the two apart.
        copy, starts, blocks = tc._nfc_view(text)
        seg = []
        for cs, os_, oe, ident in blocks:
            seg.extend(range(os_, oe) if text[os_:oe].isascii() else [os_])
        return copy, seg, blocks

    def test_segments_match_reference(self):
        """Segments as spec A1 defines them, and the copy is NFC of the whole text."""
        rng = random.Random(7)
        groups = pool_groups()
        for _ in range(6000):
            text = "".join(rng.choice(rng.choice(groups)) for _ in range(rng.randint(0, 12)))
            with self.subTest(text=text):
                copy, seg, blocks = self.view_starts(text)
                ref = ref_segment_starts(text)
                self.assertEqual(seg, ref)
                bounds = ref + [len(text)]
                self.assertEqual(copy, "".join(unicodedata.normalize("NFC", text[a:b])
                                               for a, b in zip(bounds, bounds[1:])))
                self.assertEqual(copy, unicodedata.normalize("NFC", text))
                # Spec A8: a block maps one-to-one exactly when NFC leaves it unchanged,
                # so an NFC text has identity blocks only and the segment path gives what
                # the fast path gives.
                for cs, os_, oe, ident in blocks:
                    self.assertEqual(ident, unicodedata.normalize("NFC", text[os_:oe]) == text[os_:oe])
                if unicodedata.is_normalized("NFC", text):
                    self.assertTrue(all(blk[3] for blk in blocks))

    # "a" U+0F74 U+0F73 is ONE segment: U+0F74 has class 132 and U+0F73 is a starter
    # with quick check No (it decomposes to U+0F71 U+0F72), so neither starts a segment.
    # The segment's NFC is "a" U+0F71 U+0F72 U+0F74 (U+0F71, U+0F72 reorder before U+0F74).
    SEG = "a\u0f74\u0f73"

    def test_segment_example(self):
        copy, seg, blocks = self.view_starts("x " + self.SEG + " y")
        self.assertEqual(copy, "x a\u0f71\u0f72\u0f74 y")
        self.assertEqual(seg, [0, 1, 2, 5, 6])

    def test_widen(self):
        # A name "a" matches only the first character of the segment (U+0F71 is no word
        # character, so \b holds after "a") and widens to the whole segment.
        rx = re.compile(r"(?i)\b(?:a)\b")
        self.assertEqual(tc._mask_names(rx, "x " + self.SEG + " y", "<name>"), ("x <name> y", 1))

    def test_overlapping_widened_spans_merge(self):
        # Two matches in the copy ("a" and U+0F72) lie in the same segment: both widen to
        # it, the spans overlap and become one replacement; both are counted.
        rx = re.compile("a|\u0f72")
        self.assertEqual(tc._mask_names(rx, "x " + self.SEG + " y", "<N>"), ("x <N> y", 2))

    def test_adjacent_spans_stay_separate(self):
        # e U+0301 x: segments [e U+0301] and [x]. Matches on the composed U+00E9 and on
        # x map to adjacent, not overlapping spans: two replacements.
        rx = re.compile("\u00e9|x")
        text = "e\u0301x"
        self.assertEqual(tc._nfc_view(text)[0], "\u00e9x")
        self.assertEqual(tc._mask_names(rx, text, "<N>"), ("<N><N>", 2))

    def test_hangul_vowels_stay_in_segment(self):
        # Spec A1: Hangul vowels are quick check Maybe and start no segment, so L V V x
        # has the segments [L V V] and [x]. The match on the composed syllable and the
        # one on "V x" both touch segment [L V V]: their spans overlap and merge into one
        # replacement, both are counted. (The rule before A1 split [L V] [V] [x] and
        # gave two replacements.)
        rx = re.compile("\uac00|\u1161x")
        text = "\u1100\u1161\u1161x"
        self.assertEqual(tc._nfc_view(text)[0], "\uac00\u1161x")
        self.assertEqual(tc._mask_names(rx, text, "<N>"), ("<N>", 2))

    def test_qc_maybe_starter_in_unchanged_segment(self):
        # Spec A1: U+0B3E (quick check Maybe, no word character) starts no segment, so
        # "b" U+0B3E is one segment. Spec A8: NFC leaves that segment unchanged, so the
        # match "ab" maps one-to-one and U+0B3E stays, although the text is not NFC
        # (e U+0301). Superseded 2026-10-02 (A8): this test was test_qc_maybe_starter_widens
        # and expected "<N> e U+0301" (the match widened over U+0B3E), while the same span
        # in an NFC text gave "<N>" U+0B3E. U+0B47 U+0B3E composes to U+0B4B: a segment NFC
        # changes, and a match edge inside it still widens (TestNamesLocalWidening).
        rx = re.compile(r"(?i)\bab\b")
        self.assertEqual(tc._mask_names(rx, "ab\u0b3e e\u0301", "<N>"), ("<N>\u0b3e e\u0301", 1))
        self.assertEqual(tc._mask_names(rx, "ab\u0b3e e", "<N>"), ("<N>\u0b3e e", 1))
        self.assertEqual(tc._nfc_view("\u0b47\u0b3e")[0], "\u0b4b")

    def test_long_runs_are_linear(self):
        """Spec A1 replaced a quadratic segment rule (35 s for "a" + U+0F73 x 16000);
        finding the segments is now one test per character (measured about 10 ms)."""
        for text in ("a" + "\u0f73" * 16000, "\u1100" + "\u1161" * 16000,
                     "\u1100\u1161\u11a8" * 16000, "a" + "\u0301" * 16000, "e\u0301" * 16000):
            with self.subTest(text=text[:3]):
                t0 = time.perf_counter()
                copy = tc._nfc_view(text)[0]
                self.assertLess(time.perf_counter() - t0, 2.0)
                self.assertEqual(copy, unicodedata.normalize("NFC", text))


class TestNamesNfcAndTurkish(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        fd, cls.path = tempfile.mkstemp(prefix="typesafe-unicode-names-", suffix=".txt")
        with os.fdopen(fd, "w", encoding="utf-8") as f:
            f.write("Jos\u00e9\nZoe\u0308\n\u00c5ngström\nI\u015eIK\nilker\nO\u0308\n")
        cls.prev = os.environ.get("TYPESAFE_NAMES_FILE")
        os.environ["TYPESAFE_NAMES_FILE"] = cls.path
        tc.get_name_regex(cls.path)

    @classmethod
    def tearDownClass(cls):
        if cls.prev is None:
            os.environ.pop("TYPESAFE_NAMES_FILE", None)
        else:
            os.environ["TYPESAFE_NAMES_FILE"] = cls.prev
        os.remove(cls.path)

    def test_terms_are_nfc(self):
        self.assertIn("Zo\u00eb", tc.load_names(self.path))
        self.assertNotIn("Zoe\u0308", tc.load_names(self.path))

    def test_single_letter_after_nfc_is_no_name(self):
        # Spec A2: NFC first, then at least two code points. "O" + U+0308 is two code
        # points as written but one letter (U+00D6) after NFC: no name.
        terms = tc.load_names(self.path)
        self.assertNotIn("\u00d6", terms)
        self.assertNotIn("O\u0308", terms)
        for text in ("\u00d6 kommt", "O\u0308 kommt"):
            with self.subTest(text=text):
                self.assertEqual(tc.mask_detail(text)[1]["name"], 0)

    def test_cases(self):
        cases = [
            ("Jos\u00e9 kommt", "<name> kommt", 1),           # NFC text, NFC name
            ("Jose\u0301 kommt", "<name> kommt", 1),          # NFD text
            ("Zo\u00eb und Zoe\u0308", "<name> und <name>", 2),  # NFD name in the file
            ("\u212bngström", "<name>", 1),              # U+212B ANGSTROM SIGN
            ("A\u030angstro\u0308m", "<name>", 1),            # fully decomposed
            ("\u0131\u015f\u0131k", "<name>", 1),             # Turkish dotless i
            ("I\u015f\u0131k", "<name>", 1),
            ("\u0130lker", "<name>", 1),                     # Turkish dotted capital I
            ("ILKER", "<name>", 1),
            ("Jose\u0301x", "Jose\u0301x", 0),               # no boundary on the NFC copy
            ("xJose\u0301 ä", "xJose\u0301 ä", 0),
            # Spec A1: U+0B3E (quick check Maybe) starts no segment, so U+00E9 U+0B3E is
            # one segment; NFC leaves it unchanged, so (spec A8) the match maps one-to-one
            # and U+0B3E stays, in NFC text (fast path) as in text that is not NFC
            # elsewhere (Zoe + U+0308). Superseded 2026-10-02 (A8): the first row expected
            # "<name> und <name>" (widened over U+0B3E because the text was not NFC).
            ("Jos\u00e9\u0b3e und Zoe\u0308", "<name>\u0b3e und <name>", 2),
            ("Jos\u00e9\u0b3e und Zo\u00eb", "<name>\u0b3e und <name>", 2),
        ]
        for text, want, n in cases:
            with self.subTest(text=text):
                masked, counts = tc.mask_detail(text)
                self.assertEqual((masked, counts["name"]), (want, n))

    def test_outside_spans_unchanged(self):
        text = "e\u0301 Jose\u0301 A\u030a"
        masked, counts = tc.mask_detail(text)
        self.assertEqual(masked, "e\u0301 <name> A\u030a")


# --- Spec A8: widening is local ---------------------------------------------------------
# Synthetic names, multilingual (the scripts of the golden corpus), two stored in NFD.
A8_NAMES = ["Max Mustermann", "Erika", "Jose\u0301", "Zo\u00eb", "Kowalczyk", "\u0130pek",
            "\u039d\u03af\u03ba\u03bf\u03c2", "\u041e\u043b\u0435\u0433", "\u0633\u0645\u064a\u0631",
            "\u05e0\u05d5\u05e2\u05d4", "\u0905\u0928\u093f\u0932", "\u5c71\u7530", "\ud55c\ubcc4"]


def a8_name_forms():
    """Every name as written, in NFC, NFD, upper and lower case, and with the Kelvin sign."""
    nfc = unicodedata.normalize
    forms = set()
    for line in A8_NAMES:
        for n in [line] + line.split():
            forms.update({n, nfc("NFC", n), nfc("NFD", n), n.upper(), n.lower()})
    forms.update({"\u212aowalczyk", "\u212aOWALCZYK"})
    return sorted(forms)


# Material for T: no digit, no "@", "=" or ":" and no keyword, so only the names step
# can mask anything (asserted on every sample).
A8_SCRIPTS = ["\u0396\u03c9\u03ae", "\u03c4\u03ad\u03bb\u03bf\u03c2", "\u041f\u0440\u0438\u0432\u0435\u0442",
              "\u0645\u0631\u062d\u0628\u0627", "\u05e9\u05dc\u05d5\u05dd", "\u0928\u092e\u0938\u094d\u0924\u0947",
              "\u6771\u4eac\u3067", "\ud55c\uad6d", "\U0001f600", "\U0001f44d\U0001f3fd", "und", "Herr", "x", "a", "e"]
A8_JAMO = ["\u1100", "\u1112", "\u1161", "\u1175", "\u11a8", "\u11c2", "\uac00", "\ud55c"]
A8_JOINERS = ["\u200d", "\u200b", "\u200c", "\ufeff"]
A8_QC_NO = QC_NO_STARTERS + ["\u212a", "\u0340", "\u0341", "\u0343", "\u0374", "\u0387", "\u1f71"]
A8_SEPARATORS = [" ", " ", " ", "", "-", ".", ",", "'", "\u00a0", "\u3000", "\n"]
# Suffixes: X is NFC, Y is not; neither holds a name or anything another step masks.
A8_NFC_PIECES = ["\u00e9", "\u00fc", "ä", "\u00c5", "\u65e5\u672c", "\ud55c", "\u0416", "\u03b1\u03b2", "!",
                 ".", "\U0001f600", "\u0b4b", "\u0915\u093f", "-", "\u2014", " "]
A8_NON_NFC_PIECES = ["e\u0301", "A\u030a", "\u212b", "\u2126", "\u1100\u1161", "o\u0308", "\uf900",
                     "\u0b47\u0b3e", "\u0f73", "\u212a"]


def a8_texts(rng, forms, groups, kept):
    """T: names (often directly followed by marks, quick check Maybe or No starters,
    joiners; most often by one that NFC keeps after a letter, the case A8 is about),
    scripts, jamo and separators."""
    out = []
    for _ in range(rng.randint(1, 5)):
        r = rng.random()
        if r < 0.45:
            out.append(rng.choice(forms))
            r2 = rng.random()
            if r2 < 0.35:
                out.append(rng.choice(kept))
            elif r2 < 0.7:
                out.append("".join(rng.choice(rng.choice(groups)) for _ in range(rng.randint(1, 2))))
        elif r < 0.65:
            out.append(rng.choice(A8_SCRIPTS))
        elif r < 0.8:
            out.append("".join(rng.choice(rng.choice(groups)) for _ in range(rng.randint(1, 3))))
        else:
            out.append(rng.choice(A8_JAMO))
        out.append(rng.choice(A8_SEPARATORS))
    return "".join(out).strip(" ")


def a8_suffix(rng, nfc_only):
    while True:
        pieces = [rng.choice(A8_NFC_PIECES) for _ in range(rng.randint(1, 3))]
        if not nfc_only:
            pieces.insert(rng.randint(0, len(pieces)), rng.choice(A8_NON_NFC_PIECES))
        s = "".join(pieces).strip(" ")
        if s and unicodedata.is_normalized("NFC", s) == nfc_only:
            return s


class TestNamesLocalWidening(unittest.TestCase):
    """Spec A8: a match edge widens to its segment edge only inside a segment that NFC
    changes; inside an unchanged segment offsets map one-to-one. So the masking of a
    span depends on its own segments only, and the NFC fast path equals the segment path."""

    @classmethod
    def setUpClass(cls):
        fd, cls.path = tempfile.mkstemp(prefix="typesafe-a8-names-", suffix=".txt")
        with os.fdopen(fd, "w", encoding="utf-8") as f:
            f.write("".join(n + "\n" for n in A8_NAMES))
        cls.prev = os.environ.get("TYPESAFE_NAMES_FILE")
        os.environ["TYPESAFE_NAMES_FILE"] = cls.path
        cls.rx = tc.get_name_regex(cls.path)

    @classmethod
    def tearDownClass(cls):
        if cls.prev is None:
            os.environ.pop("TYPESAFE_NAMES_FILE", None)
        else:
            os.environ["TYPESAFE_NAMES_FILE"] = cls.prev
        os.remove(cls.path)

    def test_masking_of_t_ignores_the_rest_of_the_text(self):
        """Property: for random T, an NFC suffix X and a suffix Y that is not NFC (after
        a space), mask(T + " " + X) and mask(T + " " + Y) are mask(T) followed by the
        untouched suffix, with the same counts. Before A8 this failed whenever T was NFC
        and a name match ended inside a segment of several characters that NFC keeps
        ("Max" U+0B3E, "Max" U+0301): T + " " + Y took the segment path and widened."""
        rng = random.Random(20261002)
        data = nfc_data()
        groups = [MARKS, sorted(data["maybe"]), A8_QC_NO, A8_JOINERS, A8_JAMO]
        kept = MARKS + sorted(c for c in data["maybe"] if not is_word(c))  # no word characters
        forms = a8_name_forms()
        nfc_t = sensitive = with_names = 0
        bad = []
        n_samples = 3000
        for _ in range(n_samples):
            t = a8_texts(rng, forms, groups, kept)
            x, y = a8_suffix(rng, True), a8_suffix(rng, False)
            t_nfc = unicodedata.is_normalized("NFC", t)
            alone, counts = tc.mask_detail(t)
            self.assertEqual({k: v for k, v in counts.items() if k != "name"},
                             {k: 0 for k in counts if k != "name"}, ascii(t))
            nfc_t += t_nfc
            with_names += counts["name"] > 0
            if t_nfc:
                # A name edge inside a segment of several characters: the pre-A8 code
                # widened it on the segment path only (reference segments, spec A1).
                cuts = set(ref_segment_starts(t)) | {len(t)}
                sensitive += any(m.start() not in cuts or m.end() not in cuts for m in self.rx.finditer(t))
            for suffix, suffix_nfc in ((x, True), (y, False)):
                text = t + " " + suffix
                self.assertEqual(unicodedata.is_normalized("NFC", text), t_nfc and suffix_nfc)
                masked, got = tc.mask_detail(text)
                if (masked[len(masked) - len(suffix) - 1:] != " " + suffix
                        or masked[:len(masked) - len(suffix) - 1] != alone or got != counts):
                    bad.append("T=%s suffix=%s: alone %s, got %s" % (ascii(t), ascii(suffix), ascii(alone), ascii(masked)))
        # Not vacuous: NFC texts, name hits and edges inside unchanged segments occur.
        self.assertGreater(nfc_t, n_samples // 4)
        self.assertGreater(with_names, n_samples // 4)
        self.assertGreater(sensitive, 200)
        self.assertEqual(bad, [], "%d of %d comparisons differ (%d samples, %d with NFC T, %d with a name "
                         "edge inside a segment of an NFC T); the first: %s"
                         % (len(bad), 2 * n_samples, n_samples, nfc_t, sensitive, " | ".join(bad[:3])))

    def test_every_maybe_starter_after_a_name(self):
        """"Max" + m for every quick check Maybe starter m (spec A1: m starts no segment;
        "x" + m is a segment NFC keeps): one-to-one in NFC text and in text that is not
        NFC elsewhere. "<name>" + m if m is no word character; no match if it is one
        (Hangul vowels and trailing consonants, Kirat Rai), the word goes on."""
        maybe = sorted(nfc_data()["maybe"])
        self.assertGreater(len(maybe), 80)
        for m in maybe:
            self.assertEqual(unicodedata.normalize("NFC", "x" + m), "x" + m)
            head, n = ("Max" + m, 0) if is_word(m) else ("<name>" + m, 1)
            for tail, tail_masked, tn in ((" kommt", " kommt", 0), (" kommt, Jose\u0301", " kommt, <name>", 1)):
                with self.subTest(m="U+%04X" % ord(m), tail=tail):
                    masked, counts = tc.mask_detail("Max" + m + tail)
                    self.assertEqual((masked, counts["name"]), (head + tail_masked, n + tn))

    def test_pinned(self):
        cases = [
            # "Max" U+0B3E (spec A8): the same in NFC text and in text not NFC elsewhere.
            ("Max\u0b3e kommt", "<name>\u0b3e kommt", 1),
            ("Max\u0b3e kommt, Jose\u0301", "<name>\u0b3e kommt, <name>", 2),
            ("Jose\u0301 und Max\u0b3e", "<name> und <name>\u0b3e", 2),
            ("Erika\u09be \u212b", "<name>\u09be \u212b", 1),
            # A mark NFC keeps (there is no precomposed x + U+0301): one-to-one as well.
            ("Max\u0301 kommt", "<name>\u0301 kommt", 1),
            ("Max\u0301 kommt, Jose\u0301", "<name>\u0301 kommt, <name>", 2),
            # Joiners are boundaries: never widened.
            ("Max\u200dMustermann e\u0301", "<name>\u200d<name> e\u0301", 2),
            # A segment NFC changes still widens (spec A8): e U+0301 U+0B3E is one
            # segment (U+0B3E is quick check Maybe), NFC makes it U+00E9 U+0B3E.
            ("Jose\u0301\u0b3e kommt", "<name> kommt", 1),
            ("Jos\u00e9\u0b3e kommt", "<name>\u0b3e kommt", 1),
            # U+0F73 decomposes (quick check No): "x" U+0F74 U+0F73 changes, widens.
            ("Max\u0f74\u0f73 kommt", "<name> kommt", 1),
            # Kelvin sign: the space before it and U+212A form one segment that NFC
            # changes (" K"), so the match on "Kowalczyk" widens over the space: it masks
            # more and cuts no word (spec A8 keeps this).
            ("Herr \u212aowalczyk kommt", "Herr<name> kommt", 1),
            ("Herr \u212aOWALCZYK, Max\u0b3e", "Herr<name>, <name>\u0b3e", 2),
            ("Herr Kowalczyk kommt", "Herr <name> kommt", 1),
        ]
        for text, want, n in cases:
            with self.subTest(text=text):
                masked, counts = tc.mask_detail(text)
                self.assertEqual((masked, counts["name"]), (want, n))


# --- NFC union of the email and address passes (user decision "Union", 2026-10-02) --------
DIA, ACUTE, DOT_BELOW, KELVIN = chr(0x308), chr(0x301), chr(0x323), chr(0x212A)
PLZ_A, PLZ_B = "8033" + "1", "1234" + "5"  # built from parts: no line here is a postcode shape
HERE = os.path.dirname(os.path.abspath(__file__))
EMAIL_POOL = (list("abcABC019_") + [".", "+", "-", AT, AT, "ä", "ß", " ", "a" + DIA, "u" + DIA,
                                     "e" + ACUTE, DIA, ACUTE, chr(0xE9), KELVIN, chr(0x660), chr(0xA0)])


def nfd_variant(rng, text):
    """text with each character that has a canonical decomposition decomposed with
    probability 0.6, and now and then a combining mark after a letter."""
    out = []
    for c in text:
        d = unicodedata.normalize("NFD", c)
        out.append(d if d != c and rng.random() < 0.6 else c)
        if c.isalpha() and rng.random() < 0.04:
            out.append(rng.choice([DIA, ACUTE, DOT_BELOW]))
    return "".join(out)


def golden_and_parity_texts():
    texts = []
    for name in ("mask-golden.json", "mask-parity-cases.json"):
        with open(os.path.join(HERE, name), "r", encoding="utf-8") as f:
            texts += [case["text"] for case in json.load(f)["cases"]]
    return texts


def text_only(text, find, repl):
    """What a pass gave before the union: find's spans on the text alone, replaced."""
    out, last = [], 0
    for a, b in find(text):
        out += [text[last:a], repl]
        last = b
    out.append(text[last:])
    return "".join(out)


class TestNfcUnionPasses(unittest.TestCase):
    """User decision "Union" (2026-10-02, README.md "Datenschutz & Maskierung"): the email
    pass and both address passes (street, then postcode on the street output) each
    mask the union of their spans on the text and, if the text is not NFC, on a fresh
    NFC view mapped back (widened like names, spec A8); spans that overlap strictly
    merge, adjacent ones stay apart, and each merged span counts once. Text in NFC
    gives exactly what it gave before; the union never masks less than the text alone."""

    def test_examples(self):
        cases = [
            # (pass, text, masked, count); the comment says what the pass gave before
            # Domain with a decomposed umlaut: no match on the text.
            ("email", "x local" + AT + "mu" + DIA + "nchen.example y", "x <email> y", 1),
            # Local part: the text matched from "ller" only.
            ("email", "x ju" + DIA + "rgen.mu" + DIA + "ller" + AT + "example.org y", "x <email> y", 1),
            # Right after the TLD: the text left the mark, "<email>" + U+0308.
            ("email", "info" + AT + "example.deu" + DIA, "<email>", 1),
            # A mark NFC keeps after an email ("e" + U+0301 composes): "<email>" + U+0301.
            ("email", "test.user" + AT + "mail.example" + ACUTE, "<email>", 1),
            # PLZ + NFD place name (both PLZ branches): the text matched "... Mu" only.
            ("address", "Adresse: " + PLZ_A + " Mu" + DIA + "nchen, fertig", "Adresse: <address>, fertig", 1),
            ("address", PLZ_B + " Bad Du" + DIA + "rkheim", "<address>", 1),  # Python branch
            ("address", PLZ_B + " MU" + DIA + "NCHEN", "<address>", 1),  # Go branch
            # Precomposed place name, then a combining acute: "n" + U+0301 composes to
            # U+0144, so the copy has no match, and the span on the text survives.
            ("address", PLZ_A + " München" + ACUTE, "<address>" + ACUTE, 1),
            # NFD street name with number: the text matched "hlenweg 3" only.
            ("address", "Mu" + DIA + "hlenweg 3", "<address>", 1),
            ("address", "Am Mu" + DIA + "hlbach 2", "<address>", 1),  # Go branch; text: none
            ("address", KELVIN + "önigsweg 4", "<address>", 1),  # NFC turns U+212A into K
            ("address", "Mu" + DIA + "hlenweg 3, " + PLZ_B + " Mu" + DIA + "nster", "<address>, <address>", 2),
            # A mark the house number keeps: the copy matches less, the text's span stays.
            ("address", "Musterstrasse 1" + DIA, "<address>" + DIA, 1),
        ]
        for step, text, want, n in cases:
            with self.subTest(text=ascii(text)):
                if step == "email":
                    got, ref = tc._mask_emails(text, "<email>"), ref_emails(text)
                else:
                    got, ref = tc._mask_addresses(text), ref_addresses(text)
                self.assertEqual(got, (want, n))
                self.assertEqual(got, ref)
                masked, counts = tc.mask_detail(text)
                self.assertEqual((masked, counts[step]), (want, n))

    def test_count_is_one_per_merged_span(self):
        # The text has two email spans, one up to the "e" before U+0308 and one from
        # the "f" after it (U+0308 is no word character); the copy has one, through
        # U+00EB and "f", that overlaps both: one merged span, counted once (it counted
        # 2 before the union).
        text = "ab" + AT + "cd.e" + DIA + "f" + AT + "gh.ij"
        self.assertEqual(len(tc._email_spans(text)), 2)
        self.assertEqual(tc._mask_emails(text, "<email>"), ("<email>", 1))
        self.assertEqual(ref_emails(text), ("<email>", 1))
        # One span on the text and one on the copy, overlapping: one replacement.
        text = "Adresse: " + PLZ_A + " Mu" + DIA + "nchen, fertig"
        self.assertEqual(tc._mask_addresses(text), ("Adresse: <address>, fertig", 1))
        # The merge: strict overlap only, in any input order; adjacent spans stay apart.
        self.assertEqual(tc._merge_spans([(2, 4), (0, 2)]), [[0, 2], [2, 4]])
        self.assertEqual(tc._merge_spans([(3, 6), (0, 4), (1, 2)]), [[0, 6]])
        self.assertEqual(tc._merge_spans([(0, 5), (0, 3), (4, 9), (9, 10)]), [[0, 9], [9, 10]])

    def test_nfc_text_takes_the_fast_path(self):
        """Text in NFC never builds a view and gives exactly the spans on the text: the
        email pass equals the spec pattern's subn, the address passes the brute-force
        reference on the text alone (what they gave before the union)."""
        rng = random.Random(1002)
        addresses = [t for t in (structured(rng) for _ in range(1500)) if unicodedata.is_normalized("NFC", t)]
        emails = [t for t in golden_and_parity_texts() if unicodedata.is_normalized("NFC", t)]
        self.assertGreater(len(addresses), 1000)
        self.assertGreater(len(emails), 5000)
        with mock.patch.object(tc, "_nfc_view", side_effect=AssertionError("view built for NFC text")):
            for text in emails:
                self.assertEqual(tc._mask_emails(text, "<email>"), REF_EMAIL.subn("<email>", text), ascii(text))
            for text in addresses:
                self.assertEqual(tc._mask_addresses(text), ref_addresses(text), ascii(text))

    def test_emails_match_reference(self):
        """Every golden and parity text, and a seeded soup with decomposed letters, marks
        NFC composes or keeps, U+00E9 and the Kelvin sign."""
        rng = random.Random(20261002)
        texts = golden_and_parity_texts()
        texts += ["".join(rng.choice(EMAIL_POOL) for _ in range(rng.randint(0, 40))) for _ in range(4000)]
        not_nfc = changed = 0
        for text in texts:
            got = tc._mask_emails(text, "<email>")
            self.assertEqual(got, ref_emails(text), ascii(text))
            if not unicodedata.is_normalized("NFC", text):
                not_nfc += 1
                changed += got[0] != text_only(text, lambda t: [m.span() for m in REF_EMAIL.finditer(t)], "<email>")
        self.assertGreater(not_nfc, 2000)  # not vacuous: many texts take the view
        self.assertGreater(changed, 100)  # and the union often differs from the text alone

    def test_addresses_match_reference(self):
        """The address generators of TestAddressUnion, with letters decomposed and marks
        inserted (nfd_variant)."""
        rng = random.Random(20261003)
        not_nfc = changed = 0
        for k in range(3000):
            text = nfd_variant(rng, structured(rng) if k % 2 else soup(rng))
            with self.subTest(text=ascii(text)):
                got = tc._mask_addresses(text)
                self.assertEqual(got, ref_addresses(text))
            if not unicodedata.is_normalized("NFC", text):
                not_nfc += 1
                t1 = text_only(text, tc._street_spans, "<address>")
                changed += got[0] != text_only(t1, tc._postcode_spans, "<address>")
        self.assertGreater(not_nfc, 1000)
        self.assertGreater(changed, 100)

    def test_union_passes_are_linear(self):
        """Many NFD local parts and domains, NFD street names and PLZ + NFD place names,
        sizes doubling up to a few hundred thousand characters; a pass that went
        quadratic in the text or in the number of spans would take far longer."""
        shapes = [
            ("emails", lambda n: ("ju" + DIA + "rgen" + AT + "mu" + DIA + "nchen.example ") * n),
            ("local_run", lambda n: ("a" + DIA) * n + AT + ("b" + DIA) * n + ".example"),
            ("marks", lambda n: ("x" + ACUTE) * (8 * n)),
            ("streets", lambda n: ("Mu" + DIA + "hlenweg 3, ") * n),
            ("go_streets", lambda n: ("Am Mu" + DIA + "hlbach 2 ") * n),
            ("postcodes", lambda n: (PLZ_A + " Mu" + DIA + "nchen, ") * n),
        ]
        for name, make in shapes:
            for n in (2000, 4000, 8000, 16000):
                text = make(n)
                with self.subTest(shape=name, n=n):
                    t0 = time.perf_counter()
                    if name in ("emails", "local_run", "marks"):
                        tc._mask_emails(text, "<email>")
                    else:
                        tc._mask_addresses(text)
                    self.assertLess(time.perf_counter() - t0, 2.0)


class TestSecretAndPhoneRules(unittest.TestCase):
    def test_bearer(self):
        cases = [
            ('"Authorization": "Bearer abc"', '"Authorization": <redacted> <redacted>', 2),
            ("äBearer x", "äBearer <redacted>", 1),
            ("\u216bBearer x", "\u216bBearer <redacted>", 1),
            ("\u017fBearer x", "\u017fBearer <redacted>", 1),
            ("\u212aBearer x", "\u212aBearer <redacted>", 1),
            ("xBearer abc", "xBearer abc", 0),
            ("_basic abc", "_basic abc", 0),
            ("Bearer \u00a0\n", "Bearer <redacted>\n", 1),
            ("Bearer\u00a0abc,def;x y", "Bearer\u00a0<redacted> y", 1),
            ("BASIC\u2003a'b\"c", "BASIC\u2003<redacted>", 1),
            ("Bearer <redacted>", "Bearer <redacted>", 1),
            ("authorization: Bearer abc", "authorization: <redacted> <redacted>", 2),
        ]
        for text, want, n in cases:
            with self.subTest(text=text):
                masked, counts = tc.mask_detail(text)
                self.assertEqual((masked, counts["secret_kw"]), (want, n))

    def test_key_value(self):
        cases = [
            ("token=abc\u00a0def ghi", "token=<redacted> ghi", 1),
            ("tokenä=abc", "tokenä=<redacted>", 1),
            ("pwd\u00a0:\u2003wert,rest", "pwd\u00a0:\u2003<redacted>,rest", 1),
            ("secret = 'a b'", "secret = '<redacted>'", 1),
            ("api_key=<redacted>", "api_key=<redacted>", 0),
            # Spec A3: only the exact placeholder is skipped (case-sensitive).
            ('token="<redacted>"', 'token="<redacted>"', 0),
            ("token=<REDACTED>", "token=<redacted>", 1),
            ('token="<REDACTED>"', 'token="<redacted>"', 1),
            ("secret='<Redacted>'", "secret='<redacted>'", 1),
            ("AP\u0130_KEY=<redacted>", "AP\u0130_KEY=<redacted>", 0),
        ]
        for text, want, n in cases:
            with self.subTest(text=text):
                masked, counts = tc.mask_detail(text)
                self.assertEqual((masked, counts["secret_kw"]), (want, n))

    def test_phone(self):
        zero = "0"
        cases = [
            (zero + "30 \u0661\u0662\u0663\u0664\u0665\u0666\u0667\u0668", "<phone>", 1),
            (zero + "30 1234567" + "8\u0661", "<phone>", 1),
            ("\u00b2" + zero + "30 1234567" + "8", None, 0),
            ("\u216b" + zero + "30 1234567" + "8", None, 0),
            ("a+" + "49 30 1234567" + "8", None, 0),
        ]
        for text, want, n in cases:
            with self.subTest(text=text):
                masked, counts = tc.mask_detail(text)
                self.assertEqual((masked, counts["phone"]), (text if want is None else want, n))


if __name__ == "__main__":
    unittest.main()
