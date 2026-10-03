package main

import (
	"regexp/syntax"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"unicode"
	"unicode/utf8"
)

// The address steps of MaskDetail (mask parity spec section 4, 2026-10-02,
// the user's decision: the union of ts_common.py's and Go's grammar, with
// Python's classes, leftmost-longest). A candidate (i, j) is valid if
// text[i:j] is in the language of the step's grammar (full match) and a
// Unicode word boundary (Python's \b: W on one side only, outside the text
// counting as non-W) lies at i and at j. The smallest i with a valid j wins,
// at it the largest j; the search goes on at j, on the original text, as
// re.sub does. The street step runs over the whole text, then the postcode
// step over its result.
//
// Search, after the Python lane's engine (typesafe-dev ts_common.py, P1,
// 3fa4dfe): each street grammar is PREFIX S+ NUMBER, and no prefix holds an S
// followed by a D or ends in S, while every NUMBER starts with a D. So the
// house number of a match starting at i starts at the first D after an S
// following i: at the anchor, a maximal run of S followed by a D. A match
// whose number follows an anchor starts after the D of the anchor before it.
// For each anchor, an automaton of the reversed prefixes scans from the S run
// back to that D (or the end of the last match), reporting every start; an
// automaton of the numbers scans forward from the D, reporting every end.
// The ranges scanned back do not overlap. A forward scan crosses the digit
// run of its anchor and, after it, at most four runs of S and eight other
// runes (the longest number is D+ S* letter S* [-/] S* D{1,4} S* letter, and
// the scan stops at the first rune no number can take), so it reaches no
// further than the fourth anchor after its own; each byte is scanned a
// bounded number of times and the step is linear. Postcode matches start with
// five D and an S after a non-W rune; their place names hold no D, so a
// forward scan from one candidate stops before the next.
//
// The automata are deterministic, built lazily from the grammars' compiled
// regexp/syntax programs (the classes are the ones Go's regexp would use:
// pySpaceClass, \p{Nd}, (?i) with turkishI) and shared by all calls.

// addrDFA runs one or more programs at once as a lazily built DFA over rune
// classes; tag bit p of a state is set when program p accepts there.
type addrDFA struct {
	progs []*syntax.Prog
	// Rune classes: runes that every rune instruction of the programs treats
	// alike. ascii holds the class of each ASCII rune; a non-ASCII rune r has
	// class cls[k] for the last k with lo[k] <= r.
	ascii [utf8.RuneSelf]uint16
	lo    []rune
	cls   []uint16
	// reps hold one instruction for each distinct rune set; setOf maps an
	// instruction (prog<<16 | pc) to its set; in[c][s]: class c is in set s.
	reps  []*syntax.Inst
	setOf map[uint32]int
	in    [][]bool

	mu     sync.Mutex
	index  map[string]int32
	all    []*addrState // the writer's list; readers use states
	states atomic.Pointer[[]*addrState]
}

// addrState is a DFA state: the program instructions it stands for, the tags
// of the programs that accept in it, and its transitions by class (0: not
// built yet, -1: dead, n > 0: state n-1).
type addrState struct {
	insts []uint32
	tags  uint8
	next  []atomic.Int32
}

