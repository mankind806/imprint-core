package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Every log line and transcript here is invented.

func logLine(ts, event, id, typ, effort, transcript string) string {
	m := map[string]string{"ts": ts, "hook_event_name": event, "agent_id": id, "agent_type": typ,
		"session_id": "session-1", "effort": effort}
	if event == "SubagentStop" {
		m["agent_transcript_path"] = transcript
	}
	b, _ := json.Marshal(m)
	return string(b) + "\n"
}

func writeTranscript(t *testing.T, path string, models ...string) {
	t.Helper()
	var b strings.Builder
	b.WriteString(`{"type":"user","message":{"role":"user","content":"do the thing"}}` + "\n")
	for _, m := range models {
		b.WriteString(`{"type":"assistant","effort":"medium","message":{"role":"assistant","model":"` + m + `","content":[{"type":"text","text":"model \"assistant\" ok"}]}}` + "\n")
	}
	// A line longer than bufio.Scanner's default buffer must not hide what follows.
	b.WriteString(`{"type":"user","message":{"role":"user","content":"` + strings.Repeat("x", 200_000) + `"}}` + "\n")
	b.WriteString(`{"type":"assistant","message":{"role":"assistant","model":"model-late"}}`)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
}

func measureRuns(t *testing.T, log string, target time.Duration, projects string) []measuredRun {
	t.Helper()
	entries, _, err := readLog(strings.NewReader(log))
	if err != nil {
		t.Fatal(err)
	}
	runs := pairRuns(entries, target)
	if err := resolveModels(runs, projects); err != nil {
		t.Fatal(err)
	}
	return runs
}

func TestPairResumeWithTwoStarts(t *testing.T) {
	// Two starts before one stop: the stop pairs with the later start.
	log := logLine("2026-01-01T10:00:00Z", "SubagentStart", "a1", "general-purpose", "high", "") +
		logLine("2026-01-01T10:10:00Z", "SubagentStart", "a1", "general-purpose", "high", "") +
		logLine("2026-01-01T10:13:00Z", "SubagentStop", "a1", "general-purpose", "high", "")
	runs := measureRuns(t, log, 5*time.Minute, "")
	if len(runs) != 1 {
		t.Fatalf("got %d runs, want 1: %+v", len(runs), runs)
	}
	if r := runs[0]; r.Seconds == nil || *r.Seconds != 180 || *r.OverTarget {
		t.Fatalf("want 180s under target, got %+v", r)
	}

	// Start, stop, resume, stop: two runs.
	log = logLine("2026-01-01T10:00:00Z", "SubagentStart", "a1", "general-purpose", "", "") +
		logLine("2026-01-01T10:02:00Z", "SubagentStop", "a1", "general-purpose", "", "") +
		logLine("2026-01-01T11:00:00Z", "SubagentStart", "a1", "general-purpose", "", "") +
		logLine("2026-01-01T11:07:00Z", "SubagentStop", "a1", "general-purpose", "", "")
	runs = measureRuns(t, log, 5*time.Minute, "")
	if len(runs) != 2 || *runs[0].Seconds != 120 || *runs[1].Seconds != 420 {
		t.Fatalf("want 120s and 420s, got %+v", runs)
	}
	if *runs[0].OverTarget || !*runs[1].OverTarget {
		t.Fatalf("want under, then over target: %+v", runs)
	}
}

func TestPairUnmatched(t *testing.T) {
	log := logLine("2026-01-01T10:00:00Z", "SubagentStart", "open", "Explore", "", "") +
		logLine("2026-01-01T10:05:00Z", "SubagentStop", "orphan", "Explore", "", "")
	runs := measureRuns(t, log, 5*time.Minute, "")
	if len(runs) != 2 {
		t.Fatalf("got %d runs, want 2", len(runs))
	}
	for _, r := range runs {
		if r.Seconds != nil || r.OverTarget != nil {
			t.Errorf("an unpaired run has no duration: %+v", r)
		}
	}
}

