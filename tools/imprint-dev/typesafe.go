package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"
)

const (
	DefaultEndpoint = "https://api.typesafe.ai/v1/systemone"
	DefaultModel    = "jev-latest"
	DefaultTimeout  = 8 * time.Second
	MaxPayloadBytes = 100 * 1024 // 100 KB

	KnowledgeKeepingQuestionID = "missing_provenance"
	KnowledgeKeepingNotice     = "imprint knowledge-keeping: Dieser Eintrag enthält möglicherweise einen Fakt oder Entscheid ohne Erfassungsdatum (YYYY-MM-DD), Quelle oder Methode. Gemäß knowledge-keeping sollte jede dauerhafte Aufzeichnung Herkunft, Methode und Datum nennen.\n(knowledge-keeping: This entry may record a fact or decision without a recording date (YYYY-MM-DD), source, or acquisition method. Consider adding provenance.)"

	RuleEnforcementQuestionID = "missing_enforcement"
	RuleEnforcementNotice     = "imprint core: Jede Regel nennt, was sie durchsetzt, oder sagt deutlich, dass nichts es tut. Dieser Regeltext enthält möglicherweise Vorgaben ohne Benennung des Durchsetzungs-Mechanismus.\n(imprint core: Every rule names what enforces it, or says plainly that nothing does. Consider adding enforcement details or stating that nothing enforces it.)"

	DuplicateFactQuestionID = "duplicate_fact"
	DuplicateFactNotice     = "imprint knowledge-keeping: Dieser Eintrag enthält möglicherweise einen Fakt oder Entscheid, der bereits im Register existiert. Jeder Fakt hat genau einen kanonischen Ort; bestehende Einträge sollten abgelöst ([Überholt/Abgelöst am ... durch ...]) oder referenziert werden, statt sie doppelt anzulegen.\n(knowledge-keeping: This entry may duplicate an existing fact or decision. Every fact has one canonical place; consider referencing or superseding the existing entry rather than duplicating it.)"

	SkillSuggestionQuestionID = "suggested_skill"
)

// --- Masking (analog typesafe-dev ts_common.py) ------------------------------

// The masking steps follow ts_common.py, which runs Python's re on str; the
// spec for their parity (2026-10-02, lead session) fixes these classes:
//   - S, Python's \s: unicode.IsSpace plus U+001C..U+001F (isPySpace,
//     pySpaceClass; TestMaskClasses compares both on every rune);
//   - W, Python's \w: [\p{L}\p{N}_] (isNameWordRune); D, Python's \d: \p{Nd};
//   - A, Go's \s: [\t\n\f\r ] (isRESpace; no \v);
//   - (?i): Python puts I, i, U+0130 and U+0131 into one class, Go's (?i) only
//     I and i; every i of a case-insensitive literal is written as turkishI,
//     which (?i) widens to all four.
const (
	pySpaceClass = `[\t\n\v\f\r \x{1C}-\x{1F}\x{85}\x{A0}\x{1680}\x{2000}-\x{200A}\x{2028}\x{2029}\x{202F}\x{205F}\x{3000}]`
	turkishI     = `[i\x{130}\x{131}]`
)

const (
	// bearerBasicPattern: the trigger after no ASCII word character (Go's
	// ASCII \b; "äBearer x" triggers), S+, then everything up to the next A
	// character, quotes and commas included; replaced as a whole, a
	// "<redacted>" too (since 2026-10-02; before: [^\s"',;]+ and no S).
	bearerBasicPattern = `(?i)(\b` + bearerBasicTrigger + pySpaceClass + `+)([^\t\n\f\r ]+)`
	// bearerBasicTrigger is the trigger list of ts_common.py's RX_BEARER.
	bearerBasicTrigger = `(?:bearer|bas` + turkishI + `c)`
	// secretKWKeywords is ts_common.py's _MASK_KW.
	secretKWKeywords = `(?:ap` + turkishI + `[_-]?key|token|secret|passw(?:or)?d|pass(?:phrase|wort)?|pwd|credent` + turkishI + `al|pr` +
		turkishI + `vate[_-]?key|access[_-]?key|auth(?:or` + turkishI + `zat` + turkishI + `on)?)`
	// secretKWLead is group 1 of ts_common.py's RX_KEY_VAL: a keyword, its
	// tail [\w.-]*, an optional quote, S*, = or :, S*.
	secretKWLead = secretKWKeywords + `[\p{L}\p{N}_.-]*["']?` + pySpaceClass + `*[=:]` + pySpaceClass + `*`
	// phonePattern: +49 or 0, then 8 or more Unicode decimal digits, with
	// separators; the lookarounds (no W, "+" or "." before, no D after) are
	// checked in code.
	phonePattern           = `(?:\+49|0)(?:[ \t./()-]*\p{Nd}){8,}`
	awsKeyIDPattern        = `(?:AKIA|ASIA)[0-9A-Z]{16}`
	knownTokenPattern      = `(?:ghp_|gho_|ghs_|github_pat_|sk-|sk_live_|sk_test_|rk_live_|rk_test_|pk_live_|xox[abprs]-|AKIA|ASIA|AIza|GOCSPX-|ya29\.|1//|eyJ|glpat-|npm_)[A-Za-z0-9_\-./+=]{8,}`
	emailPattern           = `[\p{L}\p{N}_.+-]+@[\p{L}\p{N}_-]+\.[\p{L}\p{N}_.-]+`
	opaqueCandidatePattern = `[A-Za-z0-9_\-+/]{24,}={0,2}`
)

