package main

// maskDetailReference is MaskDetail exactly as it stood at 301db1c, before the
// speed-up: the same pattern strings (compiled here from copies, so a later
// change to a production pattern shows up as a difference), the same order of
// steps, the same closures. It is the oracle of TestMaskDifferential and exists
// only in tests. One step changed on purpose since: names have Unicode word
// boundaries (refNamesStep; ts_common.py's parity in TestNamePythonParity).

import (
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

var (
	refBearerBasicRE     = regexp.MustCompile(`(?i)(\b(?:bearer|basic)\s+)([^\s"',;]+)`)
	refSecretKWRE        = regexp.MustCompile(`(?i)((?:api[_-]?key|token|secret|passw(?:or)?d|pass(?:phrase|wort)?|pwd|credential|private[_-]?key|access[_-]?key|auth(?:orization)?)[\w.-]*["']?\s*[=:]\s*)("[^"\n]+"|'[^'\n]+'|["']?[^\s"',;]+)`)
	refIbanRE            = regexp.MustCompile(refIBANPattern)
	refIbanFullRE        = regexp.MustCompile(`^(?:` + refIBANPattern + `)$`)
	refPhoneRE           = regexp.MustCompile(`(?:\+49|0)(?:[ \t./()-]*\d){8,}`)
	refAwsKeyIDRE        = regexp.MustCompile(`(?:AKIA|ASIA)[0-9A-Z]{16}`)
	refKnownTokenRE      = regexp.MustCompile(`(?:ghp_|gho_|ghs_|github_pat_|sk-|sk_live_|sk_test_|rk_live_|rk_test_|pk_live_|xox[abprs]-|AKIA|ASIA|AIza|GOCSPX-|ya29\.|1//|eyJ|glpat-|npm_)[A-Za-z0-9_\-./+=]{8,}`)
	refEmailRE           = regexp.MustCompile(`[\p{L}\p{N}_.+-]+@[\p{L}\p{N}_-]+\.[\p{L}\p{N}_.-]+`)
	refOpaqueCandidateRE = regexp.MustCompile(`[A-Za-z0-9_\-+/]{24,}={0,2}`)
	refStreetRE          = regexp.MustCompile(`\b(?:(?:Am|An der|Auf dem|Auf der|Im|In der|Vor dem|Hinter dem|Zum|Zur)\s+[A-ZÄÖÜ][a-zäöüßA-ZÄÖÜ0-9.-]+(?:\s+[A-ZÄÖÜ][a-zäöüßA-ZÄÖÜ0-9.-]+)*|(?:[A-ZÄÖÜ][a-zäöüßA-ZÄÖÜ0-9.-]+\s+)*(?:Straße|Strasse|Str\.|Str\b|[A-ZÄÖÜ][a-zäöüßA-ZÄÖÜ0-9.-]*(?i:straße|strasse|str\.|str\b|weg|gasse|platz|allee|ring|ufer|damm|chaussee|zeile|pfad|steig|gäßchen|gaesschen)))\s+\d+[a-zA-Z]?(?:\s*[-/]\s*\d{1,4}[a-zA-Z]?)?\b`)
	refPlzOrtRE          = regexp.MustCompile(`\b\d{5}\s+[A-ZÄÖÜ][a-zäöüßA-ZÄÖÜ.-]+(?:\s+(?:(?:am|an der)\s+[A-ZÄÖÜ][a-zäöüßA-ZÄÖÜ.-]+|im Breisgau|ob der Tauber))?\b`)
)

const refIBANPattern = `[A-Z]{2}\d{2}(?: ?[A-Z0-9]){11,30}`

// refLap, when a profiler sets refLapFn, reports that the named step has just
// ended; MaskDetail's own steps report through maskLap the same way.
var refLapFn func(step string)

func refLap(step string) {
	if refLapFn != nil {
		refLapFn(step)
	}
}

// refNamesStep is the names step as it stands since 2026-10-02 (Unicode
// word boundaries, as ts_common.py's Python \b), written plainly and apart
// from mask_fast.go: for each name in loadNames order, every start that
// (?i)NAME (no \b) finds, overlapping ones too, is kept if a word boundary
// lies at both of its ends; the leftmost non-overlapping ones are replaced.
// Up to 2026-10-02 the step was (?i)\bNAME\b with Go's ASCII \b.
func refNamesStep(text string, counts *MaskCounts) string {
	for _, name := range loadNames(getNamesFilePath()) {
		re := regexp.MustCompile(`(?i)` + regexp.QuoteMeta(name))
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

func maskDetailReference(text string) (string, MaskCounts) {
	var counts MaskCounts

	// 1. Bearer / Basic
	text = refBearerBasicRE.ReplaceAllStringFunc(text, func(m string) string {
		sub := refBearerBasicRE.FindStringSubmatch(m)
		if len(sub) >= 3 {
			if sub[2] == "<redacted>" {
				return m
			}
			counts.SecretKW++
			return sub[1] + "<redacted>"
		}
		counts.SecretKW++
		return "<redacted>"
	})

	refLap("bearer")

	// 2. Secret keywords with = or :; a quoted value is masked as a whole.
	text = refSecretKWRE.ReplaceAllStringFunc(text, func(m string) string {
		sub := refSecretKWRE.FindStringSubmatch(m)
		if len(sub) < 3 {
			counts.SecretKW++
			return "<redacted>"
		}
		val := sub[2]
		if len(val) >= 2 && (val[0] == '"' || val[0] == '\'') && val[len(val)-1] == val[0] {
			q := val[:1]
			if val[1:len(val)-1] == "<redacted>" {
				return m
			}
			counts.SecretKW++
			return sub[1] + q + "<redacted>" + q
		}
		bare := strings.TrimLeft(val, `"'`)
		if bare == "<redacted>" || strings.HasPrefix(bare, "<redacted>") || strings.EqualFold(bare, "bearer") || strings.EqualFold(bare, "basic") {
			return m
		}
		counts.SecretKW++
		return sub[1] + "<redacted>"
	})

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
	// character, "+" or "." directly before).
	text = refReplaceBounded(text, refPhoneRE, func(prev, next rune) bool {
		return !(unicode.IsLetter(prev) || unicode.IsDigit(prev) || prev == '_' || prev == '+' || prev == '.') &&
			!unicode.IsDigit(next)
	}, func(string) string {
		counts.Phone++
		return "<phone>"
	})

	refLap("phone")

	// 6. address: Straße + Hausnummer
	text = refStreetRE.ReplaceAllStringFunc(text, func(m string) string {
		counts.Address++
		return "<address>"
	})

	refLap("street")

	// 7. address: PLZ + Ort
	text = refPlzOrtRE.ReplaceAllStringFunc(text, func(m string) string {
		counts.Address++
		return "<address>"
	})

	refLap("plz")

	// 8. names: from TYPESAFE_NAMES_FILE or ~/.config/typesafe/names.txt
	// Case-insensitive to match ts_common.py's get_name_regex ((?i)), e.g.
	// "max mustermann" lowercase must also be masked.
	text = refNamesStep(text, &counts)

	refLap("names")

	// 9. opaque: AWS key IDs and known token prefixes (not inside a word), then
	// 24+ chars with at least one digit and one letter.
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
	text = refOpaqueCandidateRE.ReplaceAllStringFunc(text, func(m string) string {
		hasDigit := false
		hasLetter := false
		for _, r := range m {
			if r >= '0' && r <= '9' {
				hasDigit = true
			} else if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
				hasLetter = true
			}
		}
		if hasDigit && hasLetter {
			counts.Opaque++
			return "<redacted>"
		}
		return m
	})

	refLap("opaque")
	return text, counts
}
