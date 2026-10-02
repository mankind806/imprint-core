package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// judge is the shared Jev core of jev-kern.md: one call per hook event, the
// gate's questions in one batch, a verdict allow|warn|ask|block on stdout, one
// JSONL log line. Questions, thresholds, caps, deadlines and fail modes live in
// one file, judge/gates.json, embedded into the binary. What code can decide
// (prefilters, order, flags) is Go in the gate's builder below, never Jev.
//
//	imprint-dev judge --gate <name> [--host claude|codex|agy|pi] [--deadline-ms N] [--no-ui]
//	imprint-dev judge --list
//	imprint-dev judge --version
//
// judge never exits 2, which Claude Code reads as blocking. Exit 0 once the
// verdict JSON is written: also when the call failed, and also when the call
// or its input was wrong (error_class call or input); then the gate's fail
// mode decides, and a gate that cannot be told counts as open. Exit 1 only
// when no verdict is due: a flag that does not parse, a stray argument, or
// --list/--version without a readable registry, or stdout not writable.

//go:embed judge/gates.json
var embeddedRegistry []byte

const (
	verdictAllow = "allow"
	verdictWarn  = "warn"
	verdictAsk   = "ask"
	verdictBlock = "block"

	failModeOpen   = "open"
	failModeClosed = "closed"

	// reasonCoreFailed is the reason of a closed gate's verdict after a failed call.
	reasonCoreFailed = "core_failed"

	// maxJudgeInput caps stdin; more is an input error.
	maxJudgeInput = 16 << 20

	// judgeExitUsage is judge's only non-zero exit (see above).
	judgeExitUsage = 1

	// Bounds the registry has to keep.
	minCapChars   = 64
	maxCapChars   = 32000
	maxCapItems   = 100
	maxDeadlineMS = 9000 // below the 10 s hook timeout of hooks/hooks.json

	// Error classes of a call that could not be judged as given.
	errClassCall  = "call"  // flags or wiring: gate, host, event, emit, source, endpoint
	errClassInput = "input" // stdin: unreadable, too large, not a JSON object, no event
	errClassUsage = "usage" // no verdict due (exit 1): a flag that does not parse, --list/--version without a registry

	// errClassTooLarge: the text to mask is over the mask budget
	// (maskBudgetBytes); nothing is sent.
	errClassTooLarge = "too_large"
)

var verdictRank = map[string]int{verdictAllow: 0, verdictWarn: 1, verdictAsk: 2, verdictBlock: 3}

var judgeHosts = map[string]bool{"claude": true, "codex": true, "agy": true, "pi": true}

var judgeSources = map[string]bool{"hook": true, "bench": true, "test": true}

// --- Registry ------------------------------------------------------------------

// Registry is judge/gates.json.
type Registry struct {
	RegistryVersion string           `json:"registry_version"`
	Model           string           `json:"model"`
	Gates           map[string]*Gate `json:"gates"`
}

// Gate is one gate of the registry.
type Gate struct {
	Description string              `json:"description"`
	Stage       string              `json:"stage"`
	Events      map[string][]string `json:"events"` // event -> verdicts the host can act on there
	Prefilter   []string            `json:"prefilter"`
	CodeFlags   []string            `json:"code_flags"`
	State       map[string]StateCap `json:"state"`
	Questions   QuestionSet         `json:"questions"`
	Rules       []GateRule          `json:"rules"`
	Messages    map[string]string   `json:"messages"`
	FailMode    string              `json:"fail_mode"`
	DeadlineMS  map[string]int      `json:"deadline_ms"`
	Calibration Calibration         `json:"calibration"`
}

// StateCap bounds one state field before it is sent.
type StateCap struct {
	CapChars int    `json:"cap_chars,omitempty"`
	Keep     string `json:"keep,omitempty"` // head or tail
	MaxItems int    `json:"max_items,omitempty"`
	MinChars int    `json:"min_chars,omitempty"`
}

// GateRule turns scores and code flags into a verdict.
type GateRule struct {
	If      string `json:"if"`
	Verdict string `json:"verdict"`
	Reason  string `json:"reason"`
	expr    ruleExpr
}

// Calibration records where a gate's thresholds come from.
type Calibration struct {
	Model          string   `json:"model"`
	Date           string   `json:"date"`
	Dataset        string   `json:"dataset"`
	N              int      `json:"n"`
	HitRate        *float64 `json:"hit_rate"`
	FalseAlarmRate *float64 `json:"false_alarm_rate"`
	Note           string   `json:"note,omitempty"`
}

// loadRegistry parses and validates a registry; unknown fields are an error.
func loadRegistry(data []byte) (*Registry, error) {
	var reg Registry
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&reg); err != nil {
		return nil, fmt.Errorf("registry: %w", err)
	}
	if err := reg.validate(); err != nil {
		return nil, fmt.Errorf("registry: %w", err)
	}
	return &reg, nil
}