// The address grammars of the mask parity spec, section 4 (2026-10-02; the
// user's decision: the union of ts_common.py's grammar, Py, and Go's, each
// copied from its source without \b), with S as pySpaceClass and D as
// \p{Nd} everywhere, the other classes literal as written, and every i of a
// (?i) part as turkishI. A street is PREFIX S+ NUMBER of one grammar
// (mask_address.go relies on that shape); maskStreet and maskPlzOrt say how
// the matches are chosen. What changed for Go: Python's grammar joined it,
// with its suffixes stieg, gässchen and markt, its lowercase branch ("string
// 3" at the start of a word), a space before the letter of a house number
// ("Hauptstraße 12 b"), and after a postcode a second word ("Bad Homburg"),
// a slash ("Halle/Saale") or "im" with any word ("Berlin im Brief"); S, D
// and the word boundaries are Python's (Unicode), so no match starts or ends
// inside a word of another script any more ("München1" after a postcode
// stays unmasked). Go's preposition branch stays, so prose like "Im Jahr
// 2024" is masked (amendment A5).
const (
	addrS = pySpaceClass
	addrD = `\p{Nd}`
	// streetSuffixPy is Python's _SUF_PY (stra[ßs]e, then "strasse", added
	// by the spec for "Musterstrasse 12a"), streetSuffixGo Go's own list.
	streetSuffixPy = `stra[ßs]e|strasse|str\.?|weg|gasse|platz|allee|r` + turkishI + `ng|damm|ufer|chaussee|ze` + turkishI +
		`le|st` + turkishI + `eg|gässchen|pfad|markt`
	streetSuffixGo = `straße|strasse|str\.|str|weg|gasse|platz|allee|r` + turkishI + `ng|ufer|damm|chaussee|ze` + turkishI +
		`le|pfad|ste` + turkishI + `g|gäßchen|gaesschen`
	streetPrefixPy = `(?:[A-ZÄÖÜ][a-zäöüß]+(?:` + addrS + `+|-))*(?:[A-ZÄÖÜ][a-zäöüß]+)?(?i:` + streetSuffixPy + `)` +
		`|[a-zäöüß]+(?i:` + streetSuffixPy + `)`
	streetNumberPy = addrD + `+(?:` + addrS + `*[a-zA-Z])?(?:` + addrS + `*[-/]` + addrS + `*` + addrD + `{1,4}(?:` + addrS +
		`*[a-zA-Z])?)?`
	// The literal spaces of "An der", "Auf dem" ... stay single U+0020.
	streetPrefixGo = `(?:Am|An der|Auf dem|Auf der|Im|In der|Vor dem|Hinter dem|Zum|Zur)` + addrS + `+[A-ZÄÖÜ][a-zäöüßA-ZÄÖÜ0-9.-]+` +
		`(?:` + addrS + `+[A-ZÄÖÜ][a-zäöüßA-ZÄÖÜ0-9.-]+)*` +
		`|(?:[A-ZÄÖÜ][a-zäöüßA-ZÄÖÜ0-9.-]+` + addrS + `+)*(?:Straße|Strasse|Str\.|Str|[A-ZÄÖÜ][a-zäöüßA-ZÄÖÜ0-9.-]*(?i:` +
		streetSuffixGo + `))`
	// The house number range ends after at most 4 digits, so a following
	// 5-digit postcode is never read as a range end (the postcode step then
	// matches it on its own).
	streetNumberGo = addrD + `+[a-zA-Z]?(?:` + addrS + `*[-/]` + addrS + `*` + addrD + `{1,4}[a-zA-Z]?)?`
	plzPy          = addrD + `{5}` + addrS + `+[A-ZÄÖÜ][a-zäöüß]+(?:[-/][A-ZÄÖÜ][a-zäöüß]+)*` +
		`(?:` + addrS + `+(?:(?:am|an` + addrS + `+der|im)` + addrS + `+)?[A-ZÄÖÜ][a-zäöüß]+)?`
	plzGo = addrD + `{5}` + addrS + `+[A-ZÄÖÜ][a-zäöüßA-ZÄÖÜ.-]+` +
		`(?:` + addrS + `+(?:(?:am|an der)` + addrS + `+[A-ZÄÖÜ][a-zäöüßA-ZÄÖÜ.-]+|im Breisgau|ob der Tauber))?`
)

// secretKWPattern is ts_common.py's RX_KEY_VAL: keyword, [\w.-]*, an
// optional quote, S*, = or :, S* (group 1), then a value: "..." (group 2) or
// '...' (group 3), masked as a whole, whitespace included, or an unquoted one
// after an optional quote, up to the next A character, quote, comma or
// semicolon (group 4). Python's (?!<redacted>...) lookaheads are part of the
// value patterns (notRedacted): a value that is already "<redacted>" fails
// there and the search goes on (the S* before it giving back, later starts,
// later keywords), as in Python, instead of matching and being skipped. The
// lookaheads compare case-sensitively (amendment A3, 2026-10-02; Python wraps
// them in (?-i:...)): only the exact placeholder is skipped,
// "token=<REDACTED>" is masked. An unquoted value takes the character that ends it along
// (group 5, empty at the end of the text), which makes it the whole run, as
// Python's greedy [^...]+ after its lookahead is; no match can start with
// that character. Since 2026-10-02; before: ASCII \w and \s, a skip in code,
// and "Bearer"/"Basic" values left alone; until amendment A3 the lookaheads
// compared case-insensitively, as under Python's (?i) then.
var secretKWPattern = `(?i)(` + secretKWLead + `)` +
	`(?:("` + notRedacted(`"\n`, true) + `")|('` + notRedacted(`'\n`, true) + `')|["']?(` +
	notRedacted(`\t\n\f\r "',;`, false) + `)([\t\n\f\r "',;]|$))`

// notRedacted returns a pattern for one or more characters of the class
// [^ex] that are not "<redacted>" (exact) or do not start with it (!exact),
// compared case-sensitively (amendment A3; the pattern is wrapped in (?-i:)
// so that the (?i) around it does not fold it). RE2 has no lookahead; this
// is the same set as a regular expression: a string leaves "<redacted>"
// where it first differs (any rest may follow) or ends early; one that
// starts with all of it is in the set only when exact and longer. What
// follows must pin the end: the closing quote, which [^ex] excludes, or the
// end of the run, for a string that ends early must not be a prefix of a
// longer "<redacted>...". Where several branches could match, at most one
// fits the text, and [^ex]* is greedy, so the match is the longest, as
// [^ex]+ is. Until A3 each letter of the placeholder was compared in both
// cases.
func notRedacted(ex string, exact bool) string {
	const p = "<redacted>"
	run := `[^` + ex + `]`
	rest, ok := "", exact // the pattern after all of p, and whether there is one
	if exact {
		rest = run + `+`
	}
	for k := len(p) - 1; k >= 0; k-- {
		c := string(p[k])
		alt := `[^` + ex + c + `]` + run + `*`
		if ok {
			alt += `|` + c + rest
		}
		rest, ok = `(?:`+alt+`)`, true
		if k > 0 {
			rest += `?`
		}
	}
	return `(?-i:` + rest + `)`
}

var (
	bearerBasicRE = regexp.MustCompile(bearerBasicPattern)
	// secretKWRE: keyword, separator, then a quoted value (masked as a whole,
	// whitespace included) or an unquoted one. Same keyword list as ts_common.py
	// _MASK_KW; RE2 has no backreferences, so quote pairing is checked in code.
	secretKWRE = regexp.MustCompile(secretKWPattern)

	// ibanRE, phoneRE, awsKeyIDRE, knownTokenRE: the ts_common.py patterns; the
	// lookarounds Python uses are checked in code (replaceBoundedFast).
	ibanRE       = regexp.MustCompile(ibanPattern)
	ibanFullRE   = regexp.MustCompile(`^(?:` + ibanPattern + `)$`)
	phoneRE      = regexp.MustCompile(phonePattern)
	awsKeyIDRE   = regexp.MustCompile(awsKeyIDPattern)
	knownTokenRE = regexp.MustCompile(knownTokenPattern)

	// emailRE matches email addresses; letters and digits in any script, like
	// Python's Unicode \w in ts_common.RX_EMAIL (umlauts in local part and domain).
	emailRE = regexp.MustCompile(emailPattern)

	// opaqueCandidateRE matches candidates for long opaque strings (24+ base64/hex characters).
	opaqueCandidateRE = regexp.MustCompile(opaqueCandidatePattern)
)

const ibanPattern = `[A-Z]{2}\d{2}(?: ?[A-Z0-9]){11,30}`

// MaskCounts records the number of redactions by category.
type MaskCounts struct {
	SecretKW int `json:"secret_kw"`
	Email    int `json:"email"`
	Opaque   int `json:"opaque"`
	Address  int `json:"address"`
	Name     int `json:"name"`
	IBAN     int `json:"iban"`
	Phone    int `json:"phone"`
}

func isASCIIAlnum(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')
}

