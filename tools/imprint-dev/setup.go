package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"text/tabwriter"
)

// Security boundary (imprint-core-CL-011): what setup may read, show and write is
// decided by the path checks in this file, never by the inventory's own "rechte"
// flag and never by the env guard (which is advisory only). In short:
//
//   - id must match setupIDPattern; quelle and ziel must use a small, shell-inert
//     character set, so no id or path can traverse or inject.
//   - --apply writes only the plugin's own inventory (--root), only to the
//     per-typ allowlist in zielAllowed, never to a path on the denylist in
//     zielDenied, and never through a symlink (target, target directory, .bak
//     or quelle; checked after filepath.EvalSymlinks and again at open time).
//   - entries from --repo are never written and never diffed; they get a status
//     only, and a target outside the allowlist is not even read for it.
//   - --plan shows a diff only for allowlisted, rechte=false plugin entries.

type setupEntry struct {
	ID           string `json:"id"`
	Typ          string `json:"typ"`
	Quelle       string `json:"quelle"`
	Ziel         string `json:"ziel"`
	Rechte       *bool  `json:"rechte"`
	Beschreibung string `json:"beschreibung"`
}

var validTyps = map[string]bool{
	"shim":           true,
	"githook":        true,
	"copy":           true,
	"rechte-vorlage": true,
	"marketplace":    true,
	"mcp":            true,
	"systemd":        true,
}

