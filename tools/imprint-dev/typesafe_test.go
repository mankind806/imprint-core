package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// --- Masking Tests -----------------------------------------------------------

func TestMaskDetail(t *testing.T) {
	input := "API_KEY=abcdef1234567890abcdef123456 and token: secret_token_xyz und kontakt max.mustermann@example.org"
	masked, counts := MaskDetail(input)

	if strings.Contains(masked, "abcdef1234567890abcdef123456") {
		t.Errorf("API key was not redacted: %s", masked)
	}
	if strings.Contains(masked, "secret_token_xyz") {
		t.Errorf("token was not redacted: %s", masked)
	}
	if strings.Contains(masked, "max.mustermann@example.org") {
		t.Errorf("email was not redacted: %s", masked)
	}
	if counts.SecretKW < 1 {
		t.Errorf("expected SecretKW >= 1, got %d", counts.SecretKW)
	}
	if counts.Email != 1 {
		t.Errorf("expected Email == 1, got %d", counts.Email)
	}
}

func TestMaskOpaqueTokens(t *testing.T) {
	// 24+ chars with at least one letter and one digit
	token := "ghp_1234567890abcdefghijklmnopqrstuvwxyz"
	input := "Hier ist ein SHA256-Wert: " + token + " im Text."
	masked, counts := MaskDetail(input)

	if strings.Contains(masked, token) {
		t.Errorf("opaque token was not redacted: %s", masked)
	}
	if counts.Opaque < 1 {
		t.Errorf("expected Opaque >= 1, got %d", counts.Opaque)
	}

	// Plain text without digits or shorter than 24 chars should remain unchanged
	plain := "Dies ist ein ganz normaler Text ohne lange geheimnisvolle Tokens."
	maskedPlain, countPlain := Mask(plain)
	if maskedPlain != plain {
		t.Errorf("plain text should not be modified: %s", maskedPlain)
	}
	if countPlain != 0 {
		t.Errorf("expected 0 redactions for plain text, got %d", countPlain)
	}
}

func TestMaskDetailSecretValuesRegression(t *testing.T) {
	// 10 test cases from CL-110 / typesafe-dev tests/mask-test.py
	cases := []struct {
		input  string
		secret string // secret that must be gone; empty string if input must stay unchanged
	}{
		{"api_key=SECRETVALUE99 mail a@b.de", "SECRETVALUE99"},
		{"password=hunter2 end", "hunter2"},
		{"export API_KEY=abcd1234 x", "abcd1234"},
		{"Authorization: Bearer sk-abc123 def", "sk-abc123"},
		{`{"token": "t0k3n", "x": 1}`, "t0k3n"},
		{`{'secret':'s3cr3t'}`, "s3cr3t"},
		{"db_password: geheim123;", "geheim123"},
		{"mail an max.muster@example.org", "max.muster@example.org"},
		{"pytest -q tests/test_api.py", ""},
		{`git commit -m "fix token refresh"`, ""},
	}

	for _, tc := range cases {
		masked, _ := MaskDetail(tc.input)
		if tc.secret != "" {
			if strings.Contains(masked, tc.secret) {
				t.Errorf("MaskDetail(%q) = %q; secret %q was not redacted", tc.input, masked, tc.secret)
			}
		} else {
			if masked != tc.input {
				t.Errorf("MaskDetail(%q) = %q; expected unchanged text", tc.input, masked)
			}
		}
	}
}

