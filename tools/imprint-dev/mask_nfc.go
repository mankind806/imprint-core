package main

import (
	"sort"
	"strings"
	"unicode/utf8"
)

// The names step on text that is not in Unicode NFC (mask parity spec,
// section 5 with amendments A1, A2 and A8, 2026-10-02), as ts_common.py's
// _mask_names does it:
//
//   - Names are in NFC (loadNamesDetail). Text that is NFC (nfc(text) ==
//     text: all of ASCII and most other text) is matched as it is, the way
//     it was before (replaceNames).
//   - Other text is cut into segments: a segment starts at the first rune
//     and before every rune with canonical combining class 0 and NFC quick
//     check Yes (nfcBoundary, amendment A1). Nothing reorders or composes
//     across such a rune, so the NFC of the text is the NFC of each segment
//     put together (the copy), and finding the segments is linear.
//   - The names are matched on the copy with the same machinery, name by
//     name, longest first, Unicode word boundaries evaluated on the copy.
//   - Each match maps back to the original: in a segment that NFC leaves
//     unchanged, offsets map one-to-one; a match edge inside a segment that
//     NFC changes is widened to that segment's edge (A8: only there). So the
//     result for a span depends only on its own segments, and text that is
//     NFC consists of unchanged segments only: the copy path gives exactly
//     what the fast path gives (maskDetailReference runs the copy path on
//     every text and TestMaskDifferential compares).
//   - Widened spans that overlap become one "<name>"; every match counts.
//     Everything outside the spans is copied from the original byte for
//     byte (a decomposed word next to a name stays decomposed).
//   - Text that is not valid UTF-8: no NFC, name by name as before.

// maskNames is MaskDetail's names step; repl is called once per match and
// returns the placeholder.
func maskNames(text string, matchers []nameMatcher, repl func(string) string) string {
	if len(matchers) == 0 {
		return text
	}
	if !utf8.ValidString(text) {
		// Text that is not valid UTF-8: name by name, an invalid byte read
		// as U+FFFD (no word character), as the regexp package reads it.
		for _, nm := range matchers {
			text = nm.replace(text, repl, nil)
		}
		return text
	}
	// Text that is NFC: the quick check accepts most of it at once; for the
	// rest the copy is built and compared, which is isNFC without
	// normalising twice.
	if nfcQuickSpan(text, 0) == len(text) {
		return replaceNames(text, matchers, repl, nil)
	}
	view := newNFCView(text)
	if view.copy == text {
		return replaceNames(text, matchers, repl, nil)
	}
	tr := nameTrack{}
	cp := replaceNames(view.copy, matchers, repl, tr.pass)
	return tr.output(text, cp, &view)
}

// replaceNames replaces every name's matches in text, name after name.
// One fold-canonical copy serves every name and is kept in step with each
// name's replacements. A text with a rune whose canonical form has another
// length is scanned name by name instead. If pass is not nil, it gets each
// name's replacements (offsets in the text before that name's pass).
func replaceNames(text string, matchers []nameMatcher, repl func(string) string, pass func([]nameRep)) string {
	folded, ok := foldCanon(text)
	var reps []nameRep
	var rec *[]nameRep
	if pass != nil {
		rec = &reps
	}
	for _, nm := range matchers {
		reps = reps[:0]
		if ok && nm.foldedOK {
			text, folded, ok = nm.replaceFolded(text, folded, repl, rec)
		} else if next := nm.replace(text, repl, rec); next != text {
			text = next
			if ok {
				folded, ok = foldCanon(text)
			}
		}
		if pass != nil && len(reps) > 0 {
			pass(reps)
		}
	}
	return text
}

// nfcView is the NFC copy of a valid UTF-8 text and the map back to it.
type nfcView struct {
	copy   string
	blocks []nfcBlock // in copy order, one after another
}

// nfcBlock maps copy bytes [c, next block's c) to original bytes [o, oe):
// one to one if ident (NFC leaves that text unchanged: one or more segments),
// else as a whole (one segment that NFC changes).
type nfcBlock struct {
	c, o, oe int
	ident    bool
}

// newNFCView builds the copy segment by segment, the way nfc does: the
// quick check skips text that is NFC (one identity block), and nfcPiece
// normalises one segment from a boundary rune up to the next. A segment
// whose NFC has the same runes is an identity block too (A8).
func newNFCView(s string) nfcView {
	var b strings.Builder
	b.Grow(len(s))
	var blocks []nfcBlock
	ident := func(i, j int) {
		if i >= j {
			return
		}
		if n := len(blocks); n > 0 && blocks[n-1].ident && blocks[n-1].oe == i {
			blocks[n-1].oe = j
		} else {
			blocks = append(blocks, nfcBlock{b.Len(), i, j, true})
		}
		b.WriteString(s[i:j])
	}
	var store [32]nfcRune
	for i := 0; i < len(s); {
		k := nfcQuickSpan(s, i)
		ident(i, k)
		if k == len(s) {
			break
		}
		j, seg := nfcPiece(s, k, store[:0])
		if sameRunes(s[k:j], seg) {
			ident(k, j)
		} else {
			blocks = append(blocks, nfcBlock{b.Len(), k, j, false})
			for _, x := range seg {
				b.WriteRune(x.r)
			}
		}
		i = j
	}
	return nfcView{b.String(), blocks}
}