func newAddrDFA(res ...*syntax.Regexp) *addrDFA {
	a := &addrDFA{setOf: map[uint32]int{}, index: map[string]int32{}}
	type runeSet struct {
		runes []rune
		fold  bool
	}
	var sets []runeSet
	var bounds []rune
	for p, re := range res {
		prog, err := syntax.Compile(re.Simplify())
		if err != nil {
			panic(err)
		}
		if len(prog.Inst) > 0xffff {
			panic("address grammar: program too large")
		}
		a.progs = append(a.progs, prog)
		for pc := range prog.Inst {
			in := &prog.Inst[pc]
			switch in.Op {
			case syntax.InstRune, syntax.InstRune1:
			case syntax.InstAlt, syntax.InstAltMatch, syntax.InstCapture, syntax.InstNop, syntax.InstMatch, syntax.InstFail:
				continue
			default:
				panic("address grammar: unsupported instruction " + in.String())
			}
			fold := in.Op == syntax.InstRune && len(in.Rune) == 1 && syntax.Flags(in.Arg)&syntax.FoldCase != 0
			id := slices.IndexFunc(sets, func(s runeSet) bool { return s.fold == fold && slices.Equal(s.runes, in.Rune) })
			if id < 0 {
				id = len(sets)
				sets = append(sets, runeSet{in.Rune, fold})
				a.reps = append(a.reps, in)
				// The edges of the instruction's rune set, as MatchRune
				// sees it: a single rune (and, with FoldCase, its simple
				// fold orbit) or ranges.
				if len(in.Rune) == 1 {
					r0 := in.Rune[0]
					bounds = append(bounds, r0, r0+1)
					if fold {
						for f := unicode.SimpleFold(r0); f != r0; f = unicode.SimpleFold(f) {
							bounds = append(bounds, f, f+1)
						}
					}
				} else {
					for k := 0; k+1 < len(in.Rune); k += 2 {
						bounds = append(bounds, in.Rune[k], in.Rune[k+1]+1)
					}
				}
			}
			a.setOf[uint32(p)<<16|uint32(pc)] = id
		}
	}
	// Intervals between edges are uniform; intervals with the same
	// membership in every set share a class.
	bounds = append(bounds, 0, utf8.RuneSelf)
	slices.Sort(bounds)
	bounds = slices.Compact(bounds)
	if bounds[len(bounds)-1] > unicode.MaxRune {
		bounds = bounds[:len(bounds)-1]
	}
	classOf := map[string]uint16{}
	classify := func(r rune) uint16 {
		sig := make([]byte, len(a.reps))
		for s, in := range a.reps {
			if in.MatchRune(r) {
				sig[s] = 1
			}
		}
		c, ok := classOf[string(sig)]
		if !ok {
			c = uint16(len(a.in))
			classOf[string(sig)] = c
			row := make([]bool, len(a.reps))
			for s := range sig {
				row[s] = sig[s] == 1
			}
			a.in = append(a.in, row)
		}
		return c
	}
	for r := rune(0); r < utf8.RuneSelf; r++ {
		a.ascii[r] = classify(r)
	}
	for _, b := range bounds {
		if b >= utf8.RuneSelf {
			a.lo = append(a.lo, b)
			a.cls = append(a.cls, classify(b))
		}
	}
	var starts []uint32
	for p, prog := range a.progs {
		starts = a.closure(starts, p, uint32(prog.Start))
	}
	a.addState(starts)
	return a
}

// class returns the rune class of r.
func (a *addrDFA) class(r rune) int {
	if r < utf8.RuneSelf {
		return int(a.ascii[r])
	}
	// the last k with lo[k] <= r; lo[0] is utf8.RuneSelf
	k, n := 0, len(a.lo)
	for n > 1 {
		h := n / 2
		if a.lo[k+h] <= r {
			k += h
			n -= h
		} else {
			n = h
		}
	}
	return int(a.cls[k])
}

// closure adds instruction pc of program p and what it reaches without
// consuming a rune: the rune instructions and matches, as prog<<16 | pc.
func (a *addrDFA) closure(set []uint32, p int, pc uint32) []uint32 {
	prog := a.progs[p]
	stack := []uint32{pc}
	for len(stack) > 0 {
		pc := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		id := uint32(p)<<16 | pc
		if slices.Contains(set, id) {
			continue
		}
		in := &prog.Inst[pc]
		switch in.Op {
		case syntax.InstAlt, syntax.InstAltMatch:
			set = append(set, id)
			stack = append(stack, in.Out, in.Arg)
		case syntax.InstCapture, syntax.InstNop:
			set = append(set, id)
			stack = append(stack, in.Out)
		case syntax.InstRune, syntax.InstRune1, syntax.InstMatch:
			set = append(set, id)
		}
	}
	return set
}

