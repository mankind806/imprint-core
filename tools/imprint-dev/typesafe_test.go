package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
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

func TestMaskAddresses(t *testing.T) {
	// 1. Street + House number
	streetCases := []struct {
		input       string
		wantMasked  string
		wantAddress int
	}{
		{"Musterstraße 12", "<address>", 1},
		{"Hauptstr. 4b", "<address>", 1},
		{"Am Markt 1", "<address>", 1},
		{"Goethestr. 10", "<address>", 1},
		{"Kurfürstendamm 100", "<address>", 1},
		{"Kastanienallee 21", "<address>", 1},
		{"Schulweg 5", "<address>", 1},
		{"An der Alster 5", "<address>", 1},
		{"Auf dem Hügel 9", "<address>", 1},
		{"Im Winkel 3", "<address>", 1},
		{"In der Aue 12", "<address>", 1},
		{"Zum Bahnhof 4", "<address>", 1},
		{"Vor dem Steintor 25", "<address>", 1},
		{"Musterstraße 12-14", "<address>", 1},
		{"Musterstr. 12a", "<address>", 1},
		{"Berliner Straße 42", "<address>", 1},
		{"Hier wohnt jemand in der Hauptstr. 4b im Erdgeschoss.", "Hier wohnt jemand in der <address> im Erdgeschoss.", 1},
	}

	for _, tc := range streetCases {
		masked, counts := MaskDetail(tc.input)
		if masked != tc.wantMasked {
			t.Errorf("MaskDetail(%q) = %q; want %q", tc.input, masked, tc.wantMasked)
		}
		if counts.Address != tc.wantAddress {
			t.Errorf("MaskDetail(%q) counts.Address = %d; want %d", tc.input, counts.Address, tc.wantAddress)
		}
	}

	// 2. Postal code + City
	plzCases := []struct {
		input       string
		wantMasked  string
		wantAddress int
	}{
		{"10115 Berlin", "<address>", 1},
		{"80331 München", "<address>", 1},
		{"60311 Frankfurt am Main", "<address>", 1},
		{"70173 Stuttgart", "<address>", 1},
		{"10115 Berlin-Mitte", "<address>", 1},
		{"PLZ ist 10115 Berlin im Brief.", "PLZ ist <address> im Brief.", 1},
	}

	for _, tc := range plzCases {
		masked, counts := MaskDetail(tc.input)
		if masked != tc.wantMasked {
			t.Errorf("MaskDetail(%q) = %q; want %q", tc.input, masked, tc.wantMasked)
		}
		if counts.Address != tc.wantAddress {
			t.Errorf("MaskDetail(%q) counts.Address = %d; want %d", tc.input, counts.Address, tc.wantAddress)
		}
	}

	// 3. Combined street and PLZ
	combined := "Musterstraße 12, 10115 Berlin"
	masked, counts := MaskDetail(combined)
	if masked != "<address>, <address>" {
		t.Errorf("MaskDetail(%q) = %q; want %q", combined, masked, "<address>, <address>")
	}
	if counts.Address != 2 {
		t.Errorf("MaskDetail(%q) counts.Address = %d; want 2", combined, counts.Address)
	}

	// 4. Non-address text must not trigger false positives
	negativeCases := []string{
		`git commit -m "fix issue 12"`,
		"go test -count=1",
		"version 10115",
		"Am 12. Mai",
		"Berlin ist eine Stadt",
	}

	for _, input := range negativeCases {
		masked, counts := MaskDetail(input)
		if masked != input {
			t.Errorf("MaskDetail(%q) = %q; false positive address match", input, masked)
		}
		if counts.Address != 0 {
			t.Errorf("MaskDetail(%q) counts.Address = %d; want 0", input, counts.Address)
		}
	}
}

