package main

import (
	"regexp"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"
)

// The masking steps of MaskDetail, made fast without changing what they mask.
//
// Go's regexp has no DFA: an unanchored search runs its NFA over every byte,
// and the masking patterns cost up to 0.5 µs per byte each (measured
// 2026-10-02, docs/judge.md). Each step here finds the same matches as the
// unanchored pattern, in the same order, from cheap candidate positions:
//
//   - A match can only start where a necessary condition holds (a keyword's
//     first letters, an "@", two capitals and two digits, ...). Candidates are
//     tried left to right with the pattern anchored at the candidate, seeing
//     the rune before it as context ("^(?s:.)(?:pattern)"), so \b behaves as in
//     the full text. The first candidate that matches is the leftmost match,
//     and the anchored leftmost-first match there is the same match.
//   - Where a step rejects a match by the rune before it (replaceBounded's
//     lookbehind), candidates with such a rune are skipped without matching;
//     the old step found, rejected and retried them one rune later, rescanning
//     long digit runs each time (quadratic).
//   - Text the fast path cannot treat exactly falls back to the old regex
//     step: invalid UTF-8, and, for case-insensitive steps, the runes that
//     fold onto ASCII letters (U+212A Kelvin sign for k, U+017F long s for s,
//     and since 2026-10-02 U+0130 and U+0131 for i, which Python's (?i) folds
//     and Go's does not, see turkishI) or onto ß (U+1E9E).
//   - The address steps (since 2026-10-02, the union grammar) have no regex
//     to fall back to: mask_address.go runs automata built from the
//     grammars on every text, these runes and invalid UTF-8 included.
//
// maskDetailReference (a test-only copy of the old MaskDetail) is the oracle:
// TestMaskDifferential compares both on a large generated corpus.

const (
	runeKelvin    = "\u212a"
	runeLongS     = "\u017f"
	runeCapSharp  = "\u1e9e"
	runeCapIDot   = "\u0130" // İ
	runeDotlessI  = "\u0131" // ı
	turkishILead  = 0xc4     // the first byte of both
	turkishIFirst = 'I'      // the smallest rune of their (?i) class
)

// hasTurkishI reports whether s holds U+0130 or U+0131.
func hasTurkishI(s string) bool {
	return strings.Contains(s, runeCapIDot) || strings.Contains(s, runeDotlessI)
}

// isTurkishI: r is in Python's (?i) class of i: I, i, U+0130, U+0131.
func isTurkishI(r rune) bool { return r == 'I' || r == 'i' || r == 0x130 || r == 0x131 }

// isPySpace is S, Python's \s on str: unicode.IsSpace plus U+001C..U+001F.
func isPySpace(r rune) bool { return unicode.IsSpace(r) || (r >= 0x1c && r <= 0x1f) }

// anchoredRE matches a pattern exactly at a position, with the rune before
// it as context for \b.
type anchoredRE struct {
	at  *regexp.Regexp // ^(?:p), for position 0
	ctx *regexp.Regexp // ^(?s:.)(?:p), for a position after a rune
}

func newAnchoredRE(p string) anchoredRE {
	return anchoredRE{
		at:  regexp.MustCompile(`^(?:` + p + `)`),
		ctx: regexp.MustCompile(`^(?s:.)(?:` + p + `)`),
	}
}

// matchAt returns the end of the leftmost-first match of the pattern that
// starts at byte i of text, if there is one. limit > 0 bounds how far past i
// the pattern may read (for patterns whose matches are short and that need no
// look at the text after them).
func (a anchoredRE) matchAt(text string, i, limit int) (int, bool) {
	end := len(text)
	if limit > 0 && i+limit < end {
		end = i + limit
	}
	if i == 0 {
		loc := a.at.FindStringIndex(text[:end])
		if loc == nil {
			return 0, false
		}
		return loc[1], true
	}
	_, w := utf8.DecodeLastRuneInString(text[:i])
	loc := a.ctx.FindStringIndex(text[i-w : end])
	if loc == nil {
		return 0, false
	}
	return i - w + loc[1], true
}

