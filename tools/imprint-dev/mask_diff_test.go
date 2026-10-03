package main

import (
	"encoding/json"
	"math/rand"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"unicode"
)

// TestMaskDifferential runs MaskDetail and maskDetailReference (the old
// implementation, kept test-only) on the same texts: output and counts must be
// identical, byte for byte. The texts: the profile corpora (prose, capitals
// and digits, UUIDs and hashes, dense personal data, digit runs), the Python
// parity cases, and a soup of fragments aimed at every fast path's edge (word
// boundaries, case folds incl. the Kelvin sign, long s and capital sharp s,
// the Turkish dotted and dotless i, umlauts, every \s and a few non-\s spaces
// (Go's and Python's), invalid UTF-8, keyword runs, rejected lookbehinds,
// placeholders, digits and numbers of other scripts; since 2026-10-02 text
// that is not NFC), each under six names files (five until 2026-10-02).
// IMPRINT_MASK_DIFF_SEEDS scales the soup (default 25 seeds; docs/judge.md
// records a run with 400).

// at is put between the parts of each address, so no tracked file carries
// one (.githooks/pre-push); the same for IBANs, phone numbers and postcodes.
const at = "@"

var diffFragments = []string{
	// secrets and keywords
	"Bearer ", "bearer\t", "BASIC ", "Basic", "xBearer ", "_bearer ", "token", "TOKEN", "tokKen", "api_key", "api-key", "apikey",
	"Passwort", "passphrase", "passſword", "pwd", "credential", "private_key", "access-key", "Authorization", "auth",
	"=", ": ", ":", " = ", `"`, `'`, `"quoted value"`, `'single'`, "<redacted>", `"<redacted>"`, "token=<redacted>",
	"secret.key.v2", "my_token_x", "tokentoken", "token token=", "apixtoken=1", "pass=", "pass=\n",
	// email
	"@", "max.mustermann" + at + "example.test", "jürgen" + at + "beiſpiel.example", "a" + at + "b.c", "x@y", at + "x.y", "user+tag" + at + "sub.example.test",
	"ä" + at + "ö.ü", "a..b" + at + "c..d", "a@b" + at + "c.d", "١٢٣" + at + "example.test",
	// IBAN, phone, digits
	"DE" + "89" + " 3704" + " 0044" + " 0532" + " 0130" + " 00", "DE" + "89" + "3704004405" + "32013000", "XDE" + "89" + " 3704" + " 0044" + " 0532" + " 0130" + " 00", "DE" + "89" + " 3704" + " 0044" + " 0532" + " 0130" + " 00Bank",
	"GB" + "29NWBK" + "6016" + "1331926819", "+49 3" + "0 1234" + "5678", "+4930123456789", "0" + "30 " + "12345678", "0301234567", "(030) 123-456-78",
	"a0301234567", "x+49 1234 567890", ".0301234567", "0 1 2 3 4 5 6 7 8", "0" + "30 " + "12345678٣", "0000000000",
	strings.Repeat("0", 40), "1" + strings.Repeat("0", 30), "AB12", "AB12CD", "DE" + "00 0" + "000 0" + "000",
	// streets and postcodes
	"Musterstraße 12", "Musterstrasse 12a", "Hauptstr. 4b", "Hauptstr 4", "Lindenweg 3 - 5", "Gartenallee 7/9",
	"Am Markt 1", "An der Linde 2", "Auf dem Berg 3", "In der Gasse 4", "Hinter dem Hof 5", "Zum See 6", "Zur Post 7",
	"Ringstraße\n12", "Ufer 1", "RING 5", "WEG 1", "STRASSE 9", "Straẞe 3", "Gäßchen 4", "Gaesschen 4", "Schloss-Allee 12",
	"xStraße 5", "äStraße 5", ".Am Markt 1", "-Am Markt 1", "Am  Markt\t1", "Am 5", "Musterstraße 12345", "Weg 1234 - 12345",
	"1" + "0115 Berlin", "8" + "0331 München", "1" + "2345 Musterstadt am Fluss", "7" + "9098 Freiburg im Breisgau", "9" + "1541 Rothenburg ob der Tauber",
	"1" + "0115 Berlin im Brief", "1" + "2345" + "6 Berlin", "x1" + "0115 Berlin", "1" + "0115\nBerlin", "1" + "0115 berlin",
	// tokens
	"ghp_" + strings.Repeat("a1", 20), "sk-" + strings.Repeat("Z9", 10), "sk-short", "xoxb-" + strings.Repeat("1a", 8),
	"AKIA" + strings.Repeat("A1", 8), "ASIAABCDEFGHIJKLMNOP", "xAKIAABCDEFGHIJKLMNOP", "AIza" + strings.Repeat("x", 30),
	"eyJ" + strings.Repeat("hb", 20) + "." + strings.Repeat("Qx", 10), "1//" + strings.Repeat("0a", 6), "glpat-" + strings.Repeat("b", 20),
	"npm_" + strings.Repeat("c3", 10), "ya29." + strings.Repeat("d4", 10), "GOCSPX-" + strings.Repeat("e", 12),
	strings.Repeat("A1b2", 6), strings.Repeat("abcd", 7), strings.Repeat("1234", 7), strings.Repeat("Ab/+", 7) + "==", "===",
	// names (see the names files below)
	"Max Mustermann", "max mustermann", "MAX", "Mustermann", "Erika", "Jürgen Müller", "JÜRGEN", "Özil", "xÖzil", "Özilx",
	"Kai", "Kai", "kai_", "Max_Mustermann", "Max-Mustermann", "Hans Max Mustermann", "HANS MAX", "Karl Schmidt", "karl schmidt",
	"Anna Becker", "ANNA", "Ann", "name", "<name>", "Redacted", "\u00d6\u00dfa", "\u00d6SSa", "Schr\u00f6der", "SCHMIDT",
	// other scripts and fold classes: Greek with final sigma, Cyrillic, the
	// Ohm and Angstrom signs (their fold has another length), micro sign
	"\u0391\u039b\u0388\u039e\u0391\u039d\u0394\u03a1\u039f\u03a3", "\u03b1\u03bb\u03ad\u03be\u03b1\u03bd\u03b4\u03c1\u03bf\u03c2",
	"\u03a3\u039f\u03a6\u0399\u0391", "\u03c3\u03bf\u03c6\u03b9\u03b1", "\u0418\u0412\u0410\u041d \u041f\u0415\u0422\u0420\u041e\u0412",
	"\u0438\u0432\u0430\u043d", "\u2126", "\u212b", "\u212bngstr\u00f6m", "\u00c5NGSTR\u00d6M", "\u00b5", "\u03bc", "\u03d0", "x\u03a3\u03bf\u03c6\u03af\u03b1",
	// mask parity spec (2026-10-02), sections 0, 1a, 1b, 3: Bearer/Basic up to
	// the next ASCII space, after any S; key=value with Unicode \w and S and
	// Python's (?!<redacted>) lookaheads; Turkish i in every (?i) part; phone
	// digits of any script, a number of any script blocking before them
	`"Bearer abc"`, "Bearer abc,def", "authorization: Bearer abc", `"Authorization": "Bearer abc"`, "Bearer <redacted>",
	"Bearer\u00a0\u00a0 x", "Bearer\u00a0\u00a0", "Bearer \u00a0", "Bearer\u000b", "Bearer\u001c", "Bearer\u3000", "Bearer\u2009x;y",
	"bas\u0131c ", "BAS\u0130C ", "Bas\u0130c\u00a0", "\u017fBearer ", "\u00e4Bearer ", "\u216bBearer ", "\u0661Bearer ", "1Bearer ",
	"token\u00e4=abc", "credent\u0130al=", "ap\u0131_key=", "pr\u0130vate-key: ", "AUTHOR\u0130ZAT\u0131ON=", "auth\u212a=", "Pa\u017f\u017f=",
	"token\u00a0=\u00a0", "token\u2009:\u2009", "token\u3000=", "token\u000b=\u000b", "token\u001c=\u001f", "token\u0085=\u2028",
	"token=\u00a0\"<redacted>\"", "token= \u00a0<redacted>", "token=<redacted>password=x", "token=<REDACTED>", `token="<REDACTED>"`,
	`token="<redacted`, "token=<redacted", "token=<redac", `secret='<redacted>'`, `secret="<redacted>x"`, "token=\u00a0", "token= \u00a0",
	// amendment A3: the skip is case-sensitive
	`"<Redacted>"`, "<rEdacted>", `'<REDACTED>'`, "<REDACTED>,", "<redacteD>", "<Redacted",
	"api_key=\u00a0abc", `token="abc`, "pass=,", "token\u216b=1", "token\u0661=1", "token.\u00e4-\u00df_x=1", "<redacted>password=",
	"\u00a0", "\u2009", "\u3000", "\u001c", "\u001f", "\u0085", "\u2028", "\u200b", "\u180e", "\u0130", "\u0131",
	"0" + "30 " + "\u0661\u0662\u0663\u0664\u0665\u0666\u0667\u0668", "0" + "30 " + "\u0967\u0968\u0969\u096a\u096b\u096c\u096d\u096e",
	"\u00b2" + "0" + "30 " + "12345678", "\u216b" + "0" + "30 " + "12345678", "\u0663" + "0" + "301234567", "\u00bd" + "0" + "301234567",
	"+49 " + "\u0663\u0660" + " 1234" + "5678", "0" + "30 " + "1234" + "5678\u0967",
	"Gartenr\u0130ng 5", "Alte Gartenr\u0131ng 5", "Hauptze\u0131le 3", "Bergste\u0130g 2", "GARTENR\u0130NG 5", "Im Ring 4",
	"Y\u0131ld\u0131z", "YILDIZ", "Yildiz", "y\u0131ld\u0131z", "\u0130lker", "ilker", "ILKER", "\u0131lker", "I\u015f\u0131k", "I\u015eIK",
	"\u0131\u015f\u0131k", "I\u015f\u0131kx", "\u0130LKER Y\u0130LD\u0130Z",
	// mask parity spec section 4 (2026-10-02): the union of both address
	// grammars, Python's classes, leftmost-longest; its examples, Go's
	// preposition branch on prose (amendment A5), the suffixes only one
	// grammar has (with fold-only runes), ends the grammars disagree on,
	// Python's lowercase branch, and S and D of other scripts
	"Ku'damm 3", "1" + "2345 BERLIN", "1" + "2345 Bad Homburg", "1" + "2345 Halle/Saale", "8" + "0331 M\u00fcnchen1",
	"1" + "0115 Berlin\u00e9", "Musterstra\u00dfe 12\u00e4", "\u00e4Musterstra\u00dfe 12", "1" + "2345 Berlin.\u00e9",
	"Im Jahr 2024 wurde", "Am Montag 12 Uhr", "Im PR 41 gefixt", "Am Am Am Am 1", "Aaa Aaa Aaa 1",
	"Kirchst\u0131eg 3", "Bergst\u0130eg 2", "Alter Mar\u212at 5", "haupt\u017ftra\u00dfe 4", "STRA\u1e9eE 7", "Hauptstra\u1e9ee 7",
	"G\u00e4sschen 4", "Marktgasse 1", "markt 2", "Hauptstra\u00dfe 12 b", "Weg 3 / 4 c", "Weg 3 /4c", "Str 5 - 6 x",
	"string 3", "during 2", "Spring 2026", "Mu\u0308hlenweg 3", "8" + "0331 Mu\u0308nchen", "M\u00dcHLENWEG 3",
	"Musterstra\u00dfe\u00a012", "Musterstra\u00dfe\u2009" + "12", "Musterstra\u00dfe\u300012", "Hauptweg \u0661\u0662",
	"Weg\u001c1", "Weg\u0085" + "1", "\u0661\u0662\u0663\u0664\u0665 Berlin", "1234\u0665 Berlin", "1" + "2345\u00a0Berlin",
	"1" + "2345 Bad\u3000Homburg", "1" + "2345 Lindau an\u00a0 der Saale", "1" + "2345 Lindau im Tal", "1" + "2345 Musterstadt am",
	// mask parity spec section 5 with amendments A1, A2, A8 (2026-10-02):
	// names on an NFC copy of text that is not NFC; decomposed Latin, starters
	// with quick check Maybe (Oriya, Hangul vowels and trailing consonants),
	// conjoining jamo, singletons (Angstrom, Ohm, Kelvin signs, which pull a
	// space before them into their segment), I + combining dot (U+0130 in
	// the copy), marks out of canonical order, "a" + U+0F73, a mark at the
	// start, ">" + U+0338 (a placeholder's ">" composes)
	"Mu\u0308ller", "Jo\u0301se\u0301", "JO\u0301SE\u0301", "Max\u0b3e", "\u0b47\u0b3e", "Oz\u0b3e", "\u1100\u1161\u11a8",
	"\u1112\u1161\u11ab\u1107\u1167\u11af", "\ud55c\ubcc4", "\u1161", "\u11a8", "\u212b", "\u2126", " \u212alaus", "\u212alaus", "Klaus",
	"I\u0307lker", "i\u0307lker", "e\u0301\u0323", "e\u0323\u0301", "a\u0f73\u0f73\u0f73", "Oz\u0f74\u0f73", "\u0301", "\u0338",
	"<name>\u0338", ">\u0338", "Mustermann-\u212alaus", "Mustermann-", "x-\u212alaus", "A\u030angstro\u0308m", "Se\u0301bastien",
	"Ma\u0301x Mu\u0308stermann", "Max\u0301", "n\u0301", "O\u0308zil", "\u00f6ZIL", "Jos\u00e9",
	// mask parity spec amendment A9 (2026-10-03): token runs of 23 and 24
	// characters without an ASCII digit, decimal digits of other scripts
	// (U+11DE0 only in Go's Unicode 17) and "=" around them, for the soup
	// to put after a run; a run before the first byte of U+0661 alone
	"abcdefghij-_+/KLMNOPQRST", "bcdefghij-_+/KLMNOPQRST", "\u06f3", "\u0966", "\uff13", "\U0001d7cf", "\U00011de0",
	"=\u0661", "==\u0661", "\u0661=", "abcdefghij-_+/KLMNOPQRST\u0661", "abcdefghij-_+/KLMNOPQRST\xd9",
	"\u0661\u0662\u0663\u0664\u0665 Berlin",
	// the NFC union (2026-10-02): email, street and postcode also on an NFC
	// copy of their input; NFD umlauts in the local part, in the domain and
	// after the top-level domain, a mark that composes with the letter
	// before it ("e", "n") and one that does not ("t", "."), a lone mark
	// before the "@", an NFD place after a postcode, NFD streets, a mark
	// after a house number, "+" before an NFD local part (spans that touch)
	"ju\u0308rgen" + at + "example.test", "x" + at + "mu\u0308nchen.example", "a" + at + "b.example\u0301", "a" + at + "b.test\u0301",
	"mu\u0308" + at + "x.example", "u\u0308" + at, at + "o\u0308.example", "a" + at + "b.c\u0308", "\u0308" + at + "x.example",
	"x" + at + "y.z+u\u0308" + at + "w.v", "a.b" + at + "c.d>.\u0301", "1" + "0115 Ko\u0308ln", "8" + "0331 M\u00fcnchen\u0301",
	"8" + "0331 Mu\u0308nchen, fertig", "Ko\u0308nigstra\u00dfe 5", "Am Gru\u0308nen Markt 3", "Hauptstra\u00dfe 1\u0301",
	"Weg 3\u0308", "Stra\u00dfe\u0301 4", "<email>\u0338", "<address>\u0301", "1" + "2345 Bad Ho\u0308mburg",
	// the keyword tail is W (2026-10-03): U+0345 folds to iota but is no
	// word rune, iota and U+1FBE are; with the regex-path runes above
	"token\u0345=abc", "secret\u0345x: ", "token\u03b9=", "\u0345", "\u1fbe",
	// separators, spaces, odd bytes
	" ", "  ", "\t", "\n", "\r\n", "\f", "\v", " ", " ", ", ", "; ", ". ", "(", ")", "/", "-", "_", "<", ">",
	"\xff", "\xc3", "\xe2\x82", "é", "ß", "ẞ", "ſ", "K", "١", "Ä", "ö",
}

