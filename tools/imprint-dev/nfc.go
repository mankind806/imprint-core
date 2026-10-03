package main

import (
	"slices"
	"strings"
	"unicode/utf8"
)

//go:generate python3 gen_nfc_tables.py -o nfc_tables.go

// Unicode Normalization Form C (UAX #15) with tables generated from Python's
// unicodedata (nfc_tables.go, Unicode nfcUnicodeVersion), so that a string
// normalises in Go exactly as unicodedata.normalize("NFC", ...) does in
// ts_common.py. The standard library has no normaliser, golang.org/x/text
// would be this module's first dependency, and Go's own unicode tables are a
// later Unicode version than Python's (17.0.0 in Go 1.27, 16.0.0 in Python
// 3.14). TestNFCPython* compare nfc with Python on every code point and on
// random and targeted sequences; TestNFCTablesFresh regenerates the tables.
//
// Text that is not valid UTF-8 is normalised piece by piece: each byte that
// does not start a valid UTF-8 sequence is passed through unchanged and
// separates the text before it from the text after it, as if it were a
// starter that composes with nothing (Python strings cannot hold such bytes).

// Hangul syllables, Unicode chapter 3.12 (gen_nfc_tables.py checks these
// values against unicodedata).
const (
	hangulSBase  = 0xac00
	hangulLBase  = 0x1100
	hangulVBase  = 0x1161
	hangulTBase  = 0x11a7
	hangulLCount = 19
	hangulVCount = 21
	hangulTCount = 28
	hangulNCount = hangulVCount * hangulTCount
	hangulSCount = hangulLCount * hangulNCount
)

// nfcProps returns r's properties (nfcProp* bits).
func nfcProps(r rune) uint16 {
	if r < 0 || r >= nfcPropsLimit {
		return 0
	}
	block := int(nfcStage1[r>>nfcBlockShift])
	return nfcStage2[block<<nfcBlockShift|int(r&(1<<nfcBlockShift-1))]
}

// canonicalCombiningClass returns r's canonical combining class (0 for a
// starter), as unicodedata.combining.
func canonicalCombiningClass(r rune) uint8 {
	return uint8(nfcProps(r) & nfcPropCCC)
}

// nfcBoundary reports whether a rune with properties p starts a new piece of
// normalisation: combining class 0 and NFC quick check Yes. Nothing before
// such a rune reorders or composes with it or anything after it
// (gen_nfc_tables.py asserts what this relies on).
func nfcBoundary(p uint16) bool {
	return p&(nfcPropCCC|nfcPropQC) == 0
}

// nfcQuickSpan scans s from byte i with the NFC quick check. It returns
// len(s) if s[i:] is NFC; otherwise the start of the last boundary rune
// before the first rune that may need normalising (or i if there is none),
// so that s[i:k] is NFC and nothing in it interacts with s[k:].
func nfcQuickSpan(s string, i int) int {
	boundary := i
	var last uint16 // combining class of the previous rune
	for i < len(s) {
		if s[i] < utf8.RuneSelf {
			i++
			for i < len(s) && s[i] < utf8.RuneSelf {
				i++
			}
			boundary, last = i-1, 0
			continue
		}
		r, w := utf8.DecodeRuneInString(s[i:])
		if w == 1 { // a byte >= 0x80 that does not start valid UTF-8
			return boundary
		}
		if r < nfcTrivialBelow {
			boundary, last = i, 0
			i += w
			continue
		}
		p := nfcProps(r)
		cc := p & nfcPropCCC
		switch {
		case p&nfcPropQC != 0:
			return boundary
		case cc == 0:
			boundary = i
		case cc < last:
			return boundary
		}
		last = cc
		i += w
	}
	return len(s)
}

// isNFC reports whether nfc(s) == s. It is quick for text the NFC quick
// check accepts (all of ASCII, and most text that is NFC), stops at the first
// piece that normalisation would change, and does not allocate.
func isNFC(s string) bool {
	var store [32]nfcRune
	for i := nfcQuickSpan(s, 0); i < len(s); {
		j, seg := nfcPiece(s, i, store[:0])
		for _, x := range seg {
			r, w := utf8.DecodeRuneInString(s[i:j])
			if w == 0 || r != x.r {
				return false
			}
			i += w
		}
		if i != j {
			return false
		}
		if j < len(s) && nfcInvalidAt(s, j) {
			j++
		}
		i = nfcQuickSpan(s, j)
	}
	return true
}

// nfc returns s in Unicode Normalization Form C. Bytes that are not valid
// UTF-8 are kept as they are (see above). When the NFC quick check accepts s
// (all of ASCII, and most text that is NFC) it returns s itself without
// allocating; otherwise it copies the parts that are NFC and normalises the
// pieces in between.
func nfc(s string) string {
	k := nfcQuickSpan(s, 0)
	if k == len(s) {
		return s
	}
	var b strings.Builder
	b.Grow(len(s) + len(s)/8)
	b.WriteString(s[:k])
	var store [32]nfcRune
	for i := k; i < len(s); {
		j, seg := nfcPiece(s, i, store[:0])
		for _, x := range seg {
			b.WriteRune(x.r)
		}
		if j < len(s) && nfcInvalidAt(s, j) {
			b.WriteByte(s[j])
			j++
		}
		i = nfcQuickSpan(s, j)
		b.WriteString(s[j:i])
	}
	return b.String()
}

