package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"reflect"
	"regexp"
	"runtime"
	"strings"
	"text/tabwriter"
	"time"
	"unicode"
)

// Security boundary (imprint-core-CL-011): what setup may read, show and write is
// decided by the path checks in this file, never by the inventory's own "rechte"
// flag and never by the env guard (which is advisory only). In short:
//
//   - id must match setupIDPattern; quelle and ziel must use a small, shell-inert
//     character set, so no id or path can traverse or inject.
//   - --apply needs an explicit --root that is the plugin root this binary was
//     built from (pluginAnchors), carries .claude-plugin/plugin.json with name
//     "imprint" (no symlink on the way) and is not the --repo directory
//     (checkPluginRoot).
//   - --apply writes only the plugin's own inventory (--root), only to the
//     per-typ allowlist in zielAllowed (shim imprint-*, systemd
//     imprint-<id>.service|.timer), never to a path on the denylist in
//     zielDenied, and never through a symlink (target, target directory, .bak
//     or quelle; checked after filepath.EvalSymlinks and again at open time
//     through directory file descriptors, see setup_fs_linux.go). The
//     allowlist base and every directory between $HOME and the target must
//     not be a symlink; XDG_*_HOME counts only below $HOME.
//   - quelle is read the same way as a target: every component from the
//     resolved inventory root down is opened with openat and O_NOFOLLOW.
//   - written files get mode 0600, a shim 0755.
//   - rechte=true needs "anwenden"; --apply then prints only the human
//     script's call, never an install of the whole target.
//   - a target or .bak with more than one hard link is never read, shown or
//     replaced; files are replaced through a temp file and renameat.
//   - a shim never replaces a file without the shim marker line, nor a name
//     that is another command in PATH; a systemd unit whose name exists in
//     another unit directory is only printed, like rechte=true.
//   - entries from --repo are never written and never diffed; they get a status
//     only, and a target outside the allowlist is not even read for it.
//   - --plan shows a diff only for allowlisted, rechte=false plugin entries.

type setupEntry struct {
	ID           string `json:"id"`
	Typ          string `json:"typ"`
	Quelle       string `json:"quelle"`
	Ziel         string `json:"ziel"`
	Rechte       *bool  `json:"rechte"`
	Anwenden     string `json:"anwenden"`
	Beschreibung string `json:"beschreibung"`
}

// Values of the optional "anwenden" field. ersetzen (the default) replaces the
// whole target; fragment-merge (rechte=true JSON targets only) deep-merges the
// template into the target (jq's "*") and keeps every other key. For a
// rechte=true entry with either value --apply prints only the call of
// rechteAnwendenSkript; --plan/--check show the status only.
const (
	anwendenErsetzen      = "ersetzen"
	anwendenFragmentMerge = "fragment-merge"
)

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
)

