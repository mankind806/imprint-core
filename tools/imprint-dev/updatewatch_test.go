package main

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// These tests run the real hooks/update-watch.sh - the third exception to the
// rule that no test reads the repository - against a fake claude on PATH that
// prints a fixed version, and a temporary CLAUDE_PLUGIN_DATA. Without an sh on
// PATH they are skipped, and say so.

var updateWatchScript = filepath.Join("..", "..", "hooks", "update-watch.sh")

const (
	versionFile = "claude-code-version"
	historyFile = "claude-code-version-history"
)

// fakeClaude writes an executable claude into a new directory that prints
// output for --version and exits with code, and returns the directory.
func fakeClaude(t *testing.T, output string, code int) string {
	t.Helper()
	dir := t.TempDir()
	script := "#!/bin/sh\nprintf '%s\\n' '" + output + "'\nexit " + strconv.Itoa(code) + "\n"
	if err := os.WriteFile(filepath.Join(dir, "claude"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return dir
}

// hookTools returns a new directory holding a link to each tool the script
// needs besides sh, found on the caller's PATH, so that PATH can be built from
// it and a fake claude alone. Without one of them the test is skipped.
func hookTools(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for _, name := range []string{"sed", "grep", "date", "mkdir", "mv", "rm"} {
		p, err := exec.LookPath(name)
		if err != nil {
			t.Skip("no " + name + " on PATH, so the hook script cannot be run here: " + err.Error())
		}
		if err := os.Symlink(p, filepath.Join(dir, name)); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// runUpdateWatch runs the script with binDir in front of the tools it needs
// on PATH ("" puts no claude there), dataDir as CLAUDE_PLUGIN_DATA ("" leaves
// it unset) and input on stdin. It fails the test unless the script exits 0
// and writes nothing to stderr, and returns what it printed.
func runUpdateWatch(t *testing.T, binDir, dataDir, input string) string {
	t.Helper()
	sh, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("no sh on PATH, so the hook script cannot be run here: " + err.Error())
	}
	path := hookTools(t)
	if binDir != "" {
		path = binDir + ":" + path
	}
	env := []string{"PATH=" + path, "LC_ALL=C"}
	if dataDir != "" {
		env = append(env, "CLAUDE_PLUGIN_DATA="+dataDir)
	}
	cmd := exec.Command(sh, updateWatchScript)
	cmd.Env = env
	cmd.Stdin = strings.NewReader(input)
	var out, errOut bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errOut
	if err := cmd.Run(); err != nil {
		t.Fatalf("the hook must exit 0: %v", err)
	}
	if errOut.Len() > 0 {
		t.Fatalf("the hook must write nothing to stderr: %q", errOut.String())
	}
	return out.String()
}

func recordedVersion(t *testing.T, dataDir string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(dataDir, versionFile))
	if os.IsNotExist(err) {
		return ""
	}
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

// history returns the versions in the history file, without their dates, and
// fails the test on a line that is not a date and a version.
func history(t *testing.T, dataDir string) []string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(dataDir, historyFile))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	var versions []string
	for _, l := range strings.Split(strings.TrimSuffix(string(raw), "\n"), "\n") {
		day, v, ok := strings.Cut(l, " ")
		if !ok || !dayShape.MatchString(day) {
			t.Fatalf("history line %q is not a UTC date and a version", l)
		}
		versions = append(versions, v)
	}
	return versions
}

var dayShape = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)

func record(t *testing.T, dataDir, v string) {
	t.Helper()
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dataDir, versionFile), []byte(v), 0o644); err != nil {
		t.Fatal(err)
	}
}

// contextOf parses the hook's output as SessionStart JSON and returns its
// additionalContext.
func contextOf(t *testing.T, out string) string {
	t.Helper()
	var doc struct {
		HookSpecificOutput struct {
			HookEventName     string `json:"hookEventName"`
			AdditionalContext string `json:"additionalContext"`
		} `json:"hookSpecificOutput"`
	}
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("output is not JSON: %v\n%s", err, out)
	}
	if doc.HookSpecificOutput.HookEventName != "SessionStart" {
		t.Fatalf("hookEventName %q, want SessionStart", doc.HookSpecificOutput.HookEventName)
	}
	return doc.HookSpecificOutput.AdditionalContext
}

