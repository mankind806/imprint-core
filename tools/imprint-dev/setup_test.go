package main

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

func TestSetupPlanTreeInvariants(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tempHome, ".config"))

	root := t.TempDir()

	setupDir := filepath.Join(root, "setup")
	if err := os.MkdirAll(setupDir, 0755); err != nil {
		t.Fatalf("failed to create setup dir: %v", err)
	}

	srcDir := filepath.Join(root, "src")
	if err := os.MkdirAll(srcDir, 0755); err != nil {
		t.Fatalf("failed to create src dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, "file_gleich.txt"), []byte("content same\n"), 0644); err != nil {
		t.Fatalf("failed to write source file: %v", err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, "file_abweichend.txt"), []byte("content source\n"), 0644); err != nil {
		t.Fatalf("failed to write source file: %v", err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, "file_fehlt.txt"), []byte("content fehlt\n"), 0644); err != nil {
		t.Fatalf("failed to write source file: %v", err)
	}

	inventoryJSON := `[
		{
			"id": "item-gleich",
			"typ": "copy",
			"quelle": "src/file_gleich.txt",
			"ziel": "${HOME}/file_gleich.txt",
			"rechte": false,
			"beschreibung": "Equal file"
		},
		{
			"id": "item-abweichend",
			"typ": "shim",
			"quelle": "src/file_abweichend.txt",
			"ziel": "${XDG_CONFIG_HOME}/file_abweichend.txt",
			"rechte": true,
			"beschreibung": "Differing file"
		},
		{
			"id": "item-fehlt",
			"typ": "githook",
			"quelle": "src/file_fehlt.txt",
			"ziel": "${HOME}/missing_file.txt",
			"rechte": false,
			"beschreibung": "Missing file"
		}
	]`
	if err := os.WriteFile(filepath.Join(setupDir, "inventar.json"), []byte(inventoryJSON), 0644); err != nil {
		t.Fatalf("failed to write inventar.json: %v", err)
	}

	if err := os.WriteFile(filepath.Join(tempHome, "file_gleich.txt"), []byte("content same\n"), 0644); err != nil {
		t.Fatalf("failed to write target file: %v", err)
	}
	xdgDir := filepath.Join(tempHome, ".config")
	if err := os.MkdirAll(xdgDir, 0755); err != nil {
		t.Fatalf("failed to create xdg dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(xdgDir, "file_abweichend.txt"), []byte("content target diff\n"), 0644); err != nil {
		t.Fatalf("failed to write target file: %v", err)
	}

	beforeHash := hashTree(t, tempHome)

	var stdout1, stderr1 bytes.Buffer
	code1 := run([]string{"setup", "--plan", "--root", root}, &stdout1, &stderr1)
	if code1 != exitOK {
		t.Fatalf("expected exit code 0, got %d. stderr: %s", code1, stderr1.String())
	}

	if !bytes.Contains(stdout1.Bytes(), []byte("item-gleich")) ||
		!bytes.Contains(stdout1.Bytes(), []byte("gleich")) ||
		!bytes.Contains(stdout1.Bytes(), []byte("item-abweichend")) ||
		!bytes.Contains(stdout1.Bytes(), []byte("abweichend")) ||
		!bytes.Contains(stdout1.Bytes(), []byte("item-fehlt")) ||
		!bytes.Contains(stdout1.Bytes(), []byte("fehlt")) {
		t.Fatalf("stdout missing expected statuses, got:\n%s", stdout1.String())
	}

	afterHash := hashTree(t, tempHome)
	if beforeHash != afterHash {
		t.Fatalf("HOME tree was modified by setup --plan! before: %s, after: %s", beforeHash, afterHash)
	}

	var stdout2, stderr2 bytes.Buffer
	code2 := run([]string{"setup", "--plan", "--root", root}, &stdout2, &stderr2)
	if code2 != exitOK {
		t.Fatalf("second run expected exit code 0, got %d. stderr: %s", code2, stderr2.String())
	}

	if !bytes.Equal(stdout1.Bytes(), stdout2.Bytes()) {
		t.Fatalf("stdout mismatch between consecutive runs:\nrun 1:\n%s\nrun 2:\n%s", stdout1.String(), stdout2.String())
	}
}

func TestSetupValidationAndExitCodes(t *testing.T) {
	root := t.TempDir()

	var stdout, stderr bytes.Buffer
	code := run([]string{"setup", "--root", root}, &stdout, &stderr)
	if code != exitError {
		t.Errorf("expected exit error 2 when --plan is absent, got %d", code)
	}

	stderr.Reset()
	code = run([]string{"setup", "--plan", "--root", root}, &stdout, &stderr)
	if code != exitError {
		t.Errorf("expected exit error 2 when inventar.json is missing, got %d", code)
	}

	setupDir := filepath.Join(root, "setup")
	_ = os.MkdirAll(setupDir, 0755)
	badJSON := `[{"id": "test", "quelle": "q", "ziel": "z", "rechte": false, "beschreibung": "b"}]`
	_ = os.WriteFile(filepath.Join(setupDir, "inventar.json"), []byte(badJSON), 0644)

	stderr.Reset()
	code = run([]string{"setup", "--plan", "--root", root}, &stdout, &stderr)
	if code != exitError {
		t.Errorf("expected exit error 2 on malformed inventar.json, got %d", code)
	}

	badTypJSON := `[{"id": "test", "typ": "invalid-typ", "quelle": "q", "ziel": "z", "rechte": false, "beschreibung": "b"}]`
	_ = os.WriteFile(filepath.Join(setupDir, "inventar.json"), []byte(badTypJSON), 0644)

	stderr.Reset()
	code = run([]string{"setup", "--plan", "--root", root}, &stdout, &stderr)
	if code != exitError {
		t.Errorf("expected exit error 2 on invalid typ, got %d", code)
	}
}

func TestSetupRepoOption(t *testing.T) {
	root := t.TempDir()
	setupDir := filepath.Join(root, "setup")
	_ = os.MkdirAll(setupDir, 0755)
	_ = os.WriteFile(filepath.Join(root, "src.txt"), []byte("a\n"), 0644)
	validJSON := `[{"id": "root-item", "typ": "copy", "quelle": "src.txt", "ziel": "${HOME}/z.txt", "rechte": false, "beschreibung": "b"}]`
	_ = os.WriteFile(filepath.Join(setupDir, "inventar.json"), []byte(validJSON), 0644)

	var stdout, stderr bytes.Buffer
	code := run([]string{"setup", "--plan", "--root", root, "--repo", "/nonexistent/repo"}, &stdout, &stderr)
	if code != exitOK {
		t.Fatalf("expected exit code 0 when repo is missing, got %d", code)
	}
	if !bytes.Contains(stdout.Bytes(), []byte("note: repo setup inventory")) {
		t.Errorf("expected repo missing note in stdout, got:\n%s", stdout.String())
	}

	repoDir := t.TempDir()
	imprintDir := filepath.Join(repoDir, ".imprint")
	_ = os.MkdirAll(imprintDir, 0755)
	_ = os.WriteFile(filepath.Join(imprintDir, "setup.json"), []byte("{bad json}"), 0644)

	stdout.Reset()
	stderr.Reset()
	code = run([]string{"setup", "--plan", "--root", root, "--repo", repoDir}, &stdout, &stderr)
	if code != exitError {
		t.Fatalf("expected exit code 2 when repo setup.json is malformed, got %d", code)
	}
}

func hashTree(t *testing.T, dir string) string {
	t.Helper()
	var entries []fileHash
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(dir, p)
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		var data []byte
		if !d.IsDir() {
			data, err = os.ReadFile(p)
			if err != nil {
				return err
			}
		}
		entries = append(entries, fileHash{
			Name: rel,
			Mode: info.Mode(),
			Data: data,
		})
		return nil
	})
	if err != nil {
		t.Fatalf("hashTree failed: %v", err)
	}
	h := sha256.New()
	for _, e := range entries {
		fmt.Fprintf(h, "%s|%v|%x\n", e.Name, e.Mode, sha256.Sum256(e.Data))
	}
	return fmt.Sprintf("%x", h.Sum(nil))
}

type fileHash struct {
	Name string
	Mode os.FileMode
	Data []byte
}
