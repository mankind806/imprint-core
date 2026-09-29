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
	"strings"
	"text/tabwriter"
)

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

func parseInventory(data []byte) ([]setupEntry, error) {
	var entries []setupEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		return nil, fmt.Errorf("invalid JSON: %w", err)
	}
	for i, e := range entries {
		if strings.TrimSpace(e.ID) == "" {
			return nil, fmt.Errorf("entry %d: missing or empty 'id'", i+1)
		}
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
	}
	return entries, nil
}

// quelleEscapesRoot reports whether quelle, once cleaned, would resolve outside
// the inventory root it is joined with: an absolute path, or one that steps
// above the root via "..". Both are rejected so a crafted inventory (in
// particular an untrusted --repo's .imprint/setup.json) cannot make setup
// --plan read and echo back arbitrary files elsewhere on disk.
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

// zielEscapesErlaubteWurzeln reports whether ziel, after expandZiel, resolves outside
// the allowed root directories ($HOME, $XDG_CONFIG_HOME, $XDG_CACHE_HOME).
func zielEscapesErlaubteWurzeln(ziel string) bool {
	home := os.Getenv("HOME")
	xdgConfig := os.Getenv("XDG_CONFIG_HOME")
	if xdgConfig == "" && home != "" {
		xdgConfig = filepath.Join(home, ".config")
	}
	xdgCache := os.Getenv("XDG_CACHE_HOME")
	if xdgCache == "" && home != "" {
		xdgCache = filepath.Join(home, ".cache")
	}

	roots := []string{home, xdgConfig, xdgCache}
	zPath := expandZiel(ziel)

	for _, r := range roots {
		if strings.TrimSpace(r) == "" {
			continue
		}
		cleanRoot := filepath.Clean(r)
		rel, err := filepath.Rel(cleanRoot, zPath)
		if err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return false
		}
	}
	return true
}

func expandZiel(ziel string) string {
	home := os.Getenv("HOME")
	xdgConfig := os.Getenv("XDG_CONFIG_HOME")
	if xdgConfig == "" && home != "" {
		xdgConfig = filepath.Join(home, ".config")
	}
	xdgCache := os.Getenv("XDG_CACHE_HOME")
	if xdgCache == "" && home != "" {
		xdgCache = filepath.Join(home, ".cache")
	}
	s := ziel
	s = strings.ReplaceAll(s, "${XDG_CONFIG_HOME}", xdgConfig)
	s = strings.ReplaceAll(s, "$XDG_CONFIG_HOME", xdgConfig)
	s = strings.ReplaceAll(s, "${XDG_CACHE_HOME}", xdgCache)
	s = strings.ReplaceAll(s, "$XDG_CACHE_HOME", xdgCache)
	s = strings.ReplaceAll(s, "${HOME}", home)
	s = strings.ReplaceAll(s, "$HOME", home)
	return filepath.Clean(s)
}

func renderTemplate(content string) string {
	home := os.Getenv("HOME")
	xdgConfig := os.Getenv("XDG_CONFIG_HOME")
	if xdgConfig == "" && home != "" {
		xdgConfig = filepath.Join(home, ".config")
	}
	xdgCache := os.Getenv("XDG_CACHE_HOME")
	if xdgCache == "" && home != "" {
		xdgCache = filepath.Join(home, ".cache")
	}
	s := content
	s = strings.ReplaceAll(s, "${XDG_CONFIG_HOME}", xdgConfig)
	s = strings.ReplaceAll(s, "$XDG_CONFIG_HOME", xdgConfig)
	s = strings.ReplaceAll(s, "${XDG_CACHE_HOME}", xdgCache)
	s = strings.ReplaceAll(s, "$XDG_CACHE_HOME", xdgCache)
	s = strings.ReplaceAll(s, "${HOME}", home)
	s = strings.ReplaceAll(s, "$HOME", home)
	return s
}