func diffSoup(rng *rand.Rand, n int) string {
	var sb strings.Builder
	for sb.Len() < n {
		sb.WriteString(diffFragments[rng.Intn(len(diffFragments))])
		switch rng.Intn(6) {
		case 0:
			sb.WriteString(" ")
		case 1:
			sb.WriteString(randFrom(rng, "abcXYZ019 .-_/+=@:", 1+rng.Intn(6)))
		}
	}
	return sb.String()
}

func TestMaskDifferential(t *testing.T) {
	dir := t.TempDir()
	namesFiles := []string{filepath.Join(dir, "none.txt"), filepath.Join(dir, "names.txt"), filepath.Join(dir, "names2.txt"),
		filepath.Join(dir, "names50.txt")}
	_ = os.WriteFile(namesFiles[1], []byte("Max Mustermann\nErika Musterfrau\n"), 0o600)
	// Sequential replacement: "Hans Max" overlaps "Max Mustermann"; "name" and
	// "Redacted" hit the placeholders earlier steps wrote.
	_ = os.WriteFile(namesFiles[2], []byte("J\u00fcrgen M\u00fcller\n\u00d6zil\nKai Stra\u00dfe # comment\nAnn\nHans Max\nMax Mustermann\nname\nRedacted\n\u00d6\u00df\nY\u0131ld\u0131z\n\u0130lker\nI\u015f\u0131k\n"), 0o600)
	var fifty []string
	firsts := []string{"Anna", "Bernd", "Clara", "Dieter", "Eva", "Frank", "Gabi", "Hans", "Ines", "J\u00fcrgen", "Karl", "Lena", "Max"}
	lasts := []string{"M\u00fcller", "Schmidt", "Schneider", "Fischer", "Weber", "Meyer", "Wagner", "Becker", "Schulz", "Hoffmann"}
	for i := 0; i < 50; i++ {
		fifty = append(fifty, firsts[i%len(firsts)]+" "+lasts[(i*7)%len(lasts)])
	}
	_ = os.WriteFile(namesFiles[3], []byte(strings.Join(fifty, "\n")), 0o600)
	// names without ASCII letters (Greek, Cyrillic): the fold-canonical path
	namesFiles = append(namesFiles, filepath.Join(dir, "greek.txt"))
	_ = os.WriteFile(namesFiles[4], []byte(strings.Join(greekNames()[:12], "\n")+"\n\u00c5ngstr\u00f6m\n\u03a3\u03bf\u03c6\u03af\u03b1\n"), 0o600)
	// names stored decomposed (NFD), single letters in both forms (no names
	// since amendment A2), "name" (matches inside earlier placeholders, now
	// also where they were widened), a name ending in "-", Hangul as
	// conjoining jamo, I + combining dot above (U+0130 once in NFC)
	namesFiles = append(namesFiles, filepath.Join(dir, "nfd.txt"))
	_ = os.WriteFile(namesFiles[5], []byte("Jo\u0301se\u0301 Mu\u0308ller\nO\u0308\n\u00d6\n\u00e9\nOz\nname\nKlaus\nMustermann-\n"+
		"I\u0307lker\n\u1112\u1161\u11ab\u1107\u1167\u11af\nA\u030angstro\u0308m\nSe\u0301bastien\nMax Mustermann\nMa\u0301x\n"), 0o600)

	var texts []string
	for _, kind := range maskCorpusKinds {
		size := 96 << 10
		if kind == "digitrun" {
			size = 16 << 10 // the old phone step is quadratic on it
		}
		texts = append(texts, maskCorpus(rand.New(rand.NewSource(7)), kind, size))
	}
	for _, kind := range maskAddressKinds {
		texts = append(texts, maskCorpus(rand.New(rand.NewSource(7)), kind, 16<<10))
	}
	for _, kind := range maskNFCKinds {
		texts = append(texts, maskCorpus(rand.New(rand.NewSource(7)), kind, 16<<10))
	}
	// The Python parity cases. Their field is "text"; until 2026-10-02 this
	// read "input", so all 40 entered the corpus as empty strings.
	raw, err := os.ReadFile(filepath.Join("..", "typesafe", "tests", "mask-parity-cases.json"))
	if err != nil {
		t.Fatal(err)
	}
	var spec struct {
		Cases []struct {
			Text string `json:"text"`
		} `json:"cases"`
	}
	if err := json.Unmarshal(raw, &spec); err != nil {
		t.Fatal(err)
	}
	parity := 0
	for _, c := range spec.Cases {
		if c.Text != "" {
			texts = append(texts, c.Text)
			parity++
		}
	}
	if parity == 0 {
		t.Fatalf("mask-parity-cases.json: no case with a non-empty text (%d cases)", len(spec.Cases))
	}
	seeds := 25
	if v, err := strconv.Atoi(os.Getenv("IMPRINT_MASK_DIFF_SEEDS")); err == nil && v > 0 {
		seeds = v
	}
	for seed := 1; seed <= seeds; seed++ {
		rng := rand.New(rand.NewSource(int64(seed)))
		for i := 0; i < 100; i++ {
			texts = append(texts, diffSoup(rng, 1+rng.Intn(1500)))
		}
		texts = append(texts, diffSoup(rng, 20000))
	}
	for _, f := range diffFragments {
		texts = append(texts, f, " "+f+" ", "x"+f+"x")
	}
	// Street chains past the 16-candidate fallback, matching and failing.
	chain := func(n int, word string) string { return strings.TrimSpace(strings.Repeat(word+" ", n)) }
	for _, n := range []int{3, 15, 16, 17, 40, 300} {
		texts = append(texts,
			chain(n, "Aaa")+" Weg 1", chain(n, "Aaa")+" Weg 1x2", chain(n, "Aaa")+" Weg 12 - 14b", "Am "+chain(n, "Bbb")+" 3",
			"x.Am "+chain(n, "Ccc")+" 4 "+chain(n, "Ddd")+" Straße 5", chain(n, "Ä1")+" Ring 7", chain(n, "Xy-Am")+" Hof 8",
			"An der "+chain(n, "Eee")+" 9/10", chain(n, "Weg 1")+" Weg", chain(n, "Am Weg"), chain(n, "Ffff")+" Str 1 Gasse 2")
	}

	total := 0
	for _, t2 := range texts {
		total += len(t2)
	}
	for _, nf := range namesFiles {
		t.Setenv("TYPESAFE_NAMES_FILE", nf)
		var mu sync.Mutex
		failures := 0
		parallelEach(len(texts), func(i int) {
			got, gotCounts := MaskDetail(texts[i])
			want, wantCounts := maskDetailReference(texts[i])
			if got != want || gotCounts != wantCounts {
				mu.Lock()
				if failures++; failures <= 5 {
					t.Errorf("names %s, text %d (%d bytes): differs\ninput %.300q\n got %.300q %+v\nwant %.300q %+v",
						filepath.Base(nf), i, len(texts[i]), texts[i], got, gotCounts, want, wantCounts)
				}
				mu.Unlock()
			}
		})
	}
	t.Logf("%d texts (%d parity cases), %d bytes, %d names files: %d comparisons", len(texts), parity, total, len(namesFiles),
		len(namesFiles)*len(texts))
}

