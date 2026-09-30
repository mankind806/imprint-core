package main

import (
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// Guard tests for blind review round 3 of the setup security boundary
// (imprint-core-CL-011). Each one replays a proof of concept of that round
// (poc3a.sh, poc3b.sh, race3.sh) in temp dirs only; none touches the real
// HOME, and none runs --apply against a root other than a temp plugin root.

// testPluginAnchors are the plugin roots the tests accept as --root for
// --apply, in place of the binary's own source tree (pluginAnchors).
var testPluginAnchors struct {
	sync.Mutex
	roots []string
}

func init() {
	pluginAnchors = func() []string {
		testPluginAnchors.Lock()
		defer testPluginAnchors.Unlock()
		return append([]string(nil), testPluginAnchors.roots...)
	}
}

// anchorPluginRoot makes root an accepted plugin root for --apply until the
// test ends.
func anchorPluginRoot(t *testing.T, root string) {
	t.Helper()
	testPluginAnchors.Lock()
	testPluginAnchors.roots = append(testPluginAnchors.roots, root)
	testPluginAnchors.Unlock()
	t.Cleanup(func() {
		testPluginAnchors.Lock()
		defer testPluginAnchors.Unlock()
		for i, r := range testPluginAnchors.roots {
			if r == root {
				testPluginAnchors.roots = append(testPluginAnchors.roots[:i], testPluginAnchors.roots[i+1:]...)
				break
			}
		}
	})
}

// race3.sh (quelle-plan, quelle-apply, quelle-unit) swapped the quelle's
// directory for a symlink to ~/.ssh while setup ran; readRegularFile resolved
// the directory again and read the key into a --plan diff or copied it to
// ~/.config/imprint. The race is not hermetic; afterQuelleResolved replays
// its winning moment deterministically: right after resolveQuelle.
func TestSetupQuelleDoesNotFollowSwappedDir(t *testing.T) {
	const canary = "CANARY-SECRET-KEY"
	cases := []struct{ name, typ, quelle, swap, ziel string }{
		{"copy, last dir component", "copy", "src/id_ed25519", "src", "${XDG_CONFIG_HOME}/imprint/k"},
		{"copy, intermediate component", "copy", "a/sub/id_ed25519", "a", "${XDG_CONFIG_HOME}/imprint/k"},
		{"systemd unit", "systemd", "src/id_ed25519", "src", "${XDG_CONFIG_HOME}/systemd/user/imprint-k.service"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			home := setupTestHome(t)
			clearApplyGuardEnv(t)
			noSystemdTool(t)
			writeFiles(t, home, map[string]string{
				".ssh/id_ed25519":     canary + "\n",
				".ssh/sub/id_ed25519": canary + "\n",
				".config/imprint/k":   "different\n",
			})
			inv := `[{"id": "k", "typ": "` + c.typ + `", "quelle": "` + c.quelle + `", "ziel": "` + c.ziel + `", "rechte": false, "beschreibung": "b"}]`
			root := newSetupRoot(t, inv, map[string]string{c.quelle: "harmless\n"})
			swapDir := filepath.Join(root, c.swap)
			swapped := false
			orig := afterQuelleResolved
			afterQuelleResolved = func(string, string) {
				if swapped {
					return
				}
				swapped = true
				if err := os.Rename(swapDir, swapDir+".real"); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(filepath.Join(home, ".ssh"), swapDir); err != nil {
					t.Fatal(err)
				}
			}
			t.Cleanup(func() { afterQuelleResolved = orig })
			unswap := func() {
				swapped = false
				os.Remove(swapDir)
				if err := os.Rename(swapDir+".real", swapDir); err != nil {
					t.Fatal(err)
				}
			}

			_, out, errOut := runCLI(t, "setup", "--plan", "--root", root)
			if !swapped {
				t.Fatalf("the swap hook did not run")
			}
			assertNoCanary(t, "--plan", out+errOut, canary)
			unswap()

			code, out, errOut := runCLI(t, "setup", "--apply", "--root", root)
			assertNoCanary(t, "--apply", out+errOut, canary)
			if code != exitViolation || !strings.Contains(errOut, "Symlink") {
				t.Fatalf("--apply must fail the entry on the swapped quelle dir, got %d\n%s\n%s", code, out, errOut)
			}
			unswap()
			filepath.WalkDir(filepath.Join(home, ".config"), func(p string, de os.DirEntry, err error) error {
				if err == nil && !de.IsDir() && strings.Contains(mustRead(t, p), canary) {
					t.Fatalf("the key was copied to %s", p)
				}
				return nil
			})
		})
	}
}

