package main

import (
	"regexp"
	"sort"
	"strings"
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
//     step: invalid UTF-8, and, for case-insensitive steps, the runes that Go
//     folds onto ASCII letters (U+212A Kelvin sign for k, U+017F long s for s)
//     or onto ß (U+1E9E).
//
// maskDetailReference (a test-only copy of the old MaskDetail) is the oracle:
// TestMaskDifferential compares both on a large generated corpus.

const (
	runeKelvin   = "\u212a"
	runeLongS    = "\u017f"
	runeCapSharp = "\u1e9e"
)

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
	secretKWHead = regexp.MustCompile(`^(?i:api[_-]?key|token|secret|passw(?:or)?d|pass(?:phrase|wort)?|pwd|credential|private[_-]?key|access[_-]?key|auth(?:orization)?)`)
	emailAt      = newAnchoredRE(emailPattern)
	ibanAt       = newAnchoredRE(ibanPattern)
	phoneAt      = newAnchoredRE(phonePattern)
	plzOrtAt     = newAnchoredRE(plzOrtPattern)
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
	if strings.Contains(text, runeLongS) {
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

func isSecretKWRunByte(c byte) bool { return isASCIIWordByte(c) || c == '.' || c == '-' }

func maskSecretKW(text string, repl func(string) string) string {
	if strings.Contains(text, runeLongS) || strings.Contains(text, runeKelvin) {
		return secretKWRE.ReplaceAllStringFunc(text, repl)
	}
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
			if e, ok := secretKWAt.matchAt(text, i, secretKWWindow(text, i)-i); ok {
				return i, e, true
			}
			// The keyword matched but nothing after it did: whatever follows
			// the run of [\w.-] it sits in decides, so no later keyword in the
			// same run can match either. Skip to the run's end.
			j := i
			for j < len(text) && isSecretKWRunByte(text[j]) {
				j++
			}
			i = j - 1
		}
		return 0, 0, false
	}, repl)
}

// secretKWWindow bounds a secret-keyword match starting at i, given that a
// keyword starts there: the keyword and [\w.-]* are one run of [\w.-] bytes
// (inside it nothing else could follow), then an optional quote, \s*, [=:],
// \s*, and the value: a quoted one ends at its closing quote (it holds no
// newline), an unquoted one at the first \s, quote, comma or semicolon. The
// pattern looks at nothing past its end, so the window loses nothing, and it
// is usually small enough for regexp's backtracker. Each scan covers text
// that belongs to this candidate only (see maskSecretKW), so the scans add up
// to linear time.
func secretKWWindow(text string, i int) int {
	j := i
	for j < len(text) && isSecretKWRunByte(text[j]) {
		j++
	}
	if j < len(text) && (text[j] == '"' || text[j] == '\'') {
		j++
	}
	for j < len(text) && isRESpace(text[j]) {
		j++
	}
	if j >= len(text) || (text[j] != '=' && text[j] != ':') {
		return min(len(text), j+1)
	}
	j++
	for j < len(text) && isRESpace(text[j]) {
		j++
	}
	end := j
	if j < len(text) && (text[j] == '"' || text[j] == '\'') {
		q := text[j]
		k := j + 1
		for k < len(text) && text[k] != q && text[k] != '\n' {
			k++
		}
		if k < len(text) && text[k] == q {
			k++
		}
		end = k
		j++
	}
	for j < len(text) && !isRESpace(text[j]) && text[j] != '"' && text[j] != '\'' && text[j] != ',' && text[j] != ';' {
		j++
	}
	return min(len(text), max(end, j)+1)
}

// --- 3. email -----------------------------------------------------------------------------

func isEmailLocalRune(r rune) bool {
	return r == '_' || r == '.' || r == '+' || r == '-' || unicode.IsLetter(r) || unicode.IsNumber(r)
}