// getNamesFilePath resolves the path to the names file:
// 1. TYPESAFE_NAMES_FILE environment variable
// 2. Fallback to ~/.config/typesafe/names.txt
func getNamesFilePath() string {
	if p := strings.TrimSpace(os.Getenv("TYPESAFE_NAMES_FILE")); p != "" {
		return expandHome(p)
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	return filepath.Join(home, ".config", "typesafe", "names.txt")
}

func expandHome(path string) string {
	if strings.HasPrefix(path, "~/") {
		if home, err := os.UserHomeDir(); err == nil && home != "" {
			return filepath.Join(home, path[2:])
		}
	} else if path == "~" {
		if home, err := os.UserHomeDir(); err == nil && home != "" {
			return home
		}
	}
	return path
}

// loadNames reads names from the specified file.
// If the file is missing or unreadable, returns nil cleanly (0 hits).
// Lines with comments '#' are ignored. Names are split into first and last name components,
// and sorted by length descending (with alphabetical secondary sort for determinism).
// Each line is put into Unicode NFC first, as ts_common.load_names does; a
// term is kept if it has at least two code points in NFC (mask parity spec,
// amendment A2, 2026-10-02: a single letter identifies nobody and would mask
// every lone "Ö"; before, Go counted bytes, so "Ö" or "é" alone was a name,
// and a name stored decomposed matched only decomposed text).
func loadNames(filePath string) []string {
	names, _ := loadNamesDetail(filePath)
	return names
}

// latin1 decodes s byte by byte as ISO 8859-1, which always succeeds.
func latin1(s string) string {
	r := make([]rune, len(s))
	for i := 0; i < len(s); i++ {
		r[i] = rune(s[i])
	}
	return string(r)
}

// loadNamesDetail is loadNames; latin1Lines counts the lines that were not
// valid UTF-8 and were read as Latin-1 instead (a names file saved in
// ISO 8859-1, say), so that they still mask. Lines that are empty or hold
// only control characters are skipped.
func loadNamesDetail(filePath string) (names []string, latin1Lines int) {
	if filePath == "" {
		return nil, 0
	}
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, 0
	}
	lines := strings.Split(string(data), "\n")
	nameSet := make(map[string]bool)

	for _, line := range lines {
		if idx := strings.Index(line, "#"); idx != -1 {
			line = line[:idx]
		}
		if !utf8.ValidString(line) {
			line = latin1(line)
			latin1Lines++
		}
		line = nfc(strings.TrimSpace(line))
		if line == "" || strings.IndexFunc(line, func(r rune) bool { return !unicode.IsControl(r) }) < 0 {
			continue
		}

		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}

		if len(fields) > 1 {
			fullName := strings.Join(fields, " ")
			nameSet[fullName] = true
		}

		for _, f := range fields {
			if utf8.RuneCountInString(f) >= 2 {
				nameSet[f] = true
			}
		}
	}

	if len(nameSet) == 0 {
		return nil, latin1Lines
	}

	names = make([]string, 0, len(nameSet))
	for name := range nameSet {
		names = append(names, name)
	}

	sort.Slice(names, func(i, j int) bool {
		if len(names[i]) != len(names[j]) {
			return len(names[i]) > len(names[j])
		}
		return names[i] < names[j]
	})

	return names, latin1Lines
}

// nameRegexCache holds the compiled name patterns of one names file, so that
// MaskDetail does not read the file and compile one regex per name on every
// call. It is refreshed when the path, size or modification time changes.
var nameRegexCache struct {
	sync.Mutex
	path  string
	size  int64
	mod   time.Time
	names nameSet
	ok    bool
}

// nameSet is the names file, ready to mask with: one case-insensitive,
// word-bounded pattern per name, longest name first (the order loadNames
// gives), and the same names as literal matchers.
type nameSet struct {
	matchers []nameMatcher
	// latin1 counts the lines of the names file read as Latin-1 because
	// they were not valid UTF-8. The names themselves are never logged.
	latin1 int
}

// loadNameSet reads the names file through the cache.
func loadNameSet() nameSet {
	path := getNamesFilePath()
	if path == "" {
		return nameSet{}
	}
	st, err := os.Stat(path)
	if err != nil {
		return nameSet{}
	}
	c := &nameRegexCache
	c.Lock()
	defer c.Unlock()
	if c.ok && c.path == path && c.size == st.Size() && c.mod.Equal(st.ModTime()) {
		return c.names
	}
	var ns nameSet
	names, latin1Lines := loadNamesDetail(path)
	ns.latin1 = latin1Lines
	for _, name := range names {
		ns.matchers = append(ns.matchers, newNameMatcher(name))
	}
	c.path, c.size, c.mod, c.names, c.ok = path, st.Size(), st.ModTime(), ns, true
	return ns
}

// maskLapFn, set only by the profile test, is told when a masking step ends.
var maskLapFn func(step string)

func maskLap(step string) {
	if maskLapFn != nil {
		maskLapFn(step)
	}
}

// MaskDetail redacts sensitive patterns in text before transmission, in the same
// order as ts_common.py MASK_RES: secret_kw (bearer/basic, keywords), email, iban,
// phone, address (street, PLZ+Ort), name, opaque (AWS key IDs, known token
// prefixes, 24+ character strings with letters and a digit in or right after them).
func MaskDetail(text string) (string, MaskCounts) {
	var counts MaskCounts

	// 1. Bearer / Basic: the value is replaced, the trigger and the spaces
	// after it are kept. A value that is "<redacted>" counts too, as in
	// ts_common.py (since 2026-10-02).
	text = maskBearer(text, func(m string) string {
		counts.SecretKW++
		if sub := bearerBasicRE.FindStringSubmatch(m); len(sub) >= 3 {
			return sub[1] + "<redacted>"
		}
		return "<redacted>"
	})
	maskLap("bearer")

	// 2. Secret keywords with = or :; a quoted value is masked as a whole,
	// quotes kept; an unquoted one, with a quote before it, without it. A
	// "Bearer" or "Basic" value is masked like any other (since 2026-10-02):
	// "authorization: Bearer x" becomes "authorization: <redacted> <redacted>".
	// Only the exact placeholder "<redacted>" is passed over as a value
	// (amendment A3, 2026-10-02): "token=<REDACTED>" is masked.
	text = maskSecretKW(text, func(m string) string {
		counts.SecretKW++
		sub := secretKWRE.FindStringSubmatch(m)
		switch {
		case len(sub) < 6:
			return "<redacted>"
		case sub[2] != "":
			return sub[1] + `"<redacted>"`
		case sub[3] != "":
			return sub[1] + `'<redacted>'`
		}
		return sub[1] + "<redacted>" + sub[5]
	})
	maskLap("secret_kw")

	// 3. email
	text = maskEmail(text, func(m string) string {
		counts.Email++
		return "<email>"
	})
	maskLap("email")

	// 4. IBAN (no letter or digit directly before or after)
	text = replaceBoundedFast(text, ibanAt, 64, ibanFullRE, ibanCand(text), notASCIIAlnum, func(prev, next rune) bool {
		return !isASCIIAlnum(prev) && !isASCIIAlnum(next)
	}, false, func(string) string {
		counts.IBAN++
		return "<iban>"
	})
	maskLap("iban")

	// 5. German phone numbers: +49 or a leading 0, then 8+ digits of any
	// script (no W, "+" or "." directly before, no D after). Since
	// 2026-10-02 a number of any script blocks before it ("²", "Ⅻ"), and
	// the digits after the 0 may be of any script.
	phonePrevOK := func(prev rune) bool {
		return !(isNameWordRune(prev) || prev == '+' || prev == '.')
	}
	text = replaceBoundedFast(text, phoneAt, 0, nil, phoneCand(text), phonePrevOK, func(prev, next rune) bool {
		return phonePrevOK(prev) && !unicode.IsDigit(next)
	}, true, func(string) string {
		counts.Phone++
		return "<phone>"
	})
	maskLap("phone")

	// 6. address: Straße + Hausnummer
	text = maskStreet(text, func(m string) string {
		counts.Address++
		return "<address>"
	})
	maskLap("street")

	// 7. address: PLZ + Ort
	text = maskPlzOrt(text, func(m string) string {
		counts.Address++
		return "<address>"
	})
	maskLap("plz")

	// 8. names: from TYPESAFE_NAMES_FILE or ~/.config/typesafe/names.txt
	// Case-insensitive to match ts_common.py's get_name_regex ((?i)), e.g.
	// "max mustermann" lowercase must also be masked.
	nameRepl := func(m string) string {
		counts.Name++
		return "<name>"
	}
	// Word boundaries are Unicode ones, as in ts_common.py (Python's \b): a
	// letter, a number or "_" on both sides of a name's edge means no
	// boundary. Since 2026-10-02; before, Go's ASCII \b let "Herr Özil" or
	// "Frau Strauß" out unmasked. Text that is not in NFC is matched on an
	// NFC copy and replaced in the original (maskNames, mask_nfc.go; since
	// 2026-10-02, mask parity spec section 5 with amendments A1, A2, A8).
	text = maskNames(text, loadNameSet().matchers, nameRepl)
	maskLap("names")

	// 9. opaque: AWS key IDs and known token prefixes (not inside a word), then
	// 24+ token chars with an ASCII letter and an ASCII digit, or with a
	// decimal digit of any script right after them (maskOpaque; since
	// 2026-10-03, mask parity spec amendment A9, as ts_common.py's RX_OPAQUE).
	opaque := func(string) string {
		counts.Opaque++
		return "<redacted>"
	}
	text = replaceBoundedFast(text, awsKeyIDAt, 20, nil, awsCand(text), notASCIIAlnum, func(prev, next rune) bool {
		return !isASCIIAlnum(prev) && !isASCIIAlnum(next)
	}, false, opaque)
	maskLap("aws")
	text = replaceBoundedFast(text, knownTokenAt, 0, nil, knownTokenCand(text), notASCIIAlnum,
		func(prev, _ rune) bool { return !isASCIIAlnum(prev) }, false, opaque)
	maskLap("known")
	text = maskOpaque(text, opaque)
	maskLap("opaque")

	return text, counts
}

