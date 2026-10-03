package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"go/format"
	"math/rand/v2"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"unicode/utf8"
)

// nfcKnownAnswers: inputs with their NFC, checked against Python's
// unicodedata 16.0.0 when the table was written and again by
// TestNFCPythonRandom wherever python3 has that version. They run without
// Python, so CI (whose python3 may have another unicodedata) still tests
// nfc. Cases marked "Part n" come from NormalizationTest.txt.
var nfcKnownAnswers = []struct{ in, want, why string }{
	{"", "", "empty"},
	{"plain ASCII", "plain ASCII", "ASCII"},
	{"e\u0301", "\u00e9", "e + acute composes"},
	{"\u00e9", "\u00e9", "precomposed stays"},
	{"Gro\u00dfe Stra\u00dfe", "Gro\u00dfe Stra\u00dfe", "German NFC stays"},
	{"Mu\u0308ller", "M\u00fcller", "NFD umlaut composes"},
	{"\u212b", "\u00c5", "Angstrom sign, singleton"},
	{"\u2126", "\u03a9", "Ohm sign, singleton"},
	{"\u212a", "K", "Kelvin sign, singleton"},
	{"A\u030a", "\u00c5", "A + ring"},
	{"A\u0301\u0328", "\u0104\u0301", "reorder 230/202, then compose"},
	{"\u1e0a\u0323", "\u1e0c\u0307", "Part 0"},
	{"D\u0307\u0323", "\u1e0c\u0307", "Part 0"},
	{"D\u031b\u0307\u0323", "\u1e0c\u031b\u0307", "Part 0"},
	{"\u0112\u0300", "\u1e14", "chained composition"},
	{"E\u0300\u0304", "\u00c8\u0304", "second mark blocked"},
	{"\u1e14\u0304", "\u1e14\u0304", "composite + same-class mark"},
	{"\u05b8\u05b9\u05b1\u0591\u05c3\u05b0\u05ac\u059f", "\u05b1\u05b8\u05b9\u0591\u05c3\u05b0\u05ac\u059f", "Hebrew reordering, Part 0"},
	{"a\u0315\u0300\u05ae\u0300b", "\u00e0\u05ae\u0300\u0315b", "Part 2"},
	{"a\u0302\u0315\u0300\u05aeb", "\u1ea7\u05ae\u0315b", "Part 2"},
	{"\u1100\u1161", "\uac00", "L + V"},
	{"\u1100\u1161\u11a8", "\uac01", "L + V + T"},
	{"\uac00\u11a8", "\uac01", "LV + T"},
	{"\uac01\u11a8", "\uac01\u11a8", "LVT + T stays"},
	{"\u1100\uac00\u11a8", "\u1100\uac01", "Part 0 Hangul"},
	{"\u1100\uac00\u11a8\u11a8", "\u1100\uac01\u11a8", "Part 0 Hangul"},
	{"\uac00\u11a7", "\uac00\u11a7", "U+11A7 is no trailing consonant"},
	{"\u1100\u0334\u1161", "\u1100\u0334\u1161", "Part 3: jamo blocked by a mark"},
	{"\u11a8\u1161", "\u11a8\u1161", "T + V stays"},
	{"\u0958", "\u0915\u093c", "composition exclusion (script-specific)"},
	{"\u0915\u093c", "\u0915\u093c", "excluded pair stays decomposed"},
	{"\u2adc", "\u2add\u0338", "composition exclusion (post-composition version)"},
	{"\U0001d15e", "\U0001d157\U0001d165", "composition exclusion, musical symbol"},
	{"\u0344", "\u0308\u0301", "non-starter decomposition"},
	{"\u0f73", "\u0f71\u0f72", "non-starter decomposition, Tibetan"},
	{"\u0340\u0341\u0343\u0374\u037e\u0387", "\u0300\u0301\u0313\u02b9;\u00b7", "singletons"},
	{"\uf900\U0002f800", "\u8c48\u4e3d", "CJK compatibility singletons"},
	{"\u2000", "\u2002", "en quad singleton"},
	{"\u0b47\u0b3e", "\u0b4b", "Oriya two-part vowel"},
	{"\u0b47\u0334\u0b3e", "\u0b47\u0334\u0b3e", "Part 3: blocked"},
	{"\u0dd9\u0dcf\u0dca", "\u0ddd", "Sinhala chain"},
	{"\u0ddd\u0334", "\u0ddd\u0334", "Part 0 Sinhala"},
	{"\u1025\u102e", "\u1026", "Myanmar"},
	{"\u1e9b\u0323", "\u1e9b\u0323", "long s with dot above + dot below"},
	{"\u03b1\u0313\u0342\u0345", "\u1f86", "Greek, three marks"},
	{"\u0627\u0653\u0654", "\u0622\u0654", "Arabic"},
	{"\U0001138b\U000113c7", "\U0001138e\U000113b8", "Part 5: Tulu-Tigalari chain"},
	{"\U0001138b\U000113c5\U000113c2", "\U0001138e\U000113c5", "Part 5"},
	{"\U0001611e\U00016121", "\U00016121\U0001611e", "Part 5: Gurung Khema"},
	{"\U0001611e\U00016121\U0001611f", "\U00016121\U00016123", "Part 5"},
	{"\U00016d63\U00016d68", "\U00016d6a", "Kirat Rai"},
	{"\U00016d69\U00016d68", "\U00016d6a\U00016d67", "Kirat Rai"},
	{"\u0301abc", "\u0301abc", "leading mark"},
	{"\u0301\u0300\u0323", "\u0323\u0301\u0300", "only marks, reordered"},
	{"\ufffd\u0301", "\ufffd\u0301", "U+FFFD is a starter"},
	{"\U0001f469\u200d\U0001f4bb\ufe0f", "\U0001f469\u200d\U0001f4bb\ufe0f", "emoji ZWJ sequence stays"},
	{"\u00c5\u0301", "\u01fa", "precomposed + mark"},
	{"A\u030a\u0301", "\u01fa", "A + ring + acute"},
}