func checkEnvGuard() bool {
	if val := os.Getenv("CLAUDECODE"); val != "" {
		return true
	}
	for _, env := range os.Environ() {
		parts := strings.SplitN(env, "=", 2)
		if len(parts) == 2 && parts[1] != "" {
			key := parts[0]
			if strings.HasPrefix(key, "ANTIGRAVITY_") || strings.HasPrefix(key, "AGY_") {
				return true
			}
		}
	}
	return false
}

type processedItem struct {
	invRoot string
	repo    string
	entry   setupEntry
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
		repoData, rErr := os.ReadFile(repoInvPath)
		if rErr != nil {
			repoNote = fmt.Sprintf("note: repo setup inventory at %s is missing or unreadable (%v)", repoInvPath, rErr)
		} else {
			repoEntries, pErr := parseInventory(repoData)
			if pErr != nil {
				return nil, "", fmt.Errorf("invalid repo inventory %s: %w", repoInvPath, pErr)
			}
			for _, e := range repoEntries {
				items = append(items, processedItem{invRoot: repo, repo: repo, entry: e})
			}
		}
	}

	return items, repoNote, nil
}

type evalResult struct {
	entry    setupEntry
	status   string
	reason   string
	diffText string
}

func evaluateEntry(e setupEntry, qPath, zPath string) evalResult {
	res := evalResult{entry: e}

	qData, qErr := os.ReadFile(qPath)
	if qErr != nil {
		res.status = "nicht-prüfbar"
		res.reason = fmt.Sprintf("quelle unreadable: %v", qErr)
		return res
	}

	zData, zErr := os.ReadFile(zPath)
	if zErr != nil {
		if errors.Is(zErr, fs.ErrNotExist) {
			res.status = "fehlt"
			return res
		}
		if zInfo, statErr := os.Stat(zPath); statErr == nil && zInfo.IsDir() {
			res.status = "nicht-prüfbar"
			res.reason = "ziel is a directory"
			return res
		}
		res.status = "nicht-prüfbar"
		res.reason = fmt.Sprintf("ziel unreadable: %v", zErr)
		return res
	}

	if bytes.Equal(qData, zData) {
		res.status = "gleich"
		return res
	}

	res.status = "abweichend"
	res.diffText = generateDiff(string(qData), string(zData))
	return res
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
	repo := fs.String("repo", "", "optional repository path")

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
	} else {
		return runSetupCheck(items, repoNote, stdout)
	}
}

func runSetupPlan(items []processedItem, repoNote string, stdout io.Writer) int {
	var results []evalResult
	for _, item := range items {
		e := item.entry
		if quelleEscapesRoot(e.Quelle) {
			results = append(results, evalResult{
				entry:  e,
				status: "nicht-prüfbar",
				reason: fmt.Sprintf("quelle %q verlässt den Inventar-Root (kein absoluter Pfad oder Traversal erlaubt)", e.Quelle),
			})
			continue
		}
		if zielEscapesErlaubteWurzeln(e.Ziel) {
			results = append(results, evalResult{
				entry:  e,
				status: "nicht-prüfbar",
				reason: fmt.Sprintf("ziel %q verlässt die erlaubten Wurzeln ($HOME, $XDG_CONFIG_HOME, $XDG_CACHE_HOME)", e.Ziel),
			})
			continue
		}
		qPath := filepath.Join(item.invRoot, filepath.FromSlash(e.Quelle))
		zPath := expandZiel(e.Ziel)

		res := evaluateEntry(e, qPath, zPath)
		if entryIsRechteGated(e) {
			// Rights-gated targets (rechte=true, typically real settings/permission
			// files) may hold live secrets, hooks or env values. --plan reports only
			// the status for them, never a diff or file content, so a transcript of
			// this command cannot leak that content.
			res.diffText = ""
		}
		results = append(results, res)
	}

	if repoNote != "" {
		fmt.Fprintln(stdout, repoNote)
	}

	tw := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "id\ttyp\tstatus\tbeschreibung")
	for _, r := range results {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", r.entry.ID, r.entry.Typ, r.status, r.entry.Beschreibung)
	}
	tw.Flush()

	for _, r := range results {
		if r.status == "nicht-prüfbar" && r.reason != "" {
			fmt.Fprintf(stdout, "\n[%s] nicht-prüfbar: %s\n", r.entry.ID, r.reason)
		} else if r.status == "abweichend" && r.diffText != "" && !entryIsRechteGated(r.entry) {
			fmt.Fprintf(stdout, "\n--- diff for %s (quelle vs ziel) ---\n%s\n", r.entry.ID, r.diffText)
		}
	}

	return exitOK
}

