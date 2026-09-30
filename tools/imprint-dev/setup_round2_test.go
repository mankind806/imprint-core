package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Guard tests for blind review round 2 of the setup security boundary
// (imprint-core-CL-011). Each one replays a proof of concept of that round
// (poc2.sh N1-N8, race.sh) in temp dirs only; none touches the real HOME.

// noSystemdTool keeps systemd-analyze out of the unit path list, so only the
// documented defaults below the test's HOME and XDG dirs count.
func noSystemdTool(t *testing.T) {
	t.Helper()
	orig := systemdUnitPathsFromTool
	systemdUnitPathsFromTool = func() []string { return nil }
	t.Cleanup(func() { systemdUnitPathsFromTool = orig })
}

func gitHooksPath(t *testing.T, repo string) string {
	t.Helper()
	out, _ := exec.Command("git", "-C", repo, "config", "--local", "--get", "core.hooksPath").Output()
	return strings.TrimSpace(string(out))
}

// N1: a bare "setup --apply" inside a foreign checkout used the checkout as
// --root (default ".") and installed its shims (git, sudo), a unit and its
// hooksPath. --apply now needs an explicit --root that is the imprint plugin.
func TestSetupApplyNeedsPluginRoot(t *testing.T) {
	home := setupTestHome(t)
	clearApplyGuardEnv(t)
	foreignInv := `[
		{"id": "git", "typ": "shim", "quelle": "tools/evil", "ziel": "${HOME}/.local/bin/git", "rechte": false, "beschreibung": "b"},
		{"id": "imprint-x", "typ": "shim", "quelle": "tools/evil", "ziel": "${HOME}/.local/bin/imprint-x", "rechte": false, "beschreibung": "b"},
		{"id": "unit", "typ": "systemd", "quelle": "tools/u.service", "ziel": "${XDG_CONFIG_HOME}/systemd/user/imprint-unit.service", "rechte": false, "beschreibung": "b"},
		{"id": "hook", "typ": "githook", "quelle": ".githooks/pre-commit", "ziel": "${HOME}/x", "rechte": false, "beschreibung": "b"}
	]`
	foreign := t.TempDir()
	writeFiles(t, foreign, map[string]string{
		"setup/inventar.json":  foreignInv,
		".imprint/setup.json":  `[]`,
		"tools/u.service":      "[Service]\nExecStart=/bin/sh -c 'echo PWNED'\n",
		".githooks/pre-commit": "#!/bin/sh\necho PWNED\n",
	})
	if err := exec.Command("git", "init", "-q", foreign).Run(); err != nil {
		t.Fatal(err)
	}
	realPlugin := newSetupRoot(t, `[]`, nil)
	before := hashTree(t, home)

	t.Chdir(foreign)
	cases := []struct {
		name  string
		setup func(t *testing.T)
		args  []string
	}{
		{"no --root", nil, []string{"setup", "--apply", "--repo", "."}},
		{"no --root, no --repo", nil, []string{"setup", "--apply"}},
		{"--root . without manifest", nil, []string{"setup", "--apply", "--root", "."}},
		{"manifest with another name", func(t *testing.T) {
			writeFiles(t, foreign, map[string]string{".claude-plugin/plugin.json": `{"name": "not-imprint"}`})
		}, []string{"setup", "--apply", "--root", "."}},
		{"manifest is a symlink to the real one", func(t *testing.T) {
			if err := os.Remove(filepath.Join(foreign, ".claude-plugin", "plugin.json")); err != nil {
				t.Fatal(err)
			}
			mustSymlink(t, filepath.Join(realPlugin, ".claude-plugin", "plugin.json"), filepath.Join(foreign, ".claude-plugin", "plugin.json"))
		}, []string{"setup", "--apply", "--root", "."}},
		{"--root equals --repo", nil, []string{"setup", "--apply", "--root", realPlugin, "--repo", realPlugin}},
	}
	for _, tc := range cases {
		if tc.setup != nil {
			tc.setup(t)
		}
		code, stdout, stderr := runCLI(t, tc.args...)
		if code != exitError {
			t.Fatalf("%s: expected exit %d, got %d\n%s\n%s", tc.name, exitError, code, stdout, stderr)
		}
		if hashTree(t, home) != before {
			t.Fatalf("%s: HOME changed", tc.name)
		}
		if got := gitHooksPath(t, foreign); got != "" {
			t.Fatalf("%s: foreign core.hooksPath set to %q", tc.name, got)
		}
	}
	if code, stdout, stderr := runCLI(t, "setup", "--apply", "--root", realPlugin, "--repo", foreign); code != exitOK {
		t.Fatalf("the real plugin root must still apply, got %d\n%s\n%s", code, stdout, stderr)
	}
}

