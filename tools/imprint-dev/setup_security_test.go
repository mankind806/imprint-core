package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Guard tests for the setup security boundary (imprint-core-CL-011, blind review
// round 1). Each test replays one proof of concept from that review; none of them
// touches the real HOME: setupTestHome points HOME and every XDG base directory
// at a temp dir first.

// setupTestHome points HOME and every XDG base directory setup uses at a fresh
// temp dir, so no test reads or writes the real home.
func setupTestHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, ".local", "share"))
	return home
}

// clearApplyGuardEnv removes every variable the --apply env guard reacts to.
// The guard reacts to presence, so an empty value is not enough; t.Setenv
// registers the restore before os.Unsetenv removes the variable.
func clearApplyGuardEnv(t *testing.T) {
	t.Helper()
	for _, kv := range os.Environ() {
		key, _, _ := strings.Cut(kv, "=")
		if key == "CLAUDECODE" || strings.HasPrefix(key, "ANTIGRAVITY_") || strings.HasPrefix(key, "AGY_") {
			t.Setenv(key, "")
			if err := os.Unsetenv(key); err != nil {
				t.Fatal(err)
			}
		}
	}
}

// newSetupRoot creates a plugin root with setup/inventar.json and files.
func newSetupRoot(t *testing.T, inventory string, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	all := map[string]string{"setup/inventar.json": inventory}
	for k, v := range files {
		all[k] = v
	}
	writeFiles(t, root, all)
	return root
}

// newSetupRepo creates a foreign repository with .imprint/setup.json and files.
func newSetupRepo(t *testing.T, inventory string, files map[string]string) string {
	t.Helper()
	repo := t.TempDir()
	all := map[string]string{".imprint/setup.json": inventory}
	for k, v := range files {
		all[k] = v
	}
	writeFiles(t, repo, all)
	return repo
}

func mustSymlink(t *testing.T, target, link string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
}

