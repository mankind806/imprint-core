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
	"unicode/utf8"
)

type kind int

const (
	violation    kind = iota + 1 // the tree breaks the rule
	notCheckable                 // the rule could not be decided here; not a failure
)

type finding struct {
	Rule    string
	Kind    kind
	Path    string // slash-separated, relative to the root; "" for the tree as a whole
	Line    int    // 1-based; 0 when the finding is not about one line
	Message string
}

type env struct {
	Root string
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

func checkRemovedSkills(e *env) (checkResult, error) {
	const rule = "removed-skill-reference"
	var paths []string
	for _, d := range removedScanDirs {
		files, err := regularFilesUnder(e.Root, d)
		if err != nil {
			return checkResult{}, err
		}
		paths = append(paths, files...)
	}
	if info, err := os.Stat(filepath.Join(e.Root, "README.md")); err == nil && info.Mode().IsRegular() {
		paths = append(paths, "README.md")
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
	failed, violations, errs, na := 0, 0, 0, 0
	for _, o := range r.Outcomes {
		label := fmt.Sprintf("%s %s", o.Check.Letter, o.Check.ID)
		v, n := o.count(violation), o.count(notCheckable)
		switch {
		case o.Err != nil:
			errs++
			fmt.Fprintf(w, "  ERROR  %-26s could not run: %v\n", label, o.Err)
		case v > 0:
			failed++
			violations += v
			fmt.Fprintf(w, "  FAIL   %-26s %d violation(s)\n", label, v)
		case n > 0:
			na++
			fmt.Fprintf(w, "  n/a    %s\n", label)
		default:
			fmt.Fprintf(w, "  ok     %-26s %s\n", label, o.Result.Note)
		}
		for _, f := range o.Result.Findings {
			fmt.Fprintf(w, "           %s%s\n", f.where(), f.Message)
		}
	}
	fmt.Fprintf(w, "result: %d violation(s) in %d check(s), %d check(s) could not run, %d not checkable (exit %d)\n",
		violations, failed, errs, na, r.exitCode())
}