// nfcPiece normalises s from byte i, where a piece starts (the start of s,
// a boundary rune, or the byte after an invalid one), up to the next boundary
// rune or invalid byte, at j. It returns j and the NFC of s[i:j], built in
// seg.
func nfcPiece(s string, i int, seg []nfcRune) (int, []nfcRune) {
	j := i
	for j < len(s) {
		r, w := utf8.DecodeRuneInString(s[j:])
		if r == utf8.RuneError && w == 1 {
			break
		}
		p := nfcProps(r)
		if j > i && nfcBoundary(p) {
			break
		}
		seg = nfcDecompose(seg, r, p)
		j += w
	}
	return j, nfcCompose(nfcReorder(seg))
}

// nfcInvalidAt reports whether byte j of s does not start a valid UTF-8
// sequence.
func nfcInvalidAt(s string, j int) bool {
	r, w := utf8.DecodeRuneInString(s[j:])
	return r == utf8.RuneError && w == 1
}

// nfcRune is a rune in a piece being normalised, with its properties.
type nfcRune struct {
	r rune
	p uint16
}

// nfcDecompose appends the full canonical decomposition of r (properties p)
// to seg.
func nfcDecompose(seg []nfcRune, r rune, p uint16) []nfcRune {
	if s := r - hangulSBase; s >= 0 && s < hangulSCount {
		l := hangulLBase + s/hangulNCount
		v := hangulVBase + s%hangulNCount/hangulTCount
		seg = append(seg, nfcRune{l, nfcProps(l)}, nfcRune{v, nfcProps(v)})
		if t := s % hangulTCount; t != 0 {
			seg = append(seg, nfcRune{hangulTBase + t, nfcProps(hangulTBase + t)})
		}
		return seg
	}
	if p&nfcPropDecomp == 0 {
		return append(seg, nfcRune{r, p})
	}
	i, _ := slices.BinarySearch(nfcDecompRunes[:], r)
	m := nfcDecomp[i]
	seg = nfcDecompose(seg, m[0], nfcProps(m[0]))
	if m[1] != 0 {
		seg = nfcDecompose(seg, m[1], nfcProps(m[1]))
	}
	return seg
}

// nfcReorder puts every run of non-starters in seg into canonical order: a
// stable sort by combining class.
func nfcReorder(seg []nfcRune) []nfcRune {
	for i := 0; i < len(seg); {
		if seg[i].p&nfcPropCCC == 0 {
			i++
			continue
		}
		j := i + 1
		for j < len(seg) && seg[j].p&nfcPropCCC != 0 {
			j++
		}
		run := seg[i:j]
		if !slices.IsSortedFunc(run, nfcCompareCCC) {
			slices.SortStableFunc(run, nfcCompareCCC)
		}
		i = j
	}
	return seg
}

func nfcCompareCCC(a, b nfcRune) int {
	return int(a.p&nfcPropCCC) - int(b.p&nfcPropCCC)
}

// nfcCompose applies the canonical composition algorithm of UAX #15 to seg
// (decomposed and in canonical order) in place.
func nfcCompose(seg []nfcRune) []nfcRune {
	if len(seg) == 0 {
		return seg
	}
	starter := -1 // index of the last starter in the output
	last := 256   // class of the last rune kept since it; 0 = none in between
	if seg[0].p&nfcPropCCC == 0 {
		starter, last = 0, 0
	}
	out := 1
	for k := 1; k < len(seg); k++ {
		x := seg[k]
		cc := int(x.p & nfcPropCCC)
		// Only a rune with quick check Maybe is the second of a composite.
		if starter >= 0 && x.p&nfcPropQC == nfcPropQCMaybe && (last == 0 || last < cc) {
			if c, ok := nfcComposePair(seg[starter].r, x.r); ok {
				// A primary composite has class 0, like the starter it replaces.
				seg[starter].r = c
				continue
			}
		}
		if cc == 0 {
			starter = out
		}
		last = cc
		seg[out] = x
		out++
	}
	return seg[:out]
}

// nfcComposePair returns the primary composite of a and b, if there is one.
func nfcComposePair(a, b rune) (rune, bool) {
	if l := a - hangulLBase; l >= 0 && l < hangulLCount {
		if v := b - hangulVBase; v >= 0 && v < hangulVCount {
			return hangulSBase + (l*hangulVCount+v)*hangulTCount, true
		}
	}
	if s := a - hangulSBase; s >= 0 && s < hangulSCount && s%hangulTCount == 0 {
		if t := b - hangulTBase; t > 0 && t < hangulTCount {
			return a + t, true
		}
	}
	lo, hi := 0, len(nfcComposeIndex)
	for lo < hi {
		m := int(uint(lo+hi) >> 1)
		d := nfcDecomp[nfcComposeIndex[m]]
		if d[0] < a || d[0] == a && d[1] < b {
			lo = m + 1
		} else {
			hi = m
		}
	}
	if lo < len(nfcComposeIndex) {
		i := nfcComposeIndex[lo]
		if d := nfcDecomp[i]; d[0] == a && d[1] == b {
			return nfcDecompRunes[i], true
		}
	}
	return 0, false
}