// TestMaskClasses: the S class of the production patterns (pySpaceClass),
// the reference's own spelling of it (refS, by category) and the fast path's
// predicate (isPySpace) agree on every rune, and the predicate is Python's
// \s as the parity spec measured it: unicode.IsSpace plus U+001C..U+001F.
func TestMaskClasses(t *testing.T) {
	prod := regexp.MustCompile(`^` + pySpaceClass + `$`)
	ref := regexp.MustCompile(`^` + refS + `$`)
	n := 0
	for r := rune(0); r <= unicode.MaxRune; r++ {
		if r >= 0xD800 && r <= 0xDFFF {
			continue
		}
		want := unicode.IsSpace(r) || (r >= 0x1c && r <= 0x1f)
		if want {
			n++
		}
		if isPySpace(r) != want || prod.MatchString(string(r)) != want || ref.MatchString(string(r)) != want {
			t.Errorf("%U: isPySpace %v, pySpaceClass %v, refS %v, want %v", r, isPySpace(r), prod.MatchString(string(r)),
				ref.MatchString(string(r)), want)
		}
	}
	if n != 29 {
		t.Errorf("%d runes in S, want 29 (Unicode 16 and 17: 25 White_Space plus U+001C..U+001F)", n)
	}
}

// TestMaskSpecExamples pins the examples of the mask parity spec (sections
// 1a, 1b, 3; 2026-10-02) and the edge cases around them. Every expectation is
// the output of the Python lane's ts_common.mask_detail (typesafe-dev branch
// mask-parity-unicode, 3fa4dfe, no names file), run 2026-10-02; the rows of
// amendment A3 (the "<redacted>" skip is case-sensitive) that of 752698b, run
// 2026-10-02.
func TestMaskSpecExamples(t *testing.T) {
	t.Setenv("TYPESAFE_NAMES_FILE", filepath.Join(t.TempDir(), "none.txt"))
	phone := "0" + "30 " + "1234" + "5678"
	for _, c := range []struct {
		in, want         string
		secret, phoneCnt int
	}{
		// 1a: Bearer/Basic up to the next ASCII space, after any S, no skip
		{`"Authorization": "Bearer abc"`, `"Authorization": <redacted> <redacted>`, 2, 0},
		{"authorization: Bearer abc", "authorization: <redacted> <redacted>", 2, 0},
		{"Bearer abc,def", "Bearer <redacted>", 1, 0},
		{"Bearer <redacted>", "Bearer <redacted>", 1, 0},
		{"Bearer\u00a0abc def", "Bearer\u00a0<redacted> def", 1, 0},
		{"Bearer\u00a0\u00a0 x", "Bearer\u00a0\u00a0 <redacted>", 1, 0},
		{"Bearer\u00a0\u00a0", "Bearer\u00a0<redacted>", 1, 0},
		{"Bearer \u00a0", "Bearer <redacted>", 1, 0},
		{"Bearer  ", "Bearer  ", 0, 0},
		{"Bearer\u001cabc", "Bearer\u001c<redacted>", 1, 0},
		{"Bearer\u000babc", "Bearer\u000b<redacted>", 1, 0},
		{"xBearer abc", "xBearer abc", 0, 0},
		{"_bearer abc", "_bearer abc", 0, 0},
		{"\u00e4Bearer abc", "\u00e4Bearer <redacted>", 1, 0},
		{"\u216bBearer abc", "\u216bBearer <redacted>", 1, 0},
		{"\u017fBearer abc", "\u017fBearer <redacted>", 1, 0},
		{"bas\u0131c x", "bas\u0131c <redacted>", 1, 0},
		{"BAS\u0130C x", "BAS\u0130C <redacted>", 1, 0},
		{"ba\u0131ic x", "ba\u0131ic x", 0, 0},
		// 1b: Unicode \w in the key, S around the separator, Turkish i,
		// Python's (?!<redacted>) lookaheads under (?i)
		{"token\u00e4=abc", "token\u00e4=<redacted>", 1, 0},
		{"credent\u0130al=abc", "credent\u0130al=<redacted>", 1, 0},
		{"ap\u0131_key=x", "ap\u0131_key=<redacted>", 1, 0},
		{"token\u00a0=\u00a0abc", "token\u00a0=\u00a0<redacted>", 1, 0},
		{"token\u000b=\u000babc", "token\u000b=\u000b<redacted>", 1, 0},
		{"token\u001c=\u001cabc", "token\u001c=\u001c<redacted>", 1, 0},
		{"token\u3000:\u3000abc", "token\u3000:\u3000<redacted>", 1, 0},
		{"token\u2009=\u2009abc", "token\u2009=\u2009<redacted>", 1, 0},
		{"token=\u00a0", "token=<redacted>", 1, 0},
		{"token= \u00a0", "token= <redacted>", 1, 0},
		{"token=<redacted>password=hunter2", "token=<redacted>password=<redacted>", 1, 0},
		{"token=<redacted>,password=x", "token=<redacted>,password=<redacted>", 1, 0},
		{"token=<redacted>", "token=<redacted>", 0, 0},
		// A3: only the exact placeholder is passed over (until then, under
		// Python's (?i), "token=<REDACTED>" and `token="<REDACTED>"` stayed
		// as they were, 0 secret_kw)
		{"token=<REDACTED>", "token=<redacted>", 1, 0},
		{`token="<REDACTED>"`, `token="<redacted>"`, 1, 0},
		{`token='<REDACTED>'`, `token='<redacted>'`, 1, 0},
		{`token="<Redacted>"`, `token="<redacted>"`, 1, 0},
		{"token=<rEdacted>", "token=<redacted>", 1, 0},
		{"token=<REDACTED>x", "token=<redacted>", 1, 0},
		{"Bearer <REDACTED>", "Bearer <redacted>", 1, 0},
		{"token= \u00a0<redacted>", "token= <redacted>", 1, 0},
		{"token=\u00a0\"<redacted>\"", "token=<redacted>\"<redacted>\"", 1, 0},
		{"token= \u00a0\"<redacted>\"", "token= <redacted>\"<redacted>\"", 1, 0},
		{`token="<redacted>x"`, `token="<redacted>"`, 1, 0},
		{`token='<redacted>'`, `token='<redacted>'`, 0, 0},
		{`token="<redacted>`, `token="<redacted>`, 0, 0},
		{`token="abc`, "token=<redacted>", 1, 0},
		{`secret_token: "<redacted>"`, `secret_token: "<redacted>"`, 0, 0},
		// 3: phone digits of any script; W, "+" or "." before blocks
		{"Tel " + "0" + "30 " + "\u0661\u0662\u0663\u0664\u0665\u0666\u0667\u0668", "Tel <phone>", 0, 1},
		{"0" + "30 " + "\u0967\u0968\u0969\u096a\u096b\u096c\u096d\u096e", "<phone>", 0, 1},
		{phone + "\u0661", "<phone>", 0, 1},
		{"\u00b2" + phone, "\u00b2" + phone, 0, 0},
		{"\u216b" + phone, "\u216b" + phone, 0, 0},
	} {
		got, counts := MaskDetail(c.in)
		if got != c.want || counts.SecretKW != c.secret || counts.Phone != c.phoneCnt {
			t.Errorf("%+q: got %+q (secret_kw %d, phone %d), want %+q (%d, %d)", c.in, got, counts.SecretKW, counts.Phone,
				c.want, c.secret, c.phoneCnt)
		}
		if ref, refCounts := maskDetailReference(c.in); ref != got || refCounts != counts {
			t.Errorf("%+q: reference %+q %+v, MaskDetail %+q %+v", c.in, ref, refCounts, got, counts)
		}
	}
}

