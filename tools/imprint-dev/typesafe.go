package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const (
	DefaultEndpoint = "https://api.typesafe.ai/v1/systemone"
	DefaultModel    = "jev-latest"
	DefaultTimeout  = 8 * time.Second
	MaxPayloadBytes = 100 * 1024 // 100 KB

	KnowledgeKeepingQuestionID = "missing_provenance"
	KnowledgeKeepingNotice     = "imprint knowledge-keeping: Dieser Eintrag enthält möglicherweise einen Fakt oder Entscheid ohne Erfassungsdatum (YYYY-MM-DD), Quelle oder Methode. Gemäß knowledge-keeping sollte jede dauerhafte Aufzeichnung Herkunft, Methode und Datum nennen.\n(knowledge-keeping: This entry may record a fact or decision without a recording date (YYYY-MM-DD), source, or acquisition method. Consider adding provenance.)"
)

// --- Masking (analog typesafe-dev ts_common.py) ------------------------------

var (
	bearerBasicRE = regexp.MustCompile(`(?i)(\b(?:bearer|basic)\s+)([^\s"',;]+)`)
	secretKWRE    = regexp.MustCompile(`(?i)((?:api[_-]?key|token|secret|passw(?:or)?d|authorization)[\w.-]*["']?\s*[=:]\s*["']?)([^\s"',;]+)`)

	// emailRE matches email addresses.
	emailRE = regexp.MustCompile(`[\w.+-]+@[\w-]+\.[\w.-]+`)

	// opaqueCandidateRE matches candidates for long opaque strings (24+ base64/hex characters).
	opaqueCandidateRE = regexp.MustCompile(`[A-Za-z0-9_\-+/]{24,}={0,2}`)
)

// MaskCounts records the number of redactions by category.
type MaskCounts struct {
	SecretKW int `json:"secret_kw"`
	Email    int `json:"email"`
	Opaque   int `json:"opaque"`
}

// MaskDetail redacts sensitive patterns in text before transmission.
// Order of execution matches ts_common.mask_detail: secret_kw (bearer/basic + secret keywords), email, opaque.
func MaskDetail(text string) (string, MaskCounts) {
	var counts MaskCounts

	// 1. Bearer / Basic
	text = bearerBasicRE.ReplaceAllStringFunc(text, func(m string) string {
		sub := bearerBasicRE.FindStringSubmatch(m)
		if len(sub) >= 3 {
			if sub[2] == "<redacted>" {
				return m
			}
			counts.SecretKW++
			return sub[1] + "<redacted>"
		}
		counts.SecretKW++
		return "<redacted>"
	})

	// 2. Secret keywords with = or :
	text = secretKWRE.ReplaceAllStringFunc(text, func(m string) string {
		sub := secretKWRE.FindStringSubmatch(m)
		if len(sub) >= 3 {
			val := sub[2]
			if val == "<redacted>" || strings.EqualFold(val, "bearer") || strings.EqualFold(val, "basic") {
				return m
			}
			counts.SecretKW++
			return sub[1] + "<redacted>"
		}
		counts.SecretKW++
		return "<redacted>"
	})

	// 2. email
	text = emailRE.ReplaceAllStringFunc(text, func(m string) string {
		counts.Email++
		return "<email>"
	})

	// 3. opaque: 24+ chars with at least one digit and one letter.
	text = opaqueCandidateRE.ReplaceAllStringFunc(text, func(m string) string {
		hasDigit := false
		hasLetter := false
		for _, r := range m {
			if r >= '0' && r <= '9' {
				hasDigit = true
			} else if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
				hasLetter = true
			}
		}
		if hasDigit && hasLetter {
			counts.Opaque++
			return "<redacted>"
		}
		return m
	})

	return text, counts
}

// Mask redacts sensitive patterns and returns the masked string and total redaction count.
func Mask(text string) (string, int) {
	masked, counts := MaskDetail(text)
	return masked, counts.SecretKW + counts.Email + counts.Opaque
}

// --- Key Lookup --------------------------------------------------------------

var getKeyFn = defaultGetKey

// GetKey searches for the TypeSafe API key:
// 1. Environment variable TYPESAFE_API_KEY
// 2. Fallback to `secret-tool lookup service typesafe key api`
// Returns empty string if no key is found or lookup fails.
func GetKey() string {
	return getKeyFn()
}

func defaultGetKey() string {
	if key := strings.TrimSpace(os.Getenv("TYPESAFE_API_KEY")); key != "" {
		return key
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "secret-tool", "lookup", "service", "typesafe", "key", "api")
	out, err := cmd.Output()
	if err == nil {
		if key := strings.TrimSpace(string(out)); key != "" {
			return key
		}
	}
	return ""
}

