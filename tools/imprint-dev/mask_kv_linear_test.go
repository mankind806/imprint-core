package main

import (
	"math/rand"
	"strings"
	"testing"
	"time"
)

// The key=value step's value scans are shared between candidates since
// 2026-10-03 (secretKWScans, user decision of that day: make Go's key=value
// fast path linear, as typesafe-dev c906a98 did for Python). Before, every
// candidate scanned its value afresh, and in "token=<redacted>" repeated
// without whitespace the value of every keyword runs to the end of the text:
// MaskDetail on 256 KB took 2.8-3.4 s against about 51 ms in the 200 ns/B
// budget. The step must stay exactly what it was. The oracle is the step as
// it stood before (maskSecretKWQuadratic, secretKWWindowQuadratic below, a
// frozen copy of mask_fast.go at mask/stufe1-parity-kv-nfd), as Python kept
// RX_KEY_VAL.subn as the oracle of _mask_key_vals:
//   - TestSecretKWLinearDifferential: both steps on the same texts give the
//     same output and the same matches in the same order; and the window of
//     every position is the same, asked in ascending order (as the step asks)
//     and in a random order (a start before a remembered one scans afresh);
//   - TestSecretKWLinearTime: MaskDetail on the old blow-up shapes, 1 MiB
//     each, within a generous bound.

// kvTokens is the token list of tools/typesafe's TestKeyValLinear (Python),
// written with escapes, plus quoted placeholders and a Unicode space run.
var kvTokens = []string{"tok", "token", "en", "pass", "word", "auth-", "api_key", "PWD", "x", "1", ".", "-", "_",
	"=", ":", `"`, "'", " ", "\t", "\n", "\v", "\x1c", "\u0085", "\u00a0", "\u2028", "<redacted>",
	"<REDACTED>", ",", ";", "\u017f", "\u212a", "\u0130", "\u0131", "\u00e9", "e\u0301", "<",
	`"<redacted>"`, "'<redacted>'", "\u00a0\u00a0\u3000", "token=<redacted>"}

// kvAdversarial: the shapes whose value runs over many later keywords, or
// whose quote has no closing one on its line, each repeated.
func kvAdversarial(n int) []string {
	units := []string{"token=<redacted>", "pwd:<redacted>", "auth=<redacted>.", "secret=<redacted>x",
		`token="<redacted>`, "token='<redacted>", `token="<redacted>"`, "token='<redacted>'",
		"token=\u00a0<redacted>", "token=\u2003\u00a0<redacted>", "token" + "\u00a0=<redacted>",
		`token="<redacted>token='<redacted>`, "tokentoken", "token", "token.", "api_key=<REDACTED>"}
	var out []string
	for _, u := range units {
		out = append(out, strings.Repeat(u, n), strings.Repeat(u, n)+"=x", `"`+strings.Repeat(u, n),
			strings.Repeat(u, n)+"\n"+strings.Repeat(u, n), strings.Repeat(u+" ", n))
	}
	return out
}

// kvCompare runs both steps on text and every window both ways.
func kvCompare(t *testing.T, text string, rng *rand.Rand) {
	t.Helper()
	var gotM, wantM []string
	got := maskSecretKW(text, func(m string) string { gotM = append(gotM, m); return "<R>" })
	want := maskSecretKWQuadratic(text, func(m string) string { wantM = append(wantM, m); return "<R>" })
	if got != want || strings.Join(gotM, "\x00") != strings.Join(wantM, "\x00") {
		t.Fatalf("%+q: step %+q %+q, oracle %+q %+q", text, got, gotM, want, wantM)
	}
	var asc secretKWScans
	for i := 0; i < len(text); i++ {
		if w, o := secretKWWindow(text, i, &asc), secretKWWindowQuadratic(text, i); w != o {
			t.Fatalf("%+q: window at %d (ascending) %d, oracle %d", text, i, w, o)
		}
	}
	var any secretKWScans
	for _, i := range rng.Perm(len(text)) {
		if w, o := secretKWWindow(text, i, &any), secretKWWindowQuadratic(text, i); w != o {
			t.Fatalf("%+q: window at %d (random order) %d, oracle %d", text, i, w, o)
		}
	}
}

func TestSecretKWLinearDifferential(t *testing.T) {
	rng := rand.New(rand.NewSource(11))
	texts := 0
	for _, a := range kvTokens {
		kvCompare(t, a, rng)
		for _, b := range kvTokens {
			kvCompare(t, a+b, rng)
			texts++
		}
		texts++
	}
	for k := 0; k < 20000; k++ {
		var b strings.Builder
		for n := 3 + rng.Intn(14); n > 0; n-- {
			b.WriteString(kvTokens[rng.Intn(len(kvTokens))])
		}
		kvCompare(t, b.String(), rng)
		texts++
	}
	for _, n := range []int{1, 2, 3, 7, 40} {
		for _, text := range kvAdversarial(n) {
			kvCompare(t, text, rng)
			texts++
		}
	}
	t.Logf("%d texts: same output, matches and windows", texts)
}

func TestSecretKWLinearTime(t *testing.T) {
	t.Setenv("TYPESAFE_NAMES_FILE", "/nonexistent/imprint-names.txt")
	const size = 1 << 20
	for _, unit := range []string{"token=<redacted>", "pwd:<redacted>", "secret=<redacted>x", "token=\u00a0<redacted>",
		"tokentoken", `token="<redacted>`, "token=<redacted> "} {
		text := strings.Repeat(unit, size/len(unit))
		start := time.Now()
		got, _ := MaskDetail(text)
		elapsed := time.Since(start)
		// Measured 2026-10-03 (go1.27.1, 20 cores, load around 5): the old
		// step took 2.8-3.4 s for 256 KB of the first shape, so some 45 s
		// for this MiB; the shared scans take milliseconds. The bound is
		// generous for a loaded machine and still far below the old cost.
		if elapsed > 2*time.Second {
			t.Errorf("MaskDetail on %q x %d (%d bytes) took %v: not linear", unit, size/len(unit), len(text), elapsed)
		}
		head := text[:16<<10]
		if got, want := maskSecretKW(head, strings.ToUpper), maskSecretKWQuadratic(head, strings.ToUpper); got != want {
			t.Errorf("%q: the step differs from the oracle on the first 16 KiB", unit)
		}
		t.Logf("%q x %d: %v, %d bytes out", unit, size/len(unit), elapsed, len(got))
	}
}

// --- the step as it stood at mask/stufe1-parity-kv-nfd (frozen) -------------

func maskSecretKWQuadratic(text string, repl func(string) string) string {
	if strings.Contains(text, runeLongS) || strings.Contains(text, runeKelvin) || hasTurkishI(text) {
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
			if e, ok := secretKWAt.matchAt(text, i, secretKWWindowQuadratic(text, i)-i); ok {
				return i, e, true
			}
			i = skipRunes(text, i, isSecretKWRunRune) - 1
		}
		return 0, 0, false
	}, repl)
}

func secretKWWindowQuadratic(text string, i int) int {
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
