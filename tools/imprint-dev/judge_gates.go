package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"regexp"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

// A gate's builder is its code half: it reads the host payload, runs the
// prefilters, computes the code flags and builds the state. It masks the text
// it caps (mask first, then cap, so a cut never leaves half a secret behind);
// PostState masks every string leaf once more before sending.

type gateCase struct {
	state  any
	flags  map[string]bool
	skip   string // set when code decided and no Jev call is needed
	masked MaskCounts
}

type gateImpl struct {
	prefilters []string // names the registry may list under prefilter
	flags      []string // code flags the builder sets
	stateKeys  []string // state caps the registry must give
	build      func(g *Gate, event string, payload map[string]any) (gateCase, error)
}

var gateImpls = map[string]gateImpl{
	"done": {
		prefilters: []string{"code:stop_hook_active", "code:claim_re"},
		flags:      []string{"stop_hook_active", "claim_re", "local_evidence"},
		stateKeys:  []string{"assistant_message", "tool_calls", "cmd"},
		build:      buildDoneCase,
	},
	"foreign_return": {
		prefilters: []string{"code:min_chars"},
		flags:      []string{},
		stateKeys:  []string{"foreign_text"},
		build:      buildForeignReturnCase,
	},
}

// --- done (port of typesafe-dev bin/ts-done-check, HEAD 8b80f48) ------------------

// claimWordRE is ts-done-check's CLAIM_RE without its \b: Python's \b is
// Unicode-aware, RE2's is ASCII-only, so the word boundary is checked in code
// (claimMatch) with Python's notion of a word character.
var claimWordRE = regexp.MustCompile(`(?i)(?:fertig|erledigt|funktioniert|behoben|grün|bestanden|klappt|läuft|laufen|erfolgreich|verifiziert|done|fixed|passes|passing|works|verified|succeeded)`)

// doneEditTools is ts-done-check's EDIT_TOOLS.
var doneEditTools = map[string]bool{"Edit": true, "Write": true, "NotebookEdit": true, "MultiEdit": true}

// isPyWordRune is Python's \w for str: letters, numbers, underscore.
func isPyWordRune(r rune) bool {
	return r == '_' || unicode.IsLetter(r) || unicode.IsNumber(r)
}

// claimMatch is CLAIM_RE.search(msg) of ts-done-check.
func claimMatch(msg string) bool {
	if strings.ContainsRune(msg, '✔') || strings.ContainsRune(msg, '✅') {
		return true
	}
	pos := 0
	for pos <= len(msg) {
		loc := claimWordRE.FindStringIndex(msg[pos:])
		if loc == nil {
			return false
		}
		s, e := pos+loc[0], pos+loc[1]
		prev, next := utf8.RuneError, utf8.RuneError
		if s > 0 {
			prev, _ = utf8.DecodeLastRuneInString(msg[:s])
		}
		if e < len(msg) {
			next, _ = utf8.DecodeRuneInString(msg[e:])
		}
		if !isPyWordRune(prev) && !isPyWordRune(next) {
			return true
		}
		_, size := utf8.DecodeRuneInString(msg[s:])
		pos = s + size
	}
	return false
}

// doneCall is one entry of tool_calls, keys in ts-done-check's order: tool,
// cmd (Bash only), error (null until a result shows up).
type doneCall struct {
	Tool  any     `json:"tool"`
	Cmd   *string `json:"cmd,omitempty"`
	Error *bool   `json:"error"`
}

type doneState struct {
	AssistantMessage string     `json:"assistant_message"`
	ToolCalls        []doneCall `json:"tool_calls"`
}

// truthy is Python's truth value of a decoded JSON value.
func truthy(v any) bool {
	switch x := v.(type) {
	case nil:
		return false
	case bool:
		return x
	case float64:
		return x != 0
	case string:
		return x != ""
	case []any:
		return len(x) > 0
	case map[string]any:
		return len(x) > 0
	}
	return true
}

// pyStr is Python's str() of a decoded JSON value, for the Bash command.
func pyStr(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case nil:
		return "None"
	case bool:
		if x {
			return "True"
		}
		return "False"
	}
	return fmt.Sprint(v)
}

// eachJSONLine calls fn with every line of a JSONL file that decodes.
func eachJSONLine(path string, fn func(v any)) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	r := bufio.NewReader(f)
	for {
		line, err := r.ReadBytes('\n')
		if len(line) > 0 {
			var v any
			if json.Unmarshal(line, &v) == nil {
				fn(v)
			}
		}
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
	}
}

// doneToolCalls is tool_calls() of ts-done-check: the last maxItems tool calls
// of the transcript, chronological; the Bash command masked, then cut.
func doneToolCalls(path string, maxItems, cmdCap int, cmdKeep string) ([]doneCall, MaskCounts, error) {
	var masked MaskCounts
	var calls []*doneCall
	byID := map[string]*doneCall{}
	idKey := func(v any) string {
		switch x := v.(type) {
		case nil:
			return "none"
		case string:
			return "s:" + x
		}
		return "o:" + fmt.Sprint(v)
	}
	err := eachJSONLine(path, func(v any) {
		e, ok := v.(map[string]any)
		if !ok {
			return
		}
		msg, ok := e["message"].(map[string]any)
		if !ok {
			return
		}
		content, ok := msg["content"].([]any)
		if !ok {
			return
		}
		for _, item := range content {
			b, ok := item.(map[string]any)
			if !ok {
				continue
			}
			switch b["type"] {
			case "tool_use":
				c := &doneCall{Tool: b["name"]}
				if b["name"] == "Bash" {
					inp, _ := b["input"].(map[string]any)
					cmd := ""
					if raw, ok := inp["command"]; ok {
						cmd = pyStr(raw)
					}
					m, n := MaskDetail(cmd)
					masked = addMaskCounts(masked, n)
					m = keepRunes(m, cmdCap, cmdKeep)
					c.Cmd = &m
				}
				calls = append(calls, c)
				byID[idKey(b["id"])] = c
			case "tool_result":
				if c, ok := byID[idKey(b["tool_use_id"])]; ok {
					isErr := truthy(b["is_error"])
					c.Error = &isErr
				}
			}
		}
	})
	if err != nil {
		return nil, masked, err
	}
	if maxItems > 0 && len(calls) > maxItems {
		calls = calls[len(calls)-maxItems:]
	}
	out := make([]doneCall, 0, len(calls))
	for _, c := range calls {
		out = append(out, *c)
	}
	return out, masked, nil
}

