#!/usr/bin/env python3
"""Write nfc_tables.go, the data nfc.go needs for Unicode NFC (UAX #15).

Every table comes from Python's unicodedata, so that nfc() in Go and
ts_common.py (which normalises with unicodedata.normalize) agree on every
string. Standard library only; the output depends on nothing but
unicodedata.unidata_version, and nfc_test.go checks that the committed file
is what this script writes.

    python3 gen_nfc_tables.py                   # Go source to standard output
    python3 gen_nfc_tables.py -o nfc_tables.go  # into a file (go generate)

What is taken from where:
  - canonical combining class: unicodedata.combining;
  - canonical decomposition mapping: unicodedata.decomposition without a
    <tag>, Hangul syllables left out (nfc.go decomposes and composes them by
    the algorithm of Unicode chapter 3.12; their mappings are checked against
    that algorithm here);
  - composition exclusions (singletons, non-starter decompositions and the
    script-specific and post-composition-version exclusions): a rune with a
    canonical mapping whose unicodedata.normalize("NFC") is not the rune
    itself. unicodedata has no CompositionExclusions table, but no excluded
    rune survives NFC and every other rune with a mapping does;
  - NFC quick check: No = the composition exclusions; Maybe = every rune that
    is the second of a primary composite's mapping, the Hangul vowels and
    trailing consonants, and every primary composite whose full decomposition
    starts with such a rune (new kinds in Unicode 16: Tulu-Tigalari, Gurung
    Khema, Kirat Rai vowel signs); Yes = everything else.
"""

import argparse
import os
import sys
import tempfile
import unicodedata

MAX_RUNE = 0x10FFFF

# Hangul syllable algorithm, Unicode chapter 3.12 (nfc.go has the same values).
S_BASE, L_BASE, V_BASE, T_BASE = 0xAC00, 0x1100, 0x1161, 0x11A7
L_COUNT, V_COUNT, T_COUNT = 19, 21, 28
N_COUNT = V_COUNT * T_COUNT
S_COUNT = L_COUNT * N_COUNT

# Property bits of nfcStage2 (emitted as Go constants).
PROP_CCC = 0xFF
PROP_QC_MAYBE = 0x100
PROP_QC_NO = 0x200
PROP_QC = 0x300
PROP_DECOMP = 0x400

BLOCK_SHIFT = 5
BLOCK = 1 << BLOCK_SHIFT


def fail(msg):
    raise SystemExit("gen_nfc_tables.py: " + msg)


def check(cond, msg):
    if not cond:
        fail(msg)


def code_points():
    for cp in range(MAX_RUNE + 1):
        if 0xD800 <= cp <= 0xDFFF:
            continue
        yield cp


