package main

// maskDetailReference is MaskDetail exactly as it stood at 301db1c, before the
// speed-up: the same pattern strings (compiled here from copies, so a later
// change to a production pattern shows up as a difference), the same order of
// steps, the same closures. It is the oracle of TestMaskDifferential and exists
// only in tests. Steps changed on purpose since, each written here plainly and
// apart from the production code:
//   - names have Unicode word boundaries (refNamesStep; ts_common.py's parity
//     in TestNamePythonParity);
//   - 2026-10-02, mask parity spec sections 0, 1a, 1b and 3: Python's classes
//     (S, Unicode \w and \d), the Turkish i in every (?i) part (names,
//     keywords, Bearer/Basic, street suffixes), Bearer/Basic up to the next
//     ASCII space without a "<redacted>" skip (refBearerStep), key=value with
//     Python's (?!<redacted>) lookaheads and backtracking (refSecretKWStep),
//     phone digits of any script and W before them;
//   - 2026-10-02, section 4: the addresses are the union of Python's and Go's
//     grammar, Python's classes, Unicode \b, leftmost-longest
//     (refAddressStep); before: Go's street and postcode patterns with
//     ReplaceAllString;
//   - 2026-10-02, amendment A3: the (?!<redacted>) lookaheads of key=value
//     are case-sensitive (refSecretKWStep);
//   - 2026-10-02, section 5 with amendments A1, A2, A8: names are NFC with at
//     least two code points (refLoadNames); every valid UTF-8 text, NFC or
//     not, is matched on an NFC copy built segment by segment with nfc and
//     replaced in the original with per-byte provenance (refNamesStep);
//   - 2026-10-03, amendment A9: the opaque step is ts_common.py's RX_OPAQUE
//     tried position by position with both lookaheads (refOpaqueStep), so a
//     run of token characters may take its digit from the rune after it
//     (Unicode \d); before: a candidate regex and an ASCII digit inside.

