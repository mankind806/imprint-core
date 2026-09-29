package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidTreePassesEveryCheck(t *testing.T) {
	rep := runChecks(testEnv(t, newTree(t, nil)))
	if rep.Fatal != nil {
		t.Fatal(rep.Fatal)
	}
	if len(rep.Outcomes) != len(checks) {
		t.Fatalf("%d outcomes for %d checks", len(rep.Outcomes), len(checks))
	}
	for _, o := range rep.Outcomes {
		if o.Err != nil || len(violations(o.Result.Findings)) != 0 {
			t.Errorf("%s: err=%v findings=%+v", o.Check.ID, o.Err, o.Result.Findings)
		}
	}
	if code := rep.exitCode(); code != exitOK {
		t.Fatalf("exit %d", code)
	}
}

func TestCheckIDsAreUnique(t *testing.T) {
	seen := map[string]bool{}
	for _, c := range checks {
		if seen[c.ID] || seen[c.Letter] {
			t.Fatalf("duplicate check %s/%s", c.Letter, c.ID)
		}
		seen[c.ID], seen[c.Letter] = true, true
	}
}

// --- a -----------------------------------------------------------------------

func TestCheckDescriptions(t *testing.T) {
	onlySkills := func(n, length int) func(map[string]string) {
		return func(f map[string]string) {
			delete(f, "skills/alpha/SKILL.md")
			delete(f, "skills/beta/SKILL.md")
			for i := 0; i < n; i++ {
				name := fmt.Sprintf("s%d", i)
				f["skills/"+name+"/SKILL.md"] = skillFile(name, strings.Repeat("x", length))
			}
		}
	}
	setAlpha := func(content string) func(map[string]string) {
		return func(f map[string]string) { f["skills/alpha/SKILL.md"] = content }
	}
	tests := []struct {
		name   string
		mutate func(map[string]string)
		n      int
		want   []string
	}{
		{"valid", nil, 0, nil},
		{"exactly 400", setAlpha(skillFile("alpha", strings.Repeat("x", 400))), 0, nil},
		{"401", setAlpha(skillFile("alpha", strings.Repeat("x", 401))), 1, []string{"skills/alpha/SKILL.md:3: description is 401 characters"}},
		{"400 two-byte characters count as 400", setAlpha(skillFile("alpha", strings.Repeat("\xc3\xa4", 400))), 0, nil},
		{"401 two-byte characters", setAlpha(skillFile("alpha", strings.Repeat("\xc3\xa4", 401))), 1, []string{"401 characters"}},
		{"an escaped quote is one character", setAlpha(skillFile("alpha", strings.Repeat(`\"`, 400))), 0, nil},
		{"sum exactly 3000", onlySkills(10, 300), 0, nil},
		{"sum over 3000", onlySkills(8, 399), 1, []string{"skills: the descriptions of 8 skills add up to 3192 characters"}},
		{"one long and the sum", onlySkills(8, 401), 9, []string{"s0/SKILL.md:3", "3208 characters"}},
		{"no frontmatter", setAlpha("# alpha\n"), 1, []string{"no frontmatter"}},
		{"no description", setAlpha("---\nname: alpha\n---\n"), 1, []string{"no description"}},
		{"no skills directory", func(f map[string]string) {
			delete(f, "skills/alpha/SKILL.md")
			delete(f, "skills/beta/SKILL.md")
		}, 0, nil},
		{"stray file and empty dir are ignored", func(f map[string]string) {
			f["skills/notes.md"] = "not a skill"
			f["skills/empty/.keep"] = ""
		}, 0, nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			expectViolations(t, checkDescriptions, testEnv(t, newTree(t, tc.mutate)), tc.n, tc.want...)
		})
	}
}

// --- b -----------------------------------------------------------------------

func TestCheckCardSize(t *testing.T) {
	card := func(s string) func(map[string]string) {
		return func(f map[string]string) { f["hooks/kernkarte.md"] = s }
	}
	lines := func(n int) string { return strings.Repeat("a line\n", n) }
	tests := []struct {
		name   string
		mutate func(map[string]string)
		n      int
		want   []string
	}{
		{"valid", nil, 0, nil},
		{"exactly 4000 characters", card(strings.Repeat("y", 3999) + "\n"), 0, nil},
		{"4001 characters", card(strings.Repeat("y", 4000) + "\n"), 1, []string{"4001 characters"}},
		{"4000 two-byte characters", card(strings.Repeat("\xc3\xbc", 4000)), 0, nil},
		{"12 non-empty lines", card(lines(12)), 0, nil},
		{"12 non-empty lines with blank ones between", card(strings.Repeat("a line\n\n  \n", 12)), 0, nil},
		{"13 non-empty lines", card(lines(13)), 1, []string{"13 non-empty lines"}},
		{"CRLF lines are counted once", card(strings.Repeat("a line\r\n", 12)), 0, nil},
		{"invalid UTF-8", card("bad \xff byte\n"), 1, []string{"UTF-8"}},
		{"missing", func(f map[string]string) { delete(f, "hooks/kernkarte.md") }, 1, []string{"missing"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			expectViolations(t, checkCardSize, testEnv(t, newTree(t, tc.mutate)), tc.n, tc.want...)
		})
	}
}