var (
	bearerAt     = newAnchoredRE(bearerBasicPattern)
	secretKWAt   = newAnchoredRE(secretKWPattern)
	secretKWHead = regexp.MustCompile(`^(?i:` + secretKWKeywords + `)`)
	emailAt      = newAnchoredRE(emailPattern)
	ibanAt       = newAnchoredRE(ibanPattern)
	phoneAt      = newAnchoredRE(phonePattern)
	awsKeyIDAt   = newAnchoredRE(awsKeyIDPattern)
	knownTokenAt = newAnchoredRE(knownTokenPattern)
)

// replaceFound replaces the successive non-overlapping matches that next
// returns (next(pos) gives the leftmost match starting at or after pos), as
// regexp's ReplaceAllStringFunc does for a pattern that never matches empty.
func replaceFound(text string, next func(pos int) (s, e int, ok bool), repl func(string) string) string {
	var b strings.Builder
	pos, done := 0, 0
	for pos < len(text) {
		s, e, ok := next(pos)
		if !ok {
			break
		}
		b.WriteString(text[done:s])
		b.WriteString(repl(text[s:e]))
		done, pos = e, e
	}
	if done == 0 {
		return text
	}
	b.WriteString(text[done:])
	return b.String()
}

func isASCIILetter(c byte) bool { return (c|0x20) >= 'a' && (c|0x20) <= 'z' }
func isASCIIDigit(c byte) bool  { return c >= '0' && c <= '9' }
func isASCIIWordByte(c byte) bool {
	return isASCIILetter(c) || isASCIIDigit(c) || c == '_'
}

// Go's \s: tab, newline, form feed, carriage return, space.
func isRESpace(c byte) bool { return c == '\t' || c == '\n' || c == '\f' || c == '\r' || c == ' ' }

// hasPrefixFoldASCII reports whether s starts with the lower-case ASCII word
// w, ignoring ASCII case.
func hasPrefixFoldASCII(s, w string) bool {
	if len(s) < len(w) {
		return false
	}
	for i := 0; i < len(w); i++ {
		if s[i]|0x20 != w[i] {
			return false
		}
	}
	return true
}

func prevRune(text string, i int) rune {
	if i == 0 {
		return utf8.RuneError
	}
	r, _ := utf8.DecodeLastRuneInString(text[:i])
	return r
}

func nextRune(text string, e int) rune {
	if e >= len(text) {
		return utf8.RuneError
	}
	r, _ := utf8.DecodeRuneInString(text[e:])
	return r
}

// --- 1. bearer / basic ---------------------------------------------------------------

func maskBearer(text string, repl func(string) string) string {
	if strings.Contains(text, runeLongS) || hasTurkishI(text) {
		return bearerBasicRE.ReplaceAllStringFunc(text, repl)
	}
	return replaceFound(text, func(pos int) (int, int, bool) {
		for i := pos; i < len(text); i++ {
			if text[i]|0x20 != 'b' {
				continue
			}
			if !hasPrefixFoldASCII(text[i:], "bearer") && !hasPrefixFoldASCII(text[i:], "basic") {
				continue
			}
			if e, ok := bearerAt.matchAt(text, i, 0); ok {
				return i, e, true
			}
		}
		return 0, 0, false
	}, repl)
}

// --- 2. secret keywords ---------------------------------------------------------------

// secretKWStarts are the first three letters of every keyword alternative.
var secretKWStarts = []string{"api", "tok", "sec", "pas", "pwd", "cre", "pri", "acc", "aut"}

// secretKWFirst marks both cases of the keywords' first letters.
var secretKWFirst = func() (t [256]bool) {
	for _, w := range secretKWStarts {
		t[w[0]], t[w[0]-'a'+'A'] = true, true
	}
	return t
}()

