package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Guard tests for blind review round 4 of the setup security boundary
// (imprint-core-CL-011). They replay race-anc.sh and the proofs of concept of
// round 3 that never reached the code under test (race3.sh tdir, poc3a.sh N3b,
// poc3b.sh V2a: every run failed on an unrelated check first). Temp dirs only;
// none touches the real HOME.

// swapOnWalk replaces beforeTargetWalk until the test ends: the first walk to
// a directory at or below anc swaps anc for a symlink to repl, once. unswap
// restores anc and arms the hook again.
func swapOnWalk(t *testing.T, anc, repl string) (swapped func() bool, unswap func()) {
	t.Helper()
	ancRes, err := filepath.EvalSymlinks(anc)
	if err != nil {
		t.Fatal(err)
	}
	done := false
	orig := beforeTargetWalk
	beforeTargetWalk = func(base, dir string) {
		if done || !isWithin(ancRes, dir) {
			return
		}
		done = true
		if err := os.Rename(anc, anc+".real"); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(repl, anc); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() { beforeTargetWalk = orig })
	return func() bool { return done }, func() {
		if !done {
			return
		}
		done = false
		os.Remove(anc)
		if err := os.Rename(anc+".real", anc); err != nil {
			t.Fatal(err)
		}
	}
}

// race-anc.sh swapped ~/.local (XDG_DATA_HOME below it) or ~/a
// (XDG_CONFIG_HOME=~/a/cfg) for a symlink to ~/.ssh while setup ran: the walk
// started at the resolved allowed root and opened it by path, so a directory
// between $HOME and that root was followed (35 hits in 20 s). The walk now
// starts at the resolved $HOME. beforeTargetWalk replays the winning moment.
func TestSetupTargetWalkStartsAtHome(t *testing.T) {
	const canary = "ANCESTOR-SWAP-CANARY"
	cases := []struct {
		name, anc, ziel, xdgKey, xdgRel, real, repl string
	}{
		{"~/.local below XDG_DATA_HOME", ".local", "${XDG_DATA_HOME}/imprint/k", "", "", ".local/share/imprint/k", ".ssh/share/imprint/k"},
		{"~/a below XDG_CONFIG_HOME=~/a/cfg", "a", "${XDG_CONFIG_HOME}/imprint/k", "XDG_CONFIG_HOME", "a/cfg", "a/cfg/imprint/k", ".ssh/cfg/imprint/k"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			home := setupTestHome(t)
			clearApplyGuardEnv(t)
			noSystemdTool(t)
			if c.xdgKey != "" {
				t.Setenv(c.xdgKey, filepath.Join(home, c.xdgRel))
			}
			// The target exists on both sides, so the old walk reaches the
			// read and the .bak instead of the create path.
			writeFiles(t, home, map[string]string{c.real: "different\n", c.repl: canary + "\n"})
			inv := `[{"id": "k", "typ": "copy", "quelle": "q", "ziel": "` + c.ziel + `", "rechte": false, "beschreibung": "b"}]`
			root := newSetupRoot(t, inv, map[string]string{"q": "PAYLOAD\n"})
			ssh := filepath.Join(home, ".ssh")
			swapped, unswap := swapOnWalk(t, filepath.Join(home, c.anc), ssh)
			before := hashTree(t, ssh)

			_, out, errOut := runCLI(t, "setup", "--plan", "--root", root)
			if !swapped() {
				t.Fatalf("the swap hook did not run:\n%s\n%s", out, errOut)
			}
			assertNoCanary(t, "--plan", out+errOut, canary)
			unswap()

			code, out, errOut := runCLI(t, "setup", "--apply", "--root", root)
			if !swapped() {
				t.Fatalf("the swap hook did not run on --apply")
			}
			unswap()
			assertNoCanary(t, "--apply", out+errOut, canary)
			if code != exitViolation || !strings.Contains(errOut, "Symlink") {
				t.Fatalf("--apply must fail the entry on the swapped ancestor, got %d\n%s\n%s", code, out, errOut)
			}
			if hashTree(t, ssh) != before {
				t.Fatalf("--apply wrote through the swapped ancestor into ~/.ssh")
			}
			if got := mustRead(t, filepath.Join(home, c.real)); got != "different\n" {
				t.Fatalf("real target changed: %q", got)
			}
		})
	}

	// writeRechteCache goes the same way: XDG_CACHE_HOME=~/c/cache, ~/c
	// swapped while the cache file of a print-only unit is written.
	t.Run("~/c below XDG_CACHE_HOME=~/c/cache", func(t *testing.T) {
		home := setupTestHome(t)
		clearApplyGuardEnv(t)
		noSystemdTool(t)
		t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "c", "cache"))
		writeFiles(t, home, map[string]string{
			".local/share/systemd/user/imprint-u.service": "vendor\n",
			"c/cache/imprint/keep":                        "x\n",
			".ssh/cache/imprint/keep":                     "x\n",
		})
		inv := `[{"id": "u", "typ": "systemd", "quelle": "u.service", "ziel": "${XDG_CONFIG_HOME}/systemd/user/imprint-u.service", "rechte": false, "beschreibung": "b"}]`
		root := newSetupRoot(t, inv, map[string]string{"u.service": "[Service]\n"})
		ssh := filepath.Join(home, ".ssh")
		swapped, unswap := swapOnWalk(t, filepath.Join(home, "c"), ssh)
		before := hashTree(t, ssh)
		code, out, errOut := runCLI(t, "setup", "--apply", "--root", root)
		if !swapped() {
			t.Fatalf("the swap hook did not run:\n%s\n%s", out, errOut)
		}
		unswap()
		if code != exitViolation || strings.Contains(out, "install ") || !strings.Contains(errOut, "Symlink") {
			t.Fatalf("expected exit %d, no printed command and a Symlink error, got %d\n%s\n%s", exitViolation, code, out, errOut)
		}
		if hashTree(t, ssh) != before {
			t.Fatalf("the rights cache was written through the swapped ancestor into ~/.ssh")
		}
	})
}

