package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Every fixture is built in t.TempDir(). No test reads the real repository,
// except TestRealCardPassesSizeCheck, which exists to do exactly that.

const testCard = "First line of the fixture card.\nSecond line.\n"

const validHooksJSON = `{
  "hooks": {
    "SessionStart": [
      {"hooks": [{"type": "command", "command": "cat \"${CLAUDE_PLUGIN_ROOT}/hooks/session-start.json\""}]}
    ],
    "SubagentStart": [
      {"hooks": [{"type": "command", "command": "cat \"${CLAUDE_PLUGIN_ROOT}/hooks/subagent-start.json\""}]}
    ]
  }
}
`

func skillFile(name, description string) string {
	return "---\nname: " + name + "\ndescription: \"" + description + "\"\n---\n\n# " + name + "\n"
}

// testToday is the fixed "today" every check runs against in tests; no test
// reads the clock.
const testToday = "2026-06-15"

// validEnforcementTable uses each of the four states once, in the spellings
// the skills use.
const validEnforcementTable = `# Reference

| Rule | Enforcement |
|---|---|
| One | **Enforced** by a hook. |
| Two | **Enforceable, not enforced.** Nothing runs it; **not measured here**. |
| Three | **Behaviour rule**. |
| Four | **Reserved to a person.** |
`

// validFiles is a plugin tree that passes every check.
func validFiles(t *testing.T) map[string]string {
	t.Helper()
	files := map[string]string{
		"skills/alpha/SKILL.md":                 skillFile("alpha", "Short description."),
		"skills/beta/SKILL.md":                  skillFile("beta", "Another short description."),
		"skills/beta/references/enforcement.md": validEnforcementTable,
		"agents/reader.md":                      "---\nname: reader\n---\nReads things.\n",
		"hooks/kernkarte.md":                    testCard,
		"hooks/hooks.json":                      validHooksJSON,
		".claude-plugin/plugin.json":            `{"name": "fixture", "version": "1.2.3"}`,
		"README.md":                             "# fixture\n",
	}
	for _, h := range hookTargets {
		data, err := renderHook(h.Event, cardText([]byte(testCard)))
		if err != nil {
			t.Fatal(err)
		}
		files[h.Path] = string(data)
	}
	return files
}

func writeFiles(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for rel, content := range files {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// newTree writes a valid tree, changed by mutate first, and returns its root.
func newTree(t *testing.T, mutate func(files map[string]string)) string {
	t.Helper()
	files := validFiles(t)
	if mutate != nil {
		mutate(files)
	}
	root := t.TempDir()
	writeFiles(t, root, files)
	return root
}

// testEnv wraps root in an env for a check to run against.
func testEnv(t *testing.T, root string) *env {
	t.Helper()
	return &env{Root: root, Today: testToday}
}

func violations(fs []finding) []finding {
	var out []finding
	for _, f := range fs {
		if f.Kind == violation {
			out = append(out, f)
		}
	}
	return out
}

// expectViolations runs one check and compares the number of violations. Each
// entry of want must occur in some violation's "path:line: message" text.
func expectViolations(t *testing.T, run func(*env) (checkResult, error), e *env, n int, want ...string) []finding {
	t.Helper()
	res, err := run(e)
	if err != nil {
		t.Fatalf("check could not run: %v", err)
	}
	got := violations(res.Findings)
	var texts []string
	for _, f := range got {
		texts = append(texts, f.where()+f.Message)
	}
	if len(got) != n {
		t.Fatalf("got %d violation(s), want %d:\n%s", len(got), n, strings.Join(texts, "\n"))
	}
	all := strings.Join(texts, "\n")
	for _, w := range want {
		if !strings.Contains(all, w) {
			t.Errorf("no violation mentions %q; got:\n%s", w, all)
		}
	}
	return got
}