func TestTargetBoundary(t *testing.T) {
	// Exactly on the target is not over it; one second more is.
	log := logLine("2026-01-01T10:00:00Z", "SubagentStart", "on", "t", "", "") +
		logLine("2026-01-01T10:05:00Z", "SubagentStop", "on", "t", "", "") +
		logLine("2026-01-01T10:00:00Z", "SubagentStart", "over", "t", "", "") +
		logLine("2026-01-01T10:05:01Z", "SubagentStop", "over", "t", "", "")
	for _, r := range measureRuns(t, log, 5*time.Minute, "") {
		if want := r.AgentID == "over"; *r.OverTarget != want {
			t.Errorf("%s: over target %v, want %v", r.AgentID, *r.OverTarget, want)
		}
	}
}

func TestEmptyAgentTypeAndBadLinesSkipped(t *testing.T) {
	log := logLine("2026-01-01T10:00:00Z", "SubagentStart", "a1", "", "", "") +
		logLine("2026-01-01T10:01:00Z", "SubagentStop", "a1", "", "", "") +
		"not json\n" +
		logLine("not a time", "SubagentStart", "a2", "t", "", "") +
		logLine("2026-01-01T10:01:00Z", "SessionStart", "a3", "t", "", "")
	entries, skipped, err := readLog(strings.NewReader(log))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 || skipped != 5 {
		t.Fatalf("got %d entries and %d skipped, want 0 and 5", len(entries), skipped)
	}
}

func TestModelsFromTranscript(t *testing.T) {
	dir := t.TempDir()
	tr := filepath.Join(dir, "agent-a1.jsonl")
	writeTranscript(t, tr, "model-one", "model-two", "model-one")
	log := logLine("2026-01-01T10:00:00Z", "SubagentStart", "a1", "t", "", "") +
		logLine("2026-01-01T10:01:00Z", "SubagentStop", "a1", "t", "", tr) +
		logLine("2026-01-01T10:00:00Z", "SubagentStart", "gone", "t", "", "") +
		logLine("2026-01-01T10:01:00Z", "SubagentStop", "gone", "t", "", filepath.Join(dir, "agent-gone.jsonl")) +
		logLine("2026-01-01T10:02:00Z", "SubagentStart", "running", "t", "", "")
	runs := measureRuns(t, log, 5*time.Minute, "")
	byID := map[string]measuredRun{}
	for _, r := range runs {
		byID[r.AgentID] = r
	}
	if got := strings.Join(byID["a1"].Models, ","); got != "model-one,model-two,model-late" {
		t.Errorf("a1 models %q", got)
	}
	if byID["a1"].Effort != "medium" || byID["a1"].Transcript != transcriptFound {
		t.Errorf("a1: effort %q from the transcript, transcript %q", byID["a1"].Effort, byID["a1"].Transcript)
	}
	if r := byID["gone"]; r.Transcript != transcriptMissing || strings.Join(r.Models, ",") != unknownModel {
		t.Errorf("gone: %+v", r)
	}
	if r := byID["running"]; r.Transcript != transcriptNotRecorded || strings.Join(r.Models, ",") != unknownModel {
		t.Errorf("running: %+v", r)
	}
}

func TestProjectsFallback(t *testing.T) {
	projects := t.TempDir()
	writeTranscript(t, filepath.Join(projects, "some-project", "some-session", "subagents", "agent-a9.jsonl"), "model-found")
	log := logLine("2026-01-01T10:00:00Z", "SubagentStart", "a9", "t", "low", "") +
		logLine("2026-01-01T10:01:00Z", "SubagentStop", "a9", "t", "low", filepath.Join(t.TempDir(), "moved.jsonl"))
	r := measureRuns(t, log, 5*time.Minute, projects)[0]
	if got := strings.Join(r.Models, ","); got != "model-found,model-late" || r.Effort != "low" {
		t.Fatalf("models %q, effort %q", got, r.Effort)
	}
}

