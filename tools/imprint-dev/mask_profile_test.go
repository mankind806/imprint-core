package main

import (
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// Masking corpora and an opt-in profile. The corpora are the worst cases the
// mask was measured on (docs/judge.md); the differential test reuses them.
//
//	IMPRINT_MASK_PROFILE=1 [IMPRINT_MASK_PROFILE_SIZES=524288,2097152] [IMPRINT_MASK_PROFILE_REF=1] [IMPRINT_MASK_PROFILE_NEW=0] \
//	  GOMAXPROCS=2 go test -run TestMaskProfile -v -timeout 30m .
//
// prints, per corpus, size and names file, the time of each masking step of
// MaskDetail (and, with IMPRINT_MASK_PROFILE_REF=1, of the old reference).

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
		default:
			panic(kind)
		}
	}
	return sb.String()[:n]
}

var maskCorpusKinds = []string{"prose", "capsdigits", "uuidhash", "pii", "addresses", "keyvalues", "digitrun"}

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
		for _, kind := range maskCorpusKinds {
			text := maskCorpus(rand.New(rand.NewSource(1)), kind, size)
			for _, nf := range []string{"/nonexistent-names", names} {
				t.Setenv("TYPESAFE_NAMES_FILE", nf)
				withNames := nf == names
				label := fmt.Sprintf("%s %dKiB names=%v", kind, size>>10, withNames)
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