func (r *Registry) validate() error {
	if r.RegistryVersion == "" {
		return errors.New("registry_version is empty")
	}
	if r.Model == "" || strings.HasSuffix(r.Model, "-latest") {
		return fmt.Errorf("model %q is not a pinned version", r.Model)
	}
	if len(r.Gates) == 0 {
		return errors.New("no gates")
	}
	for name, g := range r.Gates {
		if err := g.validate(name); err != nil {
			return fmt.Errorf("gate %q: %w", name, err)
		}
	}
	return nil
}

func (g *Gate) validate(name string) error {
	if g == nil {
		return errors.New("empty")
	}
	impl, ok := gateImpls[name]
	if !ok {
		return errors.New("this core has no builder for it")
	}
	if g.Stage != "advisory" && g.Stage != "enforcing" {
		return fmt.Errorf("stage %q is neither advisory nor enforcing", g.Stage)
	}
	if len(g.Events) == 0 {
		return errors.New("no events")
	}
	for ev, allowed := range g.Events {
		seen := map[string]bool{}
		for _, v := range allowed {
			if _, ok := verdictRank[v]; !ok {
				return fmt.Errorf("event %s: unknown verdict %q", ev, v)
			}
			seen[v] = true
		}
		if !seen[verdictAllow] || !seen[verdictWarn] {
			return fmt.Errorf("event %s: allow and warn must be possible", ev)
		}
		if g.DeadlineMS[ev] <= 0 {
			return fmt.Errorf("event %s: no deadline_ms", ev)
		}
	}
	for ev := range g.DeadlineMS {
		if _, ok := g.Events[ev]; !ok {
			return fmt.Errorf("deadline_ms for unknown event %s", ev)
		}
	}
	for ev, ms := range g.DeadlineMS {
		if ms > maxDeadlineMS {
			return fmt.Errorf("event %s: deadline_ms %d is above %d", ev, ms, maxDeadlineMS)
		}
	}
	if g.FailMode != failModeOpen && g.FailMode != failModeClosed {
		return fmt.Errorf("fail_mode %q is neither open nor closed", g.FailMode)
	}
	if g.Stage == "advisory" && g.FailMode != failModeOpen {
		return errors.New("an advisory gate has fail_mode open")
	}
	if err := subset("prefilter", g.Prefilter, impl.prefilters); err != nil {
		return err
	}
	if err := subset("code flag", g.CodeFlags, impl.flags); err != nil {
		return err
	}
	if err := validateState(g.State, impl.state); err != nil {
		return err
	}
	if len(g.Questions.IDs) == 0 {
		return errors.New("no questions")
	}
	for _, id := range g.Questions.IDs {
		q := g.Questions.ByID[id]
		if q.Type != TypeNoul {
			return fmt.Errorf("question %s: type %q; this core evaluates noul only", id, q.Type)
		}
		if strings.TrimSpace(q.Instructions) == "" {
			return fmt.Errorf("question %s: no instructions", id)
		}
	}
	flags := map[string]bool{}
	for _, f := range g.CodeFlags {
		flags[f] = true
	}
	for i := range g.Rules {
		rule := &g.Rules[i]
		x, err := parseRule(rule.If)
		if err != nil {
			return fmt.Errorf("rule %d %q: %w", i, rule.If, err)
		}
		rule.expr = x
		if _, ok := verdictRank[rule.Verdict]; !ok {
			return fmt.Errorf("rule %d: unknown verdict %q", i, rule.Verdict)
		}
		if g.Stage == "advisory" && verdictRank[rule.Verdict] > verdictRank[verdictWarn] {
			return fmt.Errorf("rule %d: an advisory gate says at most warn, not %s", i, rule.Verdict)
		}
		if rule.Reason == "" || strings.TrimSpace(g.Messages[rule.Reason]) == "" {
			return fmt.Errorf("rule %d: reason %q has no message", i, rule.Reason)
		}
		s, f := map[string]bool{}, map[string]bool{}
		x.refs(s, f)
		for id := range s {
			if _, ok := g.Questions.ByID[id]; !ok {
				return fmt.Errorf("rule %d: score %q is no question of this gate", i, id)
			}
		}
		for id := range f {
			if !flags[id] {
				return fmt.Errorf("rule %d: flag %q is no code flag of this gate", i, id)
			}
		}
	}
	if d := g.Calibration.Date; d != "" {
		if _, err := time.Parse(dateLayout, d); err != nil {
			return fmt.Errorf("calibration date %q is not YYYY-MM-DD", d)
		}
	}
	return nil
}