// Mask redacts sensitive patterns and returns the masked string and total redaction count.
func Mask(text string) (string, int) {
	masked, counts := MaskDetail(text)
	return masked, counts.SecretKW + counts.Email + counts.Opaque + counts.Address + counts.Name + counts.IBAN + counts.Phone
}

// --- Key Lookup --------------------------------------------------------------

const KeyLookupTimeout = 800 * time.Millisecond

var (
	getKeyFn            = defaultGetKey
	getKeyWithContextFn = defaultGetKeyWithContext
)

// GetKey searches for the TypeSafe API key:
// 1. Environment variable TYPESAFE_API_KEY
// 2. Fallback to `secret-tool lookup service typesafe key api` within KeyLookupTimeout (800ms)
// Returns empty string if no key is found or lookup fails.
func GetKey() string {
	return getKeyFn()
}

// GetKeyWithContext searches for the TypeSafe API key using the given parent context.
// Secret-tool execution is bounded by KeyLookupTimeout (800ms) or parent context deadline.
// Fails open immediately if lookup times out or errors.
func GetKeyWithContext(ctx context.Context) string {
	return getKeyWithContextFn(ctx)
}

func defaultGetKey() string {
	return defaultGetKeyWithContext(context.Background())
}

func defaultGetKeyWithContext(parentCtx context.Context) string {
	if key := strings.TrimSpace(os.Getenv("TYPESAFE_API_KEY")); key != "" {
		return key
	}
	ctx, cancel := context.WithTimeout(parentCtx, KeyLookupTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "secret-tool", "lookup", "service", "typesafe", "key", "api")
	out, err := cmd.Output()
	if err == nil {
		if key := strings.TrimSpace(string(out)); key != "" {
			return key
		}
	}
	return ""
}

// --- Allowlist & Endpoint Verification ---------------------------------------

// isAllowedEndpoint ensures requests only target api.typesafe.ai,
// or local loopback in testing environments.
func isAllowedEndpoint(rawURL string) bool {
	u, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	if u.Scheme == "https" && u.Host == "api.typesafe.ai" && u.Path == "/v1/systemone" {
		return true
	}
	// Loopback allowed for local tests (httptest.Server)
	if (u.Scheme == "http" || u.Scheme == "https") && (u.Hostname() == "127.0.0.1" || u.Hostname() == "localhost") {
		return true
	}
	return false
}

// --- System One Types --------------------------------------------------------

type QuestionType string

const (
	TypeNoul   QuestionType = "noul"
	TypeScore  QuestionType = "score"
	TypeChoice QuestionType = "choice"
)

// Question represents a typed judgment question for TypeSafe System One.
type Question struct {
	Type         QuestionType `json:"type"`
	Instructions string       `json:"instructions"`
	Criteria     any          `json:"criteria,omitempty"`
}

// NewNoulQuestion creates a binary probability (0..1) question.
func NewNoulQuestion(instructions string) Question {
	return Question{
		Type:         TypeNoul,
		Instructions: instructions,
	}
}

// NewScoreQuestion creates a discrete ordinal score question.
func NewScoreQuestion(instructions string, criteria []string) Question {
	return Question{
		Type:         TypeScore,
		Instructions: instructions,
		Criteria:     criteria,
	}
}

// NewChoiceQuestion creates a multi-class choice question.
func NewChoiceQuestion(instructions string, criteria map[string]string) Question {
	return Question{
		Type:         TypeChoice,
		Instructions: instructions,
		Criteria:     criteria,
	}
}

// Request is the payload sent to /v1/systemone.
type Request struct {
	State     string              `json:"state"`
	Model     string              `json:"model"`
	Questions map[string]Question `json:"questions"`
}

// RawAnswer contains the answer fields returned by System One.
type RawAnswer struct {
	Noul          *float64           `json:"noul,omitempty"`
	Choice        *string            `json:"choice,omitempty"`
	Confidence    *float64           `json:"confidence,omitempty"`
	Probabilities map[string]float64 `json:"probabilities,omitempty"`
}

// Response is the structured output from /v1/systemone.
type Response struct {
	Answers map[string]RawAnswer `json:"answers"`
}

// Noul extracts the probability of a noul answer (0..1).
func (r *Response) Noul(qid string) (float64, bool) {
	if r == nil || r.Answers == nil {
		return 0, false
	}
	ans, ok := r.Answers[qid]
	if !ok || ans.Noul == nil {
		return 0, false
	}
	return *ans.Noul, true
}

// ScoreLevel returns the argmax level and confidence for a score answer.
func (r *Response) ScoreLevel(qid string) (int, float64, bool) {
	if r == nil || r.Answers == nil {
		return 0, 0, false
	}
	ans, ok := r.Answers[qid]
	if !ok || len(ans.Probabilities) == 0 {
		return 0, 0, false
	}
	bestLevel := -1
	bestProb := -1.0
	for k, v := range ans.Probabilities {
		lvl, err := strconv.Atoi(k)
		if err != nil {
			continue
		}
		if v > bestProb {
			bestProb = v
			bestLevel = lvl
		}
	}
	if bestLevel == -1 {
		return 0, 0, false
	}
	conf := 0.0
	if ans.Confidence != nil {
		conf = *ans.Confidence
	}
	return bestLevel, conf, true
}