// TestRealCardPassesSizeCheck reads this repository's own card, on purpose.
func TestRealCardPassesSizeCheck(t *testing.T) {
	root := filepath.Join("..", "..")
	if _, err := os.Stat(filepath.Join(root, cardPath)); err != nil {
		t.Fatalf("the repository's card is expected two levels up: %v", err)
	}
	expectViolations(t, checkCardSize, &env{Root: root}, 0)
}

// --- c -----------------------------------------------------------------------

func TestCheckCardGenerated(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(map[string]string)
		n      int
		want   []string
	}{
		{"valid", nil, 0, nil},
		{"card edited without gen", func(f map[string]string) { f["hooks/kernkarte.md"] = "A new card.\n" }, 2,
			[]string{sessionStartPath + ": differs", subagentStartPath + ": differs"}},
		{"payload edited by hand", func(f map[string]string) { f[subagentStartPath] += " " }, 1, []string{subagentStartPath}},
		{"payload missing", func(f map[string]string) { delete(f, sessionStartPath) }, 1, []string{sessionStartPath + ": missing"}},
		{"trailing newline missing", func(f map[string]string) {
			f[sessionStartPath] = strings.TrimSuffix(f[sessionStartPath], "\n")
		}, 1, []string{sessionStartPath}},
		{"card missing", func(f map[string]string) { delete(f, "hooks/kernkarte.md") }, 1, []string{cardPath + ": missing"}},
		{"card with CRLF still matches", func(f map[string]string) {
			f["hooks/kernkarte.md"] = strings.ReplaceAll(testCard, "\n", "\r\n")
		}, 0, nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			expectViolations(t, checkCardGenerated, testEnv(t, newTree(t, tc.mutate)), tc.n, tc.want...)
		})
	}
}

// --- d -----------------------------------------------------------------------

func TestCheckRemovedSkills(t *testing.T) {
	add := func(path, content string) func(map[string]string) {
		return func(f map[string]string) { f[path] = content }
	}
	tests := []struct {
		name   string
		mutate func(map[string]string)
		n      int
		want   []string
	}{
		{"valid", nil, 0, nil},
		{"in a skill body", add("skills/alpha/SKILL.md", skillFile("alpha", "Short.")+"See knowledge-ages for more.\n"), 1,
			[]string{`skills/alpha/SKILL.md:7: refers to the removed skill "knowledge-ages"`}},
		{"in a skill description", add("skills/alpha/SKILL.md", skillFile("alpha", "Not for this (use one-canonical-place).")), 1,
			[]string{"skills/alpha/SKILL.md:3"}},
		{"in agents", add("agents/reader.md", "use blind-first-pass\n"), 1, []string{"agents/reader.md:1"}},
		{"in hooks", add("hooks/extra.md", "x\nsupersede-dont-delete\n"), 1, []string{"hooks/extra.md:2"}},
		{"in the plugin manifest dir", add(".claude-plugin/marketplace.json", `{"description": "provenance-on-entry"}`), 1,
			[]string{".claude-plugin/marketplace.json:1"}},
		{"in the README", add("README.md", "# fixture\n\nOld: one-canonical-place.\n"), 1, []string{"README.md:3"}},
		{"nested file", add("skills/alpha/references/deep.md", "knowledge-ages\n"), 1, []string{"skills/alpha/references/deep.md:1"}},
		{"path reference", add("README.md", "see skills/blind-first-pass/SKILL.md\n"), 1, []string{"blind-first-pass"}},
		{"case-insensitive", add("README.md", "The Blind-First-Pass rule\n"), 1, nil},
		{"two names on one line", add("README.md", "knowledge-ages and one-canonical-place\n"), 2, nil},
		{"same name twice on one line is one finding", add("README.md", "knowledge-ages, knowledge-ages\n"), 1, nil},
		{"migration note with formerly", add("README.md", "knowledge-keeping (formerly one-canonical-place)\n"), 0, nil},
		{"migration note with Merged", add("README.md", "Merged into knowledge-keeping: supersede-dont-delete, provenance-on-entry\n"), 0, nil},
		{"merger is not merged", add("README.md", "the merger of knowledge-ages\n"), 1, nil},
		{"longer name is another word", add("README.md", "knowledge-ages-extra and preknowledge-ages and knowledge_ages\n"), 0, nil},
		{"outside the scanned places", func(f map[string]string) {
			f["tools/x/main.go"] = "// knowledge-ages\n"
			f[".github/workflows/ci.yml"] = "# one-canonical-place\n"
		}, 0, nil},
		{"in docs and CONTRIBUTING", func(f map[string]string) {
			f["docs/old.md"] = "one-canonical-place\n"
			f["CONTRIBUTING.md"] = "knowledge-ages\n"
		}, 2, []string{"docs/old.md:1", "CONTRIBUTING.md:1"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			expectViolations(t, checkRemovedSkills, testEnv(t, newTree(t, tc.mutate)), tc.n, tc.want...)
		})
	}
}

// --- e -----------------------------------------------------------------------

