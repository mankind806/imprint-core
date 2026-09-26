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
			f["docs/old.md"] = "one-canonical-place\n"
		}, 0, nil},
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
		{"SessionStart missing", hooks(`{"hooks": {` + sub + `}}`), 1, []string{"SessionStart is not registered"}},
		{"SubagentStart missing", hooks(`{"hooks": {` + sess + `}}`), 1, []string{"SubagentStart is not registered"}},
		{"SessionStart empty list", hooks(`{"hooks": {"SessionStart": [], ` + sub + `}}`), 1, []string{"SessionStart is not registered"}},
		{"SessionStart with a matcher", hooks(`{"hooks": {"SessionStart": [{"matcher": "startup", "hooks": []}], ` + sub + `}}`), 1,
			[]string{`SessionStart entry 1 has the matcher "startup"`}},
		{"SessionStart with an empty matcher", hooks(`{"hooks": {"SessionStart": [{"matcher": "", "hooks": []}], ` + sub + `}}`), 0, nil},
		{"SubagentStart may have a matcher", hooks(`{"hooks": {` + sess + `, "SubagentStart": [{"matcher": "x", "hooks": []}]}}`), 0, nil},
		{"unquoted plugin root", hooks(`{"hooks": {"SessionStart": [{"hooks": [{"type": "command", "command": "cat ${CLAUDE_PLUGIN_ROOT}/x"}]}], ` + sub + `}}`), 1,
			[]string{"SessionStart entry 1, hook 1"}},
		{"single-quoted plugin root", hooks(`{"hooks": {` + sess + `, "SubagentStart": [{"hooks": [{"type": "command", "command": "cat '${CLAUDE_PLUGIN_ROOT}/x'"}]}]}}`), 1,
			[]string{"SubagentStart entry 1, hook 1"}},
		{"unquoted in another event", hooks(`{"hooks": {` + sess + `, ` + sub + `, "PreToolUse": [{"matcher": "Bash", "hooks": [{"type": "command", "command": "sh $CLAUDE_PLUGIN_ROOT/a.sh"}]}]}}`), 1,
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