const startupInput = `{"session_id":"s","hook_event_name":"SessionStart","source":"startup"}`

// The guard test O24 asks for: the same fixed version twice, once after a
// change and once after that. The first run speaks, the second is silent.
func TestUpdateWatchSpeaksOnceAfterAChange(t *testing.T) {
	bin := fakeClaude(t, "2.1.283 (Claude Code)", 0)
	dir := filepath.Join(t.TempDir(), "odd \"data\" \\ dir")
	record(t, dir, "2.1.282\n")

	ctx := contextOf(t, runUpdateWatch(t, bin, dir, startupInput))
	for _, want := range []string{
		"injected by the imprint plugin",
		"Claude Code changed from 2.1.282 to 2.1.283",
		"dispatch one read-only subagent",
		"CHANGELOG.md",
		"llms.txt",
		filepath.Join(dir, "update-reports", "2.1.283.md"),
		"Use, Adapt, Drop and Nothing to do",
	} {
		if !strings.Contains(ctx, want) {
			t.Errorf("the context lacks %q:\n%s", want, ctx)
		}
	}
	if got := recordedVersion(t, dir); got != "2.1.283\n" {
		t.Errorf("recorded %q, want the new version", got)
	}

	if got := history(t, dir); len(got) != 1 || got[0] != "2.1.283" {
		t.Errorf("history %v, want [2.1.283]", got)
	}

	if out := runUpdateWatch(t, bin, dir, startupInput); out != "" {
		t.Fatalf("the second run must be silent, printed %q", out)
	}
	if got := recordedVersion(t, dir); got != "2.1.283\n" {
		t.Errorf("recorded %q after the second run", got)
	}
	if got := history(t, dir); len(got) != 1 {
		t.Errorf("history %v after the second run; an unchanged version adds no line", got)
	}

	// The next update keeps the earlier version findable.
	if out := runUpdateWatch(t, fakeClaude(t, "2.1.284 (Claude Code)", 0), dir, startupInput); !strings.Contains(out, "from 2.1.283 to 2.1.284") {
		t.Fatalf("the next update must speak again, printed %q", out)
	}
	if got := history(t, dir); len(got) != 2 || got[0] != "2.1.283" || got[1] != "2.1.284" {
		t.Errorf("history %v, want [2.1.283 2.1.284]", got)
	}
}

func TestUpdateWatchFirstRunOnlyRecords(t *testing.T) {
	bin := fakeClaude(t, "2.1.283 (Claude Code)", 0)
	dir := filepath.Join(t.TempDir(), "not", "yet", "there")
	if out := runUpdateWatch(t, bin, dir, startupInput); out != "" {
		t.Fatalf("a first run must be silent, printed %q", out)
	}
	if got := recordedVersion(t, dir); got != "2.1.283\n" {
		t.Errorf("recorded %q, want 2.1.283", got)
	}
	if got := history(t, dir); len(got) != 1 || got[0] != "2.1.283" {
		t.Errorf("history %v, want [2.1.283]", got)
	}
}

func TestUpdateWatchLeavesAnUnreadableRecordAlone(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root, so a file cannot be made unreadable here")
	}
	dir := t.TempDir()
	record(t, dir, "2.1.282\n")
	f := filepath.Join(dir, versionFile)
	if err := os.Chmod(f, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(f, 0o644) })
	if out := runUpdateWatch(t, fakeClaude(t, "2.1.283 (Claude Code)", 0), dir, startupInput); out != "" {
		t.Fatalf("printed %q", out)
	}
	if err := os.Chmod(f, 0o644); err != nil {
		t.Fatal(err)
	}
	if got := recordedVersion(t, dir); got != "2.1.282\n" {
		t.Errorf("recorded %q; an unreadable record must not be overwritten", got)
	}
}