import (
	"os"
	"regexp"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

// refS is S, Python's \s on str, spelled out by category: the ASCII
// controls \t..\r, U+001C..U+001F, U+0085 and the separators (Zs, Zl, Zp).
// TestMaskClasses checks it against unicode.IsSpace on every rune.
const refS = `[\t\n\v\f\r\x{1C}-\x{1F}\x{85}\p{Z}]`

// refFoldTurkish rewrites the literal letters of a case-insensitive pattern
// (no flags, no escapes holding an i) so that every I, i, U+0130 and U+0131
// in it matches all four, as Python's (?i) does.
func refFoldTurkish(p string) string {
	var b strings.Builder
	for _, r := range p {
		if r == 'I' || r == 'i' || r == '\u0130' || r == '\u0131' {
			b.WriteString(`[Ii\x{130}\x{131}]`)
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

var (
	// refBearerBasicRE: the trigger, S+, then up to the next ASCII space; the
	// lookbehind (no ASCII word character before) is checked in refBearerStep.
	refBearerBasicRE = regexp.MustCompile(`(?i)` + refFoldTurkish(`(?:bearer|basic)`) + refS + `+[^\t\n\f\r ]+`)
	refBearerSplitRE = regexp.MustCompile(`^(` + `(?i:` + refFoldTurkish(`bearer|basic`) + `)` + refS + `+)[^\t\n\f\r ]+$`)
	// refSecretKWHeadRE: ts_common.py's RX_KEY_VAL up to and including [=:].
	refSecretKWHeadRE = regexp.MustCompile(`^` + `(?i:` + refFoldTurkish(`api[_-]?key|token|secret|passw(?:or)?d|pass(?:phrase|wort)?|pwd|credential|private[_-]?key|access[_-]?key|auth(?:orization)?`) + `)` +
		`[\p{L}\p{N}_.-]*["']?` + refS + `*[=:]`)
	refIbanRE       = regexp.MustCompile(refIBANPattern)
	refIbanFullRE   = regexp.MustCompile(`^(?:` + refIBANPattern + `)$`)
	refPhoneRE      = regexp.MustCompile(`(?:\+49|0)(?:[ \t./()-]*\p{Nd}){8,}`)
	refAwsKeyIDRE   = regexp.MustCompile(`(?:AKIA|ASIA)[0-9A-Z]{16}`)
	refKnownTokenRE = regexp.MustCompile(`(?:ghp_|gho_|ghs_|github_pat_|sk-|sk_live_|sk_test_|rk_live_|rk_test_|pk_live_|xox[abprs]-|AKIA|ASIA|AIza|GOCSPX-|ya29\.|1//|eyJ|glpat-|npm_)[A-Za-z0-9_\-./+=]{8,}`)
	refEmailRE      = regexp.MustCompile(`[\p{L}\p{N}_.+-]+@[\p{L}\p{N}_-]+\.[\p{L}\p{N}_.-]+`)
)

// The address grammars of the mask parity spec, section 4 (2026-10-02), as
// the spec writes them: PyStreet, GoStreet, PyPlz, GoPlz, with <S> for S
// (refS) and <D> for D (\p{Nd}); the (?i) parts get refFoldTurkish.
var (
	refSufPy = `(?i:` + refFoldTurkish(`stra[ßs]e|strasse|str\.?|weg|gasse|platz|allee|ring|damm|ufer|chaussee|zeile|stieg|gässchen|pfad|markt`) + `)`
	refSufGo = `(?i:` + refFoldTurkish(`straße|strasse|str\.|str|weg|gasse|platz|allee|ring|ufer|damm|chaussee|zeile|pfad|steig|gäßchen|gaesschen`) + `)`
	refAddr  = strings.NewReplacer("<S>", refS, "<D>", `\p{Nd}`, "<SufPy>", refSufPy, "<SufGo>", refSufGo).Replace

	refPyStreet = refAddr(`(?:(?:[A-ZÄÖÜ][a-zäöüß]+(?:<S>+|-))*(?:[A-ZÄÖÜ][a-zäöüß]+)?<SufPy>|[a-zäöüß]+<SufPy>)` +
		`<S>+<D>+(?:<S>*[a-zA-Z])?(?:<S>*[-/]<S>*<D>{1,4}(?:<S>*[a-zA-Z])?)?`)
	refGoStreet = refAddr(`(?:(?:Am|An der|Auf dem|Auf der|Im|In der|Vor dem|Hinter dem|Zum|Zur)<S>+[A-ZÄÖÜ][a-zäöüßA-ZÄÖÜ0-9.-]+(?:<S>+[A-ZÄÖÜ][a-zäöüßA-ZÄÖÜ0-9.-]+)*` +
		`|(?:[A-ZÄÖÜ][a-zäöüßA-ZÄÖÜ0-9.-]+<S>+)*(?:Straße|Strasse|Str\.|Str|[A-ZÄÖÜ][a-zäöüßA-ZÄÖÜ0-9.-]*<SufGo>))` +
		`<S>+<D>+[a-zA-Z]?(?:<S>*[-/]<S>*<D>{1,4}[a-zA-Z]?)?`)
	refPyPlz = refAddr(`<D>{5}<S>+[A-ZÄÖÜ][a-zäöüß]+(?:[-/][A-ZÄÖÜ][a-zäöüß]+)*(?:<S>+(?:(?:am|an<S>+der|im)<S>+)?[A-ZÄÖÜ][a-zäöüß]+)?`)
	refGoPlz = refAddr(`<D>{5}<S>+[A-ZÄÖÜ][a-zäöüßA-ZÄÖÜ.-]+(?:<S>+(?:(?:am|an der)<S>+[A-ZÄÖÜ][a-zäöüßA-ZÄÖÜ.-]+|im Breisgau|ob der Tauber))?`)

	// refStreetRE, refPlzRE: the union, anchored at the start of the text
	// they are given, leftmost-longest.
	refStreetRE = refLongest(`^(?:(?:` + refPyStreet + `)|(?:` + refGoStreet + `))`)
	refPlzRE    = refLongest(`^(?:(?:` + refPyPlz + `)|(?:` + refGoPlz + `))`)
)

func refLongest(p string) *regexp.Regexp {
	re := regexp.MustCompile(p)
	re.Longest()
	return re
}

// refAddressStep is one address step as spec section 4 states it: (i, j) is
// a candidate if text[i:j] is in the grammar's language and Python's \b
// (refBoundary) holds at i and at j; the smallest i (not before the end of
// the last match) that has one wins, with its largest j; the search goes on
// at j. For a start i, the largest j is found from the longest match of the
// grammar in text[i:k], k = len(text) first: its end e is the largest e <= k
// with text[i:e] in the language (the grammars hold no \b, ^ or $ of their
// own), so if \b holds at e, e is the answer; if not, no end in (e-1 rune,
// k] is, and the search repeats with k = e minus its last rune.
func refAddressStep(text string, re *regexp.Regexp, counts *MaskCounts) string {
	var b strings.Builder
	done := 0
	for i := 0; i < len(text); {
		_, w := utf8.DecodeRuneInString(text[i:])
		if !refBoundary(text, i) {
			i += w
			continue
		}
		j := -1
		for k := len(text); j < 0; {
			loc := re.FindStringIndex(text[i:k])
			if loc == nil || loc[1] == 0 {
				break
			}
			e := i + loc[1]
			if refBoundary(text, e) {
				j = e
				break
			}
			_, lw := utf8.DecodeLastRuneInString(text[i:e])
			k = e - lw
		}
		if j < 0 {
			i += w
			continue
		}
		b.WriteString(text[done:i])
		b.WriteString("<address>")
		counts.Address++
		done, i = j, j
	}
	b.WriteString(text[done:])
	return b.String()
}

const refIBANPattern = `[A-Z]{2}\d{2}(?: ?[A-Z0-9]){11,30}`

// refLap, when a profiler sets refLapFn, reports that the named step has just
// ended; MaskDetail's own steps report through maskLap the same way.
var refLapFn func(step string)

func refLap(step string) {
	if refLapFn != nil {
		refLapFn(step)
	}
}

// refNamesStep is the names step as it stands since 2026-10-02 (mask parity
// spec section 5 with amendments A1, A2, A8; Unicode word boundaries as
// ts_common.py's Python \b), written plainly and apart from mask_fast.go and
// mask_nfc.go. Names come from refLoadNames. Text that is not valid UTF-8 is
// scanned as it is: for each name in order, every start that (?i)NAME (no
// \b; I, i, U+0130 and U+0131 rewritten to match each other) finds,
// overlapping ones too, is kept if a word boundary lies at both of its ends;
// the leftmost non-overlapping ones are replaced. Every other text, NFC or
// not, takes the general path, so that the differential test also checks
// that MaskDetail's fast path on NFC text is that path (A8):
//
//   - segments: a new one starts at the first rune and before every rune
//     refNFCBoundary accepts; the copy is nfc of each segment put together;
//   - every copy byte carries the original bytes [lo, hi) it stands for: its
//     own byte in a segment that nfc leaves unchanged, the whole segment in
//     one that it changes;
//   - the names run on the copy as above; the bytes of each "<name>" carry
//     the union of what the bytes it replaced carried, and an insertion id;
//     an insertion that replaced bytes of older ones is joined with them
//     (union-find), so the pieces of one placeholder stay together;
//   - at the end each run of placeholder bytes with one root is one
//     replacement of the union of their [lo, hi); one that overlaps the one
//     before is merged with it into one "<name>"; the rest of the original
//     is copied as it is.
//
// Up to 2026-10-02 the step ran on the text only, with names as loadNames
// read them (byte length >= 2, no NFC); before that it was (?i)\bNAME\b with
// Go's ASCII \b and Go's fold.
func refNamesStep(text string, counts *MaskCounts) string {
	names := refLoadNames(getNamesFilePath())
	if !utf8.ValidString(text) {
		for _, name := range names {
			re := regexp.MustCompile(`(?i)` + refFoldTurkish(regexp.QuoteMeta(name)))
			var b strings.Builder
			done := 0
			for p := 0; p < len(text); {
				loc := re.FindStringIndex(text[p:])
				if loc == nil {
					break
				}
				s, e := p+loc[0], p+loc[1]
				if refBoundary(text, s) && refBoundary(text, e) {
					b.WriteString(text[done:s])
					b.WriteString("<name>")
					counts.Name++
					done, p = e, e
					continue
				}
				_, w := utf8.DecodeRuneInString(text[s:])
				p = s + max(w, 1)
			}
			if done > 0 {
				b.WriteString(text[done:])
				text = b.String()
			}
		}
		return text
	}
	if len(names) == 0 {
		return text
	}
	var starts []int
	for i, r := range text {
		if i == 0 || refNFCBoundary(r) {
			starts = append(starts, i)
		}
	}
	starts = append(starts, len(text))
	var cp []byte
	var lo, hi, grp []int
	for k := 0; k+1 < len(starts); k++ {
		o, oe := starts[k], starts[k+1]
		n := nfc(text[o:oe])
		for b := 0; b < len(n); b++ {
			cp, grp = append(cp, n[b]), append(grp, -1)
			if n == text[o:oe] {
				lo, hi = append(lo, o+b), append(hi, o+b+1)
			} else {
				lo, hi = append(lo, o), append(hi, oe)
			}
		}
	}
	var parent []int
	find := func(x int) int {
		for parent[x] != x {
			x = parent[x]
		}
		return x
	}
	for _, name := range names {
		re := regexp.MustCompile(`(?i)` + refFoldTurkish(regexp.QuoteMeta(name)))
		cur := string(cp)
		var ncp []byte
		var nlo, nhi, ngrp []int
		done := 0
		for p := 0; p < len(cur); {
			loc := re.FindStringIndex(cur[p:])
			if loc == nil {
				break
			}
			s, e := p+loc[0], p+loc[1]
			if refBoundary(cur, s) && refBoundary(cur, e) {
				ncp, nlo, nhi, ngrp = append(ncp, cp[done:s]...), append(nlo, lo[done:s]...), append(nhi, hi[done:s]...),
					append(ngrp, grp[done:s]...)
				id := len(parent)
				parent = append(parent, id)
				a, z := lo[s], hi[s]
				for k := s; k < e; k++ {
					a, z = min(a, lo[k]), max(z, hi[k])
					if grp[k] >= 0 {
						parent[find(grp[k])] = id
					}
				}
				for k := 0; k < len("<name>"); k++ {
					ncp, nlo, nhi, ngrp = append(ncp, "<name>"[k]), append(nlo, a), append(nhi, z), append(ngrp, id)
				}
				counts.Name++
				done, p = e, e
				continue
			}
			_, w := utf8.DecodeRuneInString(cur[s:])
			p = s + max(w, 1)
		}
		if done > 0 {
			cp, lo, hi, grp = append(ncp, cp[done:]...), append(nlo, lo[done:]...), append(nhi, hi[done:]...), append(ngrp, grp[done:]...)
		}
	}
	type rep struct {
		a, z int
		text string
	}
	var reps []rep
	for i := 0; i < len(cp); {
		if grp[i] < 0 {
			i++
			continue
		}
		root, a, z, j := find(grp[i]), lo[i], hi[i], i
		for j < len(cp) && grp[j] >= 0 && find(grp[j]) == root {
			a, z = min(a, lo[j]), max(z, hi[j])
			j++
		}
		if n := len(reps); n > 0 && a < reps[n-1].z {
			reps[n-1].z, reps[n-1].text = max(reps[n-1].z, z), "<name>"
		} else {
			reps = append(reps, rep{a, z, string(cp[i:j])})
		}
		i = j
	}
	var b strings.Builder
	done := 0
	for _, r := range reps {
		b.WriteString(text[done:r.a])
		b.WriteString(r.text)
		done = r.z
	}
	b.WriteString(text[done:])
	return b.String()
}

// refQCMaybeStarters is ts_common.py's _NFC_QC_MAYBE_STARTERS (typesafe-dev
// 752698b, 2026-10-02): the runes of canonical combining class 0 with
// NFC_Quick_Check Maybe in Unicode 16.0.0, copied as a literal so that the
// reference's segment rule does not read the quick check bits of
// nfc_tables.go (TestRefQCMaybeStarters compares the two).
var refQCMaybeStarters = func() map[rune]bool {
	m := map[rune]bool{}
	for _, r := range []rune{
		0x09BE, 0x09D7, 0x0B3E, 0x0B56, 0x0B57, 0x0BBE, 0x0BD7, 0x0CC2, 0x0CD5, 0x0CD6,
		0x0D3E, 0x0D57, 0x0DCF, 0x0DDF, 0x102E,
		0x1B35, 0x11127, 0x1133E, 0x11357, 0x113B8, 0x113BB, 0x113C2, 0x113C5, 0x113C7,
		0x113C8, 0x113C9, 0x114B0, 0x114BA, 0x114BD, 0x115AF, 0x11930, 0x16D67, 0x16D68,
	} {
		m[r] = true
	}
	for _, rg := range [][2]rune{{0x1161, 0x1176}, {0x11A8, 0x11C3}, {0x1611E, 0x1612A}} {
		for r := rg[0]; r < rg[1]; r++ {
			m[r] = true
		}
	}
	return m
}()

// refNFCBoundary: a new segment starts before r (amendment A1): combining
// class 0 and NFC_Quick_Check Yes, which for class 0 is: not Maybe
// (refQCMaybeStarters) and not No (nfc(r) == r, as ts_common.py tests it).
func refNFCBoundary(r rune) bool {
	return canonicalCombiningClass(r) == 0 && !refQCMaybeStarters[r] && nfc(string(r)) == string(r)
}

// refLoadNames is loadNames written plainly (mask parity spec section 5,
// amendment A2): per line, the part before "#", read as Latin-1 if it is not
// valid UTF-8, trimmed, in NFC; lines without a non-control rune are
// skipped; the line's fields joined by one space if there are several, and
// each field with at least two code points; longest (in bytes) first, then
// in byte order.
func refLoadNames(path string) []string {
	if path == "" {
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	set := map[string]bool{}
	for _, line := range strings.Split(string(data), "\n") {
		if k := strings.IndexByte(line, '#'); k >= 0 {
			line = line[:k]
		}
		if !utf8.ValidString(line) {
			rs := make([]rune, len(line))
			for i := 0; i < len(line); i++ {
				rs[i] = rune(line[i])
			}
			line = string(rs)
		}
		line = nfc(strings.TrimSpace(line))
		visible := false
		for _, r := range line {
			visible = visible || !unicode.IsControl(r)
		}
		if !visible {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) > 1 {
			set[strings.Join(fields, " ")] = true
		}
		for _, f := range fields {
			if len([]rune(f)) >= 2 {
				set[f] = true
			}
		}
	}
	var names []string
	for n := range set {
		names = append(names, n)
	}
	sort.Slice(names, func(i, j int) bool {
		if len(names[i]) != len(names[j]) {
			return len(names[i]) > len(names[j])
		}
		return names[i] < names[j]
	})
	return names
}

// refBoundary: a word character on exactly one side of byte i, a word
// character being a letter, a number or "_" (Python's str.isalnum() or "_").
func refBoundary(text string, i int) bool {
	isWord := func(r rune) bool { return r == '_' || unicode.IsLetter(r) || unicode.IsNumber(r) }
	before, after := false, false
	if i > 0 {
		r, _ := utf8.DecodeLastRuneInString(text[:i])
		before = isWord(r)
	}
	if i < len(text) {
		r, _ := utf8.DecodeRuneInString(text[i:])
		after = isWord(r)
	}
	return before != after
}

// replaceBounded replaces the matches of re that pass ok(prev, next), the runes
// before and after the match (utf8.RuneError at the text edges). A rejected match
// is retried one rune later, as a lookbehind would.
func refReplaceBounded(text string, re *regexp.Regexp, ok func(prev, next rune) bool, repl func(m string) string) string {
	return refReplaceBoundedShrink(text, re, nil, ok, repl)
}

// replaceBoundedShrink is replaceBounded for a greedy pattern: when the rune after
// a match fails ok, the end steps back while full still matches the shorter text,
// as Python backtracks before a lookahead ("DE89 ... 00 Bank" ends before " B").
func refReplaceBoundedShrink(text string, re, full *regexp.Regexp, ok func(prev, next rune) bool, repl func(m string) string) string {
	var b strings.Builder
	pos, done := 0, 0
	for pos <= len(text) {
		loc := re.FindStringIndex(text[pos:])
		if loc == nil {
			break
		}
		s, e := pos+loc[0], pos+loc[1]
		prev, next := utf8.RuneError, utf8.RuneError
		if s > 0 {
			prev, _ = utf8.DecodeLastRuneInString(text[:s])
		}
		if e < len(text) {
			next, _ = utf8.DecodeRuneInString(text[e:])
		}
		if e > s && !ok(prev, next) && full != nil {
			for e2 := e - 1; e2 > s; e2-- {
				if !utf8.RuneStart(text[e2]) {
					continue
				}
				n2, _ := utf8.DecodeRuneInString(text[e2:])
				if full.MatchString(text[s:e2]) && ok(prev, n2) {
					e, next = e2, n2
					break
				}
			}
		}
		if e > s && ok(prev, next) {
			b.WriteString(text[done:s])
			b.WriteString(repl(text[s:e]))
			done, pos = e, e
			continue
		}
		_, size := utf8.DecodeRuneInString(text[s:])
		if size == 0 {
			break
		}
		pos = s + size
	}
	b.WriteString(text[done:])
	return b.String()
}

// refBearerStep is ts_common.py's RX_BEARER as the spec has it (section 1a):
// (?<![A-Za-z0-9_])(?i:bearer|basic)S+[^\t\n\f\r ]+, the lookbehind checked
// as refReplaceBounded checks one, the trigger and S+ kept, the rest
// replaced and counted, "<redacted>" too.
func refBearerStep(text string, counts *MaskCounts) string {
	asciiWord := func(r rune) bool { return r == '_' || (r < 0x80 && isASCIIAlnum(r)) }
	return refReplaceBounded(text, refBearerBasicRE, func(prev, _ rune) bool { return !asciiWord(prev) }, func(m string) string {
		counts.SecretKW++
		return refBearerSplitRE.FindStringSubmatch(m)[1] + "<redacted>"
	})
}

// refSecretKWStep is ts_common.py's RX_KEY_VAL (spec section 1b) as a
// backtracking matcher: at each start, left to right, the regexp up to [=:]
// (its end is the same whichever keyword alternative matched: the tail runs
// to the end of the [\w.-] run), then the S* after it from the longest to
// none, and at each length the value alternatives in order:
//
//	"(?!<redacted>")[^"\n]+"  '(?!<redacted>')[^'\n]+'  ["']?(?!<redacted>)[^\t\n\f\r "',;]+
//
// with the lookaheads case-sensitive (amendment A3, 2026-10-02: only the
// exact "<redacted>" is passed over; Python scopes them with (?-i:...); until
// then they compared case-insensitively, as under Python's (?i)). The first
// that fits is the match; with none, the next start is tried.
func refSecretKWStep(text string, counts *MaskCounts) string {
	isS := func(r rune) bool {
		return unicode.Is(unicode.Z, r) || (r >= '\t' && r <= '\r') || (r >= 0x1c && r <= 0x1f) || r == 0x85
	}
	// value tries the alternatives at v: the end of the match and its
	// replacement, or ok false.
	value := func(v int) (int, string, bool) {
		if v >= len(text) {
			return 0, "", false
		}
		for _, q := range []string{`"`, `'`} {
			if text[v] != q[0] || strings.HasPrefix(text[v+1:], "<redacted>"+q) {
				continue
			}
			c := v + 1
			for c < len(text) && text[c] != q[0] && text[c] != '\n' {
				c++
			}
			if c > v+1 && c < len(text) && text[c] == q[0] {
				return c + 1, q + "<redacted>" + q, true
			}
		}
		bare := func(b int) (int, bool) {
			if strings.HasPrefix(text[b:], "<redacted>") {
				return 0, false
			}
			e := b
			for e < len(text) {
				r, w := utf8.DecodeRuneInString(text[e:])
				if r == '\t' || r == '\n' || r == '\f' || r == '\r' || r == ' ' || r == '"' || r == '\'' || r == ',' || r == ';' {
					break
				}
				e += w
			}
			return e, e > b
		}
		if text[v] == '"' || text[v] == '\'' {
			if e, ok := bare(v + 1); ok {
				return e, "<redacted>", true
			}
		}
		if e, ok := bare(v); ok {
			return e, "<redacted>", true
		}
		return 0, "", false
	}
	var b strings.Builder
	done := 0
	for s := 0; s < len(text); {
		if loc := refSecretKWHeadRE.FindStringIndex(text[s:]); loc != nil {
			h := s + loc[1]
			ends := []int{h} // the ends of S* after [=:], shortest first
			for p := h; p < len(text); {
				r, w := utf8.DecodeRuneInString(text[p:])
				if !isS(r) {
					break
				}
				p += w
				ends = append(ends, p)
			}
			matched := false
			for k := len(ends) - 1; k >= 0 && !matched; k-- {
				if e, repl, ok := value(ends[k]); ok {
					b.WriteString(text[done:s])
					b.WriteString(text[s:ends[k]] + repl)
					counts.SecretKW++
					done, s, matched = e, e, true
				}
			}
			if matched {
				continue
			}
		}
		_, w := utf8.DecodeRuneInString(text[s:])
		s += w
	}
	b.WriteString(text[done:])
	return b.String()
}

func maskDetailReference(text string) (string, MaskCounts) {
	var counts MaskCounts

	// 1. Bearer / Basic
	text = refBearerStep(text, &counts)

	refLap("bearer")

	// 2. Secret keywords with = or :; a quoted value is masked as a whole.
	text = refSecretKWStep(text, &counts)

	refLap("secret_kw")

	// 3. email
	text = refEmailRE.ReplaceAllStringFunc(text, func(m string) string {
		counts.Email++
		return "<email>"
	})

	refLap("email")

	// 4. IBAN (no letter or digit directly before or after)
	text = refReplaceBoundedShrink(text, refIbanRE, refIbanFullRE, func(prev, next rune) bool {
		return !isASCIIAlnum(prev) && !isASCIIAlnum(next)
	}, func(string) string {
		counts.IBAN++
		return "<iban>"
	})

	refLap("iban")

	// 5. German phone numbers: +49 or a leading 0, then 8+ digits (no word
	// character, "+" or "." directly before; Python's \w and \d).
	text = refReplaceBounded(text, refPhoneRE, func(prev, next rune) bool {
		return !(unicode.In(prev, unicode.L, unicode.N) || prev == '_' || prev == '+' || prev == '.') &&
			!unicode.Is(unicode.Nd, next)
	}, func(string) string {
		counts.Phone++
		return "<phone>"
	})

	refLap("phone")

	// 6. address: street + house number (since 2026-10-02 the union grammar,
	// leftmost-longest, refAddressStep)
	text = refAddressStep(text, refStreetRE, &counts)

	refLap("street")

	// 7. address: postcode + place, over the street step's result
	text = refAddressStep(text, refPlzRE, &counts)

	refLap("plz")

	// 8. names: from TYPESAFE_NAMES_FILE or ~/.config/typesafe/names.txt
	// Case-insensitive to match ts_common.py's get_name_regex ((?i)), e.g.
	// "max mustermann" lowercase must also be masked.
	text = refNamesStep(text, &counts)

	refLap("names")

	// 9. opaque: AWS key IDs and known token prefixes (not inside a word), then
	// ts_common.py's RX_OPAQUE (since 2026-10-03, amendment A9, refOpaqueStep;
	// before: 24+ chars with at least one ASCII digit and one letter).
	opaque := func(string) string {
		counts.Opaque++
		return "<redacted>"
	}
	text = refReplaceBounded(text, refAwsKeyIDRE, func(prev, next rune) bool {
		return !isASCIIAlnum(prev) && !isASCIIAlnum(next)
	}, opaque)
	refLap("aws")
	text = refReplaceBounded(text, refKnownTokenRE, func(prev, _ rune) bool { return !isASCIIAlnum(prev) }, opaque)
	refLap("known")
	text = refOpaqueStep(text, &counts)

	refLap("opaque")
	return text, counts
}

// refOpaqueStep is ts_common.py's RX_OPAQUE under re.subn, written out as
// Python's engine runs it (mask parity spec amendment A9, 2026-10-03):
//
//	(?=[A-Za-z0-9_\-+/]*\d)(?=[A-Za-z0-9_\-+/]*[A-Za-z])[A-Za-z0-9_\-+/]{24,}={0,2}
//
// tried at every position from left to right, \d being Python's (Unicode
// Nd, so the digit may be the rune after the token characters); a match
// is replaced and the search goes on at its end, a failure moves one rune
// on. Each lookahead holds if some position q at or after p, with only
// token characters between, has the wanted rune (the backtracking of the
// greedy [...]*). Quadratic on long runs without a letter or a digit; the
// reference only runs in tests.
func refOpaqueStep(text string, counts *MaskCounts) string {
	isLetter := func(r rune) bool { return 'a' <= r && r <= 'z' || 'A' <= r && r <= 'Z' }
	isTok := func(r rune) bool {
		return isLetter(r) || '0' <= r && r <= '9' || r == '_' || r == '-' || r == '+' || r == '/'
	}
	isNd := func(r rune) bool { return unicode.Is(unicode.Nd, r) }
	ahead := func(p int, want func(rune) bool) bool {
		for q := p; q < len(text); {
			r, w := utf8.DecodeRuneInString(text[q:])
			if want(r) {
				return true
			}
			if !isTok(r) {
				return false
			}
			q += w
		}
		return false
	}
	var b strings.Builder
	done := 0
	for p := 0; p < len(text); {
		if ahead(p, isNd) && ahead(p, isLetter) {
			e := p
			for e < len(text) && isTok(rune(text[e])) {
				e++
			}
			if e-p >= 24 {
				for k := 0; k < 2 && e < len(text) && text[e] == '='; k++ {
					e++
				}
				b.WriteString(text[done:p])
				b.WriteString("<redacted>")
				counts.Opaque++
				done, p = e, e
				continue
			}
		}
		_, w := utf8.DecodeRuneInString(text[p:])
		p += w
	}
	b.WriteString(text[done:])
	return b.String()
}