// race3.sh tdir-copy and tdir-unit swapped the target directory itself for a
// symlink to ~/.ssh, but used an id every run refused (ok=0), so the race
// never reached the write. Same swap with a valid id, deterministic: a
// control run writes the target, then the swap at the walk makes the entry
// fail without anything landing in ~/.ssh.
func TestSetupTargetDirSwappedAtWalk(t *testing.T) {
	cases := []struct{ name, typ, ziel, dir string }{
		{"copy", "copy", "${XDG_CONFIG_HOME}/imprint/authkeys", ".config/imprint"},
		{"systemd unit", "systemd", "${XDG_CONFIG_HOME}/systemd/user/imprint-ak.service", ".config/systemd/user"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			home := setupTestHome(t)
			clearApplyGuardEnv(t)
			noSystemdTool(t)
			writeFiles(t, home, map[string]string{".ssh/authorized_keys": "ORIGINAL\n"})
			if err := os.MkdirAll(filepath.Join(home, c.dir), 0o700); err != nil {
				t.Fatal(err)
			}
			inv := `[{"id": "ak", "typ": "` + c.typ + `", "quelle": "q", "ziel": "` + c.ziel + `", "rechte": false, "beschreibung": "b"}]`
			root := newSetupRoot(t, inv, map[string]string{"q": "RACE-PAYLOAD\n"})
			target := filepath.Join(home, c.dir, filepath.Base(c.ziel))

			if code, out, errOut := runCLI(t, "setup", "--apply", "--root", root); code != exitOK {
				t.Fatalf("control run must write the target, got %d\n%s\n%s", code, out, errOut)
			}
			if got := mustRead(t, target); got != "RACE-PAYLOAD\n" {
				t.Fatalf("control run wrote %q", got)
			}
			if err := os.Remove(target); err != nil {
				t.Fatal(err)
			}

			ssh := filepath.Join(home, ".ssh")
			swapped, unswap := swapOnWalk(t, filepath.Join(home, c.dir), ssh)
			before := hashTree(t, ssh)
			code, out, errOut := runCLI(t, "setup", "--apply", "--root", root)
			if !swapped() {
				t.Fatalf("the swap hook did not run:\n%s\n%s", out, errOut)
			}
			unswap()
			if code != exitViolation || !strings.Contains(errOut, "Symlink") {
				t.Fatalf("--apply must fail the entry on the swapped target dir, got %d\n%s\n%s", code, out, errOut)
			}
			if hashTree(t, ssh) != before {
				t.Fatalf("--apply wrote through the swapped target dir into ~/.ssh")
			}
		})
	}
}