// validateState requires every state field the builder reads, with a cap in
// bounds: no cap means the whole input would be masked and sent.
func validateState(state map[string]StateCap, need map[string]capReq) error {
	for k := range state {
		if _, ok := need[k]; !ok {
			return fmt.Errorf("state %q is not read by this gate", k)
		}
	}
	for k, req := range need {
		sc, ok := state[k]
		if !ok {
			return fmt.Errorf("state %q missing", k)
		}
		if req.chars {
			if sc.CapChars < minCapChars || sc.CapChars > maxCapChars {
				return fmt.Errorf("state %q: cap_chars %d not in %d..%d", k, sc.CapChars, minCapChars, maxCapChars)
			}
			switch sc.Keep {
			case "head", "tail", "head_tail":
			default:
				return fmt.Errorf("state %q: keep %q is not head, tail or head_tail", k, sc.Keep)
			}
		} else if sc.CapChars != 0 || sc.Keep != "" {
			return fmt.Errorf("state %q takes no cap_chars or keep", k)
		}
		if req.items {
			if sc.MaxItems < 1 || sc.MaxItems > maxCapItems {
				return fmt.Errorf("state %q: max_items %d not in 1..%d", k, sc.MaxItems, maxCapItems)
			}
		} else if sc.MaxItems != 0 {
			return fmt.Errorf("state %q takes no max_items", k)
		}
		if sc.MinChars < 0 || (!req.minChars && sc.MinChars != 0) || (req.chars && sc.MinChars > sc.CapChars) {
			return fmt.Errorf("state %q: min_chars %d not allowed", k, sc.MinChars)
		}
	}
	return nil
}

func subset(what string, have, known []string) error {
	k := map[string]bool{}
	for _, s := range known {
		k[s] = true
	}
	for _, s := range have {
		if !k[s] {
			return fmt.Errorf("unknown %s %q", what, s)
		}
	}
	return nil
}

// allows reports whether the host can act on verdict at event. For an event
// the gate does not serve (a wiring error), it asks whether any of the gate's
// events allows it.
func (g *Gate) allows(event, verdict string) bool {
	lists := [][]string{g.Events[event]}
	if _, ok := g.Events[event]; !ok {
		lists = nil
		for _, l := range g.Events {
			lists = append(lists, l)
		}
	}
	for _, l := range lists {
		for _, v := range l {
			if v == verdict {
				return true
			}
		}
	}
	return false
}

// downgrade maps a verdict onto what the host can do at this event: the core
// does this, never the adapter (jev-kern.md section 3).
func (g *Gate) downgrade(event, verdict string) string {
	chain := map[string][]string{
		verdictBlock: {verdictBlock, verdictAsk, verdictWarn, verdictAllow},
		verdictAsk:   {verdictAsk, verdictWarn, verdictAllow},
		verdictWarn:  {verdictWarn, verdictAllow},
		verdictAllow: {verdictAllow},
	}[verdict]
	for _, v := range chain {
		if g.allows(event, v) {
			return v
		}
	}
	return verdictAllow
}

// failVerdict is the verdict after a failed call (no key, timeout, network,
// HTTP status, parse, missing answer, internal error). Owner decision
// 2026-10-02: an open (advisory) gate allows; a closed (critical) gate asks
// the person, or blocks where nobody can be asked (--no-ui, or an event where
// the host cannot ask); where it can do neither, it warns.
func (g *Gate) failVerdict(event string, noUI bool) string {
	if g.FailMode != failModeClosed {
		return verdictAllow
	}
	chain := []string{verdictAsk, verdictBlock, verdictWarn}
	if noUI {
		chain = []string{verdictBlock, verdictWarn}
	}
	for _, v := range chain {
		if g.allows(event, v) {
			return v
		}
	}
	return verdictWarn
}

func failMessage(gate, class, verdict string) string {
	if gate == "" {
		gate = "?"
	}
	switch verdict {
	case verdictAsk:
		return fmt.Sprintf("imprint judge: Die Prüfung %q konnte nicht laufen (%s). Das Gate ist kritisch: bitte selbst bestätigen.\n(imprint judge: the check %q could not run (%s). This gate is critical: please confirm yourself.)", gate, class, gate, class)
	case verdictBlock:
		return fmt.Sprintf("imprint judge: Die Prüfung %q konnte nicht laufen (%s). Das Gate ist kritisch und niemand kann gefragt werden: blockiert.\n(imprint judge: the check %q could not run (%s). This gate is critical and nobody can be asked: blocked.)", gate, class, gate, class)
	default:
		return fmt.Sprintf("imprint judge: Die Prüfung %q konnte nicht laufen (%s). Das Gate ist kritisch; dieses Ereignis kann weder fragen noch blockieren.\n(imprint judge: the check %q could not run (%s). This gate is critical; this event can neither ask nor block.)", gate, class, gate, class)
	}
}