// addState returns the id of the state for the instruction set (made if
// new); a.mu must be held, or a not yet be shared.
func (a *addrDFA) addState(set []uint32) int32 {
	// keep only rune instructions and matches: they alone decide the future
	keep := set[:0:0]
	var tags uint8
	for _, id := range set {
		switch a.progs[id>>16].Inst[id&0xffff].Op {
		case syntax.InstRune, syntax.InstRune1:
			keep = append(keep, id)
		case syntax.InstMatch:
			keep = append(keep, id)
			tags |= 1 << (id >> 16)
		}
	}
	if len(keep) == 0 {
		return -1
	}
	slices.Sort(keep)
	kb := make([]byte, 0, 4*len(keep))
	for _, id := range keep {
		kb = append(kb, byte(id>>24), byte(id>>16), byte(id>>8), byte(id))
	}
	key := string(kb)
	if d, ok := a.index[key]; ok {
		return d
	}
	d := int32(len(a.all))
	a.index[key] = d
	a.all = append(a.all, &addrState{insts: keep, tags: tags, next: make([]atomic.Int32, len(a.in))})
	all := a.all
	a.states.Store(&all)
	return d
}

// build makes the transition of state d on class c.
func (a *addrDFA) build(d int32, c int) {
	a.mu.Lock()
	defer a.mu.Unlock()
	st := a.all[d]
	if st.next[c].Load() != 0 {
		return
	}
	var set []uint32
	for _, id := range st.insts {
		p, pc := int(id>>16), id&0xffff
		in := &a.progs[p].Inst[pc]
		if in.Op == syntax.InstMatch || !a.in[c][a.setOf[id]] {
			continue
		}
		set = a.closure(set, p, in.Out)
	}
	n := a.addState(set)
	if n >= 0 {
		n++
	}
	st.next[c].Store(n)
}

// addrRun is one scan's view of an addrDFA's states.
type addrRun struct {
	a      *addrDFA
	states []*addrState
}

func (a *addrDFA) run() addrRun { return addrRun{a, *a.states.Load()} }

// step returns the state after d on rune ch, or -1 when no program can match
// any more. Another scan may have made the transition and its target after
// this scan took its view of the states: build publishes a new state before
// the transition to it, so a target beyond the view is in a fresh one.
func (r *addrRun) step(d int32, ch rune) int32 {
	c := r.a.class(ch)
	n := r.states[d].next[c].Load()
	if n == 0 {
		r.a.build(d, c)
		n = r.states[d].next[c].Load()
	}
	if n < 0 {
		return -1
	}
	if int(n) > len(r.states) {
		r.states = *r.a.states.Load()
	}
	return n - 1
}

// scanForward calls fn(j, tags) for every j > i at which some program
// accepts text[i:j], j increasing.
func (a *addrDFA) scanForward(text string, i int, fn func(j int, tags uint8)) {
	r := a.run()
	d := int32(0)
	for k := i; k < len(text); {
		ch, w := utf8.DecodeRuneInString(text[k:])
		if d = r.step(d, ch); d < 0 {
			return
		}
		k += w
		if t := r.states[d].tags; t != 0 {
			fn(k, t)
		}
	}
}

// scanBackward, for programs of reversed grammars, calls fn(i, tags) for
// every i in [lo, end) at which some program accepts text[i:end], i
// decreasing. lo must be a rune start.
func (a *addrDFA) scanBackward(text string, end, lo int, fn func(i int, tags uint8)) {
	r := a.run()
	d := int32(0)
	for k := end; k > lo; {
		ch, w := utf8.DecodeLastRuneInString(text[lo:k])
		if d = r.step(d, ch); d < 0 {
			return
		}
		k -= w
		if t := r.states[d].tags; t != 0 {
			fn(k, t)
		}
	}
}