func TestCheckHooksJSON(t *testing.T) {
	hooks := func(s string) func(map[string]string) {
		return func(f map[string]string) { f["hooks/hooks.json"] = s }
	}
	const sub = `"SubagentStart": [{"hooks": [{"type": "command", "command": "cat \"${CLAUDE_PLUGIN_ROOT}/x\""}]}]`
	const sess = `"SessionStart": [{"hooks": [{"type": "command", "command": "cat \"${CLAUDE_PLUGIN_ROOT}/x\""}]}]`
	const stop = `"SubagentStop": [{"hooks": [{"type": "command", "command": "sh \"${CLAUDE_PLUGIN_ROOT}/hooks/log-subagent.sh\""}]}]`
	tests := []struct {
		name   string
		mutate func(map[string]string)
		n      int
		want   []string
	}{
		{"valid", nil, 0, nil},
		{"missing", func(f map[string]string) { delete(f, "hooks/hooks.json") }, 1, []string{"missing"}},
		{"invalid JSON", hooks(`{"hooks": `), 1, []string{"not a valid hooks file"}},
		{"hooks is not an object", hooks(`{"hooks": []}`), 1, []string{"not a valid hooks file"}},
		{"no hooks object", hooks(`{}`), 1, []string{`no "hooks" object`}},
		{"SessionStart missing", hooks(`{"hooks": {` + sub + `, ` + stop + `}}`), 1, []string{"SessionStart is not registered"}},
		{"SubagentStart missing", hooks(`{"hooks": {` + sess + `, ` + stop + `}}`), 1, []string{"SubagentStart is not registered"}},
		// #45: check e must protect SubagentStop's registration too, the same way it
		// protects the two start hooks — the measuring hook added in 0.5.0 is guarded
		// against being dropped, not just accepted when present.
		{"SubagentStop missing", hooks(`{"hooks": {` + sess + `, ` + sub + `}}`), 1, []string{"SubagentStop is not registered"}},
		// A Copilot review on this same change found that an empty or unrelated
		// SubagentStop group satisfied the registration check above while the
		// measuring command itself was gone. These two cases pin that down.
		{"SubagentStop registered with no hooks", hooks(`{"hooks": {` + sess + `, ` + sub + `, "SubagentStop": [{"hooks": []}]}}`), 1,
			[]string{"no hook command names log-subagent.sh"}},
		{"SubagentStop registered with an unrelated command", hooks(`{"hooks": {` + sess + `, ` + sub + `, "SubagentStop": [{"hooks": [{"type": "command", "command": "cat \"${CLAUDE_PLUGIN_ROOT}/x\""}]}]}}`), 1,
			[]string{"no hook command names log-subagent.sh"}},
		{"SessionStart empty list", hooks(`{"hooks": {"SessionStart": [], ` + sub + `, ` + stop + `}}`), 1, []string{"SessionStart is not registered"}},
		{"SessionStart with a matcher", hooks(`{"hooks": {"SessionStart": [{"matcher": "startup", "hooks": []}], ` + sub + `, ` + stop + `}}`), 1,
			[]string{`SessionStart entry 1 has the matcher "startup"`}},
		{"SessionStart with an empty matcher", hooks(`{"hooks": {"SessionStart": [{"matcher": "", "hooks": []}], ` + sub + `, ` + stop + `}}`), 0, nil},
		{"SubagentStart may have a matcher", hooks(`{"hooks": {` + sess + `, "SubagentStart": [{"matcher": "x", "hooks": []}], ` + stop + `}}`), 0, nil},
		{"unquoted plugin root", hooks(`{"hooks": {"SessionStart": [{"hooks": [{"type": "command", "command": "cat ${CLAUDE_PLUGIN_ROOT}/x"}]}], ` + sub + `, ` + stop + `}}`), 1,
			[]string{"SessionStart entry 1, hook 1"}},
		{"single-quoted plugin root", hooks(`{"hooks": {` + sess + `, "SubagentStart": [{"hooks": [{"type": "command", "command": "cat '${CLAUDE_PLUGIN_ROOT}/x'"}]}], ` + stop + `}}`), 1,
			[]string{"SubagentStart entry 1, hook 1"}},
		{"unquoted in another event", hooks(`{"hooks": {` + sess + `, ` + sub + `, ` + stop + `, "PreToolUse": [{"matcher": "Bash", "hooks": [{"type": "command", "command": "sh $CLAUDE_PLUGIN_ROOT/a.sh"}]}]}}`), 1,
			[]string{"PreToolUse entry 1, hook 1"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			expectViolations(t, checkHooksJSON, testEnv(t, newTree(t, tc.mutate)), tc.n, tc.want...)
		})
	}
}