// Choice returns the chosen label, confidence, and distribution.
func (r *Response) Choice(qid string) (string, float64, map[string]float64, bool) {
	if r == nil || r.Answers == nil {
		return "", 0, nil, false
	}
	ans, ok := r.Answers[qid]
	if !ok || ans.Choice == nil {
		return "", 0, nil, false
	}
	conf := 0.0
	if ans.Confidence != nil {
		conf = *ans.Confidence
	}
	return *ans.Choice, conf, ans.Probabilities, true
}

// --- Client ------------------------------------------------------------------

// Client is a Fail-Open client for the TypeSafe System One API.
type Client struct {
	Endpoint   string
	APIKey     string
	Model      string
	Timeout    time.Duration
	HTTPClient *http.Client
}

// NewClient returns a TypeSafe client configured with defaults.
func NewClient(apiKey string) *Client {
	return &Client{
		Endpoint: DefaultEndpoint,
		APIKey:   apiKey,
		Model:    DefaultModel,
		Timeout:  DefaultTimeout,
		HTTPClient: &http.Client{
			Timeout: DefaultTimeout,
		},
	}
}

// Post sends a batched System One request.
// All text in state is capped at MaxPayloadBytes and masked before transmission.
// Fail-Open: returns (nil, err) on any network/JSON error without panicking.
func (c *Client) Post(ctx context.Context, state string, questions map[string]Question) (*Response, error) {
	if c.APIKey == "" {
		return nil, errors.New("typesafe: no api key")
	}
	if !isAllowedEndpoint(c.Endpoint) {
		return nil, fmt.Errorf("typesafe: endpoint %q not allowed", c.Endpoint)
	}

	if len(state) > MaxPayloadBytes {
		state = state[:MaxPayloadBytes]
	}

	maskedState, _ := Mask(state)

	reqBody := Request{
		State:     maskedState,
		Model:     c.Model,
		Questions: questions,
	}

	rawJSON, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("typesafe: marshal request: %w", err)
	}

	timeout := c.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}

	reqCtx := ctx
	var cancel context.CancelFunc
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		reqCtx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}

	httpReq, err := http.NewRequestWithContext(reqCtx, http.MethodPost, c.Endpoint, bytes.NewReader(rawJSON))
	if err != nil {
		return nil, fmt.Errorf("typesafe: new request: %w", err)
	}

	httpReq.Header.Set("Authorization", "Bearer "+c.APIKey)
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("User-Agent", "imprint-dev/0.1")

	client := c.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}

	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("typesafe: do request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodySnippet, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("typesafe: unexpected status %d: %s", resp.StatusCode, strings.TrimSpace(string(bodySnippet)))
	}

	var res Response
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return nil, fmt.Errorf("typesafe: decode response: %w", err)
	}

	return &res, nil
}

// --- Knowledge-Keeping Check (CL-001) ----------------------------------------

// KnowledgeKeepingQuestion returns the Jev primitive evaluating missing provenance.
func KnowledgeKeepingQuestion() Question {
	return Question{
		Type:         TypeNoul,
		Instructions: "Does this text record or register a factual statement, decision, rule, or learning without specifying an explicit calendar date (in YYYY-MM-DD format), a clear source (where it was obtained), or an acquisition method?",
	}
}

// CheckKnowledgeKeeping evaluates whether a text being written to a register or memory
// appears to record facts/decisions without date, source or method.
// Fail-Open: on any error, returns (false, "", err).
func CheckKnowledgeKeeping(ctx context.Context, client *Client, text string) (bool, string, error) {
	if client == nil || client.APIKey == "" {
		return false, "", nil
	}

	if len(text) > MaxPayloadBytes {
		text = text[:MaxPayloadBytes]
	}

	questions := map[string]Question{
		KnowledgeKeepingQuestionID: KnowledgeKeepingQuestion(),
	}

	resp, err := client.Post(ctx, text, questions)
	if err != nil {
		return false, "", err
	}

	prob, ok := resp.Noul(KnowledgeKeepingQuestionID)
	if !ok {
		return false, "", nil
	}

	if prob >= 0.5 {
		return true, KnowledgeKeepingNotice, nil
	}

	return false, "", nil
}

// --- Rule Enforcement Check (CL-002) ----------------------------------------

// RuleEnforcementQuestion returns the Jev primitive evaluating missing rule enforcement.
func RuleEnforcementQuestion() Question {
	return Question{
		Type:         TypeNoul,
		Instructions: "Does this rule text or guideline state a mandatory rule, constraint, or policy without naming what enforces it (a specific tool, hook, script, check, linter, or stating explicitly that nothing does)?",
	}
}

// CheckRuleEnforcement evaluates whether a rule or guideline text appears to state rules
// without naming what enforces it or explicitly stating that nothing does.
// Fail-Open: on any error, returns (false, "", err).
func CheckRuleEnforcement(ctx context.Context, client *Client, text string) (bool, string, error) {
	if client == nil || client.APIKey == "" {
		return false, "", nil
	}

	if len(text) > MaxPayloadBytes {
		text = text[:MaxPayloadBytes]
	}

	questions := map[string]Question{
		RuleEnforcementQuestionID: RuleEnforcementQuestion(),
	}

	resp, err := client.Post(ctx, text, questions)
	if err != nil {
		return false, "", err
	}

	prob, ok := resp.Noul(RuleEnforcementQuestionID)
	if !ok {
		return false, "", nil
	}

	if prob >= 0.5 {
		return true, RuleEnforcementNotice, nil
	}

	return false, "", nil
}

// --- Duplicate Fact Check (CL-003) -------------------------------------------

// DuplicateFactQuestion returns the Jev primitive evaluating duplicate facts or decisions.
func DuplicateFactQuestion() Question {
	return Question{
		Type:         TypeNoul,
		Instructions: "State contains an existing record and a new record. Does the new record assert or register the same factual statement, decision, or invariant already established in the existing record, without explicitly referencing, amending, or superseding it?",
	}
}

// CheckDuplicateFact evaluates whether a new record duplicates an existing record in candidates.
// Fail-Open: on any error, returns (false, "", err).
func CheckDuplicateFact(ctx context.Context, client *Client, candidates []string, newText string) (bool, string, error) {
	if client == nil || client.APIKey == "" || len(candidates) == 0 {
		return false, "", nil
	}

	state := fmt.Sprintf("Existing record:\n%s\n\nNew record:\n%s", strings.Join(candidates, "\n"), newText)
	if len(state) > MaxPayloadBytes {
		state = state[:MaxPayloadBytes]
	}

	questions := map[string]Question{
		DuplicateFactQuestionID: DuplicateFactQuestion(),
	}

	resp, err := client.Post(ctx, state, questions)
	if err != nil {
		return false, "", err
	}

	prob, ok := resp.Noul(DuplicateFactQuestionID)
	if !ok {
		return false, "", nil
	}

	if prob >= 0.6 {
		return true, DuplicateFactNotice, nil
	}

	return false, "", nil
}

// --- Skill Suggestion Check (CL-004) ----------------------------------------