func mustRead(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func assertNoCanary(t *testing.T, label, out string, canaries ...string) {
	t.Helper()
	for _, c := range canaries {
		if strings.Contains(out, c) {
			t.Fatalf("%s leaked canary %q:\n%s", label, c, out)
		}
	}
}

func assertNoDiff(t *testing.T, label, out string) {
	t.Helper()
	if strings.Contains(out, "--- diff for") {
		t.Fatalf("%s printed a diff where none may appear:\n%s", label, out)
	}
}

// PoC 1: an id with "../" used to become part of the rights-cache file name and
// wrote anywhere, even outside HOME.
func TestSetupRejectsIDTraversal(t *testing.T) {
	home := setupTestHome(t)
	clearApplyGuardEnv(t)
	outside := t.TempDir()
	cacheDir := filepath.Join(home, ".cache", "imprint")
	rel, err := filepath.Rel(cacheDir, filepath.Join(outside, "pwn"))
	if err != nil {
		t.Fatal(err)
	}

	badIDs := []string{filepath.ToSlash(rel), "../x", "a/b", "A", "-x", "a.b", "a b", "a;b", strings.Repeat("a", 65)}
	for _, id := range badIDs {
		inv := `[{"id": "` + id + `", "typ": "rechte-vorlage", "quelle": "t.json", "ziel": "${HOME}/.claude/settings.json", "rechte": true, "beschreibung": "b"}]`
		root := newSetupRoot(t, inv, map[string]string{"t.json": "{}"})
		repo := newSetupRepo(t, inv, map[string]string{"t.json": "{}"})
		pluginOnly := newSetupRoot(t, `[]`, nil)
		for _, args := range [][]string{
			{"setup", "--apply", "--root", root},
			{"setup", "--plan", "--root", root},
			{"setup", "--check", "--root", root},
			{"setup", "--apply", "--root", pluginOnly, "--repo", repo},
		} {
			code, _, stderr := runCLI(t, args...)
			if code != exitError {
				t.Fatalf("id %q, %v: expected exit %d, got %d (stderr %s)", id, args[1], exitError, code, stderr)
			}
		}
	}
	entries, _ := os.ReadDir(outside)
	if len(entries) != 0 {
		t.Fatalf("id traversal wrote outside HOME: %v", entries)
	}
	if _, err := os.Stat(cacheDir); err == nil {
		t.Fatalf("an invalid inventory must not create the cache dir")
	}

	for _, good := range []string{"a", "0", "imprint-dev", strings.Repeat("a", 64)} {
		if !setupIDPattern.MatchString(good) {
			t.Errorf("valid id %q rejected", good)
		}
	}
}

// PoC 2: a foreign --repo with rechte=false could write ~/.claude/settings.json,
// and --plan showed diffs of any file under HOME, SSH keys included.
func TestSetupRepoNeverWritesOrLeaks(t *testing.T) {
	home := setupTestHome(t)
	clearApplyGuardEnv(t)
	const canarySettings = "CANARY-settings-live-token"
	const canaryKey = "CANARY-ssh-private-key"
	const canaryAllowed = "CANARY-allowlisted-but-foreign"
	writeFiles(t, home, map[string]string{
		".claude/settings.json":     `{"env": {"T": "` + canarySettings + `"}}`,
		".ssh/id_x":                 canaryKey + "\n",
		".config/imprint/c.txt":     canaryAllowed + "\n",
		".local/share/imprint/keep": "x\n",
	})

	repoInv := `[
		{"id": "claude-settings", "typ": "copy", "quelle": "a.json", "ziel": "${HOME}/.claude/settings.json", "rechte": false, "beschreibung": "b"},
		{"id": "ssh-key", "typ": "copy", "quelle": "k", "ziel": "${HOME}/.ssh/id_x", "rechte": false, "beschreibung": "b"},
		{"id": "allowed-copy", "typ": "copy", "quelle": "c.txt", "ziel": "${XDG_CONFIG_HOME}/imprint/c.txt", "rechte": false, "beschreibung": "b"},
		{"id": "new-copy", "typ": "copy", "quelle": "c.txt", "ziel": "${XDG_DATA_HOME}/imprint/new.txt", "rechte": false, "beschreibung": "b"},
		{"id": "repo-rechte", "typ": "rechte-vorlage", "quelle": "a.json", "ziel": "${HOME}/.claude/settings.json", "rechte": true, "beschreibung": "b"},
		{"id": "repo-mcp", "typ": "mcp", "quelle": "evil-server", "ziel": "${HOME}/unused", "rechte": false, "beschreibung": "b"},
		{"id": "repo-market", "typ": "marketplace", "quelle": "evil/market", "ziel": "${HOME}/unused", "rechte": false, "beschreibung": "b"}
	]`
	repo := newSetupRepo(t, repoInv, map[string]string{
		"a.json": `{"permissions": {"allow": ["Bash(*)"]}}`,
		"k":      "attacker key\n",
		"c.txt":  "attacker content\n",
	})
	root := newSetupRoot(t, `[]`, nil)

	before := hashTree(t, home)
	for _, mode := range []string{"--plan", "--check", "--apply"} {
		code, stdout, stderr := runCLI(t, "setup", mode, "--root", root, "--repo", repo)
		if code == exitError {
			t.Fatalf("%s: unexpected exit %d, stderr %s", mode, code, stderr)
		}
		assertNoCanary(t, mode+" stdout", stdout, canarySettings, canaryKey, canaryAllowed)
		assertNoCanary(t, mode+" stderr", stderr, canarySettings, canaryKey, canaryAllowed)
		assertNoDiff(t, mode, stdout)
		for _, line := range strings.Split(stdout, "\n") {
			if strings.HasPrefix(line, "claude ") || strings.HasPrefix(line, "cp ") {
				t.Fatalf("%s printed a command for a --repo entry:\n%s", mode, stdout)
			}
		}
		for _, line := range strings.Split(stdout, "\n") {
			if (strings.HasPrefix(line, "claude-settings") || strings.HasPrefix(line, "ssh-key") || strings.HasPrefix(line, "repo-rechte")) && !strings.Contains(line, "gesperrt") {
				t.Fatalf("%s: rights path from --repo not reported as gesperrt: %q", mode, line)
			}
		}
		if after := hashTree(t, home); after != before {
			t.Fatalf("%s changed HOME for --repo entries", mode)
		}
	}
}

// Denylist and allowlist also bind the plugin's own inventory: rechte=false
// entries write only the per-typ allowlist and never a rights path.
func TestSetupPluginInventoryDenyAndAllowList(t *testing.T) {
	home := setupTestHome(t)
	clearApplyGuardEnv(t)
	const canary = "CANARY-plugin-deny"
	writeFiles(t, home, map[string]string{
		".claude/settings.json": canary,
		".bashrc":               canary,
		"random.txt":            canary,
	})
	inv := `[
		{"id": "d-claude", "typ": "copy", "quelle": "s", "ziel": "${HOME}/.claude/settings.json", "rechte": false, "beschreibung": "b"},
		{"id": "d-bashrc", "typ": "copy", "quelle": "s", "ziel": "${HOME}/.bashrc", "rechte": false, "beschreibung": "b"},
		{"id": "d-bashrcd", "typ": "copy", "quelle": "s", "ziel": "${HOME}/.bashrc.d/x.sh", "rechte": false, "beschreibung": "b"},
		{"id": "d-archiv", "typ": "copy", "quelle": "s", "ziel": "${XDG_CONFIG_HOME}/archiv/x", "rechte": false, "beschreibung": "b"},
		{"id": "d-keyring", "typ": "copy", "quelle": "s", "ziel": "${XDG_DATA_HOME}/keyrings/x", "rechte": false, "beschreibung": "b"},
		{"id": "d-wants", "typ": "systemd", "quelle": "s", "ziel": "${XDG_CONFIG_HOME}/systemd/user/default.target.wants/x.service", "rechte": false, "beschreibung": "b"},
		{"id": "v-random", "typ": "copy", "quelle": "s", "ziel": "${HOME}/random.txt", "rechte": false, "beschreibung": "b"},
		{"id": "v-shimname", "typ": "shim", "quelle": "bin/x", "ziel": "${HOME}/.local/bin/other-name", "rechte": false, "beschreibung": "b"},
		{"id": "v-unitext", "typ": "systemd", "quelle": "s", "ziel": "${XDG_CONFIG_HOME}/systemd/user/x.conf", "rechte": false, "beschreibung": "b"},
		{"id": "v-cache", "typ": "copy", "quelle": "s", "ziel": "${XDG_CACHE_HOME}/imprint/x", "rechte": false, "beschreibung": "b"}
	]`
	root := newSetupRoot(t, inv, map[string]string{"s": "new\n"})

	before := hashTree(t, home)
	code, stdout, stderr := runCLI(t, "setup", "--apply", "--root", root)
	if code != exitViolation {
		t.Fatalf("--apply with refused entries must exit %d, got %d\n%s\n%s", exitViolation, code, stdout, stderr)
	}
	if after := hashTree(t, home); after != before {
		t.Fatalf("--apply wrote a denied or non-allowlisted target")
	}
	for _, mode := range []string{"--plan", "--check"} {
		_, out, errOut := runCLI(t, "setup", mode, "--root", root)
		assertNoCanary(t, mode, out+errOut, canary)
		assertNoDiff(t, mode, out)
		for _, line := range strings.Split(out, "\n") {
			fields := strings.Fields(line)
			if len(fields) < 3 || len(fields[0]) < 2 || fields[0][1] != '-' {
				continue
			}
			want := map[byte]string{'d': "gesperrt", 'v': "verweigert"}[fields[0][0]]
			if want != "" && fields[2] != want {
				t.Errorf("%s: %s status %q, want %q", mode, fields[0], fields[2], want)
			}
		}
	}

	okInv := `[
		{"id": "ok-copy", "typ": "copy", "quelle": "s", "ziel": "${XDG_DATA_HOME}/imprint/sub/ok.txt", "rechte": false, "beschreibung": "b"},
		{"id": "ok-unit", "typ": "systemd", "quelle": "s", "ziel": "${XDG_CONFIG_HOME}/systemd/user/imprint-x.timer", "rechte": false, "beschreibung": "b"},
		{"id": "ok-shim", "typ": "shim", "quelle": "bin/ok-shim", "ziel": "${HOME}/.local/bin/ok-shim", "rechte": false, "beschreibung": "b"}
	]`
	okRoot := newSetupRoot(t, okInv, map[string]string{"s": "new\n"})
	if code, stdout, stderr := runCLI(t, "setup", "--apply", "--root", okRoot); code != exitOK {
		t.Fatalf("allowlisted targets must apply, got %d\n%s\n%s", code, stdout, stderr)
	}
	for _, p := range []string{".local/share/imprint/sub/ok.txt", ".config/systemd/user/imprint-x.timer", ".local/bin/ok-shim"} {
		if _, err := os.Stat(filepath.Join(home, p)); err != nil {
			t.Errorf("allowlisted target %s not written: %v", p, err)
		}
	}
}

// (d): typ rechte-vorlage without rechte=true makes the inventory invalid.
func TestSetupRechteVorlageRequiresRechte(t *testing.T) {
	setupTestHome(t)
	inv := `[{"id": "v", "typ": "rechte-vorlage", "quelle": "t.json", "ziel": "${HOME}/.claude/settings.json", "rechte": false, "beschreibung": "b"}]`
	root := newSetupRoot(t, inv, map[string]string{"t.json": "{}"})
	if code, _, _ := runCLI(t, "setup", "--plan", "--root", root); code != exitError {
		t.Fatalf("rechte-vorlage with rechte=false must be an invalid inventory (exit %d), got %d", exitError, code)
	}
}

// PoC 3: the root check was lexical while ReadFile/WriteFile follow symlinks.
func TestSetupSymlinks(t *testing.T) {
	const canary = "CANARY-behind-a-symlink"
	copyInv := func(ziel string) string {
		return `[{"id": "c", "typ": "copy", "quelle": "src.txt", "ziel": "` + ziel + `", "rechte": false, "beschreibung": "b"}]`
	}

	cases := []struct {
		name    string
		ziel    string
		prepare func(t *testing.T, home, outside string)
		files   map[string]string
		// root adjusts the plugin root after creation (for the quelle case).
		root func(t *testing.T, root, outside string)
	}{
		{
			name: "ziel is a symlink to a key",
			ziel: "${XDG_CONFIG_HOME}/imprint/link.txt",
			prepare: func(t *testing.T, home, _ string) {
				mustSymlink(t, filepath.Join(home, ".ssh", "id_x"), filepath.Join(home, ".config", "imprint", "link.txt"))
			},
		},
		{
			name: "ziel directory is a symlink into the denylist",
			ziel: "${XDG_CONFIG_HOME}/imprint/id_x",
			prepare: func(t *testing.T, home, _ string) {
				mustSymlink(t, filepath.Join(home, ".ssh"), filepath.Join(home, ".config", "imprint"))
			},
		},
		{
			name: "ziel directory is a symlink outside HOME",
			ziel: "${XDG_DATA_HOME}/imprint/x.txt",
			prepare: func(t *testing.T, home, outside string) {
				mustSymlink(t, outside, filepath.Join(home, ".local", "share", "imprint"))
			},
		},
		{
			name: ".bak is a symlink to a key",
			ziel: "${XDG_CONFIG_HOME}/imprint/t.txt",
			prepare: func(t *testing.T, home, _ string) {
				writeFiles(t, home, map[string]string{".config/imprint/t.txt": "old\n"})
				mustSymlink(t, filepath.Join(home, ".ssh", "id_x"), filepath.Join(home, ".config", "imprint", "t.txt.bak"))
			},
		},
		{
			name: ".bak is a directory",
			ziel: "${XDG_CONFIG_HOME}/imprint/t.txt",
			prepare: func(t *testing.T, home, _ string) {
				writeFiles(t, home, map[string]string{".config/imprint/t.txt": "old\n", ".config/imprint/t.txt.bak/keep": "x"})
			},
		},
		{
			name: "quelle is a symlink out of the plugin root",
			ziel: "${XDG_CONFIG_HOME}/imprint/q.txt",
			root: func(t *testing.T, root, outside string) {
				writeFiles(t, outside, map[string]string{"secret": canary})
				if err := os.Remove(filepath.Join(root, "src.txt")); err != nil {
					t.Fatal(err)
				}
				mustSymlink(t, filepath.Join(outside, "secret"), filepath.Join(root, "src.txt"))
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			home := setupTestHome(t)
			clearApplyGuardEnv(t)
			outside := t.TempDir()
			writeFiles(t, home, map[string]string{".ssh/id_x": canary})
			if tc.prepare != nil {
				tc.prepare(t, home, outside)
			}
			root := newSetupRoot(t, copyInv(tc.ziel), map[string]string{"src.txt": "new\n"})
			if tc.root != nil {
				tc.root(t, root, outside)
			}

			for _, mode := range []string{"--plan", "--check"} {
				_, out, errOut := runCLI(t, "setup", mode, "--root", root)
				assertNoCanary(t, mode, out+errOut, canary)
			}

			beforeHome := hashTree(t, home)
			beforeOutside := hashTree(t, outside)
			code, stdout, stderr := runCLI(t, "setup", "--apply", "--root", root)
			if code != exitViolation {
				t.Fatalf("--apply must refuse and exit %d, got %d\n%s\n%s", exitViolation, code, stdout, stderr)
			}
			assertNoCanary(t, "--apply", stdout+stderr, canary)
			if hashTree(t, home) != beforeHome {
				t.Fatalf("--apply changed HOME through a symlink")
			}
			if hashTree(t, outside) != beforeOutside {
				t.Fatalf("--apply wrote outside HOME through a symlink")
			}
			if got := mustRead(t, filepath.Join(home, ".ssh", "id_x")); got != canary {
				t.Fatalf("key behind the symlink was changed: %q", got)
			}
		})
	}
}

// The rights cache file is id-derived; a symlink planted at its place must not
// redirect the rendered template.
func TestSetupRechteCacheSymlink(t *testing.T) {
	home := setupTestHome(t)
	clearApplyGuardEnv(t)
	const canary = "CANARY-cache-redirect"
	writeFiles(t, home, map[string]string{".ssh/id_x": canary})
	mustSymlink(t, filepath.Join(home, ".ssh", "id_x"), filepath.Join(home, ".cache", "imprint", "settings-template-settings.json"))
	inv := `[{"id": "settings-template", "typ": "rechte-vorlage", "quelle": "t.json", "ziel": "${HOME}/.claude/settings.json", "rechte": true, "beschreibung": "b"}]`
	root := newSetupRoot(t, inv, map[string]string{"t.json": `{"x": 1}`})

	code, stdout, stderr := runCLI(t, "setup", "--apply", "--root", root)
	if code != exitViolation {
		t.Fatalf("expected exit %d, got %d\n%s\n%s", exitViolation, code, stdout, stderr)
	}
	if got := mustRead(t, filepath.Join(home, ".ssh", "id_x")); got != canary {
		t.Fatalf("rendered template was written through the cache symlink: %q", got)
	}
	if strings.Contains(stdout, "\ncp ") || strings.HasPrefix(stdout, "cp ") {
		t.Fatalf("no command may be printed when the cache file could not be written:\n%s", stdout)
	}
}

// PoC 4: a failed .bak used to be ignored and the target overwritten anyway; a
// .bak must also keep the original's mode.
func TestSetupBakKeepsModeAndContent(t *testing.T) {
	home := setupTestHome(t)
	clearApplyGuardEnv(t)
	target := filepath.Join(home, ".config", "imprint", "t.txt")
	writeFiles(t, home, map[string]string{".config/imprint/t.txt": "old\n"})
	if err := os.Chmod(target, 0o600); err != nil {
		t.Fatal(err)
	}
	inv := `[{"id": "c", "typ": "copy", "quelle": "src.txt", "ziel": "${XDG_CONFIG_HOME}/imprint/t.txt", "rechte": false, "beschreibung": "b"}]`
	root := newSetupRoot(t, inv, map[string]string{"src.txt": "new\n"})

	if code, stdout, stderr := runCLI(t, "setup", "--apply", "--root", root); code != exitOK {
		t.Fatalf("apply failed: %d\n%s\n%s", code, stdout, stderr)
	}
	fi, err := os.Stat(target + ".bak")
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Fatalf(".bak mode %v, want the original's 0600", fi.Mode().Perm())
	}
	if got := mustRead(t, target+".bak"); got != "old\n" {
		t.Fatalf(".bak content %q", got)
	}
	if got := mustRead(t, target); got != "new\n" {
		t.Fatalf("target content %q", got)
	}
}

