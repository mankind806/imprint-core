package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

// This file adds the structured-state call that `imprint-dev judge` uses next to
// Client.Post, which stays as it is for hook-typesafe-check and
// hook-skill-suggestion. The differences: the state is a JSON object, not one
// string; every string leaf of the state and of the questions is masked (as
// ts_common._mask_tree does), map keys stay; and every failure carries a class,
// because judge's fail mode is decided per class.

// Error classes of a failed System One call. jev-kern.md section 3 names
// no_key, timeout, http_status, parse and missing_answer; network, endpoint and
// internal are added here, so a refused connection is not reported as a timeout.
const (
	errClassNoKey         = "no_key"
	errClassTimeout       = "timeout"
	errClassNetwork       = "network"
	errClassHTTPStatus    = "http_status"
	errClassParse         = "parse"
	errClassMissingAnswer = "missing_answer"
	errClassEndpoint      = "endpoint"
	errClassInternal      = "internal"
)

// CallError is a failed call with its class.
type CallError struct {
	Class string
	Err   error
}

func (e *CallError) Error() string { return "typesafe: " + e.Class + ": " + e.Err.Error() }
func (e *CallError) Unwrap() error { return e.Err }

// errorClass returns the class of err; an error that is not a *CallError is internal.
func errorClass(err error) string {
	var ce *CallError
	if errors.As(err, &ce) {
		return ce.Class
	}
	return errClassInternal
}

// StateCall is what PostState reports besides the answers. It never holds text
// that was sent.
type StateCall struct {
	Masked        MaskCounts // placeholders in the request body, by kind: only what was sent
	ResponseKeys  []string   // top-level key names of the response body, never values
	ModelResolved string     // a top-level "model" string in the response, if the API sends one
}

// maxResponseBytes caps how much of a response body is read.
const maxResponseBytes = 1 << 20

// noRedirectHTTPClient never follows a redirect: a 3xx comes back as the
// response and fails as http_status, like ts_common's _NoRedirect opener.
func noRedirectHTTPClient() *http.Client {
	return &http.Client{
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
}

// PostState sends one batched System One request with a structured state.
// The deadline comes from ctx (judge sets one per event). Every string leaf of
// state and questions is masked before it leaves the process.
func (c *Client) PostState(ctx context.Context, state any, questions QuestionSet) (*Response, StateCall, error) {
	var call StateCall
	if c.APIKey == "" {
		return nil, call, &CallError{errClassNoKey, errors.New("no api key")}
	}
	if !isAllowedEndpoint(c.Endpoint) {
		return nil, call, &CallError{errClassEndpoint, fmt.Errorf("endpoint %q not allowed", c.Endpoint)}
	}

	rawState, err := json.Marshal(state)
	if err != nil {
		return nil, call, &CallError{errClassInternal, fmt.Errorf("marshal state: %w", err)}
	}
	type maskedLeaves struct {
		b   []byte
		err error
	}
	leavesDone := make(chan maskedLeaves, 1)
	go func() {
		b, _, err := maskJSONLeaves(rawState)
		leavesDone <- maskedLeaves{b, err}
	}()
	var maskedState []byte
	select {
	case ml := <-leavesDone:
		if ml.err != nil {
			return nil, call, &CallError{errClassInternal, fmt.Errorf("mask state: %w", ml.err)}
		}
		maskedState = ml.b
	case <-ctx.Done():
		return nil, call, &CallError{errClassTimeout, fmt.Errorf("mask state: %w", ctx.Err())}
	}
	rawQuestions, err := json.Marshal(questions)
	if err != nil {
		return nil, call, &CallError{errClassInternal, fmt.Errorf("marshal questions: %w", err)}
	}
	maskedQuestions, _, err := maskJSONLeaves(rawQuestions)
	if err != nil {
		return nil, call, &CallError{errClassInternal, fmt.Errorf("mask questions: %w", err)}
	}
	call.Masked = addMaskCounts(countPlaceholders(string(maskedState)), countPlaceholders(string(maskedQuestions)))

	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false) // keep <email>, <redacted> readable on the wire
	if err := enc.Encode(struct {
		State     json.RawMessage `json:"state"`
		Model     string          `json:"model"`
		Questions json.RawMessage `json:"questions"`
	}{maskedState, c.Model, maskedQuestions}); err != nil {
		return nil, call, &CallError{errClassInternal, fmt.Errorf("marshal request: %w", err)}
	}
	body := bytes.TrimSpace(buf.Bytes())

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.Endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, call, &CallError{errClassInternal, fmt.Errorf("new request: %w", err)}
	}
	req.Header.Set("Authorization", "Bearer "+c.APIKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "imprint-dev/0.1")

	client := c.HTTPClient
	if client == nil {
		client = noRedirectHTTPClient()
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, call, &CallError{classifyTransportError(ctx, err), err}
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 512))
		return nil, call, &CallError{errClassHTTPStatus, fmt.Errorf("unexpected status %d", resp.StatusCode)}
	}

	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return nil, call, &CallError{classifyTransportError(ctx, err), fmt.Errorf("read response: %w", err)}
	}
	var top map[string]json.RawMessage
	if err := json.Unmarshal(raw, &top); err != nil {
		return nil, call, &CallError{errClassParse, fmt.Errorf("decode response: %w", err)}
	}
	for k := range top {
		call.ResponseKeys = append(call.ResponseKeys, k)
	}
	sort.Strings(call.ResponseKeys)
	if m, ok := top["model"]; ok {
		var s string
		if json.Unmarshal(m, &s) == nil {
			call.ModelResolved = s
		}
	}
	answers, ok := top["answers"]
	if !ok {
		return nil, call, &CallError{errClassParse, errors.New("response has no answers")}
	}
	var res Response
	if err := json.Unmarshal(answers, &res.Answers); err != nil || res.Answers == nil {
		return nil, call, &CallError{errClassParse, errors.New("answers is not an object")}
	}
	return &res, call, nil
}