// SkillSuggestionQuestion returns the Jev primitive evaluating which imprint skill is most relevant.
func SkillSuggestionQuestion() Question {
	return Question{
		Type: TypeChoice,
		Instructions: "Which imprint skill is most relevant to the user's prompt or task?\n" +
			"- delegation-contract: When dispatching agents, delegating tasks to subagents, multi-agent workflows, code reviews by another agent, resolving disagreements between agents, choosing models.\n" +
			"- knowledge-keeping: When recording, updating, or correcting facts, decisions, sources, dates, deadlines, memory entries ('merk dir', 'halte fest', 'notier', 'remember this', 'supersede').\n" +
			"- measure-before-asserting: When verifying claims before stating them, checking system properties, configs, versions, paths, measuring performance, or checking if something is still true ('stimmt das noch', 'is that still true').\n" +
			"- session-handover: When closing or ending a session, handing over tasks, preparing wrap-up, context limit reached, committing final work, or saying goodbye ('that's it for today', 'Schluss für heute', 'Übergabe').\n" +
			"- none: General coding, questions, refactoring, or tasks where none of the specialized imprint governance skills apply.",
		Criteria: map[string]string{
			"delegation-contract":      "Task delegation, subagents, reviews, agent arbitration, model choice",
			"knowledge-keeping":        "Recording facts, decisions, provenance, memory notes, superseding facts",
			"measure-before-asserting": "Verification before asserting, measuring properties, checking reality vs notes",
			"session-handover":         "Closing session, handoff, committing runnable work, wrapping up",
			"none":                     "No specific imprint skill applies",
		},
	}
}

// ImprintSkillsAllowlist defines the fixed allowlist of the 4 canonical imprint skills.
var ImprintSkillsAllowlist = map[string]bool{
	"delegation-contract":      true,
	"knowledge-keeping":        true,
	"measure-before-asserting": true,
	"session-handover":         true,
}

// CheckSkillSuggestion evaluates whether a user prompt matches an imprint skill.
// Returns the skill name only if one of the 4 allowed imprint skills is chosen.
// If another value is returned (hallucination or unknown skill), it is discarded.
// Fail-Open: on any error, returns ("", err).
func CheckSkillSuggestion(ctx context.Context, client *Client, promptText string) (string, error) {
	if client == nil || client.APIKey == "" {
		return "", nil
	}

	if len(promptText) > MaxPayloadBytes {
		promptText = promptText[:MaxPayloadBytes]
	}

	questions := map[string]Question{
		SkillSuggestionQuestionID: SkillSuggestionQuestion(),
	}

	resp, err := client.Post(ctx, promptText, questions)
	if err != nil {
		return "", err
	}

	choice, _, _, ok := resp.Choice(SkillSuggestionQuestionID)
	if !ok {
		return "", nil
	}

	if ImprintSkillsAllowlist[choice] {
		return choice, nil
	}

	return "", nil
}

// defaultStopwords contains common German and English stopwords excluded from keyword overlap.
var defaultStopwords = map[string]struct{}{
	// German
	"der": {}, "die": {}, "das": {}, "den": {}, "dem": {}, "des": {},
	"ein": {}, "eine": {}, "einer": {}, "eines": {}, "einem": {}, "einen": {},
	"und": {}, "oder": {}, "aber": {}, "denn": {}, "doch": {}, "als": {}, "wie": {},
	"in": {}, "im": {}, "an": {}, "am": {}, "auf": {}, "aus": {}, "bei": {}, "mit": {},
	"nach": {}, "von": {}, "vom": {}, "zu": {}, "zur": {}, "zum": {}, "vor": {},
	"über": {}, "ueber": {}, "unter": {}, "durch": {}, "für": {}, "fuer": {}, "um": {},
	"ist": {}, "sind": {}, "war": {}, "waren": {}, "wird": {}, "werden": {}, "wurde": {}, "wurden": {},
	"sein": {}, "seine": {}, "seinem": {}, "seinen": {}, "seiner": {}, "ihr": {}, "ihre": {},
	"hat": {}, "haben": {}, "hatte": {}, "hatten": {},
	"es": {}, "er": {}, "sie": {}, "wir": {}, "man": {}, "sich": {},
	"nicht": {}, "kein": {}, "keine": {}, "keinen": {}, "keinem": {}, "keiner": {},
	"auch": {}, "so": {}, "dass": {}, "da": {}, "nur": {}, "noch": {}, "hier": {},
	"dies": {}, "diese": {}, "dieser": {}, "dieses": {}, "diesem": {}, "diesen": {},
	// English
	"the": {}, "a": {},
	"and": {}, "or": {}, "but": {}, "nor": {},
	"on": {}, "at": {}, "to": {}, "for": {}, "of": {}, "with": {}, "by": {},
	"from": {}, "up": {}, "about": {}, "into": {}, "over": {}, "after": {}, "under": {},
	"is": {}, "are": {}, "was": {}, "were": {}, "be": {}, "been": {}, "being": {},
	"have": {}, "has": {}, "had": {}, "do": {}, "does": {}, "did": {},
	"will": {}, "would": {}, "shall": {}, "should": {}, "can": {}, "could": {},
	"may": {}, "might": {}, "must": {},
	"it": {}, "its": {}, "this": {}, "that": {}, "these": {}, "those": {},
	"not": {}, "no": {}, "all": {}, "any": {}, "both": {}, "each": {},
	"more": {}, "most": {}, "other": {}, "some": {}, "such": {}, "only": {}, "same": {}, "than": {}, "too": {}, "very": {},
}

var tokenRE = regexp.MustCompile(`[\p{L}\p{N}_]+`)

func isTableSeparator(s string) bool {
	if !strings.HasPrefix(s, "|") {
		return false
	}
	trimmed := strings.ReplaceAll(s, "|", "")
	trimmed = strings.ReplaceAll(trimmed, "-", "")
	trimmed = strings.ReplaceAll(trimmed, ":", "")
	trimmed = strings.TrimSpace(trimmed)
	return trimmed == ""
}

func extractKeywords(text string) map[string]struct{} {
	words := make(map[string]struct{})
	lower := strings.ToLower(text)
	rawTokens := tokenRE.FindAllString(lower, -1)
	for _, tok := range rawTokens {
		tok = strings.Trim(tok, "_-")
		if tok == "" {
			continue
		}
		var parts []string
		if strings.Contains(tok, "_") {
			parts = strings.FieldsFunc(tok, func(r rune) bool {
				return r == '_'
			})
		}
		parts = append(parts, tok)

		for _, p := range parts {
			if len(p) < 2 {
				continue
			}
			hasLetter := false
			for _, r := range p {
				if unicode.IsLetter(r) {
					hasLetter = true
					break
				}
			}
			if !hasLetter {
				continue
			}
			if _, isStop := defaultStopwords[p]; isStop {
				continue
			}
			words[p] = struct{}{}
		}
	}
	return words
}

