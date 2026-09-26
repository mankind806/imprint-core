package main

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// These tests run the real hooks/log-subagent.sh - the second exception to the
// rule that no test reads the repository - with invented input and a temporary
// CLAUDE_PLUGIN_DATA. Without an sh on PATH they are skipped, and say so.

var hookScript = filepath.Join("..", "..", "hooks", "log-subagent.sh")

// runHook runs the script with input on stdin and dataDir as CLAUDE_PLUGIN_DATA
// ("" leaves it unset). It fails the test unless the script exits 0 and prints
// nothing, since a hook that talks or fails would reach the session.
func runHook(t *testing.T, dataDir, input string) {
	t.Helper()
	sh, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("no sh on PATH, so the hook script cannot be run here: " + err.Error())
	}
	cmd := exec.Command(sh, hookScript)
	var env []string
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(kv, "CLAUDE_PLUGIN_DATA=") {
			env = append(env, kv)
		}
	}
	if dataDir != "" {
		env = append(env, "CLAUDE_PLUGIN_DATA="+dataDir)
	}
	cmd.Env = env
	cmd.Stdin = strings.NewReader(input)
	var out, errOut bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errOut
	if err := cmd.Run(); err != nil {
		t.Fatalf("the hook must exit 0: %v", err)
	}
	if out.Len() > 0 || errOut.Len() > 0 {
		t.Fatalf("the hook must print nothing; stdout %q, stderr %q", out.String(), errOut.String())
	}
}

func hookLog(t *testing.T, dataDir string) []map[string]string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(dataDir, logFileName))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	var lines []map[string]string
	for _, l := range strings.Split(strings.TrimSuffix(string(raw), "\n"), "\n") {
		var m map[string]string
		if err := json.Unmarshal([]byte(l), &m); err != nil {
			t.Fatalf("not a JSON line of strings: %v\n%s", err, l)
		}
		lines = append(lines, m)
	}
	return lines
}

var tsShape = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}Z$`)

func TestHookLogsStart(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "not", "yet", "there")
	runHook(t, dir, `{"hook_event_name":"SubagentStart","agent_id":"a1","agent_type":"general-purpose","session_id":"s","effort":{"level":"high"}}`)
	got := hookLog(t, dir)
	if len(got) != 1 {
		t.Fatalf("got %d lines, want 1", len(got))
	}
	l := got[0]
	if !tsShape.MatchString(l["ts"]) {
		t.Errorf("ts %q is not UTC ISO 8601 with seconds", l["ts"])
	}
	want := map[string]string{"hook_event_name": "SubagentStart", "agent_id": "a1",
		"agent_type": "general-purpose", "session_id": "s", "effort": "high"}
	for k, v := range want {
		if l[k] != v {
			t.Errorf("%s = %q, want %q", k, l[k], v)
		}
	}
	if _, ok := l["agent_transcript_path"]; ok {
		t.Error("a start carries no transcript path")
	}
	if len(l) != len(want)+1 {
		t.Errorf("unexpected fields: %v", l)
	}
}

func TestHookLogsStopWithoutTheAnswer(t *testing.T) {
	dir := t.TempDir()
	// Pretty-printed, with the answer before and after the fields it imitates,
	// and a transcript path holding a backslash and a quote.
	input := `{
  "session_id": "s2",
  "last_assistant_message": "MARKER-7f3a {\"agent_type\":\"forged\",\"agent_id\":\"forged\"} \\ \"quoted\"",
  "hook_event_name": "SubagentStop",
  "agent_id": "a2",
  "agent_type": "Explore",
  "effort": "low",
  "agent_transcript_path": "C:\\data\\odd \"name\"\\agent-a2.jsonl",
  "stop_hook_active": false,
  "cwd": "/somewhere/else"
}`
	runHook(t, dir, input)
	raw, _ := os.ReadFile(filepath.Join(dir, logFileName))
	if bytes.Contains(raw, []byte("MARKER-7f3a")) || bytes.Contains(raw, []byte("forged")) || bytes.Contains(raw, []byte("somewhere")) {
		t.Fatalf("the log holds text it must not copy:\n%s", raw)
	}
	got := hookLog(t, dir)
	if len(got) != 1 {
		t.Fatalf("got %d lines, want 1", len(got))
	}
	l := got[0]
	if l["agent_type"] != "Explore" || l["agent_id"] != "a2" || l["effort"] != "low" || l["session_id"] != "s2" {
		t.Errorf("fields: %v", l)
	}
	if want := `C:\data\odd "name"\agent-a2.jsonl`; l["agent_transcript_path"] != want {
		t.Errorf("agent_transcript_path %q, want %q", l["agent_transcript_path"], want)
	}
}

func TestHookWritesNothing(t *testing.T) {
	for name, input := range map[string]string{
		"empty agent_type":   `{"hook_event_name":"SubagentStop","agent_id":"a3","agent_type":"","session_id":"s"}`,
		"missing agent_type": `{"hook_event_name":"SubagentStart","agent_id":"a3","session_id":"s"}`,
		"missing agent_id":   `{"hook_event_name":"SubagentStart","agent_type":"t","session_id":"s"}`,
		"other event":        `{"hook_event_name":"SessionStart","agent_id":"a3","agent_type":"t"}`,
		"not JSON":           "this is not json at all \\ \" '",
		"empty input":        "",
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			runHook(t, dir, input)
			if got := hookLog(t, dir); len(got) != 0 {
				t.Fatalf("wrote %v", got)
			}
		})
	}
	t.Run("CLAUDE_PLUGIN_DATA unset", func(t *testing.T) {
		runHook(t, "", `{"hook_event_name":"SubagentStart","agent_id":"a4","agent_type":"t","session_id":"s"}`)
	})
	t.Run("unwritable data directory", func(t *testing.T) {
		file := filepath.Join(t.TempDir(), "a-file")
		if err := os.WriteFile(file, nil, 0o644); err != nil {
			t.Fatal(err)
		}
		runHook(t, filepath.Join(file, "below"), `{"hook_event_name":"SubagentStart","agent_id":"a5","agent_type":"t","session_id":"s"}`)
	})
}

// What the hook writes, measure reads.
func TestHookOutputFeedsMeasure(t *testing.T) {
	dir := t.TempDir()
	runHook(t, dir, `{"hook_event_name":"SubagentStart","agent_id":"a6","agent_type":"t","session_id":"s"}`)
	runHook(t, dir, `{"hook_event_name":"SubagentStop","agent_id":"a6","agent_type":"t","session_id":"s","agent_transcript_path":""}`)
	code, stdout, stderr := runCLI(t, "measure", "--log", filepath.Join(dir, logFileName), "--format", "json")
	if code != exitOK {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	var runs []measuredRun
	if err := json.Unmarshal([]byte(stdout), &runs); err != nil || len(runs) != 1 || runs[0].Seconds == nil {
		t.Fatalf("got %v / %s", err, stdout)
	}
}