// lastAssistantText is last_assistant_text() of ts-done-check.
func lastAssistantText(path string) string {
	txt := ""
	_ = eachJSONLine(path, func(v any) {
		e, ok := v.(map[string]any)
		if !ok || e["type"] != "assistant" {
			return
		}
		msg, ok := e["message"].(map[string]any)
		if !ok {
			return
		}
		content, ok := msg["content"].([]any)
		if !ok {
			return
		}
		var parts []string
		for _, item := range content {
			b, ok := item.(map[string]any)
			if !ok || b["type"] != "text" {
				continue
			}
			s, _ := b["text"].(string)
			parts = append(parts, s)
		}
		if len(parts) > 0 {
			txt = strings.Join(parts, "\n")
		}
	})
	return txt
}

// doneLocalEvidence is local_evidence() of ts-done-check: order is decided in
// code, not by the model: a successful non-edit call after the last edit.
func doneLocalEvidence(calls []doneCall) bool {
	toolName := func(c doneCall) string { s, _ := c.Tool.(string); return s }
	lastEdit := -1
	for i, c := range calls {
		if doneEditTools[toolName(c)] {
			lastEdit = i
		}
	}
	for _, c := range calls[lastEdit+1:] {
		if c.Error != nil && !*c.Error && !doneEditTools[toolName(c)] {
			return true
		}
	}
	return false
}

func buildDoneCase(g *Gate, _ string, p map[string]any) (gateCase, error) {
	c := gateCase{flags: map[string]bool{}}
	if truthy(p["stop_hook_active"]) {
		c.flags["stop_hook_active"] = true
		c.skip = "stop_hook_active"
		return c, nil
	}
	c.flags["stop_hook_active"] = false

	tp, _ := p["transcript_path"].(string)
	msg, _ := p["last_assistant_message"].(string)
	if msg == "" && tp != "" {
		msg = lastAssistantText(tp)
	}
	claim := msg != "" && claimMatch(msg)
	c.flags["claim_re"] = claim
	if !claim {
		c.skip = "no_claim"
		return c, nil
	}

	// ts-done-check: tool_calls(tp) if tp and os.path.exists(tp) else []
	calls := []doneCall{}
	if tp != "" {
		if _, err := os.Stat(tp); err == nil {
			cmd := g.State["cmd"]
			got, n, err := doneToolCalls(tp, g.State["tool_calls"].MaxItems, cmd.CapChars, cmd.Keep)
			c.masked = addMaskCounts(c.masked, n)
			if err != nil {
				return c, err
			}
			calls = got
		}
	}
	c.flags["local_evidence"] = doneLocalEvidence(calls)

	m, n := MaskDetail(msg)
	c.masked = addMaskCounts(c.masked, n)
	am := g.State["assistant_message"]
	c.state = doneState{AssistantMessage: keepRunes(m, am.CapChars, am.Keep), ToolCalls: calls}
	return c, nil
}

// --- foreign_return -------------------------------------------------------------------

// stringLeaves joins the string leaves of a decoded JSON value: object keys in
// sorted order, list items in order.
func stringLeaves(v any) string {
	var parts []string
	var walk func(v any)
	walk = func(v any) {
		switch x := v.(type) {
		case string:
			if strings.TrimSpace(x) != "" {
				parts = append(parts, x)
			}
		case []any:
			for _, item := range x {
				walk(item)
			}
		case map[string]any:
			keys := make([]string, 0, len(x))
			for k := range x {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				walk(x[k])
			}
		}
	}
	walk(v)
	return strings.Join(parts, "\n")
}

// buildForeignReturnCase takes the foreign text: a subagent's return
// (SubagentStop: last_assistant_message) or a tool's output (PostToolUse:
// tool_response, a string or the string leaves of an object). Which tools a hook
// sends here is the hook matcher's business, not the gate's.
func buildForeignReturnCase(g *Gate, event string, p map[string]any) (gateCase, error) {
	c := gateCase{flags: map[string]bool{}}
	var text string
	switch event {
	case "SubagentStop":
		text, _ = p["last_assistant_message"].(string)
	default:
		text = stringLeaves(p["tool_response"])
	}
	ft := g.State["foreign_text"]
	if utf8.RuneCountInString(strings.TrimSpace(text)) < ft.MinChars {
		c.skip = "too_short"
		return c, nil
	}
	m, n := MaskDetail(text)
	c.masked = n
	c.state = map[string]string{"foreign_text": keepRunes(m, ft.CapChars, ft.Keep)}
	return c, nil
}