// isSecretKWRunRune is the class of the keyword and the [\w.-]* after it:
// W, "." or "-".
func isSecretKWRunRune(r rune) bool { return isNameWordRune(r) || r == '.' || r == '-' }

// skipRunes returns the end of the run of runes in class that starts at j.
func skipRunes(text string, j int, class func(rune) bool) int {
	for j < len(text) {
		r, w := utf8.DecodeRuneInString(text[j:])
		if !class(r) {
			break
		}
		j += w
	}
	return j
}

func maskSecretKW(text string, repl func(string) string) string {
	if strings.Contains(text, runeLongS) || strings.Contains(text, runeKelvin) || hasTurkishI(text) {
		return secretKWRE.ReplaceAllStringFunc(text, repl)
	}
	var scans secretKWScans // one per call: the scans of all its candidates share it
	return replaceFound(text, func(pos int) (int, int, bool) {
		for i := pos; i < len(text); i++ {
			if !secretKWFirst[text[i]] {
				continue
			}
			cand := false
			for _, w := range secretKWStarts {
				if hasPrefixFoldASCII(text[i:], w) {
					cand = true
					break
				}
			}
			if !cand || !secretKWHead.MatchString(text[i:min(len(text), i+16)]) {
				continue
			}
			if e, ok := secretKWAt.matchAt(text, i, secretKWWindow(text, i, &scans)-i); ok {
				return i, e, true
			}
			// The keyword matched but nothing after it did: whatever follows
			// the run of [\w.-] it sits in decides, so no later keyword in the
			// same run can match either. Skip to the run's end.
			i = skipRunes(text, i, isSecretKWRunRune) - 1
		}
		return 0, 0, false
	}, repl)
}

// secretKWWindow bounds a secret-keyword match starting at i, given that a
// keyword starts there: the keyword and [\w.-]* are one run of [\w.-] runes
// (inside it nothing else could follow), then an optional quote, S*, [=:],
// S*, and the value: a quoted one ends at its closing quote (it holds no
// newline), an unquoted one at the first A character, quote, comma or
// semicolon, which the pattern takes along (one byte, all of them are
// ASCII). A value after fewer of the S runes (Python's backtracking, which
// the pattern keeps) ends no later. The pattern looks at nothing past its
// end, so the window loses nothing, and it is usually small enough for
// regexp's backtracker.
//
// The two value scans go through scans, which remembers for each of its byte
// sets the last scan's start and the byte it stopped at (secretKWNext). Until
// 2026-10-03 every candidate scanned its value afresh, and a value is not the
// candidate's own: in "token=<redacted>" repeated without whitespace the
// unquoted value of every keyword runs to the end of the text, so the scans
// added up to quadratic time (MaskDetail on 256 KB: 2.8-3.4 s, against about
// 51 ms in the 200 ns/B budget; measured 2026-10-03). A window is the same as
// before, byte for byte (TestSecretKWLinearDifferential compares both at every
// candidate); only the scanning is shared. Candidates come left to right and
// their value scans start no earlier than the previous one's (between a
// keyword and its value lie only a quote, S, "=" or ":", none of which starts
// a keyword), so each byte is scanned at most once per byte set; a start
// before a remembered one scans afresh, so the result never depends on that
// order. The key run and the S runs belong to one candidate each.
func secretKWWindow(text string, i int, scans *secretKWScans) int {
	j := skipRunes(text, i, isSecretKWRunRune)
	if j < len(text) && (text[j] == '"' || text[j] == '\'') {
		j++
	}
	j = skipRunes(text, j, isPySpace)
	if j >= len(text) || (text[j] != '=' && text[j] != ':') {
		return min(len(text), j+1)
	}
	j = skipRunes(text, j+1, isPySpace)
	end := j
	if j < len(text) && (text[j] == '"' || text[j] == '\'') {
		var k int
		if text[j] == '"' {
			k = scans.dq.next(text, j+1, isDQuoteEnd)
		} else {
			k = scans.sq.next(text, j+1, isSQuoteEnd)
		}
		if k < len(text) && text[k] == text[j] {
			k++
		}
		end = k
		j++
	}
	j = scans.value.next(text, j, isUnquotedValueEnd)
	return min(len(text), max(end, j)+1)
}