// TestSecretKWTailUnfolded (2026-10-03): the keyword's tail of key=value is
// W, "." or "-" on both of MaskDetail's paths, as Python's (?i)[\w.-] is.
// Before, the regex path (secretKWRE, taken for text with a long s, Kelvin
// sign or Turkish i anywhere) folded the class and took U+0345, a mark that
// folds to iota, into the tail, while the fast path did not: the same span
// was masked or not depending on unrelated text. A Go/Python differential run
// found it. Every expectation below is ts_common.mask_detail's output on this
// branch (no names file), run 2026-10-03.
func TestSecretKWTailUnfolded(t *testing.T) {
	t.Setenv("TYPESAFE_NAMES_FILE", filepath.Join(t.TempDir(), "none.txt"))
	tail := regexp.MustCompile(`^(?i:` + secretKWTail + `)$`)
	for r := rune(0); r <= unicode.MaxRune; r++ {
		if r >= 0xd800 && r <= 0xdfff {
			continue
		}
		if got, want := tail.MatchString(string(r)), isSecretKWRunRune(r); got != want {
			t.Errorf("U+%04X: secretKWTail under (?i) %v, isSecretKWRunRune %v", r, got, want)
		}
	}
	// Each text alone and with each rune that sends the step to the regex
	// path after it: the result must not depend on that rune.
	for _, c := range []struct{ in, want string }{
		{"token\u0345=abc", "token\u0345=abc"},
		{"token\u0345x=abc", "token\u0345x=abc"},
		{"secret\u0345: abc", "secret\u0345: abc"},
		{"token\u0345=a\"b secret=c", "token\u0345=a\"b secret=<redacted>"},
		{"token\u03b9=abc", "token\u03b9=<redacted>"},
		{"token\u1fbe=abc", "token\u1fbe=<redacted>"},
	} {
		for _, after := range []string{"", " \u0131", " \u0130", " \u017f", " \u212a"} {
			got, counts := MaskDetail(c.in + after)
			want := c.want + after
			n := strings.Count(want, "<redacted>")
			if got != want || counts != (MaskCounts{SecretKW: n}) {
				t.Errorf("MaskDetail(%+q) = %+q %+v, want %+q {SecretKW:%d}", c.in+after, got, counts, want, n)
			}
		}
	}
}