func TestNFCKnownAnswers(t *testing.T) {
	for _, c := range nfcKnownAnswers {
		if got := nfc(c.in); got != c.want {
			t.Errorf("%s: nfc(%+q) = %+q, want %+q", c.why, c.in, got, c.want)
		}
		if got := isNFC(c.in); got != (c.in == c.want) {
			t.Errorf("%s: isNFC(%+q) = %v", c.why, c.in, got)
		}
		if got := nfc(c.want); got != c.want {
			t.Errorf("%s: nfc not idempotent on %+q: %+q", c.why, c.want, got)
		}
	}
}

// TestNFCComposePairHangul pins the Hangul edges of nfcComposePair on its
// own: nfcCompose only asks it about runes with quick check Maybe, which
// would hide a wrong range here.
func TestNFCComposePairHangul(t *testing.T) {
	for _, c := range []struct {
		a, b rune
		want rune
		ok   bool
	}{
		{0x1100, 0x1161, 0xac00, true}, // first L + first V
		{0x1112, 0x1175, 0xd788, true}, // last L + last V
		{0x1113, 0x1161, 0, false},     // after the last L
		{0x1100, 0x1176, 0, false},     // after the last V
		{0xac00, 0x11a8, 0xac01, true}, // LV + first T
		{0xd788, 0x11c2, 0xd7a3, true}, // last LV + last T
		{0xac00, 0x11a7, 0, false},     // U+11A7 is TBase, not a T
		{0xac00, 0x11c3, 0, false},     // after the last T
		{0xac01, 0x11a8, 0, false},     // LVT + T
		{0xd7a4, 0x11a8, 0, false},     // after the last syllable
		{0x0041, 0x030a, 0x00c5, true}, // table pair
		{0x0041, 0x11a8, 0, false},
	} {
		if got, ok := nfcComposePair(c.a, c.b); got != c.want || ok != c.ok {
			t.Errorf("nfcComposePair(U+%04X, U+%04X) = U+%04X %v, want U+%04X %v", c.a, c.b, got, ok, c.want, c.ok)
		}
	}
}

