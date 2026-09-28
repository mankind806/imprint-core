package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestRenderHookExactBytes(t *testing.T) {
	tests := []struct {
		name, event, card, want string
	}{
		{
			name:  "plain",
			event: "SessionStart",
			card:  "One line.",
			want:  `{"hookSpecificOutput":{"hookEventName":"SessionStart","additionalContext":"One line."}}` + "\n",
		},
		{
			name:  "quotes, HTML characters, newline, non-ASCII",
			event: "SubagentStart",
			card:  "Say \"not checked\" <b> & 'x'\nzweite Zeile: \xc3\xa4\xc3\xb6\xc3\xbc",
			want:  `{"hookSpecificOutput":{"hookEventName":"SubagentStart","additionalContext":"Say \"not checked\" <b> & 'x'\nzweite Zeile: ` + "\xc3\xa4\xc3\xb6\xc3\xbc" + `"}}` + "\n",
		},
		{
			name:  "backslash and tab",
			event: "SessionStart",
			card:  "a\\b\tc",
			want:  `{"hookSpecificOutput":{"hookEventName":"SessionStart","additionalContext":"a\\b\tc"}}` + "\n",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := renderHook(tc.event, tc.card)
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != tc.want {
				t.Fatalf("got  %s\nwant %s", got, tc.want)
			}
			var back hookPayload
			if err := json.Unmarshal(got, &back); err != nil {
				t.Fatal(err)
			}
			if back.HookSpecificOutput.AdditionalContext != tc.card || back.HookSpecificOutput.HookEventName != tc.event {
				t.Fatalf("round trip changed the payload: %+v", back)
			}
		})
	}
}

func TestCardText(t *testing.T) {
	tests := []struct{ name, in, want string }{
		{"trailing newline trimmed", "a\nb\n", "a\nb"},
		{"CRLF becomes LF", "a\r\nb\r\n", "a\nb"},
		{"byte order mark dropped", "\xef\xbb\xbfa\n", "a"},
		{"trailing blanks trimmed", "a  \n\n\t\n", "a"},
		{"inner blank line kept", "a\n\nb\n", "a\n\nb"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := cardText([]byte(tc.in)); got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestGenWritesPayloadsAndRulesDeterministically(t *testing.T) {
	root := t.TempDir()
	writeFiles(t, root, map[string]string{"hooks/kernkarte.md": testCard})

	written, err := writeGenerated(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(written) != 3 || written[0] != sessionStartPath || written[1] != subagentStartPath || written[2] != rulesAgentsPath {
		t.Fatalf("wrote %v", written)
	}
	first := map[string][]byte{}
	for _, p := range written {
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(p)))
		if err != nil {
			t.Fatal(err)
		}
		first[p] = data
	}
	want := `{"hookSpecificOutput":{"hookEventName":"SessionStart","additionalContext":"First line of the fixture card.\nSecond line."}}` + "\n"
	if string(first[sessionStartPath]) != want {
		t.Fatalf("session-start.json:\ngot  %s\nwant %s", first[sessionStartPath], want)
	}
	if string(first[rulesAgentsPath]) != testCard {
		t.Fatalf("rules/AGENTS.md:\ngot  %s\nwant %s", first[rulesAgentsPath], testCard)
	}

	if _, err := writeGenerated(root); err != nil {
		t.Fatal(err)
	}
	for _, p := range written {
		again, _ := os.ReadFile(filepath.Join(root, filepath.FromSlash(p)))
		if !bytes.Equal(again, first[p]) {
			t.Fatalf("%s changed on a second run", p)
		}
	}

	// A CRLF checkout of the same card generates the same bytes for hook payloads,
	// and preserves card bytes for rules/AGENTS.md.
	crlf := t.TempDir()
	crlfCard := "First line of the fixture card.\r\nSecond line.\r\n"
	writeFiles(t, crlf, map[string]string{"hooks/kernkarte.md": crlfCard})
	if _, err := writeGenerated(crlf); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{sessionStartPath, subagentStartPath} {
		other, _ := os.ReadFile(filepath.Join(crlf, filepath.FromSlash(p)))
		if !bytes.Equal(other, first[p]) {
			t.Fatalf("%s differs between LF and CRLF cards", p)
		}
	}
	crlfRules, _ := os.ReadFile(filepath.Join(crlf, filepath.FromSlash(rulesAgentsPath)))
	if !bytes.Equal(crlfRules, []byte(crlfCard)) {
		t.Fatalf("rules/AGENTS.md does not match card on CRLF checkout")
	}

	// And what gen wrote is what check c and check k accept.
	if res, err := checkCardGenerated(testEnv(t, root)); err != nil || len(violations(res.Findings)) != 0 {
		t.Fatalf("check c after gen: %v %+v", err, res.Findings)
	}
	if res, err := checkRulesAgentsInSync(testEnv(t, root)); err != nil || len(violations(res.Findings)) != 0 {
		t.Fatalf("check k after gen: %v %+v", err, res.Findings)
	}
}

func TestGenWithoutCardFails(t *testing.T) {
	if _, err := writeGenerated(t.TempDir()); err == nil {
		t.Fatal("gen without a card succeeded")
	}
}