// secretKWScans holds secretKWWindow's remembered scans for one masking call:
// to the end of a "..." value, of a '...' value, of an unquoted value.
type secretKWScans struct{ dq, sq, value secretKWNext }

// secretKWNext remembers one forward scan for the first byte of a set: no
// byte of the set lies in text[from:at], at excluded, and at is such a byte
// or len(text).
type secretKWNext struct {
	from, at int
	ok       bool
}

// next is the index of the first byte at or after j that is in the set (or
// len(text)). A j in [from, at] has the remembered answer; any other j scans.
func (c *secretKWNext) next(text string, j int, in func(byte) bool) int {
	if c.ok && c.from <= j && j <= c.at {
		return c.at
	}
	k := j
	for k < len(text) && !in(text[k]) {
		k++
	}
	c.from, c.at, c.ok = j, k, true
	return k
}

func isDQuoteEnd(c byte) bool { return c == '"' || c == '\n' }
func isSQuoteEnd(c byte) bool { return c == '\'' || c == '\n' }

// isUnquotedValueEnd: an A character, a quote, a comma or a semicolon ends an
// unquoted value.
func isUnquotedValueEnd(c byte) bool {
	return isRESpace(c) || c == '"' || c == '\'' || c == ',' || c == ';'
}

// --- 3. email -----------------------------------------------------------------------------

func isEmailLocalRune(r rune) bool {
	return r == '_' || r == '.' || r == '+' || r == '-' || unicode.IsLetter(r) || unicode.IsNumber(r)
}

// maskEmail is the email step. Text that is not valid UTF-8 goes to the
// regex as before; other text is scanned by emailSpans, on the text and, if
// it is not NFC, also on its NFC copy (maskSpansNFC, since 2026-10-02).
// Every address holds an "@", and NFC never makes one (TestNFCKeepsAnchors),
// so a text without one is passed over.
func maskEmail(text string, repl func(string) string) string {
	if !utf8.ValidString(text) {
		return emailRE.ReplaceAllStringFunc(text, repl)
	}
	return maskSpansNFC(text, emailSpans, hasAt, repl)
}

func hasAt(text string) bool { return strings.IndexByte(text, '@') >= 0 }

// emailSpans calls emit with each address in text (valid UTF-8), in order,
// not overlapping: the matches of emailPattern that ReplaceAllStringFunc
// would replace.
func emailSpans(text string, emit func(s, e int)) {
	next := func(pos int) (int, int, bool) {
		for from := pos; from < len(text); {
			k := strings.IndexByte(text[from:], '@')
			if k < 0 {
				return 0, 0, false
			}
			a := from + k
			// Every match holds exactly one "@" and starts where the run of
			// local-part runes before it starts (not before pos).
			s := a
			for s > pos {
				r, w := utf8.DecodeLastRuneInString(text[:s])
				if !isEmailLocalRune(r) {
					break
				}
				s -= w
			}
			if s < a {
				if e, ok := emailAt.matchAt(text, s, 0); ok {
					return s, e, true
				}
			}
			from = a + 1
		}
		return 0, 0, false
	}
	for pos := 0; pos < len(text); {
		s, e, ok := next(pos)
		if !ok {
			break
		}
		emit(s, e)
		pos = e
	}
}

// --- 4., 5., 9a., 9b. bounded steps --------------------------------------------------