// TestNFCInvalidUTF8: a byte that does not start valid UTF-8 is kept and
// separates what is before it from what is after it.
func TestNFCInvalidUTF8(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{"\xff", "\xff"},
		{"a\xff\u0301", "a\xff\u0301"},
		{"a\u0301\xff", "\u00e1\xff"},
		{"\xffe\u0301", "\xff\u00e9"},
		{"\u1100\xff\u1161", "\u1100\xff\u1161"},
		{"\u1100\u1161\xc3", "\uac00\xc3"},
		{"e\xe2\x82\u0301", "e\xe2\x82\u0301"},
		{"\xed\xa0\x80\u0301", "\xed\xa0\x80\u0301"}, // an encoded surrogate is three invalid bytes
		{"\u0301\xff\u0300\u0323", "\u0301\xff\u0323\u0300"},
	} {
		if got := nfc(c.in); got != c.want {
			t.Errorf("nfc(%+q) = %+q, want %+q", c.in, got, c.want)
		}
		if got := isNFC(c.in); got != (c.in == c.want) {
			t.Errorf("isNFC(%+q) = %v", c.in, got)
		}
	}
	// Random: nfc of the whole is nfc of each valid piece, bytes in between kept.
	rng := rand.New(rand.NewPCG(7, 11))
	pools := nfcTestPools()
	bad := []string{"\xff", "\x80", "\xc3", "\xe2\x82", "\xf0\x9f\x98", "\xed\xa0\x80", "\xc0\xaf"}
	for n := 0; n < 5000; n++ {
		var in, want strings.Builder
		for k := 1 + rng.IntN(4); k > 0; k-- {
			piece := nfcRandomSequence(rng, pools, 1+rng.IntN(6))
			b := bad[rng.IntN(len(bad))]
			in.WriteString(piece + b)
			want.WriteString(nfc(piece) + b)
		}
		if got := nfc(in.String()); got != want.String() {
			t.Fatalf("nfc(%+q) = %+q, want %+q", in.String(), got, want.String())
		}
	}
}

// TestNFCSameString: text the quick check accepts comes back as the same
// string, without allocating; isNFC never allocates, also on NFC text the
// quick check cannot decide (runes with quick check Maybe) and on text that is
// not NFC.
func TestNFCSameString(t *testing.T) {
	quick := []string{"", "ascii only", nfcPrefix(nfcBenchText("german-nfc"), 4096), nfcPrefix(nfcBenchText("hangul-syllables"), 4096)}
	for _, s := range quick {
		if nfcQuickSpan(s, 0) != len(s) {
			t.Fatalf("quick check rejects %.40q", s)
		}
		if allocs := testing.AllocsPerRun(10, func() { _ = nfc(s) }); allocs != 0 {
			t.Errorf("nfc(%.40q): %v allocations", s, allocs)
		}
	}
	maybeNFC := []string{"\u00e4\u0308 und \u0b47\u0334\u0b3e", "\u1100\u0334\u1161\u0dd9\u0334\u0dcf", strings.Repeat("x\u00e9\u0301", 100)}
	notNFC := []string{"e\u0301", nfcPrefix(nfcBenchText("german-nfd"), 4096), "\u212b", "abc\xff\u0301\u0300\u0323"}
	for _, s := range append(append(quick, maybeNFC...), notNFC...) {
		want := nfc(s) == s
		if got := isNFC(s); got != want {
			t.Errorf("isNFC(%.40q) = %v, want %v", s, got, want)
		}
		if allocs := testing.AllocsPerRun(10, func() { _ = isNFC(s) }); allocs != 0 {
			t.Errorf("isNFC(%.40q): %v allocations", s, allocs)
		}
	}
	for _, s := range maybeNFC {
		if nfcQuickSpan(s, 0) == len(s) || !isNFC(s) {
			t.Errorf("%+q should be NFC with quick check Maybe", s)
		}
	}
}

// TestNFCTablesGofmt: the generated file is gofmt-clean (no Python needed).
func TestNFCTablesGofmt(t *testing.T) {
	src, err := os.ReadFile("nfc_tables.go")
	if err != nil {
		t.Fatal(err)
	}
	out, err := format.Source(src)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(out, src) {
		t.Error("nfc_tables.go is not gofmt-clean; gen_nfc_tables.py has to write it so")
	}
	if !bytes.HasPrefix(src, []byte("// Code generated by gen_nfc_tables.py ")) || !bytes.Contains(src, []byte("; DO NOT EDIT.\n")) {
		t.Error("nfc_tables.go lacks its Code generated header")
	}
}

