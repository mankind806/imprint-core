package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"sort"
	"strings"
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
	Masked        MaskCounts
	ResponseKeys  []string // top-level key names of the response body, never values
	ModelResolved string   // a top-level "model" string in the response, if the API sends one
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
func (c *Client) PostState(ctx context.Context, state any, questions map[string]Question) (*Response, StateCall, error) {
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
	maskedState, stateCounts, err := maskJSONLeaves(rawState)
	if err != nil {
		return nil, call, &CallError{errClassInternal, fmt.Errorf("mask state: %w", err)}
	}
	rawQuestions, err := json.Marshal(questions)
	if err != nil {
		return nil, call, &CallError{errClassInternal, fmt.Errorf("marshal questions: %w", err)}
	}
	maskedQuestions, questionCounts, err := maskJSONLeaves(rawQuestions)
	if err != nil {
		return nil, call, &CallError{errClassInternal, fmt.Errorf("mask questions: %w", err)}
	}
	call.Masked = addMaskCounts(stateCounts, questionCounts)

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
				s, c = MaskDetail(v)
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

// keepRunes caps s to n runes, keeping the head or the tail ("tail"), as
// Python's s[:n] and s[-n:] do on a str. n <= 0 keeps everything.
func keepRunes(s string, n int, keep string) string {
	if n <= 0 {
		return s
	}
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	if strings.EqualFold(keep, "tail") {
		return string(r[len(r)-n:])
	}
	return string(r[:n])
}