// classifyTransportError tells a timeout (the deadline ran out, or a network
// timeout) from any other failure to reach the endpoint.
func classifyTransportError(ctx context.Context, err error) string {
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return errClassTimeout
	}
	var ne net.Error
	if errors.As(err, &ne) && ne.Timeout() {
		return errClassTimeout
	}
	return errClassNetwork
}

func addMaskCounts(a, b MaskCounts) MaskCounts {
	return MaskCounts{
		SecretKW: a.SecretKW + b.SecretKW,
		Email:    a.Email + b.Email,
		Opaque:   a.Opaque + b.Opaque,
		Address:  a.Address + b.Address,
		Name:     a.Name + b.Name,
		IBAN:     a.IBAN + b.IBAN,
		Phone:    a.Phone + b.Phone,
	}
}

func (m MaskCounts) total() int {
	return m.SecretKW + m.Email + m.Opaque + m.Address + m.Name + m.IBAN + m.Phone
}

// maskJSONLeaves masks every string value in a JSON document with MaskDetail
// and leaves object keys, numbers, booleans, null and the order of keys as they
// are. It is ts_common._mask_tree over the encoded document; working on the
// token stream keeps key order, which a round trip through map[string]any
// would sort.
func maskJSONLeaves(raw []byte) ([]byte, MaskCounts, error) {
	var total MaskCounts
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var out bytes.Buffer
	enc := json.NewEncoder(&out)
	enc.SetEscapeHTML(false)

	type frame struct {
		obj       bool
		n         int  // members written
		expectKey bool // object: the next string is a key
	}
	var stack []frame
	sep := func() {
		if len(stack) == 0 {
			return
		}
		top := &stack[len(stack)-1]
		switch {
		case top.obj && !top.expectKey:
			out.WriteByte(':')
		case top.n > 0:
			out.WriteByte(',')
		}
	}
	done := func() {
		if len(stack) == 0 {
			return
		}
		top := &stack[len(stack)-1]
		if top.obj && top.expectKey {
			top.expectKey = false
			return
		}
		top.n++
		if top.obj {
			top.expectKey = true
		}
	}
	write := func(v any) error {
		if err := enc.Encode(v); err != nil {
			return err
		}
		out.Truncate(out.Len() - 1) // Encode appends a newline
		return nil
	}

	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, total, err
		}
		switch v := tok.(type) {
		case json.Delim:
			switch v {
			case '{', '[':
				sep()
				out.WriteByte(byte(v))
				stack = append(stack, frame{obj: v == '{', expectKey: v == '{'})
			case '}', ']':
				out.WriteByte(byte(v))
				stack = stack[:len(stack)-1]
				done()
			}
		case string:
			isKey := len(stack) > 0 && stack[len(stack)-1].obj && stack[len(stack)-1].expectKey
			sep()
			s := v
			if !isKey {
				var c MaskCounts
				s, c = maskDetailFn(v)
				total = addMaskCounts(total, c)
			}
			if err := write(s); err != nil {
				return nil, total, err
			}
			done()
		default:
			sep()
			if err := write(v); err != nil {
				return nil, total, err
			}
			done()
		}
	}
	return bytes.TrimSpace(out.Bytes()), total, nil
}