func maskEmail(text string, repl func(string) string) string {
	if !utf8.ValidString(text) {
		return emailRE.ReplaceAllStringFunc(text, repl)
	}
	return replaceFound(text, func(pos int) (int, int, bool) {
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
	}, repl)
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

// --- 6. street + house number --------------------------------------------------------

// Go's \s: tab, newline, form feed, carriage return, space.
func isRESpace(c byte) bool { return c == '\t' || c == '\n' || c == '\f' || c == '\r' || c == ' ' }

// streetSuffixes are the (?i) suffixes of the street name, plus the
// case-sensitive Straße, Strasse, Str., Str, which they cover.
var streetSuffixes = []string{"straße", "strasse", "str.", "str", "weg", "gasse", "platz", "allee", "ring", "ufer",
	"damm", "chaussee", "zeile", "pfad", "steig", "gäßchen", "gaesschen"}

// streetSuffixLast marks the last bytes the suffixes can end in, both cases;
// their last letters have no case folds outside ASCII.
var streetSuffixLast = func() (t [256]bool) {
	for _, suf := range streetSuffixes {
		c := suf[len(suf)-1]
		t[c] = true
		if c >= 'a' && c <= 'z' {
			t[c-'a'+'A'] = true
		}
	}
	return t
}()

// streetPrepParts are the words of the prepositions of the first branch.
var streetPrepParts = map[string]bool{"Am": true, "An": true, "Auf": true, "Im": true, "In": true, "Vor": true,
	"Hinter": true, "Zum": true, "Zur": true, "der": true, "dem": true}

func isStreetUpper(r rune) bool { return (r >= 'A' && r <= 'Z') || r == 'Ä' || r == 'Ö' || r == 'Ü' }
func isStreetWordRune(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '.' || r == '-' ||
		r == 'ä' || r == 'ö' || r == 'ü' || r == 'ß' || r == 'Ä' || r == 'Ö' || r == 'Ü'
}

// isStreetWord: a whole token of the form [A-ZÄÖÜ][a-zäöüßA-ZÄÖÜ0-9.-]+.
func isStreetWord(tok string) bool {
	r, w := utf8.DecodeRuneInString(tok)
	if !isStreetUpper(r) || len(tok) == w {
		return false
	}
	for _, r := range tok[w:] {
		if !isStreetWordRune(r) {
			return false
		}
	}
	return true
}

// foldEqualRune: a and b are equal under Unicode simple case folding, as a
// (?i) literal compares them.
func foldEqualRune(a, b rune) bool {
	if a == b {
		return true
	}
	for f := unicode.SimpleFold(a); f != a; f = unicode.SimpleFold(f) {
		if f == b {
			return true
		}
	}
	return false
}

// hasSuffixFold: s ends with suf under simple case folding.
func hasSuffixFold(s, suf string) bool {
	for len(suf) > 0 {
		if len(s) == 0 {
			return false
		}
		a, wa := utf8.DecodeLastRuneInString(s)
		b, wb := utf8.DecodeLastRuneInString(suf)
		if !foldEqualRune(b, a) {
			return false
		}
		s, suf = s[:len(s)-wa], suf[:len(suf)-wb]
	}
	return true
}

// tokenBefore returns the start of the token (maximal run of non-\s bytes)
// that ends at q, and the start of the \s run before it.
func tokenBefore(text string, q int) (tokStart, spaceStart int) {
	t := q
	for t > 0 && !isRESpace(text[t-1]) {
		t--
	}
	sp := t
	for sp > 0 && isRESpace(text[sp-1]) {
		sp--
	}
	return t, sp
}

// streetAnchor is a place where a street match can end its name part: q,
// followed by \s+\d. A match whose name part ends at q starts in [rs, q) and
// ends at or before re (re includes one rune of context for the final \b).
type streetAnchor struct{ rs, q, re int }

// streetAnchors lists, left to right, every q followed by \s+\d whose name
// part can end there: it ends in a street suffix, or a chain of capitalised
// words goes back to a preposition. rs is the start of the first token, going
// back from q, that cannot belong to a street name (a match may start inside
// it, after a \b); re is the end of the longest tail the pattern allows.
// Matches starting in [rs, q) all end at q's tail: the range holds no other
// \s\d (a token starting with a digit stops the walk), so ranges of later
// anchors start after this one's digits.
func streetAnchors(text string) []streetAnchor {
	var anchors []streetAnchor
	for d := 1; d < len(text); d++ {
		if !isASCIIDigit(text[d]) || !isRESpace(text[d-1]) {
			continue
		}
		q := d - 1
		for q > 0 && isRESpace(text[q-1]) {
			q--
		}
		if q == 0 {
			continue
		}
		qualifies := false
		if streetSuffixLast[text[q-1]] {
			for _, suf := range streetSuffixes {
				if hasSuffixFold(text[:q], suf) {
					qualifies = true
					break
				}
			}
		}
		rs := 0
		end := q
		for end > 0 {
			t, sp := tokenBefore(text, end)
			tok := text[t:end]
			// A preposition of the first branch: a token ending in Am, Im,
			// Zum or Zur (it may start inside the token, after a \b), or the
			// token der or dem of the two-word ones.
			if strings.HasSuffix(tok, "Am") || strings.HasSuffix(tok, "Im") || strings.HasSuffix(tok, "Zum") ||
				strings.HasSuffix(tok, "Zur") || tok == "der" || tok == "dem" {
				qualifies = true
			}
			if isStreetWord(tok) || streetPrepParts[tok] {
				end = sp
				if end == 0 {
					rs = t
				}
				continue
			}
			rs = t
			break
		}
		if !qualifies {
			continue
		}
		// The longest tail: \d+ [a-zA-Z]? ( \s* [-/] \s* \d+ [a-zA-Z]? )?
		e := d
		for e < len(text) && isASCIIDigit(text[e]) {
			e++
		}
		if e < len(text) && isASCIILetter(text[e]) {
			e++
		}
		f := e
		for f < len(text) && isRESpace(text[f]) {
			f++
		}
		if f < len(text) && (text[f] == '-' || text[f] == '/') {
			f++
			for f < len(text) && isRESpace(text[f]) {
				f++
			}
			if f < len(text) && isASCIIDigit(text[f]) {
				for f < len(text) && isASCIIDigit(text[f]) {
					f++
				}
				if f < len(text) && isASCIILetter(text[f]) {
					f++
				}
				e = f
			}
		}
		if e < len(text) {
			_, w := utf8.DecodeRuneInString(text[e:])
			e += w
		}
		anchors = append(anchors, streetAnchor{rs, q, e})
	}
	return anchors
}

var (
	streetAt = newAnchoredRE(streetPattern)
	// streetFrom finds the leftmost match after a context rune (streetFrom0:
	// at the text start): the lazy prefix tries each start in order, and the
	// pattern's own preference at that start.
	streetFrom  = regexp.MustCompile(`^(?s:.)(?s:.*?)(` + streetPattern + `)`)
	streetFrom0 = regexp.MustCompile(`^(?s:.*?)(` + streetPattern + `)`)
)

// streetMatchIn returns the leftmost match starting in [from, a.q). Matches
// start with [A-ZÄÖÜ] after a \b; each such candidate is tried anchored, on a
// window that ends at a.re, small enough for regexp's backtracker. After 16
// failed candidates (a long chain of capitalised words) it searches the rest
// of the range at once, in time linear in its length.
func streetMatchIn(text string, from int, a streetAnchor) (int, int, bool) {
	tried := 0
	for c := from; c < a.q; {
		r, w := utf8.DecodeRuneInString(text[c:])
		if isStreetUpper(r) && isREWordRune(prevRune(text, c)) != isREWordRune(r) {
			if tried++; tried > 16 {
				return streetLeftmostFrom(text, c, a.re)
			}
			if e, ok := streetAt.matchAt(text, c, a.re-c); ok {
				return c, e, true
			}
		}
		c += w
	}
	return 0, 0, false
}

func streetLeftmostFrom(text string, c, re int) (int, int, bool) {
	if c == 0 {
		m := streetFrom0.FindStringSubmatchIndex(text[:re])
		if m == nil {
			return 0, 0, false
		}
		return m[2], m[3], true
	}
	_, w := utf8.DecodeLastRuneInString(text[:c])
	m := streetFrom.FindStringSubmatchIndex(text[c-w : re])
	if m == nil {
		return 0, 0, false
	}
	return c - w + m[2], c - w + m[3], true
}

func maskStreet(text string, repl func(string) string) string {
	if !utf8.ValidString(text) || strings.Contains(text, runeLongS) || strings.Contains(text, runeCapSharp) {
		return streetRE.ReplaceAllStringFunc(text, repl)
	}
	anchors := streetAnchors(text)
	ai := 0
	return replaceFound(text, func(pos int) (int, int, bool) {
		for ; ai < len(anchors); ai++ {
			a := anchors[ai]
			from := max(a.rs, pos)
			if from >= a.q {
				continue
			}
			if s, e, ok := streetMatchIn(text, from, a); ok {
				return s, e, true
			}
		}
		return 0, 0, false
	}, repl)
}

// --- 7. postcode + place ------------------------------------------------------------------

func maskPlzOrt(text string, repl func(string) string) string {
	return replaceFound(text, func(pos int) (int, int, bool) {
		for i := pos; i+5 < len(text); i++ {
			// \b\d{5}\s: five ASCII digits at the start of a word, then a space
			if !isASCIIDigit(text[i]) || (i > 0 && isASCIIWordByte(text[i-1])) {
				continue
			}
			if !isASCIIDigit(text[i+1]) || !isASCIIDigit(text[i+2]) || !isASCIIDigit(text[i+3]) ||
				!isASCIIDigit(text[i+4]) || !isRESpace(text[i+5]) {
				continue
			}
			if e, ok := plzOrtAt.matchAt(text, i, 0); ok {
				return i, e, true
			}
		}
		return 0, 0, false
	}, repl)
}

// --- 8. names -----------------------------------------------------------------------------

func isREWordRune(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_'
}

// nameMatcher finds (?i)\bNAME\b for one literal name: the text's runes equal
// the name's under simple case folding, with an ASCII word boundary (Go's \b)
// before and after.
type nameMatcher struct {
	runes []rune
	first [256]bool // first bytes of the runes the name's first rune folds to
	// anchor is the name's longest run of ASCII bytes, lower-cased, if it has
	// at least two; anchorRunes is the number of the name's runes before it.
	anchor      string
	anchorRunes int
}

func newNameMatcher(name string) nameMatcher {
	nm := nameMatcher{runes: []rune(name)}
	// the longest ASCII run (rune index start, length)
	bestStart, bestLen, start := 0, 0, -1
	for i, r := range append(nm.runes, utf8.RuneError) {
		if r < utf8.RuneSelf && r != utf8.RuneError {
			if start < 0 {
				start = i
			}
			continue
		}
		if start >= 0 && i-start > bestLen {
			bestStart, bestLen = start, i-start
		}
		start = -1
	}
	if bestLen >= 2 {
		nm.anchor = strings.ToLower(string(nm.runes[bestStart : bestStart+bestLen]))
		nm.anchorRunes = bestStart
	}
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
	return nm
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
	before := i > 0 && isREWordRune(prevRune(text, i))
	after := j < len(text) && isREWordRune(nextRune(text, j))
	if before == isREWordRune(first) || after == isREWordRune(last) {
		return 0, false
	}
	return j, true
}

// lowerASCII lower-cases ASCII letters only, so byte offsets stay the same.
func lowerASCII(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'A' && c <= 'Z' {
			b[i] = c + 'a' - 'A'
		}
	}
	return string(b)
}