// setupNamePrefix is the prefix every shim and systemd unit setup writes must
// carry, so no entry can take the name of an existing command or unit.
const setupNamePrefix = "imprint-"

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
		// A rights entry without "anwenden" would get a printed install that
		// replaces the whole target; only the human script applies it.
		if *e.Rechte && e.Anwenden == "" {
			return nil, fmt.Errorf("entry %d (%s): rechte=true requires 'anwenden' (ersetzen or fragment-merge)", i+1, e.ID)
		}
		switch e.Anwenden {
		case "", anwendenErsetzen:
		case anwendenFragmentMerge:
			if !*e.Rechte || !hasFileTarget(e.Typ) || e.Typ == "shim" || e.Typ == "systemd" || !strings.HasSuffix(e.Ziel, ".json") {
				return nil, fmt.Errorf("entry %d (%s): anwenden=fragment-merge requires rechte=true and a .json ziel of typ rechte-vorlage or copy", i+1, e.ID)
			}
		default:
			return nil, fmt.Errorf("entry %d (%s): invalid 'anwenden' %q (allowed: ersetzen, fragment-merge)", i+1, e.ID, e.Anwenden)
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
// or not absolute counts as unset (XDG base directory spec), and so does an
// XDG value that does not lie below $HOME (/etc, say) or a HOME of "/";
// without a usable HOME nothing is allowed at all.
type setupEnv struct {
	home, config, cache, data string
}

func currentSetupEnv() setupEnv {
	var env setupEnv
	if h := os.Getenv("HOME"); filepath.IsAbs(h) && filepath.Clean(h) != string(filepath.Separator) {
		env.home = filepath.Clean(h)
	}
	pick := func(key, fallback string) string {
		if env.home == "" {
			return ""
		}
		// A value outside $HOME ("/" or /etc, say) would make paths there
		// allowed roots; it counts as unset.
		if v := os.Getenv(key); filepath.IsAbs(v) && isStrictlyWithin(env.home, filepath.Clean(v)) {
			return filepath.Clean(v)
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

// resolvedRoots are the roots with their symlinks resolved, keeping only
// those that still lie within the resolved $HOME: an XDG directory below
// $HOME that is a symlink out of it adds no allowed root.
func (env setupEnv) resolvedRoots() []string {
	if env.home == "" {
		return nil
	}
	rh, err := resolvePath(env.home)
	if err != nil {
		return nil
	}
	var out []string
	for _, r := range resolveAll(env.roots()) {
		if isWithin(rh, r) {
			out = append(out, r)
		}
	}
	return out
}

// symlinkBelowHome returns the first existing component of dir strictly
// below $HOME that is a symlink, or "". Components are checked from $HOME
// down and the walk stops at the first one that does not exist.
func (env setupEnv) symlinkBelowHome(dir string) string {
	if env.home == "" || !isStrictlyWithin(env.home, dir) {
		return ""
	}
	rel, err := filepath.Rel(env.home, dir)
	if err != nil {
		return ""
	}
	cur := env.home
	for _, name := range strings.Split(rel, string(filepath.Separator)) {
		cur = filepath.Join(cur, name)
		fi, err := os.Lstat(cur)
		if err != nil {
			return ""
		}
		if fi.Mode()&fs.ModeSymlink != 0 {
			return cur
		}
	}
	return ""
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
	return !withinAnyStrict(env.resolvedRoots(), resolved)
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
		return bin != "" && filepath.Dir(p) == bin && filepath.Base(p) == e.ID && strings.HasPrefix(e.ID, setupNamePrefix)
	case "systemd":
		unitDir := dir(env.config, "systemd", "user")
		base := filepath.Base(p)
		return unitDir != "" && filepath.Dir(p) == unitDir &&
			(base == setupNamePrefix+e.ID+".service" || base == setupNamePrefix+e.ID+".timer")
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
	if !withinAnyStrict(env.resolvedRoots(), zr) {
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
	if !gated {
		if l := env.symlinkBelowHome(filepath.Dir(z)); l != "" {
			return refuse("verweigert", fmt.Sprintf("ziel %q: %s ist ein Symlink; die Allowlist-Basis und ihre Vorfahren unterhalb von $HOME dürfen keine Symlinks sein", e.Ziel, l))
		}
	}
	if !gated && (!zielAllowed(e, z, env, false) || !zielAllowed(e, zr, env, true)) {
		return refuse("verweigert", fmt.Sprintf("ziel %q liegt nicht in der Allowlist für typ %s", e.Ziel, e.Typ))
	}
	return res
}

// resolveQuelle returns the resolved inventory root and quelle's real path,
// which must lie inside that root after every symlink is resolved. The
// result is only a checked name: read it with openQuelle, which walks it
// again without following any symlink.
func resolveQuelle(invRoot, quelle string) (rootRes, qRes string, err error) {
	if quelleEscapesRoot(quelle) {
		return "", "", fmt.Errorf("quelle %q verlässt den Inventar-Root (kein absoluter Pfad oder Traversal erlaubt)", quelle)
	}
	rootRes, err = resolveDir(invRoot)
	if err != nil {
		return "", "", fmt.Errorf("Inventar-Root nicht auflösbar: %v", err)
	}
	qRes, err = filepath.EvalSymlinks(filepath.Join(rootRes, filepath.FromSlash(quelle)))
	if err != nil {
		return "", "", fmt.Errorf("quelle unlesbar: %v", err)
	}
	if !isStrictlyWithin(rootRes, qRes) {
		return "", "", fmt.Errorf("quelle %q liegt nach Symlink-Auflösung außerhalb des Inventar-Roots", quelle)
	}
	return rootRes, qRes, nil
}

// openQuelle opens the checked quelle qRes below rootRes the way a target is
// opened: every directory component from rootRes down with openat and
// O_NOFOLLOW (openDirBeneath), the file itself with O_NOFOLLOW, regular and
// with a single link (openRegularAt). A component swapped for a symlink
// after resolveQuelle (into ~/.ssh, say) is not followed.
func openQuelle(rootRes, qRes string) (*os.File, fs.FileInfo, error) {
	if !isStrictlyWithin(rootRes, qRes) {
		return nil, nil, fmt.Errorf("%s liegt nicht unter %s", qRes, rootRes)
	}
	d, err := openDirBeneath(rootRes, filepath.Dir(qRes), false, 0)
	if err != nil {
		return nil, nil, err
	}
	defer d.Close()
	return openRegularAt(d, filepath.Base(qRes))
}

// resolveDir returns dir as an absolute path with every symlink resolved.
func resolveDir(dir string) (string, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(abs)
}

// afterQuelleResolved runs between resolveQuelle and openQuelle. It does
// nothing; a test replaces it to swap a directory at exactly that moment, the
// window race3.sh hit by chance.
var afterQuelleResolved = func(invRoot, quelle string) {}

// readQuelle resolves, checks and reads quelle of an inventory root. The
// FileInfo is the opened file's (fstat), not a second lookup by name.
func readQuelle(invRoot, quelle string) ([]byte, fs.FileInfo, error) {
	rootRes, qRes, err := resolveQuelle(invRoot, quelle)
	if err != nil {
		return nil, nil, err
	}
	afterQuelleResolved(invRoot, quelle)
	f, fi, err := openQuelle(rootRes, qRes)
	if err != nil {
		return nil, nil, fmt.Errorf("quelle unlesbar: %v", err)
	}
	defer f.Close()
	data, err := io.ReadAll(f)
	if err != nil {
		return nil, nil, fmt.Errorf("quelle unlesbar: %v", err)
	}
	return data, fi, nil
}

// readBeneath reads rel, a file of an inventory root or of --repo (the plugin
// manifest, an inventory), after resolving only base. Every component below
// base is opened with O_NOFOLLOW, so neither a directory on the way (such as
// .claude-plugin/) nor the file may be a symlink; the file must be regular
// with a single link (openRegularAt).
func readBeneath(base, rel string) ([]byte, error) {
	baseRes, err := resolveDir(base)
	if err != nil {
		return nil, err
	}
	p := filepath.Join(baseRes, filepath.FromSlash(rel))
	if !isStrictlyWithin(baseRes, p) {
		return nil, fmt.Errorf("%s liegt nicht unter %s", rel, base)
	}
	d, err := openDirBeneath(baseRes, filepath.Dir(p), false, 0)
	if err != nil {
		return nil, err
	}
	defer d.Close()
	data, _, err := readAt(d, filepath.Base(p))
	return data, err
}

// openTargetDir opens the resolved target directory dir through directory file
// descriptors, starting at the longest existing resolved allowed root that
// contains it. A component swapped for a symlink after checkZiel is then not
// followed (Linux; see setup_fs_linux.go).
func openTargetDir(dir string, create bool, perm fs.FileMode) (*os.File, error) {
	base := ""
	for _, r := range currentSetupEnv().resolvedRoots() {
		if len(r) <= len(base) || !isWithin(r, dir) {
			continue
		}
		if fi, err := os.Lstat(r); err == nil && fi.IsDir() {
			base = r
		}
	}
	if base == "" {
		return nil, fmt.Errorf("%s liegt unter keiner vorhandenen erlaubten Wurzel", dir)
	}
	return openDirBeneath(base, dir, create, perm)
}

// openRegularAt opens name in d for reading, never through a symlink, and only
// if it is a regular file with one link: a hard link planted at a target or a
// .bak would otherwise let setup show, copy or replace another file's content
// (a key under ~/.ssh, say).
func openRegularAt(d *os.File, name string) (*os.File, fs.FileInfo, error) {
	f, err := openAt(d, name, os.O_RDONLY|oNonBlock, 0)
	if err != nil {
		return nil, nil, err
	}
	fi, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, nil, err
	}
	if !fi.Mode().IsRegular() {
		f.Close()
		return nil, nil, fmt.Errorf("%s ist keine reguläre Datei", f.Name())
	}
	if n := nlinkOf(fi); n > 1 {
		f.Close()
		return nil, nil, fmt.Errorf("%s hat %d Hardlinks; setup liest und ersetzt nur Dateien mit einem Link", f.Name(), n)
	}
	return f, fi, nil
}

func readAt(d *os.File, name string) ([]byte, fs.FileInfo, error) {
	f, fi, err := openRegularAt(d, name)
	if err != nil {
		return nil, nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(f)
	return data, fi, err
}

// readTarget reads the checked, resolved target zr through openTargetDir.
func readTarget(zr string) ([]byte, error) {
	d, err := openTargetDir(filepath.Dir(zr), false, 0)
	if err != nil {
		return nil, err
	}
	defer d.Close()
	data, _, err := readAt(d, filepath.Base(zr))
	return data, err
}

// checkReplaceable reports whether name in d may be replaced: absent, or a
// regular file with one link (no symlink, directory, FIFO or hard link).
func checkReplaceable(d *os.File, name string) error {
	f, _, err := openRegularAt(d, name)
	if err == nil {
		return f.Close()
	}
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	return err
}

// replaceAt writes data to name in d through a new temp file in the same
// directory and renameat, so setup never writes into an existing file, nor
// into whatever a link at name points to.
func replaceAt(d *os.File, name string, data []byte, perm fs.FileMode) error {
	if err := checkReplaceable(d, name); err != nil {
		return err
	}
	var rnd [8]byte
	if _, err := rand.Read(rnd[:]); err != nil {
		return err
	}
	tmp := "." + name + ".imprint-tmp-" + hex.EncodeToString(rnd[:])
	f, err := openAt(d, tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, perm)
	if err != nil {
		return err
	}
	err = f.Chmod(perm)
	if err == nil {
		_, err = f.Write(data)
	}
	if err == nil {
		err = f.Sync()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = renameAt(d, tmp, name)
	}
	if err != nil {
		_ = unlinkAt(d, tmp)
		return err
	}
	return nil
}

// installFile installs data at the checked, resolved target zr: unchanged if
// equal; otherwise, once before (if set) has accepted the old content, a .bak
// of the old content with the old mode and then the new content. Directories
// are created only for a target that does not exist yet, after every refusal;
// a .bak that cannot be replaced (replaceAt) leaves the target as it was.
func installFile(zr string, data []byte, perm, dirPerm fs.FileMode, backup bool, before func(old []byte, exists bool) error) (string, error) {
	dir, name := filepath.Dir(zr), filepath.Base(zr)
	d, err := openTargetDir(dir, false, 0)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return "", err
	}
	var old []byte
	var oldFi fs.FileInfo
	exists := false
	if d != nil {
		defer d.Close()
		old, oldFi, err = readAt(d, name)
		switch {
		case err == nil:
			exists = true
		case !errors.Is(err, fs.ErrNotExist):
			return "", err
		}
	}
	if exists && bytes.Equal(old, data) {
		return "unverändert", nil
	}
	if before != nil {
		if err := before(old, exists); err != nil {
			return "", err
		}
	}
	if d == nil {
		if d, err = openTargetDir(dir, true, dirPerm); err != nil {
			return "", fmt.Errorf("Ordner konnte nicht erstellt werden: %v", err)
		}
		defer d.Close()
	}
	if exists && backup {
		if err := replaceAt(d, name+".bak", old, oldFi.Mode().Perm()); err != nil {
			return "", fmt.Errorf(".bak konnte nicht geschrieben werden, ziel bleibt unverändert: %v", err)
		}
	}
	if err := replaceAt(d, name, data, perm); err != nil {
		return "", err
	}
	return "geschrieben", nil
}

// writeMode is the mode of a file setup writes: 0600, except a shim, which
// must be executable (0755). Nothing in the inventory can widen it.
func writeMode(typ string) fs.FileMode {
	if typ == "shim" {
		return 0o755
	}
	return 0o600
}

// shimMarker is the second line of every shim setup writes. --apply replaces
// only a file that starts with it; anything else in ~/.local/bin is foreign.
const shimMarker = "# imprint-shim: erzeugt von imprint-dev setup --apply; setup ersetzt nur Dateien mit dieser Zeile"

// shimContent is the shim a shim entry installs. quelle is placed in single
// quotes, so nothing in it is expanded; parseInventory already limits it to
// safePathPattern, and this check repeats that for defence in depth. An
// unset or empty IMPRINT_CORE_ROOT stops the shim (${...:?}) instead of
// running /<quelle>.
func shimContent(quelle string) (string, error) {
	if !safePathPattern.MatchString(quelle) || quelleEscapesRoot(quelle) {
		return "", fmt.Errorf("quelle %q ist für einen Shim nicht erlaubt", quelle)
	}
	return fmt.Sprintf("#!/bin/sh\n%s\nexec \"${IMPRINT_CORE_ROOT:?}\"/'%s' \"$@\"\n", shimMarker, filepath.ToSlash(filepath.Clean(quelle))), nil
}

// isSetupShim reports whether content is a shim setup wrote (marker line).
func isSetupShim(content []byte) bool {
	return bytes.HasPrefix(content, []byte("#!/bin/sh\n"+shimMarker+"\n"))
}

// shimPathConflict returns another executable of the given name in an
// absolute PATH directory other than the shim directory, or "". A shim of
// that name would shadow it or be shadowed by it.
func shimPathConflict(name string) string {
	env := currentSetupEnv()
	own := map[string]bool{}
	if env.home != "" {
		shimDir := filepath.Join(env.home, ".local", "bin")
		own[shimDir] = true
		if r, err := resolvePath(shimDir); err == nil {
			own[r] = true
		}
	}
	for _, dir := range filepath.SplitList(os.Getenv("PATH")) {
		dir = expandPathTilde(dir, env.home)
		if !filepath.IsAbs(dir) {
			continue
		}
		dir = filepath.Clean(dir)
		if own[dir] {
			continue
		}
		if r, err := filepath.EvalSymlinks(dir); err == nil && own[r] {
			continue
		}
		cand := filepath.Join(dir, name)
		if fi, err := os.Stat(cand); err == nil && !fi.IsDir() && fi.Mode().Perm()&0o111 != 0 {
			return cand
		}
	}
	return ""
}

// expandPathTilde expands a leading "~" of a PATH element the way bash does
// when it searches PATH: "~" and "~/x" become $HOME, "~+" $PWD, "~-" $OLDPWD
// and "~name" that user's home directory. Anything it cannot expand stays as
// it is (and is then relative, which bash does not find either).
func expandPathTilde(dir, home string) string {
	if !strings.HasPrefix(dir, "~") {
		return dir
	}
	name, rest, _ := strings.Cut(dir[1:], "/")
	base := ""
	switch name {
	case "":
		base = home
	case "+":
		base = os.Getenv("PWD")
	case "-":
		base = os.Getenv("OLDPWD")
	default:
		if u, err := user.Lookup(name); err == nil {
			base = u.HomeDir
		}
	}
	if !filepath.IsAbs(base) {
		return dir
	}
	return filepath.Join(base, rest)
}

// systemdUnitPathsFromTool lists the user unit directories systemd itself
// reports; nil when systemd-analyze is missing, slow or fails.
var systemdUnitPathsFromTool = func() []string {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "systemd-analyze", "--user", "unit-paths").Output()
	if err != nil {
		return nil
	}
	return strings.Split(strings.TrimSpace(string(out)), "\n")
}

// systemdUserUnitDirs is the user unit search path: what systemd-analyze
// reports plus the documented defaults (systemd.unit(5)), so a missing tool
// never narrows the check.
func systemdUserUnitDirs() []string {
	env := currentSetupEnv()
	var out []string
	add := func(p string) {
		if filepath.IsAbs(p) {
			out = append(out, filepath.Clean(p))
		}
	}
	for _, p := range systemdUnitPathsFromTool() {
		add(strings.TrimSpace(p))
	}
	if env.config != "" {
		add(filepath.Join(env.config, "systemd", "user.control"))
	}
	if env.data != "" {
		add(filepath.Join(env.data, "systemd", "user"))
	}
	if rt := os.Getenv("XDG_RUNTIME_DIR"); filepath.IsAbs(rt) {
		for _, s := range []string{"user.control", "transient", "generator.early", "user", "generator", "generator.late"} {
			add(filepath.Join(rt, "systemd", s))
		}
	}
	list := func(key, fallback string) []string {
		if v := os.Getenv(key); v != "" {
			return filepath.SplitList(v)
		}
		return filepath.SplitList(fallback)
	}
	for _, d := range list("XDG_CONFIG_DIRS", "/etc/xdg") {
		add(filepath.Join(d, "systemd", "user"))
	}
	for _, d := range list("XDG_DATA_DIRS", "/usr/local/share:/usr/share") {
		add(filepath.Join(d, "systemd", "user"))
	}
	for _, p := range []string{"/etc/systemd/user", "/run/systemd/user", "/usr/local/lib/systemd/user", "/usr/lib/systemd/user", "/lib/systemd/user"} {
		add(p)
	}
	return out
}

// systemdUnitElsewhere returns a unit of the given name in another systemd
// user unit directory, or "". A unit of the same name there is overridden by
// (or overrides) the one setup would write, and may already be enabled; such
// an entry is only printed, like rechte=true.
func systemdUnitElsewhere(name string) string {
	env := currentSetupEnv()
	own := map[string]bool{}
	if env.config != "" {
		ownDir := filepath.Join(env.config, "systemd", "user")
		own[ownDir] = true
		if r, err := resolvePath(ownDir); err == nil {
			own[r] = true
		}
	}
	for _, dir := range systemdUserUnitDirs() {
		if own[dir] {
			continue
		}
		if r, err := resolvePath(dir); err == nil && own[r] {
			continue
		}
		p := filepath.Join(dir, name)
		if _, err := os.Lstat(p); err == nil {
			return p
		}
	}
	return ""
}

// displaySafe escapes control and bidi characters (except newline and tab),
// so a unit shown before it is written cannot hide lines from the reader.
func displaySafe(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r == '\n' || r == '\t':
			b.WriteRune(r)
		case unicode.IsControl(r) || unicode.Is(unicode.Bidi_Control, r):
			fmt.Fprintf(&b, "\\u%04x", r)
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// unitLinePrefix starts every shown unit line, so a line in the unit that
// imitates the end marker cannot end the display early.
const unitLinePrefix = "│ "

// systemdDisplay is what --apply prints before it writes a unit: units run
// code once enabled, so the human sees every unit setup copies.
func systemdDisplay(zr string, data []byte) string {
	var b strings.Builder
	fmt.Fprintf(&b, "--- systemd-Unit %s (setup führt kein systemctl aus) ---\n", zr)
	for _, line := range strings.Split(strings.TrimSuffix(displaySafe(string(data)), "\n"), "\n") {
		b.WriteString(unitLinePrefix + line + "\n")
	}
	fmt.Fprintf(&b, "--- Ende %s ---\n", filepath.Base(zr))
	return b.String()
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

// pluginName is the name .claude-plugin/plugin.json must carry for --apply.
const pluginName = "imprint"

// pluginAnchors returns the plugin roots --apply accepts as --root. The
// anchor is the source tree the running binary was compiled from: the Go
// compiler records each source file's absolute path (runtime.Caller), and
// this file lies at <root>/tools/imprint-dev/. That path is fixed at build
// time, so neither a crafted directory with a copied manifest nor a checkout
// whose git remote claims mankind806/imprint-core (git remote set-url forges
// that in one line) can match it. The Claude plugin cache is not an anchor:
// imprint-dev is not installed from it and the hosts keep it in different,
// undocumented places. A binary built with -trimpath has no absolute source
// path and therefore no anchor: --apply then refuses. Tests replace this
// variable.
var pluginAnchors = func() []string {
	if r := binarySourceRoot(); r != "" {
		return []string{r}
	}
	return nil
}

// binarySourceRoot is the plugin root this binary was compiled from, or "".
func binarySourceRoot() string {
	_, file, _, ok := runtime.Caller(0)
	if !ok || !filepath.IsAbs(file) {
		return ""
	}
	dir := filepath.Dir(file)
	if filepath.Base(dir) != "imprint-dev" || filepath.Base(filepath.Dir(dir)) != "tools" {
		return ""
	}
	return filepath.Dir(filepath.Dir(dir))
}

// checkPluginRoot is the identity check --apply runs on --root: the plugin
// manifest must be a regular file naming this plugin, reached without any
// symlink below --root (.claude-plugin/ included); --root must be the plugin
// root this binary was built from (pluginAnchors) and must not be the --repo
// directory, so a foreign or crafted checkout is never applied.
func checkPluginRoot(root, repo string) error {
	manifest := filepath.Join(root, ".claude-plugin", "plugin.json")
	data, err := readBeneath(root, ".claude-plugin/plugin.json")
	if err != nil {
		return fmt.Errorf("--root %s ist kein imprint-Plugin: %s nicht lesbar (%v)", root, manifest, err)
	}
	var m struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(data, &m); err != nil || m.Name != pluginName {
		return fmt.Errorf("--root %s ist kein imprint-Plugin: %s trägt nicht name %q", root, manifest, pluginName)
	}
	anchors := pluginAnchors()
	if len(anchors) == 0 {
		return fmt.Errorf("dieses imprint-dev trägt keinen Quellpfad (gebaut mit -trimpath?); --apply braucht ein Binary, das aus dem Plugin-Root gebaut ist (go build ohne -trimpath, oder go run ./tools/imprint-dev im Plugin-Root)")
	}
	rootFi, err := os.Stat(root)
	if err != nil {
		return fmt.Errorf("--root %s nicht lesbar: %v", root, err)
	}
	anchored := false
	for _, a := range anchors {
		if fi, err := os.Stat(a); err == nil && os.SameFile(rootFi, fi) {
			anchored = true
		}
	}
	if !anchored {
		return fmt.Errorf("--root %s ist nicht der Plugin-Root, aus dem dieses imprint-dev gebaut ist (%s); --apply verweigert", root, strings.Join(anchors, ", "))
	}
	if repo != "" {
		rootFi, err1 := os.Stat(root)
		repoFi, err2 := os.Stat(repo)
		if err1 == nil && err2 == nil && os.SameFile(rootFi, repoFi) {
			return fmt.Errorf("--root und --repo sind dasselbe Verzeichnis; --apply verweigert")
		}
	}
	return nil
}

type processedItem struct {
	invRoot  string
	repo     string
	entry    setupEntry
	fromRepo bool
}

// loadSetupInventory reads the plugin inventory and, with repo set, the repo
// inventory. repoUnchecked reports a repo inventory that could not be read
// (missing, symlink, unreadable); an invalid one is an error.
func loadSetupInventory(root, repo string) (items []processedItem, repoNote string, repoUnchecked bool, err error) {
	rootInvPath := filepath.Join(root, "setup", "inventar.json")
	rootData, err := readBeneath(root, "setup/inventar.json")
	if err != nil {
		return nil, "", false, fmt.Errorf("cannot read %s: %w", rootInvPath, err)
	}

	rootEntries, err := parseInventory(rootData)
	if err != nil {
		return nil, "", false, fmt.Errorf("invalid inventory %s: %w", rootInvPath, err)
	}

	for _, e := range rootEntries {
		items = append(items, processedItem{invRoot: root, repo: root, entry: e})
	}

	if repo != "" {
		repoInvPath := filepath.Join(repo, ".imprint", "setup.json")
		repoData, rErr := readBeneath(repo, ".imprint/setup.json")
		if rErr != nil {
			repoNote = fmt.Sprintf("note: repo setup inventory at %s is missing or unreadable (%v)", repoInvPath, rErr)
			repoUnchecked = true
		} else {
			repoEntries, pErr := parseInventory(repoData)
			if pErr != nil {
				return nil, "", false, fmt.Errorf("invalid repo inventory %s: %w", repoInvPath, pErr)
			}
			for _, e := range repoEntries {
				items = append(items, processedItem{invRoot: repo, repo: repo, entry: e, fromRepo: true})
			}
		}
	}

	return items, repoNote, repoUnchecked, nil
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
	root := fs.String("root", ".", "plugin root directory (required with --apply)")
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

	if *apply {
		rootSet := false
		fs.Visit(func(f *flag.Flag) {
			if f.Name == "root" {
				rootSet = true
			}
		})
		if !rootSet {
			fmt.Fprintln(stderr, "imprint-dev setup: --apply verlangt --root <Plugin-Root>; ohne --root wird nichts angewendet")
			return exitError
		}
		if err := checkPluginRoot(*root, *repo); err != nil {
			fmt.Fprintf(stderr, "imprint-dev setup: %v\n", err)
			return exitError
		}
	}

	items, repoNote, repoUnchecked, err := loadSetupInventory(*root, *repo)
	if err != nil {
		fmt.Fprintf(stderr, "imprint-dev setup: %v\n%s", err, usageText)
		return exitError
	}

	if *plan {
		return runSetupPlan(items, repoNote, stdout)
	} else if *apply {
		return runSetupApply(items, repoNote, stdout, stderr)
	}
	return runSetupCheck(items, repoNote, repoUnchecked, stdout)
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
// --check and the status-only report of --repo entries under --apply. mensch
// marks a plugin entry only a human applies (rechte=true, or a systemd unit
// whose name exists in another unit directory) whose path checks passed: its
// comparison is shown, but --check does not count it as drift.
type inspectResult struct {
	status   string
	reason   string
	diffText string
	mensch   bool
}

func isDriftStatus(status string) bool {
	return status != "gleich" && status != "hinweis"
}

// displayStatus is the status column; a mensch entry carries "(Mensch)".
func (r inspectResult) displayStatus() string {
	if r.mensch {
		return r.status + " (Mensch)"
	}
	return r.status
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

// deepMerge is jq's "a * b": objects merge recursively, anything else in b
// replaces a.
func deepMerge(a, b any) any {
	am, aok := a.(map[string]any)
	bm, bok := b.(map[string]any)
	if !aok || !bok {
		return b
	}
	out := make(map[string]any, len(am)+len(bm))
	for k, v := range am {
		out[k] = v
	}
	for k, v := range bm {
		out[k] = deepMerge(am[k], v)
	}
	return out
}

// fragmentStatus compares a fragment-merge template with its target: gleich
// when deep-merging the template into the target changes nothing. Neither
// content is ever returned, only the status.
func fragmentStatus(soll, ist []byte) (status, reason string) {
	var tmpl, cur any
	if err := json.Unmarshal(soll, &tmpl); err != nil {
		return "nicht-prüfbar", "Vorlage ist kein gültiges JSON"
	}
	if _, ok := tmpl.(map[string]any); !ok {
		return "nicht-prüfbar", "Vorlage ist kein JSON-Objekt"
	}
	if err := json.Unmarshal(ist, &cur); err != nil {
		return "nicht-prüfbar", "ziel ist kein gültiges JSON"
	}
	if _, ok := cur.(map[string]any); !ok {
		return "nicht-prüfbar", "ziel ist kein JSON-Objekt"
	}
	if reflect.DeepEqual(deepMerge(cur, tmpl), cur) {
		return "gleich", ""
	}
	return "abweichend", ""
}

// inspectItem computes an entry's status without writing. A diff is produced
// only when withDiff is set and the entry is a rechte=false entry of the
// plugin's own inventory whose target passed the allowlist and is not only
// printed; a --repo entry that is rights-gated or outside the allowlist is
// not even read.
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

	res := inspectResult{mensch: gated && !item.fromRepo}
	if e.Typ == "shim" && !gated {
		if other := shimPathConflict(filepath.Base(zc.lexical)); other != "" {
			return inspectResult{status: "verweigert", reason: fmt.Sprintf("Shim-Name ist schon ein anderes Kommando in PATH (%s)", other)}
		}
	}
	if e.Typ == "systemd" && !gated && !item.fromRepo {
		if other := systemdUnitElsewhere(filepath.Base(zc.lexical)); other != "" {
			res.mensch = true
			res.reason = fmt.Sprintf("Unit-Name existiert auch in %s; --apply druckt nur, wie bei rechte=true", other)
		}
	}

	var soll []byte
	if e.Typ == "shim" && !gated {
		content, err := shimContent(e.Quelle)
		if err != nil {
			return inspectResult{status: "verweigert", reason: err.Error()}
		}
		soll = []byte(content)
	} else {
		data, _, err := readQuelle(item.invRoot, e.Quelle)
		if err != nil {
			return inspectResult{status: "nicht-prüfbar", reason: err.Error()}
		}
		soll = data
		if gated {
			soll = []byte(renderTemplate(string(data)))
		}
	}

	ist, err := readTarget(zc.resolved)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			res.status = "fehlt"
			return res
		}
		return inspectResult{status: "nicht-prüfbar", reason: fmt.Sprintf("ziel nicht lesbar: %v", err)}
	}
	if e.Typ == "shim" && !gated && !isSetupShim(ist) {
		return inspectResult{status: "verweigert", reason: "ziel ist eine fremde Datei (keine imprint-Shim-Zeile); setup ersetzt sie nie"}
	}
	if gated && e.Anwenden == anwendenFragmentMerge {
		status, reason := fragmentStatus(soll, ist)
		if status == "nicht-prüfbar" {
			return inspectResult{status: status, reason: reason}
		}
		res.status = status
		return res
	}
	if bytes.Equal(soll, ist) {
		res.status = "gleich"
		return res
	}
	res.status = "abweichend"
	if withDiff && !gated && !res.mensch && !item.fromRepo {
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

const menschNote = "note: (Mensch) = only a human applies this target (rechte=true, or a systemd unit whose name exists elsewhere); setup shows the status only and --check does not count it as drift"

func runSetupPlan(items []processedItem, repoNote string, stdout io.Writer) int {
	var entries []setupEntry
	var statuses []string
	var results []inspectResult
	hasRepo, hasMensch := false, false
	for _, item := range items {
		r := inspectItem(item, true)
		hasRepo = hasRepo || item.fromRepo
		hasMensch = hasMensch || r.mensch
		entries = append(entries, item.entry)
		statuses = append(statuses, r.displayStatus())
		results = append(results, r)
	}

	if repoNote != "" {
		fmt.Fprintln(stdout, repoNote)
	}
	if hasRepo {
		fmt.Fprintln(stdout, repoStatusOnlyNote)
	}
	if hasMensch {
		fmt.Fprintln(stdout, menschNote)
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

// runSetupCheck exits 1 on drift of an entry setup applies itself, and when a
// given --repo inventory cannot be checked at all. A mensch entry shows its
// status but is no drift: only the human applies it, may fill placeholders
// such as ${PROJEKTE} by hand and merges fragments, so a difference is not a
// fault setup can judge.
func runSetupCheck(items []processedItem, repoNote string, repoUnchecked bool, stdout io.Writer) int {
	var entries []setupEntry
	var statuses []string
	var results []inspectResult
	hasDrift := false
	hasRepo, hasMensch := false, false
	for _, item := range items {
		r := inspectItem(item, false)
		hasRepo = hasRepo || item.fromRepo
		hasMensch = hasMensch || r.mensch
		entries = append(entries, item.entry)
		statuses = append(statuses, r.displayStatus())
		results = append(results, r)
		if isDriftStatus(r.status) && !r.mensch {
			hasDrift = true
		}
	}

	if repoNote != "" {
		fmt.Fprintln(stdout, repoNote)
	}
	if hasRepo {
		fmt.Fprintln(stdout, repoStatusOnlyNote)
	}
	if hasMensch {
		fmt.Fprintln(stdout, menschNote)
	}
	printStatusTable(stdout, entries, statuses)
	for i, r := range results {
		if r.reason != "" {
			fmt.Fprintf(stdout, "\n[%s] %s: %s\n", entries[i].ID, r.status, r.reason)
		}
	}
	if repoUnchecked {
		fmt.Fprintln(stdout, "\n[--repo] nicht-prüfbar: das Inventar des Repos ist nicht lesbar (fehlt, Symlink oder keine Rechte)")
		hasDrift = true
	}

	if hasDrift {
		return exitViolation
	}
	return exitOK
}

// writeRechteCache renders a template into $XDG_CACHE_HOME/imprint and returns
// the file's path. The id-derived file name runs through the same root,
// symlink and hard link checks as every other target.
func writeRechteCache(e setupEntry, zLexical string, rendered []byte) (string, error) {
	env := currentSetupEnv()
	if env.cache == "" {
		return "", errors.New("XDG_CACHE_HOME/HOME nicht gesetzt")
	}
	cacheRoot, err := resolvePath(env.cache)
	if err != nil {
		return "", fmt.Errorf("$XDG_CACHE_HOME nicht auflösbar: %v", err)
	}
	cacheDir := filepath.Join(cacheRoot, "imprint")
	cachePath := filepath.Join(cacheDir, e.ID+"-"+filepath.Base(zLexical))
	if filepath.Dir(cachePath) != cacheDir {
		return "", fmt.Errorf("cache-Datei für %s verlässt den cache dir", e.ID)
	}
	if _, err := installFile(cachePath, rendered, 0o600, 0o700, false, nil); err != nil {
		return "", fmt.Errorf("cache-Datei konnte nicht geschrieben werden: %v", err)
	}
	return cachePath, nil
}

// rechteAnwendenSkript is the human script in the plugin root that applies a
// rights target with an "anwenden" field (checks, diff, confirmation, deep
// merge for fragment-merge). --apply prints only its call for such an entry.
const rechteAnwendenSkript = "setup/vorlagen/rechte-anwenden.sh"

// checkExecutableQuelle checks that quelle is a regular, executable file
// inside the inventory root, opened like every quelle (openQuelle), and
// returns its resolved path.
func checkExecutableQuelle(invRoot, quelle string) (string, error) {
	rootRes, qRes, err := resolveQuelle(invRoot, quelle)
	if err != nil {
		return "", err
	}
	f, fi, err := openQuelle(rootRes, qRes)
	if err != nil {
		return "", err
	}
	f.Close()
	if fi.Mode().Perm()&0o100 == 0 {
		return "", fmt.Errorf("%s ist nicht ausführbar", quelle)
	}
	return qRes, nil
}

// rechteSkriptCall is the printed call of rechteAnwendenSkript for e. The
// argument is the id without the suffix "-rechte-vorlage" (claude-rechte-vorlage
// → claude). The script must be an executable regular file inside the root.
func rechteSkriptCall(invRoot string, e setupEntry) (string, error) {
	abs, err := checkExecutableQuelle(invRoot, rechteAnwendenSkript)
	if err != nil {
		return "", fmt.Errorf("anwenden=%s braucht %s im Plugin-Root: %v", e.Anwenden, rechteAnwendenSkript, err)
	}
	arg := strings.TrimSuffix(e.ID, "-rechte-vorlage")
	return shellQuote(abs) + " " + shellQuote(arg), nil
}

// printGatedCommands renders content into the cache and prints the commands a
// human runs: a backup of an existing target, then the new content, both with
// install and an explicit mode. install removes a destination before it
// writes, so neither command writes through a link planted at the target or
// its .bak; both are also checked here (no symlink, no second hard link). The
// mode is the existing target's or writeMode's, plus execute bits where read
// bits are set if the quelle is executable, so a copied script stays
// executable and a 0600 file is not widened. Since rechte=true needs
// "anwenden", only a systemd unit whose name exists elsewhere comes here.
func printGatedCommands(stdout io.Writer, e setupEntry, zc zielCheck, content []byte, quelleMode fs.FileMode) error {
	mode := writeMode(e.Typ)
	exists := false
	d, err := openTargetDir(filepath.Dir(zc.resolved), false, 0)
	switch {
	case err == nil:
		defer d.Close()
		name := filepath.Base(zc.resolved)
		f, fi, err := openRegularAt(d, name)
		switch {
		case err == nil:
			f.Close()
			exists, mode = true, fi.Mode().Perm()
			if err := checkReplaceable(d, name+".bak"); err != nil {
				return fmt.Errorf(".bak nicht ersetzbar: %v", err)
			}
		case !errors.Is(err, fs.ErrNotExist):
			return err
		}
	case !errors.Is(err, fs.ErrNotExist):
		return err
	}
	cachePath, err := writeRechteCache(e, zc.lexical, content)
	if err != nil {
		return err
	}
	if exists {
		fmt.Fprintf(stdout, "install -m %04o %s %s\n", mode, shellQuote(zc.resolved), shellQuote(zc.resolved+".bak"))
	}
	if quelleMode&0o111 != 0 {
		mode |= (mode & 0o444) >> 2
	}
	fmt.Fprintf(stdout, "install -m %04o %s %s\n", mode, shellQuote(cachePath), shellQuote(zc.resolved))
	return nil
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
			gated := entryIsRechteGated(e)

			// A rights target with an "anwenden" field: only the human script
			// applies it; no cache file, no cp, no jq pipe.
			if gated && e.Anwenden != "" {
				call, err := rechteSkriptCall(item.invRoot, e)
				if err != nil {
					fail(e, err.Error())
					continue
				}
				fmt.Fprintln(stdout, call)
				add(e, "gedruckt")
				continue
			}

			var data []byte
			var quelleMode fs.FileMode
			perm := writeMode(e.Typ)
			if e.Typ == "shim" && !gated {
				if other := shimPathConflict(filepath.Base(zc.lexical)); other != "" {
					fail(e, fmt.Sprintf("verweigert: Shim-Name ist schon ein anderes Kommando in PATH (%s)", other))
					continue
				}
				content, err := shimContent(e.Quelle)
				if err != nil {
					fail(e, err.Error())
					continue
				}
				// The shim runs $IMPRINT_CORE_ROOT/<quelle>; it must exist in
				// the root and be executable before a shim points at it.
				if _, err := checkExecutableQuelle(item.invRoot, e.Quelle); err != nil {
					fail(e, fmt.Sprintf("Shim-quelle nicht nutzbar: %v", err))
					continue
				}
				data = []byte(content)
			} else {
				var qfi fs.FileInfo
				var err error
				data, qfi, err = readQuelle(item.invRoot, e.Quelle)
				if err != nil {
					fail(e, err.Error())
					continue
				}
				quelleMode = qfi.Mode()
			}

			if e.Typ == "systemd" && !gated {
				if other := systemdUnitElsewhere(filepath.Base(zc.lexical)); other != "" {
					fmt.Fprintf(stdout, "# %s: Unit-Name existiert auch in %s; nur gedruckt, wie bei rechte=true\n", e.ID, other)
					gated = true
				}
			}

			if gated {
				content := data
				if entryIsRechteGated(e) {
					content = []byte(renderTemplate(string(data)))
				}
				if err := printGatedCommands(stdout, e, zc, content, quelleMode); err != nil {
					fail(e, err.Error())
					continue
				}
				add(e, "gedruckt")
				continue
			}

			var before func(old []byte, exists bool) error
			switch e.Typ {
			case "shim":
				before = func(old []byte, exists bool) error {
					if exists && !isSetupShim(old) {
						return errors.New("verweigert: ziel ist eine fremde Datei (keine imprint-Shim-Zeile); setup ersetzt sie nie")
					}
					return nil
				}
			case "systemd":
				before = func([]byte, bool) error {
					fmt.Fprint(stdout, systemdDisplay(zc.resolved, data))
					return nil
				}
			}
			status, err := installFile(zc.resolved, data, perm, 0o755, true, before)
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