// --- Verdict and log ---------------------------------------------------------------

// Verdict is judge's stdout: always exactly one object (jev-kern.md section 3).
type Verdict struct {
	Verdict         string             `json:"verdict"`
	Gate            string             `json:"gate"`
	Event           string             `json:"event"`
	Reasons         []string           `json:"reasons"`
	Message         string             `json:"message"`
	Scores          map[string]float64 `json:"scores"`
	CodeFlags       map[string]bool    `json:"code_flags"`
	Prefilter       string             `json:"prefilter,omitempty"`
	Model           string             `json:"model"`
	ModelResolved   string             `json:"model_resolved,omitempty"`
	RegistryVersion string             `json:"registry_version"`
	CoreVersion     string             `json:"core_version"`
	LatencyMS       int64              `json:"latency_ms"`
	LatencyPartsMS  map[string]int64   `json:"latency_parts_ms"`
	FailMode        string             `json:"fail_mode"`
	Failed          bool               `json:"failed"`
	ErrorClass      string             `json:"error_class"`
	// Set when the call named a gate, event or host the registry or judge does
	// not know; the name itself is never echoed (it could be anything).
	UnknownGate  bool `json:"unknown_gate,omitempty"`
	UnknownEvent bool `json:"unknown_event,omitempty"`
	UnknownHost  bool `json:"unknown_host,omitempty"`
}

// judgeLogLine is one line of the judge log. It never holds the state, a
// prompt, file content or the key (jev-kern.md section 5).
type judgeLogLine struct {
	TS              string             `json:"ts"`
	Source          string             `json:"source"`
	Host            string             `json:"host"`
	Gate            string             `json:"gate"`
	Event           string             `json:"event"`
	Session         string             `json:"session,omitempty"`
	RegistryVersion string             `json:"registry_version"`
	CoreVersion     string             `json:"core_version"`
	Model           string             `json:"model"`
	ModelResolved   string             `json:"model_resolved,omitempty"`
	Verdict         string             `json:"verdict"`
	Reasons         []string           `json:"reasons"`
	Scores          map[string]float64 `json:"scores"`
	CodeFlags       map[string]bool    `json:"code_flags"`
	Prefilter       string             `json:"prefilter,omitempty"`
	MaskCounts      MaskCounts         `json:"mask_counts"`
	LatencyMS       int64              `json:"latency_ms"`
	LatencyPartsMS  map[string]int64   `json:"latency_parts_ms"`
	FailMode        string             `json:"fail_mode"`
	Failed          bool               `json:"failed"`
	ErrorClass      string             `json:"error_class"`
	NoUI            bool               `json:"no_ui,omitempty"`
	UnknownGate     bool               `json:"unknown_gate,omitempty"`
	UnknownEvent    bool               `json:"unknown_event,omitempty"`
	UnknownHost     bool               `json:"unknown_host,omitempty"`
	ExitCode        int                `json:"exit_code,omitempty"`
}

// judgeLogPath: IMPRINT_JUDGE_LOG, else ${XDG_STATE_HOME:-~/.local/state}/imprint/judge.jsonl.
func judgeLogPath() string {
	if p := strings.TrimSpace(os.Getenv("IMPRINT_JUDGE_LOG")); p != "" {
		return expandHome(p)
	}
	base := os.Getenv("XDG_STATE_HOME")
	if base == "" || !filepath.IsAbs(base) {
		home, err := os.UserHomeDir()
		if err != nil || home == "" {
			return ""
		}
		base = filepath.Join(home, ".local", "state")
	}
	return filepath.Join(base, "imprint", "judge.jsonl")
}

func appendJudgeLog(line judgeLogLine) error {
	path := judgeLogPath()
	if path == "" {
		return errors.New("no log path")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	b, err := json.Marshal(line)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	_, werr := f.Write(append(b, '\n'))
	cerr := f.Close()
	if werr != nil {
		return werr
	}
	return cerr
}

func hashSession(id string) string {
	if id == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(id))
	return hex.EncodeToString(sum[:])[:16]
}

// --- Evaluation --------------------------------------------------------------------

type judgeOpts struct {
	endpoint string
	noUI     bool
}

type judgeOutcome struct {
	verdict Verdict
	masked  MaskCounts
}