var nfcPythonOnce = sync.OnceValues(func() (string, string) {
	py, err := exec.LookPath("python3")
	if err != nil {
		return "", "python3 not found, so nfc cannot be compared with unicodedata"
	}
	out, err := exec.Command(py, "-c", "import unicodedata; print(unicodedata.unidata_version)").Output()
	if err != nil {
		return "", "python3 -c 'import unicodedata' failed: " + err.Error()
	}
	if v := strings.TrimSpace(string(out)); v != nfcUnicodeVersion {
		return "", fmt.Sprintf("python3 has unicodedata %s, nfc_tables.go is from %s, so there is nothing to compare with (a python3 with unicodedata %s can; go generate under it regenerates the tables)", v, nfcUnicodeVersion, nfcUnicodeVersion)
	}
	return py, ""
})

// nfcPython runs python3 with args and input on standard input and returns
// its standard output. It skips the test when python3 is missing or its
// unicodedata is not the version of the tables.
func nfcPython(t *testing.T, input []byte, args ...string) []byte {
	t.Helper()
	py, skip := nfcPythonOnce()
	if py == "" {
		t.Skip(skip)
	}
	cmd := exec.Command(py, args...)
	cmd.Stdin = bytes.NewReader(input)
	cmd.Env = append(os.Environ(), "PYTHONDONTWRITEBYTECODE=1", "PYTHONUTF8=1")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("python3: %v\n%s", err, stderr.String())
	}
	return out
}

// TestNFCPythonCodePoints compares nfc, isNFC and canonicalCombiningClass
// with unicodedata for every code point (surrogates excepted) as a
// one-rune string.
func TestNFCPythonCodePoints(t *testing.T) {
	raw := nfcPython(t, nil, "-c", `import json, sys, unicodedata as ud
changed, ccc, n = [], [], 0
for cp in range(0x110000):
    if 0xD800 <= cp <= 0xDFFF:
        continue
    n += 1
    c = chr(cp)
    if ud.normalize("NFC", c) != c:
        changed.append([cp, ud.normalize("NFC", c)])
    if ud.combining(c):
        ccc.append([cp, ud.combining(c)])
sys.stdout.buffer.write(json.dumps({"n": n, "changed": changed, "ccc": ccc}, ensure_ascii=False).encode())
`)
	var py struct {
		N       int
		Changed [][2]any
		CCC     [][2]int
	}
	if err := json.Unmarshal(raw, &py); err != nil {
		t.Fatal(err)
	}
	const total = utf8.MaxRune + 1 - (0xdfff - 0xd800 + 1)
	if py.N != total || len(py.Changed) == 0 || len(py.CCC) == 0 {
		t.Fatalf("python3 reported %d code points, %d changed by NFC, %d with a class; want %d and more than 0", py.N, len(py.Changed), len(py.CCC), total)
	}
	changed := map[rune]string{}
	for _, c := range py.Changed {
		changed[rune(c[0].(float64))] = c[1].(string)
	}
	ccc := map[rune]uint8{}
	for _, c := range py.CCC {
		ccc[rune(c[0])] = uint8(c[1])
	}
	n, bad := 0, 0
	for r := rune(0); r <= utf8.MaxRune; r++ {
		if r >= 0xd800 && r <= 0xdfff {
			continue
		}
		n++
		s := string(r)
		want, ok := changed[r]
		if !ok {
			want = s
		}
		got := nfc(s)
		gotQC := isNFC(s)
		gotCCC := canonicalCombiningClass(r)
		if got != want || gotQC != (want == s) || gotCCC != ccc[r] {
			bad++
			if bad <= 20 {
				t.Errorf("U+%04X: nfc %+q isNFC %v class %d; Python %+q %v %d", r, got, gotQC, gotCCC, want, want == s, ccc[r])
			}
		}
	}
	if n != total {
		t.Fatalf("compared %d code points, want %d", n, total)
	}
	if bad > 0 {
		t.Fatalf("%d of %d code points differ from Python", bad, n)
	}
	t.Logf("%d code points identical to unicodedata %s (%d changed by NFC, %d with a combining class)", n, nfcUnicodeVersion, len(changed), len(ccc))
}