// poc3a.sh N3b: a foreign ~/.local/bin/imprint-uv must stay as it is. The PoC's
// quelle did not exist, so --apply stopped before the marker check; here the
// quelle exists and is executable, and the refusal must come from the marker
// check itself. Control: without the foreign file the same shim is written.
func TestSetupShimForeignFileWithQuelle(t *testing.T) {
	home := setupTestHome(t)
	clearApplyGuardEnv(t)
	const foreign = "FOREIGN\n"
	writeFiles(t, home, map[string]string{".local/bin/imprint-uv": foreign})
	target := filepath.Join(home, ".local", "bin", "imprint-uv")
	if err := os.Chmod(target, 0o755); err != nil {
		t.Fatal(err)
	}
	inv := `[{"id": "imprint-uv", "typ": "shim", "quelle": "tools/x", "ziel": "${HOME}/.local/bin/imprint-uv", "rechte": false, "beschreibung": "b"}]`
	root := newSetupRoot(t, inv, map[string]string{"tools/x": "#!/bin/sh\n"})
	if err := os.Chmod(filepath.Join(root, "tools", "x"), 0o755); err != nil {
		t.Fatal(err)
	}

	for _, mode := range []string{"--plan", "--check"} {
		_, out, _ := runCLI(t, "setup", mode, "--root", root)
		if !strings.Contains(out, "[imprint-uv] verweigert") || !strings.Contains(out, "fremde Datei") {
			t.Fatalf("%s must refuse the foreign file:\n%s", mode, out)
		}
		assertNoDiff(t, mode, out)
	}
	before := hashTree(t, home)
	code, out, errOut := runCLI(t, "setup", "--apply", "--root", root)
	if code != exitViolation || !strings.Contains(out+errOut, "fremde Datei") {
		t.Fatalf("--apply must refuse the foreign file, got %d\n%s\n%s", code, out, errOut)
	}
	if hashTree(t, home) != before {
		t.Fatalf("--apply changed HOME over a foreign file")
	}
	if _, err := os.Lstat(target + ".bak"); !os.IsNotExist(err) {
		t.Fatalf("a .bak was written for a refused foreign file: %v", err)
	}

	if err := os.Remove(target); err != nil {
		t.Fatal(err)
	}
	if code, out, errOut := runCLI(t, "setup", "--apply", "--root", root); code != exitOK {
		t.Fatalf("control run must write the shim, got %d\n%s\n%s", code, out, errOut)
	}
	if !isSetupShim([]byte(mustRead(t, target))) {
		t.Fatalf("control run did not write a shim")
	}
}

