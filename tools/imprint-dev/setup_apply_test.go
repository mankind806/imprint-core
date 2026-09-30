package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestZielEscapesErlaubteWurzeln(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("XDG_DATA_HOME", filepath.Join(tempHome, ".local", "share"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tempHome, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(tempHome, ".cache"))

	cases := []struct {
		ziel string
		want bool
	}{
		{"${HOME}/file.txt", false},
		{"${XDG_CONFIG_HOME}/app/config.json", false},
		{"${XDG_CACHE_HOME}/imprint/cache.txt", false},
		{"$HOME/.local/bin/imprint-dev", false},
		{"/etc/passwd", true},
		{"${HOME}/../etc/passwd", true},
		{"../../etc/passwd", true},
	}

	for _, c := range cases {
		if got := zielEscapesErlaubteWurzeln(c.ziel); got != c.want {
			t.Errorf("zielEscapesErlaubteWurzeln(%q) = %v, want %v", c.ziel, got, c.want)
		}
	}
}

func TestSetupExclusivity(t *testing.T) {
	root := t.TempDir()

	invalidFlagCombos := [][]string{
		{"setup", "--root", root},
		{"setup", "--plan", "--apply", "--root", root},
		{"setup", "--plan", "--check", "--root", root},
		{"setup", "--apply", "--check", "--root", root},
		{"setup", "--plan", "--apply", "--check", "--root", root},
	}

	for _, args := range invalidFlagCombos {
		var stdout, stderr bytes.Buffer
		code := run(args, &stdout, &stderr)
		if code != exitError {
			t.Errorf("args %v: expected exit code %d, got %d", args, exitError, code)
		}
		if !bytes.Contains(stderr.Bytes(), []byte("exactly one of --plan, --apply, or --check flag is required")) &&
			!bytes.Contains(stderr.Bytes(), []byte("imprint-dev setup:")) {
			t.Errorf("args %v: unexpected stderr: %s", args, stderr.String())
		}
	}
}

func TestSetupApplyEnvGuard(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("XDG_DATA_HOME", filepath.Join(tempHome, ".local", "share"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tempHome, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(tempHome, ".cache"))

	root := t.TempDir()
	writePluginManifest(t, root)
	setupDir := filepath.Join(root, "setup")
	_ = os.MkdirAll(setupDir, 0755)
	_ = os.WriteFile(filepath.Join(root, "src.txt"), []byte("data\n"), 0644)

	invJSON := `[{"id": "item1", "typ": "copy", "quelle": "src.txt", "ziel": "${XDG_CONFIG_HOME}/imprint/dst.txt", "rechte": false, "beschreibung": "b"}]`
	_ = os.WriteFile(filepath.Join(setupDir, "inventar.json"), []byte(invJSON), 0644)

	beforeHash := hashTree(t, tempHome)

	envCases := []struct {
		key string
		val string
	}{
		{"CLAUDECODE", "1"},
		{"AGY_SESSION", "active"},
		{"ANTIGRAVITY_CONFIG_DIR", "/tmp/agy"},
		{"CLAUDECODE", ""},
		{"AGY_SESSION", ""},
		{"ANTIGRAVITY_CONFIG_DIR", ""},
	}

	for _, tc := range envCases {
		t.Run(tc.key+"="+tc.val, func(t *testing.T) {
			clearApplyGuardEnv(t)
			t.Setenv(tc.key, tc.val)

			var stdout, stderr bytes.Buffer
			code := run([]string{"setup", "--apply", "--root", root}, &stdout, &stderr)
			if code != exitError {
				t.Fatalf("expected exit error 2, got %d", code)
			}
			if !bytes.Contains(stderr.Bytes(), []byte("apply führt der Mensch aus")) {
				t.Fatalf("stderr missing required phrase 'apply führt der Mensch aus', got:\n%s", stderr.String())
			}

			afterHash := hashTree(t, tempHome)
			if beforeHash != afterHash {
				t.Fatalf("HOME was modified when Env-Guard triggered!")
			}
		})
	}
}

func TestSetupApplyAndCheckFreshHome(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("XDG_DATA_HOME", filepath.Join(tempHome, ".local", "share"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tempHome, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(tempHome, ".cache"))

	// Ensure env guard is not triggered, and keep the real ~/.local/bin out of PATH
	clearApplyGuardEnv(t)
	setHermeticPath(t)

	root := t.TempDir()
	writePluginManifest(t, root)
	setupDir := filepath.Join(root, "setup")
	_ = os.MkdirAll(setupDir, 0755)

	// Create test files
	_ = os.WriteFile(filepath.Join(root, "copy_src.txt"), []byte("copy content\n"), 0644)
	_ = os.WriteFile(filepath.Join(root, "shim_src.txt"), []byte("shim source\n"), 0644)

	// Create git repo for githook test
	cmdInit := exec.Command("git", "init", root)
	if err := cmdInit.Run(); err != nil {
		t.Fatalf("failed to init git repo: %v", err)
	}

	invJSON := `[
		{
			"id": "copy-item",
			"typ": "copy",
			"quelle": "copy_src.txt",
			"ziel": "${XDG_CONFIG_HOME}/imprint/copy_dst.txt",
			"rechte": false,
			"beschreibung": "Copy file"
		},
		{
			"id": "imprint-dev",
			"typ": "shim",
			"quelle": "bin/imprint-dev",
			"ziel": "${HOME}/.local/bin/imprint-dev",
			"rechte": false,
			"beschreibung": "Shim script"
		},
		{
			"id": "githook-item",
			"typ": "githook",
			"quelle": ".githooks/pre-push",
			"ziel": "${HOME}/.git/hooks/pre-push",
			"rechte": false,
			"beschreibung": "Git hook"
		}
	]`
	_ = os.WriteFile(filepath.Join(setupDir, "inventar.json"), []byte(invJSON), 0644)

	// Apply
	var stdoutApply, stderrApply bytes.Buffer
	codeApply := run([]string{"setup", "--apply", "--root", root}, &stdoutApply, &stderrApply)
	if codeApply != exitOK {
		t.Fatalf("apply failed with exit code %d. stderr: %s", codeApply, stderrApply.String())
	}

	// Verify copy file
	copyDstData, err := os.ReadFile(filepath.Join(tempHome, ".config", "imprint", "copy_dst.txt"))
	if err != nil || string(copyDstData) != "copy content\n" {
		t.Fatalf("copy dst file missing or incorrect: %v, content: %q", err, string(copyDstData))
	}

	// Verify shim file
	shimDstData, err := os.ReadFile(filepath.Join(tempHome, ".local", "bin", "imprint-dev"))
	expectedShim := "#!/bin/sh\n" + shimMarker + "\nexec \"$IMPRINT_CORE_ROOT\"/'bin/imprint-dev' \"$@\"\n"
	if err != nil || string(shimDstData) != expectedShim {
		t.Fatalf("shim dst file missing or incorrect: %v, content: %q", err, string(shimDstData))
	}

	// Verify git config
	cmdCheckGit := exec.Command("git", "-C", root, "config", "--get", "core.hooksPath")
	gitOut, err := cmdCheckGit.Output()
	if err != nil || strings.TrimSpace(string(gitOut)) != ".githooks" {
		t.Fatalf("git hooksPath missing or incorrect: %v, out: %q", err, string(gitOut))
	}

	// Run check
	var stdoutCheck, stderrCheck bytes.Buffer
	codeCheck := run([]string{"setup", "--check", "--root", root}, &stdoutCheck, &stderrCheck)
	if codeCheck != exitOK {
		t.Fatalf("check failed with exit code %d (drift detected). stdout: %s, stderr: %s", codeCheck, stdoutCheck.String(), stderrCheck.String())
	}

	// Run plan
	var stdoutPlan, stderrPlan bytes.Buffer
	codePlan := run([]string{"setup", "--plan", "--root", root}, &stdoutPlan, &stderrPlan)
	if codePlan != exitOK {
		t.Fatalf("plan failed with exit code %d. stderr: %s", codePlan, stderrPlan.String())
	}
	if bytes.Contains(stdoutPlan.Bytes(), []byte("abweichend")) || bytes.Contains(stdoutPlan.Bytes(), []byte("fehlt")) {
		t.Fatalf("plan output after apply contains abweichend or fehlt:\n%s", stdoutPlan.String())
	}
}

func TestSetupApplyRechteTor(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("XDG_DATA_HOME", filepath.Join(tempHome, ".local", "share"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tempHome, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(tempHome, ".cache"))

	clearApplyGuardEnv(t)

	root := t.TempDir()
	writePluginManifest(t, root)
	setupDir := filepath.Join(root, "setup")
	_ = os.MkdirAll(setupDir, 0755)

	templateContent := `{"home": "${HOME}", "config": "${XDG_CONFIG_HOME}"}`
	_ = os.WriteFile(filepath.Join(root, "tmpl.json"), []byte(templateContent), 0644)

	invJSON := `[
		{
			"id": "settings-template",
			"typ": "rechte-vorlage",
			"quelle": "tmpl.json",
			"ziel": "${HOME}/.claude/settings.json",
			"rechte": true,
			"beschreibung": "Settings template"
		}
	]`
	_ = os.WriteFile(filepath.Join(setupDir, "inventar.json"), []byte(invJSON), 0644)

	// Ensure target file does NOT exist initially
	targetPath := filepath.Join(tempHome, ".claude", "settings.json")
	if _, err := os.Stat(targetPath); err == nil {
		t.Fatalf("target file should not exist before apply")
	}

	var stdout, stderr bytes.Buffer
	code := run([]string{"setup", "--apply", "--root", root}, &stdout, &stderr)
	if code != exitOK {
		t.Fatalf("apply failed with exit code %d. stderr: %s", code, stderr.String())
	}

	// 1. Target file must STILL NOT exist after apply
	if _, err := os.Stat(targetPath); err == nil {
		t.Fatalf("target file at ziel must not be created by apply for rechte=true!")
	}

	// 2. Cache file must exist and contain rendered values
	expectedCachePath := filepath.Join(tempHome, ".cache", "imprint", "settings-template-settings.json")
	cacheData, err := os.ReadFile(expectedCachePath)
	if err != nil {
		t.Fatalf("rendered cache file missing at %s: %v", expectedCachePath, err)
	}

	expectedRendered := `{"home": "` + tempHome + `", "config": "` + filepath.Join(tempHome, ".config") + `"}`
	if string(cacheData) != expectedRendered {
		t.Fatalf("rendered cache file content mismatch: got %q, want %q", string(cacheData), expectedRendered)
	}

	// 3. Stdout must contain the install command with an explicit mode
	if !bytes.Contains(stdout.Bytes(), []byte("install -m 0644 "+expectedCachePath)) {
		t.Fatalf("stdout missing printed install command with cache path, got:\n%s", stdout.String())
	}
}

func TestSetupApplySystemd(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("XDG_DATA_HOME", filepath.Join(tempHome, ".local", "share"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tempHome, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(tempHome, ".cache"))

	// Set PATH to a directory without systemctl
	emptyPathDir := t.TempDir()
	t.Setenv("PATH", emptyPathDir)

	clearApplyGuardEnv(t)

	root := t.TempDir()
	writePluginManifest(t, root)
	setupDir := filepath.Join(root, "setup")
	_ = os.MkdirAll(setupDir, 0755)

	_ = os.WriteFile(filepath.Join(root, "service.service"), []byte("[Unit]\nDescription=Test\n"), 0644)

	invJSON := `[
		{
			"id": "systemd-service",
			"typ": "systemd",
			"quelle": "service.service",
			"ziel": "${XDG_CONFIG_HOME}/systemd/user/imprint-systemd-service.service",
			"rechte": false,
			"beschreibung": "Systemd unit"
		}
	]`
	_ = os.WriteFile(filepath.Join(setupDir, "inventar.json"), []byte(invJSON), 0644)

	var stdout, stderr bytes.Buffer
	code := run([]string{"setup", "--apply", "--root", root}, &stdout, &stderr)
	if code != exitOK {
		t.Fatalf("apply failed with exit code %d. stderr: %s", code, stderr.String())
	}

	dstPath := filepath.Join(tempHome, ".config", "systemd", "user", "imprint-systemd-service.service")
	data, err := os.ReadFile(dstPath)
	if err != nil || string(data) != "[Unit]\nDescription=Test\n" {
		t.Fatalf("systemd unit file missing or content incorrect: %v, content: %q", err, string(data))
	}
	// Units run code once enabled: every copy is shown before it is written.
	if !strings.Contains(stdout.String(), "--- systemd-Unit "+dstPath) || !strings.Contains(stdout.String(), "Description=Test") {
		t.Fatalf("unit content not shown before the copy:\n%s", stdout.String())
	}
}

func TestSetupApplyGithook(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("XDG_DATA_HOME", filepath.Join(tempHome, ".local", "share"))

	clearApplyGuardEnv(t)

	root := t.TempDir()
	writePluginManifest(t, root)
	cmdInit := exec.Command("git", "init", root)
	if err := cmdInit.Run(); err != nil {
		t.Fatalf("failed to init git repo: %v", err)
	}

	setupDir := filepath.Join(root, "setup")
	_ = os.MkdirAll(setupDir, 0755)

	invJSON := `[
		{
			"id": "hook",
			"typ": "githook",
			"quelle": ".githooks/pre-push",
			"ziel": "${HOME}/.git/hooks/pre-push",
			"rechte": false,
			"beschreibung": "Githook"
		}
	]`
	_ = os.WriteFile(filepath.Join(setupDir, "inventar.json"), []byte(invJSON), 0644)

	// Check before apply -> detects drift
	var stdoutCheck1, stderrCheck1 bytes.Buffer
	codeCheck1 := run([]string{"setup", "--check", "--root", root}, &stdoutCheck1, &stderrCheck1)
	if codeCheck1 != exitViolation {
		t.Fatalf("check before apply expected exit code 1 (drift), got %d", codeCheck1)
	}

	// Apply
	var stdoutApply, stderrApply bytes.Buffer
	codeApply := run([]string{"setup", "--apply", "--root", root}, &stdoutApply, &stderrApply)
	if codeApply != exitOK {
		t.Fatalf("apply failed with exit code %d. stderr: %s", codeApply, stderrApply.String())
	}

	// Verify git config
	cmdCheckGit := exec.Command("git", "-C", root, "config", "--get", "core.hooksPath")
	out, err := cmdCheckGit.Output()
	if err != nil || strings.TrimSpace(string(out)) != ".githooks" {
		t.Fatalf("git config core.hooksPath not set to .githooks: %v, out: %q", err, string(out))
	}

	// Check after apply -> exit 0
	var stdoutCheck2, stderrCheck2 bytes.Buffer
	codeCheck2 := run([]string{"setup", "--check", "--root", root}, &stdoutCheck2, &stderrCheck2)
	if codeCheck2 != exitOK {
		t.Fatalf("check after apply expected exit code 0, got %d. stderr: %s", codeCheck2, stderrCheck2.String())
	}
}

func TestSetupApplyIdempotency(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("XDG_DATA_HOME", filepath.Join(tempHome, ".local", "share"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tempHome, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(tempHome, ".cache"))

	clearApplyGuardEnv(t)

	root := t.TempDir()
	writePluginManifest(t, root)
	setupDir := filepath.Join(root, "setup")
	_ = os.MkdirAll(setupDir, 0755)

	_ = os.WriteFile(filepath.Join(root, "file.txt"), []byte("data\n"), 0644)

	invJSON := `[
		{
			"id": "file-item",
			"typ": "copy",
			"quelle": "file.txt",
			"ziel": "${XDG_CONFIG_HOME}/imprint/file.txt",
			"rechte": false,
			"beschreibung": "File"
		}
	]`
	_ = os.WriteFile(filepath.Join(setupDir, "inventar.json"), []byte(invJSON), 0644)

	// Apply 1
	var stdout1, stderr1 bytes.Buffer
	code1 := run([]string{"setup", "--apply", "--root", root}, &stdout1, &stderr1)
	if code1 != exitOK {
		t.Fatalf("apply 1 failed: %d, stderr: %s", code1, stderr1.String())
	}

	hash1 := hashTree(t, tempHome)

	// Apply 2
	var stdout2, stderr2 bytes.Buffer
	code2 := run([]string{"setup", "--apply", "--root", root}, &stdout2, &stderr2)
	if code2 != exitOK {
		t.Fatalf("apply 2 failed: %d, stderr: %s", code2, stderr2.String())
	}

	hash2 := hashTree(t, tempHome)

	if hash1 != hash2 {
		t.Fatalf("HOME state changed between Apply 1 and Apply 2! hash1: %s, hash2: %s", hash1, hash2)
	}

	// Verify no .bak files created
	if _, err := os.Stat(filepath.Join(tempHome, ".config", "imprint", "file.txt.bak")); err == nil {
		t.Fatalf(".bak file should not be created when file was already up to date")
	}
}

// TestSetupPlanRechteGatedNeverLeaksContent guards against a real regression:
// setup --plan used to run the same byte-diff/content report for every entry,
// including rechte=true entries whose ziel is a real, potentially secret-bearing
// file (e.g. ~/.claude/settings.json with live env values or hooks). For those
// entries --plan (and --check) must report only a status such as
// "abweichend"/"fehlt"/"gleich", never the diff text or file content, so a
// canary value that only exists in the real target file can never appear in
// the tool's stdout.
func TestSetupPlanRechteGatedNeverLeaksContent(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("XDG_DATA_HOME", filepath.Join(tempHome, ".local", "share"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tempHome, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(tempHome, ".cache"))

	root := t.TempDir()
	writePluginManifest(t, root)
	setupDir := filepath.Join(root, "setup")
	_ = os.MkdirAll(setupDir, 0755)

	const canary = "CANARY-TOKEN-super-secret-env-value-should-never-be-printed"

	// quelle: the template, as it ships in the repo (no secret in it).
	_ = os.WriteFile(filepath.Join(root, "tmpl.json"), []byte(`{"env": {"TOKEN": "placeholder"}}`), 0644)

	// ziel: stands in for the real, already-applied settings file that carries
	// a live secret the tool must never echo back.
	targetDir := filepath.Join(tempHome, ".claude")
	_ = os.MkdirAll(targetDir, 0755)
	targetPath := filepath.Join(targetDir, "settings.json")
	_ = os.WriteFile(targetPath, []byte(`{"env": {"TOKEN": "`+canary+`"}}`), 0644)

	invJSON := `[
		{
			"id": "settings-template",
			"typ": "rechte-vorlage",
			"quelle": "tmpl.json",
			"ziel": "${HOME}/.claude/settings.json",
			"rechte": true,
			"beschreibung": "Settings template"
		}
	]`
	_ = os.WriteFile(filepath.Join(setupDir, "inventar.json"), []byte(invJSON), 0644)

	for _, mode := range []string{"--plan", "--check"} {
		t.Run(mode, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code := run([]string{"setup", mode, "--root", root}, &stdout, &stderr)
			// A rights target is applied by the human only: both modes show its
			// status, marked (Mensch), and --check does not count it as drift.
			if code != exitOK {
				t.Fatalf("%s expected exit code %d, got %d. stderr: %s", mode, exitOK, code, stderr.String())
			}
			if !bytes.Contains(stdout.Bytes(), []byte("abweichend (Mensch)")) {
				t.Fatalf("%s must mark the rights entry (Mensch), got:\n%s", mode, stdout.String())
			}

			if bytes.Contains(stdout.Bytes(), []byte(canary)) {
				t.Fatalf("%s leaked the canary token into stdout:\n%s", mode, stdout.String())
			}
			if !bytes.Contains(stdout.Bytes(), []byte("settings-template")) || !bytes.Contains(stdout.Bytes(), []byte("abweichend")) {
				t.Fatalf("%s must still report the entry's status (abweichend), got:\n%s", mode, stdout.String())
			}
			if bytes.Contains(stdout.Bytes(), []byte("diff for settings-template")) {
				t.Fatalf("%s must not print a diff header for a rechte=true entry, got:\n%s", mode, stdout.String())
			}
		})
	}
}
