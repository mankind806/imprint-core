package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

type kind int

const (
	violation    kind = iota + 1 // the tree breaks the rule
	notCheckable                 // the rule could not be decided here; not a failure
	warning                      // worth acting on, but it does not fail this run
)

type finding struct {
	Rule    string
	Kind    kind
	Path    string // slash-separated, relative to the root; "" for the tree as a whole
	Line    int    // 1-based; 0 when the finding is not about one line
	Message string
}

type env struct {
	Root    string
	Today   string // YYYY-MM-DD: the day re-check dates are measured against
	Release bool   // a release run, which overdue re-check dates fail
}

type checkResult struct {
	Findings []finding
	Note     string // what was looked at, for the summary
}

type check struct {
	Letter string
	ID     string
	Title  string
	Run    func(e *env) (checkResult, error)
}

var checks = []check{
	{"a", "skill-description-length", "Each skills/*/SKILL.md description is at most 400 characters, and all of them together at most 3000.", checkDescriptions},
	{"b", "card-size", "hooks/kernkarte.md is at most 4000 characters and 12 non-empty lines.", checkCardSize},
	{"c", "card-generated", "hooks/session-start.json and hooks/subagent-start.json are byte-identical to what gen produces from hooks/kernkarte.md.", checkCardGenerated},
	{"d", "removed-skill-reference", "No reference to a removed skill, except on a line that says formerly or merged.", checkRemovedSkills},
	{"e", "hooks-json", "hooks/hooks.json registers SessionStart without a matcher and SubagentStart, and every ${CLAUDE_PLUGIN_ROOT} sits inside double quotes.", checkHooksJSON},
	{"f", "plugin-version", ".claude-plugin/plugin.json carries a semver version.", checkPluginVersion},
	{"g", "enforcement-classification", "Each row of an Enforcement table under skills/, and in docs/core-card-and-checks.md, carries at least one known classification, none unknown: Enforced; Enforceable, not enforced; Behaviour rule; Reserved to a person.", checkEnforcementClassification},
	{"h", "overdue-recheck", "No re-check date (Re-check by YYYY-MM-DD) has passed. A warning in a normal run; a violation only under --release.", checkOverdueRechecks},
}

// readRel reads a file under root. A missing file is found=false, not an error.
func readRel(root, rel string) (data []byte, found bool, err error) {
	data, err = os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return data, true, nil
}

func splitLines(raw []byte) []string {
	s := strings.TrimPrefix(string(raw), "\ufeff")
	return strings.Split(strings.ReplaceAll(s, "\r\n", "\n"), "\n")
}

// --- a: skill descriptions ---------------------------------------------------

const (
	maxDescription      = 400
	maxDescriptionTotal = 3000
)

func checkDescriptions(e *env) (checkResult, error) {
	const rule = "skill-description-length"
	dir := filepath.Join(e.Root, "skills")
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return checkResult{Note: "no skills/ directory"}, nil
	}
	if err != nil {
		return checkResult{}, err
	}
	var res checkResult
	total, counted := 0, 0
	for _, de := range entries {
		if info, err := os.Stat(filepath.Join(dir, de.Name())); err != nil || !info.IsDir() {
			continue
		}
		rel := "skills/" + de.Name() + "/SKILL.md"
		raw, found, err := readRel(e.Root, rel)
		if err != nil {
			return checkResult{}, err
		}
		if !found {
			continue
		}
		desc, line, perr := frontmatterDescription(raw)
		if perr != nil {
			res.Findings = append(res.Findings, finding{rule, violation, rel, line, perr.Error()})
			continue
		}
		n := utf8.RuneCountInString(desc)
		total += n
		counted++
		if n > maxDescription {
			res.Findings = append(res.Findings, finding{rule, violation, rel, line,
				fmt.Sprintf("description is %d characters; the limit is %d", n, maxDescription)})
		}
	}
	if total > maxDescriptionTotal {
		res.Findings = append(res.Findings, finding{rule, violation, "skills", 0,
			fmt.Sprintf("the descriptions of %d skills add up to %d characters; the limit is %d", counted, total, maxDescriptionTotal)})
	}
	res.Note = fmt.Sprintf("%d skill(s), %d characters in total", counted, total)
	return res, nil
}

// --- b: card size ------------------------------------------------------------

const (
	maxCardChars = 4000
	maxCardLines = 12
)