// QuestionSet is a gate's questions in registry order, and is sent in that
// order (ts-done-check sends claim, then backed). A Go map would sort them.
type QuestionSet struct {
	IDs  []string
	ByID map[string]Question
}

// UnmarshalJSON reads a JSON object of questions, keeping key order; a
// question with an unknown field or an ID given twice is an error.
func (q *QuestionSet) UnmarshalJSON(b []byte) error {
	dec := json.NewDecoder(bytes.NewReader(b))
	tok, err := dec.Token()
	if err != nil {
		return err
	}
	if d, ok := tok.(json.Delim); !ok || d != '{' {
		return errors.New("questions is not an object")
	}
	q.IDs, q.ByID = nil, map[string]Question{}
	for dec.More() {
		t, err := dec.Token()
		if err != nil {
			return err
		}
		id, _ := t.(string)
		if _, dup := q.ByID[id]; dup {
			return fmt.Errorf("question %q given twice", id)
		}
		var raw json.RawMessage
		if err := dec.Decode(&raw); err != nil {
			return err
		}
		var one Question
		d := json.NewDecoder(bytes.NewReader(raw))
		d.DisallowUnknownFields()
		if err := d.Decode(&one); err != nil {
			return fmt.Errorf("question %q: %w", id, err)
		}
		q.IDs = append(q.IDs, id)
		q.ByID[id] = one
	}
	_, err = dec.Token()
	return err
}

// MarshalJSON writes the questions as one object in registry order.
func (q QuestionSet) MarshalJSON() ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteByte('{')
	for i, id := range q.IDs {
		if i > 0 {
			buf.WriteByte(',')
		}
		k, err := json.Marshal(id)
		if err != nil {
			return nil, err
		}
		v, err := json.Marshal(q.ByID[id])
		if err != nil {
			return nil, err
		}
		buf.Write(k)
		buf.WriteByte(':')
		buf.Write(v)
	}
	buf.WriteByte('}')
	return buf.Bytes(), nil
}

// redactedKWRE finds a <redacted> that MaskDetail's keyword or bearer rule
// wrote (the keyword and separator stay in front of it), also inside a JSON
// string where quotes are escaped.
var redactedKWRE = regexp.MustCompile(`(?i)(?:(?:bearer|basic)\s+|(?:api[_-]?key|token|secret|passw(?:or)?d|pass(?:phrase|wort)?|pwd|credential|private[_-]?key|access[_-]?key|auth(?:orization)?)[\w.-]*(?:\\?["'])?\s*[=:]\s*(?:\\?["'])?)<redacted>`)