// findDuplicateCandidates scans filePath for existing lines that share significant keywords
// with newText. Returns top 1-3 candidate lines with at least 2 shared terms, capped at 500 chars total.
// Returns nil if file does not exist, cannot be read, or no candidate has >= 2 shared terms.
func findDuplicateCandidates(filePath, newText string) []string {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil
	}

	newKeywords := extractKeywords(newText)
	if len(newKeywords) < 2 {
		return nil
	}

	lines := strings.Split(string(data), "\n")
	type candidateMatch struct {
		line    string
		overlap int
		index   int
	}

	var matches []candidateMatch
	seenLines := make(map[string]bool)

	for i, rawLine := range lines {
		line := strings.TrimSpace(rawLine)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "#") {
			continue
		}
		if isTableSeparator(line) {
			continue
		}
		if strings.HasPrefix(line, "```") || strings.HasPrefix(line, "~~~") {
			continue
		}
		if seenLines[line] {
			continue
		}

		lineKeywords := extractKeywords(line)
		overlap := 0
		for kw := range newKeywords {
			if _, ok := lineKeywords[kw]; ok {
				overlap++
			}
		}

		if overlap >= 2 {
			seenLines[line] = true
			matches = append(matches, candidateMatch{
				line:    line,
				overlap: overlap,
				index:   i,
			})
		}
	}

	if len(matches) == 0 {
		return nil
	}

	sort.SliceStable(matches, func(i, j int) bool {
		if matches[i].overlap != matches[j].overlap {
			return matches[i].overlap > matches[j].overlap
		}
		return matches[i].index < matches[j].index
	})

	var result []string
	totalLen := 0
	for _, m := range matches {
		line := m.line
		if len(result) == 0 && len(line) > 500 {
			line = line[:500]
		}
		addedLen := len(line)
		if len(result) > 0 {
			addedLen += 1
		}
		if totalLen+addedLen > 500 {
			if len(result) > 0 {
				break
			}
		}
		result = append(result, line)
		totalLen += addedLen
		if len(result) == 3 {
			break
		}
	}

	return result
}

// --- Hook Payload & Subcommand (CL-000, CL-001, CL-002, CL-003) ----------------

// isCodeOrBuildOrTestFile identifies non-content files (source code, build definitions, tests)
// that should never be analyzed by TypeSafe System One.
func isCodeOrBuildOrTestFile(cleanPath string) bool {
	lowerPath := strings.ToLower(cleanPath)
	base := filepath.Base(cleanPath)
	lowerBase := strings.ToLower(base)
	ext := strings.ToLower(filepath.Ext(cleanPath))

	// Source code files are never memory/register or rule files.
	codeExts := map[string]bool{
		".go": true, ".py": true, ".sh": true, ".bash": true, ".zsh": true,
		".rs": true, ".c": true, ".h": true, ".cpp": true, ".hpp": true,
		".cc": true, ".hh": true, ".js": true, ".ts": true, ".jsx": true,
		".tsx": true, ".java": true, ".rb": true, ".php": true, ".cs": true,
		".swift": true, ".kt": true,
	}
	if codeExts[ext] {
		return true
	}

	// Factory and build files are never memory/register or rule files.
	if strings.Contains(lowerBase, "factory") || strings.Contains(lowerBase, "build") ||
		lowerBase == "makefile" || lowerBase == "dockerfile" || lowerBase == "containerfile" {
		return true
	}

	// Test files (test scripts, test fixtures, unit tests) are excluded.
	if strings.Contains(lowerBase, "_test.") || strings.Contains(lowerBase, ".test.") ||
		strings.Contains(lowerBase, ".spec.") || strings.HasPrefix(lowerBase, "test_") ||
		strings.HasPrefix(lowerBase, "test-") || lowerBase == "test.md" || lowerBase == "test.txt" ||
		strings.HasPrefix(lowerPath, "test/") || strings.Contains(lowerPath, "/test/") ||
		strings.HasPrefix(lowerPath, "tests/") || strings.Contains(lowerPath, "/tests/") {
		return true
	}

	return false
}

// isMemoryOrRegisterFile returns true if path is a memory, decision, or register file
// (memory/, entscheide.md, offene-entscheide.md, register.md, MEMORY.md).
// Non-memory files, source code files, build/factory files, and test files always return false.
func isMemoryOrRegisterFile(path string) bool {
	cleanPath := strings.TrimSpace(path)
	if cleanPath == "" || cleanPath == "." {
		return false
	}
	cleanPath = filepath.ToSlash(filepath.Clean(cleanPath))
	if isCodeOrBuildOrTestFile(cleanPath) {
		return false
	}

	lowerPath := strings.ToLower(cleanPath)
	lowerBase := strings.ToLower(filepath.Base(cleanPath))

	// Exact filenames / basenames.
	switch lowerBase {
	case "entscheide.md", "offene-entscheide.md", "register.md", "memory.md":
		return true
	}

	// Fixed folder path: memory/ or /memory/
	if strings.HasPrefix(lowerPath, "memory/") || strings.Contains(lowerPath, "/memory/") {
		return true
	}

	return false
}

// isRuleFile returns true if path is a rule or skill guideline file
// (e.g., rules/, skills/, AGENTS.md, agents.md).
// Non-rule files, source code files, build/factory files, and test files always return false.
func isRuleFile(path string) bool {
	cleanPath := strings.TrimSpace(path)
	if cleanPath == "" || cleanPath == "." {
		return false
	}
	cleanPath = filepath.ToSlash(filepath.Clean(cleanPath))
	if isCodeOrBuildOrTestFile(cleanPath) {
		return false
	}

	lowerPath := strings.ToLower(cleanPath)
	lowerBase := strings.ToLower(filepath.Base(cleanPath))

	// Exact filenames / basenames.
	switch lowerBase {
	case "agents.md", "rules.md":
		return true
	}

	// Fixed folder paths: rules/ or skills/
	prefixes := []string{"rules/", "skills/"}
	for _, p := range prefixes {
		if strings.HasPrefix(lowerPath, p) || strings.Contains(lowerPath, "/"+p) {
			return true
		}
	}

	return false
}

// extractFileAndContent parses tool input fields across Claude Code, Antigravity, and Codex conventions.
func extractFileAndContent(toolInput map[string]any) (string, string) {
	if toolInput == nil {
		return "", ""
	}

	filePath := ""
	for _, k := range []string{"file_path", "path", "TargetFile", "target_file", "filePath", "file"} {
		if v, ok := toolInput[k]; ok {
			if s, ok := v.(string); ok && s != "" {
				filePath = s
				break
			}
		}
	}

	content := ""
	for _, k := range []string{"new_string", "CodeContent", "ReplacementContent", "code_content", "replacement_content", "content", "text", "replacement"} {
		if v, ok := toolInput[k]; ok {
			if s, ok := v.(string); ok && s != "" {
				content = s
				break
			}
		}
	}

	return filePath, content
}

// HookOutput structures the response sent back to Claude Code or Antigravity.
type HookOutput struct {
	HookSpecificOutput HookSpecificOutput `json:"hookSpecificOutput"`
	InjectSteps        []InjectStep       `json:"injectSteps,omitempty"`
}

type HookSpecificOutput struct {
	HookEventName     string `json:"hookEventName"`
	AdditionalContext string `json:"additionalContext"`
}

type InjectStep struct {
	EphemeralMessage string `json:"ephemeralMessage"`
}