// replaceBoundedFast is replaceBoundedShrink with candidates: cand(i) is a
// necessary condition for a match to start at i, okPrev the half of ok that
// looks at the rune before the match (a candidate failing it is skipped, as
// the old loop rejected it), limit bounds the match length (0: none). When
// skipToEnd is set and a match fails only on the rune after it, no candidate
// inside that match can do better (its greedy end is the same), so the search
// goes on after it.
func replaceBoundedFast(text string, at anchoredRE, limit int, full *regexp.Regexp,
	cand func(i int) bool, okPrev func(prev rune) bool, ok func(prev, next rune) bool,
	skipToEnd bool, repl func(m string) string) string {
	var b strings.Builder
	pos, done := 0, 0
	for i := pos; i < len(text); i++ {
		if !cand(i) {
			continue
		}
		prev := prevRune(text, i)
		if !okPrev(prev) {
			continue
		}
		e, found := at.matchAt(text, i, limit)
		if !found {
			continue
		}
		next := nextRune(text, e)
		if e > i && !ok(prev, next) && full != nil {
			for e2 := e - 1; e2 > i; e2-- {
				if !utf8.RuneStart(text[e2]) {
					continue
				}
				n2, _ := utf8.DecodeRuneInString(text[e2:])
				if full.MatchString(text[i:e2]) && ok(prev, n2) {
					e, next = e2, n2
					break
				}
			}
		}
		if e > i && ok(prev, next) {
			b.WriteString(text[done:i])
			b.WriteString(repl(text[i:e]))
			done = e
			i = e - 1
			continue
		}
		if skipToEnd {
			i = e - 1
		}
	}
	if done == 0 {
		return text
	}
	b.WriteString(text[done:])
	return b.String()
}

func ibanCand(text string) func(int) bool {
	return func(i int) bool {
		return i+3 < len(text) && text[i] >= 'A' && text[i] <= 'Z' && text[i+1] >= 'A' && text[i+1] <= 'Z' &&
			isASCIIDigit(text[i+2]) && isASCIIDigit(text[i+3])
	}
}

func phoneCand(text string) func(int) bool {
	return func(i int) bool {
		return text[i] == '0' || (text[i] == '+' && strings.HasPrefix(text[i:], "+49"))
	}
}

func awsCand(text string) func(int) bool {
	return func(i int) bool {
		return text[i] == 'A' && (strings.HasPrefix(text[i:], "AKIA") || strings.HasPrefix(text[i:], "ASIA"))
	}
}

// knownTokenPrefixes, by first byte: the alternatives of knownTokenPattern.
var knownTokenPrefixes = func() map[byte][]string {
	m := map[byte][]string{}
	for _, p := range []string{"ghp_", "gho_", "ghs_", "github_pat_", "sk-", "sk_live_", "sk_test_", "rk_live_", "rk_test_",
		"pk_live_", "xoxa-", "xoxb-", "xoxp-", "xoxr-", "xoxs-", "AKIA", "ASIA", "AIza", "GOCSPX-", "ya29.", "1//", "eyJ", "glpat-", "npm_"} {
		m[p[0]] = append(m[p[0]], p)
	}
	return m
}()

// knownTokenFirst marks the first bytes of knownTokenPrefixes.
var knownTokenFirst = func() (t [256]bool) {
	for c := range knownTokenPrefixes {
		t[c] = true
	}
	return t
}()

func knownTokenCand(text string) func(int) bool {
	return func(i int) bool {
		if !knownTokenFirst[text[i]] {
			return false
		}
		for _, p := range knownTokenPrefixes[text[i]] {
			if strings.HasPrefix(text[i:], p) {
				return true
			}
		}
		return false
	}
}

func notASCIIAlnum(r rune) bool { return !isASCIIAlnum(r) }

// --- 6., 7. street, postcode: mask_address.go -------------------------------------------

// --- 8. names -----------------------------------------------------------------------------