// newVerdict is an allow verdict for gate, with the registry's model and
// version; a nil gate (one that cannot be told) counts as open.
func newVerdict(reg *Registry, name string, g *Gate, event string) Verdict {
	v := Verdict{
		Verdict:        verdictAllow,
		Gate:           name,
		Event:          event,
		Reasons:        []string{},
		Scores:         map[string]float64{},
		CodeFlags:      map[string]bool{},
		CoreVersion:    coreVersion(),
		LatencyPartsMS: map[string]int64{},
		FailMode:       failModeOpen,
	}
	if reg != nil {
		v.Model = reg.Model
		v.RegistryVersion = reg.RegistryVersion
	}
	if g != nil {
		v.FailMode = g.FailMode
	}
	return v
}

// setFailed applies the fail mode: open allows; closed asks, blocks or warns.
func setFailed(v *Verdict, g *Gate, event, class string, noUI bool) {
	v.Failed = true
	v.ErrorClass = class
	v.Verdict = verdictAllow
	v.Reasons = []string{}
	v.Message = ""
	if g == nil {
		return
	}
	v.Verdict = g.failVerdict(event, noUI)
	if v.Verdict != verdictAllow {
		v.Reasons = []string{reasonCoreFailed}
		v.Message = failMessage(v.Gate, class, v.Verdict)
	}
}

// evaluateGate runs one gate on one payload within ctx, whose deadline counts
// from the start of judge. It never panics and never returns an error: every
// failure becomes failed=true with the gate's fail verdict.
func evaluateGate(ctx context.Context, reg *Registry, name string, g *Gate, event string, payload map[string]any, o judgeOpts) (out judgeOutcome) {
	v := &out.verdict
	*v = newVerdict(reg, name, g, event)
	fail := func(class string) { setFailed(v, g, event, class, o.noUI) }
	timedOut := func() bool { return errors.Is(ctx.Err(), context.DeadlineExceeded) }
	defer func() {
		if r := recover(); r != nil {
			fail(errClassInternal)
		}
	}()

	t := time.Now()
	c, err := gateImpls[name].build(ctx, g, event, payload)
	v.LatencyPartsMS["build"] = time.Since(t).Milliseconds()
	for k, f := range c.flags {
		v.CodeFlags[k] = f
	}
	if err != nil {
		switch {
		case errors.Is(err, errTooLarge):
			fail(errClassTooLarge)
		case timedOut():
			fail(errClassTimeout)
		default:
			fail(errClassInternal)
		}
		return out
	}
	if c.skip != "" {
		v.Prefilter = c.skip
		return out
	}
	if timedOut() {
		fail(errClassTimeout)
		return out
	}

	t = time.Now()
	key := GetKeyWithContext(ctx)
	v.LatencyPartsMS["key"] = time.Since(t).Milliseconds()
	if key == "" {
		if timedOut() {
			fail(errClassTimeout)
		} else {
			fail(errClassNoKey)
		}
		return out
	}

	client := NewClient(key)
	client.Endpoint = o.endpoint
	client.Model = reg.Model
	client.HTTPClient = noRedirectHTTPClient()
	t = time.Now()
	resp, call, err := client.PostState(ctx, c.state, g.Questions)
	v.LatencyPartsMS["post"] = time.Since(t).Milliseconds()
	out.masked = call.Masked
	v.ModelResolved = call.ModelResolved
	if os.Getenv("IMPRINT_JUDGE_DEBUG") == "1" && call.ResponseKeys != nil {
		fmt.Fprintf(os.Stderr, "imprint-dev judge: response keys %v\n", call.ResponseKeys)
	}
	if err != nil {
		fail(errorClass(err))
		return out
	}

	missing := false
	for _, id := range g.Questions.IDs {
		p, ok := resp.Noul(id)
		if !ok {
			missing = true
			continue
		}
		v.Scores[id] = p
	}
	if missing {
		fail(errClassMissingAnswer)
		return out
	}

	env := ruleEnv{scores: v.Scores, flags: v.CodeFlags}
	verdict := verdictAllow
	var messages []string
	seen := map[string]bool{}
	for _, rule := range g.Rules {
		if !ruleApplies(rule.expr, env) || !rule.expr.eval(env) {
			continue
		}
		if verdictRank[rule.Verdict] > verdictRank[verdict] {
			verdict = rule.Verdict
		}
		if !seen[rule.Reason] {
			seen[rule.Reason] = true
			v.Reasons = append(v.Reasons, rule.Reason)
			messages = append(messages, g.Messages[rule.Reason])
		}
	}
	v.Verdict = g.downgrade(event, verdict)
	if v.Verdict != verdictAllow {
		v.Message = strings.Join(messages, "\n\n")
	}
	return out
}

// --- CLI ------------------------------------------------------------------------------

