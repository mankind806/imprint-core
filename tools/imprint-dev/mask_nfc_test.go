package main

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
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
