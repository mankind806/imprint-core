#!/usr/bin/env python3
"""Write tests/mask-golden.json, the golden corpus of the mask parity spec (section 8).

Every case holds a text, the exact output of ts_common.mask_detail for it and all seven
counts. tests/test_mask_golden.py checks the file against ts_common (Python);
TestMaskGolden in imprint-core's tools/imprint-dev checks MaskDetail (Go) against the
same file (tools/typesafe is the canonical home of ts_common.py since 2026-10-03; before,
typesafe-dev generated the file and imprint-core copied it).

    python3 tests/gen_mask_golden.py            # writes tests/mask-golden.json
    python3 tests/gen_mask_golden.py -o FILE    # writes FILE

Standard library only and deterministic: fixed seed and date, lists instead of sets,
so the same Python and unicodedata write the same bytes. All data is synthetic. The
corpus: every fragment (all categories, every example named in the spec and its
amendments, multilingual) in every short context, every separator character (Python
\\s) in a few templates, and a seeded soup of fragments, words and single characters.
The A8 fragments (local widening), the A9 fragments (opaque runs before a decimal
digit), the NFC union fragments and the key=value tail fragments are in the grid only,
not in the soup's pool.

Exclusions, asserted (not hoped for) and listed in the header:
  - code points that are unassigned (Cn) in Python's unicodedata, and surrogates;
  - code points whose W, S or D predicate (spec section 0) differs between Python and
    Go: checked for every code point against Go's definitions written out below;
  - names whose matches can overlap (checked on the names list);
  - invalid UTF-8 (every text encodes strictly);
  - the names-loader differences of spec A7 (checked on the names list).

Superseded 2026-10-03: the spec A4 exclusion (an opaque candidate run directly followed
by a non-ASCII decimal digit) is gone. Spec A9 makes Python's rule the target of both
implementations; such runs are in the corpus, and every case's opaque step is asserted
against A9's rule text (a9_opaque below), written out independently of ts_common.

Pre-push safety: no line matches a shape of imprint-core's .githooks/pre-push in a C
(bytes) or UTF-8 (characters) reading. Characters outside ASCII except the German
letters are written as JSON \\u escapes anyway; a character that would complete a
shape (the "@" of an address, the separator of a phone number, a digit of an IBAN or
postcode) is escaped too. No line trips ts_common's local leak alarm either.
"""
import argparse
import hashlib
import json
import os
import platform
import random
import re
import sys
import tempfile
import time
import unicodedata

HERE = os.path.dirname(os.path.abspath(__file__))
REPO = os.path.dirname(HERE)
OUT = os.path.join(HERE, "mask-golden.json")
SEED = 20261002
CORPUS_DATE = "2026-10-03"  # date of the corpus definition (last change: key=value tail fragments), not of the run
N_SOUP = 1000
MAX_BYTES = 2_000_000
BS = chr(92)
AT = "@"
Z = "0"


def plz(a, b):
    """Five digits from two parts: no line of this file holds a postcode shape."""
    return a + b


# --- names (header "names"): synthetic, multilingual, two stored in NFD ----------------
NAMES = [
    "Max Mustermann",
    "Erika Musterfrau",
    "Jürgen Weißgerber",
    "Jose\u0301 Nu\u0301n\u0303ez",                     # NFD of Jos\u00e9 N\u00fa\u00f1ez
    "Zoe\u0308 Ha\u0308berle",                          # NFD of Zo\u00eb Häberle
    "\u0130pek I\u015f\u0131kda\u011f",                 # Turkish
    "Ingrid Ilgaz",
    "\u039d\u03af\u03ba\u03bf\u03c2 \u03a3\u03c4\u03b1\u03c5\u03c1\u03af\u03b4\u03b7\u03c2",  # Greek, final sigma
    "\u041e\u043b\u0435\u0433 \u0401\u0436\u0438\u043a\u043e\u0432",  # Cyrillic
    "\u0633\u0645\u064a\u0631",                         # Arabic
    "\u05e0\u05d5\u05e2\u05d4",                         # Hebrew
    "\u0905\u0928\u093f\u0932",                         # Devanagari
    "\u5c71\u7530\u592a\u90ce",                         # CJK
    "\ud55c\ubcc4",                                     # Hangul
    "Susanne Kowalczyk",
    "Anne-Kathrin Lindqvist",
]