// entryIsRechteGated reports whether an entry's target is rights-gated
// (rechte=true): its content is never printed by --plan/--check/--apply,
// only its status or the command a human would run.
func entryIsRechteGated(e setupEntry) bool {
	return e.Rechte != nil && *e.Rechte
}

func runSetupApply(items []processedItem, repoNote string, stdout, stderr io.Writer) int {
	if repoNote != "" {
		fmt.Fprintln(stdout, repoNote)
	}

	type applyResult struct {
		entry  setupEntry
		status string
		action string
	}

	var results []applyResult

	for _, item := range items {
		e := item.entry
		if quelleEscapesRoot(e.Quelle) {
			results = append(results, applyResult{
				entry:  e,
				status: "Fehler",
				action: "quelle verlässt den Inventar-Root",
			})
			continue
		}
		if zielEscapesErlaubteWurzeln(e.Ziel) {
			results = append(results, applyResult{
				entry:  e,
				status: "Fehler",
				action: "ziel verlässt die erlaubten Wurzeln ($HOME, $XDG_CONFIG_HOME, $XDG_CACHE_HOME)",
			})
			continue
		}

		// Handle rechte=true ("Rechte-Tor")
		if entryIsRechteGated(e) {
			qPath := filepath.Join(item.invRoot, filepath.FromSlash(e.Quelle))
			qData, err := os.ReadFile(qPath)
			if err != nil {
				results = append(results, applyResult{entry: e, status: "Fehler", action: fmt.Sprintf("quelle unlesbar: %v", err)})
				continue
			}

			rendered := renderTemplate(string(qData))

			home := os.Getenv("HOME")
			xdgCache := os.Getenv("XDG_CACHE_HOME")
			if xdgCache == "" {
				xdgCache = filepath.Join(home, ".cache")
			}
			cacheDir := filepath.Join(xdgCache, "imprint")
			if err := os.MkdirAll(cacheDir, 0755); err != nil {
				results = append(results, applyResult{entry: e, status: "Fehler", action: fmt.Sprintf("cache dir konnte nicht erstellt werden: %v", err)})
				continue
			}

			zPath := expandZiel(e.Ziel)
			cachePath := filepath.Join(cacheDir, fmt.Sprintf("%s-%s", e.ID, filepath.Base(zPath)))
			if err := os.WriteFile(cachePath, []byte(rendered), 0644); err != nil {
				results = append(results, applyResult{entry: e, status: "Fehler", action: fmt.Sprintf("cache-datei konnte nicht geschrieben werden: %v", err)})
				continue
			}

			if zInfo, err := os.Stat(zPath); err == nil && !zInfo.IsDir() {
				fmt.Fprintf(stdout, "cp %s %s.bak\n", zPath, zPath)
			}
			fmt.Fprintf(stdout, "cp %s %s\n", cachePath, zPath)

			results = append(results, applyResult{entry: e, status: "gedruckt", action: "Vorlage in Cache gerendert, Befehl gedruckt"})
			continue
		}

		// Handle rechte=false
		switch e.Typ {
		case "shim":
			zPath := expandZiel(e.Ziel)
			expectedContent := fmt.Sprintf("#!/bin/sh\nexec \"$IMPRINT_CORE_ROOT/%s\" \"$@\"\n", filepath.ToSlash(e.Quelle))
			zData, err := os.ReadFile(zPath)
			if err == nil && string(zData) == expectedContent {
				results = append(results, applyResult{entry: e, status: "unverändert", action: "Shim existiert bereits und ist identisch"})
			} else {
				if err == nil {
					_ = os.WriteFile(zPath+".bak", zData, 0755)
				}
				if err := os.MkdirAll(filepath.Dir(zPath), 0755); err != nil {
					results = append(results, applyResult{entry: e, status: "Fehler", action: fmt.Sprintf("Ordner konnte nicht erstellt werden: %v", err)})
					continue
				}
				if err := os.WriteFile(zPath, []byte(expectedContent), 0755); err != nil {
					results = append(results, applyResult{entry: e, status: "Fehler", action: fmt.Sprintf("Shim konnte nicht geschrieben werden: %v", err)})
					continue
				}
				results = append(results, applyResult{entry: e, status: "geschrieben", action: "Shim erstellt"})
			}

		case "githook":
			cmdCheck := exec.Command("git", "-C", item.repo, "config", "--get", "core.hooksPath")
			out, err := cmdCheck.Output()
			if err == nil && strings.TrimSpace(string(out)) == ".githooks" {
				results = append(results, applyResult{entry: e, status: "unverändert", action: "git hooksPath ist bereits .githooks"})
			} else {
				cmdSet := exec.Command("git", "-C", item.repo, "config", "core.hooksPath", ".githooks")
				if err := cmdSet.Run(); err != nil {
					results = append(results, applyResult{entry: e, status: "Fehler", action: fmt.Sprintf("git config fehlgeschlagen: %v", err)})
				} else {
					results = append(results, applyResult{entry: e, status: "geschrieben", action: "git config core.hooksPath auf .githooks gesetzt"})
				}
			}

		case "copy", "systemd":
			qPath := filepath.Join(item.invRoot, filepath.FromSlash(e.Quelle))
			qData, err := os.ReadFile(qPath)
			if err != nil {
				results = append(results, applyResult{entry: e, status: "Fehler", action: fmt.Sprintf("quelle unlesbar: %v", err)})
				continue
			}
			zPath := expandZiel(e.Ziel)
			zData, err := os.ReadFile(zPath)
			if err == nil && bytes.Equal(qData, zData) {
				results = append(results, applyResult{entry: e, status: "unverändert", action: "Datei ist bereits identisch"})
			} else {
				if err == nil {
					_ = os.WriteFile(zPath+".bak", zData, 0644)
				}
				if err := os.MkdirAll(filepath.Dir(zPath), 0755); err != nil {
					results = append(results, applyResult{entry: e, status: "Fehler", action: fmt.Sprintf("Ordner konnte nicht erstellt werden: %v", err)})
					continue
				}
				if err := os.WriteFile(zPath, qData, 0644); err != nil {
					results = append(results, applyResult{entry: e, status: "Fehler", action: fmt.Sprintf("Datei konnte nicht geschrieben werden: %v", err)})
					continue
				}
				results = append(results, applyResult{entry: e, status: "geschrieben", action: "Datei kopiert"})
			}

		case "marketplace":
			fmt.Fprintf(stdout, "claude plugin marketplace add %s\n", e.Quelle)
			results = append(results, applyResult{entry: e, status: "gedruckt", action: "Marketplace-Befehl gedruckt"})

		case "mcp":
			fmt.Fprintf(stdout, "claude mcp add %s %s\n", e.ID, e.Quelle)
			results = append(results, applyResult{entry: e, status: "gedruckt", action: "MCP-Befehl gedruckt"})

		default:
			results = append(results, applyResult{entry: e, status: "Fehler", action: fmt.Sprintf("unbekannter typ %s", e.Typ)})
		}
	}

	tw := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "id\ttyp\tstatus\tbeschreibung")
	for _, r := range results {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", r.entry.ID, r.entry.Typ, r.status, r.entry.Beschreibung)
	}
	tw.Flush()

	return exitOK
}