var (
	// setupIDPattern is the only accepted shape of an entry id. The id becomes
	// part of file names (cache file, shim name), so it must not carry a path
	// separator, a dot segment or a shell metacharacter.
	setupIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)
	// safePathPattern is the character set allowed in a file quelle and in a
	// ziel once its ${VAR} placeholders are removed: nothing a shell would
	// expand, split or glob.
	safePathPattern = regexp.MustCompile(`^[A-Za-z0-9._/+-]+$`)
	// safeArgPattern is the character set allowed in the quelle of a
	// marketplace or mcp entry, which ends up as an argument of a printed
	// command.
	safeArgPattern = regexp.MustCompile(`^[A-Za-z0-9._/:@+=-]+$`)
	// safeShellWord is what shellQuote leaves unquoted.
	safeShellWord = regexp.MustCompile(`^[A-Za-z0-9._/:@%+=,-]+$`)
	// systemdUnitPattern is the file name a systemd entry may write.
	systemdUnitPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._@-]*\.(service|timer)$`)
)

// zielPlaceholders are the only variables a ziel may use, longest first so
// "$XDG_..." is never mistaken for "$HOME" plus a suffix.
var zielPlaceholders = []string{
	"${XDG_CONFIG_HOME}", "$XDG_CONFIG_HOME",
	"${XDG_CACHE_HOME}", "$XDG_CACHE_HOME",
	"${XDG_DATA_HOME}", "$XDG_DATA_HOME",
	"${HOME}", "$HOME",
}

func parseInventory(data []byte) ([]setupEntry, error) {
	var entries []setupEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		return nil, fmt.Errorf("invalid JSON: %w", err)
	}
	seen := map[string]bool{}
	for i, e := range entries {
		if strings.TrimSpace(e.ID) == "" {
			return nil, fmt.Errorf("entry %d: missing or empty 'id'", i+1)
		}
		if !setupIDPattern.MatchString(e.ID) {
			return nil, fmt.Errorf("entry %d: invalid 'id' %q (allowed: %s)", i+1, e.ID, setupIDPattern.String())
		}
		if seen[e.ID] {
			return nil, fmt.Errorf("entry %d (%s): duplicate 'id'", i+1, e.ID)
		}
		seen[e.ID] = true
		if !validTyps[e.Typ] {
			return nil, fmt.Errorf("entry %d (%s): invalid 'typ' %q", i+1, e.ID, e.Typ)
		}
		if strings.TrimSpace(e.Quelle) == "" {
			return nil, fmt.Errorf("entry %d (%s): missing or empty 'quelle'", i+1, e.ID)
		}
		if strings.TrimSpace(e.Ziel) == "" {
			return nil, fmt.Errorf("entry %d (%s): missing or empty 'ziel'", i+1, e.ID)
		}
		if e.Rechte == nil {
			return nil, fmt.Errorf("entry %d (%s): missing 'rechte'", i+1, e.ID)
		}
		if strings.TrimSpace(e.Beschreibung) == "" {
			return nil, fmt.Errorf("entry %d (%s): missing or empty 'beschreibung'", i+1, e.ID)
		}
		if e.Typ == "rechte-vorlage" && !*e.Rechte {
			return nil, fmt.Errorf("entry %d (%s): typ rechte-vorlage requires rechte=true", i+1, e.ID)
		}
		if e.Typ == "marketplace" || e.Typ == "mcp" {
			if !safeArgPattern.MatchString(e.Quelle) {
				return nil, fmt.Errorf("entry %d (%s): 'quelle' contains characters outside %s", i+1, e.ID, safeArgPattern.String())
			}
		} else if !safePathPattern.MatchString(e.Quelle) {
			return nil, fmt.Errorf("entry %d (%s): 'quelle' contains characters outside %s", i+1, e.ID, safePathPattern.String())
		}
		if !zielCharsetOK(e.Ziel) {
			return nil, fmt.Errorf("entry %d (%s): 'ziel' may use only ${HOME}, ${XDG_CONFIG_HOME}, ${XDG_CACHE_HOME}, ${XDG_DATA_HOME} and characters in %s", i+1, e.ID, safePathPattern.String())
		}
	}
	return entries, nil
}

func zielCharsetOK(ziel string) bool {
	s := ziel
	for _, p := range zielPlaceholders {
		s = strings.ReplaceAll(s, p, "/")
	}
	return safePathPattern.MatchString(s)
}

// quelleEscapesRoot reports whether quelle, once cleaned, would resolve outside
// the inventory root it is joined with: an absolute path, or one that steps
// above the root via "..". Both are rejected so a crafted inventory (in
// particular an untrusted --repo's .imprint/setup.json) cannot make setup
// --plan read and echo back arbitrary files elsewhere on disk. Symlinks are
// handled separately by resolveQuelle.
func quelleEscapesRoot(quelle string) bool {
	cleaned := filepath.Clean(filepath.FromSlash(quelle))
	if filepath.IsAbs(cleaned) {
		return true
	}
	if cleaned == ".." || strings.HasPrefix(cleaned, ".."+string(filepath.Separator)) {
		return true
	}
	return false
}

// setupEnv holds the base directories setup works with. A value that is empty
// or not absolute counts as unset (XDG base directory spec); without an
// absolute HOME nothing is allowed at all.
type setupEnv struct {
	home, config, cache, data string
}

func currentSetupEnv() setupEnv {
	var env setupEnv
	if h := os.Getenv("HOME"); filepath.IsAbs(h) {
		env.home = filepath.Clean(h)
	}
	pick := func(key, fallback string) string {
		if v := os.Getenv(key); filepath.IsAbs(v) {
			return filepath.Clean(v)
		}
		if env.home == "" {
			return ""
		}
		return filepath.Join(env.home, fallback)
	}
	env.config = pick("XDG_CONFIG_HOME", ".config")
	env.cache = pick("XDG_CACHE_HOME", ".cache")
	env.data = pick("XDG_DATA_HOME", filepath.Join(".local", "share"))
	return env
}

func (env setupEnv) roots() []string {
	var out []string
	for _, r := range []string{env.home, env.config, env.cache, env.data} {
		if r != "" {
			out = append(out, r)
		}
	}
	return out
}

func (env setupEnv) replacer() *strings.Replacer {
	return strings.NewReplacer(
		"${XDG_CONFIG_HOME}", env.config, "$XDG_CONFIG_HOME", env.config,
		"${XDG_CACHE_HOME}", env.cache, "$XDG_CACHE_HOME", env.cache,
		"${XDG_DATA_HOME}", env.data, "$XDG_DATA_HOME", env.data,
		"${HOME}", env.home, "$HOME", env.home,
	)
}

// expandZiel replaces the ziel placeholders and cleans the result. It returns
// "" when HOME is unusable.
func expandZiel(ziel string) string {
	env := currentSetupEnv()
	if env.home == "" {
		return ""
	}
	return filepath.Clean(env.replacer().Replace(ziel))
}

func renderTemplate(content string) string {
	return currentSetupEnv().replacer().Replace(content)
}

// isWithin reports whether p is base or lies below it (lexically).
func isWithin(base, p string) bool {
	rel, err := filepath.Rel(base, p)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}

// isStrictlyWithin reports whether p lies below base, not at base itself.
func isStrictlyWithin(base, p string) bool {
	return isWithin(base, p) && filepath.Clean(base) != filepath.Clean(p)
}

// resolvePath resolves every symlink in the longest existing prefix of p and
// appends the not-yet-existing remainder. A dangling symlink anywhere on the
// way is an error, since what it would create is not knowable in advance.
func resolvePath(p string) (string, error) {
	cur := filepath.Clean(p)
	var rest []string
	for {
		r, err := filepath.EvalSymlinks(cur)
		if err == nil {
			return filepath.Join(append([]string{r}, rest...)...), nil
		}
		if !errors.Is(err, fs.ErrNotExist) {
			return "", err
		}
		if fi, lerr := os.Lstat(cur); lerr == nil && fi.Mode()&fs.ModeSymlink != 0 {
			return "", fmt.Errorf("%s is a dangling symlink", cur)
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return "", err
		}
		rest = append([]string{filepath.Base(cur)}, rest...)
		cur = parent
	}
}

func resolveAll(paths []string) []string {
	var out []string
	for _, p := range paths {
		if r, err := resolvePath(p); err == nil {
			out = append(out, r)
		}
	}
	return out
}

// zielEscapesErlaubteWurzeln reports whether ziel, after expandZiel, resolves
// outside the allowed root directories ($HOME and the XDG config, cache and
// data homes), lexically or after resolving symlinks, or is one of those roots
// itself.
func zielEscapesErlaubteWurzeln(ziel string) bool {
	env := currentSetupEnv()
	z := expandZiel(ziel)
	if z == "" || !filepath.IsAbs(z) {
		return true
	}
	if !withinAnyStrict(env.roots(), z) {
		return true
	}
	resolved, err := resolvePath(z)
	if err != nil {
		return true
	}
	return !withinAnyStrict(resolveAll(env.roots()), resolved)
}

func withinAnyStrict(bases []string, p string) bool {
	for _, b := range bases {
		if isStrictlyWithin(b, p) {
			return true
		}
	}
	return false
}

// deniedHomeEntries are paths below $HOME that always count as rights paths:
// --apply never writes them and --plan/--check never show their content.
var deniedHomeEntries = []string{
	".claude", ".gemini", ".codex", ".ssh", ".gnupg", ".password-store", ".pki",
	".aws", ".kube", ".docker", ".mozilla", ".gnome2/keyrings",
	".config/archiv", ".config/gh", ".config/git", ".config/environment.d",
	".config/autostart", ".config/fish", ".config/containers",
	".config/systemd/user/default.target.wants",
	".local/share/archiv", ".local/share/keyrings", ".local/share/kwalletd",
}

// deniedConfigEntries and deniedDataEntries repeat the XDG parts of the list
// for a relocated XDG_CONFIG_HOME / XDG_DATA_HOME.
var (
	deniedConfigEntries = []string{"archiv", "gh", "git", "environment.d", "autostart", "fish", "containers", "systemd/user/default.target.wants"}
	deniedDataEntries   = []string{"archiv", "keyrings", "kwalletd"}
)

// deniedHomeNamePrefixes deny any direct child of $HOME whose name starts with
// one of them (shell start-up files and credential files, e.g. .bashrc.d).
var deniedHomeNamePrefixes = []string{
	".bashrc", ".bash_profile", ".bash_login", ".bash_logout", ".profile",
	".zshrc", ".zshenv", ".zprofile", ".zlogin", ".pam_environment",
	".gitconfig", ".git-credentials", ".netrc", ".xprofile", ".xinitrc", ".xsession",
}

func (env setupEnv) denyPrefixes() []string {
	var out []string
	add := func(base string, rels []string) {
		if base == "" {
			return
		}
		for _, r := range rels {
			out = append(out, filepath.Join(base, filepath.FromSlash(r)))
		}
	}
	add(env.home, deniedHomeEntries)
	add(env.config, deniedConfigEntries)
	add(env.data, deniedDataEntries)
	return out
}

// zielDenied reports whether p (lexical or resolved; the caller checks both)
// is on the denylist. homes are the lexical and resolved $HOME.
func zielDenied(p string, prefixes []string, homes []string) bool {
	for _, d := range prefixes {
		if isWithin(d, p) {
			return true
		}
	}
	for _, h := range homes {
		if !isStrictlyWithin(h, p) {
			continue
		}
		rel, _ := filepath.Rel(h, p)
		first := strings.SplitN(rel, string(filepath.Separator), 2)[0]
		for _, pre := range deniedHomeNamePrefixes {
			if strings.HasPrefix(first, pre) {
				return true
			}
		}
	}
	for _, part := range strings.Split(p, string(filepath.Separator)) {
		if strings.HasSuffix(part, ".wants") || strings.HasSuffix(part, ".requires") {
			return true
		}
	}
	return false
}

// zielAllowed is the per-typ allowlist of what --apply may write. Only these
// targets exist for a rechte=false entry; githook, marketplace and mcp write no
// file at all. The same check runs on the lexical path with the lexical base
// directories and on the resolved path with the resolved base directories.
func zielAllowed(e setupEntry, p string, env setupEnv, resolve bool) bool {
	dir := func(base string, rel ...string) string {
		if base == "" {
			return ""
		}
		d := filepath.Join(append([]string{base}, rel...)...)
		if resolve {
			r, err := resolvePath(d)
			if err != nil {
				return ""
			}
			return r
		}
		return d
	}
	switch e.Typ {
	case "shim":
		bin := dir(env.home, ".local", "bin")
		return bin != "" && filepath.Dir(p) == bin && filepath.Base(p) == e.ID
	case "systemd":
		unitDir := dir(env.config, "systemd", "user")
		return unitDir != "" && filepath.Dir(p) == unitDir && systemdUnitPattern.MatchString(filepath.Base(p))
	case "copy":
		for _, d := range []string{dir(env.config, "imprint"), dir(env.data, "imprint")} {
			if d != "" && isStrictlyWithin(d, p) {
				return true
			}
		}
	}
	return false
}

// zielCheck is the outcome of checkZiel: either a usable target (status "")
// or a refusal with status gesperrt (denylist), verweigert (outside roots or
// allowlist) or nicht-prüfbar (symlink or unresolvable).
type zielCheck struct {
	lexical  string
	resolved string
	status   string
	reason   string
}

// checkZiel runs every path check on e's ziel. For a rechte=false entry the
// target must be on the allowlist and off the denylist; a rechte=true entry is
// never written by setup, so it only has to stay inside the roots. In every
// case the target itself must not be a symlink, and all checks run again on
// the path with the target directory's symlinks resolved.
func checkZiel(e setupEntry) zielCheck {
	env := currentSetupEnv()
	gated := entryIsRechteGated(e)
	z := expandZiel(e.Ziel)
	res := zielCheck{lexical: z}
	refuse := func(status, reason string) zielCheck {
		res.status, res.reason = status, reason
		return res
	}
	if z == "" || !filepath.IsAbs(z) || !withinAnyStrict(env.roots(), z) {
		return refuse("verweigert", fmt.Sprintf("ziel %q verlässt die erlaubten Wurzeln ($HOME, $XDG_CONFIG_HOME, $XDG_CACHE_HOME, $XDG_DATA_HOME)", e.Ziel))
	}
	homes := []string{env.home}
	if !gated && zielDenied(z, env.denyPrefixes(), homes) {
		return refuse("gesperrt", fmt.Sprintf("ziel %q liegt auf der Sperrliste (gilt immer als Rechte-Pfad)", e.Ziel))
	}
	dirRes, err := resolvePath(filepath.Dir(z))
	if err != nil {
		return refuse("nicht-prüfbar", fmt.Sprintf("ziel-Verzeichnis nicht auflösbar: %v", err))
	}
	zr := filepath.Join(dirRes, filepath.Base(z))
	res.resolved = zr
	if !withinAnyStrict(resolveAll(env.roots()), zr) {
		return refuse("verweigert", fmt.Sprintf("ziel %q verlässt nach Symlink-Auflösung die erlaubten Wurzeln", e.Ziel))
	}
	if !gated {
		if rh, err := resolvePath(env.home); err == nil {
			homes = append(homes, rh)
		}
		if zielDenied(zr, append(env.denyPrefixes(), resolveAll(env.denyPrefixes())...), homes) {
			return refuse("gesperrt", fmt.Sprintf("ziel %q führt über einen Symlink auf die Sperrliste", e.Ziel))
		}
	}
	if fi, err := os.Lstat(zr); err == nil && fi.Mode()&fs.ModeSymlink != 0 {
		return refuse("nicht-prüfbar", fmt.Sprintf("ziel %q ist ein Symlink; setup folgt keinem Symlink", e.Ziel))
	}
	if !gated && (!zielAllowed(e, z, env, false) || !zielAllowed(e, zr, env, true)) {
		return refuse("verweigert", fmt.Sprintf("ziel %q liegt nicht in der Allowlist für typ %s", e.Ziel, e.Typ))
	}
	return res
}

// resolveQuelle returns quelle's real path, which must lie inside the
// inventory root after every symlink is resolved.
func resolveQuelle(invRoot, quelle string) (string, error) {
	if quelleEscapesRoot(quelle) {
		return "", fmt.Errorf("quelle %q verlässt den Inventar-Root (kein absoluter Pfad oder Traversal erlaubt)", quelle)
	}
	rootRes, err := filepath.EvalSymlinks(invRoot)
	if err != nil {
		return "", fmt.Errorf("Inventar-Root nicht auflösbar: %v", err)
	}
	qRes, err := filepath.EvalSymlinks(filepath.Join(invRoot, filepath.FromSlash(quelle)))
	if err != nil {
		return "", fmt.Errorf("quelle unlesbar: %v", err)
	}
	if !isStrictlyWithin(rootRes, qRes) {
		return "", fmt.Errorf("quelle %q liegt nach Symlink-Auflösung außerhalb des Inventar-Roots", quelle)
	}
	return qRes, nil
}

// readRegularFile reads path without following a symlink at path and only
// if it is a regular file (no directory, FIFO or device).
func readRegularFile(path string) ([]byte, error) {
	fi, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if fi.Mode()&fs.ModeSymlink != 0 {
		return nil, fmt.Errorf("%s ist ein Symlink", path)
	}
	if !fi.Mode().IsRegular() {
		return nil, fmt.Errorf("%s ist keine reguläre Datei", path)
	}
	f, err := os.OpenFile(path, os.O_RDONLY|oNoFollow, 0)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return io.ReadAll(f)
}

// writeRegularFile writes data to path with mode perm, refusing a symlink or
// a non-regular file at path (checked with Lstat and again by O_NOFOLLOW).
func writeRegularFile(path string, data []byte, perm fs.FileMode) error {
	if fi, err := os.Lstat(path); err == nil {
		if fi.Mode()&fs.ModeSymlink != 0 {
			return fmt.Errorf("%s ist ein Symlink", path)
		}
		if !fi.Mode().IsRegular() {
			return fmt.Errorf("%s ist keine reguläre Datei", path)
		}
	} else if !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC|oNoFollow, perm)
	if err != nil {
		return err
	}
	if err := f.Chmod(perm); err != nil {
		f.Close()
		return err
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// shimContent is the shim a shim entry installs. quelle is placed in single
// quotes, so nothing in it is expanded; parseInventory already limits it to
// safePathPattern, and this check repeats that for defence in depth.
func shimContent(quelle string) (string, error) {
	if !safePathPattern.MatchString(quelle) || quelleEscapesRoot(quelle) {
		return "", fmt.Errorf("quelle %q ist für einen Shim nicht erlaubt", quelle)
	}
	return fmt.Sprintf("#!/bin/sh\nexec \"$IMPRINT_CORE_ROOT\"/'%s' \"$@\"\n", filepath.ToSlash(filepath.Clean(quelle))), nil
}

// shellQuote quotes s for a POSIX shell, so a printed command is safe to paste.
func shellQuote(s string) string {
	if s != "" && safeShellWord.MatchString(s) {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// checkEnvGuard reports whether --apply runs inside an agent session. It is
// advisory only (an agent can unset variables); the boundary is in the path
// checks. A variable counts when it is present, even with an empty value.
func checkEnvGuard() bool {
	if _, ok := os.LookupEnv("CLAUDECODE"); ok {
		return true
	}
	for _, kv := range os.Environ() {
		key, _, _ := strings.Cut(kv, "=")
		if strings.HasPrefix(key, "ANTIGRAVITY_") || strings.HasPrefix(key, "AGY_") {
			return true
		}
	}
	return false
}

type processedItem struct {
	invRoot  string
	repo     string
	entry    setupEntry
	fromRepo bool
}

func loadSetupInventory(root, repo string) ([]processedItem, string, error) {
	rootInvPath := filepath.Join(root, "setup", "inventar.json")
	rootData, err := os.ReadFile(rootInvPath)
	if err != nil {
		return nil, "", fmt.Errorf("cannot read %s: %w", rootInvPath, err)
	}

	rootEntries, err := parseInventory(rootData)
	if err != nil {
		return nil, "", fmt.Errorf("invalid inventory %s: %w", rootInvPath, err)
	}

	var items []processedItem
	for _, e := range rootEntries {
		items = append(items, processedItem{invRoot: root, repo: root, entry: e})
	}

	var repoNote string
	if repo != "" {
		repoInvPath := filepath.Join(repo, ".imprint", "setup.json")
		repoData, rErr := readRegularFile(repoInvPath)
		if rErr != nil {
			repoNote = fmt.Sprintf("note: repo setup inventory at %s is missing or unreadable (%v)", repoInvPath, rErr)
		} else {
			repoEntries, pErr := parseInventory(repoData)
			if pErr != nil {
				return nil, "", fmt.Errorf("invalid repo inventory %s: %w", repoInvPath, pErr)
			}
			for _, e := range repoEntries {
				items = append(items, processedItem{invRoot: repo, repo: repo, entry: e, fromRepo: true})
			}
		}
	}

	return items, repoNote, nil
}

func generateDiff(qContent, zContent string) string {
	qLines := splitLinesString(qContent)
	zLines := splitLinesString(zContent)
	edits := diffLines(qLines, zLines)
	if len(edits) > 15 {
		edits = append(edits[:15], "... (diff truncated)")
	}
	return strings.Join(edits, "\n")
}

func splitLinesString(s string) []string {
	s = strings.TrimPrefix(s, "\ufeff")
	s = strings.ReplaceAll(s, "\r\n", "\n")
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}

func diffLines(srcLines, dstLines []string) []string {
	m, n := len(srcLines), len(dstLines)
	dp := make([][]int, m+1)
	for i := range dp {
		dp[i] = make([]int, n+1)
	}
	for i := 0; i < m; i++ {
		for j := 0; j < n; j++ {
			if srcLines[i] == dstLines[j] {
				dp[i+1][j+1] = dp[i][j] + 1
			} else if dp[i+1][j] >= dp[i][j+1] {
				dp[i+1][j+1] = dp[i+1][j]
			} else {
				dp[i+1][j+1] = dp[i][j+1]
			}
		}
	}
	var edits []string
	i, j := m, n
	for i > 0 || j > 0 {
		if i > 0 && j > 0 && srcLines[i-1] == dstLines[j-1] {
			i--
			j--
		} else if j > 0 && (i == 0 || dp[i][j-1] >= dp[i-1][j]) {
			edits = append(edits, "+ "+dstLines[j-1])
			j--
		} else if i > 0 && (j == 0 || dp[i][j-1] < dp[i-1][j]) {
			edits = append(edits, "- "+srcLines[i-1])
			i--
		}
	}
	for k := 0; k < len(edits)/2; k++ {
		edits[k], edits[len(edits)-1-k] = edits[len(edits)-1-k], edits[k]
	}
	return edits
}

func runSetup(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("setup", flag.ContinueOnError)
	plan := fs.Bool("plan", false, "compare setup inventory entries against target files without writing")
	apply := fs.Bool("apply", false, "apply setup inventory entries to target files")
	check := fs.Bool("check", false, "check setup inventory entries for drift without writing")
	root := fs.String("root", ".", "plugin root directory")
	repo := fs.String("repo", "", "optional repository path (status only, never applied)")

	if done, code := parseFlags(fs, args, stderr); done {
		if code != exitOK {
			fmt.Fprint(stderr, usageText)
		}
		return code
	}

	modeCount := 0
	if *plan {
		modeCount++
	}
	if *apply {
		modeCount++
	}
	if *check {
		modeCount++
	}

	if modeCount != 1 {
		fmt.Fprintln(stderr, "imprint-dev setup: exactly one of --plan, --apply, or --check flag is required")
		fmt.Fprint(stderr, usageText)
		return exitError
	}

	if *apply && checkEnvGuard() {
		fmt.Fprintln(stderr, "imprint-dev setup: apply führt der Mensch aus")
		return exitError
	}

	items, repoNote, err := loadSetupInventory(*root, *repo)
	if err != nil {
		fmt.Fprintf(stderr, "imprint-dev setup: %v\n%s", err, usageText)
		return exitError
	}

	if *plan {
		return runSetupPlan(items, repoNote, stdout)
	} else if *apply {
		return runSetupApply(items, repoNote, stdout, stderr)
	}
	return runSetupCheck(items, repoNote, stdout)
}

// entryIsRechteGated reports whether an entry's target is rights-gated
// (rechte=true): its content is never printed by --plan/--check/--apply,
// only its status or the command a human would run.
func entryIsRechteGated(e setupEntry) bool {
	return e.Rechte != nil && *e.Rechte
}

// hasFileTarget reports whether the typ installs a file at ziel. githook only
// sets core.hooksPath; marketplace and mcp only print a command.
func hasFileTarget(typ string) bool {
	return typ != "githook" && typ != "marketplace" && typ != "mcp"
}

// inspectResult is the read-only status of one entry, shared by --plan,
// --check and the status-only report of --repo entries under --apply.
type inspectResult struct {
	status   string
	reason   string
	diffText string
}

func isDriftStatus(status string) bool {
	return status != "gleich" && status != "hinweis"
}

// inspectGithook reads core.hooksPath from the repository's own config only
// (--local, so a missing repository is an error rather than the global value).
func inspectGithook(repo string) inspectResult {
	out, err := exec.Command("git", "-C", repo, "config", "--local", "--get", "core.hooksPath").Output()
	if err == nil {
		if strings.TrimSpace(string(out)) == ".githooks" {
			return inspectResult{status: "gleich"}
		}
		return inspectResult{status: "abweichend"}
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
		return inspectResult{status: "fehlt"}
	}
	return inspectResult{status: "nicht-prüfbar", reason: fmt.Sprintf("git config core.hooksPath nicht lesbar: %v", err)}
}

// inspectItem computes an entry's status without writing. A diff is produced
// only when withDiff is set and the entry is a rechte=false entry of the
// plugin's own inventory whose target passed the allowlist; a --repo entry
// that is rights-gated or outside the allowlist is not even read.
func inspectItem(item processedItem, withDiff bool) inspectResult {
	e := item.entry
	switch e.Typ {
	case "marketplace", "mcp":
		return inspectResult{status: "hinweis"}
	case "githook":
		return inspectGithook(item.repo)
	}
	gated := entryIsRechteGated(e)
	if item.fromRepo && gated {
		return inspectResult{status: "gesperrt", reason: "rechte=true aus --repo: weder gelesen noch angewendet"}
	}
	if e.Typ != "shim" && quelleEscapesRoot(e.Quelle) {
		return inspectResult{status: "nicht-prüfbar", reason: fmt.Sprintf("quelle %q verlässt den Inventar-Root (kein absoluter Pfad oder Traversal erlaubt)", e.Quelle)}
	}
	zc := checkZiel(e)
	if zc.status != "" {
		return inspectResult{status: zc.status, reason: zc.reason}
	}

	var soll []byte
	if e.Typ == "shim" && !gated {
		content, err := shimContent(e.Quelle)
		if err != nil {
			return inspectResult{status: "verweigert", reason: err.Error()}
		}
		soll = []byte(content)
	} else {
		qPath, err := resolveQuelle(item.invRoot, e.Quelle)
		if err != nil {
			return inspectResult{status: "nicht-prüfbar", reason: err.Error()}
		}
		data, err := readRegularFile(qPath)
		if err != nil {
			return inspectResult{status: "nicht-prüfbar", reason: fmt.Sprintf("quelle unlesbar: %v", err)}
		}
		soll = data
		if gated {
			soll = []byte(renderTemplate(string(data)))
		}
	}

	ist, err := readRegularFile(zc.resolved)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return inspectResult{status: "fehlt"}
		}
		return inspectResult{status: "nicht-prüfbar", reason: fmt.Sprintf("ziel nicht lesbar: %v", err)}
	}
	if bytes.Equal(soll, ist) {
		return inspectResult{status: "gleich"}
	}
	res := inspectResult{status: "abweichend"}
	if withDiff && !gated && !item.fromRepo {
		res.diffText = generateDiff(string(soll), string(ist))
	}
	return res
}

func printStatusTable(stdout io.Writer, ids []setupEntry, statuses []string) {
	tw := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "id\ttyp\tstatus\tbeschreibung")
	for i, e := range ids {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", e.ID, e.Typ, statuses[i], e.Beschreibung)
	}
	tw.Flush()
}

const repoStatusOnlyNote = "note: entries from --repo are never applied and never diffed; only their status is shown"

func runSetupPlan(items []processedItem, repoNote string, stdout io.Writer) int {
	var entries []setupEntry
	var statuses []string
	var results []inspectResult
	hasRepo := false
	for _, item := range items {
		r := inspectItem(item, true)
		hasRepo = hasRepo || item.fromRepo
		entries = append(entries, item.entry)
		statuses = append(statuses, r.status)
		results = append(results, r)
	}

	if repoNote != "" {
		fmt.Fprintln(stdout, repoNote)
	}
	if hasRepo {
		fmt.Fprintln(stdout, repoStatusOnlyNote)
	}
	printStatusTable(stdout, entries, statuses)

	for i, r := range results {
		if r.reason != "" {
			fmt.Fprintf(stdout, "\n[%s] %s: %s\n", entries[i].ID, r.status, r.reason)
		} else if r.status == "abweichend" && r.diffText != "" {
			fmt.Fprintf(stdout, "\n--- diff for %s (quelle vs ziel) ---\n%s\n", entries[i].ID, r.diffText)
		}
	}
	return exitOK
}

func runSetupCheck(items []processedItem, repoNote string, stdout io.Writer) int {
	var entries []setupEntry
	var statuses []string
	var results []inspectResult
	hasDrift := false
	hasRepo := false
	for _, item := range items {
		r := inspectItem(item, false)
		hasRepo = hasRepo || item.fromRepo
		entries = append(entries, item.entry)
		statuses = append(statuses, r.status)
		results = append(results, r)
		if isDriftStatus(r.status) {
			hasDrift = true
		}
	}

	if repoNote != "" {
		fmt.Fprintln(stdout, repoNote)
	}
	if hasRepo {
		fmt.Fprintln(stdout, repoStatusOnlyNote)
	}
	printStatusTable(stdout, entries, statuses)
	for i, r := range results {
		if r.reason != "" {
			fmt.Fprintf(stdout, "\n[%s] %s: %s\n", entries[i].ID, r.status, r.reason)
		}
	}

	if hasDrift {
		return exitViolation
	}
	return exitOK
}

// writeTarget installs data at the checked, resolved target zr: unchanged if
// equal, else a .bak of the old content (with the old file's mode) and then
// the new content. A failed backup aborts before the target is touched.
func writeTarget(zr string, data []byte, perm fs.FileMode) (string, error) {
	fi, lerr := os.Lstat(zr)
	exists := lerr == nil
	if lerr != nil && !errors.Is(lerr, fs.ErrNotExist) {
		return "", lerr
	}
	var old []byte
	if exists {
		var err error
		old, err = readRegularFile(zr)
		if err != nil {
			return "", err
		}
		if bytes.Equal(old, data) {
			return "unverändert", nil
		}
	}
	dir := filepath.Dir(zr)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("Ordner konnte nicht erstellt werden: %v", err)
	}
	if again, err := filepath.EvalSymlinks(dir); err != nil || again != dir {
		return "", fmt.Errorf("ziel-Verzeichnis %s hat sich während der Prüfung verändert", dir)
	}
	if exists {
		if err := writeRegularFile(zr+".bak", old, fi.Mode().Perm()); err != nil {
			return "", fmt.Errorf(".bak konnte nicht geschrieben werden, ziel bleibt unverändert: %v", err)
		}
	}
	if err := writeRegularFile(zr, data, perm); err != nil {
		return "", err
	}
	return "geschrieben", nil
}

// writeRechteCache renders a rights template into $XDG_CACHE_HOME/imprint and
// returns the file's path. The id-derived file name runs through the same
// root and symlink checks as every other target.
func writeRechteCache(e setupEntry, zLexical string, rendered []byte) (string, error) {
	env := currentSetupEnv()
	if env.cache == "" {
		return "", errors.New("XDG_CACHE_HOME/HOME nicht gesetzt")
	}
	cacheDir := filepath.Join(env.cache, "imprint")
	if err := os.MkdirAll(cacheDir, 0o700); err != nil {
		return "", fmt.Errorf("cache dir konnte nicht erstellt werden: %v", err)
	}
	cacheRes, err := filepath.EvalSymlinks(cacheDir)
	if err != nil {
		return "", err
	}
	cacheRoot, err := resolvePath(env.cache)
	if err != nil || !isStrictlyWithin(cacheRoot, cacheRes) {
		return "", fmt.Errorf("cache dir %s verlässt $XDG_CACHE_HOME", cacheDir)
	}
	cachePath := filepath.Join(cacheRes, e.ID+"-"+filepath.Base(zLexical))
	if filepath.Dir(cachePath) != cacheRes {
		return "", fmt.Errorf("cache-Datei für %s verlässt den cache dir", e.ID)
	}
	if err := writeRegularFile(cachePath, rendered, 0o600); err != nil {
		return "", fmt.Errorf("cache-Datei konnte nicht geschrieben werden: %v", err)
	}
	return cachePath, nil
}

func runSetupApply(items []processedItem, repoNote string, stdout, stderr io.Writer) int {
	if repoNote != "" {
		fmt.Fprintln(stdout, repoNote)
	}

	var entries []setupEntry
	var statuses []string
	var failures []string
	hasRepo := false
	add := func(e setupEntry, status string) {
		entries = append(entries, e)
		statuses = append(statuses, status)
	}
	fail := func(e setupEntry, reason string) {
		add(e, "Fehler")
		failures = append(failures, fmt.Sprintf("[%s] %s", e.ID, reason))
	}

	for _, item := range items {
		e := item.entry

		// Entries from --repo are never written, rendered or turned into a
		// printed command: status only.
		if item.fromRepo {
			hasRepo = true
			add(e, inspectItem(item, false).status+" (repo, nicht angewendet)")
			continue
		}

		if hasFileTarget(e.Typ) || entryIsRechteGated(e) {
			zc := checkZiel(e)
			if zc.status != "" {
				fail(e, zc.status+": "+zc.reason)
				continue
			}

			if entryIsRechteGated(e) {
				qPath, err := resolveQuelle(item.invRoot, e.Quelle)
				if err != nil {
					fail(e, err.Error())
					continue
				}
				qData, err := readRegularFile(qPath)
				if err != nil {
					fail(e, fmt.Sprintf("quelle unlesbar: %v", err))
					continue
				}
				cachePath, err := writeRechteCache(e, zc.lexical, []byte(renderTemplate(string(qData))))
				if err != nil {
					fail(e, err.Error())
					continue
				}
				if fi, err := os.Lstat(zc.resolved); err == nil && fi.Mode().IsRegular() {
					fmt.Fprintf(stdout, "cp %s %s\n", shellQuote(zc.resolved), shellQuote(zc.resolved+".bak"))
				}
				fmt.Fprintf(stdout, "cp %s %s\n", shellQuote(cachePath), shellQuote(zc.resolved))
				add(e, "gedruckt")
				continue
			}

			var data []byte
			perm := fs.FileMode(0o644)
			if e.Typ == "shim" {
				content, err := shimContent(e.Quelle)
				if err != nil {
					fail(e, err.Error())
					continue
				}
				data, perm = []byte(content), 0o755
			} else {
				qPath, err := resolveQuelle(item.invRoot, e.Quelle)
				if err != nil {
					fail(e, err.Error())
					continue
				}
				data, err = readRegularFile(qPath)
				if err != nil {
					fail(e, fmt.Sprintf("quelle unlesbar: %v", err))
					continue
				}
			}
			status, err := writeTarget(zc.resolved, data, perm)
			if err != nil {
				fail(e, err.Error())
				continue
			}
			add(e, status)
			continue
		}

		switch e.Typ {
		case "githook":
			if inspectGithook(item.repo).status == "gleich" {
				add(e, "unverändert")
				continue
			}
			if err := exec.Command("git", "-C", item.repo, "config", "--local", "core.hooksPath", ".githooks").Run(); err != nil {
				fail(e, fmt.Sprintf("git config fehlgeschlagen: %v", err))
				continue
			}
			add(e, "geschrieben")
		case "marketplace":
			fmt.Fprintf(stdout, "claude plugin marketplace add %s\n", shellQuote(e.Quelle))
			add(e, "gedruckt")
		case "mcp":
			fmt.Fprintf(stdout, "claude mcp add %s %s\n", shellQuote(e.ID), shellQuote(e.Quelle))
			add(e, "gedruckt")
		default:
			fail(e, fmt.Sprintf("unbekannter typ %s", e.Typ))
		}
	}

	if hasRepo {
		fmt.Fprintln(stdout, repoStatusOnlyNote)
	}
	printStatusTable(stdout, entries, statuses)
	for _, f := range failures {
		fmt.Fprintln(stderr, "imprint-dev setup: "+f)
	}
	if len(failures) > 0 {
		return exitViolation
	}
	return exitOK
}