// foldEqualRune: a and b are equal under Unicode simple case folding, as a
// (?i) literal compares them, with I, i, U+0130 and U+0131 in one class, as
// in Python (since 2026-10-02).
func foldEqualRune(a, b rune) bool {
	if a == b || (isTurkishI(a) && isTurkishI(b)) {
		return true
	}
	for f := unicode.SimpleFold(a); f != a; f = unicode.SimpleFold(f) {
		if f == b {
			return true
		}
	}
	return false
}

// isNameWordRune is Python's \w on str, which ts_common's name pattern uses:
// str.isalnum() or "_" (letters and numbers of any script; marks are not).
func isNameWordRune(r rune) bool {
	return r == '_' || unicode.IsLetter(r) || unicode.IsNumber(r)
}

// nameMatcher finds a literal name: the text's runes equal the name's under
// simple case folding, with a Unicode word boundary (isNameWordRune, as
// Python's \b) before and after.
type nameMatcher struct {
	runes []rune
	first [256]bool // first bytes of the runes the name's first rune folds to
	// folded is the name in fold-canonical form (foldCanon); foldedOK is
	// false if the name holds a rune whose canonical form has another length.
	folded   string
	foldedOK bool
}

func newNameMatcher(name string) nameMatcher {
	nm := nameMatcher{runes: []rune(name)}
	nm.folded, nm.foldedOK = foldCanon(name)
	r0 := nm.runes[0]
	add := func(r rune) {
		var buf [utf8.UTFMax]byte
		utf8.EncodeRune(buf[:], r)
		nm.first[buf[0]] = true
	}
	add(r0)
	for f := unicode.SimpleFold(r0); f != r0; f = unicode.SimpleFold(f) {
		add(f)
	}
	if isTurkishI(r0) {
		nm.first['I'], nm.first['i'], nm.first[turkishILead] = true, true, true
	}
	return nm
}

// Fold-canonical form: every rune replaced by the smallest rune of its
// simple case-fold orbit, the classes a (?i) literal compares by, with I, i,
// U+0130 and U+0131 in one class (canonical "I"; U+0130 and U+0131 thus
// have a canonical rune of another length). Two runes fold equal exactly
// when their canonical runes are the same. Where every canonical rune has the
// length of its rune, offsets in the canonical text are offsets in the text.
var (
	foldCanonOnce sync.Once
	foldCanonMap  map[rune]rune // non-ASCII runes whose canonical rune differs
	foldCanonLead [256]bool     // lead bytes of runes whose canonical rune has another length
)

// initFoldCanon visits every rune that has a case mapping or is a cased
// letter (unicode.CaseRanges, Upper, Lower, Title); every rune with a fold
// orbit is among them (TestFoldCanonExhaustive checks all runes), and each
// gets the minimum of its full unicode.SimpleFold orbit.
func initFoldCanon() {
	foldCanonMap = map[rune]rune{}
	visit := func(r rune) {
		m := r
		for f := unicode.SimpleFold(r); f != r; f = unicode.SimpleFold(f) {
			if f < m {
				m = f
			}
		}
		if isTurkishI(r) {
			m = turkishIFirst
		}
		if m != r {
			foldCanonMap[r] = m
			if utf8.RuneLen(m) != utf8.RuneLen(r) {
				var buf [utf8.UTFMax]byte
				utf8.EncodeRune(buf[:], r)
				foldCanonLead[buf[0]] = true
			}
		}
	}
	for _, cr := range unicode.CaseRanges {
		for r := rune(cr.Lo); r <= rune(cr.Hi); r++ {
			visit(r)
		}
	}
	for _, tab := range []*unicode.RangeTable{unicode.Upper, unicode.Lower, unicode.Title} {
		for _, r16 := range tab.R16 {
			for r := rune(r16.Lo); r <= rune(r16.Hi); r += rune(r16.Stride) {
				visit(r)
			}
		}
		for _, r32 := range tab.R32 {
			for r := rune(r32.Lo); r <= rune(r32.Hi); r += rune(r32.Stride) {
				visit(r)
			}
		}
	}
}

