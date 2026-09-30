package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Guard tests for imprint-core-CL-011, blind review round 5.

// Befund 1: "git -C <root> config --local core.hooksPath .githooks" silently
// walks up to an ancestor repository's config when root has no .git of its
// own. --root nested in a foreign repo without its own .git must refuse
// (status Fehler) instead of changing the foreign repo's config.
func TestSetupApplyGithookRefusesForeignRepoRoot(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("XDG_DATA_HOME", filepath.Join(tempHome, ".local", "share"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tempHome, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(tempHome, ".cache"))
	clearApplyGuardEnv(t)

	foreignRepo := t.TempDir()
	if err := exec.Command("git", "init", foreignRepo).Run(); err != nil {
		t.Fatalf("git init: %v", err)
	}

	root := filepath.Join(foreignRepo, "nested-plugin")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	// root has no .git of its own: git -C root would discover foreignRepo's.

	inv := `[
		{
			"id": "hook",
			"typ": "githook",
			"quelle": ".githooks/pre-push",
			"ziel": "${HOME}/.git/hooks/pre-push",
			"rechte": false,
			"beschreibung": "Githook"
		}
	]`
	writeFiles(t, root, map[string]string{
		"setup/inventar.json":        inv,
		".claude-plugin/plugin.json": `{"name": "imprint"}`,
	})
	anchorPluginRoot(t, root)

	var stdout, stderr bytes.Buffer
	code := run([]string{"setup", "--apply", "--root", root}, &stdout, &stderr)
	if code != exitViolation {
		t.Fatalf("expected exitViolation, got %d; stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), "Fehler") {
		t.Fatalf("expected status Fehler in the table, got:\n%s", stdout.String())
	}

	out, err := exec.Command("git", "-C", foreignRepo, "config", "--get", "core.hooksPath").Output()
	if err == nil {
		t.Fatalf("parent repo's config was changed: core.hooksPath=%q", strings.TrimSpace(string(out)))
	}
}

// Befund 2: --apply used to write every entry as it validated it, so one
// invalid entry among several valid ones still left the valid ones written.
// [valid, missing quelle] must write nothing at all.
func TestSetupApplyValidatesAllBeforeWriting(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("XDG_DATA_HOME", filepath.Join(tempHome, ".local", "share"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tempHome, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(tempHome, ".cache"))
	clearApplyGuardEnv(t)
	setHermeticPath(t)

	root := t.TempDir()
	writePluginManifest(t, root)
	if err := os.MkdirAll(filepath.Join(root, "setup"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "good-src.txt"), []byte("good content\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// "missing-src.txt" is intentionally never created.

	inv := `[
		{
			"id": "good",
			"typ": "copy",
			"quelle": "good-src.txt",
			"ziel": "${XDG_CONFIG_HOME}/imprint/good.txt",
			"rechte": false,
			"beschreibung": "valid entry"
		},
		{
			"id": "bad",
			"typ": "copy",
			"quelle": "missing-src.txt",
			"ziel": "${XDG_CONFIG_HOME}/imprint/bad.txt",
			"rechte": false,
			"beschreibung": "entry with a missing source"
		}
	]`
	if err := os.WriteFile(filepath.Join(root, "setup", "inventar.json"), []byte(inv), 0o644); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	code := run([]string{"setup", "--apply", "--root", root}, &stdout, &stderr)
	if code != exitViolation {
		t.Fatalf("expected exitViolation, got %d; stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	if _, err := os.Stat(filepath.Join(tempHome, ".config", "imprint", "good.txt")); err == nil {
		t.Fatalf("the valid entry must not be written when a sibling entry fails validation")
	}
	if _, err := os.Stat(filepath.Join(tempHome, ".config", "imprint", "bad.txt")); err == nil {
		t.Fatalf("the invalid entry must not be written either")
	}
}

// Befund 3: rechte-anwenden.sh (imprint-core-CL-012) merges permissions.allow
// and permissions.deny as a union of the target's own entries and the
// template's, not as jq's "a * b" (which replaces the whole array). After a
// correct apply, the target legitimately holds more entries than the
// template; fragmentStatus must still report "gleich" as long as every
// template entry is present.
func TestFragmentStatusPermissionsArraysAreSubsetChecked(t *testing.T) {
	tmpl := []byte(`{"permissions":{"allow":["a","b"],"deny":["x"]}}`)
	cur := []byte(`{"permissions":{"allow":["b","a","c"],"deny":["x","y"]},"other":"unchanged"}`)

	status, reason := fragmentStatus(tmpl, cur)
	if status != "gleich" {
		t.Fatalf("expected gleich for a target that is a superset of the template, got %q (%s)", status, reason)
	}

	// A template entry missing from the target must still be drift.
	tmplMissing := []byte(`{"permissions":{"allow":["a","b","d"],"deny":["x"]}}`)
	status, _ = fragmentStatus(tmplMissing, cur)
	if status != "abweichend" {
		t.Fatalf("expected abweichend when a template entry is missing from the target, got %q", status)
	}

	// Any key outside permissions.allow/deny still needs an exact match.
	curChangedOther := []byte(`{"permissions":{"allow":["b","a","c"],"deny":["x","y"]},"other":"changed"}`)
	status, _ = fragmentStatus(tmpl, curChangedOther)
	if status != "gleich" {
		// "other" is not part of the template, so it is untouched by the
		// merge and must not affect the comparison either way.
		t.Fatalf("expected gleich regardless of a key the template does not mention, got %q", status)
	}
}

// Befund 4: the printed rechte-anwenden.sh call for the "agy" target needs
// PROJEKTE and TRUSTED_WORKSPACE set (envsubst in the script); --apply must
// print a comment saying so next to that call, and only that one.
func TestSetupApplyPrintsAgyEnvHinweis(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("XDG_DATA_HOME", filepath.Join(tempHome, ".local", "share"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tempHome, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(tempHome, ".cache"))
	clearApplyGuardEnv(t)

	root := t.TempDir()
	writePluginManifest(t, root)
	if err := os.MkdirAll(filepath.Join(root, "setup", "vorlagen"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "setup", "vorlagen", "rechte-anwenden.sh"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "agy-tmpl.json"), []byte(`{}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "claude-tmpl.json"), []byte(`{}`), 0o644); err != nil {
		t.Fatal(err)
	}

	inv := `[
		{
			"id": "agy-rechte-vorlage",
			"typ": "rechte-vorlage",
			"quelle": "agy-tmpl.json",
			"ziel": "${HOME}/.gemini/antigravity-cli/settings.json",
			"rechte": true,
			"anwenden": "ersetzen",
			"beschreibung": "agy settings"
		},
		{
			"id": "claude-rechte-vorlage",
			"typ": "rechte-vorlage",
			"quelle": "claude-tmpl.json",
			"ziel": "${HOME}/.claude/settings.json",
			"rechte": true,
			"anwenden": "ersetzen",
			"beschreibung": "claude settings"
		}
	]`
	if err := os.WriteFile(filepath.Join(root, "setup", "inventar.json"), []byte(inv), 0o644); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	code := run([]string{"setup", "--apply", "--root", root}, &stdout, &stderr)
	if code != exitOK {
		t.Fatalf("apply failed with exit code %d. stderr: %s", code, stderr.String())
	}

	out := stdout.String()
	script, _ := filepath.EvalSymlinks(filepath.Join(root, "setup", "vorlagen", "rechte-anwenden.sh"))
	agyCall := shellQuote(script) + " agy\n" + rechteSkriptAgyHinweis
	if !strings.Contains(out, agyCall) {
		t.Fatalf("expected the agy call directly followed by the env hinweis, got:\n%s", out)
	}
	claudeCall := shellQuote(script) + " claude\n"
	if !strings.Contains(out, claudeCall) {
		t.Fatalf("expected the claude call in stdout, got:\n%s", out)
	}
	if n := strings.Count(out, rechteSkriptAgyHinweis); n != 1 {
		t.Fatalf("expected the agy env hinweis exactly once (only for agy), got %d times:\n%s", n, out)
	}
}