// replaceAnchored is replace for a name with an ASCII anchor, on a text
// without the Kelvin sign or long s (the only non-ASCII runes that fold onto
// ASCII letters): every match has the anchor, lower-cased, in lower (the text
// lower-cased in ASCII) at its start plus the bytes of the runes before it.
// All occurrences are found with strings.Index, each gives at most one start
// (walking back over the name's runes before the anchor), every start is
// checked exactly, and the leftmost non-overlapping ones are replaced, as the
// regexp replaces a literal's matches.
func (nm nameMatcher) replaceAnchored(text, lower string, repl func(string) string) string {
	var starts [][2]int
	for from := 0; from < len(lower); {
		k := strings.Index(lower[from:], nm.anchor)
		if k < 0 {
			break
		}
		p := from + k
		from = p + 1
		s := p
		ok := true
		for r := nm.anchorRunes - 1; r >= 0; r-- {
			if s == 0 {
				ok = false
				break
			}
			tr, w := utf8.DecodeLastRuneInString(text[:s])
			if !foldEqualRune(nm.runes[r], tr) {
				ok = false
				break
			}
			s -= w
		}
		if !ok {
			continue
		}
		if e, ok := nm.matchAt(text, s); ok {
			starts = append(starts, [2]int{s, e})
		}
	}
	if len(starts) == 0 {
		return text
	}
	sort.Slice(starts, func(i, j int) bool { return starts[i][0] < starts[j][0] })
	var b strings.Builder
	done := 0
	for _, m := range starts {
		if m[0] < done {
			continue
		}
		b.WriteString(text[done:m[0]])
		b.WriteString(repl(text[m[0]:m[1]]))
		done = m[1]
	}
	b.WriteString(text[done:])
	return b.String()
}

func (nm nameMatcher) replace(text string, repl func(string) string) string {
	return replaceFound(text, func(pos int) (int, int, bool) {
		for i := pos; i < len(text); i++ {
			if !nm.first[text[i]] {
				continue
			}
			if e, ok := nm.matchAt(text, i); ok {
				return i, e, true
			}
		}
		return 0, 0, false
	}, repl)
}

// --- 9c. opaque --------------------------------------------------------------------------

func isOpaqueByte(c byte) bool {
	return isASCIILetter(c) || isASCIIDigit(c) || c == '_' || c == '-' || c == '+' || c == '/'
}

// maskOpaque is [A-Za-z0-9_\-+/]{24,}={0,2}: every run of 24 or more such
// bytes, with up to two "=" after it, is a match.
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
			if j-i >= 24 {
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