func checkCardSize(e *env) (checkResult, error) {
	const rule = "card-size"
	raw, found, err := readRel(e.Root, cardPath)
	if err != nil {
		return checkResult{}, err
	}
	if !found {
		return checkResult{Findings: []finding{{rule, violation, cardPath, 0, "missing: the core card is required"}}}, nil
	}
	var res checkResult
	if !utf8.Valid(raw) {
		res.Findings = append(res.Findings, finding{rule, violation, cardPath, 0, "not valid UTF-8"})
	}
	chars := utf8.RuneCount(raw)
	lines := 0
	for _, l := range splitLines(raw) {
		if strings.TrimSpace(l) != "" {
			lines++
		}
	}
	if chars > maxCardChars {
		res.Findings = append(res.Findings, finding{rule, violation, cardPath, 0,
			fmt.Sprintf("%d characters; the limit is %d", chars, maxCardChars)})
	}
	if lines > maxCardLines {
		res.Findings = append(res.Findings, finding{rule, violation, cardPath, 0,
			fmt.Sprintf("%d non-empty lines; the limit is %d", lines, maxCardLines)})
	}
	res.Note = fmt.Sprintf("%d characters, %d non-empty lines", chars, lines)
	return res, nil
}

// --- c: generated payloads ---------------------------------------------------

func checkCardGenerated(e *env) (checkResult, error) {
	const rule = "card-generated"
	files, found, err := generate(e.Root)
	if err != nil {
		return checkResult{}, err
	}
	if !found {
		return checkResult{Findings: []finding{{rule, violation, cardPath, 0,
			"missing, so the hook payloads have no source to be generated from"}}}, nil
	}
	var res checkResult
	for _, f := range files {
		got, found, err := readRel(e.Root, f.Path)
		if err != nil {
			return checkResult{}, err
		}
		switch {
		case !found:
			res.Findings = append(res.Findings, finding{rule, violation, f.Path, 0, "missing; run: imprint-dev gen"})
		case !bytes.Equal(got, f.Data):
			res.Findings = append(res.Findings, finding{rule, violation, f.Path, 0,
				"differs from what gen produces from " + cardPath + "; edit the card, then run: imprint-dev gen"})
		}
	}
	res.Note = fmt.Sprintf("%d payload(s) compared", len(files))
	return res, nil
}

// --- d: removed skill names --------------------------------------------------

var (
	removedSkills   = []string{"one-canonical-place", "provenance-on-entry", "supersede-dont-delete", "knowledge-ages", "blind-first-pass"}
	removedScanDirs = []string{"skills", "agents", "hooks", ".claude-plugin"}
	migrationWord   = regexp.MustCompile(`(?i)\b(formerly|merged)\b`)
)

// isNameChar says whether c can be part of a skill name, which is what makes a
// match a whole word: knowledge-ages-extra is not knowledge-ages.
func isNameChar(c byte) bool {
	return c == '-' || c == '_' || '0' <= c && c <= '9' || 'a' <= c && c <= 'z' || 'A' <= c && c <= 'Z'
}

// removedNamesIn returns each removed skill name that occurs in line as a whole
// word, ignoring case.
func removedNamesIn(line string) []string {
	lower := strings.ToLower(line)
	var found []string
	for _, name := range removedSkills {
		for from := 0; from < len(lower); {
			i := strings.Index(lower[from:], name)
			if i < 0 {
				break
			}
			start, end := from+i, from+i+len(name)
			if (start == 0 || !isNameChar(lower[start-1])) && (end == len(lower) || !isNameChar(lower[end])) {
				found = append(found, name)
				break
			}
			from = start + 1
		}
	}
	return found
}