# --- fragments: (id, text) -------------------------------------------------------------
FRAGMENTS = [
    # secret_kw, Bearer/Basic (spec 1a)
    ("bearer", "Bearer fake-tok3n"),
    ("bearer_lower_dotted", "bearer abc.def"),
    ("basic_em_space_quotes", "BASIC\u2003a'b\"c"),
    ("bearer_nbsp_comma", "Bearer\u00a0abc,def;x"),
    ("bearer_vt_in_value", "Bearer\x0babc\x0bdef"),
    ("bearer_nel", "Bearer\x85abc"),
    ("bearer_json_header", '"Authorization": "Bearer abc"'),
    ("authorization_bearer", "authorization: Bearer abc"),
    ("bearer_after_umlaut", "äBearer x"),
    ("bearer_after_roman_numeral", "\u216bBearer x"),
    ("bearer_after_long_s", "\u017fBearer x"),
    ("bearer_after_kelvin", "\u212aBearer x"),
    ("bearer_after_dotless_i", "\u0131Bearer x"),
    ("bearer_after_dotted_capital_i", "\u0130Bearer x"),
    ("bearer_after_ascii_letter", "xBearer abc"),
    ("basic_after_underscore", "_basic abc"),
    ("basic_dotted_capital_i", "BAS\u0130C abc"),
    ("basic_dotless_i", "bas\u0131c abc"),
    ("bearer_redacted", "Bearer <redacted>"),
    ("bearer_redacted_upper", "Bearer <REDACTED>"),
    ("bearer_info_separator", "Bearer\x1cabc"),
    ("basic_unit_separator", "Basic\x1fabc def"),
    ("bearer_cjk_value", "Bearer\u3000\u5bc6\u94a5"),
    # secret_kw, key = value (spec 1b, A3)
    ("kv_value_crosses_nbsp", "token=abc\u00a0def ghi"),
    ("kv_key_umlaut_tail", "tokenä=abc"),
    ("kv_unicode_spaces", "pwd\u00a0:\u2003wert,rest"),
    ("kv_single_quoted_space", "secret = 'a b'"),
    ("kv_double_quoted", 'api_key="x y z"'),
    ("kv_dotted_capital_i", "AP\u0130_KEY=abc"),
    ("kv_dotless_i", "credent\u0131al=abc"),
    ("kv_password", "password: fake7x"),
    ("kv_passwort", "Passwort=abc12"),
    ("kv_private_key", "private-key = 'q'"),
    ("kv_access_key", "ACCESS_KEY=abcd"),
    ("kv_auth_json", '{"auth": "xyz", "n": 1}'),
    ("kv_arabic_digits", "token=\u0661\u0662\u0663"),
    ("kv_placeholder_exact", "token=<redacted>"),
    ("kv_placeholder_upper", "token=<REDACTED>"),
    ("kv_placeholder_mixed_quoted", 'password: "<Redacted>"'),
    ("kv_placeholder_exact_quoted", 'secret="<redacted>"'),
    ("kv_basic_value", "Authorization: Basic abc"),
    # email
    ("email", "test.user" + AT + "mail.example"),
    ("email_plus", "max+tag" + AT + "sub.mail.example"),
    ("email_umlaut", "jürgen" + AT + "bücher.example"),
    ("email_cyrillic", "\u0438\u0432\u0430\u043d" + AT + "\u043f\u043e\u0447\u0442\u0430.example"),
    ("email_angle_dot", "<a.b" + AT + "mail.example>."),
    # iban
    ("iban_spaced", " ".join(["DE89", "3704", "0044", "0532", "0130", "00"])),
    ("iban_compact", "DE" + "89370400440532013000"),
    ("iban_gb", "GB" + "82 WEST 1234 5698 7654 32"),
    ("iban_lowercase", " ".join(["de89", "3704", "0044", "0532", "0130", "00"])),
    ("iban_glued", "X" + "DE" + "89370400440532013000"),
    # phone (spec 3)
    ("phone_plus49", "+49 30 1234" + "5678"),
    ("phone_zero", Z + "30 1234" + "5678"),
    ("phone_slash", Z + "30/1234 5678"),
    ("phone_parens", "(" + Z + "30) 1234" + "5678"),
    ("phone_dash", Z + "151-2345" + "6789"),
    ("phone_arabic_indic", Z + "30 \u0661\u0662\u0663\u0664\u0665\u0666\u0667\u0668"),
    ("phone_trailing_arabic_digit", Z + "30 1234" + "5678\u0661"),
    ("phone_devanagari", Z + "30 \u0967\u0968\u0969\u096a\u096b\u096c\u096d\u096e"),
    ("phone_after_superscript", "\u00b2" + Z + "30 1234" + "5678"),
    ("phone_after_roman_numeral", "\u216b" + Z + "30 1234" + "5678"),
    ("phone_after_letter_plus", "a+49 30 1234" + "5678"),
    ("phone_too_short", Z + "30 1234" + "5"),
    # address, examples of spec section 4
    ("street_strasse_letter", "Musterstrasse 12a"),
    ("street_an_der", "An der Linde 2"),
    ("street_zum", "Zum See 6"),
    ("plz_uppercase_city", plz("1234", "5") + " BERLIN"),
    ("plz_two_word_city", plz("1234", "5") + " Bad Homburg"),
    ("plz_slash_city", plz("1234", "5") + " Halle/Saale"),
    ("street_kudamm", "Ku'damm 3"),
    ("plz_im_breisgau", plz("7909", "8") + " Freiburg im Breisgau"),
    ("plz_ob_der_tauber", plz("9154", "1") + " Rothenburg ob der Tauber"),
    ("plz_im_brief", plz("1011", "5") + " Berlin im Brief"),
    ("plz_im_brief_sentence", "PLZ ist " + plz("1011", "5") + " Berlin im Brief."),
    ("plz_digit_after_city", plz("8033", "1") + " München1"),
    ("plz_accent_after_city", plz("1011", "5") + " Berlin\u00e9"),
    ("street_umlaut_after_number", "Musterstraße 12ä"),
    ("street_umlaut_before", "äMusterstraße 12"),
    # address, prose the union masks (spec A5, user decision)
    ("a5_im_jahr", "Im Jahr 2024 wurde"),
    ("a5_am_montag", "Am Montag 12 Uhr"),
    ("a5_im_pr", "Im PR 41 gefixt"),
    # address, more
    ("street", "Musterstraße 12"),
    ("street_abbrev", "Hauptstr. 4b"),
    ("street_range", "Lindenweg 3-5"),
    ("street_range_spaced", "Goethestraße 12 / 3 b"),
    ("street_am_alten_markt", "Am Alten Markt 7"),
    ("street_hinter_dem", "Hinter dem Tor 9"),
    ("street_upper", "HAUPTSTRASSE 1"),
    ("street_long_s", "Haupt\u017ftraße 5"),
    ("street_kelvin", "Alter Mar\u212at 3"),
    ("street_dotted_capital_i", "GoetheR\u0130NG 4"),
    ("street_dotless_i", "Hauptze\u0131le 3"),
    ("street_info_separator", "Musterweg\x1c12"),
    ("street_unit_separator", "Zum See\x1f6"),
    ("street_arabic_number", "Musterstraße \u0661\u0662"),
    ("street_ideographic_space", "Musterstraße\u300012"),
    ("plz_record_separator", plz("1234", "5") + "\x1eBerlin"),
    ("plz_arabic_digits", "\u0661\u0662\u0663\u0664\u0665 Berlin"),
    ("plz", plz("8033", "1") + " München"),
    ("plz_am", plz("1234", "5") + " Frankfurt am Main"),
    ("plz_an_der", plz("1234", "5") + " Neustadt an der Weinstraße"),
    ("plz_hyphen_city", plz("1234", "5") + " Berlin-Mitte"),
    ("street_and_plz", "Musterweg 3, " + plz("1234", "5") + " Musterstadt"),
    # names (spec 5, A1, A2; the header names list)
    ("name_full", "Max Mustermann"),
    ("name_upper", "MAX MUSTERMANN"),
    ("name_part_lower", "max"),
    ("name_inside_word", "Mustermanns"),
    ("name_second", "Erika Musterfrau"),
    ("name_umlaut_sz", "Jürgen Weißgerber"),
    ("name_capital_sharp_s", "Wei\u1e9egerber"),
    ("name_sharp_s_as_ss", "WEISSGERBER"),
    ("name_nfc_text_nfd_list", "Jos\u00e9 N\u00fa\u00f1ez"),
    ("name_nfd_text", "Jose\u0301 Nu\u0301n\u0303ez"),
    ("name_nfd_upper", "JOSE\u0301"),
    ("name_nfd_no_boundary", "Jose\u0301x"),
    ("name_zoe_nfc", "Zo\u00eb Häberle"),
    ("name_zoe_upper", "ZO\u00cb"),
    ("name_haeberle_nfd", "Ha\u0308berle"),
    ("name_turkish", "\u0130pek I\u015f\u0131kda\u011f"),
    ("name_turkish_upper", "IPEK I\u015eIKDA\u011e"),
    ("name_turkish_dotless", "\u0131pek"),
    ("name_ingrid_dotted", "\u0130NGR\u0130D"),
    ("name_ingrid_dotless", "\u0131ngr\u0131d \u0131lgaz"),
    ("name_greek", "\u039d\u03af\u03ba\u03bf\u03c2 \u03a3\u03c4\u03b1\u03c5\u03c1\u03af\u03b4\u03b7\u03c2"),
    ("name_greek_upper", "\u03a3\u03a4\u0391\u03a5\u03a1\u038a\u0394\u0397\u03a3"),
    ("name_greek_medial_sigma", "\u03c3\u03c4\u03b1\u03c5\u03c1\u03af\u03b4\u03b7\u03c3"),
    ("name_greek_nfd", "\u039d\u03b9\u0301\u03ba\u03bf\u03c2"),
    ("name_cyrillic", "\u041e\u043b\u0435\u0433 \u0401\u0436\u0438\u043a\u043e\u0432"),
    ("name_cyrillic_upper", "\u041e\u041b\u0415\u0413"),
    ("name_cyrillic_nfd", "\u0415\u0308\u0436\u0438\u043a\u043e\u0432"),
    ("name_arabic", "\u0633\u0645\u064a\u0631"),
    ("name_hebrew", "\u05e0\u05d5\u05e2\u05d4"),
    ("name_devanagari", "\u0905\u0928\u093f\u0932"),
    ("name_cjk", "\u5c71\u7530\u592a\u90ce"),
    ("name_cjk_glued", "\u5c71\u7530\u592a\u90ce\u3055\u3093"),
    ("name_hangul", "\ud55c\ubcc4"),
    ("name_hangul_jamo", "\u1112\u1161\u11ab\u1107\u1167\u11af"),
    ("name_hangul_glued", "\ud55c\ubcc4\ub2d8"),
    ("name_hangul_vowel_after", "\ud55c\ubcc4\u1161"),
    ("name_long_s", "\u017fu\u017fanne"),
    ("name_kelvin", "\u212aOWALCZYK"),
    ("name_hyphenated", "Anne-Kathrin Lindqvist"),
    ("name_hyphen_part", "Kathrin"),
    ("name_zwsp_between", "Max\u200bMustermann"),
    ("name_zwj_between", "Max\u200dMustermann"),
    ("name_tibetan_after", "Max\u0f74\u0f73"),
    ("name_qc_maybe_after", "Max\u0b3e"),
    ("name_mark_after", "Max\u0301"),
    ("name_after_oriya_pair", "\u0b47\u0b3e Erika"),
    # opaque (spec 7)
    ("aws_key_id", "AKIAIOSFODNN7EXAMPLE"),
    ("known_prefix_github", "ghp_fake0123456789abcdef"),
    ("known_prefix_sk", "sk-fake-AbC123XYZ0000"),
    ("known_prefix_slack", "xoxb-fake-1234-5678"),
    ("known_prefix_jwt", "eyJhbGciOiJIUzI1NiJ9.fake.sig"),
    ("opaque_hash", "abcdefABCDEF0123456789ghijkl"),
    ("opaque_base64", "QmFzZTY0U3RyaW5nV2l0aE51bWJlcnMxMjM0NQ=="),
    ("opaque_no_digit", "abcdefghijklmnopqrstuvwxyzABCD"),
    ("opaque_short", "abc123def456"),
    # Unicode (spec 0, A1, A2, section 8)
    ("single_letter_nfc", "Ö"),
    ("single_letter_nfd", "O\u0308"),
    ("single_letter_e_acute", "\u00e9"),
    ("e_acute_nfd", "e\u0301"),
    ("superscripts", "x\u00b2 + y\u00b3"),
    ("roman_numerals", "Kapitel \u216b und \u2167"),
    ("emoji_zwj_family", "\U0001f468\u200d\U0001f469\u200d\U0001f467"),
    ("emoji_skin_tone", "\U0001f44d\U0001f3fd ok"),
    ("hangul_jamo", "\u1100\u1161\u11a8 \uac01"),
    ("tibetan_run", "a" + "\u0f73" * 40),
    ("combining_run", "a" + "\u0301" * 40),
    ("greek_sigma_words", "\u03bb\u03cc\u03b3\u03bf\u03c2 \u039b\u038c\u0393\u039f\u03a3"),
]