func TestPluginRootOutsideDoubleQuotes(t *testing.T) {
	tests := []struct {
		cmd  string
		want bool
	}{
		{`cat "${CLAUDE_PLUGIN_ROOT}/hooks/x.json"`, false},
		{`cat ${CLAUDE_PLUGIN_ROOT}/hooks/x.json`, true},
		{`cat '${CLAUDE_PLUGIN_ROOT}/hooks/x.json'`, true},
		{`"${CLAUDE_PLUGIN_ROOT}"/hooks/x.sh`, false},
		{`cat "$CLAUDE_PLUGIN_ROOT/x"`, false},
		{`cat $CLAUDE_PLUGIN_ROOT/x`, true},
		{`echo \${CLAUDE_PLUGIN_ROOT}`, false},
		{`echo "a \" ${CLAUDE_PLUGIN_ROOT}"`, false},
		{`echo "a" ${CLAUDE_PLUGIN_ROOT}`, true},
		{`echo 'it''s' "${CLAUDE_PLUGIN_ROOT}"`, false},
		{`echo $CLAUDE_PLUGIN_ROOTS`, false},
		{`"${CLAUDE_PLUGIN_ROOT:-.}/x"`, false},
		{`${CLAUDE_PLUGIN_ROOT:-.}/x`, true},
		{`echo no variable here`, false},
	}
	for _, tc := range tests {
		t.Run(tc.cmd, func(t *testing.T) {
			if got := pluginRootOutsideDoubleQuotes(tc.cmd); got != tc.want {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
		})
	}
}

// --- f -----------------------------------------------------------------------

func TestCheckPluginVersion(t *testing.T) {
	manifest := func(s string) func(map[string]string) {
		return func(f map[string]string) { f[".claude-plugin/plugin.json"] = s }
	}
	version := func(v string) func(map[string]string) {
		return manifest(`{"name": "fixture", "version": "` + v + `"}`)
	}
	tests := []struct {
		name   string
		mutate func(map[string]string)
		n      int
		want   []string
	}{
		{"valid", nil, 0, nil},
		{"current release 0.9.2", version("0.9.2"), 0, nil},
		{"zero major", version("0.3.0"), 0, nil},
		{"pre-release and build", version("1.2.3-rc.1+build.5"), 0, nil},
		{"multi-digit", version("10.20.30"), 0, nil},
		{"two parts", version("1.2"), 1, []string{`version "1.2" is not semver`}},
		{"leading v", version("v1.2.3"), 1, nil},
		{"leading zero", version("01.2.3"), 1, nil},
		{"empty pre-release", version("1.2.3-"), 1, nil},
		{"empty", version(""), 1, nil},
		{"not a string", manifest(`{"version": 1}`), 1, []string{"not a string"}},
		{"no version", manifest(`{"name": "fixture"}`), 1, []string{`no "version"`}},
		{"invalid JSON", manifest(`{"version": `), 1, []string{"not valid JSON"}},
		{"missing", func(f map[string]string) { delete(f, ".claude-plugin/plugin.json") }, 1, []string{"missing"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			expectViolations(t, checkPluginVersion, testEnv(t, newTree(t, tc.mutate)), tc.n, tc.want...)
		})
	}
}

// --- g -----------------------------------------------------------------------

func TestCheckEnforcementClassification(t *testing.T) {
	table := func(rows ...string) string {
		return "# Reference\n\n| Rule | Enforcement |\n|---|---|\n" + strings.Join(rows, "\n") + "\n"
	}
	setRef := func(content string) func(map[string]string) {
		return func(f map[string]string) { f["skills/beta/references/enforcement.md"] = content }
	}
	tests := []struct {
		name   string
		mutate func(map[string]string)
		n      int
		want   []string
	}{
		{"valid", nil, 0, nil},
		{"punctuation inside and outside the bold", setRef(table(
			"| A | **Behaviour rule.** |",
			"| B | **Behaviour rule**. |",
			"| C | **Behaviour rule**, and more. |",
			"| D | **Enforceable, not enforced** — see below. |",
		)), 0, nil},
		{"emphasis that is not a state", setRef(table(
			"| A | **Enforced** only **where measured**, and **not measured here**. |",
		)), 0, nil},
		{"a state in plain prose does not count", setRef(table(
			"| A | **Enforced** for one half; a behaviour rule for the other. |",
		)), 0, nil},
		{"no state", setRef(table("| A | Nothing enforces this. |")), 1,
			[]string{"skills/beta/references/enforcement.md:5: names no state"}},
		{"empty cell", setRef(table("| A | |")), 1, []string{":5: names no state"}},
		{"missing cell", setRef(table("| A |")), 1, []string{":5: names no state"}},
		{"unknown state", setRef(table("| A | **Partly enforced.** By a hook. |")), 1,
			[]string{`:5: the state "Partly enforced" is not one of the four`}},
		{"misspelt state", setRef(table("| A | **Enforcable, not enforced** |")), 1,
			[]string{`"Enforcable, not enforced"`}},
		{"case matters", setRef(table("| A | **behaviour rule** |")), 1, []string{`"behaviour rule"`}},
		{"unknown lead, known state later", setRef(table("| A | **Mostly** enforced; **Behaviour rule** otherwise. |")), 1,
			[]string{`"Mostly"`}},
		{"two states, both known, is allowed (one per rule half)", setRef(table(
			"| A | **Behaviour rule** for one half; **Enforceable, not enforced** for the other. |",
		)), 0, nil},
		{"a known and an unknown classification in the same row", setRef(table(
			"| A | **Partly enforced** for one half; **Behaviour rule** for the other. |",
		)), 1, []string{`:5: the state "Partly enforced" is not one of the four`}},
		{"one finding per bad row", setRef(table(
			"| A | **Enforced** |",
			"| B | nothing |",
			"| C | **Behaviour rule** |",
			"| D | **Maybe** |",
		)), 2, []string{":6: names no state", `:8: the state "Maybe"`}},
		{"an escaped pipe stays in its cell", setRef(table(`| A \| B | **Enforced** |`)), 0, nil},
		{"the Enforcement column is found by its header", setRef(
			"| Enforcement | Rule |\n|:--|--:|\n| **Enforced** | one |\n| two | **Behaviour rule** |\n"), 1,
			[]string{":4: names no state"}},
		{"other tables are not read", setRef(
			"| Claim | Position |\n|---|---|\n| A | plain text |\n"), 0, nil},
		{"a table in a code fence is not read", setRef(
			"```\n| Rule | Enforcement |\n|---|---|\n| A | plain |\n```\n"), 0, nil},
		{"a header without a delimiter row is not a table", setRef(
			"| Rule | Enforcement |\n| A | plain |\n"), 0, nil},
		{"the table ends at the first line that is not a row", setRef(
			table("| A | **Enforced** |") + "After the table.\n| stray | row |\n"), 0, nil},
		{"two tables in one file", setRef(
			table("| A | plain |") + "\n" + table("| B | **Nope** |")), 2,
			[]string{":5: names no state", ":11: the state \"Nope\""}},
		{"files other than Markdown are not read", func(f map[string]string) {
			f["skills/beta/notes.txt"] = table("| A | plain |")
		}, 0, nil},
		{"other tables outside skills/ and the doc are not read", func(f map[string]string) {
			f["README.md"] = table("| A | plain |")
		}, 0, nil},
		{"the doc's own enforcement table is read", func(f map[string]string) {
			f["docs/core-card-and-checks.md"] = table("| A | plain |")
		}, 1, []string{"docs/core-card-and-checks.md:5: names no state"}},
		{"CRLF line endings", setRef(strings.ReplaceAll(table("| A | **Enforced** |", "| B | plain |"), "\n", "\r\n")), 1,
			[]string{":6: names no state"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			expectViolations(t, checkEnforcementClassification, testEnv(t, newTree(t, tc.mutate)), tc.n, tc.want...)
		})
	}
}

func TestEnforcementNoteCountsTablesAndRows(t *testing.T) {
	res, err := checkEnforcementClassification(testEnv(t, newTree(t, nil)))
	if err != nil {
		t.Fatal(err)
	}
	if res.Note != "1 table(s), 4 row(s)" {
		t.Fatalf("note %q", res.Note)
	}
}

// --- h -----------------------------------------------------------------------

func TestCheckOverdueRechecks(t *testing.T) {
	// testToday is 2026-06-15.
	setReadme := func(content string) func(map[string]string) {
		return func(f map[string]string) { f["README.md"] = content }
	}
	tests := []struct {
		name   string
		mutate func(map[string]string)
		n      int
		want   []string
	}{
		{"no dates", nil, 0, nil},
		{"due today is not overdue", setReadme("*Re-check by 2026-06-15.*\n"), 0, nil},
		{"due later", setReadme("Re-check by 2026-12-13.\n"), 0, nil},
		{"one day past", setReadme("# x\n*Re-check by 2026-06-14.*\n"), 1,
			[]string{"README.md:2: re-check was due by 2026-06-14, 1 day(s) before 2026-06-15"}},
		{"lower case inside a table row", setReadme("| A | **Behaviour rule**; re-check by 2026-01-01. |\n"), 1,
			[]string{"README.md:1: re-check was due by 2026-01-01, 165 day(s)"}},
		{"bold", setReadme("**Re-check by 2025-06-15** whether it holds.\n"), 1, []string{"365 day(s)"}},
		{"without by", setReadme("re-check 2026-06-01\n"), 1, []string{"due by 2026-06-01"}},
		{"two on one line", setReadme("Re-check by 2026-01-01, and re-check by 2026-02-01.\n"), 2,
			[]string{"2026-01-01", "2026-02-01"}},
		{"not a calendar date", setReadme("Re-check by 2026-02-30.\n"), 1, []string{"2026-02-30 is not a calendar date"}},
		{"a date with other words is not a re-check date", setReadme("Measured 2020-01-01. Checked: 2020-01-01. Due by 2020-01-01.\n"), 0, nil},
		{"recheck without a hyphen is not read", setReadme("Recheck by 2020-01-01.\n"), 0, nil},
		{"skills, agents and hooks are read", func(f map[string]string) {
			f["skills/alpha/references/r.md"] = "Re-check by 2020-01-01.\n"
			f["agents/reader.md"] += "Re-check by 2020-01-01.\n"
			f["hooks/notes.md"] = "Re-check by 2020-01-01.\n"
		}, 3, []string{"skills/alpha/references/r.md:1", "agents/reader.md:5", "hooks/notes.md:1"}},
		{"files outside the plugin are not read", func(f map[string]string) {
			f["tools/x/x_test.go"] = "// Re-check by 2020-01-01.\n"
			f[".github/workflows/ci.yml"] = "# Re-check by 2020-01-01.\n"
		}, 0, nil},
		{"docs and CONTRIBUTING are read", func(f map[string]string) {
			f["docs/guide.md"] = "Re-check by 2020-01-01.\n"
			f["CONTRIBUTING.md"] = "Re-check by 2020-01-01.\n"
		}, 2, []string{"docs/guide.md:1", "CONTRIBUTING.md:1"}},
	}
	for _, tc := range tests {
		t.Run(tc.name+" (release)", func(t *testing.T) {
			e := testEnv(t, newTree(t, tc.mutate))
			e.Release = true
			expectViolations(t, checkOverdueRechecks, e, tc.n, tc.want...)
		})
		t.Run(tc.name+" (normal run)", func(t *testing.T) {
			res, err := checkOverdueRechecks(testEnv(t, newTree(t, tc.mutate)))
			if err != nil {
				t.Fatal(err)
			}
			if v := violations(res.Findings); len(v) != 0 {
				t.Fatalf("a normal run reported violations: %+v", v)
			}
			if len(res.Findings) != tc.n {
				t.Fatalf("got %d warning(s), want %d: %+v", len(res.Findings), tc.n, res.Findings)
			}
			for _, f := range res.Findings {
				if f.Kind != warning {
					t.Fatalf("finding of kind %d, want a warning: %+v", f.Kind, f)
				}
			}
		})
	}
}

func TestOverdueRecheckNeedsToday(t *testing.T) {
	e := testEnv(t, newTree(t, nil))
	e.Today = ""
	if _, err := checkOverdueRechecks(e); err == nil {
		t.Fatal("no error without a date for today")
	}
}

// N17: an overdue re-check date blocks a release, never the ordinary run.
func TestOverdueRecheckExitCodes(t *testing.T) {
	root := newTree(t, func(f map[string]string) { f["README.md"] = "# fixture\n\n*Re-check by 2026-06-14.*\n" })
	for _, tc := range []struct {
		release bool
		code    int
	}{{false, exitOK}, {true, exitViolation}} {
		rep := runChecks(&env{Root: root, Today: testToday, Release: tc.release})
		if got := rep.exitCode(); got != tc.code {
			t.Errorf("release=%v: exit %d, want %d", tc.release, got, tc.code)
		}
	}
}

// --- i -----------------------------------------------------------------------

// rHostV2Script embeds rHostV2Block itself (the production constant, so the
// fixture cannot drift from what check j actually requires) in a hook script
// that also calls imprint_host and invokes claude — the shape Codex's
// integration review asked to be pinned as the passing case for both check i
// (hook-env-portable) and check j (hook-host-binary).
const rHostV3Script = "#!/bin/sh\n" + rHostV3Block + "\nhost=$(imprint_host)\nout=\"$(claude --version)\" || exit 0\n"
const rHostV2Script = rHostV3Script

// rHostV3BlockUnused is rHostV3Block, verbatim, in a script that invokes
// claude but never calls imprint_host outside its own definition — the
// "defined but unused" case check j must still fail (P2 review finding).
const rHostV3BlockUnused = "#!/bin/sh\n" + rHostV3Block + "\nout=\"$(claude --version)\" || exit 0\n"
const rHostV2BlockUnused = rHostV3BlockUnused

// rHostV3BlockModified is rHostV3Block with its last statement changed
// ("echo unknown" to "echo other"), called and paired with a claude
// invocation — the "defined, called, but not verbatim" case.
const rHostV3BlockModified = `#!/bin/sh
# imprint_host prints the host runtime running this hook: codex, claude, agy or unknown (R-HOST v3).
imprint_host() {
	codex_home=${CODEX_HOME:-$HOME/.codex}
	claude_home=${CLAUDE_CONFIG_DIR:-$HOME/.claude}
	agy_home=${ANTIGRAVITY_CONFIG_DIR:-$HOME/.gemini}
	for p in "${CLAUDE_PLUGIN_DATA:-}" "${CLAUDE_PLUGIN_ROOT:-}"; do
		case $p in
		"$codex_home"/*) echo codex; return ;;
		"$claude_home"/*) echo claude; return ;;
		"$agy_home"/*) echo agy; return ;;
		esac
	done
	echo other
}
host=$(imprint_host)
out="$(claude --version)" || exit 0
`
const rHostV2BlockModified = rHostV3BlockModified

func TestCheckHookEnvPortable(t *testing.T) {
	script := func(content string) func(map[string]string) {
		return func(f map[string]string) { f["hooks/foo.sh"] = content }
	}
	tests := []struct {
		name   string
		mutate func(map[string]string)
		n      int
		want   []string
	}{
		{"valid, no scripts", nil, 0, nil},
		{"plugin root and data are allowed", script(
			"#!/bin/sh\ncat \"${CLAUDE_PLUGIN_ROOT}/x\"\necho \"$CLAUDE_PLUGIN_DATA\"\n"), 0, nil},
		{"any other CLAUDE_* variable is a finding", script(
			"#!/bin/sh\ncd \"$CLAUDE_PROJECT_DIR\"\n"), 1,
			[]string{"hooks/foo.sh:2: uses $CLAUDE_PROJECT_DIR"}},
		{"the braced form is caught too", script(
			"#!/bin/sh\necho \"${CLAUDE_PROJECT_DIR:-.}\"\n"), 1, []string{"CLAUDE_PROJECT_DIR"}},
		{"in hooks.json's own command", func(f map[string]string) {
			f["hooks/hooks.json"] = `{"hooks": {"SessionStart": [{"hooks": [{"type": "command", "command": "echo $CLAUDE_PROJECT_DIR"}]}]}}` + "\n"
		}, 1, []string{"hooks/hooks.json:1: uses $CLAUDE_PROJECT_DIR"}},
		{"two scripts, two findings", func(f map[string]string) {
			f["hooks/foo.sh"] = "#!/bin/sh\necho \"$CLAUDE_PROJECT_DIR\"\n"
			f["hooks/bar.sh"] = "#!/bin/sh\necho \"$CLAUDE_SOMETHING\"\n"
		}, 2, []string{"hooks/foo.sh", "hooks/bar.sh"}},
		{"a non-.sh file under hooks is not scanned", func(f map[string]string) {
			f["hooks/notes.md"] = "$CLAUDE_PROJECT_DIR\n"
		}, 0, nil},
		{"hooks.json missing is not this check's problem", func(f map[string]string) {
			delete(f, "hooks/hooks.json")
		}, 0, nil},
		{"CLAUDE_CONFIG_DIR with a default is allowed", script(
			"#!/bin/sh\nhome=${CLAUDE_CONFIG_DIR:-$HOME/.claude}\n"), 0, nil},
		{"a bare CLAUDE_CONFIG_DIR is still a finding", script(
			"#!/bin/sh\necho \"$CLAUDE_CONFIG_DIR\"\n"), 1,
			[]string{"hooks/foo.sh:2: uses $CLAUDE_CONFIG_DIR"}},
		{"a braced CLAUDE_CONFIG_DIR without a default is still a finding", script(
			"#!/bin/sh\necho \"${CLAUDE_CONFIG_DIR}\"\n"), 1, []string{"hooks/foo.sh:2: uses $CLAUDE_CONFIG_DIR"}},
		{"the default-form exception is only for CLAUDE_CONFIG_DIR", script(
			"#!/bin/sh\necho \"${CLAUDE_PROJECT_DIR:-x}\"\n"), 1, []string{"hooks/foo.sh:2: uses $CLAUDE_PROJECT_DIR"}},
		{"R-HOST v3's imprint_host passes on its own", script(rHostV3Script), 0, nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			expectViolations(t, checkHookEnvPortable, testEnv(t, newTree(t, tc.mutate)), tc.n, tc.want...)
		})
	}
}

