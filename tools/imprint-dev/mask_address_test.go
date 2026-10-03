package main

import (
	"math/rand"
	"runtime"
	"strings"
	"sync"
	"testing"
	"unicode"
)

// buildAll makes every transition of every reachable state and returns the
// number of states.
func (a *addrDFA) buildAll() int {
	for d := 0; d < len(a.all); d++ {
		for c := range a.in {
			if a.all[d].next[c].Load() == 0 {
				a.build(int32(d), c)
			}
		}
	}
	return len(a.all)
}

// TestAddressAutomata: the automata of the address steps, built in full,
// stay under a bound, so the lazily built ones MaskDetail shares cannot grow
// past it whatever the text (measured 2026-10-02: 237, 20 and 80 states); and
// the rune classes they step by agree, on every rune, with the instructions
// of the compiled grammars (regexp/syntax's MatchRune, the test Go's regexp
// applies).
func TestAddressAutomata(t *testing.T) {
	d := newAddrAutomata()
	for _, c := range []struct {
		name string
		a    *addrDFA
		max  int
	}{{"prefix", d.prefix, 400}, {"number", d.number, 40}, {"plz", d.plz, 160}} {
		if n := c.a.buildAll(); n > c.max {
			t.Errorf("%s: %d states, more than %d", c.name, n, c.max)
		} else {
			t.Logf("%s: %d states, %d rune classes, %d rune sets", c.name, n, len(c.a.in), len(c.a.reps))
		}
		const chunk = 1 << 14
		var mu sync.Mutex
		bad := 0
		parallelEach((unicode.MaxRune+1)/chunk, func(k int) {
			for r := rune(k * chunk); r < rune((k+1)*chunk); r++ {
				if r >= 0xd800 && r <= 0xdfff {
					continue
				}
				cl := c.a.class(r)
				for s, in := range c.a.reps {
					if in.MatchRune(r) != c.a.in[cl][s] {
						mu.Lock()
						if bad++; bad <= 5 {
							t.Errorf("%s: %U: class %d says %v for %s", c.name, r, cl, c.a.in[cl][s], in)
						}
						mu.Unlock()
					}
				}
			}
		})
	}
}

// TestAddressAutomataConcurrent: automata shared by concurrent calls from
// their first, empty state on (as MaskDetail's are) give every call the
// result that one call alone gets.
func TestAddressAutomataConcurrent(t *testing.T) {
	var texts []string
	for i, kind := range append([]string{"addresses", "pii"}, maskAddressKinds...) {
		rng := rand.New(rand.NewSource(int64(i)))
		for k := 0; k < 40; k++ {
			texts = append(texts, maskCorpus(rng, kind, 200+rng.Intn(2000)))
		}
	}
	rng := rand.New(rand.NewSource(99))
	for k := 0; k < 200; k++ {
		texts = append(texts, diffSoup(rng, 1+rng.Intn(800)))
	}
	mask := func(d *addrAutomata, s string) string {
		repl := func(string) string { return "<address>" }
		return d.maskPlzOrt(d.maskStreet(s, repl), repl)
	}
	alone := newAddrAutomata()
	want := make([]string, len(texts))
	for i, s := range texts {
		want[i] = mask(alone, s)
	}
	for round := 0; round < 20; round++ {
		shared := newAddrAutomata()
		var wg sync.WaitGroup
		var mu sync.Mutex
		for g := 0; g < 2*runtime.GOMAXPROCS(0); g++ {
			wg.Add(1)
			go func(g int) {
				defer wg.Done()
				for k := range texts {
					i := (k*7 + g*13) % len(texts)
					if got := mask(shared, texts[i]); got != want[i] {
						mu.Lock()
						t.Errorf("round %d, goroutine %d, text %d: %.80q, want %.80q", round, g, i, got, want[i])
						mu.Unlock()
						return
					}
				}
			}(g)
		}
		wg.Wait()
		if t.Failed() {
			return
		}
	}
	if strings.Count(strings.Join(want, ""), "<address>") < 1000 {
		t.Errorf("the texts hold too few addresses to exercise the automata")
	}
}