# Spec A8 (local widening), added 2026-10-02: a name directly followed by a character
# that starts no segment, in text that is NFC and in text that is not NFC elsewhere;
# a segment NFC changes still widens (Kelvin sign before a name takes the space). These
# fragments go into the grid only: the seeded soup keeps drawing from FRAGMENTS, so
# its cases stay the same as before A8 and the diff of the corpus shows A8 alone.
A8_FRAGMENTS = [
    ("a8_maybe_after_name_nfd_elsewhere", "Max\u0b3e und Jose\u0301"),
    ("a8_maybe_after_name_nfc_elsewhere", "Max\u0b3e und Jos\u00e9"),
    ("a8_bengali_maybe_after_name", "Erika\u09be"),
    ("a8_tamil_maybe_after_name", "Ingrid\u0bbe e\u0301"),
    ("a8_tulu_maybe_after_name", "Max\U000113b8"),
    ("a8_greek_name_malayalam_maybe", "\u039d\u03af\u03ba\u03bf\u03c2\u0d3e"),
    ("a8_hangul_vowel_after_name", "Max\u1161 e\u0301"),
    ("a8_mark_after_name_nfd_elsewhere", "Max\u0301 und Zoe\u0308"),
    ("a8_cyrillic_nfd_name_mark_after", "\u0415\u0308\u0436\u0438\u043a\u043e\u0432\u0301"),
    ("a8_changed_segment_widens", "Jose\u0301\u0b3e"),
    ("a8_kelvin_after_space", "Herr \u212aowalczyk"),
    ("a8_kelvin_and_maybe", "Frau \u212aOWALCZYK und Max\u0b3e"),
]