// TestMaskAddressExamples pins the address examples of the mask parity spec
// (section 4 and amendment A5, 2026-10-02) and the edge cases around them:
// the union of Python's and Go's grammar, Python's classes, Unicode \b,
// leftmost-longest. Every expectation is the output of the Python lane's
// ts_common.mask_detail (typesafe-dev branch mask-parity-unicode, 6fc6326,
// whose address code is that of 3fa4dfe; no names file), run 2026-10-02,
// except the two NFD rows, which the NFC union (2026-10-02) changed: theirs
// follow the union spec (TestMaskNFCUnion). Checked 2026-10-02 against
// ts_common.mask_detail of typesafe-dev b340863 (synced here as of 5e77bad),
// no names file: both rows and all 14 of TestMaskNFCUnion agree, and
// TestMaskGolden passes its full corpus, 7,063 of 7,063.
// Postcodes are put together from parts (.githooks/pre-push).
func TestMaskAddressExamples(t *testing.T) {
	t.Setenv("TYPESAFE_NAMES_FILE", filepath.Join(t.TempDir(), "none.txt"))
	plz := func(head, rest string) string { return head + rest }
	for _, c := range []struct {
		in, want string
		n        int
	}{
		// section 4: masked in full
		{"Musterstrasse 12a", "<address>", 1},
		{"An der Linde 2", "<address>", 1},
		{"Zum See 6", "<address>", 1},
		{plz("1234", "5 BERLIN"), "<address>", 1},
		{plz("1234", "5 Bad Homburg"), "<address>", 1},
		{plz("1234", "5 Halle/Saale"), "<address>", 1},
		{"Ku'damm 3", "Ku'<address>", 1},
		{plz("7909", "8 Freiburg im Breisgau"), "<address>", 1},
		{plz("9154", "1 Rothenburg ob der Tauber"), "<address>", 1},
		{plz("PLZ ist 1011", "5 Berlin im Brief."), "PLZ ist <address>.", 1},
		// section 4: a boundary would cut a Unicode word, so nothing is masked
		{plz("8033", "1 M\u00fcnchen1"), plz("8033", "1 M\u00fcnchen1"), 0},
		{plz("1011", "5 Berlin\u00e9"), plz("1011", "5 Berlin\u00e9"), 0},
		{"Musterstra\u00dfe 12\u00e4", "Musterstra\u00dfe 12\u00e4", 0},
		{"\u00e4Musterstra\u00dfe 12", "\u00e4Musterstra\u00dfe 12", 0},
		// A5: Go's preposition branch masks prose
		{"Im Jahr 2024 wurde", "<address> wurde", 1},
		{"Am Montag 12 Uhr", "<address> Uhr", 1},
		{"Im PR 41 gefixt", "<address> gefixt", 1},
		{"Am 1", "Am 1", 0},
		{"Aaa Aaa Aaa 1", "Aaa Aaa Aaa 1", 0},
		// the grammars disagree on the end: the longer one wins
		{"Hauptstra\u00dfe 12 b", "<address>", 1},
		{"Weg 3 / 4 c", "<address>", 1},
		// Python's lowercase branch, its suffixes, folds of both
		{"string 3", "<address>", 1},
		{"Spring 2026", "<address>", 1},
		{"Kirchst\u0131eg 3", "<address>", 1},
		{"Alter Mar\u212at 5", "<address>", 1},
		{"haupt\u017ftra\u00dfe 4", "<address>", 1},
		{"STRA\u1e9eE 7", "<address>", 1},
		{"G\u00e4sschen 4", "<address>", 1},
		{"G\u00e4\u00dfchen 4", "G\u00e4\u00dfchen 4", 0},
		// S and D of other scripts
		{"Musterstra\u00dfe\u300012", "<address>", 1},
		{"Hauptweg \u0661\u0662", "<address>", 1},
		{"\u0661\u0662\u0663\u0664\u0665 Berlin", "<address>", 1},
		{plz("1234", "5\u00a0Berlin"), "<address>", 1},
		// a place ending in "." before a word rune
		{plz("1234", "5 Berlin.\u00e9"), "<address>\u00e9", 1},
		// decomposed umlauts (NFD): a combining mark is no word rune, so on the
		// text itself a boundary lies before it and the address stopped there
		// (until 2026-10-02: "<address>" + U+0308 + "nchen" and "Mu" + U+0308
		// + "<address>"); since the NFC union the match on the NFC copy is
		// added and merged with it
		{plz("8033", "1 Mu\u0308nchen"), "<address>", 1},
		{"Mu\u0308hlenweg 3", "<address>", 1},
	} {
		got, counts := MaskDetail(c.in)
		if got != c.want || counts.Address != c.n || counts != (MaskCounts{Address: c.n}) {
			t.Errorf("%+q: got %+q %+v, want %+q (address %d)", c.in, got, counts, c.want, c.n)
		}
		if ref, refCounts := maskDetailReference(c.in); ref != got || refCounts != counts {
			t.Errorf("%+q: reference %+q %+v, MaskDetail %+q %+v", c.in, ref, refCounts, got, counts)
		}
	}
}