func TestMaxPayloadBytes(t *testing.T) {
	var receivedReq Request
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&receivedReq)
		prob := 0.1
		resp := Response{
			Answers: map[string]RawAnswer{
				"q": {Noul: &prob},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer ts.Close()

	client := NewClient("test-key")
	client.Endpoint = ts.URL
	client.HTTPClient = ts.Client()

	longText := strings.Repeat("x", MaxPayloadBytes+1024)
	_, err := client.Post(context.Background(), longText, map[string]Question{"q": NewNoulQuestion("test")})
	if err != nil {
		t.Fatalf("Post failed: %v", err)
	}
	if len(receivedReq.State) > MaxPayloadBytes {
		t.Errorf("expected receivedReq.State capped at %d bytes, got %d", MaxPayloadBytes, len(receivedReq.State))
	}
}

// --- Allowlist Tests ---------------------------------------------------------

func TestAllowlist(t *testing.T) {
	cases := []struct {
		url     string
		allowed bool
	}{
		{"https://api.typesafe.ai/v1/systemone", true},
		{"http://api.typesafe.ai/v1/systemone", false}, // unencrypted
		{"https://api.typesafe.ai/v2/systemone", false}, // wrong path
		{"https://evil.typesafe.ai/v1/systemone", false},
		{"https://typesafe.ai/v1/systemone", false},
		{"http://127.0.0.1:8080/v1/systemone", true},  // test loopback
		{"http://localhost:3000/systemone", true},     // test loopback
		{"invalid-url", false},
	}

	for _, tc := range cases {
		got := isAllowedEndpoint(tc.url)
		if got != tc.allowed {
			t.Errorf("isAllowedEndpoint(%q) = %v; want %v", tc.url, got, tc.allowed)
		}
	}
}

// --- Key Resolution Tests ----------------------------------------------------

func TestGetKeyFromEnv(t *testing.T) {
	orig := os.Getenv("TYPESAFE_API_KEY")
	defer os.Setenv("TYPESAFE_API_KEY", orig)

	os.Setenv("TYPESAFE_API_KEY", "  test-key-12345  ")
	got := GetKey()
	if got != "test-key-12345" {
		t.Errorf("GetKey() = %q; want %q", got, "test-key-12345")
	}

	os.Unsetenv("TYPESAFE_API_KEY")
	// Without env var, it will try secret-tool or return empty string
	// (in test environment, secret-tool lookup will fail-open and return empty string or whatever is in vault)
}

// --- System One Client Tests with Loopback httptest.Server -------------------

func TestClientPostBatched(t *testing.T) {
	var receivedAuth string
	var receivedUA string
	var receivedReq Request

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedAuth = r.Header.Get("Authorization")
		receivedUA = r.Header.Get("User-Agent")

		_ = json.NewDecoder(r.Body).Decode(&receivedReq)

		noulVal := 0.85
		choiceVal := "opt_b"
		confVal := 0.92
		resp := Response{
			Answers: map[string]RawAnswer{
				"q_noul": {
					Noul: &noulVal,
				},
				"q_score": {
					Confidence: &confVal,
					Probabilities: map[string]float64{
						"0": 0.05,
						"1": 0.85,
						"2": 0.10,
					},
				},
				"q_choice": {
					Choice:     &choiceVal,
					Confidence: &confVal,
					Probabilities: map[string]float64{
						"opt_a": 0.08,
						"opt_b": 0.92,
					},
				},
			},
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer ts.Close()

	client := NewClient("secret-test-key")
	client.Endpoint = ts.URL
	client.HTTPClient = ts.Client()

	questions := map[string]Question{
		"q_noul":   NewNoulQuestion("Is this true?"),
		"q_score":  NewScoreQuestion("Rate compliance", []string{"Level 0", "Level 1", "Level 2"}),
		"q_choice": NewChoiceQuestion("Select category", map[string]string{"opt_a": "A", "opt_b": "B"}),
	}

	ctx := context.Background()
	stateText := "State text containing token: 1234567890abcdef12345678"
	resp, err := client.Post(ctx, stateText, questions)
	if err != nil {
		t.Fatalf("client.Post failed: %v", err)
	}

	if receivedAuth != "Bearer secret-test-key" {
		t.Errorf("Authorization header = %q; want %q", receivedAuth, "Bearer secret-test-key")
	}
	if receivedUA != "imprint-dev/0.1" {
		t.Errorf("User-Agent header = %q; want %q", receivedUA, "imprint-dev/0.1")
	}
	if strings.Contains(receivedReq.State, "1234567890abcdef12345678") {
		t.Errorf("State sent to server was not masked: %s", receivedReq.State)
	}

	// Verify noul helper
	prob, ok := resp.Noul("q_noul")
	if !ok || prob != 0.85 {
		t.Errorf("Noul() = (%f, %v); want (0.85, true)", prob, ok)
	}

	// Verify score helper
	lvl, conf, ok := resp.ScoreLevel("q_score")
	if !ok || lvl != 1 || conf != 0.92 {
		t.Errorf("ScoreLevel() = (%d, %f, %v); want (1, 0.92, true)", lvl, conf, ok)
	}

	// Verify choice helper
	choice, chConf, probs, ok := resp.Choice("q_choice")
	if !ok || choice != "opt_b" || chConf != 0.92 || probs["opt_b"] != 0.92 {
		t.Errorf("Choice() = (%q, %f, %v, %v); want (opt_b, 0.92, ..., true)", choice, chConf, probs, ok)
	}
}

func TestClientFailOpenOnErrors(t *testing.T) {
	// 1. Missing API key
	clientNoKey := NewClient("")
	_, err := clientNoKey.Post(context.Background(), "state", nil)
	if err == nil {
		t.Error("expected error for missing api key")
	}

	// 2. Disallowed endpoint
	clientBadURL := NewClient("some-key")
	clientBadURL.Endpoint = "https://unauthorized.domain.org/v1/systemone"
	_, err = clientBadURL.Post(context.Background(), "state", nil)
	if err == nil {
		t.Error("expected error for disallowed endpoint")
	}

	// 3. HTTP 500 error from server
	ts500 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "internal server error", http.StatusInternalServerError)
	}))
	defer ts500.Close()

	client500 := NewClient("test-key")
	client500.Endpoint = ts500.URL
	client500.HTTPClient = ts500.Client()
	_, err = client500.Post(context.Background(), "state", nil)
	if err == nil {
		t.Error("expected error for HTTP 500")
	}

	// 4. Timeout
	tsSlow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(100 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"answers":{}}`))
	}))
	defer tsSlow.Close()

	clientSlow := NewClient("test-key")
	clientSlow.Endpoint = tsSlow.URL
	clientSlow.Timeout = 10 * time.Millisecond
	clientSlow.HTTPClient = tsSlow.Client()
	_, err = clientSlow.Post(context.Background(), "state", nil)
	if err == nil {
		t.Error("expected timeout error")
	}
}

// --- Knowledge-Keeping Checks (CL-001) ---------------------------------------

func TestCheckKnowledgeKeeping(t *testing.T) {
	// Server responds based on question instructions
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req Request
		_ = json.NewDecoder(r.Body).Decode(&req)

		prob := 0.1
		// If text lacks date (YYYY-MM-DD) or source, simulate high missing provenance prob
		if strings.Contains(req.State, "OHNE_DATUM") {
			prob = 0.88
		}

		resp := Response{
			Answers: map[string]RawAnswer{
				KnowledgeKeepingQuestionID: {
					Noul: &prob,
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer ts.Close()

	client := NewClient("test-key")
	client.Endpoint = ts.URL
	client.HTTPClient = ts.Client()

	ctx := context.Background()

	// Text without date/source
	textMissing := "Entscheidung: OHNE_DATUM Wir schalten den neuen TypeSafe Reranker scharf."
	needsNotice, notice, err := CheckKnowledgeKeeping(ctx, client, textMissing)
	if err != nil {
		t.Fatalf("CheckKnowledgeKeeping error: %v", err)
	}
	if !needsNotice {
		t.Errorf("expected needsNotice = true for text missing provenance")
	}
	if !strings.Contains(notice, "knowledge-keeping") {
		t.Errorf("notice missing knowledge-keeping guidance: %s", notice)
	}

	// Text with date and source
	textComplete := "2026-09-29: Gemäß Nutzerentscheidung CL-105 bleibt CGO_ENABLED=0."
	needsNotice, _, err = CheckKnowledgeKeeping(ctx, client, textComplete)
	if err != nil {
		t.Fatalf("CheckKnowledgeKeeping error: %v", err)
	}
	if needsNotice {
		t.Errorf("expected needsNotice = false for complete provenance text")
	}
}

// --- Path & Field Extraction Tests -------------------------------------------

func TestIsMemoryOrRegisterFile(t *testing.T) {
	cases := []struct {
		path string
		want bool
	}{
		// Empty / invalid
		{"", false},
		{"   ", false},
		{".", false},

		// Source code files - ALWAYS false
		{"main.go", false},
		{"pkg/factory/code.go", false},
		{"factory.go", false},
		{"script.py", false},
		{"deploy.sh", false},
		{"lib.rs", false},
		{"program.c", false},
		{"header.h", false},
		{"app.ts", false},
		{"index.js", false},
		{"memory/script.py", false}, // Code inside memory dir must still be false

		// Factory / build files - ALWAYS false
		{"factory.md", false},
		{"factory_settings.json", false},
		{"build.gradle", false},
		{"Makefile", false},
		{"Dockerfile", false},
		{"Containerfile", false},

		// Exact basenames - true
		{"entscheide.md", true},
		{"docs/entscheide.md", true},
		{"offene-entscheide.md", true},
		{"register.md", true},
		{"docs/register.md", true},
		{"MEMORY.md", true},
		{"AGENTS.md", true},

		// Folder paths - true
		{"memory/notes.md", true},
		{"/var/home/user/memory/notes.md", true},
		{"rules/AGENTS.md", true},
		{"rules/custom.md", true},
		{"skills/knowledge-keeping/SKILL.md", true},
	}

	for _, tc := range cases {
		got := isMemoryOrRegisterFile(tc.path)
		if got != tc.want {
			t.Errorf("isMemoryOrRegisterFile(%q) = %v; want %v", tc.path, got, tc.want)
		}
	}
}

func TestExtractFileAndContent(t *testing.T) {
	// 1. Claude Code Edit tool (file_path, new_string)
	editInput := map[string]any{
		"file_path":  "memory/decisions.md",
		"old_string": "old text",
		"new_string": "new content for memory",
	}
	f1, c1 := extractFileAndContent(editInput)
	if f1 != "memory/decisions.md" || c1 != "new content for memory" {
		t.Errorf("Edit extraction failed: got (%q, %q)", f1, c1)
	}

	// 2. Antigravity write_to_file (TargetFile, CodeContent)
	writeInput := map[string]any{
		"TargetFile":  "entscheide.md",
		"CodeContent": "new file content",
	}
	f2, c2 := extractFileAndContent(writeInput)
	if f2 != "entscheide.md" || c2 != "new file content" {
		t.Errorf("write_to_file extraction failed: got (%q, %q)", f2, c2)
	}

	// 3. Antigravity replace_file_content (TargetFile, ReplacementContent)
	replaceInput := map[string]any{
		"TargetFile":         "register.md",
		"ReplacementContent": "replacement lines",
	}
	f3, c3 := extractFileAndContent(replaceInput)
	if f3 != "register.md" || c3 != "replacement lines" {
		t.Errorf("replace_file_content extraction failed: got (%q, %q)", f3, c3)
	}

	// 4. Nil input
	f4, c4 := extractFileAndContent(nil)
	if f4 != "" || c4 != "" {
		t.Errorf("nil extraction failed: got (%q, %q)", f4, c4)
	}
}

// --- Hook CLI Subcommand Tests -----------------------------------------------

func TestHookTypesafeCheckCLI(t *testing.T) {
	// Mock TypeSafe server
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req Request
		_ = json.NewDecoder(r.Body).Decode(&req)

		prob := 0.1
		if strings.Contains(req.State, "OHNE_DATUM") {
			prob = 0.90
		}

		resp := Response{
			Answers: map[string]RawAnswer{
				KnowledgeKeepingQuestionID: {
					Noul: &prob,
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer ts.Close()

	// Case 1: Without key -> exits 0 immediately with no output
	{
		t.Setenv("TYPESAFE_API_KEY", "")
		origKeyFn := getKeyFn
		getKeyFn = func() string { return "" }

		var stdout, stderr bytes.Buffer
		stdin := strings.NewReader(`{"tool_name":"write_to_file","tool_input":{"TargetFile":"entscheide.md","CodeContent":"OHNE_DATUM Fakt ohne Datum und Herkunft."}}`)
		code := runWithStdin([]string{"hook-typesafe-check", "--endpoint", ts.URL}, stdin, &stdout, &stderr)
		getKeyFn = origKeyFn

		if code != exitOK {
			t.Fatalf("code = %d; want exitOK", code)
		}
		if stdout.Len() != 0 {
			t.Errorf("expected empty stdout without key, got %q", stdout.String())
		}
	}

	// Case 2: Non-memory file -> exits 0 with no output
	{
		t.Setenv("TYPESAFE_API_KEY", "test-key")
		var stdout, stderr bytes.Buffer
		stdin := strings.NewReader(`{"tool_name":"write_to_file","tool_input":{"TargetFile":"main.go","CodeContent":"OHNE_DATUM package main"}}`)
		code := runWithStdin([]string{"hook-typesafe-check", "--endpoint", ts.URL}, stdin, &stdout, &stderr)
		if code != exitOK {
			t.Fatalf("code = %d; want exitOK", code)
		}
		if stdout.Len() != 0 {
			t.Errorf("expected empty stdout for non-memory file, got %q", stdout.String())
		}
	}

	// Case 2b: Path exclusion does NOT invoke key lookup (factory.go -> kein Key-Lookup)
	{
		origKeyFn := getKeyFn
		getKeyFn = func() string {
			t.Fatal("key lookup must not be invoked for excluded/non-memory files like factory.go")
			return "fail"
		}

		var stdout, stderr bytes.Buffer
		stdin := strings.NewReader(`{"tool_name":"write_to_file","tool_input":{"TargetFile":"factory.go","CodeContent":"OHNE_DATUM package factory"}}`)
		code := runWithStdin([]string{"hook-typesafe-check", "--endpoint", ts.URL}, stdin, &stdout, &stderr)
		getKeyFn = origKeyFn

		if code != exitOK {
			t.Fatalf("code = %d; want exitOK", code)
		}
		if stdout.Len() != 0 {
			t.Errorf("expected empty stdout for factory.go, got %q", stdout.String())
		}
	}

	// Case 3: Memory file with missing provenance via write_to_file -> exits 0 with additionalContext JSON
	{
		t.Setenv("TYPESAFE_API_KEY", "test-key")
		var stdout, stderr bytes.Buffer
		stdin := strings.NewReader(`{"hook_event_name":"PostToolUse","tool_name":"write_to_file","tool_input":{"TargetFile":"docs/entscheide.md","CodeContent":"OHNE_DATUM Neuer Beschluss ohne Herkunft."}}`)
		code := runWithStdin([]string{"hook-typesafe-check", "--endpoint", ts.URL}, stdin, &stdout, &stderr)
		if code != exitOK {
			t.Fatalf("code = %d; want exitOK", code)
		}
		if stdout.Len() == 0 {
			t.Fatal("expected hook output, got empty stdout")
		}

		var out HookOutput
		if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
			t.Fatalf("failed to decode hook output JSON: %v", err)
		}
		if out.HookSpecificOutput.HookEventName != "PostToolUse" {
			t.Errorf("HookEventName = %q; want PostToolUse", out.HookSpecificOutput.HookEventName)
		}
		if !strings.Contains(out.HookSpecificOutput.AdditionalContext, "knowledge-keeping") {
			t.Errorf("AdditionalContext does not contain knowledge-keeping notice: %s", out.HookSpecificOutput.AdditionalContext)
		}
		if len(out.InjectSteps) == 0 || !strings.Contains(out.InjectSteps[0].EphemeralMessage, "knowledge-keeping") {
			t.Errorf("InjectSteps missing ephemeralMessage")
		}
	}

	// Case 3b: Memory file with missing provenance via Claude Code Edit (file_path, new_string)
	{
		t.Setenv("TYPESAFE_API_KEY", "test-key")
		var stdout, stderr bytes.Buffer
		stdin := strings.NewReader(`{"hook_event_name":"PostToolUse","tool_name":"Edit","tool_input":{"file_path":"rules/AGENTS.md","old_string":"foo","new_string":"OHNE_DATUM Neuer Leitsatz ohne Erfassungsdatum."}}`)
		code := runWithStdin([]string{"hook-typesafe-check", "--endpoint", ts.URL}, stdin, &stdout, &stderr)
		if code != exitOK {
			t.Fatalf("code = %d; want exitOK", code)
		}
		if stdout.Len() == 0 {
			t.Fatal("expected hook output for Edit with new_string, got empty stdout")
		}

		var out HookOutput
		if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
			t.Fatalf("failed to decode hook output JSON: %v", err)
		}
		if !strings.Contains(out.HookSpecificOutput.AdditionalContext, "knowledge-keeping") {
			t.Errorf("AdditionalContext does not contain knowledge-keeping notice: %s", out.HookSpecificOutput.AdditionalContext)
		}
	}

	// Case 4: Memory file with valid provenance -> exits 0 with no output
	{
		t.Setenv("TYPESAFE_API_KEY", "test-key")
		var stdout, stderr bytes.Buffer
		stdin := strings.NewReader(`{"hook_event_name":"PostToolUse","tool_name":"write_to_file","tool_input":{"TargetFile":"docs/entscheide.md","CodeContent":"2026-09-29: Gemäß Beschluss CL-105..."}}`)
		code := runWithStdin([]string{"hook-typesafe-check", "--endpoint", ts.URL}, stdin, &stdout, &stderr)
		if code != exitOK {
			t.Fatalf("code = %d; want exitOK", code)
		}
		if stdout.Len() != 0 {
			t.Errorf("expected empty stdout for compliant text, got %q", stdout.String())
		}
	}

	// Case 5: Malformed JSON on stdin -> fail-open (exit 0, no output)
	{
		t.Setenv("TYPESAFE_API_KEY", "test-key")
		var stdout, stderr bytes.Buffer
		stdin := strings.NewReader(`{not valid json`)
		code := runWithStdin([]string{"hook-typesafe-check", "--endpoint", ts.URL}, stdin, &stdout, &stderr)
		if code != exitOK {
			t.Fatalf("code = %d; want exitOK", code)
		}
		if stdout.Len() != 0 {
			t.Errorf("expected empty stdout on malformed json, got %q", stdout.String())
		}
	}
}

// --- Hook Wrapper Script Tests -----------------------------------------------

func TestHookWrapperScript(t *testing.T) {
	scriptPath := filepath.Join("..", "..", "hooks", "typesafe-check.sh")
	info, err := os.Stat(scriptPath)
	if err != nil {
		t.Fatalf("hooks/typesafe-check.sh missing: %v", err)
	}

	// Check executable permission
	if info.Mode()&0111 == 0 {
		t.Errorf("hooks/typesafe-check.sh is not executable: mode %v", info.Mode())
	}

	// Check script contents
	data, err := os.ReadFile(scriptPath)
	if err != nil {
		t.Fatalf("failed to read hooks/typesafe-check.sh: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "command -v imprint-dev >/dev/null 2>&1 || exit 0") {
		t.Errorf("missing command -v check in script: %s", content)
	}
	if !strings.Contains(content, "exec imprint-dev hook-typesafe-check") {
		t.Errorf("missing exec imprint-dev hook-typesafe-check in script: %s", content)
	}

	// Check hooks.json registration
	hooksJSONPath := filepath.Join("..", "..", "hooks", "hooks.json")
	hooksData, err := os.ReadFile(hooksJSONPath)
	if err != nil {
		t.Fatalf("failed to read hooks/hooks.json: %v", err)
	}
	if !strings.Contains(string(hooksData), "hooks/typesafe-check.sh") {
		t.Errorf("hooks.json does not reference hooks/typesafe-check.sh: %s", string(hooksData))
	}
}
