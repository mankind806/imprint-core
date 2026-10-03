package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"regexp/syntax"
	"strings"
	"testing"
)

// pyToGo rewrites a regular expression from ts_common.py into the spelling of
// the constants in typesafe.go, by the mapping documented there (mask parity
// spec sections 0 and 4, 2026-10-02): \s is S (pySpaceClass), \d is D
// (\p{Nd}), \w is W ([\p{L}\p{N}_]), and every I, i, U+0130 and U+0131 of a
// case-insensitive part ((?i) at the start, (?i:...), or fold for the whole
// pattern) is turkishI. What the mapping does not define is an error: an
// escaped letter other than s, d and w (\b, \S ...), a group (?... other than
// (?:, (?i:, (?-i: and (?P<name>, and a class in a case-insensitive part that
// holds one of the four I runes, alone or in a range.
func pyToGo(src string, fold bool) (string, error) {
	spaceInner := pySpaceClass[1 : len(pySpaceClass)-1]
	isI := func(r rune) bool { return r == 'I' || r == 'i' || r == '\u0130' || r == '\u0131' }
	var b strings.Builder
	cur := fold
	if strings.HasPrefix(src, "(?i)") {
		b.WriteString("(?i)")
		src = src[len("(?i)"):]
		cur = true
	}
	rs := []rune(src)
	var stack []bool
	for k := 0; k < len(rs); k++ {
		switch r := rs[k]; {
		case r == '\\':
			if k++; k >= len(rs) {
				return "", errors.New("a backslash at the end")
			}
			switch e := rs[k]; {
			case e == 's':
				b.WriteString(pySpaceClass)
			case e == 'd':
				b.WriteString(`\p{Nd}`)
			case e == 'w':
				b.WriteString(`[\p{L}\p{N}_]`)
			case strings.ContainsRune("tnfrv", e): // the same control escapes in both
				b.WriteRune('\\')
				b.WriteRune(e)
			case e < 0x80 && (e >= 'a' && e <= 'z' || e >= 'A' && e <= 'Z' || e >= '0' && e <= '9'):
				return "", fmt.Errorf(`\%c has no mapping`, e)
			default:
				b.WriteRune('\\')
				b.WriteRune(e)
			}
		case r == '[':
			b.WriteRune('[')
			j := k + 1
			if j < len(rs) && rs[j] == '^' {
				b.WriteRune('^')
				j++
			}
			var lits []rune // the class's runes, escapes resolved, for the I check
			for first := true; j < len(rs) && (first || rs[j] != ']'); j, first = j+1, false {
				c := rs[j]
				if c != '\\' {
					b.WriteRune(c)
					lits = append(lits, c)
					continue
				}
				if j++; j >= len(rs) {
					return "", errors.New("a backslash at the end")
				}
				switch e := rs[j]; {
				case e == 's':
					b.WriteString(spaceInner)
				case e == 'd':
					b.WriteString(`\p{Nd}`)
				case e == 'w':
					b.WriteString(`\p{L}\p{N}_`)
				case strings.ContainsRune("tnfrv", e):
					b.WriteRune('\\')
					b.WriteRune(e)
					lits = append(lits, 0)
				case e < 0x80 && (e >= 'a' && e <= 'z' || e >= 'A' && e <= 'Z' || e >= '0' && e <= '9'):
					return "", fmt.Errorf(`\%c in a class has no mapping`, e)
				default:
					b.WriteRune('\\')
					b.WriteRune(e)
					lits = append(lits, 0) // never part of a range check
				}
			}
			if j >= len(rs) {
				return "", errors.New("a class without its ]")
			}
			b.WriteRune(']')
			k = j
			if cur {
				for n, c := range lits {
					inRange := c == '-' && n > 0 && n+1 < len(lits)
					for _, x := range []rune{'I', 'i', '\u0130', '\u0131'} {
						if isI(c) || inRange && lits[n-1] <= x && x <= lits[n+1] {
							return "", fmt.Errorf("a class in a case-insensitive part holds %q: no mapping", x)
						}
					}
				}
			}
		case r == '(':
			rest := string(rs[k:])
			stack = append(stack, cur)
			var open string
			switch {
			case strings.HasPrefix(rest, "(?i:"):
				open, cur = "(?i:", true
			case strings.HasPrefix(rest, "(?-i:"):
				open, cur = "(?-i:", false
			case strings.HasPrefix(rest, "(?:"):
				open = "(?:"
			case strings.HasPrefix(rest, "(?P<"):
				open = rest[:strings.IndexByte(rest, '>')+1]
			case strings.HasPrefix(rest, "(?"):
				return "", fmt.Errorf("group %.4q has no mapping", rest)
			default:
				open = "("
			}
			b.WriteString(open)
			k += len([]rune(open)) - 1
		case r == ')':
			if len(stack) == 0 {
				return "", errors.New("a ) without its (")
			}
			cur, stack = stack[len(stack)-1], stack[:len(stack)-1]
			b.WriteRune(')')
		case cur && isI(r):
			b.WriteString(turkishI)
		default:
			b.WriteRune(r)
		}
	}
	if len(stack) != 0 {
		return "", errors.New("a ( without its )")
	}
	return b.String(), nil
}