// TestMaskOpaqueExamples pins the opaque step of mask parity spec amendment
// A9 (2026-10-03, superseding A4): a maximal run of 24 or more characters of
// [A-Za-z0-9_+/-], with up to two "=" after it, is masked if it holds an
// ASCII letter and either an ASCII digit or, directly after the run (before
// any "="), a decimal digit of any script (Python's \d in RX_OPAQUE's
// lookahead). Every expectation is the output of ts_common.mask_detail with
// no names file, run 2026-10-03 on typesafe-dev 752698b (RX_OPAQUE as a
// regex) and on 752698b with master's _LinearOpaqueMatcher (9a30e8d): the
// two agree on every row. The rows marked "Go only" have no Python text;
// the comment says what Python gives on the nearest one.
func TestMaskOpaqueExamples(t *testing.T) {
	t.Setenv("TYPESAFE_NAMES_FILE", filepath.Join(t.TempDir(), "none.txt"))
	r24 := "abcdefghij" + "-_+/" + "KLMNOPQRST" // 24 token characters, no digit
	r23 := r24[1:]
	r24d := r24[:23] + "7" // with an ASCII digit
	red := "<redacted>"
	for _, c := range []struct {
		in, want string
		counts   MaskCounts
	}{
		// a decimal digit of another script right after the run (4 bytes
		// for U+1D7CF)
		{r24 + "١", red + "١", MaskCounts{Opaque: 1}},
		{r24 + "۳", red + "۳", MaskCounts{Opaque: 1}},
		{r24 + "०", red + "०", MaskCounts{Opaque: 1}},
		{r24 + "３", red + "３", MaskCounts{Opaque: 1}},
		{r24 + "\U0001d7cf", red + "\U0001d7cf", MaskCounts{Opaque: 1}},
		{"x " + r24 + "١ y", "x " + red + "١ y", MaskCounts{Opaque: 1}},
		{"_______________________a١", red + "١", MaskCounts{Opaque: 1}},
		// too short, no letter, nothing or no Nd digit after the run
		{r23 + "١", r23 + "١", MaskCounts{}},
		{r24, r24, MaskCounts{}},
		{r24d, red, MaskCounts{Opaque: 1}},
		{strings.Repeat("1", 24) + "١", strings.Repeat("1", 24) + "١", MaskCounts{}},
		{strings.Repeat("_-+/", 6) + "١", strings.Repeat("_-+/", 6) + "١", MaskCounts{}},
		{r24 + " ١", r24 + " ١", MaskCounts{}},
		{"١" + r24, "١" + r24, MaskCounts{}},
		{r24 + "\u00e4", r24 + "\u00e4", MaskCounts{}},
		{r24 + "\u00b2", r24 + "\u00b2", MaskCounts{}},
		{r24 + "Ⅻ", r24 + "Ⅻ", MaskCounts{}},
		{r24 + "�", r24 + "�", MaskCounts{}},
		// "=" padding: the digit must come before any "="; after the digit
		// "=" is no padding
		{r24 + "=١", r24 + "=١", MaskCounts{}},
		{r24 + "==١", r24 + "==١", MaskCounts{}},
		{r24 + "١=", red + "١=", MaskCounts{Opaque: 1}},
		{r24 + "١==", red + "١==", MaskCounts{Opaque: 1}},
		{r24d + "===", red + "=", MaskCounts{Opaque: 1}},
		{r24d + "==١", red + "١", MaskCounts{Opaque: 1}},
		// one run after another
		{r24 + "١" + r24 + "١", red + "١" + red + "١", MaskCounts{Opaque: 2}},
		{r24 + "١" + r24, red + "١" + r24, MaskCounts{Opaque: 1}},
		// earlier steps: a placeholder takes the digit, or borders the run
		{r24 + "-١٢٣٤٥ Berlin", r24 + "-<address>", MaskCounts{Address: 1}},
		{r24 + "/١" + at + "example.test", r24 + "/<email>", MaskCounts{Email: 1}},
		{"a" + at + "b.de/" + r24 + "١", "<email>" + red + "١", MaskCounts{Email: 1, Opaque: 1}},
		{red + r24 + "١", red + red + "١", MaskCounts{Opaque: 1}},
		{"Bearer " + r24 + "١", "Bearer " + red, MaskCounts{SecretKW: 1}},
		{"token=abc " + r24 + "١", "token=" + red + " " + red + "١", MaskCounts{SecretKW: 1, Opaque: 1}},
		{"gh" + "p_" + r24 + "١", red + "١", MaskCounts{Opaque: 1}},
		// Go only: invalid UTF-8 after the run is no digit (Python, given
		// U+FFFD or a lone surrogate there, leaves the text as it is); the
		// first byte of U+0661 alone, and a stray byte
		{r24 + "\xd9", r24 + "\xd9", MaskCounts{}},
		{r24 + "\xff", r24 + "\xff", MaskCounts{}},
		// Go only, known limit (spec section 0): U+11DE0 is a digit in Go's
		// Unicode 17 and unassigned in Python's unicodedata 16.0, which
		// leaves this text as it is
		{r24 + "\U00011de0", red + "\U00011de0", MaskCounts{Opaque: 1}},
	} {
		got, counts := MaskDetail(c.in)
		if got != c.want || counts != c.counts {
			t.Errorf("%+q: got %+q %+v, want %+q %+v", c.in, got, counts, c.want, c.counts)
		}
		if ref, refCounts := maskDetailReference(c.in); ref != got || refCounts != counts {
			t.Errorf("%+q: reference %+q %+v, MaskDetail %+q %+v", c.in, ref, refCounts, got, counts)
		}
	}
}