// Files setup writes get 0600, a shim 0755; the quelle's mode never widens
// them.
func TestSetupWrittenFileModes(t *testing.T) {
	home := setupTestHome(t)
	clearApplyGuardEnv(t)
	noSystemdTool(t)
	inv := `[
		{"id": "c", "typ": "copy", "quelle": "q", "ziel": "${XDG_DATA_HOME}/imprint/c", "rechte": false, "beschreibung": "b"},
		{"id": "u", "typ": "systemd", "quelle": "q", "ziel": "${XDG_CONFIG_HOME}/systemd/user/imprint-u.service", "rechte": false, "beschreibung": "b"},
		{"id": "imprint-s", "typ": "shim", "quelle": "q", "ziel": "${HOME}/.local/bin/imprint-s", "rechte": false, "beschreibung": "b"}
	]`
	root := newSetupRoot(t, inv, map[string]string{"q": "#!/bin/sh\n"})
	if err := os.Chmod(filepath.Join(root, "q"), 0o777); err != nil {
		t.Fatal(err)
	}
	if code, out, errOut := runCLI(t, "setup", "--apply", "--root", root); code != exitOK {
		t.Fatalf("apply: %d\n%s\n%s", code, out, errOut)
	}
	for p, want := range map[string]os.FileMode{
		".local/share/imprint/c":                 0o600,
		".config/systemd/user/imprint-u.service": 0o600,
		".local/bin/imprint-s":                   0o755,
	} {
		fi, err := os.Stat(filepath.Join(home, p))
		if err != nil {
			t.Fatal(err)
		}
		if fi.Mode().Perm() != want {
			t.Errorf("%s: mode %04o, want %04o", p, fi.Mode().Perm(), want)
		}
	}
}

// Finding 2 (V4c, V4h): --apply accepts only the plugin root this binary was
// built from; a crafted root with the manifest and a git remote naming
// mankind806/imprint-core is refused, and .claude-plugin/ must not be a
// symlink. Nothing here runs --apply against the real checkout.
func TestSetupApplyAnchor(t *testing.T) {
	home := setupTestHome(t)
	clearApplyGuardEnv(t)
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	checkout := filepath.Dir(filepath.Dir(wd)) // tools/imprint-dev -> root
	src := binarySourceRoot()
	sfi, err1 := os.Stat(src)
	cfi, err2 := os.Stat(checkout)
	if err1 != nil || err2 != nil || !os.SameFile(sfi, cfi) {
		t.Fatalf("binarySourceRoot() = %q, want this checkout %q (%v, %v)", src, checkout, err1, err2)
	}

	orig := pluginAnchors
	t.Cleanup(func() { pluginAnchors = orig })
	pluginAnchors = func() []string { return []string{src} }
	if err := checkPluginRoot(checkout, ""); err != nil {
		t.Fatalf("the checkout the binary was built from must be accepted: %v", err)
	}

	inv := `[
		{"id": "c", "typ": "copy", "quelle": "q", "ziel": "${XDG_CONFIG_HOME}/imprint/config", "rechte": false, "beschreibung": "b"},
		{"id": "hook", "typ": "githook", "quelle": ".githooks/pre-push", "ziel": "${HOME}/x", "rechte": false, "beschreibung": "b"}
	]`
	forged := t.TempDir()
	writeFiles(t, forged, map[string]string{
		".claude-plugin/plugin.json": `{"name": "imprint"}`,
		"setup/inventar.json":        inv,
		"q":                          "FOREIGN-CFG\n",
	})
	for _, args := range [][]string{{"init", "-q", forged}, {"-C", forged, "remote", "add", "origin", "https://github.com/mankind806/imprint-core.git"}} {
		if err := exec.Command("git", args...).Run(); err != nil {
			t.Fatal(err)
		}
	}
	before := hashTree(t, home)
	code, out, errOut := runCLI(t, "setup", "--apply", "--root", forged)
	if code != exitError || !strings.Contains(errOut, "nicht der Plugin-Root") {
		t.Fatalf("forged root: want exit %d, got %d\n%s\n%s", exitError, code, out, errOut)
	}
	pluginAnchors = func() []string { return nil }
	if code, _, errOut := runCLI(t, "setup", "--apply", "--root", forged); code != exitError || !strings.Contains(errOut, "keinen Quellpfad") {
		t.Fatalf("a binary without source path must refuse --apply, got %d\n%s", code, errOut)
	}
	if hashTree(t, home) != before || gitHooksPath(t, forged) != "" {
		t.Fatalf("a refused root changed HOME or its hooksPath")
	}

	// An anchored root whose .claude-plugin/ is a symlink to a real manifest.
	pluginAnchors = orig
	linked := newSetupRoot(t, `[]`, nil)
	real := newSetupRoot(t, `[]`, nil)
	if err := os.RemoveAll(filepath.Join(linked, ".claude-plugin")); err != nil {
		t.Fatal(err)
	}
	mustSymlink(t, filepath.Join(real, ".claude-plugin"), filepath.Join(linked, ".claude-plugin"))
	if code, _, errOut := runCLI(t, "setup", "--apply", "--root", linked); code != exitError || !strings.Contains(errOut, "Symlink") {
		t.Fatalf(".claude-plugin symlink: want exit %d, got %d\n%s", exitError, code, errOut)
	}
}