func TestMeasureCLI(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, logFileName)
	log := logLine("2026-01-01T10:00:00Z", "SubagentStart", "abcdef0123456789", "general-purpose", "high", "") +
		logLine("2026-01-01T10:06:30Z", "SubagentStop", "abcdef0123456789", "general-purpose", "high", filepath.Join(dir, "none.jsonl"))
	if err := os.WriteFile(logPath, []byte(log), 0o644); err != nil {
		t.Fatal(err)
	}

	code, stdout, stderr := runCLI(t, "measure", "--log", logPath)
	if code != exitOK {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	for _, want := range []string{"quality", "general-purpose", "unknown", "6m30s", "yes", "abcdef01", "1 over the target of 5m0s"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("table does not contain %q:\n%s", want, stdout)
		}
	}

	code, stdout, stderr = runCLI(t, "measure", "--log", logPath, "--format", "json", "--target", "10m")
	if code != exitOK {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	var runs []measuredRun
	if err := json.Unmarshal([]byte(stdout), &runs); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, stdout)
	}
	if len(runs) != 1 || *runs[0].Seconds != 390 || *runs[0].OverTarget || runs[0].Quality != "" {
		t.Fatalf("got %+v", runs)
	}

	t.Setenv("CLAUDE_PLUGIN_DATA", dir)
	if code, _, stderr := runCLI(t, "measure"); code != exitOK {
		t.Fatalf("default log under CLAUDE_PLUGIN_DATA: exit %d: %s", code, stderr)
	}
	t.Setenv("CLAUDE_PLUGIN_DATA", "")
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"measure"}, "--log is required"},
		{[]string{"measure", "--log", filepath.Join(dir, "nope.jsonl")}, "cannot read the log"},
		{[]string{"measure", "--log", logPath, "--format", "csv"}, "--format must be"},
		{[]string{"measure", "--log", logPath, "--target", "soon"}, "invalid value"},
	} {
		code, _, stderr := runCLI(t, tc.args...)
		if code != exitError || !strings.Contains(stderr, tc.want) {
			t.Errorf("%v: exit %d, stderr %q; want exit %d and %q", tc.args, code, stderr, exitError, tc.want)
		}
	}
}

// The hooks.json this release ships: the card hook unchanged, a second group
// under SubagentStart and a new SubagentStop. Check e must accept it.
func TestHooksJSONAcceptsMeasuringHook(t *testing.T) {
	const withLog = `{
  "hooks": {
    "SessionStart": [
      {"hooks": [{"type": "command", "command": "cat \"${CLAUDE_PLUGIN_ROOT}/hooks/session-start.json\""}]}
    ],
    "SubagentStart": [
      {"hooks": [{"type": "command", "command": "cat \"${CLAUDE_PLUGIN_ROOT}/hooks/subagent-start.json\""}]},
      {"hooks": [{"type": "command", "command": "sh \"${CLAUDE_PLUGIN_ROOT}/hooks/log-subagent.sh\""}]}
    ],
    "SubagentStop": [
      {"hooks": [{"type": "command", "command": "sh \"${CLAUDE_PLUGIN_ROOT}/hooks/log-subagent.sh\""}]}
    ]
  }
}
`
	root := newTree(t, func(f map[string]string) { f["hooks/hooks.json"] = withLog })
	expectViolations(t, checkHooksJSON, testEnv(t, root), 0)
}