func loadJudgeRegistry(path string) (*Registry, error) {
	if path == "" {
		return loadRegistry(embeddedRegistry)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return loadRegistry(data)
}

// judgeRun writes the one verdict and the one log line of a call.
type judgeRun struct {
	start          time.Time
	reg            *Registry
	source         string
	host           string // a known host, or ""
	unknownHost    bool
	session        string
	noUI           bool
	flagGate       string // --gate, if the registry knows it
	stdout, stderr io.Writer
}

func (r *judgeRun) emit(v Verdict, masked MaskCounts) int {
	v.LatencyMS = time.Since(r.start).Milliseconds()
	v.UnknownHost = r.unknownHost
	code := writeJSON(r.stdout, r.stderr, v)
	line := judgeLogLine{
		TS:              time.Now().UTC().Format(time.RFC3339Nano),
		Source:          r.source,
		Host:            r.host,
		Gate:            v.Gate,
		Event:           v.Event,
		Session:         hashSession(r.session),
		RegistryVersion: v.RegistryVersion,
		CoreVersion:     v.CoreVersion,
		Model:           v.Model,
		ModelResolved:   v.ModelResolved,
		Verdict:         v.Verdict,
		Reasons:         v.Reasons,
		Scores:          v.Scores,
		CodeFlags:       v.CodeFlags,
		Prefilter:       v.Prefilter,
		MaskCounts:      masked,
		LatencyMS:       v.LatencyMS,
		LatencyPartsMS:  v.LatencyPartsMS,
		FailMode:        v.FailMode,
		Failed:          v.Failed,
		ErrorClass:      v.ErrorClass,
		NoUI:            r.noUI,
		UnknownGate:     v.UnknownGate,
		UnknownEvent:    v.UnknownEvent,
		UnknownHost:     v.UnknownHost,
	}
	if code != exitOK {
		line.ExitCode = code
	}
	if err := appendJudgeLog(line); err != nil {
		fmt.Fprintf(r.stderr, "imprint-dev judge: log not written: %v\n", err)
	}
	return code
}

// knownEvent reports whether some gate of the registry serves event.
func (r *judgeRun) knownEvent(event string) bool {
	if r.reg == nil {
		return false
	}
	for _, g := range r.reg.Gates {
		if _, ok := g.Events[event]; ok {
			return true
		}
	}
	return false
}

// callFailure answers a call or input error with a verdict. Whose fail mode
// decides: the gate --gate names, if the registry knows it, always; else
// gate g, if one could be told; else, if the registry has a closed gate, the
// strictest verdict the event allows across the closed gates (never allow);
// else open. Only names the registry or judge knows are echoed.
func (r *judgeRun) callFailure(name string, g *Gate, event, class, format string, a ...any) int {
	fmt.Fprintf(r.stderr, "imprint-dev judge: "+format+"\n", a...)
	if r.flagGate != "" {
		name, g = r.flagGate, r.reg.Gates[r.flagGate]
	}
	unknownGate := name != "" && g == nil
	if g == nil {
		name = ""
	}
	unknownEvent := event != "" && !r.knownEvent(event)
	if unknownEvent {
		event = ""
	}
	v := newVerdict(r.reg, name, g, event)
	v.UnknownGate, v.UnknownEvent = unknownGate, unknownEvent
	setFailed(&v, g, event, class, r.noUI)
	if g == nil {
		if fv, ok := strictFallback(r.reg, event, r.noUI); ok {
			v.FailMode = failModeClosed
			v.Verdict = fv
			v.Reasons = []string{reasonCoreFailed}
			v.Message = failMessage("", class, fv)
		}
	}
	return r.emit(v, MaskCounts{})
}

// strictFallback is the verdict for a failed call whose gate cannot be told,
// when the registry has closed (critical) gates: it might have been one of
// them, so the strictest verdict the event allows across the closed gates
// (those serving the event, else all of them) — ask, else block, else warn,
// never allow. ok is false when the registry has no closed gate.
func strictFallback(reg *Registry, event string, noUI bool) (verdict string, ok bool) {
	if reg == nil {
		return verdictAllow, false
	}
	var lists [][]string
	var all [][]string
	for _, g := range reg.Gates {
		if g.FailMode != failModeClosed {
			continue
		}
		if l, served := g.Events[event]; served {
			lists = append(lists, l)
		}
		for _, l := range g.Events {
			all = append(all, l)
		}
	}
	if len(all) == 0 {
		return verdictAllow, false
	}
	if len(lists) == 0 {
		lists = all
	}
	chain := []string{verdictAsk, verdictBlock, verdictWarn}
	if noUI {
		chain = []string{verdictBlock, verdictWarn}
	}
	for _, v := range chain {
		for _, l := range lists {
			for _, x := range l {
				if x == v {
					return v, true
				}
			}
		}
	}
	return verdictWarn, true
}

// usageExit writes the minimal log line of a call that gets no verdict (exit
// 1): no payload field, only what the flags and registry say.
func usageExit(stderr io.Writer, reg *Registry, source string, format string, a ...any) int {
	if format != "" {
		fmt.Fprintf(stderr, "imprint-dev judge: "+format+"\n", a...)
	}
	if !judgeSources[source] {
		source = "unknown"
	}
	line := judgeLogLine{
		TS:             time.Now().UTC().Format(time.RFC3339Nano),
		Source:         source,
		CoreVersion:    coreVersion(),
		Reasons:        []string{},
		Scores:         map[string]float64{},
		CodeFlags:      map[string]bool{},
		LatencyPartsMS: map[string]int64{},
		Failed:         true,
		ErrorClass:     errClassUsage,
		ExitCode:       judgeExitUsage,
	}
	if reg != nil {
		line.RegistryVersion, line.Model = reg.RegistryVersion, reg.Model
	}
	if err := appendJudgeLog(line); err != nil {
		fmt.Fprintf(stderr, "imprint-dev judge: log not written: %v\n", err)
	}
	return judgeExitUsage
}

func runJudge(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	start := time.Now()
	fs := flag.NewFlagSet("judge", flag.ContinueOnError)
	fs.SetOutput(stderr)
	gateFlag := fs.String("gate", "", "gate name (must match the stdin envelope's gate, if it names one)")
	hostFlag := fs.String("host", "", "claude, codex, agy or pi (must match the envelope's host, if it names one)")
	emit := fs.String("emit", "verdict", "output form; only verdict is built (hook: not yet)")
	deadlineMS := fs.Int("deadline-ms", 0, "total deadline in ms from the start of judge, incl. reading stdin and the key (default: the registry's for the event)")
	noUI := fs.Bool("no-ui", false, "nobody can be asked: a critical gate whose call failed blocks instead of asking")
	list := fs.Bool("list", false, "print gates, registry version and pinned model as JSON")
	showVersion := fs.Bool("version", false, "print core version, registry version and pinned model as JSON")
	registryPath := fs.String("registry", "", "registry file instead of the embedded one (tests and calibration only)")
	endpoint := fs.String("endpoint", DefaultEndpoint, "TypeSafe API endpoint (api.typesafe.ai or loopback)")
	source := fs.String("source", "hook", "log source: hook, bench or test")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return exitOK
		}
		return usageExit(stderr, nil, *source, "")
	}
	if fs.NArg() > 0 {
		return usageExit(stderr, nil, *source, "unexpected argument")
	}

	reg, regErr := loadJudgeRegistry(*registryPath)
	if *showVersion || *list {
		if regErr != nil {
			return usageExit(stderr, nil, *source, "%v", regErr)
		}
		if *showVersion {
			return writeJSON(stdout, stderr, map[string]string{
				"core_version":     coreVersion(),
				"registry_version": reg.RegistryVersion,
				"model":            reg.Model,
			})
		}
		return writeJSON(stdout, stderr, listRegistry(reg))
	}

	// From here on every outcome is one verdict on stdout and exit 0.
	r := &judgeRun{start: start, reg: reg, source: *source, noUI: *noUI, stdout: stdout, stderr: stderr}
	if !judgeSources[r.source] {
		r.source = "unknown"
	}
	gateOf := func(name string) *Gate {
		if reg == nil {
			return nil
		}
		return reg.Gates[name]
	}
	if gateOf(*gateFlag) != nil {
		r.flagGate = *gateFlag
	}
	setHost := func(h string) {
		r.host, r.unknownHost = "", false
		if judgeHosts[h] {
			r.host = h
		} else if h != "" {
			r.unknownHost = true
		}
	}
	setHost(*hostFlag)

	raw, err := io.ReadAll(io.LimitReader(stdin, maxJudgeInput+1))
	if err != nil {
		return r.callFailure(*gateFlag, gateOf(*gateFlag), "", errClassInput, "cannot read stdin: %v", err)
	}
	if len(raw) > maxJudgeInput {
		return r.callFailure(*gateFlag, gateOf(*gateFlag), "", errClassInput, "stdin is larger than %d bytes", maxJudgeInput)
	}
	var top map[string]any
	if err := json.Unmarshal(raw, &top); err != nil || top == nil {
		return r.callFailure(*gateFlag, gateOf(*gateFlag), "", errClassInput, "stdin is not a JSON object")
	}

	// stdin is authoritative: an envelope {"host","gate","payload"} (jev-kern.md
	// section 3) or, from a one-line hook wrapper, the host's raw hook JSON.
	payload := top
	envHost, envGate := "", ""
	if p, ok := top["payload"]; ok {
		pm, ok := p.(map[string]any)
		if !ok {
			return r.callFailure(*gateFlag, gateOf(*gateFlag), "", errClassInput, "envelope payload is not a JSON object")
		}
		payload = pm
		envHost, _ = top["host"].(string)
		envGate, _ = top["gate"].(string)
	}
	r.session, _ = payload["session_id"].(string)
	event, _ := payload["hook_event_name"].(string)

	host, herr := agree("host", *hostFlag, envHost)
	if herr == nil {
		setHost(host)
	}
	gateName, err := agree("gate", *gateFlag, envGate)
	if err != nil {
		// --gate decides if the registry knows it (callFailure); otherwise the
		// gate cannot be told, whatever the envelope says.
		return r.callFailure(*gateFlag, nil, event, errClassCall, "--gate and the envelope's gate differ")
	}
	if gateName == "" {
		return r.callFailure("", nil, event, errClassCall, "no gate: give --gate or an envelope with gate")
	}
	if regErr != nil {
		return r.callFailure(gateName, nil, event, errClassCall, "%v", regErr)
	}
	g, ok := reg.Gates[gateName]
	if !ok {
		return r.callFailure(gateName, nil, event, errClassCall, "unknown gate (see --list)")
	}
	if event == "" && len(g.Events) == 1 {
		for ev := range g.Events {
			event = ev
		}
	}
	switch {
	case herr != nil:
		return r.callFailure(gateName, g, event, errClassCall, "--host and the envelope's host differ")
	case r.unknownHost:
		return r.callFailure(gateName, g, event, errClassCall, "unknown host: use claude, codex, agy or pi")
	case *emit != "verdict":
		return r.callFailure(gateName, g, event, errClassCall, "--emit: only verdict is built in this stage")
	case !judgeSources[*source]:
		return r.callFailure(gateName, g, event, errClassCall, "--source: use hook, bench or test")
	case !isAllowedEndpoint(*endpoint):
		return r.callFailure(gateName, g, event, errClassCall, "--endpoint is not allowed (api.typesafe.ai or loopback)")
	case *deadlineMS < 0:
		return r.callFailure(gateName, g, event, errClassCall, "--deadline-ms must not be negative")
	case event == "":
		return r.callFailure(gateName, g, event, errClassInput, "the payload names no hook_event_name, and gate %q serves several events", gateName)
	}
	if _, ok := g.Events[event]; !ok {
		return r.callFailure(gateName, g, event, errClassCall, "gate %q does not serve this event", gateName)
	}

	deadline := time.Duration(g.DeadlineMS[event]) * time.Millisecond
	if *deadlineMS > 0 {
		deadline = time.Duration(*deadlineMS) * time.Millisecond
	}
	ctx, cancel := context.WithDeadline(context.Background(), start.Add(deadline))
	defer cancel()
	res := evaluateGate(ctx, reg, gateName, g, event, payload, judgeOpts{endpoint: *endpoint, noUI: *noUI})
	return r.emit(res.verdict, res.masked)
}