// poc3b.sh V2a-d with an existing quelle (the PoC's was missing). The marker
// line claims the file for setup: a file that starts with it is replaced, and
// the old content is kept in .bak with its old mode, so nothing is lost. This
// is an accepted limit, not a check: whoever can write ~/.local/bin can also
// write the marker. Only the exact start "#!/bin/sh\n<marker>\n" counts; a
// BOM, another shebang or the marker further down leave the file foreign.
func TestSetupShimMarkerWithQuelle(t *testing.T) {
	inv := `[{"id": "imprint-t", "typ": "shim", "quelle": "bin/t", "ziel": "${HOME}/.local/bin/imprint-t", "rechte": false, "beschreibung": "b"}]`
	newRoot := func(t *testing.T) string {
		root := newSetupRoot(t, inv, map[string]string{"bin/t": "#!/bin/sh\n"})
		if err := os.Chmod(filepath.Join(root, "bin", "t"), 0o755); err != nil {
			t.Fatal(err)
		}
		return root
	}

	t.Run("V2a forged marker is replaced, .bak keeps content and mode", func(t *testing.T) {
		home := setupTestHome(t)
		clearApplyGuardEnv(t)
		forged := "#!/bin/sh\n" + shimMarker + "\necho FOREIGN-TOOL\n"
		writeFiles(t, home, map[string]string{".local/bin/imprint-t": forged})
		target := filepath.Join(home, ".local", "bin", "imprint-t")
		if err := os.Chmod(target, 0o700); err != nil {
			t.Fatal(err)
		}
		root := newRoot(t)
		if code, out, errOut := runCLI(t, "setup", "--apply", "--root", root); code != exitOK {
			t.Fatalf("a file with the marker is replaced, got %d\n%s\n%s", code, out, errOut)
		}
		if got := mustRead(t, target); !isSetupShim([]byte(got)) || !strings.Contains(got, "'bin/t'") {
			t.Fatalf("target not replaced by the shim: %q", got)
		}
		if got := mustRead(t, target+".bak"); got != forged {
			t.Fatalf(".bak %q, want the old content", got)
		}
		fi, err := os.Stat(target + ".bak")
		if err != nil {
			t.Fatal(err)
		}
		if fi.Mode().Perm() != 0o700 {
			t.Fatalf(".bak mode %o, want 0700", fi.Mode().Perm())
		}
	})

	for name, content := range map[string]string{
		"V2d BOM before the marker": "\xef\xbb\xbf#!/bin/sh\n" + shimMarker + "\necho BOM\n",
		"other shebang":             "#!/bin/bash\n" + shimMarker + "\necho BASH\n",
		"marker on line 3":          "#!/bin/sh\n# x\n" + shimMarker + "\necho LATE\n",
	} {
		t.Run(name+" stays foreign", func(t *testing.T) {
			home := setupTestHome(t)
			clearApplyGuardEnv(t)
			writeFiles(t, home, map[string]string{".local/bin/imprint-t": content})
			root := newRoot(t)
			before := hashTree(t, home)
			code, out, errOut := runCLI(t, "setup", "--apply", "--root", root)
			if code != exitViolation || !strings.Contains(out+errOut, "fremde Datei") {
				t.Fatalf("expected exit %d for a foreign file, got %d\n%s\n%s", exitViolation, code, out, errOut)
			}
			if hashTree(t, home) != before {
				t.Fatalf("a foreign file was replaced")
			}
		})
	}

	// V2b and V2c: the marker does not help a hard link or a symlink at the
	// target; the file behind it stays as it is.
	for _, kind := range []string{"V2b hardlink", "V2c symlink"} {
		t.Run(kind+" with marker is refused", func(t *testing.T) {
			home := setupTestHome(t)
			clearApplyGuardEnv(t)
			other := filepath.Join(t.TempDir(), "other")
			const content = "#!/bin/sh\n" + shimMarker + "\necho OTHER\n"
			if err := os.WriteFile(other, []byte(content), 0o755); err != nil {
				t.Fatal(err)
			}
			target := filepath.Join(home, ".local", "bin", "imprint-t")
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				t.Fatal(err)
			}
			want := "Symlink"
			if kind == "V2b hardlink" {
				want = "Hardlinks"
				if err := os.Link(other, target); err != nil {
					t.Skipf("hard links not supported here: %v", err)
				}
			} else if err := os.Symlink(other, target); err != nil {
				t.Fatal(err)
			}
			root := newRoot(t)
			before := hashTree(t, home)
			code, out, errOut := runCLI(t, "setup", "--apply", "--root", root)
			if code != exitViolation || !strings.Contains(out+errOut, want) {
				t.Fatalf("expected exit %d and %q, got %d\n%s\n%s", exitViolation, want, code, out, errOut)
			}
			if hashTree(t, home) != before || mustRead(t, other) != content {
				t.Fatalf("the file behind the %s was changed", kind)
			}
		})
	}
}