# Spec A9 (supersedes A4), added 2026-10-03: a maximal run R of [A-Za-z0-9_+/-] is masked
# iff len(R) >= 24, R holds an ASCII letter, and R holds an ASCII digit or the character
# directly after R (before any '=') is a decimal digit (Nd); up to two '=' after R go with
# it. Runs of 23, 24 and 25 characters directly before an Arabic-Indic, an Extended
# Arabic-Indic and a Devanagari digit (every pair); the padding variants take each length
# and each digit once (23 with U+0661, 24 with U+06F3, 25 with U+0966; every pair would
# put the file within 40 kB of MAX_BYTES): after '=' and after '==' (not masked, the digit
# is not directly after R), and with an ASCII digit of their own before '==' (masked with
# the padding). A superscript two (No) and a Roman numeral (Nl) are no decimal digits.
# Grid only, like A8: the soup keeps drawing from FRAGMENTS and stays the same.
_A9_RUN = "ab_c+d/e-" * 3  # every token character besides letters and digits, low entropy
_A9_DIGITS = [("u0661", "\u0661"), ("u06f3", "\u06f3"), ("u0966", "\u0966")]
_A9_DIAGONAL = list(zip((23, 24, 25), _A9_DIGITS))
A9_FRAGMENTS = (
    [("a9_run%d_%s" % (n, dn), _A9_RUN[:n] + d) for n in (23, 24, 25) for dn, d in _A9_DIGITS]
    + [("a9_run%d_eq_%s" % (n, dn), _A9_RUN[:n] + "=" + d) for n, (dn, d) in _A9_DIAGONAL]
    + [("a9_run%d_eqeq_%s" % (n, dn), _A9_RUN[:n] + "==" + d) for n, (dn, d) in _A9_DIAGONAL]
    + [("a9_run%d_ascii_digit_eqeq_%s" % (n, dn), _A9_RUN[:n - 1] + "7==" + d)
       for n, (dn, d) in _A9_DIAGONAL]
    + [("a9_run24_superscript_two", _A9_RUN[:24] + "\u00b2"),
       ("a9_run24_roman_numeral", _A9_RUN[:24] + "\u216b")]
)

# NFC union of the email and address passes (user decision "Union", 2026-10-02): a
# decomposed umlaut in an email's local part, in its domain and right after its TLD,
# an NFD span that bridges two spans on the text (one merged span, counted once),
# postcode + NFD place name (Python and Go branches), NFD street names with a number
# (Python and Go branches, the Kelvin sign), and a precomposed place name or house
# number followed by a combining mark (the span on the text survives the union; the
# bare postcode case of the spec is g/plz/mark_after).
# Grid only, like A8_FRAGMENTS: the seeded soup keeps drawing from FRAGMENTS.
UNION_FRAGMENTS = [
    ("union_email_nfd_domain", "x local" + AT + "mu\u0308nchen.example y"),
    ("union_email_nfd_local", "x ju\u0308rgen.mu\u0308ller" + AT + "example.org y"),
    ("union_email_nfd_after_tld", "info" + AT + "mail.exampleu\u0308"),
    ("union_email_nfd_bridges_two", "ab" + AT + "mail.example\u0308x" + AT + "mail.example"),
    ("union_plz_nfd_place", "Adresse: " + plz("8033", "1") + " Mu\u0308nchen, fertig"),
    ("union_plz_nfd_place_py", plz("1234", "5") + " Bad Du\u0308rkheim"),
    ("union_plz_nfd_place_go", plz("1234", "5") + " MU\u0308NCHEN"),
    ("union_plz_nfc_place_mark_after", "Adresse: " + plz("8033", "1") + " München\u0301, fertig"),
    ("union_street_nfd", "Mu\u0308hlenweg 3"),
    ("union_street_nfd_go", "Am Mu\u0308hlbach 2"),
    ("union_street_kelvin", "\u212aönigsweg 4"),
    ("union_street_and_plz_nfd", "Mu\u0308hlenweg 3, " + plz("1234", "5") + " Mu\u0308nster"),
    ("union_street_number_mark_after", "Musterstrasse 1\u0308"),
]

# Key=value tail, added 2026-10-03: the tail [\w.-]* after a keyword is W, "." and "-" in
# both implementations (Python's (?i) leaves \w unfolded). U+0345 folds to iota but is no
# word character, so no tail and nothing masked. Go's regex path, taken for text with a
# long s, Kelvin sign or Turkish i anywhere, folded the class and masked these until
# 2026-10-03; its fast path did not. With and without such a rune after the pair (the
# context "turkish" puts one around every fragment).
# Grid only, like A8_FRAGMENTS: the seeded soup keeps drawing from FRAGMENTS.
KV_TAIL_FRAGMENTS = [
    ("kv_tail_mark", "token\u0345=abc"),
    ("kv_tail_mark_dotless_i", "token\u0345=abc \u0131"),
    ("kv_tail_mark_long_s", "token\u0345x=abc \u017f"),
    ("kv_tail_mark_second_key_kelvin", "token\u0345=a\"b secret=c \u212a"),
]

# The examples the spec and its amendments name; each must be the text of a case.
SPEC_EXAMPLES = [
    "bearer_after_umlaut", "bearer_after_roman_numeral", "bearer_after_long_s", "bearer_after_kelvin",
    "bearer_after_dotless_i", "bearer_after_dotted_capital_i", "bearer_json_header",
    "authorization_bearer", "kv_key_umlaut_tail", "kv_placeholder_upper", "kv_placeholder_exact",
    "phone_arabic_indic", "phone_trailing_arabic_digit", "phone_after_superscript",
    "phone_after_roman_numeral", "street_strasse_letter", "street_an_der", "street_zum",
    "plz_uppercase_city", "plz_two_word_city", "plz_slash_city", "street_kudamm", "plz_im_breisgau",
    "plz_ob_der_tauber", "plz_im_brief", "plz_digit_after_city", "plz_accent_after_city",
    "street_umlaut_after_number", "street_umlaut_before", "a5_im_jahr", "a5_am_montag", "a5_im_pr",
    "tibetan_run", "single_letter_nfc", "single_letter_e_acute", "name_nfd_text",
    "name_qc_maybe_after", "a8_maybe_after_name_nfd_elsewhere", "a8_maybe_after_name_nfc_elsewhere",
    "a8_kelvin_after_space", "union_email_nfd_domain", "union_email_nfd_local", "union_plz_nfd_place",
    "union_plz_nfc_place_mark_after",
]