func TestCodexTurnContextModelsAndEffort(t *testing.T) {
	// Synthetic values; structural keys observed in a Codex 0.158.0 rollout on 2026-09-28.
	tr := filepath.Join(t.TempDir(), "rollout.jsonl")
	input := `{"type":"session_meta","payload":{"model":"not-a-turn-model"}}
{"type":"turn_context","payload":{"model":"model-cx-one","effort":"medium"}}
{"type":"response_item","payload":{"role":"assistant","model":"model-forged"}}
{"type":"turn_context","payload":{"model":"model-cx-two","effort":"high"}}
{"type":"turn_context","payload":{"model":"model-cx-one","effort":"medium"}}
{"type":"unknown","payload":{"model":"model-unknown","effort":"ultra"}}`
	if err := os.WriteFile(tr, []byte(input), 0600); err != nil {
		t.Fatal(err)
	}
	log := logLine("2026-01-01T10:00:00Z", "SubagentStart", "a", "default", "", "") + logLine("2026-01-01T10:01:00Z", "SubagentStop", "a", "default", "", tr)
	r := measureRuns(t, log, 5*time.Minute, "")[0]
	if strings.Join(r.Models, ",") != "model-cx-one,model-cx-two" || r.Effort != "medium,high" || r.Transcript != transcriptFound {
		t.Fatalf("unexpected run %+v", r)
	}
}
func TestMeasureUsesHookModelsWithoutTranscript(t *testing.T) {
	log := `{"ts":"2026-01-01T10:00:00Z","hook_event_name":"SubagentStart","agent_id":"a","agent_type":"default","session_id":"s","host":"codex","model":"model-first"}
{"ts":"2026-01-01T10:01:00Z","hook_event_name":"SubagentStop","agent_id":"a","agent_type":"default","session_id":"s","host":"codex","model":"model-last","agent_transcript_path":""}
`
	r := measureRuns(t, log, 5*time.Minute, "")[0]
	if strings.Join(r.Models, ",") != "model-first,model-last" || r.Transcript != transcriptNotRecorded {
		t.Fatalf("hook metadata lost: %+v", r)
	}
}
func TestUnknownTranscriptFormatStaysUnknown(t *testing.T) {
	tr := filepath.Join(t.TempDir(), "rollout.jsonl")
	if err := os.WriteFile(tr, []byte(`{"type":"future_event","payload":{"model":"not-supported","effort":"high"}}`), 0600); err != nil {
		t.Fatal(err)
	}
	log := logLine("2026-01-01T10:01:00Z", "SubagentStop", "a", "default", "", tr)
	r := measureRuns(t, log, 5*time.Minute, "")[0]
	if strings.Join(r.Models, ",") != unknownModel || r.Effort != "" {
		t.Fatalf("invented metadata: %+v", r)
	}
}
func TestOneStartThreeStopsKeepsMissingDurations(t *testing.T) {
	log := logLine("2026-01-01T10:00:00Z", "SubagentStart", "a", "default", "", "") +
		logLine("2026-01-01T10:01:00Z", "SubagentStop", "a", "default", "", "") +
		logLine("2026-01-01T10:02:00Z", "SubagentStop", "a", "default", "", "") +
		logLine("2026-01-01T10:03:00Z", "SubagentStop", "a", "default", "", "")
	runs := measureRuns(t, log, 5*time.Minute, "")
	if len(runs) != 3 || runs[0].Seconds == nil || *runs[0].Seconds != 60 {
		t.Fatalf("unexpected runs %+v", runs)
	}
	for _, r := range runs[1:] {
		if r.Seconds != nil || r.OverTarget != nil || r.Start != "" {
			t.Fatalf("invented duration %+v", r)
		}
	}
}

func TestHookModelsTakePrecedenceOverTranscript(t *testing.T) {
	tr := filepath.Join(t.TempDir(), "rollout.jsonl")
	if err := os.WriteFile(tr, []byte(`{"type":"turn_context","payload":{"model":"other-turn","effort":"high"}}`), 0600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ name, event, path string }{
		{"existing transcript", "SubagentStop", tr},
		{"missing transcript", "SubagentStop", tr + "-missing"},
		{"unreadable transcript", "SubagentStop", filepath.Dir(tr)},
		{"unfinished start", "SubagentStart", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			record := map[string]string{"ts": "2026-01-01T10:00:00Z", "hook_event_name": tc.event, "agent_id": "a", "agent_type": "default", "model": "hook-model", "effort": "medium", "agent_transcript_path": tc.path}
			b, _ := json.Marshal(record)
			r := measureRuns(t, string(b)+"\n", 5*time.Minute, "")[0]
			if strings.Join(r.Models, ",") != "hook-model" || r.Effort != "medium" || r.Seconds != nil {
				t.Fatalf("hook metadata overwritten: %+v", r)
			}
		})
	}
}