// nfcTestPools: runes to draw random sequences from. Taking them from the
// tables only steers which inputs are tried; the expected output comes from
// Python.
type nfcPools struct {
	starters, decomposable, marks, maybe, firsts, hangul, special []rune
}

func nfcTestPools() nfcPools {
	var p nfcPools
	for _, r := range "aAeEiouyDKsSnNz09 .-'\"" {
		p.starters = append(p.starters, r)
	}
	p.decomposable = nfcDecompRunes[:]
	for r := rune(0); r < nfcPropsLimit; r++ {
		props := nfcProps(r)
		if props&nfcPropCCC != 0 {
			p.marks = append(p.marks, r)
		}
		if props&nfcPropQC == nfcPropQCMaybe {
			p.maybe = append(p.maybe, r)
		}
	}
	for _, i := range nfcComposeIndex {
		p.firsts = append(p.firsts, nfcDecomp[i][0])
	}
	for r := rune(hangulLBase); r < hangulLBase+hangulLCount; r++ {
		p.hangul = append(p.hangul, r)
	}
	for r := rune(hangulVBase); r < hangulVBase+hangulVCount; r++ {
		p.hangul = append(p.hangul, r)
	}
	for r := rune(hangulTBase); r <= hangulTBase+hangulTCount; r++ { // U+11A7 and U+11C3 too
		p.hangul = append(p.hangul, r)
	}
	p.hangul = append(p.hangul, 0xac00, 0xac01, 0xac1c, 0xd788, 0xd7a3, 0xd7a4)
	p.special = []rune{
		0x212b, 0x2126, 0x212a, 0x00c5, 0x00e5, 0x03a9, 0x0041, 0x030a, 0x0301, 0x0300, 0x0308, 0x0323,
		0x0327, 0x0328, 0x031b, 0x0315, 0x05ae, 0x0334, 0x0345, 0x0340, 0x0341, 0x0343, 0x0344, 0x0374,
		0x037e, 0x0387, 0x0958, 0x0915, 0x093c, 0x09dc, 0x0a33, 0x0f43, 0x0f73, 0x0f75, 0x0f81, 0x0f71,
		0x0f72, 0x0f74, 0x0f80, 0xfb1d, 0xfb2a, 0x2adc, 0x1d15e, 0x1d165, 0x1d16d, 0xf900, 0x2f800,
		0x2000, 0x1e9b, 0x0b47, 0x0b3e, 0x0b56, 0x0b57, 0x0dd9, 0x0dcf, 0x0dca, 0x0ddd, 0x1025, 0x102e,
		0x0cc6, 0x0cc2, 0x0cd5, 0x0cca, 0x1b05, 0x1b35, 0x1138b, 0x1138e, 0x113b8, 0x113c2, 0x113c5,
		0x113c7, 0x113c8, 0x113c9, 0x1611e, 0x1611f, 0x16120, 0x16121, 0x16122, 0x16126, 0x16129,
		0x16d63, 0x16d67, 0x16d68, 0x16d69, 0x16d6a, 0x0627, 0x0653, 0x0654, 0x0655, 0x064b, 0x0650,
		0x03b1, 0x0313, 0x0314, 0x0342, 0xfffd, 0x200d, 0xfe0f, 0x1f469, 0x00a0, 0x0130, 0x0131,
	}
	return p
}