CONTEXTS = [
    ("bare", "{}"),
    ("spaces", "x {} y"),
    ("sentence", "Text: {}."),
    ("parens", "({})"),
    ("dquotes", '"{}"'),
    ("squote_comma", "'{}', weiter"),
    ("umlaut_glued", "ä{}ä"),
    ("roman_superscript", "\u216b{}\u00b2"),
    ("underscores", "_{}_"),
    ("arabic_digit_after", "{}\u0661"),
    ("nbsp", "\u00a0{}\u00a0"),
    ("ideographic_space", "\u3000{}\u3000"),
    ("tab_newline", "\t{}\n"),
    ("info_separators", "\x1c{}\x1f"),
    ("greek", "\u0396\u03c9\u03ae {} \u03c4\u03ad\u03bb\u03bf\u03c2"),
    ("cyrillic", "\u041f\u0440\u0438\u0432\u0435\u0442, {}! \u041f\u043e\u043a\u0430"),
    ("arabic", "\u0645\u0631\u062d\u0628\u0627 {} \u0663"),
    ("hebrew", "\u05e9\u05dc\u05d5\u05dd {} \u05e2\u05d5\u05dc\u05dd"),
    ("devanagari", "\u0928\u092e\u0938\u094d\u0924\u0947 {} \u096a\u0968"),
    ("cjk_glued", "\u6771\u4eac\u3067{}\u3067\u3059"),
    ("hangul", "\ud55c\uad6d {} \uc2dc"),
    ("emoji", "\U0001f600 {} \U0001f44d\U0001f3fd"),
    ("zwsp_zwj", "\u200b{}\u200d"),
    ("nfd_around", "e\u0301 {} A\u030a"),
    ("turkish", "\u0130 {} \u0131"),
    ("prose_a5", "Im Jahr 2024 wurde {} gemeldet"),
    ("twice", "{} {}"),
    ("mark_after", "{}\u0301"),
    ("as_token_value", "token={}"),
]

# Every separator character (Python \s, spec section 0 S) in these templates.
S_TEMPLATES = [
    ("bearer", "Bearer{}abc"),
    ("street", "Musterweg{}12"),
    ("plz", plz("1234", "5") + "{}Berlin"),
    ("name", "Max{}Mustermann"),
    ("phone", Z + "30{}1234" + "5678"),
]
S_CONTEXTS = ["bare", "greek", "nfd_around"]

# Soup material.
WORDS = ["und", "der", "Termin", "bitte", "Weg", "Straße", "am", "im", "an der", "Am", "Im", "Jahr",
         "PR", "Haus", "Uhr", "token", "secret", "Bearer", "Basic", "=", ":", "<redacted>",
         "<REDACTED>", "Max", "Erika", "Ingrid", "\u03ba\u03b1\u03b9", "\u0438", "\u0648",
         "\u05d5", "\u0914\u0930", "\u3068", "\uadf8\ub9ac\uace0", "\u0130", "\u0131"]
DIGIT_RUNS = ["1", "12", "2024", "4b", "007", plz("1234", "5"), "\u0661\u0662", "\u0661\u0662\u0663\u0664\u0665",
              "\u0967\u0968", "\uff11\uff12", "\u0e51\u0e52", "\U0001d7ce", "1\u0662"]
SEPARATORS = [" ", " ", " ", " ", ", ", ". ", "-", "/", "", "\n", "\t", "\u00a0", "\u3000", "\x1c",
              "\u2028", "\x85", "\x0b", "  "]
CHAR_POOL = (list("aZx09_-./:=@<>'\"+,;()") + list("äöüßÄÖÜ") + [
    "\u00e9", "\u00f1", "\u00b2", "\u00b3", "\u00b5", "\u00b9", "\u1e9e", "\u017f", "\u212a", "\u0130",
    "\u0131", "\u00c5", "\u2126", "\u212b", "\u03b1", "\u03b2", "\u03c2", "\u03c3", "\u03a3", "\u0390",
    "\u03b0", "\u03d0", "\u03d1", "\u03d5", "\u03f1", "\u03f5", "\u0345", "\u1fbe", "\u0430", "\u0416",
    "\u0451", "\u0401", "\u0628", "\u0661", "\u06f3", "\u05d0", "\u05b7", "\u0915", "\u093f", "\u093c",
    "\u096a", "\u4e2d", "\uf900", "\uac00", "\u1100", "\u1161", "\u11a8", "\U0001f600", "\U0001f3fd",
    "\U0001f44d", "\u0301", "\u0308", "\u0323", "\u0327", "\u0f71", "\u0f72", "\u0f73", "\u0f74",
    "\u0b3e", "\u0b47", "\u3099", "\u304b", "\u200d", "\u200b", "\ufeff", "\u2167", "\u216b",
    "\u2170", "\u0e51", "\uff11", "\U0001d7ce", "\U000113c2", "\U000113b8", "\u00a0", "\u3000",
])

CATEGORIES_EXPECTED = ("secret_kw", "email", "address", "name", "opaque", "iban", "phone")

# Go's predicates (spec section 0), written out: unicode.IsSpace is White_Space (the
# Latin-1 part: \t \n \v \f \r space U+0085 U+00A0); W is "_", letters and numbers by
# general category (unicode.IsLetter, unicode.IsNumber); D is Nd (unicode.IsDigit).
GO_WHITE_SPACE = [0x09, 0x0A, 0x0B, 0x0C, 0x0D, 0x20, 0x85, 0xA0, 0x1680, *range(0x2000, 0x200B),
                  0x2028, 0x2029, 0x202F, 0x205F, 0x3000]
GO_S_EXTRA = list(range(0x1C, 0x20))  # spec 0: S adds U+001C..U+001F to IsSpace in Go
TURKISH_I = "Ii\u0130\u0131"