func runSetupCheck(items []processedItem, repoNote string, stdout io.Writer) int {
	if repoNote != "" {
		fmt.Fprintln(stdout, repoNote)
	}

	type checkResult struct {
		entry  setupEntry
		status string
		reason string
	}

	var results []checkResult
	hasDrift := false

	for _, item := range items {
		e := item.entry
		if quelleEscapesRoot(e.Quelle) {
			results = append(results, checkResult{
				entry:  e,
				status: "nicht-prüfbar",
				reason: fmt.Sprintf("quelle %q verlässt den Inventar-Root", e.Quelle),
			})
			hasDrift = true
			continue
		}
		if zielEscapesErlaubteWurzeln(e.Ziel) {
			results = append(results, checkResult{
				entry:  e,
				status: "nicht-prüfbar",
				reason: fmt.Sprintf("ziel %q verlässt die erlaubten Wurzeln ($HOME, $XDG_CONFIG_HOME, $XDG_CACHE_HOME)", e.Ziel),
			})
			hasDrift = true
			continue
		}

		if e.Typ == "marketplace" || e.Typ == "mcp" {
			results = append(results, checkResult{entry: e, status: "hinweis"})
			continue
		}

		if e.Typ == "githook" {
			cmdCheck := exec.Command("git", "-C", item.repo, "config", "--get", "core.hooksPath")
			out, err := cmdCheck.Output()
			if err == nil && strings.TrimSpace(string(out)) == ".githooks" {
				results = append(results, checkResult{entry: e, status: "gleich"})
			} else {
				results = append(results, checkResult{entry: e, status: "abweichend"})
				hasDrift = true
			}
			continue
		}

		if e.Typ == "shim" {
			zPath := expandZiel(e.Ziel)
			expectedContent := fmt.Sprintf("#!/bin/sh\nexec \"$IMPRINT_CORE_ROOT/%s\" \"$@\"\n", filepath.ToSlash(e.Quelle))
			zData, err := os.ReadFile(zPath)
			if err != nil {
				if errors.Is(err, fs.ErrNotExist) {
					results = append(results, checkResult{entry: e, status: "fehlt"})
				} else {
					results = append(results, checkResult{entry: e, status: "nicht-prüfbar", reason: fmt.Sprintf("ziel unlesbar: %v", err)})
				}
				hasDrift = true
			} else if string(zData) == expectedContent {
				results = append(results, checkResult{entry: e, status: "gleich"})
			} else {
				results = append(results, checkResult{entry: e, status: "abweichend"})
				hasDrift = true
			}
			continue
		}

		// Handle copy, systemd, rechte-vorlage, and any rechte=true
		var sollData []byte
		qPath := filepath.Join(item.invRoot, filepath.FromSlash(e.Quelle))
		qData, err := os.ReadFile(qPath)
		if err != nil {
			results = append(results, checkResult{
				entry:  e,
				status: "nicht-prüfbar",
				reason: fmt.Sprintf("quelle unlesbar: %v", err),
			})
			hasDrift = true
			continue
		}

		if entryIsRechteGated(e) || e.Typ == "rechte-vorlage" {
			sollData = []byte(renderTemplate(string(qData)))
		} else {
			sollData = qData
		}

		zPath := expandZiel(e.Ziel)
		zData, err := os.ReadFile(zPath)
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				results = append(results, checkResult{entry: e, status: "fehlt"})
			} else {
				results = append(results, checkResult{entry: e, status: "nicht-prüfbar", reason: fmt.Sprintf("ziel unlesbar: %v", err)})
			}
			hasDrift = true
		} else if bytes.Equal(sollData, zData) {
			results = append(results, checkResult{entry: e, status: "gleich"})
		} else {
			results = append(results, checkResult{entry: e, status: "abweichend"})
			hasDrift = true
		}
	}

	tw := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "id\ttyp\tstatus\tbeschreibung")
	for _, r := range results {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", r.entry.ID, r.entry.Typ, r.status, r.entry.Beschreibung)
	}
	tw.Flush()

	for _, r := range results {
		if r.status == "nicht-prüfbar" && r.reason != "" {
			fmt.Fprintf(stdout, "\n[%s] nicht-prüfbar: %s\n", r.entry.ID, r.reason)
		}
	}

	if hasDrift {
		return exitViolation
	}
	return exitOK
}