def hangul_jamo(cp):
    s = cp - S_BASE
    jamo = [L_BASE + s // N_COUNT, V_BASE + (s % N_COUNT) // T_COUNT]
    if s % T_COUNT:
        jamo.append(T_BASE + s % T_COUNT)
    return jamo


def canonical_order(runes, ccc):
    """Stable sort of every run of non-starters by combining class."""
    out, run = [], []
    for r in runes:
        if ccc.get(r, 0) == 0:
            out += sorted(run, key=lambda x: ccc[x])
            run = []
            out.append(r)
        else:
            run.append(r)
    return out + sorted(run, key=lambda x: ccc[x])


def build():
    ccc = {}
    raw = {}
    for cp in code_points():
        ch = chr(cp)
        k = unicodedata.combining(ch)
        if k:
            ccc[cp] = k
        d = unicodedata.decomposition(ch)
        if S_BASE <= cp < S_BASE + S_COUNT:
            jamo = hangul_jamo(cp)
            check(d == " ".join("%04X" % j for j in jamo),
                  "U+%04X: unicodedata maps it to %r, the Hangul algorithm to %r" % (cp, d, jamo))
            js = "".join(map(chr, jamo))
            check(unicodedata.normalize("NFD", ch) == js and unicodedata.normalize("NFC", js) == ch,
                  "U+%04X: NFD/NFC disagree with the Hangul algorithm" % cp)
            continue
        if d and not d.startswith("<"):
            raw[cp] = tuple(int(x, 16) for x in d.split())

    for j in list(range(L_BASE, L_BASE + L_COUNT)) + list(range(V_BASE, V_BASE + V_COUNT)) + \
            list(range(T_BASE + 1, T_BASE + T_COUNT)):
        check(j not in ccc and j not in raw, "Hangul jamo U+%04X has a class or a mapping" % j)

    full = {}

    def decompose(cp):
        if cp not in full:
            if cp in raw:
                out = []
                for r in raw[cp]:
                    out += decompose(r)
                full[cp] = out
            else:
                full[cp] = [cp]
        return full[cp]

    excluded = set()
    for cp, m in raw.items():
        check(len(m) in (1, 2), "U+%04X: mapping of %d runes" % (cp, len(m)))
        ch = chr(cp)
        nfd = "".join(map(chr, canonical_order(decompose(cp), ccc)))
        check(unicodedata.normalize("NFD", ch) == nfd, "U+%04X: NFD is not its ordered full decomposition" % cp)
        if unicodedata.normalize("NFC", ch) != ch:
            excluded.add(cp)

    pairs = {}
    for cp, m in raw.items():
        if len(m) == 1 or ccc.get(cp, 0) or ccc.get(m[0], 0):
            check(cp in excluded, "U+%04X: singleton or non-starter decomposition survives NFC" % cp)
        if cp in excluded:
            continue
        check(len(m) == 2, "U+%04X: kept by NFC but not a pair" % cp)
        check(m not in pairs, "U+%04X: pair %r composes twice" % (cp, m))
        for r in m:
            check(not (L_BASE <= r < T_BASE + T_COUNT or S_BASE <= r < S_BASE + S_COUNT),
                  "U+%04X: table pair with a Hangul rune" % cp)
        pairs[m] = cp

    backward = {b for (_, b) in pairs}
    backward |= set(range(V_BASE, V_BASE + V_COUNT)) | set(range(T_BASE + 1, T_BASE + T_COUNT))
    maybe = set(backward)
    for cp in pairs.values():
        if full[cp][0] in backward:
            maybe.add(cp)
    check(not (maybe & excluded), "a rune is both quick check Maybe and No")

    # nfc.go treats a rune with class 0 and quick check Yes as a boundary:
    # nothing before it reorders or composes with anything from it on.
    for cp in pairs.values():
        check(ccc.get(cp, 0) == 0, "U+%04X: primary composite with a class" % cp)
        if cp not in maybe:
            first = full[cp][0]
            check(ccc.get(first, 0) == 0 and first not in maybe,
                  "U+%04X: quick check Yes, but its decomposition starts with U+%04X" % (cp, first))

    def prop(cp):
        p = ccc.get(cp, 0)
        if cp in excluded:
            p |= PROP_QC_NO
        elif cp in maybe:
            p |= PROP_QC_MAYBE
        if cp in raw:
            p |= PROP_DECOMP
        return p

    nonzero = [cp for cp in code_points() if prop(cp)]
    limit = (nonzero[-1] // BLOCK + 1) * BLOCK
    trivial_below = min(cp for cp in nonzero if prop(cp) & (PROP_CCC | PROP_QC))
    check(trivial_below > 0x7F, "an ASCII rune is not trivial")

    blocks, stage1 = {}, []
    for start in range(0, limit, BLOCK):
        values = tuple(prop(cp) if not 0xD800 <= cp <= 0xDFFF else 0 for cp in range(start, start + BLOCK))
        stage1.append(blocks.setdefault(values, len(blocks)))
    stage2 = [v for values in sorted(blocks, key=blocks.get) for v in values]

    decomp_runes = sorted(raw)
    index = {cp: i for i, cp in enumerate(decomp_runes)}
    compose = [index[cp] for _, cp in sorted(pairs.items())]

    return {
        "version": unicodedata.unidata_version,
        "limit": limit,
        "trivial_below": trivial_below,
        "stage1": stage1,
        "stage1_type": "uint8" if len(blocks) <= 256 else "uint16",
        "stage2": stage2,
        "decomp_runes": decomp_runes,
        "decomp": [raw[cp] + (0,) * (2 - len(raw[cp])) for cp in decomp_runes],
        "compose": compose,
        "counts": (len(raw), len(excluded), len(pairs), len(maybe), len(ccc)),
    }


def lines(items, per_line):
    out = []
    for i in range(0, len(items), per_line):
        out.append("\t" + ", ".join(items[i:i + per_line]) + ",\n")
    return "".join(out)


def render(t):
    n_raw, n_excl, n_pairs, n_maybe, n_ccc = t["counts"]
    out = []
    w = out.append
    w("// Code generated by gen_nfc_tables.py from Python's unicodedata %s; DO NOT EDIT.\n\n" % t["version"])
    w("package main\n\n")
    w("// Tables for nfc.go, from Python's unicodedata %s: %d runes with a\n" % (t["version"], n_raw))
    w("// canonical decomposition mapping (Hangul syllables are done by algorithm),\n")
    w("// %d of them composition exclusions, %d primary composites, %d runes with\n" % (n_excl, n_pairs, n_ccc))
    w("// a combining class and %d with NFC quick check Maybe. Regenerate with\n" % n_maybe)
    w("// go generate; gen_nfc_tables.py says how each table is derived.\n\n")
    w("// nfcUnicodeVersion is the unicodedata.unidata_version of the tables.\n")
    w('const nfcUnicodeVersion = "%s"\n\n' % t["version"])
    w("// Bits of a rune's properties in nfcStage2: its canonical combining class,\n")
    w("// its NFC quick check value (Yes when both QC bits are clear) and whether it\n")
    w("// has a mapping in nfcDecomp.\n")
    consts = [
        ("nfcPropCCC", "0x%x" % PROP_CCC),
        ("nfcPropQCMaybe", "0x%x" % PROP_QC_MAYBE),
        ("nfcPropQCNo", "0x%x" % PROP_QC_NO),
        ("nfcPropQC", "0x%x" % PROP_QC),
        ("nfcPropDecomp", "0x%x" % PROP_DECOMP),
    ]
    width = max(len(name) for name, _ in consts)
    w("const (\n")
    for name, value in consts:
        w("\t%s = %s\n" % (name.ljust(width), value))
    w(")\n\n")
    w("// nfcPropsLimit: every rune from here on has properties 0.\n")
    w("const nfcPropsLimit = 0x%x\n\n" % t["limit"])
    w("// nfcTrivialBelow: every rune below it has combining class 0 and NFC quick\n")
    w("// check Yes.\n")
    w("const nfcTrivialBelow = 0x%x\n\n" % t["trivial_below"])
    w("// nfcBlockShift: rune r < nfcPropsLimit has the properties\n")
    w("// nfcStage2[nfcStage1[r>>nfcBlockShift]<<nfcBlockShift | r&(1<<nfcBlockShift-1)].\n")
    w("const nfcBlockShift = %d\n\n" % BLOCK_SHIFT)
    w("var nfcStage1 = [...]%s{\n" % t["stage1_type"])
    w(lines([str(v) for v in t["stage1"]], 32))
    w("}\n\n")
    w("var nfcStage2 = [...]uint16{\n")
    w(lines([str(v) for v in t["stage2"]], BLOCK))
    w("}\n\n")
    w("// nfcDecompRunes lists in order every rune with a canonical decomposition\n")
    w("// mapping, except the Hangul syllables; nfcDecomp[i] is the mapping of\n")
    w("// nfcDecompRunes[i], one rune or two (0 when there is no second).\n")
    w("var nfcDecompRunes = [...]rune{\n")
    w(lines(["0x%04x" % cp for cp in t["decomp_runes"]], 12))
    w("}\n\n")
    w("var nfcDecomp = [...][2]rune{\n")
    w(lines(["{0x%04x, 0x%04x}" % m for m in t["decomp"]], 6))
    w("}\n\n")
    w("// nfcComposeIndex lists the primary composites (two-rune mappings that are\n")
    w("// not composition exclusions, so NFC composes them) as indexes into\n")
    w("// nfcDecompRunes, ordered by their mapping.\n")
    w("var nfcComposeIndex = [...]uint16{\n")
    w(lines([str(i) for i in t["compose"]], 16))
    w("}\n")
    return "".join(out)


def main():
    ap = argparse.ArgumentParser(description=__doc__.split("\n")[0])
    ap.add_argument("-o", metavar="FILE", help="write the Go source to FILE instead of standard output")
    args = ap.parse_args()
    src = render(build())
    if not args.o:
        sys.stdout.write(src)
        return
    out_dir = os.path.dirname(os.path.abspath(args.o))
    fd, tmp = tempfile.mkstemp(dir=out_dir, prefix=".nfc_tables.", suffix=".tmp")
    try:
        with os.fdopen(fd, "w", encoding="utf-8", newline="\n") as f:
            f.write(src)
        os.replace(tmp, args.o)
    except BaseException:
        os.unlink(tmp)
        raise


if __name__ == "__main__":
    main()