// runHookTypesafeCheck executes the hook-typesafe-check CLI subcommand.
// Never blocks or crashes: exits 0 on all paths.
func runHookTypesafeCheck(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("hook-typesafe-check", flag.ContinueOnError)
	fs.SetOutput(stderr)

	endpoint := fs.String("endpoint", DefaultEndpoint, "TypeSafe API endpoint")
	timeoutSec := fs.Int("timeout", 8, "timeout in seconds (max 8)")

	if err := fs.Parse(args); err != nil {
		return exitOK // fail-open
	}

	// Read hook payload from stdin (up to 2MB).
	rawInput, err := io.ReadAll(io.LimitReader(stdin, 2*1024*1024))
	if err != nil || len(bytes.TrimSpace(rawInput)) == 0 {
		return exitOK
	}

	var payload struct {
		HookEventName string         `json:"hook_event_name"`
		ToolName      string         `json:"tool_name"`
		ToolInput     map[string]any `json:"tool_input"`
		// Raw content fallback if tool_input not structured
		Content string `json:"content"`
	}

	if err := json.Unmarshal(rawInput, &payload); err != nil {
		return exitOK
	}

	filePath, text := extractFileAndContent(payload.ToolInput)
	if text == "" && payload.Content != "" {
		text = payload.Content
	}

	if len(text) > MaxPayloadBytes {
		text = text[:MaxPayloadBytes]
	}

	isRule := isRuleFile(filePath)
	isMemory := isMemoryOrRegisterFile(filePath)

	// Optimization: check path and trivial content FIRST before doing any key search.
	// Non-relevant files exit immediately without triggering secret-tool.
	if (!isRule && !isMemory) || len(strings.TrimSpace(text)) < 15 {
		return exitOK
	}

	// Key lookup only after confirming this is a relevant file.
	apiKey := GetKey()
	if apiKey == "" {
		return exitOK
	}

	timeout := time.Duration(*timeoutSec) * time.Second
	if timeout <= 0 || timeout > DefaultTimeout {
		timeout = DefaultTimeout
	}

	client := NewClient(apiKey)
	client.Endpoint = *endpoint
	client.Timeout = timeout

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	var candidates []string
	if isMemory {
		candidates = findDuplicateCandidates(filePath, text)
	}

	state := text
	if isMemory && len(candidates) > 0 {
		state = fmt.Sprintf("Existing record:\n%s\n\nNew record:\n%s", strings.Join(candidates, "\n"), text)
	}
	if len(state) > MaxPayloadBytes {
		state = state[:MaxPayloadBytes]
	}

	questions := make(map[string]Question)
	if isMemory {
		questions[KnowledgeKeepingQuestionID] = KnowledgeKeepingQuestion()
		if len(candidates) > 0 {
			questions[DuplicateFactQuestionID] = DuplicateFactQuestion()
		}
	}
	if isRule {
		questions[RuleEnforcementQuestionID] = RuleEnforcementQuestion()
	}

	resp, err := client.Post(ctx, state, questions)
	if err != nil || resp == nil {
		return exitOK
	}

	var notices []string
	if isMemory {
		if prob, ok := resp.Noul(KnowledgeKeepingQuestionID); ok && prob >= 0.5 {
			notices = append(notices, KnowledgeKeepingNotice)
		}
	}
	if isRule {
		if prob, ok := resp.Noul(RuleEnforcementQuestionID); ok && prob >= 0.5 {
			notices = append(notices, RuleEnforcementNotice)
		}
	}
	if isMemory && len(candidates) > 0 {
		if prob, ok := resp.Noul(DuplicateFactQuestionID); ok && prob >= 0.6 {
			notices = append(notices, DuplicateFactNotice)
		}
	}

	if len(notices) == 0 {
		return exitOK
	}

	notice := strings.Join(notices, "\n\n")

	eventName := payload.HookEventName
	if eventName == "" {
		eventName = "PostToolUse"
	}

	out := HookOutput{
		HookSpecificOutput: HookSpecificOutput{
			HookEventName:     eventName,
			AdditionalContext: notice,
		},
		InjectSteps: []InjectStep{
			{EphemeralMessage: notice},
		},
	}

	enc := json.NewEncoder(stdout)
	_ = enc.Encode(out)

	return exitOK
}

// extractPromptText parses the prompt text from UserPromptSubmit payloads across
// Claude Code, Antigravity, and Codex hook conventions, or falls back to raw content.
func extractPromptText(rawInput []byte) string {
	var rawMap map[string]any
	if err := json.Unmarshal(rawInput, &rawMap); err == nil {
		for _, k := range []string{"prompt", "user_prompt", "text", "input", "query", "content"} {
			if v, ok := rawMap[k]; ok {
				if s, ok := v.(string); ok && strings.TrimSpace(s) != "" {
					return strings.TrimSpace(s)
				}
				if subMap, ok := v.(map[string]any); ok {
					for _, subK := range []string{"prompt", "user_prompt", "text", "query", "content"} {
						if sv, ok := subMap[subK]; ok {
							if s, ok := sv.(string); ok && strings.TrimSpace(s) != "" {
								return strings.TrimSpace(s)
							}
						}
					}
				}
			}
		}
		if ti, ok := rawMap["tool_input"].(map[string]any); ok {
			for _, subK := range []string{"prompt", "user_prompt", "text", "query", "content"} {
				if sv, ok := ti[subK]; ok {
					if s, ok := sv.(string); ok && strings.TrimSpace(s) != "" {
						return strings.TrimSpace(s)
					}
				}
			}
		}
		return ""
	}

	// Roh-Content fallback (not JSON)
	trimmed := strings.TrimSpace(string(rawInput))
	return trimmed
}

// runHookSkillSuggestion executes the hook-skill-suggestion CLI subcommand.
// Suggests relevant imprint skills for user prompts via TypeSafe System One.
// Never blocks or crashes: exits 0 on all paths.
func runHookSkillSuggestion(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("hook-skill-suggestion", flag.ContinueOnError)
	fs.SetOutput(stderr)

	endpoint := fs.String("endpoint", DefaultEndpoint, "TypeSafe API endpoint")
	timeoutSec := fs.Int("timeout", 1, "timeout in seconds (max 2)")

	if err := fs.Parse(args); err != nil {
		return exitOK // fail-open
	}

	// Read hook payload from stdin (up to 2MB).
	rawInput, err := io.ReadAll(io.LimitReader(stdin, 2*1024*1024))
	if err != nil || len(bytes.TrimSpace(rawInput)) == 0 {
		return exitOK
	}

	promptText := extractPromptText(rawInput)
	if len(strings.TrimSpace(promptText)) < 10 {
		return exitOK
	}

	timeout := time.Duration(*timeoutSec) * time.Second
	if timeout <= 0 {
		timeout = 1 * time.Second
	} else if timeout > 2*time.Second {
		timeout = 2 * time.Second
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	apiKey := GetKeyWithContext(ctx)
	if apiKey == "" {
		return exitOK
	}

	client := NewClient(apiKey)
	client.Endpoint = *endpoint
	client.Timeout = timeout

	skill, err := CheckSkillSuggestion(ctx, client, promptText)
	if err != nil || !ImprintSkillsAllowlist[skill] {
		return exitOK
	}

	notice := fmt.Sprintf("imprint core: Für diesen Arbeitsschritt könnte der Skill '%s' relevant sein.\n(imprint core: Skill '%s' might be relevant for this task.)", skill, skill)

	eventName := "UserPromptSubmit"
	var rawMap map[string]any
	if err := json.Unmarshal(rawInput, &rawMap); err == nil {
		if ev, ok := rawMap["hook_event_name"].(string); ok && ev != "" {
			eventName = ev
		}
	}

	out := HookOutput{
		HookSpecificOutput: HookSpecificOutput{
			HookEventName:     eventName,
			AdditionalContext: notice,
		},
		InjectSteps: []InjectStep{
			{EphemeralMessage: notice},
		},
	}

	enc := json.NewEncoder(stdout)
	_ = enc.Encode(out)

	return exitOK
}