// countPlaceholders counts MaskDetail's placeholders in text, by kind: what
// was masked and is actually in the text, not what a cut dropped. A
// <redacted> after a keyword or bearer counts as secret_kw, any other as opaque.
func countPlaceholders(text string) MaskCounts {
	c := MaskCounts{
		Email:   strings.Count(text, "<email>"),
		IBAN:    strings.Count(text, "<iban>"),
		Phone:   strings.Count(text, "<phone>"),
		Address: strings.Count(text, "<address>"),
		Name:    strings.Count(text, "<name>"),
	}
	red := strings.Count(text, "<redacted>")
	c.SecretKW = len(redactedKWRE.FindAllStringIndex(text, -1))
	if red > c.SecretKW {
		c.Opaque = red - c.SecretKW
	}
	return c
}

// maskPlaceholders are the placeholders MaskDetail writes. A cut never splits
// one of them; no other "<" is ever trimmed.
var maskPlaceholders = []string{"<redacted>", "<address>", "<email>", "<phone>", "<iban>", "<name>"}

// maxPlaceholderLen is the length of the longest placeholder.
const maxPlaceholderLen = len("<redacted>")

// headTailSeparator stands between head and tail when both are kept.
const headTailSeparator = "\n[…]\n"

// errTooLarge: the text a gate would mask is larger than its mask budget.
var errTooLarge = errors.New("text too large to mask within the deadline")

// Worst-case masking cost per byte, measured 2026-10-02 (go1.27.1,
// GOMAXPROCS=2, this machine; TestMaskProfile, docs/judge.md), rounded up:
//   - all steps on the fast path: 127 ns (dense street addresses), up to 232
//     under load: 200;
//   - a text with a rune whose case fold has another length (Kelvin sign, long
//     s, capital sharp s, ...) or invalid UTF-8 sends some steps back to the
//     old regex: up to 705 ns: 900;
//   - per name pattern: 0.9 on the fold-canonical search (Greek names on
//     Greek text): 1.5; 15.7 when such a text is scanned name by name: 20;
//     up to 21 with the regexps (invalid UTF-8): 25.
const (
	maskWorstNsPerByte    = 200.0
	maskFallbackNsPerByte = 900.0
	nameFoldNsPerByte     = 1.5
	nameScanNsPerByte     = 20.0
	nameRegexNsPerByte    = 25.0
)

// maskNsPerByte is the worst-case masking cost per byte of text, with the
// given number of name patterns, on the path the text will take.
func maskNsPerByte(text string, patterns int) float64 {
	p := float64(patterns)
	switch {
	case !utf8.ValidString(text):
		return maskFallbackNsPerByte + nameRegexNsPerByte*p
	case hasFoldLengthRune(text):
		return maskFallbackNsPerByte + nameScanNsPerByte*p
	default:
		return maskWorstNsPerByte + nameFoldNsPerByte*p
	}
}

// maskBudgetBytes is how many bytes of plain text (the fast path) one call
// may mask: half the time left until the deadline at the worst-case rate,
// capped at the stdin limit. Without a deadline it is the stdin limit.
func maskBudgetBytes(ctx context.Context) int {
	dl, ok := ctx.Deadline()
	if !ok {
		return maxJudgeInput
	}
	perByte := maskWorstNsPerByte + nameFoldNsPerByte*float64(len(loadNameSet().res))
	n := float64(time.Until(dl).Nanoseconds()) / 2 / perByte
	if n <= 0 {
		return 0
	}
	return int(min(n, float64(maxJudgeInput)))
}

// maskDetailFn is MaskDetail; a test puts a slow one in its place to see
// that masking stops waiting at the deadline.
var maskDetailFn = MaskDetail

// maskBudget counts down the masking time (ns, worst case) a call may still
// spend: half the time left until the deadline.
type maskBudget struct{ leftNs float64 }