// --- Allowlist & Endpoint Verification ---------------------------------------

// isAllowedEndpoint ensures requests only target api.typesafe.ai,
// or local loopback in testing environments.
func isAllowedEndpoint(rawURL string) bool {
	u, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	if u.Scheme == "https" && u.Host == "api.typesafe.ai" && u.Path == "/v1/systemone" {
		return true
	}
	// Loopback allowed for local tests (httptest.Server)
	if (u.Scheme == "http" || u.Scheme == "https") && (u.Hostname() == "127.0.0.1" || u.Hostname() == "localhost") {
		return true
	}
	return false
}

// --- System One Types --------------------------------------------------------

type QuestionType string

const (
	TypeNoul   QuestionType = "noul"
	TypeScore  QuestionType = "score"
	TypeChoice QuestionType = "choice"
)

// Question represents a typed judgment question for TypeSafe System One.
type Question struct {
	Type         QuestionType `json:"type"`
	Instructions string       `json:"instructions"`
	Criteria     any          `json:"criteria,omitempty"`
}

// NewNoulQuestion creates a binary probability (0..1) question.
func NewNoulQuestion(instructions string) Question {
	return Question{
		Type:         TypeNoul,
		Instructions: instructions,
	}
}

// NewScoreQuestion creates a discrete ordinal score question.
func NewScoreQuestion(instructions string, criteria []string) Question {
	return Question{
		Type:         TypeScore,
		Instructions: instructions,
		Criteria:     criteria,
	}
}

// NewChoiceQuestion creates a multi-class choice question.
func NewChoiceQuestion(instructions string, criteria map[string]string) Question {
	return Question{
		Type:         TypeChoice,
		Instructions: instructions,
		Criteria:     criteria,
	}
}

// Request is the payload sent to /v1/systemone.
type Request struct {
	State     string              `json:"state"`
	Model     string              `json:"model"`
	Questions map[string]Question `json:"questions"`
}

// RawAnswer contains the answer fields returned by System One.
type RawAnswer struct {
	Noul          *float64           `json:"noul,omitempty"`
	Choice        *string            `json:"choice,omitempty"`
	Confidence    *float64           `json:"confidence,omitempty"`
	Probabilities map[string]float64 `json:"probabilities,omitempty"`
}

// Response is the structured output from /v1/systemone.
type Response struct {
	Answers map[string]RawAnswer `json:"answers"`
}

// Noul extracts the probability of a noul answer (0..1).
func (r *Response) Noul(qid string) (float64, bool) {
	if r == nil || r.Answers == nil {
		return 0, false
	}
	ans, ok := r.Answers[qid]
	if !ok || ans.Noul == nil {
		return 0, false
	}
	return *ans.Noul, true
}

// ScoreLevel returns the argmax level and confidence for a score answer.
func (r *Response) ScoreLevel(qid string) (int, float64, bool) {
	if r == nil || r.Answers == nil {
		return 0, 0, false
	}
	ans, ok := r.Answers[qid]
	if !ok || len(ans.Probabilities) == 0 {
		return 0, 0, false
	}
	bestLevel := -1
	bestProb := -1.0
	for k, v := range ans.Probabilities {
		lvl, err := strconv.Atoi(k)
		if err != nil {
			continue
		}
		if v > bestProb {
			bestProb = v
			bestLevel = lvl
		}
	}
	if bestLevel == -1 {
		return 0, 0, false
	}
	conf := 0.0
	if ans.Confidence != nil {
		conf = *ans.Confidence
	}
	return bestLevel, conf, true
}

// Choice returns the chosen label, confidence, and distribution.
func (r *Response) Choice(qid string) (string, float64, map[string]float64, bool) {
	if r == nil || r.Answers == nil {
		return "", 0, nil, false
	}
	ans, ok := r.Answers[qid]
	if !ok || ans.Choice == nil {
		return "", 0, nil, false
	}
	conf := 0.0
	if ans.Confidence != nil {
		conf = *ans.Confidence
	}
	return *ans.Choice, conf, ans.Probabilities, true
}

// --- Client ------------------------------------------------------------------

// Client is a Fail-Open client for the TypeSafe System One API.
type Client struct {
	Endpoint   string
	APIKey     string
	Model      string
	Timeout    time.Duration
	HTTPClient *http.Client
}

// NewClient returns a TypeSafe client configured with defaults.
func NewClient(apiKey string) *Client {
	return &Client{
		Endpoint: DefaultEndpoint,
		APIKey:   apiKey,
		Model:    DefaultModel,
		Timeout:  DefaultTimeout,
		HTTPClient: &http.Client{
			Timeout: DefaultTimeout,
		},
	}
}