// TestPyToGo pins the mapping on small cases, the errors included.
func TestPyToGo(t *testing.T) {
	for _, c := range []struct {
		src  string
		fold bool
		want string
	}{
		{`\d+\s*[a-zA-Z]`, false, `\p{Nd}+` + pySpaceClass + `*[a-zA-Z]`},
		{`(?i:ring|zeile)|Im`, false, `(?i:r` + turkishI + `ng|ze` + turkishI + `le)|Im`},
		{`api[_-]?key`, true, `ap` + turkishI + `[_-]?key`},
		{`[\w.-]*[\"']?\s*`, false, `[\p{L}\p{N}_.-]*[\"']?` + pySpaceClass + `*`},
		{`(?i)(?-i:Im)i`, false, `(?i)(?-i:Im)` + turkishI},
		{`[\s,]`, false, `[` + pySpaceClass[1:len(pySpaceClass)-1] + `,]`},
	} {
		got, err := pyToGo(c.src, c.fold)
		if err != nil || got != c.want {
			t.Errorf("pyToGo(%q, %v) = %q, %v; want %q", c.src, c.fold, got, err, c.want)
		}
	}
	for _, src := range []string{`\bweg`, `(?=x)`, `(?i:[a-z])`, `(?i:[ij])`, `(?i:[\S])`, `(a`, `a)`, `[ab`} {
		if got, err := pyToGo(src, false); err == nil {
			t.Errorf("pyToGo(%q) = %q, want an error", src, got)
		}
	}
}