// nfcRandomSequence returns n runes from the pools, weighted towards what
// reorders and composes.
func nfcRandomSequence(rng *rand.Rand, p nfcPools, n int) string {
	var b strings.Builder
	for ; n > 0; n-- {
		var r rune
		switch k := rng.IntN(100); {
		case k < 12:
			r = p.starters[rng.IntN(len(p.starters))]
		case k < 26:
			r = p.decomposable[rng.IntN(len(p.decomposable))]
		case k < 46:
			r = p.marks[rng.IntN(len(p.marks))]
		case k < 56:
			r = p.maybe[rng.IntN(len(p.maybe))]
		case k < 66:
			r = p.firsts[rng.IntN(len(p.firsts))]
		case k < 80:
			r = p.hangul[rng.IntN(len(p.hangul))]
		case k < 85:
			r = hangulSBase + rune(rng.IntN(hangulSCount))
		case k < 95:
			r = p.special[rng.IntN(len(p.special))]
		default:
			for r = rune(rng.IntN(utf8.MaxRune + 1)); r >= 0xd800 && r <= 0xdfff; r = rune(rng.IntN(utf8.MaxRune + 1)) {
			}
		}
		b.WriteRune(r)
	}
	return b.String()
}

// nfcRandomCases: seeded sequences of four shapes, plus a few long runs of
// marks.
func nfcRandomCases(count int) []string {
	rng := rand.New(rand.NewPCG(2026, 1002))
	p := nfcTestPools()
	pick := func(pool []rune) rune { return pool[rng.IntN(len(pool))] }
	cases := make([]string, 0, count+8)
	for len(cases) < count {
		var b strings.Builder
		switch len(cases) % 4 {
		case 0: // anything
			b.WriteString(nfcRandomSequence(rng, p, 1+rng.IntN(12)))
		case 1: // a base and marks in random order (reordering, blocking)
			b.WriteRune(pick([][]rune{p.starters, p.firsts, p.decomposable}[rng.IntN(3)]))
			for k := 1 + rng.IntN(6); k > 0; k-- {
				b.WriteRune(pick([][]rune{p.marks, p.maybe, p.special}[rng.IntN(3)]))
			}
			if rng.IntN(2) == 0 {
				b.WriteString(nfcRandomSequence(rng, p, 1+rng.IntN(3)))
			}
		case 2: // Hangul jamo and syllables, now and then a mark
			for k := 1 + rng.IntN(8); k > 0; k-- {
				switch rng.IntN(10) {
				case 0:
					b.WriteRune(pick(p.marks))
				case 1, 2:
					b.WriteRune(hangulSBase + rune(rng.IntN(hangulSCount)))
				default:
					b.WriteRune(pick(p.hangul))
				}
			}
		case 3: // the special runes: singletons, exclusions, chains
			for k := 1 + rng.IntN(6); k > 0; k-- {
				b.WriteRune(pick(p.special))
			}
		}
		cases = append(cases, b.String())
	}
	for _, n := range []int{100, 1000, 5000} {
		var b strings.Builder
		b.WriteString("a")
		for k := 0; k < n; k++ {
			b.WriteRune(pick(p.marks))
		}
		cases = append(cases, b.String())
	}
	cases = append(cases, nfcPrefix(nfcBenchText("marks-230-220"), 30000), nfcPrefix(nfcBenchText("german-nfd"), 20000), nfcPrefix(nfcBenchText("hangul-jamo"), 20000))
	return cases
}

// TestNFCPythonRandom compares nfc and isNFC with unicodedata.normalize and
// is_normalized on seeded random sequences and on the known answers.
func TestNFCPythonRandom(t *testing.T) {
	cases := nfcRandomCases(30000)
	for _, c := range nfcKnownAnswers {
		cases = append(cases, c.in)
	}
	in, err := json.Marshal(cases)
	if err != nil {
		t.Fatal(err)
	}
	raw := nfcPython(t, in, "-c", `import json, sys, unicodedata as ud
cases = json.loads(sys.stdin.buffer.read().decode())
out = [[ud.normalize("NFC", s), ud.is_normalized("NFC", s)] for s in cases]
sys.stdout.buffer.write(json.dumps(out, ensure_ascii=False).encode())
`)
	var want [][2]any
	if err := json.Unmarshal(raw, &want); err != nil {
		t.Fatal(err)
	}
	if len(want) != len(cases) {
		t.Fatalf("python3 answered %d of %d cases", len(want), len(cases))
	}
	bad, changed := 0, 0
	for i, s := range cases {
		w, wQC := want[i][0].(string), want[i][1].(bool)
		got, gotQC := nfc(s), isNFC(s)
		if w != s {
			changed++
		}
		if got != w || gotQC != wQC || gotQC != (got == s) {
			bad++
			if bad <= 20 {
				t.Errorf("%+q: nfc %+q isNFC %v; Python %+q %v", s, got, gotQC, w, wQC)
			}
		}
	}
	k := len(cases) - len(nfcKnownAnswers)
	for i, c := range nfcKnownAnswers {
		if w := want[k+i][0].(string); w != c.want {
			t.Errorf("known answer %q (%s) is wrong: Python says %+q", c.in, c.why, w)
		}
	}
	if bad > 0 {
		t.Fatalf("%d of %d sequences differ from Python", bad, len(cases))
	}
	t.Logf("%d sequences identical to unicodedata %s (%d changed by NFC)", len(cases), nfcUnicodeVersion, changed)
}