func canonRune(r rune) rune {
	if r < utf8.RuneSelf {
		if r >= 'a' && r <= 'z' {
			return r - 'a' + 'A'
		}
		return r
	}
	foldCanonOnce.Do(initFoldCanon)
	if m, ok := foldCanonMap[r]; ok {
		return m
	}
	return r
}

// foldCanon returns s in fold-canonical form; ok is false if some rune's
// canonical rune has another UTF-8 length (the Kelvin sign, long s, capital
// sharp s, Angstrom sign, Ohm sign, U+0130, U+0131, ...) or s is not valid
// UTF-8.
func foldCanon(s string) (string, bool) {
	foldCanonOnce.Do(initFoldCanon)
	b := make([]byte, len(s))
	for i := 0; i < len(s); {
		c := s[i]
		if c < utf8.RuneSelf {
			if c >= 'a' && c <= 'z' {
				c -= 'a' - 'A'
			}
			b[i] = c
			i++
			continue
		}
		r, w := utf8.DecodeRuneInString(s[i:])
		if r == utf8.RuneError && w == 1 {
			return "", false
		}
		m := canonRune(r)
		if utf8.RuneLen(m) != w {
			return "", false
		}
		utf8.EncodeRune(b[i:], m)
		i += w
	}
	return string(b), true
}

// hasFoldLengthRune reports whether s holds a rune whose canonical rune has
// another length (the names step then scans name by name).
func hasFoldLengthRune(s string) bool {
	foldCanonOnce.Do(initFoldCanon)
	for i := 0; i < len(s); i++ {
		if foldCanonLead[s[i]] {
			r, w := utf8.DecodeRuneInString(s[i:])
			if utf8.RuneLen(canonRune(r)) != w {
				return true
			}
		}
	}
	return false
}

// matchAt returns the end of the name matched at byte i, if it matches there.
func (nm nameMatcher) matchAt(text string, i int) (int, bool) {
	j := i
	for _, nr := range nm.runes {
		if j >= len(text) {
			return 0, false
		}
		r, w := utf8.DecodeRuneInString(text[j:])
		if !foldEqualRune(nr, r) {
			return 0, false
		}
		j += w
	}
	first, _ := utf8.DecodeRuneInString(text[i:])
	last, _ := utf8.DecodeLastRuneInString(text[:j])
	before := i > 0 && isNameWordRune(prevRune(text, i))
	after := j < len(text) && isNameWordRune(nextRune(text, j))
	if before == isNameWordRune(first) || after == isNameWordRune(last) {
		return 0, false
	}
	return j, true
}

// nameRep is one replacement of a name pass: bytes [p, e) of the text
// before the pass became n bytes (the placeholder). The names step on an NFC
// copy (mask_nfc.go) records them to map the copy back.
type nameRep struct{ p, e, n int }

// replaceFolded is replace on a text whose fold-canonical form is folded
// (same offsets): every match is an occurrence of the name's canonical form
// there, so strings.Index finds all candidate starts, each is checked exactly
// (matchAt, with \b), and the leftmost non-overlapping ones are replaced, as
// the regexp replaces a literal's matches. It returns the new text and its
// canonical form, kept in step by the same edits. If rec is not nil, each
// replacement is appended to it.
func (nm nameMatcher) replaceFolded(text, folded string, repl func(string) string, rec *[]nameRep) (string, string, bool) {
	var tb, fb strings.Builder
	done := 0
	for from := 0; from < len(folded); {
		k := strings.Index(folded[from:], nm.folded)
		if k < 0 {
			break
		}
		p := from + k
		from = p + 1
		if p < done {
			continue
		}
		e, ok := nm.matchAt(text, p)
		if !ok {
			continue
		}
		r := repl(text[p:e])
		rf, rok := foldCanon(r)
		if !rok {
			// cannot happen for "<name>"; recompute the hard way
			if rec != nil {
				*rec = (*rec)[:0]
			}
			return nm.replace(text, repl, rec), "", false
		}
		if rec != nil {
			*rec = append(*rec, nameRep{p, e, len(r)})
		}
		tb.WriteString(text[done:p])
		tb.WriteString(r)
		fb.WriteString(folded[done:p])
		fb.WriteString(rf)
		done, from = e, e
	}
	if done == 0 {
		return text, folded, true
	}
	tb.WriteString(text[done:])
	fb.WriteString(folded[done:])
	return tb.String(), fb.String(), true
}