// PoC 5: the env guard ignored variables that are set but empty.
func TestSetupApplyEnvGuardEmptyValues(t *testing.T) {
	home := setupTestHome(t)
	inv := `[{"id": "c", "typ": "copy", "quelle": "src.txt", "ziel": "${XDG_CONFIG_HOME}/imprint/x.txt", "rechte": false, "beschreibung": "b"}]`
	root := newSetupRoot(t, inv, map[string]string{"src.txt": "x\n"})
	before := hashTree(t, home)
	for _, key := range []string{"CLAUDECODE", "AGY_ANYTHING", "ANTIGRAVITY_ANYTHING"} {
		t.Run(key, func(t *testing.T) {
			clearApplyGuardEnv(t)
			t.Setenv(key, "")
			code, _, stderr := runCLI(t, "setup", "--apply", "--root", root)
			if code != exitError || !strings.Contains(stderr, "apply führt der Mensch aus") {
				t.Fatalf("empty %s must trigger the guard, got %d: %s", key, code, stderr)
			}
			if hashTree(t, home) != before {
				t.Fatalf("HOME changed although the guard triggered")
			}
		})
	}
}

// PoC 6: quelle/ziel/id with shell metacharacters reached printed commands and
// the shim unquoted. They are refused as an invalid inventory.
func TestSetupRejectsShellMetacharacters(t *testing.T) {
	home := setupTestHome(t)
	clearApplyGuardEnv(t)
	bad := []string{
		`{"id": "m", "typ": "mcp", "quelle": "srv;touch PWNED", "ziel": "${HOME}/u", "rechte": false, "beschreibung": "b"}`,
		`{"id": "m", "typ": "mcp", "quelle": "srv;id", "ziel": "${HOME}/u", "rechte": false, "beschreibung": "b"}`,
		`{"id": "m", "typ": "marketplace", "quelle": "$(id)", "ziel": "${HOME}/u", "rechte": false, "beschreibung": "b"}`,
		`{"id": "m", "typ": "marketplace", "quelle": "a b", "ziel": "${HOME}/u", "rechte": false, "beschreibung": "b"}`,
		`{"id": "s", "typ": "shim", "quelle": "bin/x\";id;\"", "ziel": "${HOME}/.local/bin/s", "rechte": false, "beschreibung": "b"}`,
		`{"id": "s", "typ": "shim", "quelle": "bin/$(id)", "ziel": "${HOME}/.local/bin/s", "rechte": false, "beschreibung": "b"}`,
		`{"id": "c", "typ": "copy", "quelle": "src.txt", "ziel": "${XDG_CONFIG_HOME}/imprint/$(id)", "rechte": false, "beschreibung": "b"}`,
		`{"id": "c", "typ": "copy", "quelle": "src.txt", "ziel": "${PWD}/imprint/x", "rechte": false, "beschreibung": "b"}`,
	}
	before := hashTree(t, home)
	for _, entry := range bad {
		root := newSetupRoot(t, "["+entry+"]", map[string]string{"src.txt": "x\n"})
		code, stdout, stderr := runCLI(t, "setup", "--apply", "--root", root)
		if code != exitError {
			t.Fatalf("entry %s: expected exit %d, got %d\n%s\n%s", entry, exitError, code, stdout, stderr)
		}
		if strings.Contains(stdout, "claude ") || strings.HasPrefix(stdout, "cp ") || strings.Contains(stdout, "\ncp ") {
			t.Fatalf("entry %s: a command was printed:\n%s", entry, stdout)
		}
	}
	if hashTree(t, home) != before {
		t.Fatalf("an invalid inventory changed HOME")
	}
}

