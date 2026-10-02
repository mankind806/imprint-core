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
// Exit 0 once the verdict JSON is written, also when the call failed (then
// failed=true and the gate's fail mode decided the verdict); exit 2 only when
// the call itself is wrong (flags, stdin, gate, event).

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

	maxJudgeInput = 16 << 20
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
	Questions   map[string]Question `json:"questions"`
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
	if g.FailMode != failModeOpen && g.FailMode != failModeClosed {
		return fmt.Errorf("fail_mode %q is neither open nor closed", g.FailMode)
	}
	if err := subset("prefilter", g.Prefilter, impl.prefilters); err != nil {
		return err
	}
	if err := subset("code flag", g.CodeFlags, impl.flags); err != nil {
		return err
	}
	for _, k := range impl.stateKeys {
		if _, ok := g.State[k]; !ok {
			return fmt.Errorf("state %q missing", k)
		}
	}
	if len(g.Questions) == 0 {
		return errors.New("no questions")
	}
	for id, q := range g.Questions {
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
		if rule.Reason == "" || strings.TrimSpace(g.Messages[rule.Reason]) == "" {
			return fmt.Errorf("rule %d: reason %q has no message", i, rule.Reason)
		}
		s, f := map[string]bool{}, map[string]bool{}
		x.refs(s, f)
		for id := range s {
			if _, ok := g.Questions[id]; !ok {
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

func (g *Gate) allows(event, verdict string) bool {
	for _, v := range g.Events[event] {
		if v == verdict {
			return true
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
	deadline time.Duration // 0: the registry's deadline for the event
	noUI     bool
}

type judgeOutcome struct {
	verdict Verdict
	masked  MaskCounts
}

// evaluateGate runs one gate on one payload. It never panics and never returns
// an error: every failure becomes failed=true with the gate's fail verdict.
func evaluateGate(reg *Registry, name string, g *Gate, event string, payload map[string]any, o judgeOpts, start time.Time) (out judgeOutcome) {
	v := &out.verdict
	*v = Verdict{
		Verdict:         verdictAllow,
		Gate:            name,
		Event:           event,
		Reasons:         []string{},
		Scores:          map[string]float64{},
		CodeFlags:       map[string]bool{},
		Model:           reg.Model,
		RegistryVersion: reg.RegistryVersion,
		CoreVersion:     coreVersion(),
		LatencyPartsMS:  map[string]int64{},
		FailMode:        g.FailMode,
	}
	fail := func(class string) {
		v.Failed = true
		v.ErrorClass = class
		v.Verdict = g.failVerdict(event, o.noUI)
		v.Reasons = []string{}
		v.Message = ""
		if v.Verdict != verdictAllow {
			v.Reasons = []string{reasonCoreFailed}
			v.Message = failMessage(name, class, v.Verdict)
		}
	}
	defer func() {
		if r := recover(); r != nil {
			fail(errClassInternal)
		}
		v.LatencyMS = time.Since(start).Milliseconds()
	}()

	c, err := gateImpls[name].build(g, event, payload)
	for k, f := range c.flags {
		v.CodeFlags[k] = f
	}
	out.masked = c.masked
	if err != nil {
		fail(errClassInternal)
		return out
	}
	if c.skip != "" {
		v.Prefilter = c.skip
		return out
	}

	deadline := time.Duration(g.DeadlineMS[event]) * time.Millisecond
	if o.deadline > 0 {
		deadline = o.deadline
	}
	ctx, cancel := context.WithTimeout(context.Background(), deadline)
	defer cancel()

	t := time.Now()
	key := GetKeyWithContext(ctx)
	v.LatencyPartsMS["key"] = time.Since(t).Milliseconds()
	if key == "" {
		fail(errClassNoKey)
		return out
	}

	client := NewClient(key)
	client.Endpoint = o.endpoint
	client.Model = reg.Model
	client.HTTPClient = noRedirectHTTPClient()
	t = time.Now()
	resp, call, err := client.PostState(ctx, c.state, g.Questions)
	v.LatencyPartsMS["post"] = time.Since(t).Milliseconds()
	out.masked = addMaskCounts(out.masked, call.Masked)
	v.ModelResolved = call.ModelResolved
	if os.Getenv("IMPRINT_JUDGE_DEBUG") == "1" && call.ResponseKeys != nil {
		fmt.Fprintf(os.Stderr, "imprint-dev judge: response keys %v\n", call.ResponseKeys)
	}
	if err != nil {
		fail(errorClass(err))
		return out
	}

	ids := make([]string, 0, len(g.Questions))
	for id := range g.Questions {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	missing := false
	for _, id := range ids {
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

func runJudge(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	start := time.Now()
	fs := flag.NewFlagSet("judge", flag.ContinueOnError)
	gateFlag := fs.String("gate", "", "gate name (must match the stdin envelope's gate, if it names one)")
	hostFlag := fs.String("host", "", "claude, codex, agy or pi (must match the envelope's host, if it names one)")
	emit := fs.String("emit", "verdict", "output form; only verdict is built (hook: not yet)")
	deadlineMS := fs.Int("deadline-ms", 0, "total deadline in ms incl. key lookup (default: the registry's for the event)")
	noUI := fs.Bool("no-ui", false, "nobody can be asked: a critical gate whose call failed blocks instead of asking")
	list := fs.Bool("list", false, "print gates, registry version and pinned model as JSON")
	showVersion := fs.Bool("version", false, "print core version, registry version and pinned model as JSON")
	registryPath := fs.String("registry", "", "registry file instead of the embedded one (tests and calibration only)")
	endpoint := fs.String("endpoint", DefaultEndpoint, "TypeSafe API endpoint (api.typesafe.ai or loopback)")
	source := fs.String("source", "hook", "log source: hook, bench or test")
	if done, code := parseFlags(fs, args, stderr); done {
		return code
	}
	usage := func(format string, a ...any) int {
		fmt.Fprintf(stderr, "imprint-dev judge: "+format+"\n", a...)
		return exitError
	}

	reg, err := loadJudgeRegistry(*registryPath)
	if err != nil {
		return usage("%v", err)
	}
	if *showVersion {
		return writeJSON(stdout, stderr, map[string]string{
			"core_version":     coreVersion(),
			"registry_version": reg.RegistryVersion,
			"model":            reg.Model,
		})
	}
	if *list {
		return writeJSON(stdout, stderr, listRegistry(reg))
	}
	if *emit != "verdict" {
		return usage("--emit %q is not built in this stage; only verdict", *emit)
	}
	if *deadlineMS < 0 {
		return usage("--deadline-ms must not be negative")
	}
	if !judgeSources[*source] {
		return usage("--source %q: use hook, bench or test", *source)
	}
	if !isAllowedEndpoint(*endpoint) {
		return usage("--endpoint %q is not allowed", *endpoint)
	}

	raw, err := io.ReadAll(io.LimitReader(stdin, maxJudgeInput+1))
	if err != nil {
		return usage("cannot read stdin: %v", err)
	}
	if len(raw) > maxJudgeInput {
		return usage("stdin is larger than %d bytes", maxJudgeInput)
	}
	var top map[string]any
	if err := json.Unmarshal(raw, &top); err != nil || top == nil {
		return usage("stdin is not a JSON object")
	}

	// stdin is authoritative: an envelope {"host","gate","payload"} (jev-kern.md
	// section 3) or, from a one-line hook wrapper, the host's raw hook JSON.
	payload := top
	envHost, envGate := "", ""
	if p, ok := top["payload"]; ok {
		pm, ok := p.(map[string]any)
		if !ok {
			return usage("envelope payload is not a JSON object")
		}
		payload = pm
		envHost, _ = top["host"].(string)
		envGate, _ = top["gate"].(string)
	}
	gateName, err := agree("gate", *gateFlag, envGate)
	if err != nil {
		return usage("%v", err)
	}
	host, err := agree("host", *hostFlag, envHost)
	if err != nil {
		return usage("%v", err)
	}
	if gateName == "" {
		return usage("no gate: give --gate or an envelope with gate")
	}
	if host != "" && !judgeHosts[host] {
		return usage("host %q: use claude, codex, agy or pi", host)
	}
	if host == "" {
		host = "unknown"
	}
	g, ok := reg.Gates[gateName]
	if !ok {
		return usage("unknown gate %q (see --list)", gateName)
	}
	event, _ := payload["hook_event_name"].(string)
	if event == "" {
		if len(g.Events) != 1 {
			return usage("the payload names no hook_event_name, and gate %q serves several events", gateName)
		}
		for ev := range g.Events {
			event = ev
		}
	}
	if _, ok := g.Events[event]; !ok {
		return usage("gate %q does not serve event %q", gateName, event)
	}

	opts := judgeOpts{endpoint: *endpoint, deadline: time.Duration(*deadlineMS) * time.Millisecond, noUI: *noUI}
	res := evaluateGate(reg, gateName, g, event, payload, opts, start)
	v := res.verdict
	v.LatencyMS = time.Since(start).Milliseconds()

	code := writeJSON(stdout, stderr, v)

	sessionID, _ := payload["session_id"].(string)
	line := judgeLogLine{
		TS:              time.Now().UTC().Format(time.RFC3339Nano),
		Source:          *source,
		Host:            host,
		Gate:            gateName,
		Event:           event,
		Session:         hashSession(sessionID),
		RegistryVersion: v.RegistryVersion,
		CoreVersion:     v.CoreVersion,
		Model:           v.Model,
		ModelResolved:   v.ModelResolved,
		Verdict:         v.Verdict,
		Reasons:         v.Reasons,
		Scores:          v.Scores,
		CodeFlags:       v.CodeFlags,
		Prefilter:       v.Prefilter,
		MaskCounts:      res.masked,
		LatencyMS:       v.LatencyMS,
		LatencyPartsMS:  v.LatencyPartsMS,
		FailMode:        v.FailMode,
		Failed:          v.Failed,
		ErrorClass:      v.ErrorClass,
		NoUI:            *noUI,
	}
	if err := appendJudgeLog(line); err != nil {
		fmt.Fprintf(stderr, "imprint-dev judge: log not written: %v\n", err)
	}
	return code
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
		return exitError
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
		for id := range g.Questions {
			lg.Questions = append(lg.Questions, id)
		}
		sort.Strings(lg.Events)
		sort.Strings(lg.Questions)
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