# The shapes of imprint-core's .githooks/pre-push (2026-10-02). The hook greps under C
# and under the user's own locale. Byte view: what grep sees under C. Text view: a
# UTF-8 locale, where a letter range may take letters outside ASCII (GNU grep 3.12 in
# de_DE.UTF-8 reads a-z as matching the lowercase German letters and A-Z as matching
# the capital umlauts, measured 2026-10-02); every letter range here takes all German
# letters, the only non-ASCII characters the file holds unescaped. [[:space:]] as
# glibc's C.UTF-8 defines it. The named group marks the character escaped to break a match.
_UMLAUTS_UPPER, _UMLAUTS_LOWER = ["Ä", "Ö", "Ü"], ["ä", "ö", "ü", "ß"]
_UTF8_SPACE = "".join(map(chr, [0x09, 0x0A, 0x0B, 0x0C, 0x0D, 0x20, 0x1680, *range(0x2000, 0x2007),
                                *range(0x2008, 0x200B), 0x2028, 0x2029, 0x205F, 0x3000]))


def _shape_patterns():
    de = "".join(_UMLAUTS_UPPER + _UMLAUTS_LOWER)
    text = [
        ("address", re.compile(r"[A-Za-z0-9._%+-" + de + r"]+@[A-Za-z0-9.-" + de + r"]+\.[A-Za-z" + de
                               + r"]{2,}"), "at"),
        ("phone number", re.compile(r"(?:^|[^0-9])0[0-9]{2,4}(?P<sep>[ /-])[0-9]{4,}"), "sep"),
        ("IBAN", re.compile(r"[A-Z" + de + r"]{2}(?P<digit>[0-9])[0-9](?: ?[A-Z0-9" + de + r"]{4}){3,}"), "digit"),
        ("postcode and place", re.compile(
            r"[0-9]{4}(?P<last>[0-9])[" + re.escape(_UTF8_SPACE) + r"]+[A-Z" + de + r"][a-z" + de + r"-]{2,}"),
         "last"),
    ]
    up = b"|".join(c.encode() for c in _UMLAUTS_UPPER)
    low = b"|".join(c.encode() for c in _UMLAUTS_LOWER)
    raw = [
        ("address", re.compile(rb"[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}"), "at"),
        ("phone number", re.compile(rb"(?:^|[^0-9])0[0-9]{2,4}(?P<sep>[ /-])[0-9]{4,}"), "sep"),
        ("IBAN", re.compile(rb"[A-Z]{2}(?P<digit>[0-9])[0-9](?: ?[A-Z0-9]{4}){3,}"), "digit"),
        ("postcode and place", re.compile(rb"[0-9]{4}(?P<last>[0-9])[ \t\n\x0b\x0c\r]+(?:[A-Z]|" + up
                                          + rb")(?:[a-z-]|" + low + rb"){2,}"), "last"),
    ]
    return text, raw


SHAPES_TEXT, SHAPES_BYTES = _shape_patterns()


def shape_hit(line):
    """(shape, start, end, preferred char index) of the first shape match in either
    reading of line, or None."""
    for name, rx, group in SHAPES_TEXT:
        m = rx.search(line)
        if m:
            p = m.start() + m.group().index("@") if group == "at" else m.start(group)
            return name, m.start(), m.end(), p
    raw = line.encode("utf-8")
    for name, rx, group in SHAPES_BYTES:
        m = rx.search(raw)
        if m:
            char_at = []
            for i, c in enumerate(line):
                char_at += [i] * len(c.encode("utf-8"))
            p = m.start() + m.group().index(b"@") if group == "at" else m.start(group)
            return name, char_at[m.start()], char_at[m.end() - 1] + 1, char_at[p]
    return None


# --- JSON text, one case per line ---------------------------------------------------------
KEEP = frozenset("äöüßÄÖÜ")  # written as they are (repository convention), all else escaped
SHORT = {'"': BS + '"', BS: BS + BS, "\n": BS + "n", "\r": BS + "r", "\t": BS + "t",
         "\b": BS + "b", "\f": BS + "f"}


def esc(c):
    o = ord(c)
    if o > 0xFFFF:
        o -= 0x10000
        return BS + "u%04x" % (0xD800 + (o >> 10)) + BS + "u%04x" % (0xDC00 + (o & 0x3FF))
    return BS + "u%04x" % o


def piece(c, forced=False):
    if forced:
        return esc(c)
    if c in SHORT:
        return SHORT[c]
    if " " <= c <= "~" or c in KEEP:
        return c
    return esc(c)


def jenc(v):
    """JSON text of a header value (strings as in the cases, no forced escapes)."""
    if isinstance(v, str):
        return '"' + "".join(piece(c) for c in v) + '"'
    if isinstance(v, bool) or v is None:
        return json.dumps(v)
    if isinstance(v, int):
        return str(v)
    if isinstance(v, list):
        return "[" + ", ".join(jenc(x) for x in v) + "]"
    if isinstance(v, dict):
        return "{" + ", ".join(jenc(k) + ": " + jenc(x) for k, x in v.items()) + "}"
    raise TypeError(type(v))


def build_line(case, forced, categories):
    """JSON text of one case and, for every character written as itself, which field
    and index it came from (the characters the shape repair may escape)."""
    parts, owner, pos = [], {}, 0
    for field in ("id", "text", "masked"):
        head = ('{"' if field == "id" else ', "') + field + '": "'
        parts.append(head)
        pos += len(head)
        for i, c in enumerate(case[field]):
            p = piece(c, i in forced[field])
            if p == c:
                owner[pos] = (field, i)
            parts.append(p)
            pos += len(p)
        parts.append('"')
        pos += 1
    parts.append(', "counts": {' + ", ".join('"%s": %d' % (k, case["counts"][k]) for k in categories) + "}}")
    return "".join(parts), owner


def render_case(case, categories):
    """The case's line with every shape broken; returns (line, number of forced escapes)."""
    forced = {"id": set(), "text": set(), "masked": set()}
    for _ in range(10000):
        line, owner = build_line(case, forced, categories)
        hit = shape_hit(line)
        if hit is None:
            return line, sum(len(f) for f in forced.values())
        name, a, b, p = hit
        if p not in owner:
            free = [q for q in range(a, b) if q in owner]
            if not free:
                raise SystemExit("cannot break the %s shape in %s" % (name, line))
            p = free[0]
        field, i = owner[p]
        forced[field].add(i)
    raise SystemExit("shape repair does not converge: " + case["id"])