// TestNFCPythonPairs compares nfc with unicodedata on every pair a+b, with a
// any rune that has a canonical mapping, a combining class or is the first
// of a primary composite (Hangul syllables sampled), and b any rune that
// composes backwards (the second of a primary composite, a Hangul vowel or
// trailing consonant and their neighbours U+11A7, U+11C3, or a composite
// whose decomposition starts with one), plus a few marks that block. Python
// picks both sets from its own data and returns the pairs NFC changes.
func TestNFCPythonPairs(t *testing.T) {
	raw := nfcPython(t, nil, "-c", `import json, sys, unicodedata as ud
A, B, second = set(), set(), set()
canon = {}
for cp in range(0x110000):
    if 0xD800 <= cp <= 0xDFFF:
        continue
    c = chr(cp)
    d = ud.decomposition(c)
    if d and not d.startswith("<"):
        canon[cp] = [int(x, 16) for x in d.split()]
        if len(canon[cp]) == 2 and ud.normalize("NFC", c) == c:
            A.add(canon[cp][0])
            second.add(canon[cp][1])
    if ud.combining(c):
        A.add(cp)
for cp in canon:
    if 0xAC00 <= cp <= 0xD7A3:
        if cp % 53 == 0 or cp in (0xAC00, 0xAC01, 0xD7A3):
            A.add(cp)
    else:
        A.add(cp)
B |= second | set(range(0x11A7, 0x11C4))
second_chars = {chr(x) for x in second}
for cp, m in canon.items():
    if ud.normalize("NFD", chr(cp))[0] in second_chars and ud.normalize("NFC", chr(cp)) == chr(cp):
        B.add(cp)
B |= {0x0334, 0x0315, 0x05AE, 0x0345, 0x093C, 0x0F71}
A, B = sorted(A), sorted(B)
changed = []
for i, a in enumerate(A):
    for j, b in enumerate(B):
        s = chr(a) + chr(b)
        n = ud.normalize("NFC", s)
        if n != s:
            changed.append([i, j, n])
sys.stdout.buffer.write(json.dumps({"a": A, "b": B, "changed": changed}, ensure_ascii=False).encode())
`)
	var py struct {
		A, B    []rune
		Changed [][3]any
	}
	if err := json.Unmarshal(raw, &py); err != nil {
		t.Fatal(err)
	}
	if len(py.A) == 0 || len(py.B) == 0 || len(py.Changed) == 0 {
		t.Fatalf("python3 sent %d first runes, %d second runes, %d changed pairs", len(py.A), len(py.B), len(py.Changed))
	}
	changed := map[[2]int]string{}
	for _, c := range py.Changed {
		changed[[2]int{int(c[0].(float64)), int(c[1].(float64))}] = c[2].(string)
	}
	bad := 0
	for i, a := range py.A {
		for j, b := range py.B {
			s := string(a) + string(b)
			want, ok := changed[[2]int{i, j}]
			if !ok {
				want = s
			}
			got := nfc(s)
			if got != want || isNFC(s) != (want == s) {
				bad++
				if bad <= 20 {
					t.Errorf("U+%04X U+%04X: nfc %+q isNFC %v; Python %+q", a, b, got, isNFC(s), want)
				}
			}
		}
	}
	if bad > 0 {
		t.Fatalf("%d of %d pairs differ from Python", bad, len(py.A)*len(py.B))
	}
	t.Logf("%d pairs (%d x %d) identical to unicodedata %s, %d changed by NFC", len(py.A)*len(py.B), len(py.A), len(py.B), nfcUnicodeVersion, len(changed))
}