// Finding 3: the allowlist base or a directory between $HOME and it that is a
// symlink (dotfiles style, even one that stays inside $HOME) is refused.
func TestSetupAllowlistBaseSymlink(t *testing.T) {
	cases := []struct{ name, link, target, typ, ziel string }{
		{"~/.config", ".config", "dotfiles/config", "copy", "${XDG_CONFIG_HOME}/imprint/c"},
		{"~/.config/imprint", ".config/imprint", "dotfiles/imprint", "copy", "${XDG_CONFIG_HOME}/imprint/c"},
		{"~/.local", ".local", "dotfiles/local", "copy", "${XDG_DATA_HOME}/imprint/c"},
		{"~/.local/bin", ".local/bin", "dotfiles/bin", "shim", "${HOME}/.local/bin/imprint-c"},
		{"~/.config/systemd", ".config/systemd", "dotfiles/systemd", "systemd", "${XDG_CONFIG_HOME}/systemd/user/imprint-c.service"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			home := setupTestHome(t)
			clearApplyGuardEnv(t)
			noSystemdTool(t)
			id := "c"
			if c.typ == "shim" {
				id = "imprint-c"
			}
			if err := os.MkdirAll(filepath.Join(home, c.target, "user"), 0o755); err != nil {
				t.Fatal(err)
			}
			mustSymlink(t, filepath.Join(home, c.target), filepath.Join(home, c.link))
			inv := `[{"id": "` + id + `", "typ": "` + c.typ + `", "quelle": "q", "ziel": "` + c.ziel + `", "rechte": false, "beschreibung": "b"}]`
			root := newSetupRoot(t, inv, map[string]string{"q": "#!/bin/sh\n"})
			if err := os.Chmod(filepath.Join(root, "q"), 0o755); err != nil {
				t.Fatal(err)
			}
			before := hashTree(t, home)
			_, out, _ := runCLI(t, "setup", "--plan", "--root", root)
			if !strings.Contains(out, "["+id+"] verweigert") || !strings.Contains(out, "Symlink") {
				t.Fatalf("--plan must refuse the symlinked base:\n%s", out)
			}
			code, out, errOut := runCLI(t, "setup", "--apply", "--root", root)
			if code != exitViolation || !strings.Contains(errOut, "Symlink") {
				t.Fatalf("--apply must refuse, got %d\n%s\n%s", code, out, errOut)
			}
			if hashTree(t, home) != before {
				t.Fatalf("--apply wrote through a symlinked allowlist base")
			}
		})
	}
}