# --- checks ---------------------------------------------------------------------------------
def check_predicates():
    """Spec section 0 for every code point: Python's \\w, \\d and \\s equal Go's W, D and S as
    written out above (code points unassigned in Python are excluded separately), and
    Python's (?i) class of each of I, i, U+0130, U+0131 is exactly those four."""
    everything = "".join(chr(c) for c in range(0x110000) if not 0xD800 <= c <= 0xDFFF)
    w = set(re.findall(r"\w", everything))
    d = set(re.findall(r"\d", everything))
    s = set(re.findall(r"\s", everything))
    go_s = set(map(chr, GO_WHITE_SPACE + GO_S_EXTRA))
    bad = []
    for c in everything:
        cat = unicodedata.category(c)
        if cat == "Cn":
            continue
        if (c in w) != (c == "_" or cat[0] in "LN") or (c in d) != (cat == "Nd") or (c in s) != (c in go_s):
            bad.append("U+%04X" % ord(c))
    assert not bad, "W/S/D differ from Go's definitions: " + " ".join(bad[:20])
    for c in TURKISH_I:
        cls = re.findall("(?i)" + re.escape(c), everything)
        assert cls == list(TURKISH_I), "(?i) class of U+%04X: %r" % (ord(c), cls)


def same_word(a, b):
    """Equal under Python's case-insensitive matching (the names step)."""
    return bool(re.fullmatch("(?i)" + re.escape(a), b) and re.fullmatch("(?i)" + re.escape(b), a))


def check_names(tc, path):
    """Spec A7 (loader differences) and the overlap exclusion: no two lines share a word
    (case-insensitively, after NFC), no line repeats a word, every term starts and ends
    with a word character, and the loader keeps every line and every part."""
    nfc = lambda s: unicodedata.normalize("NFC", s)  # noqa: E731
    for line in NAMES:
        assert line == line.strip() and "  " not in line and "#" not in line and "\t" not in line, line
        assert not any("\x1c" <= c <= "\x1f" for c in line), line
        line.encode("utf-8")
    expected = set()
    for line in NAMES:
        expected.add(nfc(line))
        expected.update(nfc(line).split())
    terms = tc.load_names(path)
    assert sorted(terms) == sorted(expected), "loader dropped or changed a term"
    for t in terms:
        assert len(t) >= 2 and re.match(r"\w", t[0]) and re.match(r"\w", t[-1]), t
    words = [re.findall(r"\w+", nfc(line)) for line in NAMES]
    for k, ws in enumerate(words):
        for i, a in enumerate(ws):
            assert not any(same_word(a, b) for b in ws[i + 1:]), "word repeated in %r" % NAMES[k]
            for other in words[k + 1:]:
                assert not any(same_word(a, b) for b in other), "%r shared between lines" % a


_A9_TOKEN = frozenset("ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789_+/-")


def a9_opaque(text, repl):
    """Spec A9's rule text, written out without ts_common: every maximal run R of
    [A-Za-z0-9_+/-] with len(R) >= 24, an ASCII letter in R, and an ASCII digit in R or a
    decimal digit (Nd) directly after R (before any '='), is replaced together with up to
    two '=' after it. Returns (text, count) like re.subn."""
    out, count, i, n = [], 0, 0, len(text)
    while i < n:
        if text[i] not in _A9_TOKEN:
            out.append(text[i])
            i += 1
            continue
        j = i
        while j < n and text[j] in _A9_TOKEN:
            j += 1
        run = text[i:j]
        after = text[j] if j < n else ""
        if (len(run) >= 24 and any(c.isascii() and c.isalpha() for c in run)
                and (any(c.isascii() and c.isdigit() for c in run)
                     or (after != "" and unicodedata.category(after) == "Nd"))):
            k = j
            while k < n and k < j + 2 and text[k] == "=":
                k += 1
            out.append(repl)
            count += 1
            i = k
        else:
            out.append(run)
            i = j
    return "".join(out), count


def excluded(text):
    """The reason text is excluded, or None."""
    for c in text:
        if unicodedata.category(c) in ("Cn", "Cs"):
            return "unassigned_or_surrogate"
    return None


def mask_and_check_a9(tc, text):
    """mask_detail(text), and spec A9: the opaque step gives exactly what A9's rule text
    (a9_opaque) gives on the same input."""
    t = text
    for cat, rx, repl in tc.MASK_RES:
        if rx is tc.RX_OPAQUE:
            assert rx.subn(repl, t) == a9_opaque(t, repl), "A9: " + ascii(text)
        t, _ = rx.subn(repl, t)
    masked, counts = tc.mask_detail(text)
    assert masked == t
    return masked, counts


# --- corpus -----------------------------------------------------------------------------------
def candidates(rng):
    """(id, text) in a fixed order: grid, separators, soup."""
    for fid, frag in FRAGMENTS + A8_FRAGMENTS + A9_FRAGMENTS + UNION_FRAGMENTS + KV_TAIL_FRAGMENTS:
        for cid, ctx in CONTEXTS:
            yield "g/%s/%s" % (fid, cid), ctx.replace("{}", frag)
    contexts = dict(CONTEXTS)
    s_chars = re.findall(r"\s", "".join(chr(c) for c in range(0x3001)))
    for tid, tpl in S_TEMPLATES:
        for s in s_chars:
            for cid in S_CONTEXTS:
                yield "s/%s/U+%04X/%s" % (tid, ord(s), cid), contexts[cid].replace("{}", tpl.replace("{}", s))
    for k in range(N_SOUP * 3):
        out = []
        for _ in range(rng.randint(2, 6)):
            r = rng.random()
            if r < 0.35:
                out.append(rng.choice(FRAGMENTS)[1])
            elif r < 0.6:
                out.append(rng.choice(WORDS))
            elif r < 0.85:
                out.append("".join(rng.choice(CHAR_POOL) for _ in range(rng.randint(1, 4))))
            else:
                out.append(rng.choice(DIGIT_RUNS))
            out.append(rng.choice(SEPARATORS))
        yield "r/%04d" % k, "".join(out)


def generate(tc):
    categories = list(tc.DEFAULT_CATEGORIES)
    assert tuple(categories) == CATEGORIES_EXPECTED, categories
    rng = random.Random(SEED)
    cases, lines, seen = [], [], set()
    filtered = {"unassigned_or_surrogate": 0, "local_leak_alarm": 0, "duplicate_text": 0}
    forced_total, soup = 0, 0
    for cid, text in candidates(rng):
        if cid.startswith("r/") and soup >= N_SOUP:
            break
        if text in seen:
            filtered["duplicate_text"] += 1
            continue
        reason = excluded(text)
        if reason:
            filtered[reason] += 1
            continue
        masked, counts = mask_and_check_a9(tc, text)
        case = {"id": cid, "text": text, "masked": masked, "counts": counts}
        line, forced = render_case(case, categories)
        if tc.local_alarm(tc.alarm_view(line)):
            filtered["local_leak_alarm"] += 1
            continue
        seen.add(text)
        cases.append(case)
        lines.append(line)
        forced_total += forced
        soup += cid.startswith("r/")
    return categories, cases, lines, filtered, forced_total


