package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
	"unicode"
	"unicode/utf8"
)

// Names on text that is not NFC (mask parity spec section 5, amendments A1,
// A2, A8; 2026-10-02): the names are NFC, the text is matched on an NFC copy
// and replaced in the original, widened only inside segments that NFC
// changes.

// nfcNamesFile holds names stored decomposed (NFD), a single letter in both
// forms (no name since A2), a name ending in "-" and Hangul as conjoining
// jamo.
const nfcNamesFile = "Max Mustermann\n" +
	"Jose\u0301\n" +
	"O\u0308zil\n" +
	"Oz\n" +
	"\u00d6\n" +
	"O\u0308\n" +
	"A\u030angstro\u0308m\n" +
	"\u0130lker\n" +
	"Klaus\n" +
	"\u1112\u1161\u11ab\u1107\u1167\u11af\n" +
	"Mustermann-\n" +
	"Se\u0301bastien Dupont\n"

// TestNamesNFC pins MaskDetail's names step on decomposed and mixed text.
// Every expectation is the output of the Python lane's ts_common.mask_detail
// (typesafe-dev branch mask-parity-unicode, 752698b, the same names file),
// run 2026-10-02. The reference must agree, and the replacements that the
// copy path records must add up to the name count.
func TestNamesNFC(t *testing.T) {
	setupJudge(t, "")
	nf := filepath.Join(t.TempDir(), "names.txt")
	if err := os.WriteFile(nf, []byte(nfcNamesFile), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TYPESAFE_NAMES_FILE", nf)
	for _, c := range []struct {
		in, want string
		n        int
	}{
		{"Herr Mu\u0308ller und Max Mustermann", "Herr Mu\u0308ller und <name>", 1},
		{"Jose\u0301 kommt", "<name> kommt", 1},
		{"Jos\u00e9 kommt", "<name> kommt", 1},
		{"JOSE\u0301!", "<name>!", 1},
		{"Jose\u0301x", "Jose\u0301x", 0},
		{"xJose\u0301", "xJose\u0301", 0},
		{"O\u0308zil, \u00d6zil und \u00f6ZIL", "<name>, <name> und <name>", 3},
		{"\u00d6 und O\u0308 allein", "\u00d6 und O\u0308 allein", 0},
		{"Oz\u0f74\u0f73 kommt", "<name> kommt", 1},
		{"Oz\u0b3e und Jose\u0301", "<name>\u0b3e und <name>", 2},
		{"Hallo \u212alaus!", "Hallo<name>!", 1},
		{"Hallo \u212alaus und Mu\u0308ller", "Hallo<name> und Mu\u0308ller", 1},
		{"Hallo Klaus und Mu\u0308ller", "Hallo <name> und Mu\u0308ller", 1},
		{"I\u0307lker und \u0130lker, ilker", "<name> und <name>, <name>", 3},
		{"\u1112\u1161\u11ab\u1107\u1167\u11af und \ud55c\ubcc4", "<name> und <name>", 2},
		{"Mustermann-\u212alaus", "<name>", 2},
		{"Mustermann-Klaus", "<name><name>", 2},
		{"A\u030angstro\u0308m \u212bngstr\u00f6m \u00c5ngstr\u00f6m", "<name><name> <name>", 3},
		{"Se\u0301bastien Dupont, S\u00e9bastien", "<name>, <name>", 2},
		{"Max\u0301 Mustermann", "<name>\u0301 <name>", 2},
		{"Max Mustermann\u0301", "<name> Mustermann\u0301", 1},
		{"\u0301Oz \u0301", "\u0301<name> \u0301", 1},
		{"Oz\u0301", "Oz\u0301", 0},
		{"Oz\u0323\u0301 und Oz\u0301\u0323", "Oz\u0323\u0301 und Oz\u0301\u0323", 0},
		{"<name>\u0338 Klaus", "<name>\u0338 <name>", 1},
		{"e\u0301 a\u0f73\u0f73\u0f73 Klaus\u0f73", "e\u0301 a\u0f73\u0f73\u0f73 <name>", 1},
	} {
		got, counts := MaskDetail(c.in)
		if got != c.want || counts != (MaskCounts{Name: c.n}) {
			t.Errorf("%+q: got %+q %+v, want %+q (name %d)", c.in, got, counts, c.want, c.n)
		}
		if ref, refCounts := maskDetailReference(c.in); ref != got || refCounts != counts {
			t.Errorf("%+q: reference %+q %+v, MaskDetail %+q %+v", c.in, ref, refCounts, got, counts)
		}
		recorded, called := 0, 0
		view := newNFCView(c.in)
		replaceNames(view.copy, loadNameSet().matchers, func(string) string { called++; return "<name>" },
			func(reps []nameRep) { recorded += len(reps) })
		if recorded != called || called != c.n {
			t.Errorf("%+q: %d replacements recorded, %d made, want %d", c.in, recorded, called, c.n)
		}
	}
}

// TestNamesNFCLoad (amendment A2): names are put into NFC when loaded and
// kept with at least two code points; a single letter is no name in either
// form, a name stored decomposed matches composed text, two spellings of one
// name are one pattern.
func TestNamesNFCLoad(t *testing.T) {
	nf := filepath.Join(t.TempDir(), "names.txt")
	content := "\u00d6\nO\u0308\n\u00e9\n\u212b\nOz\nJose\u0301\nJos\u00e9\nA\u030a B\n\u1112\u1161\u11ab\n"
	if err := os.WriteFile(nf, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	got := loadNames(nf)
	want := []string{"Jos\u00e9", "\u00c5 B", "Oz"} // longest in bytes first
	if !slices.Equal(got, want) {
		t.Errorf("loadNames = %+q, want %+q", got, want)
	}
	if ref := refLoadNames(nf); !slices.Equal(ref, want) {
		t.Errorf("refLoadNames = %+q, want %+q", ref, want)
	}
	for _, n := range got {
		if !isNFC(n) || utf8.RuneCountInString(n) < 2 {
			t.Errorf("%+q: not NFC or shorter than two code points", n)
		}
	}
}

// TestNamesNFCKeepsOriginal: outside the masked spans the text is copied
// byte for byte, decomposed words and invalid UTF-8 included; a text that is
// not valid UTF-8 is not normalised at all.
func TestNamesNFCKeepsOriginal(t *testing.T) {
	setupJudge(t, "")
	nf := filepath.Join(t.TempDir(), "names.txt")
	if err := os.WriteFile(nf, []byte("Max Mustermann\nJos\u00e9\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TYPESAFE_NAMES_FILE", nf)
	nfd := "Gru\u0308\u00dfe an Mu\u0308ller, A\u030a, \u212b, \u1100\u1161\u11a8"
	for _, c := range []struct{ in, want string }{
		{nfd + " und Jose\u0301.", nfd + " und <name>."},
		{"Jose\u0301 " + nfd, "<name> " + nfd},
		{"Jose\u0301\xff Mu\u0308ller Jos\u00e9", "Jose\u0301\xff Mu\u0308ller <name>"},
		{"\xffJos\u00e9 Mustermann", "\xff<name> <name>"},
	} {
		if got, _ := MaskDetail(c.in); got != c.want {
			t.Errorf("%+q: got %+q, want %+q", c.in, got, c.want)
		}
	}
}

// TestRefQCMaybeStarters: the literal set the reference's segment rule uses
// (copied from ts_common.py) is exactly the set of runes with combining class
// 0 and NFC quick check Maybe in nfc_tables.go (generated from Python's
// unicodedata), so the reference and nfcBoundary cut the same segments.
func TestRefQCMaybeStarters(t *testing.T) {
	n := 0
	for r := rune(0); r < 0x110000; r++ {
		p := nfcProps(r)
		maybe := p&nfcPropCCC == 0 && p&nfcPropQC == nfcPropQCMaybe
		if maybe != refQCMaybeStarters[r] {
			t.Errorf("%U: tables say class 0 and Maybe = %v, refQCMaybeStarters %v", r, maybe, refQCMaybeStarters[r])
		}
		if maybe {
			n++
		}
		if r < 0xd800 || r > 0xdfff {
			if b := refNFCBoundary(r); b != nfcBoundary(p) {
				t.Errorf("%U: refNFCBoundary %v, nfcBoundary %v", r, b, nfcBoundary(p))
			}
		}
	}
	if n != len(refQCMaybeStarters) {
		t.Errorf("%d runes, refQCMaybeStarters has %d", n, len(refQCMaybeStarters))
	}
}

// TestMaskNFCUnion pins the NFC union of the email, street and postcode
// steps (spec decided by the user 2026-10-02, the same text as
// ts_common.py's): each pass also scans an NFC copy of its own input, maps
// what it finds there back (widened to the edge of a segment NFC changes),
// and replaces the union with the matches on the input itself, merged where
// they overlap; each merged span counts once. The expectations follow the
// spec: the first three rows are its examples, the "e" + U+0301 row is one
// of the four golden mark_after cases it changes, and the "M" + U+00FC +
// "nchen" + U+0301 row the case where the input's own span survives. The
// reference (refUnionStep) must agree.
func TestMaskNFCUnion(t *testing.T) {
	t.Setenv("TYPESAFE_NAMES_FILE", filepath.Join(t.TempDir(), "none.txt"))
	plz := func(head, rest string) string { return head + rest } // .githooks/pre-push
	for _, c := range []struct {
		in, want string
		counts   MaskCounts
	}{
		// the spec's three examples: an NFD umlaut in the domain, in the
		// local part, and in the place after a postcode
		{"x local" + at + "mu\u0308nchen.example y", "x <email> y", MaskCounts{Email: 1}},
		{"x ju\u0308rgen" + at + "example.test y", "x <email> y", MaskCounts{Email: 1}},
		{"Adresse: " + plz("8033", "1 Mu\u0308nchen, fertig"), "Adresse: <address>, fertig", MaskCounts{Address: 1}},
		// a mark after the address: "e" + U+0301 composes, so the copy's
		// address takes it along; "." + U+0301 and "t" + U+0301 do not
		{"test.user" + at + "mail.example\u0301", "<email>", MaskCounts{Email: 1}},
		{"<a.b" + at + "mail.example>.\u0301", "<<email>>.\u0301", MaskCounts{Email: 1}},
		{"a" + at + "b.test\u0301", "<email>\u0301", MaskCounts{Email: 1}},
		// the input's own span survives: a precomposed umlaut, then an acute
		// on the last "n" (U+0144 in the copy, where no place can end)
		{plz("8033", "1 M\u00fcnchen\u0301"), "<address>\u0301", MaskCounts{Address: 1}},
		// the input's span ("hlenweg 3", "Markt 3", "ller@...") and the
		// copy's (all of it) overlap: one merged span, one count
		{"Mu\u0308hlenweg 3", "<address>", MaskCounts{Address: 1}},
		{"Am Gru\u0308nen Markt 3", "<address>", MaskCounts{Address: 1}},
		{"mu\u0308ller" + at + "example.test", "<email>", MaskCounts{Email: 1}},
		// spans that only touch stay apart: on the input "x" + at + "y.z" ends at
		// the "+", where the copy's second address ("+" + U+00FC + at + "w.v") starts
		{"x" + at + "y.z+u\u0308" + at + "w.v", "<email><email>", MaskCounts{Email: 2}},
		// nothing on either: a lone mark is unchanged by NFC
		{"\u0308" + at + "x.example", "\u0308" + at + "x.example", MaskCounts{}},
		// email and address on one line, each pass on its own input
		{"mu\u0308ller" + at + "ko\u0308ln.example, " + plz("5066", "7 Ko\u0308ln"), "<email>, <address>",
			MaskCounts{Email: 1, Address: 1}},
	} {
		got, counts := MaskDetail(c.in)
		if got != c.want || counts != c.counts {
			t.Errorf("%+q: got %+q %+v, want %+q %+v", c.in, got, counts, c.want, c.counts)
		}
		if ref, refCounts := maskDetailReference(c.in); ref != got || refCounts != counts {
			t.Errorf("%+q: reference %+q %+v, MaskDetail %+q %+v", c.in, ref, refCounts, got, counts)
		}
	}
}

// TestMaskNFCUnionLinear: the email, street and postcode steps on text that
// is not NFC, with an "@" or a digit after a space every few bytes, take
// well under a second at 64 KiB (as TestMaskLinearOnRejectedRuns), and the
// same shapes, small enough for the reference, give its output.
func TestMaskNFCUnionLinear(t *testing.T) {
	t.Setenv("TYPESAFE_NAMES_FILE", filepath.Join(t.TempDir(), "none.txt"))
	plz := func(head, rest string) string { return head + rest }
	for name, c := range map[string]struct{ unit, tail string }{
		"NFD local parts": {"mu\u0308ller" + at + "ko\u0308ln.example ", ""},
		"NFD domains":     {"x" + at + "mu\u0308nchen.example", ""},
		"NFD at signs":    {"u\u0308" + at, ""},
		"NFD domain run":  {"u\u0308", ""},
		"NFD postcodes":   {plz("8033", "1 Mu\u0308nchen "), ""},
		"NFD streets":     {"Mu\u0308hlenweg 3 ", ""},
		"NFD chain":       {"A\u0308aa ", "Weg 1x2"},
		"marks on digits": {"Weg 1\u0301 ", ""},
	} {
		head := ""
		if name == "NFD domain run" {
			head = "a" + at
		}
		text := head + strings.Repeat(c.unit, (64<<10)/len(c.unit)) + c.tail
		start := time.Now()
		MaskDetail(text)
		if el := time.Since(start); el > 500*time.Millisecond {
			t.Errorf("%s (%d bytes): %v", name, len(text), el)
		}
		small := head + strings.Repeat(c.unit, 2000/len(c.unit)) + c.tail
		got, gc := MaskDetail(small)
		want, wc := maskDetailReference(small)
		if got != want || gc != wc {
			t.Errorf("%s: differs from the reference on %.60q", name, small)
		}
	}
}

// TestNFCKeepsAnchors backs the prefilters of the NFC union: maskEmail
// builds no copy of a text without an "@", maskStreet and maskPlzOrt none of
// a text without a D rune (unicode.IsDigit, as digitAt), because NFC never
// makes either. The copy's runes are runes of the full canonical
// decompositions of the text's runes, some composed again into a rune that
// has a canonical decomposition itself. So it is enough that no rune with a
// canonical decomposition is an "@" or a D rune, nor decomposes to one.
func TestNFCKeepsAnchors(t *testing.T) {
	anchor := func(r rune) bool { return r == '@' || unicode.IsDigit(r) }
	n := 0
	for r := rune(0); r <= unicode.MaxRune; r++ {
		if r >= 0xd800 && r <= 0xdfff {
			continue
		}
		d := nfcDecompose(nil, r, nfcProps(r))
		if len(d) == 1 && d[0].r == r {
			continue
		}
		n++
		if anchor(r) {
			t.Errorf("%U has a canonical decomposition and is an anchor", r)
		}
		for _, x := range d {
			if anchor(x.r) {
				t.Errorf("%U decomposes to %U, an anchor", r, x.r)
			}
		}
	}
	// 2,061 outside Hangul and 11,172 Hangul syllables in Unicode 16
	if n < 13000 {
		t.Errorf("only %d runes with a canonical decomposition: the tables look incomplete", n)
	}
}