func TestMaskNames(t *testing.T) {
	// Create temporary names file
	tmpDir := t.TempDir()
	namesFile := filepath.Join(tmpDir, "names.txt")
	content := `# Test team members
Max Mustermann # Lead
Erika Musterfrau
Hans Peter von Schmidt
`
	if err := os.WriteFile(namesFile, []byte(content), 0o644); err != nil {
		t.Fatalf("failed to write names file: %v", err)
	}

	t.Setenv("TYPESAFE_NAMES_FILE", namesFile)

	cases := []struct {
		input     string
		want      string
		wantCount int
	}{
		{"Hallo Max Mustermann", "Hallo <name>", 1},
		{"Erika sprach mit Max", "<name> sprach mit <name>", 2},
		{"Herr Mustermann ist im Meeting", "Herr <name> ist im Meeting", 1},
		{"Frau Musterfrau antwortete", "Frau <name> antwortete", 1},
		{"Hans Peter von Schmidt war dabei", "<name> war dabei", 1},
		{"Maximaler Aufwand für Max", "Maximaler Aufwand für <name>", 1}, // \b boundary check
	}

	for _, tc := range cases {
		masked, counts := MaskDetail(tc.input)
		if masked != tc.want {
			t.Errorf("MaskDetail(%q) = %q; want %q", tc.input, masked, tc.want)
		}
		if counts.Name != tc.wantCount {
			t.Errorf("MaskDetail(%q) counts.Name = %d; want %d", tc.input, counts.Name, tc.wantCount)
		}
		totalMasked, totalCount := Mask(tc.input)
		if totalMasked != tc.want || totalCount != tc.wantCount {
			t.Errorf("Mask(%q) = (%q, %d); want (%q, %d)", tc.input, totalMasked, totalCount, tc.want, tc.wantCount)
		}
	}

	// Missing names file -> clean skip, 0 hits
	t.Setenv("TYPESAFE_NAMES_FILE", filepath.Join(tmpDir, "nonexistent.txt"))
	plain := "Max Mustermann und Erika Musterfrau"
	masked, counts := MaskDetail(plain)
	if masked != plain {
		t.Errorf("MaskDetail with missing names file modified text: %q", masked)
	}
	if counts.Name != 0 {
		t.Errorf("expected 0 Name count for missing file, got %d", counts.Name)
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

func TestGetKeyBudget(t *testing.T) {
	// Verify KeyLookupTimeout constant is at most 800ms
	if KeyLookupTimeout > 800*time.Millisecond {
		t.Errorf("KeyLookupTimeout = %v; must be <= 800ms for hook budget", KeyLookupTimeout)
	}

	// When TYPESAFE_API_KEY is unset and secret-tool fails or times out: fail-open returns ""
	t.Setenv("TYPESAFE_API_KEY", "")

	// Test GetKeyWithContext with an already cancelled context
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	start := time.Now()
	key := GetKeyWithContext(ctx)
	duration := time.Since(start)

	if key != "" {
		t.Errorf("GetKeyWithContext(cancelled) = %q; want empty string", key)
	}
	if duration > 500*time.Millisecond {
		t.Errorf("GetKeyWithContext took %v; expected immediate fail-open", duration)
	}
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

// --- Rule Enforcement Checks (CL-002) ---------------------------------------

func TestCheckRuleEnforcement(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req Request
		_ = json.NewDecoder(r.Body).Decode(&req)

		prob := 0.1
		// If text lacks enforcement mechanism and does not state nothing enforces it
		if strings.Contains(req.State, "OHNE_DURCHSETZUNG") {
			prob = 0.88
		}

		resp := Response{
			Answers: map[string]RawAnswer{
				RuleEnforcementQuestionID: {
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

	// 1. Regel ohne Durchsetzung -> schlägt an (prob >= 0.5)
	textNoEnforcement := "OHNE_DURCHSETZUNG Jede Funktion darf maximal 20 Zeilen lang sein."
	needsNotice, notice, err := CheckRuleEnforcement(ctx, client, textNoEnforcement)
	if err != nil {
		t.Fatalf("CheckRuleEnforcement error: %v", err)
	}
	if !needsNotice {
		t.Errorf("expected needsNotice = true for rule missing enforcement")
	}
	if !strings.Contains(notice, "Jede Regel nennt, was sie durchsetzt") {
		t.Errorf("notice missing rule-enforcement guidance: %s", notice)
	}

	// 2. Regel mit Durchsetzungs-Nennung -> schlägt nicht an
	textWithEnforcement := "Jede Funktion darf maximal 20 Zeilen lang sein; durchgesetzt von check-len in tools/imprint-dev."
	needsNotice, _, err = CheckRuleEnforcement(ctx, client, textWithEnforcement)
	if err != nil {
		t.Fatalf("CheckRuleEnforcement error: %v", err)
	}
	if needsNotice {
		t.Errorf("expected needsNotice = false for rule naming enforcement")
	}

	// 3. Regel mit explizitem „nichts tut es“ -> schlägt nicht an
	textExplicitNothing := "Jede Funktion soll verständlich sein; nichts setzt das durch."
	needsNotice, _, err = CheckRuleEnforcement(ctx, client, textExplicitNothing)
	if err != nil {
		t.Fatalf("CheckRuleEnforcement error: %v", err)
	}
	if needsNotice {
		t.Errorf("expected needsNotice = false for rule explicitly stating nothing enforces it")
	}
}

// --- Duplicate Fact Checks (CL-003) -----------------------------------------

func TestFindDuplicateCandidates(t *testing.T) {
	tmpDir := t.TempDir()
	regFile := filepath.Join(tmpDir, "register.md")

	content := `# Register der Architekturentscheide
| Datum | Beschluss | Quelle |
|---|---|---|
| 2026-09-28 | SQLite wird im WAL-Modus betrieben für parallele Lesezugriffe. | Architekturbeschluss 1 |
| 2026-09-27 | Docker-Builds laufen immer ohne Root-Rechte im Container. | Sicherheitsrichtlinie |
| 2026-09-26 | Go-Binaries werden mit CGO_ENABLED=0 statisch gebaut. | Release-Leitfaden |
`
	if err := os.WriteFile(regFile, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write test register file: %v", err)
	}

	// 1. Existing file with keyword matches (>= 2 shared significant terms)
	cand := findDuplicateCandidates(regFile, "2026-09-29: SQLite Datenbank wird immer im WAL-Modus betrieben.")
	if len(cand) != 1 {
		t.Fatalf("expected 1 candidate, got %d: %v", len(cand), cand)
	}
	if !strings.Contains(cand[0], "SQLite wird im WAL-Modus") {
		t.Errorf("candidate line mismatch: %s", cand[0])
	}

	// 2. Non-existent file -> nil
	candMissing := findDuplicateCandidates(filepath.Join(tmpDir, "does-not-exist.md"), "SQLite WAL Modus")
	if candMissing != nil {
		t.Errorf("expected nil for non-existent file, got %v", candMissing)
	}

	// 3. Disjoint texts (< 2 shared terms) -> nil
	candDisjoint := findDuplicateCandidates(regFile, "Python-Pakete werden mit uv verwaltet.")
	if candDisjoint != nil {
		t.Errorf("expected nil for disjoint text, got %v", candDisjoint)
	}

	// 4. Trivial lines (headings and table separators) are not matched
	candHeading := findDuplicateCandidates(regFile, "Register der Architekturentscheide")
	if candHeading != nil {
		t.Errorf("headings should be ignored, got %v", candHeading)
	}

	// 5. Length cap: lines capped at 500 chars total
	longFile := filepath.Join(tmpDir, "long.md")
	longLine1 := "2026-09-01: SQLite WAL " + strings.Repeat("A", 300)
	longLine2 := "2026-09-02: SQLite WAL " + strings.Repeat("B", 300)
	if err := os.WriteFile(longFile, []byte(longLine1+"\n"+longLine2+"\n"), 0644); err != nil {
		t.Fatalf("failed to write long test file: %v", err)
	}
	candLong := findDuplicateCandidates(longFile, "SQLite WAL Betrieb")
	if len(candLong) == 0 {
		t.Fatalf("expected candidates for long file, got none")
	}
	totalLen := len(strings.Join(candLong, "\n"))
	if totalLen > 500 {
		t.Errorf("total candidates length %d exceeds 500 characters", totalLen)
	}

	// 6. Max 3 candidates returned
	multiFile := filepath.Join(tmpDir, "multi.md")
	multiContent := `
2026-09-01: SQLite WAL 1
2026-09-02: SQLite WAL 2
2026-09-03: SQLite WAL 3
2026-09-04: SQLite WAL 4
2026-09-05: SQLite WAL 5
`
	if err := os.WriteFile(multiFile, []byte(multiContent), 0644); err != nil {
		t.Fatalf("failed to write multi test file: %v", err)
	}
	candMulti := findDuplicateCandidates(multiFile, "SQLite WAL Betrieb")
	if len(candMulti) > 3 {
		t.Errorf("expected at most 3 candidates, got %d", len(candMulti))
	}
	if len(candMulti) != 3 {
		t.Errorf("expected exactly 3 candidates, got %d", len(candMulti))
	}
}

func TestCheckDuplicateFact(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req Request
		_ = json.NewDecoder(r.Body).Decode(&req)

		prob := 0.2
		if strings.Contains(req.State, "DUPLIKAT") {
			prob = 0.85
		}

		resp := Response{
			Answers: map[string]RawAnswer{
				DuplicateFactQuestionID: {
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

	// 1. Duplikat erkannt (prob >= 0.6) -> Notice vorhanden
	candidates := []string{"2026-09-28: SQLite wird im WAL-Modus betrieben."}
	needsNotice, notice, err := CheckDuplicateFact(ctx, client, candidates, "DUPLIKAT Neuer Eintrag: SQLite im WAL-Modus")
	if err != nil {
		t.Fatalf("CheckDuplicateFact error: %v", err)
	}
	if !needsNotice {
		t.Errorf("expected needsNotice = true for duplicate fact")
	}
	if !strings.Contains(notice, "bereits im Register existiert") {
		t.Errorf("notice missing duplicate fact text: %s", notice)
	}

	// 2. Kein Duplikat (prob < 0.6) -> keine Notice
	needsNotice, notice, err = CheckDuplicateFact(ctx, client, candidates, "Kein Duplikat: Postgres wird im Standard-Modus betrieben.")
	if err != nil {
		t.Fatalf("CheckDuplicateFact error: %v", err)
	}
	if needsNotice {
		t.Errorf("expected needsNotice = false when prob < 0.6")
	}
	if notice != "" {
		t.Errorf("expected empty notice when prob < 0.6, got %q", notice)
	}

	// 3. Leere Kandidaten -> sofort false ohne API-Aufruf
	needsNotice, notice, err = CheckDuplicateFact(ctx, client, nil, "Text")
	if err != nil || needsNotice || notice != "" {
		t.Errorf("expected (false, \"\", nil) for empty candidates")
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

		// Test files - ALWAYS false
		{"memory/test_notes.md", false},
		{"test/memory.md", false},
		{"tests/notes.md", false},
		{"memory/notes_test.go", false},
		{"memory/test-plan.md", false},
		{"memory/test.md", false},

		// Rule and skill files - false for isMemoryOrRegisterFile (handled by isRuleFile)
		{"rules/AGENTS.md", false},
		{"rules/custom.md", false},
		{"skills/knowledge-keeping/SKILL.md", false},
		{"skills/delegation-contract/SKILL.md", false},
		{"AGENTS.md", false},

		// Exact basenames - true
		{"entscheide.md", true},
		{"docs/entscheide.md", true},
		{"offene-entscheide.md", true},
		{"register.md", true},
		{"docs/register.md", true},
		{"MEMORY.md", true},
		{"memory.md", true},

		// Folder paths - true
		{"memory/notes.md", true},
		{"/var/home/user/memory/notes.md", true},
		{"memory/decisions.md", true},
	}

	for _, tc := range cases {
		got := isMemoryOrRegisterFile(tc.path)
		if got != tc.want {
			t.Errorf("isMemoryOrRegisterFile(%q) = %v; want %v", tc.path, got, tc.want)
		}
	}
}

func TestIsRuleFile(t *testing.T) {
	cases := []struct {
		path string
		want bool
	}{
		// Empty / invalid
		{"", false},
		{"   ", false},
		{".", false},

		// Source code files - ALWAYS false
		{"rules/check.go", false},
		{"skills/run.py", false},
		{"rules/deploy.sh", false},
		{"rules/main.ts", false},
		{"main.go", false},

		// Factory / build files - ALWAYS false
		{"rules/factory.md", false},
		{"rules/build.sh", false},
		{"skills/Makefile", false},
		{"rules/Dockerfile", false},

		// Test files - ALWAYS false
		{"rules/rules_test.go", false},
		{"rules/test_rule.md", false},
		{"rules/test-spec.md", false},
		{"rules/test.md", false},
		{"test/rules.md", false},
		{"tests/skills.md", false},

		// Memory files - false for isRuleFile
		{"memory/notes.md", false},
		{"entscheide.md", false},
		{"docs/entscheide.md", false},
		{"register.md", false},
		{"docs/register.md", false},

		// Non-rule markdown
		{"README.md", false},
		{"docs/status.md", false},

		// Positive rule files
		{"rules/AGENTS.md", true},
		{"rules/custom.md", true},
		{"AGENTS.md", true},
		{"agents.md", true},
		{"rules.md", true},
		{"skills/delegation-contract/SKILL.md", true},
		{"skills/knowledge-keeping/SKILL.md", true},
		{"skills/measure-before-asserting/SKILL.md", true},
		{"skills/session-handover/SKILL.md", true},
		{"skills/knowledge-keeping/references/provenance.md", true},
		{"/home/user/repo/rules/policy.md", true},
	}

	for _, tc := range cases {
		got := isRuleFile(tc.path)
		if got != tc.want {
			t.Errorf("isRuleFile(%q) = %v; want %v", tc.path, got, tc.want)
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
	// Mock TypeSafe server handling provenance, rule enforcement, and duplicate fact questions
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req Request
		_ = json.NewDecoder(r.Body).Decode(&req)

		probKK := 0.1
		if strings.Contains(req.State, "OHNE_DATUM") {
			probKK = 0.90
		}
		probRE := 0.1
		if strings.Contains(req.State, "OHNE_DURCHSETZUNG") {
			probRE = 0.90
		}
		probDup := 0.1
		if strings.Contains(req.State, "DUPLIKAT") {
			probDup = 0.90
		}

		resp := Response{
			Answers: map[string]RawAnswer{
				KnowledgeKeepingQuestionID: {
					Noul: &probKK,
				},
				RuleEnforcementQuestionID: {
					Noul: &probRE,
				},
				DuplicateFactQuestionID: {
					Noul: &probDup,
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

	// Case 2: Non-relevant file (main.go) -> exits 0 with no output
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
			t.Fatal("key lookup must not be invoked for excluded/non-relevant files like factory.go")
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

	// Case 2c: Test file exclusion does NOT invoke key lookup (rules_test.go -> kein Key-Lookup)
	{
		origKeyFn := getKeyFn
		getKeyFn = func() string {
			t.Fatal("key lookup must not be invoked for test files")
			return "fail"
		}

		var stdout, stderr bytes.Buffer
		stdin := strings.NewReader(`{"tool_name":"write_to_file","tool_input":{"TargetFile":"rules/rules_test.go","CodeContent":"OHNE_DURCHSETZUNG package main"}}`)
		code := runWithStdin([]string{"hook-typesafe-check", "--endpoint", ts.URL}, stdin, &stdout, &stderr)
		getKeyFn = origKeyFn

		if code != exitOK {
			t.Fatalf("code = %d; want exitOK", code)
		}
		if stdout.Len() != 0 {
			t.Errorf("expected empty stdout for rules_test.go, got %q", stdout.String())
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
		stdin := strings.NewReader(`{"hook_event_name":"PostToolUse","tool_name":"Edit","tool_input":{"file_path":"memory/notes.md","old_string":"foo","new_string":"OHNE_DATUM Neuer Leitsatz ohne Erfassungsdatum."}}`)
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

	// Case 3c: Rule file with missing enforcement via write_to_file (CL-002)
	{
		t.Setenv("TYPESAFE_API_KEY", "test-key")
		var stdout, stderr bytes.Buffer
		stdin := strings.NewReader(`{"hook_event_name":"PostToolUse","tool_name":"write_to_file","tool_input":{"TargetFile":"rules/AGENTS.md","CodeContent":"OHNE_DURCHSETZUNG Alle Commits müssen signiert sein."}}`)
		code := runWithStdin([]string{"hook-typesafe-check", "--endpoint", ts.URL}, stdin, &stdout, &stderr)
		if code != exitOK {
			t.Fatalf("code = %d; want exitOK", code)
		}
		if stdout.Len() == 0 {
			t.Fatal("expected hook output for rule file, got empty stdout")
		}

		var out HookOutput
		if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
			t.Fatalf("failed to decode hook output JSON: %v", err)
		}
		if !strings.Contains(out.HookSpecificOutput.AdditionalContext, "Jede Regel nennt, was sie durchsetzt") {
			t.Errorf("AdditionalContext does not contain rule-enforcement notice: %s", out.HookSpecificOutput.AdditionalContext)
		}
		if strings.Contains(out.HookSpecificOutput.AdditionalContext, "knowledge-keeping") {
			t.Errorf("Rule-only file should not have knowledge-keeping notice")
		}
	}

	// Case 3d: Combined payload (file matching both memory and rule) -> both notices joined with \n\n
	{
		t.Setenv("TYPESAFE_API_KEY", "test-key")
		var stdout, stderr bytes.Buffer
		stdin := strings.NewReader(`{"hook_event_name":"PostToolUse","tool_name":"write_to_file","tool_input":{"TargetFile":"memory/AGENTS.md","CodeContent":"OHNE_DATUM OHNE_DURCHSETZUNG Regel ohne Datum und ohne Durchsetzung."}}`)
		code := runWithStdin([]string{"hook-typesafe-check", "--endpoint", ts.URL}, stdin, &stdout, &stderr)
		if code != exitOK {
			t.Fatalf("code = %d; want exitOK", code)
		}
		if stdout.Len() == 0 {
			t.Fatal("expected hook output for combined file, got empty stdout")
		}

		var out HookOutput
		if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
			t.Fatalf("failed to decode hook output JSON: %v", err)
		}
		if !strings.Contains(out.HookSpecificOutput.AdditionalContext, "knowledge-keeping") {
			t.Errorf("AdditionalContext missing knowledge-keeping notice in combined: %s", out.HookSpecificOutput.AdditionalContext)
		}
		if !strings.Contains(out.HookSpecificOutput.AdditionalContext, "Jede Regel nennt, was sie durchsetzt") {
			t.Errorf("AdditionalContext missing rule-enforcement notice in combined: %s", out.HookSpecificOutput.AdditionalContext)
		}
		if !strings.Contains(out.HookSpecificOutput.AdditionalContext, "\n\n") {
			t.Errorf("Combined notices must be joined by double newline: %s", out.HookSpecificOutput.AdditionalContext)
		}
	}

	// Case 3e: Memory file with duplicate fact (CL-003) -> exits 0 with DuplicateFactNotice
	{
		tmpDir := t.TempDir()
		memDir := filepath.Join(tmpDir, "memory")
		if err := os.MkdirAll(memDir, 0755); err != nil {
			t.Fatalf("failed to create memDir: %v", err)
		}
		memFile := filepath.Join(memDir, "entscheide.md")
		if err := os.WriteFile(memFile, []byte("2026-09-28: SQLite Datenbank läuft im WAL-Modus für Nebenläufigkeit.\n"), 0644); err != nil {
			t.Fatalf("failed to write memFile: %v", err)
		}

		t.Setenv("TYPESAFE_API_KEY", "test-key")
		var stdout, stderr bytes.Buffer
		payload := fmt.Sprintf(`{"hook_event_name":"PostToolUse","tool_name":"write_to_file","tool_input":{"TargetFile":%q,"CodeContent":"2026-09-29: Gemäß Beschluss B: DUPLIKAT SQLite Datenbank im WAL-Modus."}}`, memFile)
		code := runWithStdin([]string{"hook-typesafe-check", "--endpoint", ts.URL}, strings.NewReader(payload), &stdout, &stderr)
		if code != exitOK {
			t.Fatalf("code = %d; want exitOK", code)
		}
		if stdout.Len() == 0 {
			t.Fatal("expected hook output for duplicate fact, got empty stdout")
		}

		var out HookOutput
		if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
			t.Fatalf("failed to decode hook output JSON: %v", err)
		}
		if !strings.Contains(out.HookSpecificOutput.AdditionalContext, "bereits im Register existiert") {
			t.Errorf("AdditionalContext missing duplicate fact notice: %s", out.HookSpecificOutput.AdditionalContext)
		}
		if strings.Contains(out.HookSpecificOutput.AdditionalContext, "ohne Erfassungsdatum") {
			t.Errorf("Compliant text with date should not have provenance notice: %s", out.HookSpecificOutput.AdditionalContext)
		}
	}

	// Case 3f: Memory file with both missing provenance AND duplicate fact -> both notices joined by \n\n
	{
		tmpDir := t.TempDir()
		memDir := filepath.Join(tmpDir, "memory")
		if err := os.MkdirAll(memDir, 0755); err != nil {
			t.Fatalf("failed to create memDir: %v", err)
		}
		memFile := filepath.Join(memDir, "entscheide.md")
		if err := os.WriteFile(memFile, []byte("2026-09-28: SQLite Datenbank läuft im WAL-Modus für Nebenläufigkeit.\n"), 0644); err != nil {
			t.Fatalf("failed to write memFile: %v", err)
		}

		t.Setenv("TYPESAFE_API_KEY", "test-key")
		var stdout, stderr bytes.Buffer
		payload := fmt.Sprintf(`{"hook_event_name":"PostToolUse","tool_name":"write_to_file","tool_input":{"TargetFile":%q,"CodeContent":"OHNE_DATUM DUPLIKAT SQLite Datenbank im WAL-Modus."}}`, memFile)
		code := runWithStdin([]string{"hook-typesafe-check", "--endpoint", ts.URL}, strings.NewReader(payload), &stdout, &stderr)
		if code != exitOK {
			t.Fatalf("code = %d; want exitOK", code)
		}
		if stdout.Len() == 0 {
			t.Fatal("expected hook output for missing provenance + duplicate fact, got empty stdout")
		}

		var out HookOutput
		if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
			t.Fatalf("failed to decode hook output JSON: %v", err)
		}
		if !strings.Contains(out.HookSpecificOutput.AdditionalContext, "knowledge-keeping") {
			t.Errorf("AdditionalContext missing provenance notice: %s", out.HookSpecificOutput.AdditionalContext)
		}
		if !strings.Contains(out.HookSpecificOutput.AdditionalContext, "bereits im Register existiert") {
			t.Errorf("AdditionalContext missing duplicate fact notice: %s", out.HookSpecificOutput.AdditionalContext)
		}
		if !strings.Contains(out.HookSpecificOutput.AdditionalContext, "\n\n") {
			t.Errorf("Multiple notices must be joined by double newline: %s", out.HookSpecificOutput.AdditionalContext)
		}
	}

	// Case 3g: Memory file that does not exist on disk -> no duplicate check, exits 0 with no duplicate notice
	{
		tmpDir := t.TempDir()
		memFile := filepath.Join(tmpDir, "memory", "nonexistent.md")

		t.Setenv("TYPESAFE_API_KEY", "test-key")
		var stdout, stderr bytes.Buffer
		payload := fmt.Sprintf(`{"hook_event_name":"PostToolUse","tool_name":"write_to_file","tool_input":{"TargetFile":%q,"CodeContent":"2026-09-29: Gemäß Beschluss B: DUPLIKAT SQLite Datenbank im WAL-Modus."}}`, memFile)
		code := runWithStdin([]string{"hook-typesafe-check", "--endpoint", ts.URL}, strings.NewReader(payload), &stdout, &stderr)
		if code != exitOK {
			t.Fatalf("code = %d; want exitOK", code)
		}
		if stdout.Len() != 0 {
			t.Errorf("expected empty stdout when file does not exist on disk, got %q", stdout.String())
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

	// Case 4b: Rule file with valid enforcement -> exits 0 with no output
	{
		t.Setenv("TYPESAFE_API_KEY", "test-key")
		var stdout, stderr bytes.Buffer
		stdin := strings.NewReader(`{"hook_event_name":"PostToolUse","tool_name":"write_to_file","tool_input":{"TargetFile":"rules/AGENTS.md","CodeContent":"Jede Regel nennt was sie durchsetzt; durchgesetzt von check-rules."}}`)
		code := runWithStdin([]string{"hook-typesafe-check", "--endpoint", ts.URL}, stdin, &stdout, &stderr)
		if code != exitOK {
			t.Fatalf("code = %d; want exitOK", code)
		}
		if stdout.Len() != 0 {
			t.Errorf("expected empty stdout for compliant rule text, got %q", stdout.String())
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

// --- Skill Suggestion Tests (CL-004) -----------------------------------------

func TestSkillSuggestionQuestion(t *testing.T) {
	q := SkillSuggestionQuestion()
	if q.Type != TypeChoice {
		t.Errorf("SkillSuggestionQuestion().Type = %q; want %q", q.Type, TypeChoice)
	}
	if !strings.Contains(q.Instructions, "delegation-contract") ||
		!strings.Contains(q.Instructions, "knowledge-keeping") ||
		!strings.Contains(q.Instructions, "measure-before-asserting") ||
		!strings.Contains(q.Instructions, "session-handover") ||
		!strings.Contains(q.Instructions, "none") {
		t.Errorf("SkillSuggestionQuestion().Instructions missing skill descriptions: %s", q.Instructions)
	}
	criteria, ok := q.Criteria.(map[string]string)
	if !ok {
		t.Fatalf("SkillSuggestionQuestion().Criteria is not map[string]string: %T", q.Criteria)
	}
	expectedKeys := []string{"delegation-contract", "knowledge-keeping", "measure-before-asserting", "session-handover", "none"}
	for _, k := range expectedKeys {
		if _, exists := criteria[k]; !exists {
			t.Errorf("SkillSuggestionQuestion().Criteria missing key %q", k)
		}
	}
}

func TestCheckSkillSuggestion(t *testing.T) {
	var mu sync.Mutex
	var mockChoice string
	var mockStatus int = http.StatusOK

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		status := mockStatus
		choice := mockChoice
		mu.Unlock()

		var req Request
		_ = json.NewDecoder(r.Body).Decode(&req)
		if status != http.StatusOK {
			http.Error(w, "server error", status)
			return
		}
		resp := Response{
			Answers: map[string]RawAnswer{
				SkillSuggestionQuestionID: {Choice: &choice},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer ts.Close()

	client := NewClient("test-key")
	client.Endpoint = ts.URL
	client.HTTPClient = ts.Client()

	// Case 1: All 4 allowed skills must be accepted
	for _, allowedSkill := range []string{"delegation-contract", "knowledge-keeping", "measure-before-asserting", "session-handover"} {
		mu.Lock()
		mockChoice = allowedSkill
		mu.Unlock()
		skill, err := CheckSkillSuggestion(context.Background(), client, "A prompt matching "+allowedSkill)
		if err != nil {
			t.Fatalf("unexpected error for %q: %v", allowedSkill, err)
		}
		if skill != allowedSkill {
			t.Errorf("skill = %q; want %q", skill, allowedSkill)
		}
	}

	// Case 1b: Hallucinated or unknown skills must be discarded
	for _, unknownSkill := range []string{"python-developer", "code-architect", "unknown-imprint-skill", "random"} {
		mu.Lock()
		mockChoice = unknownSkill
		mu.Unlock()
		skill, err := CheckSkillSuggestion(context.Background(), client, "Some prompt")
		if err != nil {
			t.Fatalf("unexpected error for unknown skill %q: %v", unknownSkill, err)
		}
		if skill != "" {
			t.Errorf("unknown skill %q was not discarded: got %q; want empty string", unknownSkill, skill)
		}
	}

	// Case 2: Skill "none" chosen
	mu.Lock()
	mockChoice = "none"
	mu.Unlock()
	skill, err := CheckSkillSuggestion(context.Background(), client, "Refaktoriere die Schleife in main.go")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if skill != "" {
		t.Errorf("skill = %q; want empty string", skill)
	}

	// Case 3: Empty choice
	mu.Lock()
	mockChoice = ""
	mu.Unlock()
	skill, err = CheckSkillSuggestion(context.Background(), client, "Ein normaler Text")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if skill != "" {
		t.Errorf("skill = %q; want empty string", skill)
	}

	// Case 4: No API key
	noKeyClient := NewClient("")
	skill, err = CheckSkillSuggestion(context.Background(), noKeyClient, "Prompt")
	if err != nil || skill != "" {
		t.Errorf("expected empty string and nil error when no API key, got %q, %v", skill, err)
	}

	// Case 5: Nil client
	skill, err = CheckSkillSuggestion(context.Background(), nil, "Prompt")
	if err != nil || skill != "" {
		t.Errorf("expected empty string and nil error when nil client, got %q, %v", skill, err)
	}

	// Case 6: Server error -> returns error
	mu.Lock()
	mockStatus = http.StatusInternalServerError
	mu.Unlock()
	_, err = CheckSkillSuggestion(context.Background(), client, "Prompt")
	if err == nil {
		t.Errorf("expected error on HTTP 500, got nil")
	}
}

func TestExtractPromptText(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  string
	}{
		{"prompt field", `{"prompt": "Hello world"}`, "Hello world"},
		{"user_prompt field", `{"user_prompt": "Hello from user"}`, "Hello from user"},
		{"text field", `{"text": "Text content"}`, "Text content"},
		{"input field string", `{"input": "Input text"}`, "Input text"},
		{"input field map", `{"input": {"prompt": "Nested prompt"}}`, "Nested prompt"},
		{"tool_input field map", `{"tool_input": {"prompt": "Nested tool prompt"}}`, "Nested tool prompt"},
		{"query field", `{"query": "Search query"}`, "Search query"},
		{"content field", `{"content": "Raw content in json"}`, "Raw content in json"},
		{"raw plain text", "Delegiere diesen Task an einen Subagenten", "Delegiere diesen Task an einen Subagenten"},
		{"empty string", "", ""},
		{"json with unknown field", `{"unknown": 123}`, ""},
	}

	for _, tc := range cases {
		got := extractPromptText([]byte(tc.input))
		if got != tc.want {
			t.Errorf("%s: extractPromptText() = %q; want %q", tc.name, got, tc.want)
		}
	}
}

func TestRunHookSkillSuggestion(t *testing.T) {
	var mu sync.Mutex
	var mockChoice string
	var mockStatus int = http.StatusOK

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		status := mockStatus
		choice := mockChoice
		mu.Unlock()

		var req Request
		_ = json.NewDecoder(r.Body).Decode(&req)
		if status != http.StatusOK {
			http.Error(w, "server error", status)
			return
		}
		resp := Response{
			Answers: map[string]RawAnswer{
				SkillSuggestionQuestionID: {Choice: &choice},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer ts.Close()

	// Case 1: No API key -> exits 0, no output
	{
		t.Setenv("TYPESAFE_API_KEY", "")
		var stdout, stderr bytes.Buffer
		stdin := strings.NewReader(`{"prompt": "Delegiere diesen Task an einen Subagenten"}`)
		code := runWithStdin([]string{"hook-skill-suggestion", "--endpoint", ts.URL}, stdin, &stdout, &stderr)
		if code != exitOK {
			t.Fatalf("code = %d; want exitOK", code)
		}
		if stdout.Len() != 0 {
			t.Errorf("expected empty stdout when no API key, got %q", stdout.String())
		}
	}

	// Case 2: Short prompt (< 10 chars) -> exits 0, no output
	{
		t.Setenv("TYPESAFE_API_KEY", "test-key")
		var stdout, stderr bytes.Buffer
		stdin := strings.NewReader(`{"prompt": "Kurz"}`)
		code := runWithStdin([]string{"hook-skill-suggestion", "--endpoint", ts.URL}, stdin, &stdout, &stderr)
		if code != exitOK {
			t.Fatalf("code = %d; want exitOK", code)
		}
		if stdout.Len() != 0 {
			t.Errorf("expected empty stdout for prompt < 10 chars, got %q", stdout.String())
		}
	}

	// Case 3: Empty stdin -> exits 0, no output
	{
		t.Setenv("TYPESAFE_API_KEY", "test-key")
		var stdout, stderr bytes.Buffer
		stdin := strings.NewReader(``)
		code := runWithStdin([]string{"hook-skill-suggestion", "--endpoint", ts.URL}, stdin, &stdout, &stderr)
		if code != exitOK {
			t.Fatalf("code = %d; want exitOK", code)
		}
		if stdout.Len() != 0 {
			t.Errorf("expected empty stdout for empty stdin, got %q", stdout.String())
		}
	}

	// Case 4: Skill suggested ("delegation-contract") -> JSON output with notice
	{
		t.Setenv("TYPESAFE_API_KEY", "test-key")
		mu.Lock()
		mockChoice = "delegation-contract"
		mockStatus = http.StatusOK
		mu.Unlock()
		var stdout, stderr bytes.Buffer
		stdin := strings.NewReader(`{"hook_event_name": "UserPromptSubmit", "prompt": "Delegiere diese Aufgabe an einen Subagenten und lass ihn prüfen"}`)
		code := runWithStdin([]string{"hook-skill-suggestion", "--endpoint", ts.URL}, stdin, &stdout, &stderr)
		if code != exitOK {
			t.Fatalf("code = %d; want exitOK", code)
		}
		if stdout.Len() == 0 {
			t.Fatal("expected stdout output for suggested skill, got empty")
		}

		var out HookOutput
		if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
			t.Fatalf("failed to decode HookOutput: %v", err)
		}
		if out.HookSpecificOutput.HookEventName != "UserPromptSubmit" {
			t.Errorf("HookEventName = %q; want %q", out.HookSpecificOutput.HookEventName, "UserPromptSubmit")
		}
		wantNotice := "imprint core: Für diesen Arbeitsschritt könnte der Skill 'delegation-contract' relevant sein.\n(imprint core: Skill 'delegation-contract' might be relevant for this task.)"
		if out.HookSpecificOutput.AdditionalContext != wantNotice {
			t.Errorf("AdditionalContext = %q; want %q", out.HookSpecificOutput.AdditionalContext, wantNotice)
		}
		if len(out.InjectSteps) == 0 || out.InjectSteps[0].EphemeralMessage != wantNotice {
			t.Errorf("EphemeralMessage mismatch: %v", out.InjectSteps)
		}
	}

	// Case 5: Raw content (non-JSON) prompt with suggested skill
	{
		t.Setenv("TYPESAFE_API_KEY", "test-key")
		mu.Lock()
		mockChoice = "session-handover"
		mockStatus = http.StatusOK
		mu.Unlock()
		var stdout, stderr bytes.Buffer
		stdin := strings.NewReader(`Schluss für heute, lass uns die Übergabe machen`)
		code := runWithStdin([]string{"hook-skill-suggestion", "--endpoint", ts.URL}, stdin, &stdout, &stderr)
		if code != exitOK {
			t.Fatalf("code = %d; want exitOK", code)
		}
		if stdout.Len() == 0 {
			t.Fatal("expected stdout output for raw content prompt, got empty")
		}
		var out HookOutput
		if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
			t.Fatalf("failed to decode HookOutput: %v", err)
		}
		if !strings.Contains(out.HookSpecificOutput.AdditionalContext, "session-handover") {
			t.Errorf("expected session-handover notice, got %q", out.HookSpecificOutput.AdditionalContext)
		}
	}

	// Case 6: "none" chosen -> exits 0, no output
	{
		t.Setenv("TYPESAFE_API_KEY", "test-key")
		mu.Lock()
		mockChoice = "none"
		mockStatus = http.StatusOK
		mu.Unlock()
		var stdout, stderr bytes.Buffer
		stdin := strings.NewReader(`{"prompt": "Schreibe eine Hilfsfunktion zum Parsen von Datumsangaben"}`)
		code := runWithStdin([]string{"hook-skill-suggestion", "--endpoint", ts.URL}, stdin, &stdout, &stderr)
		if code != exitOK {
			t.Fatalf("code = %d; want exitOK", code)
		}
		if stdout.Len() != 0 {
			t.Errorf("expected empty stdout when none chosen, got %q", stdout.String())
		}
	}

	// Case 6b: Hallucinated / unknown skill chosen -> exits 0, no output (discarded)
	{
		t.Setenv("TYPESAFE_API_KEY", "test-key")
		mu.Lock()
		mockChoice = "python-expert"
		mockStatus = http.StatusOK
		mu.Unlock()
		var stdout, stderr bytes.Buffer
		stdin := strings.NewReader(`{"prompt": "Schreibe ein Python-Skript fuer Datenanalyse"}`)
		code := runWithStdin([]string{"hook-skill-suggestion", "--endpoint", ts.URL}, stdin, &stdout, &stderr)
		if code != exitOK {
			t.Fatalf("code = %d; want exitOK", code)
		}
		if stdout.Len() != 0 {
			t.Errorf("expected empty stdout when unknown skill chosen, got %q", stdout.String())
		}
	}

	// Case 7: Server error (HTTP 500) -> fail-open (exits 0, no output)
	{
		t.Setenv("TYPESAFE_API_KEY", "test-key")
		mu.Lock()
		mockStatus = http.StatusInternalServerError
		mu.Unlock()
		var stdout, stderr bytes.Buffer
		stdin := strings.NewReader(`{"prompt": "Delegiere diese Aufgabe an einen Subagenten"}`)
		code := runWithStdin([]string{"hook-skill-suggestion", "--endpoint", ts.URL}, stdin, &stdout, &stderr)
		if code != exitOK {
			t.Fatalf("code = %d; want exitOK", code)
		}
		if stdout.Len() != 0 {
			t.Errorf("expected empty stdout on server error, got %q", stdout.String())
		}
		mu.Lock()
		mockStatus = http.StatusOK
		mu.Unlock()
	}

	// Case 8: Timeout -> fail-open (exits 0, no output)
	{
		slowServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			time.Sleep(1500 * time.Millisecond)
		}))
		defer slowServer.Close()

		t.Setenv("TYPESAFE_API_KEY", "test-key")
		var stdout, stderr bytes.Buffer
		stdin := strings.NewReader(`{"prompt": "Delegiere diese Aufgabe an einen Subagenten"}`)
		code := runWithStdin([]string{"hook-skill-suggestion", "--endpoint", slowServer.URL, "--timeout", "1"}, stdin, &stdout, &stderr)
		if code != exitOK {
			t.Fatalf("code = %d; want exitOK", code)
		}
		if stdout.Len() != 0 {
			t.Errorf("expected empty stdout on timeout, got %q", stdout.String())
		}
	}

	// Case 9: Bad flags -> fail-open (exits 0, no output)
	{
		t.Setenv("TYPESAFE_API_KEY", "test-key")
		var stdout, stderr bytes.Buffer
		stdin := strings.NewReader(`{"prompt": "Delegiere diese Aufgabe an einen Subagenten"}`)
		code := runWithStdin([]string{"hook-skill-suggestion", "--unknown-flag"}, stdin, &stdout, &stderr)
		if code != exitOK {
			t.Fatalf("code = %d; want exitOK", code)
		}
	}
}

func TestSkillSuggestionWrapperScript(t *testing.T) {
	scriptPath := filepath.Join("..", "..", "hooks", "skill-suggestion.sh")
	info, err := os.Stat(scriptPath)
	if err != nil {
		t.Fatalf("hooks/skill-suggestion.sh missing: %v", err)
	}

	// Check executable permission
	if info.Mode()&0111 == 0 {
		t.Errorf("hooks/skill-suggestion.sh is not executable: mode %v", info.Mode())
	}

	// Check script contents
	data, err := os.ReadFile(scriptPath)
	if err != nil {
		t.Fatalf("failed to read hooks/skill-suggestion.sh: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "command -v imprint-dev >/dev/null 2>&1 || exit 0") {
		t.Errorf("missing command -v check in script: %s", content)
	}
	if !strings.Contains(content, "exec imprint-dev hook-skill-suggestion") {
		t.Errorf("missing exec imprint-dev hook-skill-suggestion in script: %s", content)
	}

	// Check hooks.json registration
	hooksJSONPath := filepath.Join("..", "..", "hooks", "hooks.json")
	hooksData, err := os.ReadFile(hooksJSONPath)
	if err != nil {
		t.Fatalf("failed to read hooks/hooks.json: %v", err)
	}
	var doc struct {
		Hooks map[string][]hookGroup `json:"hooks"`
	}
	if err := json.Unmarshal(hooksData, &doc); err != nil {
		t.Fatalf("failed to parse hooks/hooks.json: %v", err)
	}
	groups, ok := doc.Hooks["UserPromptSubmit"]
	if !ok || len(groups) == 0 {
		t.Fatalf("UserPromptSubmit not registered in hooks.json")
	}
	found := false
	for _, g := range groups {
		for _, h := range g.Hooks {
			if strings.Contains(h.Command, "hooks/skill-suggestion.sh") {
				found = true
				break
			}
		}
	}
	if !found {
		t.Errorf("hooks.json UserPromptSubmit does not reference hooks/skill-suggestion.sh: %s", string(hooksData))
	}
}