// reverseRegexp returns re for the reversed language.
func reverseRegexp(re *syntax.Regexp) *syntax.Regexp {
	c := *re
	c.Sub = nil
	for _, s := range re.Sub {
		c.Sub = append(c.Sub, reverseRegexp(s))
	}
	switch re.Op {
	case syntax.OpConcat:
		slices.Reverse(c.Sub)
	case syntax.OpLiteral:
		c.Rune = slices.Clone(re.Rune)
		slices.Reverse(c.Rune)
	case syntax.OpCharClass, syntax.OpAlternate, syntax.OpCapture, syntax.OpStar, syntax.OpPlus, syntax.OpQuest,
		syntax.OpRepeat, syntax.OpEmptyMatch, syntax.OpNoMatch:
	default:
		panic("address grammar: cannot reverse " + re.String())
	}
	return &c
}

// parseAddr parses an address grammar as regexp.Compile does.
func parseAddr(p string) *syntax.Regexp {
	re, err := syntax.Parse(p, syntax.Perl)
	if err != nil {
		panic(err)
	}
	return re
}

// addrAutomata are the automata of the two address steps: the reversed street
// prefixes and the house numbers (tag bit 0 Python's grammar, bit 1 Go's),
// and the union of the postcode grammars.
type addrAutomata struct{ prefix, number, plz *addrDFA }

func newAddrAutomata() *addrAutomata {
	return &addrAutomata{
		prefix: newAddrDFA(reverseRegexp(parseAddr(streetPrefixPy)), reverseRegexp(parseAddr(streetPrefixGo))),
		number: newAddrDFA(parseAddr(streetNumberPy), parseAddr(streetNumberGo)),
		plz:    newAddrDFA(parseAddr(`(?:` + plzPy + `)|(?:` + plzGo + `)`)),
	}
}

// addrDFAs are the automata MaskDetail uses, made on first use (about 0.5 ms;
// all states built: 6 ms, measured 2026-10-02) and shared by all calls.
var addrDFAs = sync.OnceValue(newAddrAutomata)

// isWordAt: W(text[p]); false at the end. isWordBefore: W(text[p-1]); false at
// the start.
func isWordAt(text string, p int) bool {
	if p >= len(text) {
		return false
	}
	if c := text[p]; c < utf8.RuneSelf {
		return isASCIIWordByte(c)
	}
	r, _ := utf8.DecodeRuneInString(text[p:])
	return isNameWordRune(r)
}

func isWordBefore(text string, p int) bool {
	if p <= 0 {
		return false
	}
	if c := text[p-1]; c < utf8.RuneSelf {
		return isASCIIWordByte(c)
	}
	r, _ := utf8.DecodeLastRuneInString(text[:p])
	return isNameWordRune(r)
}

// wordBoundary is Python's \b at p.
func wordBoundary(text string, p int) bool { return isWordBefore(text, p) != isWordAt(text, p) }

// isSpaceASCII marks the ASCII runes in S.
var isSpaceASCII = func() (t [utf8.RuneSelf]bool) {
	for r := rune(0); r < utf8.RuneSelf; r++ {
		t[r] = isPySpace(r)
	}
	return t
}()

// spaceAt returns the width of the S rune at k, or 0.
func spaceAt(text string, k int) int {
	if c := text[k]; c < utf8.RuneSelf {
		if isSpaceASCII[c] {
			return 1
		}
		return 0
	}
	r, w := utf8.DecodeRuneInString(text[k:])
	if isPySpace(r) {
		return w
	}
	return 0
}

// digitAt returns the width of the D rune at k, or 0.
func digitAt(text string, k int) int {
	if k >= len(text) {
		return 0
	}
	if c := text[k]; c < utf8.RuneSelf {
		if isASCIIDigit(c) {
			return 1
		}
		return 0
	}
	r, w := utf8.DecodeRuneInString(text[k:])
	if unicode.IsDigit(r) {
		return w
	}
	return 0
}