// TestNFCTablesFresh reruns the generator and requires the committed
// nfc_tables.go to be its output.
func TestNFCTablesFresh(t *testing.T) {
	gen, err := filepath.Abs("gen_nfc_tables.py")
	if err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(t.TempDir(), "nfc_tables.go")
	nfcPython(t, nil, gen, "-o", out)
	want, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile("nfc_tables.go")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatal("nfc_tables.go is not what gen_nfc_tables.py writes; run go generate")
	}
}

// nfcBenchText returns 1 MiB of text of the named kind.
func nfcBenchText(kind string) string {
	const size = 1 << 20
	var unit string
	switch kind {
	case "ascii":
		unit = "The quick brown fox jumps over the lazy dog; 0123456789 and so on.\n"
	case "german-nfc", "german-nfd":
		unit = "Gr\u00f6\u00dfere \u00c4nderungen f\u00fcr \u00fcberm\u00e4\u00dfig lange Stra\u00dfenbahnfahrten \u00fcber M\u00fcnchen und K\u00f6ln, sagt J\u00fcrgen Wei\u00df.\n"
		if kind == "german-nfd" {
			unit = strings.NewReplacer("\u00e4", "a\u0308", "\u00f6", "o\u0308", "\u00fc", "u\u0308",
				"\u00c4", "A\u0308", "\u00d6", "O\u0308", "\u00dc", "U\u0308").Replace(unit)
		}
	case "hangul-syllables", "hangul-jamo":
		unit = "\ud55c\uad6d\uc5b4 \ud14d\uc2a4\ud2b8\ub97c \uc815\uaddc\ud654\ud569\ub2c8\ub2e4. "
		if kind == "hangul-jamo" {
			var b strings.Builder
			for _, r := range unit {
				if s := r - hangulSBase; s >= 0 && s < hangulSCount {
					b.WriteRune(hangulLBase + s/hangulNCount)
					b.WriteRune(hangulVBase + s%hangulNCount/hangulTCount)
					if t := s % hangulTCount; t != 0 {
						b.WriteRune(hangulTBase + t)
					}
					continue
				}
				b.WriteRune(r)
			}
			unit = b.String()
		}
	case "marks-230-220":
		// One starter, then marks of classes 230 and 220 alternating: a single
		// piece to reorder, the worst case for canonical ordering.
		var b strings.Builder
		b.WriteString("a")
		for b.Len() < size {
			b.WriteString("\u0301\u0323")
		}
		return nfcPrefix(b.String(), size)
	default:
		panic(kind)
	}
	return nfcPrefix(strings.Repeat(unit, size/len(unit)+1), size)
}

// nfcPrefix returns the longest prefix of s of at most n bytes that ends at a
// rune boundary.
func nfcPrefix(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n]
}

// BenchmarkNFC: go test -run '^$' -bench NFC reports ns/byte for each kind
// of 1 MiB text.
func BenchmarkNFC(b *testing.B) {
	for _, kind := range []string{"ascii", "german-nfc", "german-nfd", "hangul-syllables", "hangul-jamo", "marks-230-220"} {
		s := nfcBenchText(kind)
		b.Run(kind, func(b *testing.B) {
			b.SetBytes(int64(len(s)))
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_ = nfc(s)
			}
			b.ReportMetric(float64(b.Elapsed().Nanoseconds())/float64(b.N)/float64(len(s)), "ns/byte")
		})
	}
}

func BenchmarkIsNFC(b *testing.B) {
	for _, kind := range []string{"ascii", "german-nfc", "german-nfd", "hangul-jamo"} {
		s := nfcBenchText(kind)
		b.Run(kind, func(b *testing.B) {
			b.SetBytes(int64(len(s)))
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_ = isNFC(s)
			}
			b.ReportMetric(float64(b.Elapsed().Nanoseconds())/float64(b.N)/float64(len(s)), "ns/byte")
		})
	}
}