// TestGrammarSyncWithTsCommon (packet G5 of mask parity, 2026-10-02): the
// address grammars (PyStreet, GoStreet, PyPlz, GoPlz as prefix and number,
// the suffix lists), the keyword list and key=value lead, and the Bearer/Basic
// pattern in typesafe.go are ts_common.py's, read from the vendored copy in
// tools/typesafe by python3 and rewritten by pyToGo; both sides must parse
// (regexp/syntax, as regexp.Compile) to the same expression. So the two
// cannot drift apart silently; TestMaskGolden checks the behaviour. Without
// python3 the test skips and says so.
func TestGrammarSyncWithTsCommon(t *testing.T) {
	py, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 not found: the grammars and keyword lists of typesafe.go are not compared with tools/typesafe/ts_common.py")
	}
	setupJudge(t, "")
	tsDir, err := filepath.Abs(filepath.Join("..", "typesafe"))
	if err != nil {
		t.Fatal(err)
	}
	script := `import json, sys
sys.path.insert(0, sys.argv[1])
import ts_common as tc
out = {n: getattr(tc, n) for n in ["_SUF_PY", "_SUF_GO", "_STREET_PREFIX_PY", "_STREET_NUMBER_PY",
       "_STREET_PREFIX_GO", "_STREET_NUMBER_GO", "_PLZ_PY", "_PLZ_GO", "_MASK_KW"]}
out["RX_BEARER"] = tc.RX_BEARER.pattern
out["RX_KEY_VAL"] = tc.RX_KEY_VAL.pattern
json.dump(out, sys.stdout)
`
	cmd := exec.Command(py, "-B", "-c", script, tsDir)
	cmd.Env = append(os.Environ(), "PYTHONDONTWRITEBYTECODE=1")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	raw, err := cmd.Output()
	if err != nil {
		t.Fatalf("python3: %v\n%s", err, stderr.String())
	}
	var src map[string]string
	if err := json.Unmarshal(raw, &src); err != nil {
		t.Fatalf("python3 output: %v %s", err, raw)
	}

	// RX_BEARER: the ASCII lookbehind before the trigger is Go's ASCII \b
	// there (a trigger starts with a letter); group 1 holds trigger and
	// separator, the value follows (Go: group 2).
	bearerRE := regexp.MustCompile(`^\(\?i\)\(\(\?-i:\(\?<!\[A-Za-z0-9_\]\)\)(.*)\)([^()]*)$`)
	bm := bearerRE.FindStringSubmatch(src["RX_BEARER"])
	// RX_KEY_VAL: group 1 is the lead (keyword, tail, quote, S, = or :, S),
	// up to where the value alternatives begin.
	kvLead, kvOK := strings.CutPrefix(src["RX_KEY_VAL"], "(?i)(")
	if kvOK {
		kvLead, _, kvOK = strings.Cut(kvLead, ")(?:(?P<dq>")
	}
	if bm == nil || !kvOK {
		t.Fatalf("RX_BEARER or RX_KEY_VAL in ts_common.py has another shape now; update this test and the patterns in typesafe.go together:\n%q\n%q",
			src["RX_BEARER"], src["RX_KEY_VAL"])
	}

	pairs := []struct {
		name, goSrc, pySrc string
		fold               bool // the part is used under (?i)
	}{
		{"streetSuffixPy = _SUF_PY", streetSuffixPy, src["_SUF_PY"], true},
		{"streetSuffixGo = _SUF_GO", streetSuffixGo, src["_SUF_GO"], true},
		{"streetPrefixPy = _STREET_PREFIX_PY", streetPrefixPy, src["_STREET_PREFIX_PY"], false},
		{"streetNumberPy = _STREET_NUMBER_PY", streetNumberPy, src["_STREET_NUMBER_PY"], false},
		{"streetPrefixGo = _STREET_PREFIX_GO", streetPrefixGo, src["_STREET_PREFIX_GO"], false},
		{"streetNumberGo = _STREET_NUMBER_GO", streetNumberGo, src["_STREET_NUMBER_GO"], false},
		{"plzPy = _PLZ_PY", plzPy, src["_PLZ_PY"], false},
		{"plzGo = _PLZ_GO", plzGo, src["_PLZ_GO"], false},
		{"secretKWKeywords = _MASK_KW", secretKWKeywords, src["_MASK_KW"], true},
		{"secretKWLead = RX_KEY_VAL group 1", secretKWLead, kvLead, true},
		{"bearerBasicPattern = RX_BEARER", bearerBasicPattern, `(?i)(\b` + bm[1] + `)(` + bm[2] + `)`, false},
	}
	parse := func(p string, fold bool) (string, error) {
		if fold {
			p = `(?i:` + p + `)`
		}
		re, err := syntax.Parse(p, syntax.Perl)
		if err != nil {
			return "", err
		}
		return re.String(), nil
	}
	identical := 0
	for _, p := range pairs {
		pySrc := p.pySrc
		var mapped string
		if strings.HasPrefix(p.name, "bearerBasicPattern") {
			// \b was put in on the Go side of the mapping; map the rest.
			trig, err1 := pyToGo(bm[1], true)
			val, err2 := pyToGo(bm[2], true)
			if err := errors.Join(err1, err2); err != nil {
				t.Errorf("%s: %v", p.name, err)
				continue
			}
			mapped = `(?i)(\b` + trig + `)(` + val + `)`
		} else {
			m, err := pyToGo(pySrc, p.fold)
			if err != nil {
				t.Errorf("%s: %v: %q", p.name, err, pySrc)
				continue
			}
			mapped = m
		}
		a, errA := parse(p.goSrc, p.fold)
		b, errB := parse(mapped, p.fold)
		if err := errors.Join(errA, errB); err != nil {
			t.Errorf("%s: %v", p.name, err)
			continue
		}
		if a != b {
			t.Errorf("%s: typesafe.go and ts_common.py differ after the mapping\n   Go: %s\n   Py: %s\n(Python's source: %q)", p.name, p.goSrc, mapped, pySrc)
			continue
		}
		if mapped == p.goSrc {
			identical++
		}
	}
	t.Logf("%d pairs, each the same expression; %d also the same text after the mapping", len(pairs), identical)
}