// A history that cannot be written holds the record back too, so the next
// start tries again rather than recording a version the history lacks.
func TestUpdateWatchKeepsRecordAndHistoryInStep(t *testing.T) {
	dir := t.TempDir()
	record(t, dir, "2.1.282\n")
	if err := os.Mkdir(filepath.Join(dir, historyFile), 0o755); err != nil {
		t.Fatal(err)
	}
	if out := runUpdateWatch(t, fakeClaude(t, "2.1.283 (Claude Code)", 0), dir, startupInput); out != "" {
		t.Fatalf("printed %q", out)
	}
	if got := recordedVersion(t, dir); got != "2.1.282\n" {
		t.Errorf("recorded %q; without a history line the record must stay", got)
	}
	matches, _ := filepath.Glob(filepath.Join(dir, versionFile+".tmp.*"))
	if len(matches) > 0 {
		t.Errorf("left temporary files behind: %v", matches)
	}
}

// A downgrade is recorded without a notice, so no range runs backwards, and
// the next upgrade is read from the version actually in use.
func TestUpdateWatchRecordsADowngradeSilently(t *testing.T) {
	for _, tc := range []struct{ old, new string }{
		{"2.1.283", "2.1.282"},
		{"2.2.0", "2.1.999"},
		{"3.0.0", "2.9.9"},
		{"2.1.283", "2.1.283-beta.1"},
	} {
		t.Run(tc.old+" to "+tc.new, func(t *testing.T) {
			dir := t.TempDir()
			record(t, dir, tc.old+"\n")
			if out := runUpdateWatch(t, fakeClaude(t, tc.new+" (Claude Code)", 0), dir, startupInput); out != "" {
				t.Fatalf("printed %q", out)
			}
			if got := recordedVersion(t, dir); got != tc.new+"\n" {
				t.Errorf("recorded %q, want %s", got, tc.new)
			}
		})
	}
	t.Run("then an upgrade reads from there", func(t *testing.T) {
		dir := t.TempDir()
		record(t, dir, "2.1.282\n")
		out := runUpdateWatch(t, fakeClaude(t, "2.1.10 (Claude Code)", 0), dir, startupInput)
		if out != "" {
			t.Fatalf("printed %q", out)
		}
		out = runUpdateWatch(t, fakeClaude(t, "2.1.284 (Claude Code)", 0), dir, startupInput)
		if !strings.Contains(contextOf(t, out), "from 2.1.10 to 2.1.284") {
			t.Fatalf("printed %q", out)
		}
	})
}

func TestUpdateWatchTreatsAJunkRecordAsAFirstRun(t *testing.T) {
	for name, junk := range map[string]string{
		"a path":                   "../../etc\n2.1.282\n",
		"a version, then junk":     "2.1.282\ntrailing junk\n",
		"a version, then unending": "2.1.282\nx",
		"words after the version":  "2.1.282 and more\n",
	} {
		t.Run(name, func(t *testing.T) {
			bin := fakeClaude(t, "2.1.283 (Claude Code)", 0)
			dir := t.TempDir()
			record(t, dir, junk)
			if out := runUpdateWatch(t, bin, dir, startupInput); out != "" {
				t.Fatalf("a junk record must not reach the context, printed %q", out)
			}
			if got := recordedVersion(t, dir); got != "2.1.283\n" {
				t.Errorf("recorded %q, want 2.1.283", got)
			}
		})
	}
}

func TestUpdateWatchLeavesCompactionAlone(t *testing.T) {
	bin := fakeClaude(t, "2.1.283 (Claude Code)", 0)
	dir := t.TempDir()
	record(t, dir, "2.1.282\n")
	input := `{"session_id":"s","hook_event_name":"SessionStart","source":"compact"}`
	if out := runUpdateWatch(t, bin, dir, input); out != "" {
		t.Fatalf("a start after compaction must be silent, printed %q", out)
	}
	if got := recordedVersion(t, dir); got != "2.1.282\n" {
		t.Errorf("recorded %q; compaction must leave the record for the next start", got)
	}
}

