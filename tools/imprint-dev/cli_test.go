package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// runCLI calls run and captures its output.
func runCLI(t *testing.T, args ...string) (code int, stdout, stderr string) {
	t.Helper()
	var out, errOut bytes.Buffer
	code = run(args, &out, &errOut)
	return code, out.String(), errOut.String()
}

func TestRunExitCodes(t *testing.T) {
	valid := newTree(t, nil)
	broken := newTree(t, func(f map[string]string) { f[".claude-plugin/plugin.json"] = `{"version": "1.2"}` })
	file := filepath.Join(t.TempDir(), "plain-file")
	if err := os.WriteFile(file, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name string
		args []string
		code int
		want string // in stderr
	}{
		{"valid tree", []string{"check", "--root", valid}, exitOK, "result: 0 violation(s)"},
		{"violation", []string{"check", "--root", broken}, exitViolation, "FAIL   f plugin-version"},
		{"root does not exist", []string{"check", "--root", filepath.Join(t.TempDir(), "nowhere")}, exitError, "cannot read the plugin root"},
		{"root is a file", []string{"check", "--root", file}, exitError, "not a directory"},
		{"no arguments", nil, exitError, "usage"},
		{"unknown subcommand", []string{"lint"}, exitError, "unknown subcommand"},
		{"stray argument", []string{"gen", "--root", valid, "extra"}, exitError, "unexpected argument"},
		{"unknown flag", []string{"check", "--nope"}, exitError, "flag provided but not defined"},
		{"gen without a card", []string{"gen", "--root", t.TempDir()}, exitError, "not found"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			code, _, stderr := runCLI(t, tc.args...)
			if code != tc.code {
				t.Fatalf("exit %d, want %d; stderr:\n%s", code, tc.code, stderr)
			}
			if !strings.Contains(stderr, tc.want) {
				t.Fatalf("stderr does not contain %q:\n%s", tc.want, stderr)
			}
		})
	}
}

func TestRunValidSummary(t *testing.T) {
	code, _, stderr := runCLI(t, "check", "--root", newTree(t, nil))
	if code != exitOK {
		t.Fatalf("exit %d:\n%s", code, stderr)
	}
	for _, want := range []string{"ok     a skill-description-length", "0 not checkable"} {
		if !strings.Contains(stderr, want) {
			t.Errorf("stderr does not contain %q:\n%s", want, stderr)
		}
	}
}

func TestRunGen(t *testing.T) {
	root := t.TempDir()
	writeFiles(t, root, map[string]string{"hooks/kernkarte.md": testCard})
	code, stdout, stderr := runCLI(t, "gen", "--root", root)
	if code != exitOK {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	for _, p := range []string{sessionStartPath, subagentStartPath} {
		if !strings.Contains(stdout, "wrote "+p) {
			t.Errorf("stdout does not name %s:\n%s", p, stdout)
		}
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(p))); err != nil {
			t.Error(err)
		}
	}
}

func TestRunSARIF(t *testing.T) {
	root := newTree(t, func(f map[string]string) {
		f[".claude-plugin/plugin.json"] = `{"version": "1.2"}`
		f["README.md"] = "# fixture\n\nsee knowledge-ages\n"
	})
	out := filepath.Join(t.TempDir(), "out.sarif")
	code, _, stderr := runCLI(t, "check", "--root", root, "--sarif", out)
	if code != exitViolation {
		t.Fatalf("exit %d:\n%s", code, stderr)
	}
	raw, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	var log struct {
		Version string `json:"version"`
		Runs    []struct {
			Tool struct {
				Driver struct {
					Name  string `json:"name"`
					Rules []struct {
						ID string `json:"id"`
					} `json:"rules"`
				} `json:"driver"`
			} `json:"tool"`
			Invocations []struct {
				ExecutionSuccessful bool `json:"executionSuccessful"`
			} `json:"invocations"`
			Results []struct {
				RuleID    string `json:"ruleId"`
				RuleIndex int    `json:"ruleIndex"`
				Kind      string `json:"kind"`
				Level     string `json:"level"`
				Locations []struct {
					PhysicalLocation struct {
						ArtifactLocation struct {
							URI string `json:"uri"`
						} `json:"artifactLocation"`
						Region *struct {
							StartLine int `json:"startLine"`
						} `json:"region"`
					} `json:"physicalLocation"`
				} `json:"locations"`
			} `json:"results"`
		} `json:"runs"`
	}
	if err := json.Unmarshal(raw, &log); err != nil {
		t.Fatal(err)
	}
	if log.Version != "2.1.0" || len(log.Runs) != 1 {
		t.Fatalf("version %q, %d runs", log.Version, len(log.Runs))
	}
	r := log.Runs[0]
	if r.Tool.Driver.Name != "imprint-dev" {
		t.Fatalf("driver %q", r.Tool.Driver.Name)
	}
	if len(r.Tool.Driver.Rules) != len(checks) {
		t.Fatalf("%d rules, want one per check (%d)", len(r.Tool.Driver.Rules), len(checks))
	}
	for i, c := range checks {
		if r.Tool.Driver.Rules[i].ID != c.ID {
			t.Fatalf("rule %d is %q, want %q", i, r.Tool.Driver.Rules[i].ID, c.ID)
		}
	}
	if len(r.Invocations) != 1 || !r.Invocations[0].ExecutionSuccessful {
		t.Fatalf("invocations %+v", r.Invocations)
	}
	type key struct{ rule, kind, level, uri string }
	got := map[key]int{}
	for _, res := range r.Results {
		if r.Tool.Driver.Rules[res.RuleIndex].ID != res.RuleID {
			t.Errorf("ruleIndex %d does not point at %s", res.RuleIndex, res.RuleID)
		}
		uri := ""
		if len(res.Locations) == 1 {
			uri = res.Locations[0].PhysicalLocation.ArtifactLocation.URI
			if reg := res.Locations[0].PhysicalLocation.Region; reg != nil {
				uri += ":" + string(rune('0'+reg.StartLine))
			}
		}
		got[key{res.RuleID, res.Kind, res.Level, uri}]++
	}
	want := map[key]int{
		{"plugin-version", "fail", "error", ".claude-plugin/plugin.json"}: 1,
		{"removed-skill-reference", "fail", "error", "README.md:3"}:       1,
	}
	if len(got) != len(want) {
		t.Fatalf("results %v, want %v", got, want)
	}
	for k, n := range want {
		if got[k] != n {
			t.Fatalf("results %v, want %v", got, want)
		}
	}
}

func TestRunSARIFWhenTheRootCannotBeRead(t *testing.T) {
	out := filepath.Join(t.TempDir(), "out.sarif")
	code, _, _ := runCLI(t, "check", "--root", filepath.Join(t.TempDir(), "nowhere"), "--sarif", out)
	if code != exitError {
		t.Fatalf("exit %d", code)
	}
	raw, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"executionSuccessful": false`) {
		t.Fatalf("SARIF does not record the failed run:\n%s", raw)
	}
}

func TestRunSARIFUnwritableIsAnError(t *testing.T) {
	out := filepath.Join(t.TempDir(), "missing-dir", "out.sarif")
	if code, _, _ := runCLI(t, "check", "--root", newTree(t, nil), "--sarif", out); code != exitError {
		t.Fatalf("exit %d, want %d", code, exitError)
	}
}
