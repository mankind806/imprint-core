package main

import (
	"encoding/json"
	"math/rand"
	"runtime"
	"strings"
	"sync"
	"testing"
	"unicode/utf8"
)

// parallelEach runs check(i) for i in [0, n) on all CPUs; inputs are made
// beforehand, so results do not depend on the schedule.
func parallelEach(n int, check func(i int)) {
	var wg sync.WaitGroup
	next := make(chan int)
	for w := 0; w < runtime.GOMAXPROCS(0); w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range next {
				check(i)
			}
		}()
	}
	for i := 0; i < n; i++ {
		next <- i
	}
	close(next)
	wg.Wait()
}

// Cutting and masking (maskCapped): no fragment of a secret may be sent, wherever
// a window edge or the final cut falls. Fillers are the inputs that shrink most
// under masking (a long JWT, a base64 blob, UUIDs, lockfile integrity hashes),
// which is what pushed a cut onto the raw window's edge before. Secrets are
// put together at run time (.githooks/pre-push), and the fillers' alphabets
// keep clear of the secrets' 4-grams, so a hit can only be a leak.

type secretCase struct {
	name, text string
	secret     string // the part that must not appear, not even 4 runes of it
}

func testSecrets() []secretCase {
	return []secretCase{
		{"email", "jd1984" + "@" + "example.test", "jd1984" + "@" + "example.test"},
		{"iban", "DE" + "89" + " 3704" + " 0044" + " 0532" + " 0130" + " 00", "DE" + "89" + " 3704" + " 0044" + " 0532" + " 0130" + " 00"},
		{"password", `password="correct horse battery staple"`, "correct horse battery staple"},
		{"phone", "0" + "30 " + "12340123", "0" + "30 " + "12340123"},
		{"token", "ghp_" + "QWERTYUIOPASDFGHJKLZXCVBNMQWERTYUIOP", "ghp_" + "QWERTYUIOPASDFGHJKLZXCVBNMQWERTYUIOP"},
	}
}

// leakedGram returns a substring of secret of 4 runes that occurs in out, or "".
func leakedGram(out, secret string) string {
	r := []rune(secret)
	for i := 0; i+4 <= len(r); i++ {
		if g := string(r[i : i+4]); strings.Contains(out, g) {
			return g
		}
	}
	return ""
}

const (
	b64Alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/"
	uuidHex     = "56789abcdef" // no 0-4: a UUID cut below 24 runes stays unmasked and must not look like a secret
)

func randFrom(rng *rand.Rand, alphabet string, n int) string {
	b := make([]byte, n)
	for i := range b {
		b[i] = alphabet[rng.Intn(len(alphabet))]
	}
	return string(b)
}

// filler returns about n runes of one adversarial kind.
func filler(rng *rand.Rand, kind string, n int) string {
	var sb strings.Builder
	for sb.Len() < n {
		switch kind {
		case "jwt":
			sb.WriteString("eyJ" + randFrom(rng, b64Alphabet[:62], 3000) + "." + randFrom(rng, b64Alphabet[:62], 400) + "\n")
		case "base64":
			sb.WriteString(randFrom(rng, b64Alphabet, 5000) + "\n")
		case "uuids":
			for i := 0; i < 40; i++ {
				h := randFrom(rng, uuidHex, 32)
				sb.WriteString(h[:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:] + "\n")
			}
		case "integrity":
			sb.WriteString(`    "integrity": "sha512-` + randFrom(rng, b64Alphabet, 86) + `==",` + "\n")
		default: // prose
			sb.WriteString("Lorem ipsum dolor sit amet, consectetur adipiscing elit. ")
		}
	}
	return headRunes(sb.String(), n)
}

// placed puts secret, between two newlines, so that it starts pos runes from
// the start (fromEnd=false) or ends pos runes before the end (fromEnd=true) of a
// text of filler of kind; the text is total+len(secret)+1 runes long.
func placed(rng *rand.Rand, kind, secret string, pos, total int, fromEnd bool) string {
	start := pos
	if fromEnd {
		// the text is total+n+1 runes; its secret ends at start+n
		start = total + 1 - pos
	}
	before := filler(rng, kind, start-1)
	after := filler(rng, kind, total-start)
	return before + "\n" + secret + "\n" + after
}

// TestMaskCappedNoFragmentAtTheEdges places each secret across the window
// edges maskSide uses (the first window, side+margin; twice that, where a
// mildly shrinking text grows to; the largest, 2^maskGrowSteps times; counted
// from the cut side) and across the final cut, in each adversarial filler, for
// head, tail and both halves of head_tail. Windows in between depend on the
// text; the property test below covers them.
func TestMaskCappedNoFragmentAtTheEdges(t *testing.T) {
	setupJudge(t, "")
	const capChars = 300
	rng := rand.New(rand.NewSource(1))
	sep := utf8.RuneCountInString(headTailSeparator)
	h := (capChars - sep) / 2
	type sideCase struct {
		keep    string
		side    int  // runes kept on the side under test
		fromEnd bool // the side under test is the tail
	}
	type placement struct {
		label, text, secret, keep string
	}
	var ps []placement
	for _, sd := range []sideCase{
		{"head", capChars, false},
		{"tail", capChars, true},
		{"head_tail", h, false},
		{"head_tail", capChars - sep - h, true},
	} {
		edges := []int{sd.side + maskMargin, 2 * (sd.side + maskMargin), (1 << maskGrowSteps) * (sd.side + maskMargin), sd.side}
		total := (1<<maskGrowSteps+2)*(sd.side+maskMargin) + 2000
		for _, kind := range []string{"jwt", "base64", "uuids", "integrity"} {
			for _, sc := range testSecrets() {
				n := utf8.RuneCountInString(sc.text)
				for _, edge := range edges {
					// from the cut side the secret starts (head) or ends (tail) at
					// edge+off, so the edge runs through it
					for _, off := range []int{-n / 2, -1} {
						ps = append(ps, placement{
							label:  sd.keep + "/" + kind + "/" + sc.name + "/edge " + itoa(edge) + "/off " + itoa(off),
							text:   placed(rng, kind, sc.text, edge+off, total, sd.fromEnd),
							secret: sc.secret,
							keep:   sd.keep,
						})
					}
				}
			}
		}
	}
	var mu sync.Mutex
	parallelEach(len(ps), func(i int) {
		p := ps[i]
		out := maskCapped(p.text, capChars, p.keep)
		g := leakedGram(out, p.secret)
		m := utf8.RuneCountInString(out)
		if g != "" || m > capChars {
			mu.Lock()
			t.Errorf("%s: sent %q of the secret; %d runes (cap %d)", p.label, g, m, capChars)
			mu.Unlock()
		}
	})
	t.Logf("%d placements", len(ps))
}