func TestUpdateWatchStaysSilentOnFailure(t *testing.T) {
	for name, tc := range map[string]struct {
		output string
		code   int
	}{
		"claude fails":                 {"2.1.283 (Claude Code)", 1},
		"not a version":                {"command not found", 0},
		"a path instead of a version":  {"../x (Claude Code)", 0},
		"a quote inside the version":   {`2.1.283"x (Claude Code)`, 0},
		"a version with a slash after": {"2.1.283/../x", 0},
		"a number too long to compare": {"2.1.9999999999999999999999 (Claude Code)", 0},
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			record(t, dir, "2.1.282\n")
			if out := runUpdateWatch(t, fakeClaude(t, tc.output, tc.code), dir, startupInput); out != "" {
				t.Fatalf("printed %q", out)
			}
			if got := recordedVersion(t, dir); got != "2.1.282\n" {
				t.Errorf("recorded %q; a failed read must leave the record alone", got)
			}
		})
	}
	t.Run("no claude on PATH", func(t *testing.T) {
		dir := t.TempDir()
		record(t, dir, "2.1.282\n")
		if out := runUpdateWatch(t, "", dir, startupInput); out != "" {
			t.Fatalf("printed %q", out)
		}
		if got := recordedVersion(t, dir); got != "2.1.282\n" {
			t.Errorf("recorded %q", got)
		}
	})
	t.Run("date fails", func(t *testing.T) {
		bin := fakeClaude(t, "2.1.283 (Claude Code)", 0)
		if err := os.WriteFile(filepath.Join(bin, "date"), []byte("#!/bin/sh\nexit 1\n"), 0o755); err != nil {
			t.Fatal(err)
		}
		dir := t.TempDir()
		record(t, dir, "2.1.282\n")
		if out := runUpdateWatch(t, bin, dir, startupInput); out != "" {
			t.Fatalf("printed %q", out)
		}
		if got := recordedVersion(t, dir); got != "2.1.282\n" {
			t.Errorf("recorded %q; a failed date must leave the record alone", got)
		}
		if got := history(t, dir); len(got) != 0 {
			t.Errorf("history %v; a failed date must write no line", got)
		}
	})
	t.Run("CLAUDE_PLUGIN_DATA unset", func(t *testing.T) {
		if out := runUpdateWatch(t, fakeClaude(t, "2.1.283 (Claude Code)", 0), "", startupInput); out != "" {
			t.Fatalf("printed %q", out)
		}
	})
	t.Run("unwritable data directory", func(t *testing.T) {
		file := filepath.Join(t.TempDir(), "a-file")
		if err := os.WriteFile(file, nil, 0o644); err != nil {
			t.Fatal(err)
		}
		if out := runUpdateWatch(t, fakeClaude(t, "2.1.283 (Claude Code)", 0), filepath.Join(file, "below"), startupInput); out != "" {
			t.Fatalf("printed %q", out)
		}
	})
}

// The hook only works while hooks/hooks.json runs it at SessionStart, and the
// first answer waits for it, so its timeout must stay at 10 seconds.
func TestUpdateWatchIsRegistered(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", hooksPath))
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Hooks map[string][]struct {
			Hooks []struct {
				Type    string `json:"type"`
				Command string `json:"command"`
				Timeout *int   `json:"timeout"`
			} `json:"hooks"`
		} `json:"hooks"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	const want = `sh "${CLAUDE_PLUGIN_ROOT}/hooks/update-watch.sh"`
	for _, g := range doc.Hooks["SessionStart"] {
		for _, h := range g.Hooks {
			if h.Command != want {
				continue
			}
			if h.Type != "command" {
				t.Errorf("type %q, want command", h.Type)
			}
			if h.Timeout == nil || *h.Timeout != 10 {
				t.Errorf("timeout %v, want 10", h.Timeout)
			}
			return
		}
	}
	t.Fatalf("hooks/hooks.json does not run %s at SessionStart", want)
}