// Post sends a batched System One request.
// All text in state is capped at MaxPayloadBytes and masked before transmission.
// Fail-Open: returns (nil, err) on any network/JSON error without panicking.
func (c *Client) Post(ctx context.Context, state string, questions map[string]Question) (*Response, error) {
	if c.APIKey == "" {
		return nil, errors.New("typesafe: no api key")
	}
	if !isAllowedEndpoint(c.Endpoint) {
		return nil, fmt.Errorf("typesafe: endpoint %q not allowed", c.Endpoint)
	}

	if len(state) > MaxPayloadBytes {
		state = state[:MaxPayloadBytes]
	}

	maskedState, _ := Mask(state)

	reqBody := Request{
		State:     maskedState,
		Model:     c.Model,
		Questions: questions,
	}

	rawJSON, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("typesafe: marshal request: %w", err)
	}

	timeout := c.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}

	reqCtx := ctx
	var cancel context.CancelFunc
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		reqCtx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}

	httpReq, err := http.NewRequestWithContext(reqCtx, http.MethodPost, c.Endpoint, bytes.NewReader(rawJSON))
	if err != nil {
		return nil, fmt.Errorf("typesafe: new request: %w", err)
	}

	httpReq.Header.Set("Authorization", "Bearer "+c.APIKey)
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("User-Agent", "imprint-dev/0.1")

	client := c.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}

	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("typesafe: do request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodySnippet, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("typesafe: unexpected status %d: %s", resp.StatusCode, strings.TrimSpace(string(bodySnippet)))
	}

	var res Response
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return nil, fmt.Errorf("typesafe: decode response: %w", err)
	}

	return &res, nil
}

// --- Knowledge-Keeping Check (CL-001) ----------------------------------------

// KnowledgeKeepingQuestion returns the Jev primitive evaluating missing provenance.
func KnowledgeKeepingQuestion() Question {
	return Question{
		Type:         TypeNoul,
		Instructions: "Does this text record or register a factual statement, decision, rule, or learning without specifying an explicit calendar date (in YYYY-MM-DD format), a clear source (where it was obtained), or an acquisition method?",
	}
}

// CheckKnowledgeKeeping evaluates whether a text being written to a register or memory
// appears to record facts/decisions without date, source or method.
// Fail-Open: on any error, returns (false, "", err).
func CheckKnowledgeKeeping(ctx context.Context, client *Client, text string) (bool, string, error) {
	if client == nil || client.APIKey == "" {
		return false, "", nil
	}

	if len(text) > MaxPayloadBytes {
		text = text[:MaxPayloadBytes]
	}

	questions := map[string]Question{
		KnowledgeKeepingQuestionID: KnowledgeKeepingQuestion(),
	}

	resp, err := client.Post(ctx, text, questions)
	if err != nil {
		return false, "", err
	}

	prob, ok := resp.Noul(KnowledgeKeepingQuestionID)
	if !ok {
		return false, "", nil
	}

	if prob >= 0.5 {
		return true, KnowledgeKeepingNotice, nil
	}

	return false, "", nil
}

// --- Hook Payload & Subcommand (CL-000, CL-001) ------------------------------

// isMemoryOrRegisterFile returns true if path is a memory, decision, or register file.
// Non-memory files, source code files, and build/factory files always return false.
func isMemoryOrRegisterFile(path string) bool {
	cleanPath := strings.TrimSpace(path)
	if cleanPath == "" || cleanPath == "." {
		return false
	}
	cleanPath = filepath.ToSlash(filepath.Clean(cleanPath))
	lowerPath := strings.ToLower(cleanPath)
	base := filepath.Base(cleanPath)
	lowerBase := strings.ToLower(base)
	ext := strings.ToLower(filepath.Ext(cleanPath))

	// Source code files are never memory/register files.
	codeExts := map[string]bool{
		".go": true, ".py": true, ".sh": true, ".bash": true, ".zsh": true,
		".rs": true, ".c": true, ".h": true, ".cpp": true, ".hpp": true,
		".cc": true, ".hh": true, ".js": true, ".ts": true, ".jsx": true,
		".tsx": true, ".java": true, ".rb": true, ".php": true, ".cs": true,
		".swift": true, ".kt": true,
	}
	if codeExts[ext] {
		return false
	}

	// Factory and build files are never memory/register files.
	if strings.Contains(lowerBase, "factory") || strings.Contains(lowerBase, "build") ||
		lowerBase == "makefile" || lowerBase == "dockerfile" || lowerBase == "containerfile" {
		return false
	}

	// Exact filenames / basenames.
	switch lowerBase {
	case "entscheide.md", "offene-entscheide.md", "register.md", "memory.md", "agents.md":
		return true
	}

	// Fixed folder paths: /memory/, /rules/, /skills/ or starting with memory/, rules/, skills/.
	prefixes := []string{"memory/", "rules/", "skills/"}
	for _, p := range prefixes {
		if strings.HasPrefix(lowerPath, p) || strings.Contains(lowerPath, "/"+p) {
			return true
		}
	}

	return false
}

