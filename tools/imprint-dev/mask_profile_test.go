package main

import (
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

// Masking corpora and an opt-in profile. The corpora are the worst cases the
// mask was measured on (docs/judge.md); the differential test reuses them.
//
//	IMPRINT_MASK_PROFILE=1 [IMPRINT_MASK_PROFILE_SIZES=524288,2097152] [IMPRINT_MASK_PROFILE_REF=1] [IMPRINT_MASK_PROFILE_NEW=0] \
//	  [IMPRINT_MASK_PROFILE_KINDS=addresses,pii] [IMPRINT_MASK_PROFILE_PREFIX='ſ '] \
//	  GOMAXPROCS=2 go test -run TestMaskProfile -v -timeout 30m .
//
// prints, per corpus, size and names file, the time of each masking step of
// MaskDetail (and, with IMPRINT_MASK_PROFILE_REF=1, of the old reference).
// IMPRINT_MASK_PROFILE_KINDS picks corpora; IMPRINT_MASK_PROFILE_PREFIX is put
// before each text (a rune that sends steps to their fallback, say).

// maskCorpus returns about n bytes of one kind of text, made from rng.
func maskCorpus(rng *rand.Rand, kind string, n int) string {
	var sb strings.Builder
	pick := func(xs ...string) string { return xs[rng.Intn(len(xs))] }
	digits := func(k int) string { return randFrom(rng, "0123456789", k) }
	for sb.Len() < n {
		switch kind {
		case "prose":
			sb.WriteString(pick("Lorem ipsum dolor sit amet, consectetur adipiscing elit. ",
				"Die Besprechung ist am Montag; bitte das Protokoll lesen und Fragen sammeln. ",
				"The build passed after the change, and the review is pending. ",
				"Auf der Liste stehen drei Punkte: Planung, Umsetzung und Abnahme. ",
				"Passwort-Regeln und Token-Laufzeiten stehen im Handbuch, Kapitel 4. "))
		case "capsdigits":
			sb.WriteString(randFrom(rng, "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789 ", 64))
		case "uuidhash":
			switch rng.Intn(4) {
			case 0:
				h := randFrom(rng, "0123456789abcdef", 32)
				sb.WriteString(h[:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:] + "\n")
			case 1:
				sb.WriteString(`    "integrity": "sha512-` + randFrom(rng, b64Alphabet, 86) + `==",` + "\n")
			case 2:
				sb.WriteString(randFrom(rng, "0123456789abcdef", 64) + "  ./file" + digits(3) + "\n")
			default:
				sb.WriteString(randFrom(rng, b64Alphabet, 76) + "\n")
			}
		case "pii":
			sb.WriteString(pick(
				"Mail an "+pick("max.mustermann", "erika", "j.doe")+"@"+pick("example.test", "beispiel.example")+". ",
				"Tel. 0"+digits(3)+" "+digits(7)+", mobil +49 1"+digits(2)+" "+digits(8)+". ",
				"IBAN DE"+digits(2)+" "+digits(4)+" "+digits(4)+" "+digits(4)+" "+digits(4)+" "+digits(2)+" bitte. ",
				pick("Musterstraße", "Hauptstr.", "Lindenweg", "Am Markt", "Auf dem Berg", "Gartenallee")+" "+digits(2)+pick("", "a", " - 4")+", ",
				digits(5)+" "+pick("Berlin", "München", "Musterstadt am Fluss", "Freiburg im Breisgau")+". ",
				pick("Max Mustermann", "Erika Musterfrau", "max mustermann")+" schreibt: ",
				"api_key="+randFrom(rng, b64Alphabet[:62], 32)+" ",
				"Authorization: Bearer "+randFrom(rng, b64Alphabet[:62], 40)+" ",
				`password="`+pick("correct horse battery staple", "hunter2")+`" `,
				"ghp_"+randFrom(rng, b64Alphabet[:62], 36)+" ",
				"AKIA"+randFrom(rng, "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789", 16)+" ",
			))
		case "addresses":
			// dense street addresses: the street pattern runs on nearly every byte
			sb.WriteString(pick("Musterstraße", "Hauptstr.", "Lindenweg", "Am Alten Markt", "An der Linde", "Gartenallee",
				"Schloss-Allee", "Bahnhofsplatz") + " " + digits(1+rng.Intn(3)) + pick("", "a", " - 4", "/2") + pick(", ", "\n", " "))
		case "keyvalues":
			// dense key=value secrets
			sb.WriteString(pick("token", "api_key", "password", "secret", "auth", "access-key") + pick("=", ": ", "=\"") +
				randFrom(rng, b64Alphabet[:62], 8+rng.Intn(24)) + pick(" ", "\" ", ", ", "\n"))
		case "digitrun":
			// a letter, then a long run of digits: the old phone step retried
			// every position and rescanned the run each time.
			sb.WriteString("a" + strings.Repeat("0", 4096) + " ")
		case "streetchain":
			// since 2026-10-02 (address union): chains of capitalised words
			// that the street automaton scans back over, ending in a house
			// number that fails or matches
			word := pick("Aaa", "Bbb-Ccc", "\u00c41", "Xy-Am", "Am", "\u00dcber")
			n := []int{5, 16, 40, 100, 300}[rng.Intn(5)]
			sb.WriteString(strings.Repeat(word+" ", n) + pick("Weg 1x2", "Weg 12 - 14b", "Hof 8", "3", "Stra\u00dfe 5x") + pick(" ", ", ", "\n"))
		case "anchors":
			// prose with a number every few bytes: every space before a digit
			// is an anchor of the street step; the preposition branch masks
			// some of them (amendment A5)
			sb.WriteString(pick("Im Jahr ", "Am Montag ", "Seite ", "x ", "Zum Teil ", "In der Woche ", "um ", "Im PR ") +
				digits(1+rng.Intn(4)) + pick(" ", ", ", ". ", "a "))
		case "lowerrun":
			// long lowercase runs before a suffix and a number: Python's
			// lowercase branch scans back over the whole run
			letters := []rune("abcdefghijklmnopqrstuvwxyz\u00e4\u00f6\u00fc\u00df")
			for k := 20 + rng.Intn(400); k > 0; k-- {
				sb.WriteRune(letters[rng.Intn(len(letters))])
			}
			sb.WriteString(pick("weg", "stra\u00dfe", "ring", "x", "markt") + " " + digits(1+rng.Intn(3)) + pick(" ", "\n", "x "))
		case "unicodeaddr":
			// streets and postcodes with non-ASCII spaces and digits (S, D)
			sb.WriteString(pick("Musterstra\u00dfe", "Am Alten Markt", "Hauptweg") + pick("\u00a0", "\u3000", "\u2009", " ") +
				pick("\u0661\u0662", "\u0967\u0968", "12") + ", " + pick("\u0661\u0662\u0663\u0664\u0665", "\u0967\u0968\u0969\u096a\u096b") +
				pick("\u00a0", " ") + pick("Berlin", "M\u00fcnchen", "Bad Homburg") + pick(", ", "\n"))
		case "nfdgerman":
			// since 2026-10-02 (names on an NFC copy): German prose and the
			// profile's names with decomposed umlauts and accents (NFD), so
			// nearly every word is a segment NFC changes
			sb.WriteString(pick("Mu\u0308ller", "Gru\u0308\u00dfe", "scho\u0308n", "U\u0308bung", "Ba\u0308cker", "Jo\u0301se\u0301",
				"Max Mustermann", "Erika Musterfrau", "Ma\u0301x Mu\u0308stermann", "E\u0301rika", "und", "die", "Stra\u00dfe") +
				pick(" ", ", ", ". ", "\n"))
		case "jamo":
			// Hangul as conjoining jamo: every syllable composes, L V or L V T
			for k := 1 + rng.Intn(4); k > 0; k-- {
				sb.WriteRune(rune(0x1100 + rng.Intn(19)))
				sb.WriteRune(rune(0x1161 + rng.Intn(21)))
				if rng.Intn(2) == 0 {
					sb.WriteRune(rune(0x11a8 + rng.Intn(27)))
				}
			}
			sb.WriteString(pick(" ", " Max Mustermann ", ", ", "\n"))
		case "markrun":
			// a letter and a long run of combining marks of several classes
			// out of canonical order: one segment, reordered
			sb.WriteString(pick("a", "Max", "e", "Mustermann"))
			marks := []rune{0x301, 0x323, 0x334, 0x5b0, 0x308, 0x328}
			for k := 1 + rng.Intn(300); k > 0; k-- {
				sb.WriteRune(marks[rng.Intn(len(marks))])
			}
			sb.WriteString(pick(" ", " Erika ", "\n"))
		case "tibetan":
			// "a" + U+0F73 x N: U+0F73 decomposes to two marks of classes
			// 129 and 130 and is no boundary, so each run is one segment
			// of 2N marks to reorder (amendment A1 measured the old rule
			// quadratic on it)
			sb.WriteString("a" + strings.Repeat("\u0f73", 1+rng.Intn(4000)) + pick(" ", " Max Mustermann ", "\n"))
		case "f73one":
			// the whole text one segment: "a" + U+0F73 x (n/3)
			sb.WriteString("a" + strings.Repeat("\u0f73", n/3+1))
		default:
			panic(kind)
		}
	}
	s := sb.String()[:n]
	if slices.Contains(maskAddressKinds, kind) || slices.Contains(maskNFCKinds, kind) {
		// end on a whole rune: invalid UTF-8 would send other steps to
		// their fallback
		for r, w := utf8.DecodeLastRuneInString(s); r == utf8.RuneError && w == 1; r, w = utf8.DecodeLastRuneInString(s) {
			s = s[:len(s)-1]
		}
	}
	return s
}

var maskCorpusKinds = []string{"prose", "capsdigits", "uuidhash", "pii", "addresses", "keyvalues", "digitrun"}

// maskAddressKinds are the corpora added with the address union (2026-10-02):
// the profile runs them after maskCorpusKinds, the differential test at
// 16 KiB (the reference scans long chains once per word).
var maskAddressKinds = []string{"streetchain", "anchors", "lowerrun", "unicodeaddr"}

// maskNFCKinds are the corpora added with names on an NFC copy (2026-10-02):
// text that is not NFC, so the names step builds the copy and maps back.
// The profile runs them after maskAddressKinds, the differential test at
// 16 KiB.
var maskNFCKinds = []string{"nfdgerman", "jamo", "markrun", "tibetan", "f73one"}

func TestMaskProfile(t *testing.T) {
	if os.Getenv("IMPRINT_MASK_PROFILE") != "1" {
		t.Skip("set IMPRINT_MASK_PROFILE=1 to profile")
	}
	sizes := []int{512 << 10, 2 << 20, 16 << 20}
	if v := os.Getenv("IMPRINT_MASK_PROFILE_SIZES"); v != "" {
		sizes = nil
		for _, f := range strings.Split(v, ",") {
			n, err := strconv.Atoi(f)
			if err != nil {
				t.Fatal(err)
			}
			sizes = append(sizes, n)
		}
	}
	kinds := append(append(append([]string(nil), maskCorpusKinds...), maskAddressKinds...), maskNFCKinds...)
	if v := os.Getenv("IMPRINT_MASK_PROFILE_KINDS"); v != "" {
		kinds = strings.Split(v, ",")
	}
	prefix := os.Getenv("IMPRINT_MASK_PROFILE_PREFIX")
	withRef := os.Getenv("IMPRINT_MASK_PROFILE_REF") == "1"
	withNew := os.Getenv("IMPRINT_MASK_PROFILE_NEW") != "0"
	names := filepath.Join(t.TempDir(), "names.txt")
	if err := os.WriteFile(names, []byte("Max Mustermann\nErika Musterfrau\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TYPESAFE_API_KEY", "")
	run := func(label string, f func(string) (string, MaskCounts), set func(func(string)), text string) {
		laps := map[string]time.Duration{}
		var order []string
		last := time.Now()
		set(func(step string) {
			now := time.Now()
			if _, ok := laps[step]; !ok {
				order = append(order, step)
			}
			laps[step] += now.Sub(last)
			last = now
		})
		start := time.Now()
		f(text)
		total := time.Since(start)
		set(nil)
		var parts []string
		for _, s := range order {
			parts = append(parts, fmt.Sprintf("%s=%d", s, laps[s].Milliseconds()))
		}
		t.Logf("%-46s total=%6dms  %s", label, total.Milliseconds(), strings.Join(parts, " "))
	}
	for _, size := range sizes {
		for _, kind := range kinds {
			text := prefix + maskCorpus(rand.New(rand.NewSource(1)), kind, size)
			for _, nf := range []string{"/nonexistent-names", names} {
				t.Setenv("TYPESAFE_NAMES_FILE", nf)
				withNames := nf == names
				label := fmt.Sprintf("%s%s %dKiB names=%v", map[bool]string{true: "", false: "prefixed "}[prefix == ""], kind, size>>10, withNames)
				if withNew {
					run("new "+label, MaskDetail, func(f func(string)) { maskLapFn = f }, text)
				}
				if withRef && (kind != "digitrun" || size <= 64<<10) {
					run("old "+label, maskDetailReference, func(f func(string)) { refLapFn = f }, text)
				}
			}
		}
	}
}