// regularFilesUnder lists the regular files under root/dir, slash-separated and
// relative to root. A missing dir is an empty list.
func regularFilesUnder(root, dir string) ([]string, error) {
	base := filepath.Join(root, filepath.FromSlash(dir))
	if _, err := os.Stat(base); errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	var out []string
	err := filepath.WalkDir(base, func(p string, de fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if de.Name() == ".git" && p != base { // a repository dir, or a work tree's .git file
			if de.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if !de.Type().IsRegular() {
			return nil
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		out = append(out, filepath.ToSlash(rel))
		return nil
	})
	return out, err
}

// pluginFiles lists the files the text checks read: everything under the
// plugin's own directories, and the README.
func pluginFiles(root string) ([]string, error) {
	var paths []string
	for _, d := range removedScanDirs {
		files, err := regularFilesUnder(root, d)
		if err != nil {
			return nil, err
		}
		paths = append(paths, files...)
	}
	if info, err := os.Stat(filepath.Join(root, "README.md")); err == nil && info.Mode().IsRegular() {
		paths = append(paths, "README.md")
	}
	return paths, nil
}

func checkRemovedSkills(e *env) (checkResult, error) {
	const rule = "removed-skill-reference"
	paths, err := pluginFiles(e.Root)
	if err != nil {
		return checkResult{}, err
	}
	var res checkResult
	for _, rel := range paths {
		raw, found, err := readRel(e.Root, rel)
		if err != nil {
			return checkResult{}, err
		}
		if !found {
			continue
		}
		for i, line := range splitLines(raw) {
			names := removedNamesIn(line)
			if len(names) == 0 || migrationWord.MatchString(line) {
				continue
			}
			for _, name := range names {
				res.Findings = append(res.Findings, finding{rule, violation, rel, i + 1,
					fmt.Sprintf("refers to the removed skill %q; a migration note must say formerly or merged on the same line", name)})
			}
		}
	}
	res.Note = fmt.Sprintf("%d file(s) scanned", len(paths))
	return res, nil
}

// --- e: hooks.json -----------------------------------------------------------

const hooksPath = "hooks/hooks.json"

type hookGroup struct {
	Matcher *string `json:"matcher"`
	Hooks   []struct {
		Type    string `json:"type"`
		Command string `json:"command"`
	} `json:"hooks"`
}

func checkHooksJSON(e *env) (checkResult, error) {
	const rule = "hooks-json"
	bad := func(msg string) finding { return finding{rule, violation, hooksPath, 0, msg} }
	raw, found, err := readRel(e.Root, hooksPath)
	if err != nil {
		return checkResult{}, err
	}
	if !found {
		return checkResult{Findings: []finding{bad("missing: SessionStart and SubagentStart must be registered here")}}, nil
	}
	var doc struct {
		Hooks map[string][]hookGroup `json:"hooks"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return checkResult{Findings: []finding{bad("not a valid hooks file: " + err.Error())}}, nil
	}
	if doc.Hooks == nil {
		return checkResult{Findings: []finding{bad(`no "hooks" object`)}}, nil
	}
	var res checkResult
	for _, ev := range []string{"SessionStart", "SubagentStart"} {
		if len(doc.Hooks[ev]) == 0 {
			res.Findings = append(res.Findings, bad(ev+" is not registered"))
		}
	}
	for i, g := range doc.Hooks["SessionStart"] {
		if g.Matcher != nil && *g.Matcher != "" {
			res.Findings = append(res.Findings, bad(fmt.Sprintf(
				"SessionStart entry %d has the matcher %q; it must have none, so that every start source, fork included, gets the card", i+1, *g.Matcher)))
		}
	}
	events := make([]string, 0, len(doc.Hooks))
	for ev := range doc.Hooks {
		events = append(events, ev)
	}
	sort.Strings(events)
	commands := 0
	for _, ev := range events {
		for i, g := range doc.Hooks[ev] {
			for j, h := range g.Hooks {
				commands++
				if pluginRootOutsideDoubleQuotes(h.Command) {
					res.Findings = append(res.Findings, bad(fmt.Sprintf(
						"%s entry %d, hook %d: ${CLAUDE_PLUGIN_ROOT} is used outside double quotes in %q", ev, i+1, j+1, h.Command)))
				}
			}
		}
	}
	res.Note = fmt.Sprintf("%d event(s), %d hook(s)", len(events), commands)
	return res, nil
}

// pluginRootOutsideDoubleQuotes reports whether cmd expands CLAUDE_PLUGIN_ROOT,
// as ${CLAUDE_PLUGIN_ROOT...} or $CLAUDE_PLUGIN_ROOT, anywhere but inside
// double quotes. It follows POSIX shell quoting: single quotes, double quotes
// and backslash escapes.
func pluginRootOutsideDoubleQuotes(cmd string) bool {
	const (
		plain = iota
		single
		double
	)
	state := plain
	for i := 0; i < len(cmd); i++ {
		c := cmd[i]
		if c == '$' && refersToPluginRoot(cmd[i:]) && state != double {
			return true
		}
		switch state {
		case single:
			if c == '\'' {
				state = plain
			}
		case double:
			switch c {
			case '\\':
				i++
			case '"':
				state = plain
			}
		default:
			switch c {
			case '\\':
				i++
			case '\'':
				state = single
			case '"':
				state = double
			}
		}
	}
	return false
}

func refersToPluginRoot(s string) bool {
	for _, p := range []string{"${CLAUDE_PLUGIN_ROOT", "$CLAUDE_PLUGIN_ROOT"} {
		if rest, ok := strings.CutPrefix(s, p); ok {
			return rest == "" || !isIdentChar(rest[0])
		}
	}
	return false
}

func isIdentChar(c byte) bool {
	return c == '_' || '0' <= c && c <= '9' || 'a' <= c && c <= 'z' || 'A' <= c && c <= 'Z'
}

// --- f: plugin version -------------------------------------------------------

const pluginPath = ".claude-plugin/plugin.json"

// The regular expression semver.org recommends for SemVer 2.0.0.
var semverRE = regexp.MustCompile(`^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)(?:-((?:0|[1-9]\d*|\d*[a-zA-Z-][0-9a-zA-Z-]*)(?:\.(?:0|[1-9]\d*|\d*[a-zA-Z-][0-9a-zA-Z-]*))*))?(?:\+([0-9a-zA-Z-]+(?:\.[0-9a-zA-Z-]+)*))?$`)

func checkPluginVersion(e *env) (checkResult, error) {
	const rule = "plugin-version"
	bad := func(msg string) checkResult {
		return checkResult{Findings: []finding{{rule, violation, pluginPath, 0, msg}}}
	}
	raw, found, err := readRel(e.Root, pluginPath)
	if err != nil {
		return checkResult{}, err
	}
	if !found {
		return bad("missing"), nil
	}
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(raw, &doc); err != nil {
		return bad("not valid JSON: " + err.Error()), nil
	}
	rawVersion, ok := doc["version"]
	if !ok {
		return bad(`no "version"`), nil
	}
	var version string
	if err := json.Unmarshal(rawVersion, &version); err != nil {
		return bad(`"version" is not a string`), nil
	}
	if !semverRE.MatchString(version) {
		return bad(fmt.Sprintf("version %q is not semver (MAJOR.MINOR.PATCH)", version)), nil
	}
	return checkResult{Note: "version " + version}, nil
}

// --- g: enforcement classification -------------------------------------------

// enforcementStates is the four-state vocabulary the skills' enforcement tables
// use. A row names its state in bold, as in **Behaviour rule.**
var enforcementStates = []string{"Enforced", "Enforceable, not enforced", "Behaviour rule", "Reserved to a person"}

var (
	boldSpanRE  = regexp.MustCompile(`\*\*([^*]+)\*\*`)
	delimCellRE = regexp.MustCompile(`^:?-+:?$`)
)

// stateOf normalises the text of a bold span, so that **Behaviour rule.** and
// **Behaviour rule** read alike, and reports whether it names a state.
func stateOf(bold string) (string, bool) {
	s := strings.TrimRight(strings.TrimSpace(bold), ".,;: ")
	for _, st := range enforcementStates {
		if s == st {
			return s, true
		}
	}
	return s, false
}

// tableCells splits a Markdown table row into its trimmed cells. An escaped
// pipe (\|) stays inside its cell.
func tableCells(line string) []string {
	s := strings.TrimSpace(line)
	s = strings.TrimPrefix(s, "|")
	if strings.HasSuffix(s, "|") && !strings.HasSuffix(s, `\|`) {
		s = s[:len(s)-1]
	}
	var cells []string
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		switch {
		case s[i] == '\\' && i+1 < len(s) && s[i+1] == '|':
			b.WriteByte('|')
			i++
		case s[i] == '|':
			cells = append(cells, strings.TrimSpace(b.String()))
			b.Reset()
		default:
			b.WriteByte(s[i])
		}
	}
	return append(cells, strings.TrimSpace(b.String()))
}

func isTableLine(line string) bool { return strings.HasPrefix(strings.TrimSpace(line), "|") }

func isDelimiterRow(line string) bool {
	for _, c := range tableCells(line) {
		if !delimCellRE.MatchString(c) {
			return false
		}
	}
	return true
}

// classifyEnforcement returns what is wrong with one Enforcement cell, or "".
// The state is the bold span the cell opens with; a state named in bold later
// in the cell counts too, so that a row naming one known state per rule half
// is accepted rather than flagged. Other bold in the cell, such as **not
// measured here**, is emphasis and not a state. A cell is reported only when
// it names no state at all, or when its leading bold is not one of the four.
func classifyEnforcement(cell string) string {
	var known []string
	for _, m := range boldSpanRE.FindAllStringSubmatch(cell, -1) {
		if st, ok := stateOf(m[1]); ok {
			known = append(known, st)
		}
	}
	if strings.HasPrefix(cell, "**") {
		if m := boldSpanRE.FindStringSubmatchIndex(cell); m != nil && m[0] == 0 {
			if st, ok := stateOf(cell[m[2]:m[3]]); !ok {
				return fmt.Sprintf("the state %q is not one of the four (%s)", st, strings.Join(enforcementStates, "; "))
			}
		}
	}
	if len(known) == 0 {
		return fmt.Sprintf("names no state; open the Enforcement cell with one of the four in bold (%s)", strings.Join(enforcementStates, "; "))
	}
	return ""
}

func checkEnforcementClassification(e *env) (checkResult, error) {
	const rule = "enforcement-classification"
	var files []string
	for _, root := range []string{"skills", "docs/core-card-and-checks.md"} {
		found, err := regularFilesUnder(e.Root, root)
		if err != nil {
			return checkResult{}, err
		}
		files = append(files, found...)
	}
	var res checkResult
	tables, rows := 0, 0
	for _, rel := range files {
		if !strings.HasSuffix(strings.ToLower(rel), ".md") {
			continue
		}
		raw, found, err := readRel(e.Root, rel)
		if err != nil {
			return checkResult{}, err
		}
		if !found {
			continue
		}
		lines := splitLines(raw)
		fence := ""
		for i := 0; i < len(lines); i++ {
			trimmed := strings.TrimSpace(lines[i])
			if fence != "" {
				if strings.HasPrefix(trimmed, fence) {
					fence = ""
				}
				continue
			}
			if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
				fence = trimmed[:3]
				continue
			}
			if !isTableLine(lines[i]) || i+1 >= len(lines) || !isTableLine(lines[i+1]) || !isDelimiterRow(lines[i+1]) {
				continue
			}
			col := -1
			for j, h := range tableCells(lines[i]) {
				if strings.EqualFold(h, "Enforcement") {
					col = j
					break
				}
			}
			for i += 2; i < len(lines) && isTableLine(lines[i]); i++ {
				if col < 0 {
					continue
				}
				rows++
				cell := ""
				if cells := tableCells(lines[i]); col < len(cells) {
					cell = cells[col]
				}
				if msg := classifyEnforcement(cell); msg != "" {
					res.Findings = append(res.Findings, finding{rule, violation, rel, i + 1, msg})
				}
			}
			if col >= 0 {
				tables++
			}
			i-- // the loop's own i++ moves past the line that ended the table
		}
	}
	res.Note = fmt.Sprintf("%d table(s), %d row(s)", tables, rows)
	return res, nil
}

// --- h: overdue re-check dates -----------------------------------------------

// recheckRE matches a due date as the tree writes it: "Re-check by 2026-12-13",
// in any case and with or without "by".
var recheckRE = regexp.MustCompile(`(?i)\bre-check\s+(?:by\s+)?(\d{4}-\d{2}-\d{2})\b`)

const dateLayout = "2006-01-02"

func checkOverdueRechecks(e *env) (checkResult, error) {
	const rule = "overdue-recheck"
	today, err := time.Parse(dateLayout, e.Today)
	if err != nil {
		return checkResult{}, fmt.Errorf("today's date %q is not YYYY-MM-DD", e.Today)
	}
	paths, err := pluginFiles(e.Root)
	if err != nil {
		return checkResult{}, err
	}
	k, consequence := warning, "; this blocks a release (check --release)"
	if e.Release {
		k, consequence = violation, "; a release waits until it is re-checked and re-dated"
	}
	var res checkResult
	dates, overdue := 0, 0
	for _, rel := range paths {
		raw, found, err := readRel(e.Root, rel)
		if err != nil {
			return checkResult{}, err
		}
		if !found {
			continue
		}
		for i, line := range splitLines(raw) {
			for _, m := range recheckRE.FindAllStringSubmatch(line, -1) {
				dates++
				due, err := time.Parse(dateLayout, m[1])
				if err != nil {
					overdue++
					res.Findings = append(res.Findings, finding{rule, k, rel, i + 1,
						fmt.Sprintf("the re-check date %s is not a calendar date%s", m[1], consequence)})
					continue
				}
				if due.Before(today) {
					overdue++
					days := int(today.Sub(due).Hours() / 24)
					res.Findings = append(res.Findings, finding{rule, k, rel, i + 1,
						fmt.Sprintf("re-check was due by %s, %d day(s) before %s%s", m[1], days, e.Today, consequence)})
				}
			}
		}
	}
	if overdue == 0 {
		res.Note = fmt.Sprintf("%d re-check date(s), none before %s", dates, e.Today)
	} else {
		res.Note = fmt.Sprintf("%d re-check date(s), %d overdue before %s", dates, overdue, e.Today)
	}
	return res, nil
}

// --- running and reporting ---------------------------------------------------

type outcome struct {
	Check  check
	Result checkResult
	Err    error
}

type report struct {
	Root     string
	Fatal    error // the root itself could not be read; no check ran
	Outcomes []outcome
}

func runChecks(e *env) report {
	rep := report{Root: e.Root}
	info, err := os.Stat(e.Root)
	if err == nil && !info.IsDir() {
		err = fmt.Errorf("%s is not a directory", e.Root)
	}
	if err == nil {
		_, err = os.ReadDir(e.Root)
	}
	if err != nil {
		rep.Fatal = fmt.Errorf("cannot read the plugin root: %w", err)
		return rep
	}
	for _, c := range checks {
		res, err := c.Run(e)
		sort.SliceStable(res.Findings, func(i, j int) bool {
			a, b := res.Findings[i], res.Findings[j]
			if a.Path != b.Path {
				return a.Path < b.Path
			}
			if a.Line != b.Line {
				return a.Line < b.Line
			}
			return a.Message < b.Message
		})
		rep.Outcomes = append(rep.Outcomes, outcome{Check: c, Result: res, Err: err})
	}
	return rep
}

func (o outcome) count(k kind) int {
	n := 0
	for _, f := range o.Result.Findings {
		if f.Kind == k {
			n++
		}
	}
	return n
}

func (r report) exitCode() int {
	if r.Fatal != nil {
		return exitError
	}
	code := exitOK
	for _, o := range r.Outcomes {
		if o.Err != nil {
			return exitError
		}
		if o.count(violation) > 0 {
			code = exitViolation
		}
	}
	return code
}

func (f finding) where() string {
	switch {
	case f.Path == "":
		return ""
	case f.Line > 0:
		return fmt.Sprintf("%s:%d: ", f.Path, f.Line)
	default:
		return f.Path + ": "
	}
}

func printSummary(w io.Writer, r report) {
	fmt.Fprintf(w, "imprint-dev check --root %s\n", r.Root)
	if r.Fatal != nil {
		fmt.Fprintf(w, "  ERROR  %v\nresult: the check could not run (exit %d)\n", r.Fatal, exitError)
		return
	}
	failed, violations, errs, na, warnings := 0, 0, 0, 0, 0
	for _, o := range r.Outcomes {
		label := fmt.Sprintf("%s %s", o.Check.Letter, o.Check.ID)
		v, n, wn := o.count(violation), o.count(notCheckable), o.count(warning)
		warnings += wn
		switch {
		case o.Err != nil:
			errs++
			fmt.Fprintf(w, "  ERROR  %-30s could not run: %v\n", label, o.Err)
		case v > 0:
			failed++
			violations += v
			fmt.Fprintf(w, "  FAIL   %-30s %d violation(s)\n", label, v)
		case wn > 0:
			fmt.Fprintf(w, "  WARN   %-30s %d warning(s)\n", label, wn)
		case n > 0:
			na++
			fmt.Fprintf(w, "  n/a    %s\n", label)
		default:
			fmt.Fprintf(w, "  ok     %-30s %s\n", label, o.Result.Note)
		}
		for _, f := range o.Result.Findings {
			fmt.Fprintf(w, "           %s%s\n", f.where(), f.Message)
		}
	}
	fmt.Fprintf(w, "result: %d violation(s) in %d check(s), %d check(s) could not run, %d not checkable, %d warning(s) (exit %d)\n",
		violations, failed, errs, na, warnings, r.exitCode())
}