func newMaskBudget(ctx context.Context) *maskBudget {
	dl, ok := ctx.Deadline()
	if !ok {
		return &maskBudget{leftNs: math.Inf(1)}
	}
	return &maskBudget{leftNs: float64(time.Until(dl).Nanoseconds()) / 2}
}

// maskWithin runs MaskDetail on text and waits for it at most until ctx's
// deadline; then it returns ctx's error and leaves the masking goroutine to
// run out (judge is one process per call and exits right after its verdict).
func maskWithin(ctx context.Context, text string) (string, error) {
	done := make(chan string, 1)
	go func() {
		m, _ := maskDetailFn(text)
		done <- m
	}()
	select {
	case m := <-done:
		return m, nil
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

// maskCapped masks the whole text, then keeps at most capChars runes of the
// masked text (cutMasked). It refuses a text whose worst-case masking time
// is over the budget (errTooLarge), does not start once ctx's deadline has
// passed, and stops waiting when it passes (maskWithin).
func (b *maskBudget) maskCapped(ctx context.Context, text string, capChars int, keep string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	cost := float64(len(text)) * maskNsPerByte(text, len(loadNameSet().res))
	if cost > b.leftNs {
		return "", errTooLarge
	}
	b.leftNs -= cost
	m, err := maskWithin(ctx, text)
	if err != nil {
		return "", err
	}
	return cutMasked(m, capChars, keep), nil
}

// headRunes is the first n runes of s (all of s if it is shorter), without
// converting s as a whole; n <= 0 keeps everything.
func headRunes(s string, n int) string {
	if n <= 0 {
		return s
	}
	i := 0
	for k := 0; k < n; k++ {
		if i >= len(s) {
			return s
		}
		_, size := utf8.DecodeRuneInString(s[i:])
		i += size
	}
	return s[:i]
}

// tailRunes is the last n runes of s (all of s if it is shorter); n <= 0
// keeps everything.
func tailRunes(s string, n int) string {
	if n <= 0 {
		return s
	}
	j := len(s)
	for k := 0; k < n; k++ {
		if j <= 0 {
			return s
		}
		_, size := utf8.DecodeLastRuneInString(s[:j])
		j -= size
	}
	return s[j:]
}

// placeholderAcross returns the start and end (byte offsets in m) of a
// placeholder that the cut at byte offset at runs through, or ok=false. Such a
// placeholder starts at the last "<" before at, since none contains a "<".
func placeholderAcross(m string, at int) (start, end int, ok bool) {
	for i := at - 1; i >= 0 && i > at-maxPlaceholderLen; i-- {
		if m[i] != '<' {
			continue
		}
		for _, ph := range maskPlaceholders {
			if strings.HasPrefix(m[i:], ph) && i+len(ph) > at {
				return i, i + len(ph), true
			}
		}
		return 0, 0, false
	}
	return 0, 0, false
}

// cutMasked keeps at most capChars runes of the masked text m: its head, its
// tail, or both (head_tail, joined by headTailSeparator, which counts toward
// the cap). A cut that would split a placeholder moves to its edge, so the
// result is always a prefix, a suffix, or prefix + separator + suffix of m.
// capChars <= 0 keeps everything.
func cutMasked(m string, capChars int, keep string) string {
	if capChars <= 0 || len(headRunes(m, capChars)) == len(m) {
		return m
	}
	headEnd := func(n int) int {
		end := len(headRunes(m, n))
		if s, _, ok := placeholderAcross(m, end); ok {
			end = s
		}
		return end
	}
	tailStart := func(n int) int {
		start := len(m) - len(tailRunes(m, n))
		if _, e, ok := placeholderAcross(m, start); ok {
			start = e
		}
		return start
	}
	switch keep {
	case "tail":
		return m[tailStart(capChars):]
	case "head_tail":
		sep := utf8.RuneCountInString(headTailSeparator)
		h := (capChars - sep) / 2
		return m[:headEnd(h)] + headTailSeparator + m[tailStart(capChars-sep-h):]
	default:
		return m[:headEnd(capChars)]
	}
}