// maskStreet is the street step: the union of PyStreet and GoStreet.
func maskStreet(text string, repl func(string) string) string {
	return addrDFAs().maskStreet(text, repl)
}

// maskPlzOrt is the postcode step: the union of PyPlz and GoPlz.
func maskPlzOrt(text string, repl func(string) string) string {
	return addrDFAs().maskPlzOrt(text, repl)
}

func (dfa *addrAutomata) maskStreet(text string, repl func(string) string) string {
	var b strings.Builder
	done := 0   // text[:done] is written
	pos := 0    // no match starts before pos (the end of the last one)
	afterD := 0 // no match starts before the end of the last anchor's D
	for k := 0; k < len(text); {
		w := spaceAt(text, k)
		if w == 0 {
			if text[k] < utf8.RuneSelf {
				k++
			} else {
				_, n := utf8.DecodeRuneInString(text[k:])
				k += n
			}
			continue
		}
		// an anchor: the maximal S run text[rs:h], then a D at h
		rs := k
		for k += w; k < len(text); k += w {
			if w = spaceAt(text, k); w == 0 {
				break
			}
		}
		h := k
		dw := digitAt(text, h)
		if dw == 0 {
			continue
		}
		lo := max(pos, afterD)
		afterD = h + dw
		if rs <= lo {
			continue
		}
		// The smallest start for each grammar (tag bit): a prefix of it ends
		// at rs, and a word boundary lies at the start.
		var first [2]int
		found := uint8(0)
		dfa.prefix.scanBackward(text, rs, lo, func(i int, tags uint8) {
			if wordBoundary(text, i) {
				for t := range 2 {
					if tags&(1<<t) != 0 {
						first[t] = i
						found |= 1 << t
					}
				}
			}
		})
		if found == 0 {
			continue
		}
		// The largest end for each grammar: a number of it from h, a word
		// boundary at the end.
		var last [2]int
		ends := uint8(0)
		dfa.number.scanForward(text, h, func(j int, tags uint8) {
			if wordBoundary(text, j) {
				for t := range 2 {
					if tags&(1<<t) != 0 {
						last[t] = j
						ends |= 1 << t
					}
				}
			}
		})
		i, j := -1, -1
		for t := range 2 {
			if found&ends&(1<<t) == 0 {
				continue
			}
			switch {
			case i < 0 || first[t] < i:
				i, j = first[t], last[t]
			case first[t] == i:
				j = max(j, last[t])
			}
		}
		if i < 0 {
			continue
		}
		b.WriteString(text[done:i])
		b.WriteString(repl(text[i:j]))
		done, pos = j, j
	}
	if done == 0 {
		return text
	}
	b.WriteString(text[done:])
	return b.String()
}

func (dfa *addrAutomata) maskPlzOrt(text string, repl func(string) string) string {
	var b strings.Builder
	done := 0
	for i := 0; i < len(text); {
		w := digitAt(text, i)
		if w == 0 {
			if text[i] < utf8.RuneSelf {
				i++
			} else {
				_, n := utf8.DecodeRuneInString(text[i:])
				i += n
			}
			continue
		}
		if !isWordBefore(text, i) && fiveDigitsThenSpace(text, i) {
			j := -1
			dfa.plz.scanForward(text, i, func(e int, _ uint8) {
				if wordBoundary(text, e) {
					j = e
				}
			})
			if j > 0 {
				b.WriteString(text[done:i])
				b.WriteString(repl(text[i:j]))
				done, i = j, j
				continue
			}
		}
		i += w
	}
	if done == 0 {
		return text
	}
	b.WriteString(text[done:])
	return b.String()
}

// fiveDigitsThenSpace: five D runes from i, then an S rune.
func fiveDigitsThenSpace(text string, i int) bool {
	for n := 0; n < 5; n++ {
		w := digitAt(text, i)
		if w == 0 {
			return false
		}
		i += w
	}
	return i < len(text) && spaceAt(text, i) > 0
}