func itoa(n int) string {
	b, _ := json.Marshal(n)
	return string(b)
}

// TestMaskCappedProperty: 1000 random texts (mixed fillers, random cap, random
// keep, one to three secrets at random places); no 4 runes of a secret are sent
// and the cap holds.
func TestMaskCappedProperty(t *testing.T) {
	setupJudge(t, "")
	rng := rand.New(rand.NewSource(20261002))
	kinds := []string{"jwt", "base64", "uuids", "integrity", "prose"}
	keeps := []string{"head", "tail", "head_tail"}
	secrets := testSecrets()
	const runs = 1000
	type run struct {
		capChars int
		keep     string
		text     string
		used     []secretCase
	}
	rs := make([]run, runs)
	for i := range rs {
		capChars := 64 + rng.Intn(900)
		keep := keeps[rng.Intn(len(keeps))]
		total := 2*(capChars+maskMargin) + rng.Intn(16*(capChars+maskMargin))
		var sb strings.Builder
		for utf8.RuneCountInString(sb.String()) < total {
			sb.WriteString(filler(rng, kinds[rng.Intn(len(kinds))], 200+rng.Intn(4000)))
		}
		text := []rune(sb.String())
		var used []secretCase
		for k := 0; k < 1+rng.Intn(3); k++ {
			sc := secrets[rng.Intn(len(secrets))]
			at := rng.Intn(len(text))
			text = append(text[:at:at], append([]rune("\n"+sc.text+"\n"), text[at:]...)...)
			used = append(used, sc)
		}
		rs[i] = run{capChars, keep, string(text), used}
	}
	var mu sync.Mutex
	parallelEach(runs, func(i int) {
		r := rs[i]
		out := maskCapped(r.text, r.capChars, r.keep)
		var msg string
		if n := utf8.RuneCountInString(out); n > r.capChars {
			msg = "over the cap"
		}
		for _, sc := range r.used {
			if g := leakedGram(out, sc.secret); g != "" {
				msg = "sent " + g + " of " + sc.name
			}
		}
		if msg != "" {
			mu.Lock()
			t.Errorf("run %d (cap %d, %s, %d bytes): %s", i, r.capChars, r.keep, len(r.text), msg)
			mu.Unlock()
		}
	})
	t.Logf("%d random runs", runs)
}

// TestForeignReturnNoFragmentEndToEnd: an address right at the head window's
// edge, behind a JWT that masking shrinks to ten runes, does not reach the request.
func TestForeignReturnNoFragmentEndToEnd(t *testing.T) {
	setupJudge(t, "test-key")
	ts := newFakeTS(t, answers(map[string]float64{"instruction_to_agent": 0.1, "exfil_request": 0.1}))
	rng := rand.New(rand.NewSource(7))
	sep := utf8.RuneCountInString(headTailSeparator)
	h := (8000 - sep) / 2
	for _, sc := range testSecrets() {
		for _, edge := range []int{h + maskMargin, 2 * (h + maskMargin), 16 * (h + maskMargin)} {
			n := utf8.RuneCountInString(sc.text)
			text := placed(rng, "jwt", sc.text, edge-n/2, 40*(h+maskMargin), false)
			p, _ := json.Marshal(map[string]any{"hook_event_name": "PostToolUse", "tool_response": text})
			if code, _, stderr := runJudgeCLI(t, string(p), "--gate", "foreign_return", "--endpoint", ts.srv.URL); code != exitOK {
				t.Fatal(stderr)
			}
			var req struct {
				State map[string]string `json:"state"`
			}
			_ = json.Unmarshal(ts.lastBody(t), &req)
			if g := leakedGram(req.State["foreign_text"], sc.secret); g != "" {
				t.Errorf("%s at %d: %q sent", sc.name, edge, g)
			}
		}
	}
}

func TestMaskCappedKeepsShortTextWhole(t *testing.T) {
	setupJudge(t, "")
	tok := "ghp_" + strings.Repeat("a1", 30)
	if got := maskCapped("short "+tok, 100, "head_tail"); got != "short <redacted>" {
		t.Errorf("under the cap: %q", got)
	}
	long := strings.Repeat("word ", 1000)
	for _, keep := range []string{"head", "tail", "head_tail"} {
		if n := utf8.RuneCountInString(maskCapped(long, 100, keep)); n != 100 {
			t.Errorf("%s: %d runes of plain text, want the cap", keep, n)
		}
	}
}