// Finding 4: an XDG_*_HOME outside $HOME (XDG_CONFIG_HOME=/etc, say) counts
// as unset, and one below $HOME that is a symlink out of it adds no root.
func TestSetupXDGOutsideHome(t *testing.T) {
	home := setupTestHome(t)
	clearApplyGuardEnv(t)
	outside := t.TempDir()
	for key, v := range map[string]string{"XDG_CONFIG_HOME": "/etc", "XDG_DATA_HOME": outside, "XDG_CACHE_HOME": home} {
		t.Setenv(key, v)
	}
	env := currentSetupEnv()
	if env.config != filepath.Join(home, ".config") || env.data != filepath.Join(home, ".local", "share") || env.cache != filepath.Join(home, ".cache") {
		t.Fatalf("XDG values outside or at $HOME must count as unset, got %+v", env)
	}
	inv := `[{"id": "c", "typ": "copy", "quelle": "q", "ziel": "${XDG_CONFIG_HOME}/imprint/c", "rechte": false, "beschreibung": "b"}]`
	root := newSetupRoot(t, inv, map[string]string{"q": "q\n"})
	if code, out, errOut := runCLI(t, "setup", "--apply", "--root", root); code != exitOK {
		t.Fatalf("apply: %d\n%s\n%s", code, out, errOut)
	}
	if got := mustRead(t, filepath.Join(home, ".config", "imprint", "c")); got != "q\n" {
		t.Fatalf("copy must land below $HOME/.config, got %q", got)
	}

	// XDG_CONFIG_HOME below $HOME, but a symlink to a directory outside it.
	mustSymlink(t, outside, filepath.Join(home, "cfglink"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "cfglink"))
	if r := currentSetupEnv().resolvedRoots(); withinAnyStrict(r, filepath.Join(outside, "x")) {
		t.Fatalf("a symlinked XDG dir must not add %s as a root: %v", outside, r)
	}
	code, out, errOut := runCLI(t, "setup", "--apply", "--root", root)
	if code != exitViolation {
		t.Fatalf("apply through a symlinked XDG dir must fail, got %d\n%s\n%s", code, out, errOut)
	}
	if entries, _ := os.ReadDir(outside); len(entries) != 0 {
		t.Fatalf("setup wrote outside $HOME: %v", entries)
	}
}