// extractFileAndContent parses tool input fields across Claude Code, Antigravity, and Codex conventions.
func extractFileAndContent(toolInput map[string]any) (string, string) {
	if toolInput == nil {
		return "", ""
	}

	filePath := ""
	for _, k := range []string{"file_path", "path", "TargetFile", "target_file", "filePath", "file"} {
		if v, ok := toolInput[k]; ok {
			if s, ok := v.(string); ok && s != "" {
				filePath = s
				break
			}
		}
	}

	content := ""
	for _, k := range []string{"new_string", "CodeContent", "ReplacementContent", "code_content", "replacement_content", "content", "text", "replacement"} {
		if v, ok := toolInput[k]; ok {
			if s, ok := v.(string); ok && s != "" {
				content = s
				break
			}
		}
	}

	return filePath, content
}

// HookOutput structures the response sent back to Claude Code or Antigravity.
type HookOutput struct {
	HookSpecificOutput HookSpecificOutput `json:"hookSpecificOutput"`
	InjectSteps        []InjectStep       `json:"injectSteps,omitempty"`
}

type HookSpecificOutput struct {
	HookEventName     string `json:"hookEventName"`
	AdditionalContext string `json:"additionalContext"`
}

type InjectStep struct {
	EphemeralMessage string `json:"ephemeralMessage"`
}

// runHookTypesafeCheck executes the hook-typesafe-check CLI subcommand.
// Never blocks or crashes: exits 0 on all paths.
func runHookTypesafeCheck(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("hook-typesafe-check", flag.ContinueOnError)
	fs.SetOutput(stderr)

	endpoint := fs.String("endpoint", DefaultEndpoint, "TypeSafe API endpoint")
	timeoutSec := fs.Int("timeout", 8, "timeout in seconds (max 8)")

	if err := fs.Parse(args); err != nil {
		return exitOK // fail-open
	}

	// Read hook payload from stdin (up to 2MB).
	rawInput, err := io.ReadAll(io.LimitReader(stdin, 2*1024*1024))
	if err != nil || len(bytes.TrimSpace(rawInput)) == 0 {
		return exitOK
	}

	var payload struct {
		HookEventName string         `json:"hook_event_name"`
		ToolName      string         `json:"tool_name"`
		ToolInput     map[string]any `json:"tool_input"`
		// Raw content fallback if tool_input not structured
		Content string `json:"content"`
	}

	if err := json.Unmarshal(rawInput, &payload); err != nil {
		return exitOK
	}

	filePath, text := extractFileAndContent(payload.ToolInput)
	if text == "" && payload.Content != "" {
		text = payload.Content
	}

	if len(text) > MaxPayloadBytes {
		text = text[:MaxPayloadBytes]
	}

	// Optimization: check path and trivial content FIRST before doing any key search.
	// Non-memory files exit immediately without triggering secret-tool.
	if !isMemoryOrRegisterFile(filePath) || len(strings.TrimSpace(text)) < 15 {
		return exitOK
	}

	// Key lookup only after confirming this is a memory/register file.
	apiKey := GetKey()
	if apiKey == "" {
		return exitOK
	}

	timeout := time.Duration(*timeoutSec) * time.Second
	if timeout <= 0 || timeout > DefaultTimeout {
		timeout = DefaultTimeout
	}

	client := NewClient(apiKey)
	client.Endpoint = *endpoint
	client.Timeout = timeout

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	needsNotice, notice, _ := CheckKnowledgeKeeping(ctx, client, text)
	if !needsNotice || notice == "" {
		return exitOK
	}

	eventName := payload.HookEventName
	if eventName == "" {
		eventName = "PostToolUse"
	}

	out := HookOutput{
		HookSpecificOutput: HookSpecificOutput{
			HookEventName:     eventName,
			AdditionalContext: notice,
		},
		InjectSteps: []InjectStep{
			{EphemeralMessage: notice},
		},
	}

	enc := json.NewEncoder(stdout)
	_ = enc.Encode(out)

	return exitOK
}