// sameRunes reports whether s holds exactly the runes of seg.
func sameRunes(s string, seg []nfcRune) bool {
	i := 0
	for _, x := range seg {
		r, w := utf8.DecodeRuneInString(s[i:])
		if w == 0 || r != x.r {
			return false
		}
		i += w
	}
	return i == len(s)
}

// block returns the block that holds copy byte c.
func (v *nfcView) block(c int) nfcBlock {
	k := sort.Search(len(v.blocks), func(i int) bool { return v.blocks[i].c > c })
	return v.blocks[k-1]
}

// start maps the first copy byte c of a match to where the match starts in
// the original: the same byte in an identity block, else the segment's
// start.
func (v *nfcView) start(c int) int {
	bl := v.block(c)
	if bl.ident {
		return bl.o + c - bl.c
	}
	return bl.o
}

// end maps the last copy byte c of a match to where the match ends in the
// original: after the same byte in an identity block, else the segment's
// end.
func (v *nfcView) end(c int) int {
	bl := v.block(c)
	if bl.ident {
		return bl.o + c - bl.c + 1
	}
	return bl.oe
}

// nameTrack follows the replacements of the name passes on the copy. Copy0
// is the copy as built; ins are the placeholders in the current copy, in
// order: bytes [cs, ce) of it stand for bytes [s, e) of copy0. A later name
// may match across a placeholder (a name "name" inside "<name>"): its
// replacement stands for all of copy0 that the bytes it replaced stood for,
// and what is left of the older placeholder keeps its span, so pieces of
// one placeholder are contiguous and their spans overlap. Bytes outside
// every placeholder are copy0's, in order.
type nameTrack struct {
	ins []nameIns
}

type nameIns struct{ cs, ce, s, e int }

// span returns the bytes of copy0 that byte x of the current copy stands
// for.
func (t *nameTrack) span(x int) (int, int) {
	k := sort.Search(len(t.ins), func(i int) bool { return t.ins[i].ce > x })
	if k < len(t.ins) && t.ins[k].cs <= x {
		return t.ins[k].s, t.ins[k].e
	}
	if k == 0 {
		return x, x + 1
	}
	r := t.ins[k-1]
	c := r.e + x - r.ce
	return c, c + 1
}

// pass applies one name's replacements (offsets in the copy before them,
// in order, not overlapping).
func (t *nameTrack) pass(reps []nameRep) {
	out := make([]nameIns, 0, len(t.ins)+len(reps))
	shift, ri := 0, 0
	for _, m := range reps {
		s0, _ := t.span(m.p)
		_, e0 := t.span(m.e - 1)
		for ri < len(t.ins) && t.ins[ri].cs < m.e {
			r := t.ins[ri]
			if r.ce <= m.p {
				out = append(out, nameIns{r.cs + shift, r.ce + shift, r.s, r.e})
				ri++
				continue
			}
			if r.cs < m.p { // its head stays before the match
				out = append(out, nameIns{r.cs + shift, m.p + shift, r.s, r.e})
			}
			if r.ce > m.e { // its tail stays after the match
				t.ins[ri].cs = m.e
				break
			}
			ri++
		}
		out = append(out, nameIns{m.p + shift, m.p + shift + m.n, s0, e0})
		shift += m.n - (m.e - m.p)
	}
	for ; ri < len(t.ins); ri++ {
		r := t.ins[ri]
		out = append(out, nameIns{r.cs + shift, r.ce + shift, r.s, r.e})
	}
	t.ins = out
}

// output replaces in the original text what the placeholders of the final
// copy cp stand for. The pieces of one placeholder (contiguous, overlapping
// spans) are one replacement with their text; its copy0 span maps to the
// original through the view, widened inside changed segments. A replacement
// whose span overlaps the one before merges with it into one "<name>".
func (t *nameTrack) output(orig, cp string, v *nfcView) string {
	if len(t.ins) == 0 {
		return orig
	}
	type repl struct {
		a, b int
		text string
	}
	var reps []repl
	for i := 0; i < len(t.ins); {
		s, e := t.ins[i].s, t.ins[i].e
		j := i + 1
		for j < len(t.ins) && t.ins[j].cs == t.ins[j-1].ce && t.ins[j].s < e {
			s, e = min(s, t.ins[j].s), max(e, t.ins[j].e)
			j++
		}
		a, b := v.start(s), v.end(e-1)
		if n := len(reps); n > 0 && a < reps[n-1].b {
			reps[n-1].b = max(reps[n-1].b, b)
			reps[n-1].text = "<name>"
		} else {
			reps = append(reps, repl{a, b, cp[t.ins[i].cs:t.ins[j-1].ce]})
		}
		i = j
	}
	var out strings.Builder
	out.Grow(len(orig))
	done := 0
	for _, r := range reps {
		out.WriteString(orig[done:r.a])
		out.WriteString(r.text)
		done = r.b
	}
	out.WriteString(orig[done:])
	return out.String()
}