func TestShellQuoteAndShimContent(t *testing.T) {
	cases := map[string]string{
		"/home/u/.cache/imprint/x-settings.json": "/home/u/.cache/imprint/x-settings.json",
		"a b":                                    "'a b'",
		"a;b":                                    "'a;b'",
		"$(id)":                                  "'$(id)'",
		"it's":                                   `'it'\''s'`,
		"":                                       "''",
	}
	for in, want := range cases {
		if got := shellQuote(in); got != want {
			t.Errorf("shellQuote(%q) = %q, want %q", in, got, want)
		}
	}
	got, err := shimContent("bin/imprint-dev")
	if err != nil || got != "#!/bin/sh\nexec \"$IMPRINT_CORE_ROOT\"/'bin/imprint-dev' \"$@\"\n" {
		t.Fatalf("shimContent = %q, %v", got, err)
	}
	for _, q := range []string{"bin/$(id)", "bin/`id`", `bin/"x`, "../x", "/abs"} {
		if _, err := shimContent(q); err == nil {
			t.Errorf("shimContent(%q) must be refused", q)
		}
	}
}

// --plan keeps working on the plugin's real inventory (setup/inventar.json of
// this repository), read-only and without touching HOME.
func TestSetupPlanRealPluginInventory(t *testing.T) {
	home := setupTestHome(t)
	root := filepath.Join("..", "..")
	if _, err := os.Stat(filepath.Join(root, "setup", "inventar.json")); err != nil {
		t.Fatalf("real inventory not found: %v", err)
	}
	before := hashTree(t, home)
	code, stdout, stderr := runCLI(t, "setup", "--plan", "--root", root)
	if code != exitOK {
		t.Fatalf("--plan on the real inventory exited %d\n%s\n%s", code, stdout, stderr)
	}
	if !strings.Contains(stdout, "pre-push-githook") {
		t.Fatalf("--plan output misses the real inventory's entry:\n%s", stdout)
	}
	if code, _, stderr := runCLI(t, "setup", "--check", "--root", root); code == exitError {
		t.Fatalf("--check on the real inventory could not run: %s", stderr)
	}
	if hashTree(t, home) != before {
		t.Fatalf("--plan/--check on the real inventory changed HOME")
	}
}