// replace replaces the name's leftmost non-overlapping matches in text,
// scanning rune by rune; if rec is not nil, each replacement is appended to
// it.
func (nm nameMatcher) replace(text string, repl func(string) string, rec *[]nameRep) string {
	r := repl
	if rec != nil {
		r = func(m string) string {
			out := repl(m)
			(*rec)[len(*rec)-1].n = len(out)
			return out
		}
	}
	return replaceFound(text, func(pos int) (int, int, bool) {
		for i := pos; i < len(text); i++ {
			if !nm.first[text[i]] {
				continue
			}
			if e, ok := nm.matchAt(text, i); ok {
				if rec != nil {
					*rec = append(*rec, nameRep{i, e, 0})
				}
				return i, e, true
			}
		}
		return 0, 0, false
	}, r)
}

// --- 9c. opaque --------------------------------------------------------------------------

func isOpaqueByte(c byte) bool {
	return isASCIILetter(c) || isASCIIDigit(c) || c == '_' || c == '-' || c == '+' || c == '/'
}

// maskOpaque is ts_common.py's RX_OPAQUE,
// (?=[A-Za-z0-9_\-+/]*\d)(?=[A-Za-z0-9_\-+/]*[A-Za-z])[A-Za-z0-9_\-+/]{24,}={0,2}
// with Python's \d (Unicode Nd): a maximal run of 24 or more such bytes is a
// match, with up to two "=" after it, if it holds an ASCII letter and either
// an ASCII digit or, directly after the run (before any "="), a decimal
// digit of any script ("...abcd" + U+0661). The lookaheads, tried from the
// run's start, see the whole run and the rune after it; from a later start
// they see less, so a run that fails at its start fails everywhere in it.
// Invalid UTF-8 after the run is no digit (utf8.RuneError). Mask parity
// spec amendment A9, 2026-10-03; until then Go wanted an ASCII digit in
// the run (amendment A4, superseded).
func maskOpaque(text string, repl func(string) string) string {
	return replaceFound(text, func(pos int) (int, int, bool) {
		for i := pos; i < len(text); {
			if !isOpaqueByte(text[i]) {
				i++
				continue
			}
			j := i
			for j < len(text) && isOpaqueByte(text[j]) {
				j++
			}
			if j-i >= 24 && opaqueRunQualifies(text, i, j) {
				for k := 0; k < 2 && j < len(text) && text[j] == '='; k++ {
					j++
				}
				return i, j, true
			}
			i = j
		}
		return 0, 0, false
	}, repl)
}

// opaqueRunQualifies: the run text[i:j] holds an ASCII letter, and an ASCII
// digit or a Unicode decimal digit directly after it (text[j] is no opaque
// byte, so a digit there is not ASCII).
func opaqueRunQualifies(text string, i, j int) bool {
	letter, digit := false, false
	for k := i; k < j && !(letter && digit); k++ {
		if isASCIIDigit(text[k]) {
			digit = true
		} else if isASCIILetter(text[k]) {
			letter = true
		}
	}
	if !letter {
		return false
	}
	if digit {
		return true
	}
	if j < len(text) && text[j] >= utf8.RuneSelf {
		r, _ := utf8.DecodeRuneInString(text[j:])
		return unicode.IsDigit(r)
	}
	return false
}