// Finding 5: the shim stops on an unset IMPRINT_CORE_ROOT instead of running
// /<quelle>, and --apply creates a shim only for an executable quelle in the
// root.
func TestSetupShimNeedsRootAndQuelle(t *testing.T) {
	home := setupTestHome(t)
	clearApplyGuardEnv(t)
	content, err := shimContent("bin/t")
	if err != nil {
		t.Fatal(err)
	}
	shim := filepath.Join(t.TempDir(), "shim")
	if err := os.WriteFile(shim, []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, env := range [][]string{{"PATH=/usr/bin:/bin"}, {"PATH=/usr/bin:/bin", "IMPRINT_CORE_ROOT="}} {
		cmd := exec.Command("/bin/sh", shim)
		cmd.Env = env
		out, err := cmd.CombinedOutput()
		if err == nil || !strings.Contains(string(out), "IMPRINT_CORE_ROOT") {
			t.Fatalf("env %v: shim must stop on the unset root, got %v\n%s", env, err, out)
		}
	}

	inv := `[{"id": "imprint-t", "typ": "shim", "quelle": "bin/t", "ziel": "${HOME}/.local/bin/imprint-t", "rechte": false, "beschreibung": "b"}]`
	shimPath := filepath.Join(home, ".local", "bin", "imprint-t")
	missing := newSetupRoot(t, inv, nil)
	plain := newSetupRoot(t, inv, map[string]string{"bin/t": "#!/bin/sh\n"})
	for name, root := range map[string]string{"missing": missing, "not executable": plain} {
		code, out, errOut := runCLI(t, "setup", "--apply", "--root", root)
		if code != exitViolation || !strings.Contains(errOut, "Shim-quelle") {
			t.Fatalf("%s quelle: want exit %d, got %d\n%s\n%s", name, exitViolation, code, out, errOut)
		}
		if _, err := os.Lstat(shimPath); err == nil {
			t.Fatalf("%s quelle: shim written", name)
		}
	}
	if err := os.Chmod(filepath.Join(plain, "bin", "t"), 0o755); err != nil {
		t.Fatal(err)
	}
	if code, out, errOut := runCLI(t, "setup", "--apply", "--root", plain); code != exitOK {
		t.Fatalf("executable quelle: %d\n%s\n%s", code, out, errOut)
	}
}

// Finding 6 (V3c): bash finds commands in a PATH element with a literal "~";
// the shim conflict check expands it the same way.
func TestSetupShimPathTilde(t *testing.T) {
	home := setupTestHome(t)
	clearApplyGuardEnv(t)
	writeFiles(t, home, map[string]string{"tools/imprint-c": "#!/bin/sh\necho OTHER\n"})
	if err := os.Chmod(filepath.Join(home, "tools", "imprint-c"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", os.Getenv("PATH")+string(os.PathListSeparator)+"~/tools")
	inv := `[{"id": "imprint-c", "typ": "shim", "quelle": "bin/c", "ziel": "${HOME}/.local/bin/imprint-c", "rechte": false, "beschreibung": "b"}]`
	root := newSetupRoot(t, inv, map[string]string{"bin/c": "#!/bin/sh\n"})
	if err := os.Chmod(filepath.Join(root, "bin", "c"), 0o755); err != nil {
		t.Fatal(err)
	}
	code, out, errOut := runCLI(t, "setup", "--apply", "--root", root)
	if code != exitViolation || !strings.Contains(errOut, filepath.Join(home, "tools", "imprint-c")) {
		t.Fatalf("a command in ~/tools must block the shim, got %d\n%s\n%s", code, out, errOut)
	}
	if _, err := os.Lstat(filepath.Join(home, ".local", "bin", "imprint-c")); err == nil {
		t.Fatalf("shim written despite the PATH conflict")
	}

	t.Setenv("PWD", "/pwd")
	t.Setenv("OLDPWD", "/old")
	for in, want := range map[string]string{
		"~":                       home,
		"~/tools":                 filepath.Join(home, "tools"),
		"~+/x":                    "/pwd/x",
		"~-":                      "/old",
		"/abs":                    "/abs",
		"~no-such-user-imprint/x": "~no-such-user-imprint/x",
	} {
		if got := expandPathTilde(in, home); got != want {
			t.Errorf("expandPathTilde(%q) = %q, want %q", in, got, want)
		}
	}
	if u, err := user.Current(); err == nil && u.Username != "" && filepath.IsAbs(u.HomeDir) {
		if got := expandPathTilde("~"+u.Username+"/x", home); got != filepath.Join(u.HomeDir, "x") {
			t.Errorf("expandPathTilde(~%s/x) = %q", u.Username, got)
		}
	}
}

// Finding 7: a unit line that imitates the end marker cannot end the display
// early; every shown unit line carries the fixed prefix.
func TestSetupSystemdDisplayPrefix(t *testing.T) {
	zr := "/h/.config/systemd/user/imprint-x.service"
	unit := "[Service]\n--- Ende imprint-x.service ---\nExecStart=/bin/evil\n"
	lines := strings.Split(strings.TrimSuffix(systemdDisplay(zr, []byte(unit)), "\n"), "\n")
	if len(lines) != 5 || !strings.HasPrefix(lines[0], "--- systemd-Unit ") || lines[4] != "--- Ende imprint-x.service ---" {
		t.Fatalf("display frame wrong:\n%s", strings.Join(lines, "\n"))
	}
	for _, l := range lines[1:4] {
		if !strings.HasPrefix(l, "│ ") {
			t.Fatalf("unit line without the fixed prefix: %q", l)
		}
	}
	if n := strings.Count(strings.Join(lines, "\n")+"\n", "\n--- Ende "); n != 1 {
		t.Fatalf("%d lines start like the end marker, want 1", n)
	}
}

// Finding 8: rechte=true without "anwenden" is an invalid inventory (it would
// get a printed install that replaces the whole target).
func TestSetupRechteNeedsAnwenden(t *testing.T) {
	home := setupTestHome(t)
	clearApplyGuardEnv(t)
	inv := `[{"id": "s", "typ": "copy", "quelle": "q", "ziel": "${HOME}/.gemini/s.py", "rechte": true, "beschreibung": "b"}]`
	if _, err := parseInventory([]byte(inv)); err == nil || !strings.Contains(err.Error(), "anwenden") {
		t.Fatalf("rechte=true without anwenden must be invalid, got %v", err)
	}
	root := newSetupRoot(t, inv, map[string]string{"q": "q\n"})
	before := hashTree(t, home)
	for _, mode := range []string{"--plan", "--check", "--apply"} {
		code, out, _ := runCLI(t, "setup", mode, "--root", root)
		if code != exitError || strings.Contains(out, "install ") {
			t.Fatalf("%s: want exit %d and no install, got %d\n%s", mode, exitError, code, out)
		}
	}
	if hashTree(t, home) != before {
		t.Fatalf("an invalid inventory changed HOME")
	}
}