// agree returns the one value a flag and the envelope give; both set and
// different is a call error.
func agree(what, flagValue, envValue string) (string, error) {
	if flagValue != "" && envValue != "" && flagValue != envValue {
		return "", fmt.Errorf("--%s %q and the envelope's %s %q differ", what, flagValue, what, envValue)
	}
	if flagValue != "" {
		return flagValue, nil
	}
	return envValue, nil
}

func writeJSON(stdout, stderr io.Writer, v any) int {
	enc := json.NewEncoder(stdout)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		fmt.Fprintf(stderr, "imprint-dev judge: cannot write output: %v\n", err)
		return judgeExitUsage
	}
	return exitOK
}

type listedGate struct {
	Gate             string   `json:"gate"`
	Stage            string   `json:"stage"`
	Events           []string `json:"events"`
	FailMode         string   `json:"fail_mode"`
	Questions        []string `json:"questions"`
	CalibrationModel string   `json:"calibration_model"`
	CalibrationDate  string   `json:"calibration_date"`
}

func listRegistry(reg *Registry) any {
	var gates []listedGate
	for name, g := range reg.Gates {
		lg := listedGate{Gate: name, Stage: g.Stage, FailMode: g.FailMode,
			CalibrationModel: g.Calibration.Model, CalibrationDate: g.Calibration.Date}
		for ev := range g.Events {
			lg.Events = append(lg.Events, ev)
		}
		lg.Questions = append(lg.Questions, g.Questions.IDs...)
		sort.Strings(lg.Events)
		gates = append(gates, lg)
	}
	sort.Slice(gates, func(i, j int) bool { return gates[i].Gate < gates[j].Gate })
	return struct {
		RegistryVersion string       `json:"registry_version"`
		Model           string       `json:"model"`
		CoreVersion     string       `json:"core_version"`
		Gates           []listedGate `json:"gates"`
	}{reg.RegistryVersion, reg.Model, coreVersion(), gates}
}