def main():
    ap = argparse.ArgumentParser(description=__doc__.split("\n")[0])
    ap.add_argument("-o", metavar="FILE", default=OUT, help="output file (default tests/mask-golden.json)")
    args = ap.parse_args()
    t0 = time.perf_counter()

    # The names file must be in place before ts_common is imported: the import reads
    # TYPESAFE_NAMES_FILE, and the real, private names file must never be read here.
    fd, names_path = tempfile.mkstemp(prefix="typesafe-golden-names-", suffix=".txt")
    try:
        with os.fdopen(fd, "w", encoding="utf-8") as f:
            f.write("".join(n + "\n" for n in NAMES))
        os.environ["TYPESAFE_NAMES_FILE"] = names_path
        sys.path.insert(0, REPO)
        import ts_common as tc

        check_predicates()
        check_names(tc, names_path)
        assert tc.get_name_regex(names_path) is not None
        categories, cases, lines, filtered, forced = generate(tc)
    finally:
        os.remove(names_path)

    # Final checks on the corpus.
    assert cases, "no cases"
    present = {c["text"] for c in cases}
    grid = FRAGMENTS + A8_FRAGMENTS + A9_FRAGMENTS + UNION_FRAGMENTS + KV_TAIL_FRAGMENTS
    frags = dict(grid)
    assert len(frags) == len(grid), "fragment id used twice"
    missing = [f for f in SPEC_EXAMPLES if frags[f] not in present]
    assert not missing, "spec examples missing: %s" % missing
    missing = [f for f, text in A9_FRAGMENTS if text not in present]
    assert not missing, "A9 fragments missing: %s" % missing
    a9_cases = sum(1 for c in cases if c["id"].startswith("g/a9_"))
    category_cases = {k: sum(1 for c in cases if c["counts"][k]) for k in categories}
    assert all(category_cases.values()), category_cases
    for c in cases:
        assert excluded(c["text"]) is None, c["id"]
        c["text"].encode("utf-8")
        c["masked"].encode("utf-8")

    header = {
        "description": "Golden corpus of the mask parity spec (section 8, amendments A1-A9, A9 superseding A4, "
                       "NFC union of the email and address passes, key=value tail fragments, 2026-10-03): "
                       "for every case, "
                       "ts_common.mask_detail (Python) and MaskDetail (Go) must return exactly 'masked' and all "
                       "seven 'counts' for 'text', with TYPESAFE_NAMES_FILE holding exactly 'names', one per "
                       "line. Generated, do not edit: python3 tests/gen_mask_golden.py.",
        "generator": "tests/gen_mask_golden.py",
        "date": CORPUS_DATE,
        "python": platform.python_version(),
        "unidata_version": unicodedata.unidata_version,
        "seed": SEED,
        "case_count": len(cases),
        "categories": categories,
        "names": NAMES,
        "exclusions": [
            "code points unassigned (Cn) in Python's unicodedata, and surrogates: in no text (Go's newer "
            "Unicode letters are Cn here)",
            "code points whose W, S or D predicate (spec section 0) differs between Python and Go: none; "
            "asserted for every code point against Go's definitions (W: '_', letters and numbers by "
            "general category; D: Nd; S: White_Space and U+001C..U+001F). Case folding differs only in "
            "the class I, i, U+0130, U+0131, which both implementations share (kept in)",
            "overlapping names: no two lines of 'names' share a word (case-insensitive, after NFC), no "
            "line repeats a word, every name starts and ends with a word character",
            "invalid UTF-8: every text encodes strictly",
            "names loader (spec A7): no line with two spaces in a row, no U+001C..U+001F, UTF-8 only",
        ],
        "superseded": [
            "2026-10-03, spec A9: the exclusion of spec A4 (no text holds a run of 24 or more opaque token "
            "characters directly followed by a decimal digit outside ASCII; Python masks it, Go not, until "
            "done-check-timeout is merged) is withdrawn. Python's rule is the target of both implementations; "
            "the grid holds such runs (fragments a9_*)",
        ],
        "filtered": filtered,
        "category_cases": category_cases,
        "escaping": "JSON escapes for every character outside ASCII except the German letters; "
                    "characters that would complete a pre-push shape of imprint-core's .githooks/pre-push "
                    "are escaped too (%d)" % forced,
    }
    keys = list(header)
    out = ['{"header": {']
    out += [jenc(k) + ": " + jenc(header[k]) + ("," if k != keys[-1] else "") for k in keys]
    out += ["},", '"cases": [']
    out += [ln + ("," if i < len(lines) - 1 else "") for i, ln in enumerate(lines)]
    out += ["]}"]
    text = "\n".join(out) + "\n"

    for i, ln in enumerate(out):
        assert shape_hit(ln) is None, "line %d matches a pre-push shape" % (i + 1)
        assert not tc.local_alarm(tc.alarm_view(ln)), "line %d trips the local leak alarm" % (i + 1)
    assert json.loads(text) == {"header": header, "cases": cases}, "JSON round trip"
    data = text.encode("utf-8")
    assert len(data) < MAX_BYTES, "file has %d bytes" % len(data)

    out_dir = os.path.dirname(os.path.abspath(args.o))
    fd, tmp = tempfile.mkstemp(dir=out_dir, prefix=".mask-golden.", suffix=".tmp")
    try:
        with os.fdopen(fd, "wb") as f:
            f.write(data)
        os.chmod(tmp, 0o644)  # mkstemp creates 0600
        os.replace(tmp, args.o)
    except BaseException:
        os.unlink(tmp)
        raise
    print("%s: %d cases, %d bytes, sha256 %s, %.1f s" % (
        os.path.relpath(args.o), len(cases), len(data), hashlib.sha256(data).hexdigest(),
        time.perf_counter() - t0))
    print("filtered: %s; forced escapes: %d; A9 grid cases: %d" % (filtered, forced, a9_cases))
    print("cases per category: %s" % category_cases)


if __name__ == "__main__":
    main()