// Finding 2: a systemd unit needs the name imprint-<id>.service|.timer; a
// name that also exists in another user unit directory is only printed, and
// every unit copy is shown first.
func TestSetupSystemdUnitNames(t *testing.T) {
	home := setupTestHome(t)
	clearApplyGuardEnv(t)
	noSystemdTool(t)
	etcXDG := t.TempDir()
	t.Setenv("XDG_CONFIG_DIRS", etcXDG)
	unit := "[Unit]\nDescription=imprint\n[Service]\nExecStart=/bin/true\x1b[2K\n"
	writeFiles(t, home, map[string]string{".local/share/systemd/user/imprint-data.service": "vendor\n"})
	writeFiles(t, etcXDG, map[string]string{"systemd/user/imprint-etc.timer": "vendor\n"})
	inv := `[
		{"id": "data", "typ": "systemd", "quelle": "u", "ziel": "${XDG_CONFIG_HOME}/systemd/user/imprint-data.service", "rechte": false, "beschreibung": "b"},
		{"id": "etc", "typ": "systemd", "quelle": "u", "ziel": "${XDG_CONFIG_HOME}/systemd/user/imprint-etc.timer", "rechte": false, "beschreibung": "b"},
		{"id": "own", "typ": "systemd", "quelle": "u", "ziel": "${XDG_CONFIG_HOME}/systemd/user/imprint-own.service", "rechte": false, "beschreibung": "b"}
	]`
	root := newSetupRoot(t, inv, map[string]string{"u": unit})

	_, planOut, _ := runCLI(t, "setup", "--plan", "--root", root)
	assertNoDiff(t, "--plan", planOut)
	for _, id := range []string{"data", "etc"} {
		if !strings.Contains(planOut, "["+id+"] fehlt: Unit-Name existiert auch in") {
			t.Fatalf("--plan must report the name collision of %s:\n%s", id, planOut)
		}
	}

	code, stdout, stderr := runCLI(t, "setup", "--apply", "--root", root)
	if code != exitOK {
		t.Fatalf("apply: %d\n%s\n%s", code, stdout, stderr)
	}
	unitDir := filepath.Join(home, ".config", "systemd", "user")
	for _, name := range []string{"imprint-data.service", "imprint-etc.timer"} {
		if _, err := os.Lstat(filepath.Join(unitDir, name)); err == nil {
			t.Fatalf("%s collides with another unit directory and must only be printed", name)
		}
		if !strings.Contains(stdout, " "+filepath.Join(unitDir, name)+"\n") || !strings.Contains(stdout, "install -m 0600 ") {
			t.Fatalf("no printed install command for %s:\n%s", name, stdout)
		}
	}
	if got := mustRead(t, filepath.Join(unitDir, "imprint-own.service")); got != unit {
		t.Fatalf("own unit content %q", got)
	}
	shown := "--- systemd-Unit " + filepath.Join(unitDir, "imprint-own.service")
	if !strings.Contains(stdout, shown) || !strings.Contains(stdout, `ExecStart=/bin/true\u001b[2K`) || strings.Contains(stdout, "\x1b") {
		t.Fatalf("the unit must be shown, with control characters escaped, before it is written:\n%q", stdout)
	}
	if code, out, _ := runCLI(t, "setup", "--check", "--root", root); code != exitOK || !strings.Contains(out, "(Mensch)") {
		t.Fatalf("--check: printed-only units are no drift, got %d\n%s", code, out)
	}
}