// --- j -----------------------------------------------------------------------

func TestCheckHookHostBinary(t *testing.T) {
	script := func(content string) func(map[string]string) {
		return func(f map[string]string) { f["hooks/foo.sh"] = content }
	}
	tests := []struct {
		name   string
		mutate func(map[string]string)
		n      int
		want   []string
	}{
		{"valid, no scripts", nil, 0, nil},
		{"no mention of claude, codex or agy", script("#!/bin/sh\necho hi\n"), 0, nil},
		{"a documentation mention in a comment is not an invocation", script(
			"#!/bin/sh\n# reads the version with `claude --version`\necho hi\n"), 0, nil},
		{"an existence probe is not an invocation", script(
			"#!/bin/sh\ncommand -v claude >/dev/null || exit 0\n"), 0, nil},
		{"invocation via command substitution without imprint_host is a finding", script(
			"#!/bin/sh\nout=\"$(claude --version)\" || exit 0\n"), 1,
			[]string{"hooks/foo.sh:2: invokes the claude, codex or agy binary"}},
		{"agy invocation via command substitution without imprint_host is a finding", script(
			"#!/bin/sh\nout=\"$(agy --version)\" || exit 0\n"), 1,
			[]string{"hooks/foo.sh:2: invokes the claude, codex or agy binary"}},
		// P2 (Codex review): a bare, unused stub used to pass this check. A
		// definition — canonical or not — is no longer enough on its own.
		{"a minimal stub, even if it's called, is not R-HOST v3 verbatim", script(
			"#!/bin/sh\nimprint_host() {\n  echo claude\n}\nhost=$(imprint_host)\nout=\"$(claude --version)\" || exit 0\n"), 1,
			[]string{"hooks/foo.sh:6: invokes the claude, codex or agy binary as a command; imprint_host differs from R-HOST v3"}},
		{"bash's function keyword is not R-HOST v3's own shape either", script(
			"#!/bin/sh\nfunction imprint_host() {\n  echo claude\n}\nhost=$(imprint_host)\nout=\"$(claude --version)\" || exit 0\n"), 1,
			[]string{"imprint_host differs from R-HOST v3"}},
		{"a commented-out definition does not count", script(
			"#!/bin/sh\n# imprint_host() {\n#   echo claude\n# }\nout=\"$(claude --version)\" || exit 0\n"), 1,
			[]string{"hooks/foo.sh:5: invokes"}},
		{"codex at the start of a line is caught too", script(
			"#!/bin/sh\ncodex exec 'do it'\n"), 1, []string{"hooks/foo.sh:2"}},
		{"agy at the start of a line is caught too", script(
			"#!/bin/sh\nagy exec 'do it'\n"), 1, []string{"hooks/foo.sh:2"}},
		{"after a semicolon", script("#!/bin/sh\ntrue; claude --version\n"), 1, nil},
		{"after a pipe", script("#!/bin/sh\necho x | claude filter\n"), 1, nil},
		{"a non-.sh file under hooks is not scanned", func(f map[string]string) {
			f["hooks/notes.md"] = "$(claude --version)\n"
		}, 0, nil},
		{"the canonical block, verbatim but never called, is a finding", script(rHostV3BlockUnused), 1,
			[]string{"hooks/foo.sh:16: defines imprint_host() as R-HOST v3's canonical block, verbatim, but never calls it"}},
		{"the canonical block, called but modified, is still a finding", script(rHostV3BlockModified), 1,
			[]string{"imprint_host differs from R-HOST v3"}},
		// P2 follow-up (advisor review): a mere word match is not a call. A
		// second, shadowing definition outside the canonical block must not
		// count as satisfying it, and a comment merely mentioning the name
		// must not count as a call either — both used to slip past the
		// "called" scan below since it matched \bimprint_host\b unconditionally.
		{"a second definition outside the canonical block does not count as calling it", script(
			"#!/bin/sh\n"+rHostV3Block+"\nimprint_host() { echo claude; }\nhost=$(imprint_host)\nout=\"$(claude --version)\" || exit 0\n"), 1,
			[]string{"imprint_host differs from R-HOST v3", "a second imprint_host definition sits outside the canonical block"}},
		{"a comment mentioning imprint_host does not count as calling it", script(
			"#!/bin/sh\n"+rHostV3Block+"\n# imprint_host decides the host\nout=\"$(claude --version)\" || exit 0\n"), 1,
			[]string{"never calls it outside its own definition"}},
		// P2 residue (Codex's re-review of the first fix): a bare word match
		// still counted a mention inside quotes as a call.
		{"a quoted echo mentioning imprint_host is not a call", script(
			"#!/bin/sh\n"+rHostV3Block+"\necho 'imprint_host is configured'\nout=\"$(claude --version)\" || exit 0\n"), 1,
			[]string{"never calls it outside its own definition"}},
		{"a quoted printf mentioning imprint_host is not a call", script(
			"#!/bin/sh\n"+rHostV3Block+"\nprintf '%s\\n' \"imprint_host\"\nout=\"$(claude --version)\" || exit 0\n"), 1,
			[]string{"never calls it outside its own definition"}},
		{"Codex's exact reproduction: canonical block, a quoted echo, a blind claude call", script(
			"#!/bin/sh\n"+rHostV3Block+"\necho 'imprint_host is configured'\n$(claude --version)\n"), 1,
			[]string{"never calls it outside its own definition"}},
		{"a quoted command substitution assignment is a real call", script(
			"#!/bin/sh\n"+rHostV3Block+"\nhost=\"$(imprint_host)\"\nout=\"$(claude --version)\" || exit 0\n"), 0, nil},
		{"a call inside a case subject is a real call", script(
			"#!/bin/sh\n"+rHostV3Block+"\ncase \"$(imprint_host)\" in\n\tclaude) out=\"$(claude --version)\" ;;\nesac\n"), 0, nil},
		{"indentation style (tabs vs spaces) does not break the verbatim match", script(
			strings.ReplaceAll(rHostV3Script, "\t", "    ")), 0, nil},
		{"R-HOST v3's canonical block, verbatim and called, passes", script(rHostV3Script), 0, nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			expectViolations(t, checkHookHostBinary, testEnv(t, newTree(t, tc.mutate)), tc.n, tc.want...)
		})
	}
}

// --- k -----------------------------------------------------------------------

func TestCheckRulesAgentsInSync(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(map[string]string)
		n      int
		want   []string
	}{
		{"valid", nil, 0, nil},
		{"rules/AGENTS.md missing", func(f map[string]string) {
			delete(f, "rules/AGENTS.md")
		}, 1, []string{"rules/AGENTS.md: missing; run: imprint-dev gen"}},
		{"rules/AGENTS.md differs", func(f map[string]string) {
			f["rules/AGENTS.md"] = "Modified card line.\n"
		}, 1, []string{"rules/AGENTS.md: differs from hooks/kernkarte.md; run: imprint-dev gen"}},
		{"card missing", func(f map[string]string) {
			delete(f, "hooks/kernkarte.md")
		}, 1, []string{"hooks/kernkarte.md: missing, so rules/AGENTS.md has no source"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			expectViolations(t, checkRulesAgentsInSync, testEnv(t, newTree(t, tc.mutate)), tc.n, tc.want...)
		})
	}
}