// N3 and finding 3: a shim needs the imprint- prefix, never shadows another
// command in PATH and never replaces a file without the shim marker.
func TestSetupShimNeverReplacesForeign(t *testing.T) {
	home := setupTestHome(t)
	clearApplyGuardEnv(t)
	const canary = "REAL-BINARY-CANARY"
	bin := filepath.Join(home, ".local", "bin")
	writeFiles(t, home, map[string]string{".local/bin/uv": canary + "\n", ".local/bin/imprint-foreign": canary + "\n"})
	other := t.TempDir()
	writeFiles(t, other, map[string]string{"imprint-shadow": "#!/bin/sh\n"})
	if err := os.Chmod(filepath.Join(other, "imprint-shadow"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", os.Getenv("PATH")+string(os.PathListSeparator)+other+string(os.PathListSeparator)+bin)

	inv := `[
		{"id": "uv", "typ": "shim", "quelle": "bin/x", "ziel": "${HOME}/.local/bin/uv", "rechte": false, "beschreibung": "b"},
		{"id": "imprint-foreign", "typ": "shim", "quelle": "bin/x", "ziel": "${HOME}/.local/bin/imprint-foreign", "rechte": false, "beschreibung": "b"},
		{"id": "imprint-shadow", "typ": "shim", "quelle": "bin/x", "ziel": "${HOME}/.local/bin/imprint-shadow", "rechte": false, "beschreibung": "b"}
	]`
	root := newSetupRoot(t, inv, nil)
	before := hashTree(t, home)
	for _, mode := range []string{"--plan", "--check"} {
		_, out, _ := runCLI(t, "setup", mode, "--root", root)
		assertNoCanary(t, mode, out, canary)
		assertNoDiff(t, mode, out)
		for _, id := range []string{"uv", "imprint-foreign", "imprint-shadow"} {
			if !strings.Contains(out, "["+id+"] verweigert") {
				t.Fatalf("%s: %s not verweigert:\n%s", mode, id, out)
			}
		}
	}
	code, stdout, stderr := runCLI(t, "setup", "--apply", "--root", root)
	if code != exitViolation {
		t.Fatalf("apply must refuse every shim, got %d\n%s\n%s", code, stdout, stderr)
	}
	if hashTree(t, home) != before {
		t.Fatalf("a refused shim changed HOME")
	}

	// A shim the plugin wrote itself (marker line) is replaced, with a .bak.
	own := "#!/bin/sh\n" + shimMarker + "\nexec old\n"
	writeFiles(t, home, map[string]string{".local/bin/imprint-own": own})
	ownRoot := newSetupRoot(t, `[{"id": "imprint-own", "typ": "shim", "quelle": "bin/new", "ziel": "${HOME}/.local/bin/imprint-own", "rechte": false, "beschreibung": "b"}]`, map[string]string{"bin/new": "#!/bin/sh\n"})
	if err := os.Chmod(filepath.Join(ownRoot, "bin", "new"), 0o755); err != nil {
		t.Fatal(err)
	}
	if code, stdout, stderr := runCLI(t, "setup", "--apply", "--root", ownRoot); code != exitOK {
		t.Fatalf("own shim must be replaced, got %d\n%s\n%s", code, stdout, stderr)
	}
	if got := mustRead(t, filepath.Join(bin, "imprint-own")); !strings.Contains(got, "'bin/new'") {
		t.Fatalf("own shim not replaced: %q", got)
	}
	if got := mustRead(t, filepath.Join(bin, "imprint-own.bak")); got != own {
		t.Fatalf("own shim .bak %q", got)
	}
}

// N4: a target or .bak hard-linked to a key was written through (.bak) or
// shown in the --plan diff (target). Neither is read, shown or replaced now.
func TestSetupHardlinks(t *testing.T) {
	const canary = "PRIVATE-KEY-CANARY"
	inv := `[{"id": "x", "typ": "copy", "quelle": "q", "ziel": "${XDG_CONFIG_HOME}/imprint/x", "rechte": false, "beschreibung": "b"}]`
	mustLink := func(t *testing.T, oldname, newname string) {
		t.Helper()
		if err := os.Link(oldname, newname); err != nil {
			t.Skipf("hard links not supported here: %v", err)
		}
	}

	t.Run(".bak hard-linked to authorized_keys", func(t *testing.T) {
		home := setupTestHome(t)
		clearApplyGuardEnv(t)
		writeFiles(t, home, map[string]string{".ssh/authorized_keys": canary + "\n", ".config/imprint/x": "old\n"})
		mustLink(t, filepath.Join(home, ".ssh", "authorized_keys"), filepath.Join(home, ".config", "imprint", "x.bak"))
		root := newSetupRoot(t, inv, map[string]string{"q": "ssh-ed25519 ATTACKER\n"})
		before := hashTree(t, home)
		if code, stdout, stderr := runCLI(t, "setup", "--apply", "--root", root); code != exitViolation {
			t.Fatalf("expected exit %d, got %d\n%s\n%s", exitViolation, code, stdout, stderr)
		}
		if hashTree(t, home) != before {
			t.Fatalf("HOME changed through a hard-linked .bak")
		}
	})

	t.Run("target hard-linked to a private key", func(t *testing.T) {
		home := setupTestHome(t)
		clearApplyGuardEnv(t)
		writeFiles(t, home, map[string]string{".ssh/id_ed25519": canary + "\n"})
		if err := os.MkdirAll(filepath.Join(home, ".config", "imprint"), 0o755); err != nil {
			t.Fatal(err)
		}
		mustLink(t, filepath.Join(home, ".ssh", "id_ed25519"), filepath.Join(home, ".config", "imprint", "x"))
		root := newSetupRoot(t, inv, map[string]string{"q": "plugin content\n"})
		repo := newSetupRepo(t, inv, map[string]string{"q": "guess\n"})
		for _, args := range [][]string{
			{"setup", "--plan", "--root", root},
			{"setup", "--check", "--root", root},
			{"setup", "--plan", "--root", newSetupRoot(t, `[]`, nil), "--repo", repo},
		} {
			_, out, errOut := runCLI(t, args...)
			assertNoCanary(t, strings.Join(args, " "), out+errOut, canary)
			assertNoDiff(t, strings.Join(args, " "), out)
		}
		before := hashTree(t, home)
		if code, stdout, stderr := runCLI(t, "setup", "--apply", "--root", root); code != exitViolation {
			t.Fatalf("expected exit %d, got %d\n%s\n%s", exitViolation, code, stdout, stderr)
		}
		if hashTree(t, home) != before {
			t.Fatalf("HOME changed through a hard-linked target")
		}
	})
}

// race.sh swapped ~/.config/imprint for a symlink to ~/.ssh while --apply ran.
// The race itself is not hermetic; this replays its outcome deterministically:
// the swap has already happened between checkZiel and the write.
func TestSetupWriteDoesNotFollowSwappedDir(t *testing.T) {
	home := setupTestHome(t)
	const canary = "SWAPPED-DIR-CANARY"
	writeFiles(t, home, map[string]string{".ssh/sub/k": canary, ".ssh/authorized_keys": canary})
	mustSymlink(t, filepath.Join(home, ".ssh"), filepath.Join(home, ".config", "imprint"))
	mustSymlink(t, filepath.Join(home, ".ssh"), filepath.Join(home, ".local", "share", "imprint"))
	before := hashTree(t, home)

	for _, zr := range []string{
		filepath.Join(home, ".config", "imprint", "authorized_keys"), // last dir component swapped
		filepath.Join(home, ".config", "imprint", "new"),
		filepath.Join(home, ".local", "share", "imprint", "sub", "k"), // intermediate component swapped
	} {
		if _, err := installFile(zr, []byte("RACE-PAYLOAD\n"), 0o644, 0o755, true, nil); err == nil {
			t.Fatalf("installFile(%s) followed a swapped directory", zr)
		}
		if data, err := readTarget(zr); err == nil || strings.Contains(string(data), canary) {
			t.Fatalf("readTarget(%s) followed a swapped directory: %q, %v", zr, data, err)
		}
	}
	if hashTree(t, home) != before {
		t.Fatalf("a write went through a swapped directory")
	}
}

// N8 and finding 5: --check with a --repo inventory that cannot be read
// (missing, mistyped, symlink, unreadable) is nicht-prüfbar and exits 1.
func TestSetupCheckRepoInventoryUnreadable(t *testing.T) {
	setupTestHome(t)
	root := newSetupRoot(t, `[]`, nil)
	other := newSetupRepo(t, `[]`, nil)
	symRepo := t.TempDir()
	mustSymlink(t, filepath.Join(other, ".imprint", "setup.json"), filepath.Join(symRepo, ".imprint", "setup.json"))
	repos := map[string]string{
		"missing":  filepath.Join(t.TempDir(), "does-not-exist"),
		"no file":  t.TempDir(),
		"symlink":  symRepo,
		"readable": other,
	}
	if os.Geteuid() != 0 {
		unreadable := newSetupRepo(t, `[]`, nil)
		p := filepath.Join(unreadable, ".imprint", "setup.json")
		if err := os.Chmod(p, 0); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { os.Chmod(p, 0o644) })
		repos["unreadable"] = unreadable
	}
	for name, repo := range repos {
		code, out, _ := runCLI(t, "setup", "--check", "--root", root, "--repo", repo)
		want := exitViolation
		if name == "readable" {
			want = exitOK
		}
		if code != want {
			t.Fatalf("%s: --check exit %d, want %d\n%s", name, code, want, out)
		}
		if want == exitViolation && !strings.Contains(out, "[--repo] nicht-prüfbar") {
			t.Fatalf("%s: status nicht-prüfbar missing:\n%s", name, out)
		}
		if code, _, _ := runCLI(t, "setup", "--plan", "--root", root, "--repo", repo); code != exitOK {
			t.Fatalf("%s: --plan must stay exit 0, got %d", name, code)
		}
	}
}

// N6: XDG base directories bent onto rights paths or above $HOME never make
// setup write there.
func TestSetupXDGRedirects(t *testing.T) {
	home := setupTestHome(t)
	clearApplyGuardEnv(t)
	outside := t.TempDir()
	writeFiles(t, home, map[string]string{".claude/keep": "k", ".ssh/keep": "k"})
	mustSymlink(t, filepath.Join(home, ".claude"), filepath.Join(outside, "cfglink"))
	mustSymlink(t, filepath.Join(home, ".ssh"), filepath.Join(home, "sshlink"))
	inv := `[
		{"id": "c", "typ": "copy", "quelle": "q", "ziel": "${XDG_CONFIG_HOME}/imprint/c", "rechte": false, "beschreibung": "b"},
		{"id": "d", "typ": "copy", "quelle": "q", "ziel": "${XDG_DATA_HOME}/imprint/d", "rechte": false, "beschreibung": "b"},
		{"id": "u", "typ": "systemd", "quelle": "q", "ziel": "${XDG_CONFIG_HOME}/systemd/user/imprint-u.service", "rechte": false, "beschreibung": "b"}
	]`
	root := newSetupRoot(t, inv, map[string]string{"q": "q\n"})
	denied := []string{filepath.Join(home, ".claude"), filepath.Join(home, ".ssh"), filepath.Join(home, ".config", "systemd", "user", "default.target.wants")}
	count := func() int {
		n := 0
		for _, d := range denied {
			filepath.WalkDir(d, func(_ string, de os.DirEntry, err error) error {
				if err == nil && !de.IsDir() {
					n++
				}
				return nil
			})
		}
		return n
	}
	baseline := count()
	for _, kv := range [][2]string{
		{"XDG_CONFIG_HOME", filepath.Join(home, ".claude")},
		{"XDG_CONFIG_HOME", filepath.Join(home, ".ssh")},
		{"XDG_CONFIG_HOME", filepath.Join(outside, "cfglink")},
		{"XDG_CONFIG_HOME", filepath.Join(home, "sshlink")},
		{"XDG_DATA_HOME", filepath.Join(home, ".claude", "plugins", "marketplaces")},
		{"XDG_CONFIG_HOME", filepath.Join(home, ".config", "systemd", "user", "default.target.wants")},
		{"XDG_CONFIG_HOME", "/"},
		{"XDG_DATA_HOME", filepath.Dir(home)},
	} {
		t.Run(kv[0]+"="+kv[1], func(t *testing.T) {
			t.Setenv(kv[0], kv[1])
			runCLI(t, "setup", "--apply", "--root", root)
			if n := count(); n != baseline {
				t.Fatalf("setup wrote %d file(s) into a rights path", n-baseline)
			}
		})
	}

	// An XDG value above $HOME counts as unset: a literal rights path outside
	// HOME is refused and no command for it is printed.
	t.Setenv("XDG_CONFIG_HOME", "/")
	lit := newSetupRoot(t, `[{"id": "r", "typ": "rechte-vorlage", "quelle": "q", "ziel": "/etc/passwd", "rechte": true, "anwenden": "ersetzen", "beschreibung": "b"}]`, map[string]string{"q": "q\n", "setup/vorlagen/rechte-anwenden.sh": "#!/bin/sh\n"})
	code, stdout, _ := runCLI(t, "setup", "--apply", "--root", lit)
	if code != exitViolation || strings.Contains(stdout, "/etc/passwd") {
		t.Fatalf("XDG_CONFIG_HOME=/ must not allow /etc/passwd, got %d\n%s", code, stdout)
	}
}

// N7: spellings of a ziel that normalise onto a rights path are refused.
func TestSetupZielNormalisation(t *testing.T) {
	home := setupTestHome(t)
	clearApplyGuardEnv(t)
	writeFiles(t, home, map[string]string{".claude/keep": "k", ".ssh/keep": "k"})
	before := hashTree(t, home)
	for _, z := range []string{
		"${HOME}//.claude/x", "${HOME}/./.ssh/x", "${XDG_CONFIG_HOME}/imprint/../../.ssh/x",
		"${XDG_CONFIG_HOME}/imprint/..", "${XDG_CONFIG_HOME}/imprint", "${HOME}/.ssh/", "$HOMEx/.ssh/x",
	} {
		inv := `[{"id": "n", "typ": "copy", "quelle": "q", "ziel": "` + z + `", "rechte": false, "beschreibung": "b"}]`
		root := newSetupRoot(t, inv, map[string]string{"q": "q\n"})
		if code, stdout, stderr := runCLI(t, "setup", "--apply", "--root", root); code == exitOK {
			t.Fatalf("ziel %s: --apply must refuse\n%s\n%s", z, stdout, stderr)
		}
		if hashTree(t, home) != before {
			t.Fatalf("ziel %s changed HOME", z)
		}
	}
}

// Finding 6 and the #36 add-ons: the "anwenden" field. The fixture mirrors the
// rights entries of #36. A rights target with the field is applied only by
// the human script (--apply prints its call, no cp, no jq); --plan/--check
// show the status only, compared with jq's deep merge "*"; --check does not
// count rights targets as drift; a copied script keeps its execute bit.
func TestSetupAnwenden(t *testing.T) {
	home := setupTestHome(t)
	clearApplyGuardEnv(t)
	const canary = "CANARY-other-key-in-settings"
	fixture := `[
		{"id": "agy-rechte-vorlage", "typ": "rechte-vorlage", "quelle": "setup/vorlagen/agy-settings.json", "ziel": "${HOME}/.gemini/antigravity-cli/settings.json", "rechte": true, "anwenden": "ersetzen", "beschreibung": "b"},
		{"id": "claude-rechte-vorlage", "typ": "rechte-vorlage", "quelle": "setup/vorlagen/claude-rechte.json", "ziel": "${HOME}/.claude/settings.json", "rechte": true, "anwenden": "fragment-merge", "beschreibung": "b"},
		{"id": "agy-statusline", "typ": "copy", "quelle": "setup/vorlagen/statusline.py", "ziel": "${HOME}/.gemini/antigravity-cli/statusline.py", "rechte": true, "anwenden": "ersetzen", "beschreibung": "b"}
	]`
	files := map[string]string{
		"setup/vorlagen/agy-settings.json":  `{"permissions": {"allow": []}}`,
		"setup/vorlagen/claude-rechte.json": `{"permissions": {"allow": ["Bash(gh pr merge:*)"], "deny": ["mcp__computer-use"]}}`,
		"setup/vorlagen/statusline.py":      "#!/usr/bin/env python3\n",
		"setup/vorlagen/rechte-anwenden.sh": "#!/bin/sh\n",
	}
	root := newSetupRoot(t, fixture, files)
	for _, f := range []string{"setup/vorlagen/statusline.py", "setup/vorlagen/rechte-anwenden.sh"} {
		if err := os.Chmod(filepath.Join(root, f), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	settings := filepath.Join(home, ".claude", "settings.json")
	writeFiles(t, home, map[string]string{
		".claude/settings.json":                      `{"permissions": {"allow": ["old"], "defaultMode": "plan"}, "env": {"T": "` + canary + `"}}`,
		".gemini/antigravity-cli/statusline.py":      "old\n",
		".gemini/antigravity-cli/settings.json.keep": "k",
	})
	if err := os.Chmod(filepath.Join(home, ".gemini", "antigravity-cli", "statusline.py"), 0o600); err != nil {
		t.Fatal(err)
	}

	code, stdout, stderr := runCLI(t, "setup", "--apply", "--root", root)
	if code != exitOK {
		t.Fatalf("apply: %d\n%s\n%s", code, stdout, stderr)
	}
	assertNoCanary(t, "--apply", stdout+stderr, canary)
	script, _ := filepath.EvalSymlinks(filepath.Join(root, "setup", "vorlagen", "rechte-anwenden.sh"))
	for _, want := range []string{shellQuote(script) + " claude\n", shellQuote(script) + " agy\n", shellQuote(script) + " agy-statusline\n"} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("missing script call %q:\n%s", want, stdout)
		}
	}
	for _, line := range strings.Split(stdout, "\n") {
		if strings.Contains(line, "jq") || (strings.Contains(line, "settings.json") && !strings.HasPrefix(line, shellQuote(script))) {
			t.Fatalf("a rights target with 'anwenden' may only get the script call, got %q", line)
		}
	}
	for _, id := range []string{"claude-rechte-vorlage", "agy-rechte-vorlage"} {
		matches, _ := filepath.Glob(filepath.Join(home, ".cache", "imprint", id+"-*"))
		if len(matches) != 0 {
			t.Fatalf("no cache file may be rendered for %s: %v", id, matches)
		}
	}
	if strings.Contains(stdout, "install ") {
		t.Fatalf("a rights target is never installed by a printed command:\n%s", stdout)
	}
	if got := mustRead(t, settings); !strings.Contains(got, canary) || !strings.Contains(got, `"old"`) {
		t.Fatalf("--apply changed the rights target: %s", got)
	}

	statusOf := func(out, id string) string {
		for _, line := range strings.Split(out, "\n") {
			if f := strings.Fields(line); len(f) >= 3 && f[0] == id {
				return strings.Join(f[2:4], " ")
			}
		}
		return ""
	}
	for _, mode := range []string{"--plan", "--check"} {
		code, out, errOut := runCLI(t, "setup", mode, "--root", root)
		if code != exitOK {
			t.Fatalf("%s: rights targets are no drift, got %d\n%s", mode, code, out)
		}
		assertNoCanary(t, mode, out+errOut, canary)
		assertNoDiff(t, mode, out)
		if got := statusOf(out, "claude-rechte-vorlage"); got != "abweichend (Mensch)" {
			t.Fatalf("%s: claude status %q\n%s", mode, got, out)
		}
	}

	// Deep merge: the template's permissions applied, defaultMode and env kept,
	// is "gleich"; a shallow ".permissions = ..." would have dropped defaultMode.
	writeFiles(t, home, map[string]string{".claude/settings.json": `{"permissions": {"allow": ["Bash(gh pr merge:*)"], "deny": ["mcp__computer-use"], "defaultMode": "plan"}, "env": {"T": "` + canary + `"}}`})
	_, out, _ := runCLI(t, "setup", "--check", "--root", root)
	if got := statusOf(out, "claude-rechte-vorlage"); got != "gleich (Mensch)" {
		t.Fatalf("deep-merged target must be gleich, got %q\n%s", got, out)
	}
	if deepMerge(map[string]any{"p": map[string]any{"d": "x", "a": 1.0}}, map[string]any{"p": map[string]any{"a": 2.0}}).(map[string]any)["p"].(map[string]any)["d"] != "x" {
		t.Fatalf("deepMerge dropped a nested key")
	}

	// Without the script in the root nothing is printed for such an entry.
	noScript := newSetupRoot(t, fixture, map[string]string{
		"setup/vorlagen/agy-settings.json":  files["setup/vorlagen/agy-settings.json"],
		"setup/vorlagen/claude-rechte.json": files["setup/vorlagen/claude-rechte.json"],
		"setup/vorlagen/statusline.py":      files["setup/vorlagen/statusline.py"],
	})
	code, stdout, _ = runCLI(t, "setup", "--apply", "--root", noScript)
	if code != exitViolation || strings.Contains(stdout, "settings.json") {
		t.Fatalf("missing script must fail the entry without printing a command, got %d\n%s", code, stdout)
	}

	// Schema: only ersetzen and fragment-merge; fragment-merge only for a
	// rechte=true .json file target.
	for _, bad := range []string{
		`{"id": "a", "typ": "rechte-vorlage", "quelle": "q", "ziel": "${HOME}/.claude/settings.json", "rechte": true, "anwenden": "merge", "beschreibung": "b"}`,
		`{"id": "a", "typ": "copy", "quelle": "q", "ziel": "${XDG_CONFIG_HOME}/imprint/a.json", "rechte": false, "anwenden": "fragment-merge", "beschreibung": "b"}`,
		`{"id": "a", "typ": "rechte-vorlage", "quelle": "q", "ziel": "${HOME}/.claude/settings.txt", "rechte": true, "anwenden": "fragment-merge", "beschreibung": "b"}`,
		`{"id": "imprint-a", "typ": "shim", "quelle": "q", "ziel": "${HOME}/.local/bin/imprint-a.json", "rechte": true, "anwenden": "fragment-merge", "beschreibung": "b"}`,
		`{"id": "a", "typ": "mcp", "quelle": "q", "ziel": "${HOME}/a.json", "rechte": true, "anwenden": "fragment-merge", "beschreibung": "b"}`,
		`{"id": "a", "typ": "copy", "quelle": "q", "ziel": "${HOME}/.gemini/a.py", "rechte": true, "beschreibung": "b"}`,
	} {
		if _, err := parseInventory([]byte("[" + bad + "]")); err == nil {
			t.Errorf("inventory must be invalid: %s", bad)
		}
	}
}

// The printed backup of a print-only systemd unit (its name exists in another
// unit directory) must not write through a link planted at its .bak (N4a, by
// the human's hand): such an entry fails and prints nothing. Since round 3 a
// rights target always has "anwenden" and gets only the script call, so the
// unit collision is the one entry left that prints install.
func TestSetupGatedBackupNotFollowed(t *testing.T) {
	const canary = "GATED-BAK-CANARY"
	inv := `[{"id": "u", "typ": "systemd", "quelle": "u.service", "ziel": "${XDG_CONFIG_HOME}/systemd/user/imprint-u.service", "rechte": false, "beschreibung": "b"}]`
	for _, kind := range []string{"symlink", "hardlink"} {
		t.Run(kind, func(t *testing.T) {
			home := setupTestHome(t)
			clearApplyGuardEnv(t)
			noSystemdTool(t)
			writeFiles(t, home, map[string]string{
				".ssh/authorized_keys":                        canary,
				".config/systemd/user/imprint-u.service":      "old\n",
				".local/share/systemd/user/imprint-u.service": "vendor\n",
			})
			bak := filepath.Join(home, ".config", "systemd", "user", "imprint-u.service.bak")
			key := filepath.Join(home, ".ssh", "authorized_keys")
			if kind == "symlink" {
				mustSymlink(t, key, bak)
			} else if err := os.Link(key, bak); err != nil {
				t.Skipf("hard links not supported here: %v", err)
			}
			root := newSetupRoot(t, inv, map[string]string{"u.service": "[Service]\n"})
			code, stdout, stderr := runCLI(t, "setup", "--apply", "--root", root)
			if code != exitViolation || strings.Contains(stdout, "install ") {
				t.Fatalf("expected exit %d and no printed command, got %d\n%s\n%s", exitViolation, code, stdout, stderr)
			}
			if got := mustRead(t, key); got != canary {
				t.Fatalf("key changed: %q", got)
			}
		})
	}
}
